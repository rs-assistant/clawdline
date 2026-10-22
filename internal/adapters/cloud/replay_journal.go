package cloud

// The replay window, kept past the process that claimed it.
//
// `internal/domain/cloud`'s ReplayWindow refuses a (sender, seq) it has seen,
// but it lives in memory: before this file a restart — or, worse, every
// `wire()` after a key rotation — started an empty window, and a relay holding
// a viewer's recent envelope could hand it to this machine again and have it
// answered again. A command that has no durable receipt of its own
// (`work-gate-settings-update` is one) would then take effect twice.
//
// Two rules close that together, and neither is enough alone:
//
//   - **An envelope's `ts` must be within MaxTimestampSkew of this machine's
//     clock** (the transport's admission ladder). The browser stamps `ts`
//     with its own clock and signs it, so the relay cannot move it; an
//     envelope that old is refused however it arrives.
//   - **Every claim inside that window is on disk before it is acted on.**
//     This journal appends one line per accepted claim and, when it is
//     opened, seeds the window with every claim whose `ts` is still young
//     enough to pass the first rule. So an envelope young enough to be
//     admitted is one this machine remembers, across a reconnect, a rotation
//     and a restart.
//
// What is kept is bounded three ways, each registered in
// `internal/domain/capacity`: rows, bytes, and age. Age is what lets a row go
// — a claim older than ReplayJournalSecondsLimit has a `ts` the first rule
// already refuses. Rows and bytes are a ceiling on a burst inside that age;
// past them a claim is refused rather than a younger row forgotten, because a
// forgotten row is exactly the replay this exists against.
//
// Appends are not synced one by one. A daemon that crashes or is restarted
// keeps every line (they are in the kernel's page cache already); a machine
// that loses power within the skew window may lose the last lines, and with
// them the memory of envelopes from the last moments before it went down.
// Syncing every terminal keystroke's envelope was the alternative, and it
// costs a disk flush per keystroke for a case that also needs the relay to
// be hostile at that exact moment.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/domain/cloud"
)

// ReplayJournalFile is the journal's name inside the cloud key directory.
const ReplayJournalFile = "replay-journal-v1"

// The journal's three bounds (`cloud.replay_journal_*` in the capacity
// register).
const (
	// ReplayJournalRowsLimit is how many claims the journal holds. At
	// 600 seconds of retention it is 109 envelopes a second, sustained, from
	// every viewer together.
	ReplayJournalRowsLimit = 65_536
	// ReplayJournalBytesLimit is the file's size. A row is at most a 128-byte
	// sender, two 20-digit numbers and three separators — 171 bytes — and a
	// usual one is under 80, so the row bound is normally the one reached.
	ReplayJournalBytesLimit = 8 << 20
	// ReplayJournalSecondsLimit is how long a claim is kept, measured from
	// the envelope's own `ts`. It must be at least MaxTimestampSkew: a claim
	// let go sooner would belong to an envelope the clock rule still admits.
	ReplayJournalSecondsLimit = 600
)

// ErrReplayJournalFull is a claim the journal had no room for once every row
// past its age was let go. The envelope is refused rather than accepted
// unrecorded.
var ErrReplayJournalFull = errors.New("the replay journal is full of claims young enough to matter")

// ErrReplayJournalUnreadable is a journal file that exists and is not one.
var ErrReplayJournalUnreadable = errors.New("the replay journal cannot be read")

// ReplayRecorder makes an accepted replay claim outlast this process. The
// transport calls it after the in-memory claim and before the envelope is
// opened or delivered; an error refuses the envelope.
type ReplayRecorder interface {
	Record(sender string, seq, ts uint64) error
}

type replayClaim struct {
	sender  string
	seq, ts uint64
}

// ReplayJournal is the on-disk half of the replay window.
type ReplayJournal struct {
	mu     sync.Mutex
	dir    string
	now    func() time.Time
	claims []replayClaim
	bytes  int64
	// rows and size are the two ceilings, ReplayJournalRowsLimit and
	// ReplayJournalBytesLimit unless a test lowers them.
	rows int
	size int64
}

var _ ReplayRecorder = (*ReplayJournal)(nil)

// OpenReplayJournal reads the journal in dir, claims every row still young
// enough to matter in window, and rewrites the file without the rest.
//
// A journal that exists and cannot be read is an error, never an empty
// journal: reading it as empty would reopen exactly the window it records.
// One unterminated last line is not an error — it is the append a crash cut
// short, and the envelope it was for was never acted on.
func OpenReplayJournal(dir string, now func() time.Time, window *cloud.ReplayWindow) (*ReplayJournal, error) {
	if now == nil {
		now = time.Now
	}
	if window == nil {
		return nil, errors.New("a replay journal needs the window it seeds")
	}
	j := &ReplayJournal{dir: dir, now: now, rows: ReplayJournalRowsLimit, size: ReplayJournalBytesLimit}
	data, err := j.read()
	if err != nil {
		return nil, err
	}
	claims, err := parseReplayJournal(data)
	if err != nil {
		return nil, err
	}
	j.claims = j.young(claims)
	for _, claim := range j.claims {
		// The order is the order they were first claimed, so the window ends
		// up as it was; a claim it refuses now (a sender past
		// MaxTrackedSenders) is still kept on disk for the next start.
		window.Claim(claim.sender, claim.seq)
	}
	if err := j.rewrite(); err != nil {
		return nil, err
	}
	return j, nil
}

