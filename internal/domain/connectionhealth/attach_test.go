package connectionhealth

import (
	"context"
	"sync"
	"testing"
	"time"
)

// Attach offers what the ring already holds, once and in order, then every
// record the writer appends as stamped — the same records Read returns, with
// no gap and no repeat, and without re-reading the ring per record.
func TestAttachHandsOnEveryStampedRecordOnce(t *testing.T) {
	c, tr, j, w := fixture(t)
	w.Scan()
	h, _ := tr.Begin(handlerSpec())
	for i := 0; i < 6; i++ {
		c.advance(time.Second)
		w.Scan()
	}
	w.Flush() // slow_detected is in the ring before the store attaches
	var mu sync.Mutex
	var got []Record
	w.Attach(func(r Record) { mu.Lock(); got = append(got, r); mu.Unlock() })
	for i := 0; i < 10; i++ {
		c.advance(time.Second)
		w.Scan()
	}
	w.Flush()
	h.End(true)
	c.advance(time.Second)
	w.Scan()
	w.Flush()
	p, err := j.Read(Query{})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != len(p.Records) || len(got) < 4 {
		t.Fatalf("offered %d records, the ring holds %d", len(got), len(p.Records))
	}
	for i := range got {
		if got[i] != p.Records[i] {
			t.Fatalf("record %d offered as %+v, stamped as %+v", i, got[i], p.Records[i])
		}
	}
	if got[0].Event != EventBoot || got[len(got)-1].Event != EventReleasedStalled {
		t.Fatalf("first %s last %s", got[0].Event, got[len(got)-1].Event)
	}
}

// A durable store that blocks — a disk that hangs inside Offer, which the
// store's contract says never happens but a broken disk might make happen —
// holds only the journal writer: scans keep their cadence, call sites keep
// going, and Run still returns when the store lets go.
func TestABlockedStoreHoldsNeitherTheScanNorTheCallSites(t *testing.T) {
	c := newFakeClock()
	tr := NewTracker(c)
	j, _ := NewJournal(c, "boot-a", "abc123")
	w := NewWatchdog(tr, j, c, time.Millisecond)
	release := make(chan struct{})
	w.Attach(func(r Record) {
		if r.Event != EventBoot {
			<-release
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	h := tr.Start(handlerSpec())
	deadline := time.Now().Add(5 * time.Second)
	var r Report
	for {
		c.advance(time.Second)
		if rep, ok := w.Latest(); ok {
			r = rep
		}
		if r.ScanSeq > 60 && len(r.Findings) == 1 && r.Findings[0].State == StateBlocked {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the scan stopped behind a blocked store: %+v", r)
		}
		for i := 0; i < 100; i++ {
			x := tr.Start(handlerSpec())
			_ = x.Step(StageHandlerCompleted, "cloud.link.read_handler")
			x.End(true)
		}
		time.Sleep(time.Millisecond)
	}
	h.End(true)
	cancel()
	close(release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return once the store let go")
	}
}
