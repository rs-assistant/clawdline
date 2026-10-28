package cloud

import (
	"crypto/ed25519"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/cloud"
)

func ms(at time.Time) uint64 { return uint64(at.UnixMilli()) }

// What one process claimed, the next one refuses — and only for as long as
// the clock rule could still admit the envelope.
func TestAReplayClaimOutlivesTheProcessAndExpiresWithItsClock(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	clock := func() time.Time { return now }
	window := cloud.NewReplayWindow(0)
	journal, err := OpenReplayJournal(dir, clock, window)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		seq uint64
		ts  time.Time
	}{{10, now}, {11, now.Add(-time.Minute)}, {3, now.Add(-20 * time.Minute)}} {
		if window.Claim("dev_a", c.seq) != cloud.ReplayAccepted {
			t.Fatalf("seq %d was not a fresh claim", c.seq)
		}
		if err := journal.Record("dev_a", c.seq, ms(c.ts)); err != nil {
			t.Fatal(err)
		}
	}
	// The next process: a new window seeded from the file.
	reopened := cloud.NewReplayWindow(0)
	if _, err := OpenReplayJournal(dir, clock, reopened); err != nil {
		t.Fatal(err)
	}
	for _, seq := range []uint64{10, 11} {
		if reopened.Claim("dev_a", seq) != cloud.ReplayRefused {
			t.Fatalf("seq %d, claimed before the restart, was claimable again", seq)
		}
	}
	if reopened.Claim("dev_a", 12) != cloud.ReplayAccepted {
		t.Fatal("the next sequence was refused")
	}
	// Seq 3 was stamped twenty minutes ago: the clock rule refuses its
	// envelope, so the journal let it go and the file no longer holds it.
	data, err := os.ReadFile(filepath.Join(dir, ReplayJournalFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "dev_a 3 ") || strings.Count(string(data), "\n") != 2 {
		t.Fatalf("the reopened journal holds %q", data)
	}
	if info, err := os.Stat(filepath.Join(dir, ReplayJournalFile)); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the journal is not owner-only: %v %v", info.Mode(), err)
	}
	if ReplayJournalSecondsLimit*time.Second < cloud.MaxTimestampSkew {
		t.Fatal("a claim would be let go while the clock rule still admits its envelope")
	}
}

// A crash can cut the last append short; its envelope was never acted on.
// Anything else that is not a claim is not an empty journal.
func TestAnUnreadableReplayJournalIsNotAnEmptyOne(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }
	write := func(t *testing.T, body string) string {
		t.Helper()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, ReplayJournalFile), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	line := "dev_a 4 " + itoaMS(now) + "\n"

	torn := write(t, line+"dev_a 5 17")
	window := cloud.NewReplayWindow(0)
	if _, err := OpenReplayJournal(torn, clock, window); err != nil {
		t.Fatalf("a torn last line refused the journal: %v", err)
	}
	if window.Claim("dev_a", 4) != cloud.ReplayRefused {
		t.Fatal("the whole line before the torn one was lost")
	}

	for name, body := range map[string]string{
		"a line that is not a claim": "nonsense\n" + line,
		"a sequence that is not one": "dev_a x " + itoaMS(now) + "\n",
	} {
		if _, err := OpenReplayJournal(write(t, body), clock, cloud.NewReplayWindow(0)); !errors.Is(err, ErrReplayJournalUnreadable) {
			t.Errorf("%s: answered %v", name, err)
		}
	}
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ReplayJournalFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenReplayJournal(dir, clock, cloud.NewReplayWindow(0)); !errors.Is(err, ErrReplayJournalUnreadable) {
		t.Errorf("a directory in the journal's place answered %v", err)
	}
}

// At its bound the journal lets go what has aged out and nothing younger: a
// claim it has no room for is refused, not recorded over somebody's recent row.
func TestAFullReplayJournalRefusesRatherThanForgets(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }
	journal, err := OpenReplayJournal(t.TempDir(), clock, cloud.NewReplayWindow(0))
	if err != nil {
		t.Fatal(err)
	}
	journal.rows = 3
	if err := journal.Record("dev_a", 1, ms(now.Add(-time.Hour))); err != nil {
		t.Fatal(err)
	}
	for seq := uint64(2); seq <= 3; seq++ {
		if err := journal.Record("dev_a", seq, ms(now)); err != nil {
			t.Fatal(err)
		}
	}
	// Full; the hour-old row goes to make room.
	if err := journal.Record("dev_a", 4, ms(now)); err != nil {
		t.Fatalf("an aged-out row was not let go: %v", err)
	}
	if err := journal.Record("dev_a", 5, ms(now)); !errors.Is(err, ErrReplayJournalFull) {
		t.Fatalf("a journal of young rows answered %v", err)
	}
	if journal.Rows() != 3 {
		t.Fatalf("the journal holds %d rows, wanted its bound", journal.Rows())
	}
	journal.size = journal.bytes
	journal.rows = ReplayJournalRowsLimit
	if err := journal.Record("dev_a", 6, ms(now)); !errors.Is(err, ErrReplayJournalFull) {
		t.Fatalf("a journal at its byte bound answered %v", err)
	}
	if err := journal.Record("dev a", 7, ms(now)); err == nil {
		t.Fatal("a sender the line format cannot hold was written")
	}
}