// Path is where the journal is.
func (j *ReplayJournal) Path() string { return filepath.Join(j.dir, ReplayJournalFile) }

// Rows is how many claims the journal holds, for a test and a reading.
func (j *ReplayJournal) Rows() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return len(j.claims)
}

// Size is how many bytes the journal's file holds.
func (j *ReplayJournal) Size() int64 {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.bytes
}

// Record appends one claim. At a bound it first lets go every claim past its
// age; if that frees nothing, the claim is refused with ErrReplayJournalFull.
func (j *ReplayJournal) Record(sender string, seq, ts uint64) error {
	if sender == "" || strings.ContainsFunc(sender, func(r rune) bool { return r <= ' ' || r > '~' }) {
		// The envelope's own token rule already excludes these; refusing here
		// keeps a line from ever holding two claims.
		return errors.New("the replay journal cannot hold that sender id")
	}
	line := replayLine(replayClaim{sender: sender, seq: seq, ts: ts})
	j.mu.Lock()
	defer j.mu.Unlock()
	if len(j.claims)+1 > j.rows || j.bytes+int64(len(line)) > j.size {
		before := len(j.claims)
		j.claims = j.young(j.claims)
		if len(j.claims) < before {
			if err := j.rewrite(); err != nil {
				return err
			}
		}
		if len(j.claims)+1 > j.rows || j.bytes+int64(len(line)) > j.size {
			return ErrReplayJournalFull
		}
	}
	if err := j.append(line); err != nil {
		return err
	}
	j.claims = append(j.claims, replayClaim{sender: sender, seq: seq, ts: ts})
	j.bytes += int64(len(line))
	return nil
}

// young is every claim whose envelope could still pass the clock rule.
func (j *ReplayJournal) young(claims []replayClaim) []replayClaim {
	floor := j.now().Add(-ReplayJournalSecondsLimit * time.Second).UnixMilli()
	out := claims[:0:0]
	for _, claim := range claims {
		if floor < 0 || claim.ts >= uint64(floor) {
			out = append(out, claim)
		}
	}
	return out
}

func (j *ReplayJournal) read() ([]byte, error) {
	info, err := os.Lstat(j.Path())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrReplayJournalUnreadable, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %s is not a plain file", ErrReplayJournalUnreadable, j.Path())
	}
	if info.Size() > ReplayJournalBytesLimit+replayLineBytesLimit {
		return nil, fmt.Errorf("%w: %s is larger than its bound", ErrReplayJournalUnreadable, j.Path())
	}
	data, err := os.ReadFile(j.Path())
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrReplayJournalUnreadable, err)
	}
	return data, nil
}

// replayLineBytesLimit is the longest line a valid row can be: a sender of
// cloud.MaxSegmentBytes, two 20-digit numbers, two spaces and a newline. It is
// derived from those, not a bound of its own.
const replayLineBytesLimit = cloud.MaxSegmentBytes + 20 + 20 + 3

func parseReplayJournal(data []byte) ([]replayClaim, error) {
	lines := strings.Split(string(data), "\n")
	// Every whole line ends in a newline, so the last piece is empty — or it
	// is the append a crash cut short. Its envelope was never acted on (the
	// line is written before the envelope is opened), so it is let go.
	lines = lines[:len(lines)-1]
	claims := make([]replayClaim, 0, len(lines))
	for i, text := range lines {
		claim, ok := parseReplayLine(text)
		if !ok {
			return nil, fmt.Errorf("%w: line %d is not a claim", ErrReplayJournalUnreadable, i+1)
		}
		claims = append(claims, claim)
	}
	return claims, nil
}

func parseReplayLine(text string) (replayClaim, bool) {
	fields := strings.Split(text, " ")
	if len(fields) != 3 || fields[0] == "" || len(fields[0]) > cloud.MaxSegmentBytes {
		return replayClaim{}, false
	}
	seq, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return replayClaim{}, false
	}
	ts, err := strconv.ParseUint(fields[2], 10, 64)
	if err != nil {
		return replayClaim{}, false
	}
	return replayClaim{sender: fields[0], seq: seq, ts: ts}, true
}

func replayLine(claim replayClaim) string {
	return claim.sender + " " + strconv.FormatUint(claim.seq, 10) + " " + strconv.FormatUint(claim.ts, 10) + "\n"
}

// rewrite replaces the file with j.claims, atomically.
func (j *ReplayJournal) rewrite() error {
	var body strings.Builder
	for _, claim := range j.claims {
		body.WriteString(replayLine(claim))
	}
	if err := os.MkdirAll(j.dir, 0o700); err != nil {
		return err
	}
	if err := writeFileAtomically(j.dir, j.Path(), []byte(body.String())); err != nil {
		return err
	}
	j.bytes = int64(body.Len())
	return nil
}

// append writes one line to the end of the file. The file is opened for each
// line rather than held, so a link that is replaced or stopped holds nothing.
func (j *ReplayJournal) append(line string) error {
	if info, err := os.Lstat(j.Path()); err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("%w: %s is not a plain file", ErrReplayJournalUnreadable, j.Path())
	}
	file, err := os.OpenFile(j.Path(), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.WriteString(line); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
