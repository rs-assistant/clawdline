package connectionhealth

import (
	"context"
	"runtime"
	"testing"
	"time"
)

// A journal whose lock is held (a slow reader, a stuck writer) must not hold
// a scan. Every transition past the queue is dropped and counted, the report
// says the journal is partial, and after the lock is released the journal
// names the loss exactly where it happened.
func TestAScanNeverWaitsForAHeldJournal(t *testing.T) {
	c, tr, j, w := fixture(t)
	w.Scan()
	j.mu.Lock()
	done := make(chan Report)
	const gaps = MaxPendingTransitions + 44
	go func() {
		var r Report
		for i := 0; i < gaps; i++ {
			c.advance(3 * time.Second) // each scan is a scheduling gap: one transition
			r = w.Scan()
		}
		h, _ := tr.Begin(handlerSpec())
		for i := 0; i < 16; i++ {
			c.advance(time.Second)
			r = w.Scan()
		}
		_ = h
		done <- r
	}()
	var r Report
	select {
	case r = <-done:
	case <-time.After(5 * time.Second):
		j.mu.Unlock()
		t.Fatal("a scan waited on the journal's lock")
	}
	j.mu.Unlock()
	if r.ScanSeq != 1+gaps+16 || r.Transitions != QualityPartial || r.TransitionsPending != MaxPendingTransitions || r.TransitionsLost == 0 {
		t.Fatalf("report while the journal was held: seq %d transitions %s pending %d lost %d", r.ScanSeq, r.Transitions, r.TransitionsPending, r.TransitionsLost)
	}
	if len(r.Findings) != 1 || r.Findings[0].State != StateBlocked {
		t.Fatalf("a stall was not seen while the journal was held: %+v", r.Findings)
	}

	w.Flush()
	p, err := j.Read(Query{Limit: MaxQueryRecords})
	if err != nil {
		t.Fatal(err)
	}
	var all []Record
	for {
		all = append(all, p.Records...)
		if !p.Coverage.Truncated {
			break
		}
		p, _ = j.Read(Query{AfterSeq: p.Coverage.NextAfterSeq})
	}
	var lost uint64
	lostAt := -1
	for i, rec := range all {
		if rec.Seq != uint64(i+1) {
			t.Fatalf("sequence hole at %d: %d", i, rec.Seq)
		}
		if rec.Event == EventTransitionsLost {
			lost += rec.Count
			lostAt = i
		}
	}
	// boot, then the MaxPendingTransitions gaps that were queued, then the
	// loss.
	if lost != r.TransitionsLost || lostAt != 1+MaxPendingTransitions {
		t.Fatalf("journal says %d lost at %d; report says %d", lost, lostAt, r.TransitionsLost)
	}
	if q, _ := j.Read(Query{AfterSeq: uint64(lostAt), Limit: 1}); q.Coverage.Quality != QualityPartial {
		t.Fatalf("a page holding the loss reads %s", q.Coverage.Quality)
	}
	if q, _ := j.Read(Query{AfterSeq: uint64(lostAt + 1)}); q.Coverage.Quality != QualityComplete {
		t.Fatalf("a page after the loss reads %s", q.Coverage.Quality)
	}
	// The slow and stall transitions came while the queue was still full:
	// they are in the count, not silently missing, and the report kept them.
	// The loss is followed only by the stall that still stands, re-stated
	// at the same instant, so the journal cannot end on "loss, then quiet".
	if lostAt != len(all)-2 || all[lostAt].MonoNS != r.MonoNS ||
		all[lostAt+1].Event != EventStateAfterLoss || all[lostAt+1].State != StateBlocked {
		t.Fatalf("the loss and the standing stall are not the journal's last words at the last scan's time: %+v", all[lostAt:])
	}
}

// Run keeps publishing reports while the journal is held, uses exactly one
// writer, and leaves no goroutine behind once its context ends and the
// journal is free again.
func TestRunAdvancesWhileTheJournalIsHeldAndLeavesNothingRunning(t *testing.T) {
	before := runtime.NumGoroutine()
	clock := NewSystemClock()
	tr := NewTracker(clock)
	j, err := NewJournal(clock, "boot-a", "abc123")
	if err != nil {
		t.Fatal(err)
	}
	w := NewWatchdog(tr, j, clock, time.Millisecond)
	s := handlerSpec()
	s.Warn, s.Block = time.Millisecond, 2*time.Millisecond
	h, _ := tr.Begin(s)
	defer h.End(true)

	j.mu.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { w.Run(ctx); close(stopped) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if r, ok := w.Latest(); ok && r.ScanSeq >= 50 {
			if len(r.Findings) != 1 || (r.Findings[0].State != StateBlocked && r.Findings[0].State != StateUnverified) {
				t.Errorf("findings while held: %+v", r.Findings)
			}
			break
		}
		if time.Now().After(deadline) {
			j.mu.Unlock()
			cancel()
			t.Fatal("reports stopped advancing while the journal was held")
		}
		time.Sleep(time.Millisecond)
	}
	if n := runtime.NumGoroutine() - before; n > 2 {
		t.Errorf("Run uses %d goroutines, want the scanner and one writer", n)
	}
	cancel()
	j.mu.Unlock()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after its context ended")
	}
	for i := 0; runtime.NumGoroutine() > before; i++ {
		if i > 500 {
			t.Fatalf("%d goroutines left after Run returned", runtime.NumGoroutine()-before)
		}
		time.Sleep(time.Millisecond)
	}
	if _, ok := w.Latest(); !ok {
		t.Fatal("no report published")
	}
}

// A watchdog without a journal says its transitions are not collected,
// rather than reading as a complete, empty journal.
func TestNoJournalIsNotCollectedNotComplete(t *testing.T) {
	c := newFakeClock()
	w := NewWatchdog(NewTracker(c), nil, c, time.Second)
	w.Flush()
	if r := w.Scan(); r.Transitions != QualityNotCollected || r.TransitionsLost != 0 {
		t.Fatalf("%+v", r)
	}
}