// The transport writes the claim before it opens the envelope, and refuses an
// envelope whose claim could not be written.
func TestAnEnvelopeWhoseClaimCannotBeWrittenIsRefused(t *testing.T) {
	recorder := &failingRecorder{err: ErrReplayJournalFull}
	tr, raw := replayFixture(t, recorder)
	if tr.AcceptDirect("viewer-1", raw) {
		t.Fatal("an envelope was accepted while its claim could not be kept")
	}
	if got := tr.Status().InboundDropped[DropReplayWindowFull]; got != 1 || recorder.calls != 1 {
		t.Fatalf("drops %v, record calls %d", tr.Status().InboundDropped, recorder.calls)
	}
	recorder = &failingRecorder{err: errors.New("disk")}
	tr, raw = replayFixture(t, recorder)
	if tr.AcceptDirect("viewer-1", raw) || tr.Status().InboundDropped[DropReplayUnrecorded] != 1 {
		t.Fatalf("an unwritable claim answered %v", tr.Status().InboundDropped)
	}
	recorder = &failingRecorder{}
	tr, raw = replayFixture(t, recorder)
	if !tr.AcceptDirect("viewer-1", raw) || recorder.calls != 1 || recorder.sender != "viewer-1" || recorder.seq != 17 {
		t.Fatalf("a recorded claim: accepted? calls %d %+v", recorder.calls, recorder)
	}
}

type failingRecorder struct {
	err    error
	calls  int
	sender string
	seq    uint64
}

func (r *failingRecorder) Record(sender string, seq, _ uint64) error {
	r.calls++
	r.sender, r.seq = sender, seq
	return r.err
}

func itoaMS(at time.Time) string {
	return strings.TrimSpace(strings.Split(replayLine(replayClaim{sender: "x", ts: ms(at)}), " ")[2])
}

// replayFixture is a machine's transport with one viewer it knows, and that
// viewer's sealed r/ read at seq 17.
func replayFixture(t *testing.T, recorder ReplayRecorder) (*Transport, []byte) {
	t.Helper()
	viewer, err := cloud.NewDeviceKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	master, err := cloud.NewContentKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	tr := &Transport{opts: Options{
		Role: "machine", Identity: Identity{MachineID: "machine"}, Status: NewStatusRecorder(time.Now()),
		Replay: cloud.NewReplayWindow(8), ReplayJournal: recorder, ContentKey: master, Now: time.Now,
		PublicKeyFor: func(sender string) (ed25519.PublicKey, bool) {
			return viewer.PublicKey(), sender == "viewer-1"
		},
	}}
	env, err := cloud.Seal([]byte(`{"v":1}`), cloud.SealParams{Ch: "r/machine", Seq: 17, Ts: ms(time.Now()),
		Class: cloud.ClassCtl, KeyID: MasterKeyID, Sender: "viewer-1", Key: master, Signer: viewer})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := env.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	return tr, raw
}

// A claim is on disk, not only in the page cache, before its envelope is
// acted on: a write the disk did not confirm refuses the envelope, and the
// claim is not counted as held.
func TestAReplayClaimTheDiskDidNotConfirmIsRefused(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }
	journal, err := OpenReplayJournal(t.TempDir(), clock, cloud.NewReplayWindow(0))
	if err != nil {
		t.Fatal(err)
	}
	synced := 0
	journal.syncFile = func(*os.File) error { synced++; return nil }
	if err := journal.Record("viewer-01", 1, ms(now)); err != nil {
		t.Fatal(err)
	}
	if synced != 1 {
		t.Fatalf("an accepted claim was synced %d times", synced)
	}
	journal.syncFile = func(*os.File) error { return errors.New("the disk did not confirm") }
	if err := journal.Record("viewer-01", 2, ms(now)); err == nil {
		t.Fatal("a claim the disk did not confirm was accepted")
	}
	if journal.Rows() != 1 {
		t.Fatalf("an unconfirmed claim was counted: %d rows", journal.Rows())
	}
}

// The cost of that confirmation, per claim, on this machine's disk.
func BenchmarkReplayJournalRecord(b *testing.B) {
	now := time.Now()
	journal, err := OpenReplayJournal(b.TempDir(), func() time.Time { return now }, cloud.NewReplayWindow(0))
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < b.N; i++ {
		if err := journal.Record("viewer-01", uint64(i+1), ms(now)); err != nil {
			b.Fatal(err)
		}
	}
}
