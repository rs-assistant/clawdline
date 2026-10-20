package healthupload

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// StateSchema names the progress file.
const StateSchema = "clawdline.connection-health.upload-state/1"

// stateFile is the progress file's name inside Options.StateDir.
const stateFile = "upload-state.json"

// persisted is everything that must survive a restart: where the next batch
// starts, the one batch in flight with its exact body (so a retry after a
// restart sends the same bytes under the same batch_id), this clock hour's
// spending, and the lifetime counts. Its size is bounded by MaxStateBytes:
// one body of at most MaxBatchBytes, at most MaxReceipts receipts and a
// fixed set of counters.
type persisted struct {
	Schema string `json:"schema"`
	// size is the progress file's length as last read or written, and sized
	// whether either has happened; neither is stored in the file.
	size  int
	sized bool
	// Cursor resumes after the last entry whose batch was settled
	// (receipted, refused or abandoned). Empty starts at the oldest
	// retained entry.
	Cursor  string        `json:"cursor"`
	Pending *pendingBatch `json:"pending,omitempty"`
	Hour    hourBudget    `json:"hour"`
	Totals  Totals        `json:"totals"`
	// Receipts are the newest durable receipts, newest last.
	Receipts []Receipt `json:"receipts,omitempty"`
}

type pendingBatch struct {
	BatchID    string    `json:"batch_id"`
	BootID     string    `json:"boot_id"`
	Body       []byte    `json:"body"`
	NextCursor string    `json:"next_cursor"`
	Events     int       `json:"events"`
	FirstSeq   uint64    `json:"first_seq"`
	LastSeq    uint64    `json:"last_seq"`
	Created    time.Time `json:"created"`
	Attempts   int       `json:"attempts"`
	NotBefore  time.Time `json:"not_before"`
	// Skipped is what the batch's span holds that is not sent, applied to
	// the totals only when the batch settles, so a restart cannot count it
	// twice.
	Skipped skipped `json:"skipped"`
}

// skipped counts what a span of the durable store held besides sendable
// records.
type skipped struct {
	Excluded map[string]uint64 `json:"excluded,omitempty"`
	// StorageLoss is the records the durable store says never reached disk.
	StorageLoss uint64 `json:"storage_loss,omitempty"`
	// Gaps counts store gaps (pruned, missing, corrupt, torn) by reason.
	Gaps map[string]uint64 `json:"gaps,omitempty"`
}

func (s *skipped) exclude(reason string) {
	if s.Excluded == nil {
		s.Excluded = map[string]uint64{}
	}
	s.Excluded[reason]++
}

func (s *skipped) gap(reason string) {
	if s.Gaps == nil {
		s.Gaps = map[string]uint64{}
	}
	s.Gaps[reason]++
}

func (s skipped) empty() bool { return len(s.Excluded) == 0 && s.StorageLoss == 0 && len(s.Gaps) == 0 }

type hourBudget struct {
	Start    time.Time `json:"start"`
	Bytes    int64     `json:"bytes"`
	Requests int64     `json:"requests"`
}

// Totals are lifetime counts across restarts.
type Totals struct {
	// Receipted counts events Cloud stored under a durable receipt;
	// Duplicates those it answered duplicate_event (already stored).
	Receipted  uint64 `json:"receipted_events"`
	Duplicates uint64 `json:"duplicate_events"`
	Batches    uint64 `json:"receipted_batches"`
	// Refused counts batches and events Cloud refused for good (4xx other
	// than 401/403/429): they are not resent.
	RefusedBatches uint64 `json:"refused_batches"`
	RefusedEvents  uint64 `json:"refused_events"`
	// Abandoned counts batches given up after MaxAttempts or MaxRetrySeconds
	// without a receipt: Cloud may or may not hold them (persisted unknown).
	AbandonedBatches uint64            `json:"abandoned_batches"`
	AbandonedEvents  uint64            `json:"abandoned_events"`
	Excluded         map[string]uint64 `json:"excluded,omitempty"`
	StorageLoss      uint64            `json:"storage_loss_records"`
	Gaps             map[string]uint64 `json:"store_gaps,omitempty"`
	Attempts         uint64            `json:"attempts"`
	BytesSent        uint64            `json:"bytes_sent"`
}

func (t *Totals) add(s skipped) {
	for k, v := range s.Excluded {
		if t.Excluded == nil {
			t.Excluded = map[string]uint64{}
		}
		t.Excluded[k] += v
	}
	for k, v := range s.Gaps {
		if t.Gaps == nil {
			t.Gaps = map[string]uint64{}
		}
		t.Gaps[k] += v
	}
	t.StorageLoss += s.StorageLoss
}

// Receipt is what this daemon keeps of a durable Cloud receipt.
type Receipt struct {
	ReceiptID   string `json:"receipt_id"`
	BatchID     string `json:"batch_id"`
	BootID      string `json:"boot_id"`
	Digest      string `json:"digest"`
	PersistedAt string `json:"persisted_at"`
	RetainUntil string `json:"retain_until"`
	Accepted    int    `json:"accepted"`
	Duplicates  int    `json:"duplicates"`
	FirstSeq    uint64 `json:"first_seq"`
	LastSeq     uint64 `json:"last_seq"`
	// Replay is true when Cloud answered 200: the batch was already stored.
	Replay bool `json:"replay"`
}

var errStateTooLarge = errors.New("healthupload: progress state exceeds its bound")

// loadState reads the progress file. Missing is a fresh start; a file that
// exists and cannot be read or parsed is an error, never a fresh start,
// because starting over would resend the whole store under new batches.
func loadState(dir string) (persisted, bool, error) {
	data, err := os.ReadFile(filepath.Join(dir, stateFile))
	if errors.Is(err, fs.ErrNotExist) {
		return persisted{Schema: StateSchema, sized: true}, false, nil
	}
	if err != nil {
		return persisted{}, false, err
	}
	if len(data) > MaxStateBytes {
		return persisted{}, false, errStateTooLarge
	}
	var p persisted
	if err := json.Unmarshal(data, &p); err != nil || p.Schema != StateSchema {
		return persisted{}, false, errors.New("healthupload: progress state is not readable")
	}
	if p.Pending != nil && (len(p.Pending.Body) == 0 || len(p.Pending.Body) > MaxBatchBytes) {
		return persisted{}, false, errors.New("healthupload: progress state holds an invalid batch")
	}
	p.size, p.sized = len(data), true
	return p, true, nil
}

// saveState replaces the progress file atomically: write a temporary file,
// fsync it, rename it over the old one, fsync the directory. It answers the
// bytes written.
func saveState(dir string, p persisted) (int, error) {
	data, err := json.Marshal(p)
	if err != nil {
		return 0, err
	}
	if len(data) > MaxStateBytes {
		return 0, errStateTooLarge
	}
	return len(data), writeState(dir, data)
}

func writeState(dir string, data []byte) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, stateFile+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, filepath.Join(dir, stateFile)); err != nil {
		return err
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}
