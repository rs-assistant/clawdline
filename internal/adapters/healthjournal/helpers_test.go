package healthjournal

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/connectionhealth"
)

type fakeClock struct {
	mu  sync.Mutex
	now connectionhealth.Instant
}

func newClock(wall time.Time) *fakeClock {
	return &fakeClock{now: connectionhealth.Instant{Mono: time.Second, Wall: wall.UTC()}}
}

func (c *fakeClock) Now() connectionhealth.Instant {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now.Mono += d
	c.now.Wall = c.now.Wall.Add(d)
	c.mu.Unlock()
}

var t0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

// producer stamps records through the real foundation journal, the way the
// daemon will hand them to Offer.
type producer struct {
	j    *connectionhealth.Journal
	last uint64
}

func newProducer(t *testing.T, clock connectionhealth.Clock, boot, build string) *producer {
	t.Helper()
	j, err := connectionhealth.NewJournal(clock, boot, build)
	if err != nil {
		t.Fatal(err)
	}
	return &producer{j: j}
}

// next returns every record stamped since the last call, the boot record
// first on the first call.
func (p *producer) next(t *testing.T, n int) []connectionhealth.Record {
	t.Helper()
	for i := 0; i < n; i++ {
		err := p.j.Append(connectionhealth.Record{
			Event: connectionhealth.EventStallDetected, Source: connectionhealth.SourceDaemon,
			Stage: connectionhealth.StageHandlerStarted, Class: connectionhealth.ClassRead,
			Kind: connectionhealth.Finite, Location: "cloud.link.read_handler",
			Correlation: "tr-1", AgeNS: int64(i), Rule: connectionhealth.RuleVersion,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	var out []connectionhealth.Record
	for {
		page, err := p.j.Read(connectionhealth.Query{AfterSeq: p.last})
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, page.Records...)
		p.last = page.Coverage.NextAfterSeq
		if !page.Coverage.Truncated {
			return out
		}
	}
}

func offerAll(t *testing.T, s *Store, recs []connectionhealth.Record) []Receipt {
	t.Helper()
	var out []Receipt
	for _, r := range recs {
		rc, err := s.Offer(r)
		if err != nil {
			t.Fatalf("offer %d: %v", r.Seq, err)
		}
		out = append(out, rc)
	}
	return out
}

func open(t *testing.T, o Options) *Store {
	t.Helper()
	s, err := Open(o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeNow(t, s) })
	return s
}

func closeNow(t *testing.T, s *Store) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.Close(ctx); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func flush(t *testing.T, s *Store) uint64 {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	n, err := s.Flush(ctx)
	if err != nil {
		t.Fatalf("flush: %v", err)
	}
	return n
}

// readAll pages through everything from cursor with pages of limit.
func readAll(t *testing.T, s *Store, cursor string, limit int) ([]Entry, []Page) {
	t.Helper()
	var entries []Entry
	var pages []Page
	for i := 0; i < 10000; i++ {
		p, err := s.Read(Query{Cursor: cursor, Limit: limit})
		if err != nil {
			t.Fatalf("read %q: %v", cursor, err)
		}
		pages = append(pages, p)
		entries = append(entries, p.Entries...)
		if p.Coverage.NextCursor == cursor && len(p.Entries) == 0 {
			return entries, pages
		}
		cursor = p.Coverage.NextCursor
		if !p.Coverage.Truncated && len(p.Entries) == 0 {
			return entries, pages
		}
	}
	t.Fatal("pagination did not end")
	return nil, nil
}

// faultFile wraps a real segment file and fails or blocks on demand.
type faultFile struct {
	*os.File
	ctl *faults
}

type faults struct {
	mu       sync.Mutex
	writeErr error
	syncErr  error
	block    chan struct{} // non-nil: Write waits for it to close
	entered  chan struct{} // signalled when a Write starts blocking
	failOpen error
}

func (f *faults) get() (error, error, chan struct{}, chan struct{}) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.writeErr, f.syncErr, f.block, f.entered
}

// release lets a blocked Write through and stops blocking new ones.
func (f *faults) release() {
	f.mu.Lock()
	b := f.block
	f.block = nil
	f.mu.Unlock()
	close(b)
}

func (f *faults) set(fn func(*faults)) {
	f.mu.Lock()
	fn(f)
	f.mu.Unlock()
}

func (w faultFile) Write(b []byte) (int, error) {
	werr, _, block, entered := w.ctl.get()
	if block != nil {
		if entered != nil {
			select {
			case entered <- struct{}{}:
			default:
			}
		}
		<-block
	}
	if werr != nil {
		// A torn write: half the bytes reach the file, then the error.
		n, _ := w.File.Write(b[:len(b)/2])
		return n, werr
	}
	return w.File.Write(b)
}

func (w faultFile) Sync() error {
	if _, serr, _, _ := w.ctl.get(); serr != nil {
		return serr
	}
	return w.File.Sync()
}

func (f *faults) opener() func(string) (segmentFile, error) {
	return func(path string) (segmentFile, error) {
		f.mu.Lock()
		failOpen := f.failOpen
		f.mu.Unlock()
		if failOpen != nil {
			return nil, failOpen
		}
		file, err := createSegment(path)
		if err != nil {
			return nil, err
		}
		return faultFile{File: file.(*os.File), ctl: f}, nil
	}
}
