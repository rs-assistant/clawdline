package connectionhealth

import (
	"context"
	"testing"
	"time"
)

// A stall and its recovery are queued for the writer when the daemon stops.
// Run must hand both to the journal and the durable sink before it returns:
// the lifecycle flushes the durable store only after Run, and a flush cannot
// see what was never offered. Go's select picks either ready branch, so this
// repeats what used to fail one schedule in two.
func TestACanceledRunJournalsTheQueuedStallAndRecovery(t *testing.T) {
	for i := 0; i < 100; i++ {
		c, tr, j, w := fixture(t)
		var sunk []Event
		w.Attach(func(r Record) { sunk = append(sunk, r.Event) })
		stallThenRecover(t, c, tr, w)
		before := len(w.pending)
		if before < 3 {
			t.Fatalf("want a queued slow, stall and recovery, have %d", before)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		w.Run(ctx)
		if n := len(w.pending); n != 0 {
			t.Fatalf("run %d: Run returned with %d/%d transitions still queued", i, n, before)
		}
		got := events(t, j)
		if !hasEvent(got, EventStallDetected) || !hasEvent(got, EventRecovered) {
			t.Fatalf("run %d: journal lacks the stall or its recovery: %v", i, got)
		}
		if !hasEvent(sunk, EventStallDetected) || !hasEvent(sunk, EventRecovered) {
			t.Fatalf("run %d: durable sink lacks the stall or its recovery: %v", i, sunk)
		}
	}
}

// A durable sink that never returns holds only the writer: Run's scans have
// already stopped, Pending says what is still queued, and the writer
// finishes once the sink lets go, without a second goroutine.
func TestAHeldSinkHoldsOnlyTheWriterAndReportsWhatIsQueued(t *testing.T) {
	c, tr, _, w := fixture(t)
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	w.Attach(func(r Record) {
		if r.Event != EventStallDetected {
			return
		}
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
	})
	stallThenRecover(t, c, tr, w)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); w.Run(ctx) }()
	<-entered
	cancel()
	select {
	case <-done:
		t.Fatal("Run returned while its writer was still holding a transition")
	case <-time.After(50 * time.Millisecond):
	}
	if n, _ := w.Pending(); n < 1 {
		t.Fatalf("the recovery behind the held stall is not reported as unwritten: %d", n)
	}
	close(release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after the sink let go")
	}
	if n, _ := w.Pending(); n != 0 {
		t.Fatalf("%d transitions left after the writer drained", n)
	}
}

func hasEvent(es []Event, want Event) bool {
	for _, e := range es {
		if e == want {
			return true
		}
	}
	return false
}

// stallThenRecover scans every second while one phase passes its warn and
// block deadlines, then progresses: slow, stall and recovery are queued for
// the writer, none journaled yet.
func stallThenRecover(t *testing.T, c *fakeClock, tr *Tracker, w *Watchdog) {
	t.Helper()
	p := tr.Start(handlerSpec())
	w.Scan()
	for i := 0; i < 16; i++ {
		c.advance(time.Second)
		w.Scan()
	}
	if err := p.Progress(StageHandlerStarted); err != nil {
		t.Fatal(err)
	}
	c.advance(time.Second)
	w.Scan()
}

// The cost a stop adds: draining a full transition queue
// (MaxPendingTransitions) into the journal and a sink that keeps nothing.
func BenchmarkStopDrainsAFullQueue(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		c := newFakeClock()
		tr := NewTracker(c)
		j, err := NewJournal(c, "boot-a", "abc123")
		if err != nil {
			b.Fatal(err)
		}
		w := NewWatchdog(tr, j, c, time.Second)
		w.Attach(func(Record) {})
		w.at = c.Now()
		for k := 0; k < MaxPendingTransitions; k++ {
			w.record(Record{Event: EventStallDetected, Source: SourceDaemon, Stage: StageHandlerStarted,
				Class: ClassRead, Kind: Finite, Location: "cloud.link.read_handler", PhaseID: uint64(k + 1),
				Rule: RuleVersion})
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		b.StartTimer()
		w.Run(ctx)
	}
}
