package connectionhealth

import (
	"context"
	"math"
	"runtime"
	"testing"
	"time"
)

// A cursor past the newest sequence this boot assigned is invalid_cursor,
// including the largest uint64, whose +1 used to wrap the gap arithmetic to
// zero and answer an empty "complete" page. The newest sequence itself is a
// valid, caught-up cursor.
func TestAFutureCursorIsInvalidNotAnEmptyPage(t *testing.T) {
	_, _, j, _ := fixture(t)
	for _, after := range []uint64{2, 1 << 40, math.MaxUint64} {
		if _, err := j.Read(Query{AfterSeq: after}); CodeOf(err) != CodeInvalidCursor {
			t.Errorf("after_seq %d: %v", after, err)
		}
	}
	p, err := j.Read(Query{AfterSeq: 1})
	if err != nil || len(p.Records) != 0 || p.Coverage.Quality != QualityComplete || p.Coverage.NextAfterSeq != 1 {
		t.Fatalf("a caught-up cursor: %+v %v", p, err)
	}
}

// record_too_large is reachable: with the per-record bound lowered, a valid
// record over it is refused, counted and given no sequence number.
func TestARecordOverTheBoundIsRefusedAndCounted(t *testing.T) {
	c := newFakeClock()
	j, err := newJournalBounded(c, "boot-a", "abc123", MaxJournalRecords, MaxJournalBytes, 300)
	if err != nil {
		t.Fatal(err)
	}
	small := Record{Event: EventOverflow, Count: 1, Rule: RuleVersion}
	if err := j.Append(small); err != nil {
		t.Fatal(err)
	}
	big := Record{Event: EventStallDetected, Source: SourceDaemon, Stage: StageHandlerStarted, Class: ClassRead,
		Kind: Finite, Location: "cloud.link.read_handler.with_a_rather_long_location_code_for_this_test",
		Correlation: "op-0123456789abcdefghijklmnopqrstuvwxyz0123456789abcdefghij",
		PhaseID: 1, AgeNS: 1, DeadlineNS: 1, Count: 1, Rule: RuleVersion}
	if err := j.Append(big); CodeOf(err) != CodeRecordTooLarge {
		t.Fatalf("an oversized record: %v", err)
	}
	if err := j.Append(small); err != nil {
		t.Fatal(err)
	}
	p, _ := j.Read(Query{})
	if p.Coverage.Refused != 1 || len(p.Records) != 3 || p.Records[2].Seq != 3 {
		t.Fatalf("the refusal took a sequence or was not counted: %+v", p)
	}
}

// When the transition that said a handler stalled was dropped at the queue's
// bound, the journal's loss record is followed by the stall as it stands, so
// an exported journal never reads "loss, then quiet" while it is still stuck.
func TestALossIsFollowedByTheStallsStillStanding(t *testing.T) {
	c, tr, j, w := fixture(t)
	w.Scan()
	h, _ := tr.Begin(handlerSpec())
	// Fill the queue without a writer, so the slow and stall transitions of
	// the handler are among the dropped.
	for i := 0; i < MaxPendingTransitions; i++ {
		c.advance(3 * time.Second)
		w.Scan()
	}
	// Two scans on time: a gap makes a crossing unverified, the next one
	// on time calls it blocked, and that stall_detected is dropped too.
	c.advance(time.Second)
	w.Scan()
	c.advance(time.Second)
	w.Scan()
	r, _ := w.Latest()
	if r.TransitionsLost == 0 || finding(t, r, 1).State != StateBlocked {
		t.Fatalf("the fixture did not lose the stall: lost %d, %+v", r.TransitionsLost, r.Findings)
	}
	w.Flush()
	p, err := j.Read(Query{AfterSeq: 1 + MaxPendingTransitions})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Records) != 2 || p.Records[0].Event != EventTransitionsLost ||
		p.Records[1].Event != EventStateAfterLoss || p.Records[1].State != StateBlocked ||
		p.Records[1].PhaseID != 1 || p.Records[1].Location != "cloud.link.read_handler" ||
		p.Coverage.Quality != QualityPartial {
		t.Fatalf("after the loss: %+v", p.Records)
	}
	for _, rec := range p.Records {
		if rec.Event == EventStallDetected {
			t.Fatal("a re-statement was written as a new stall_detected")
		}
	}
	h.End(true)
}

// No caller may write a re-statement or give a state to another event.
func TestStateIsTheJournalsOwn(t *testing.T) {
	_, _, j, _ := fixture(t)
	if err := j.Append(Record{Event: EventStateAfterLoss, State: StateBlocked}); CodeOf(err) != CodeInvalidField {
		t.Fatalf("a caller wrote state_after_loss: %v", err)
	}
	if err := j.Append(Record{Event: EventStallDetected, State: StateBlocked}); CodeOf(err) != CodeInvalidField {
		t.Fatalf("a caller stamped a state: %v", err)
	}
}

// A query holding the journal's lock — a slow reader, a stuck export —
// holds neither the business path nor the scan: Start, Step and End finish,
// and the watchdog still sees the phase.
func TestAHeldQueryNeverHoldsTheBusinessPath(t *testing.T) {
	c, tr, j, w := fixture(t)
	j.mu.Lock()
	done := make(chan Report)
	go func() {
		for i := 0; i < 1000; i++ {
			h := tr.Start(handlerSpec())
			_ = h.Step(StageHandlerCompleted, "cloud.link.read_handler")
			h.End(true)
		}
		stuck := tr.Start(handlerSpec())
		c.advance(16 * time.Second)
		w.Scan()
		r := w.Scan()
		_ = stuck
		done <- r
	}()
	select {
	case r := <-done:
		j.mu.Unlock()
		if len(r.Findings) != 1 || r.Findings[0].State != StateBlocked {
			t.Fatalf("the scan beside a held query: %+v", r.Findings)
		}
		completed, _ := tr.Counts()
		if completed[ClassRead] != 1000 {
			t.Fatalf("completions: %d", completed[ClassRead])
		}
	case <-time.After(5 * time.Second):
		j.mu.Unlock()
		t.Fatal("the business path or the scan waited on a held journal")
	}
}

// Step moves the stage and the location together; a bad location is refused
// and leaves the phase where it was. A nil tracker and the zero Handle are
// no-ops everywhere, which is what monitoring off is.
func TestStepNamesWhereThePhaseWaitsAndNilIsOff(t *testing.T) {
	_, tr, _, _ := fixture(t)
	h := tr.Start(handlerSpec())
	if err := h.Step(StageLaneQueued, "cloud.transport.inbound_callback"); err != nil {
		t.Fatal(err)
	}
	if err := h.Step(StageHandlerStarted, "/etc/passwd"); CodeOf(err) != CodeInvalidField {
		t.Fatalf("a path as a location: %v", err)
	}
	phases, _ := tr.Snapshot()
	if len(phases) != 1 || phases[0].Spec.Location != "cloud.transport.inbound_callback" || phases[0].Spec.Stage != StageLaneQueued {
		t.Fatalf("after Step: %+v", phases)
	}
	h.End(true)

	var off *Tracker
	z := off.Start(handlerSpec())
	if z.Recorded() || off.NewCorrelation() != "" || z.Step(StageIdle, "x") != nil || z.Progress(StageIdle) != nil {
		t.Fatal("a nil tracker recorded something")
	}
	z.End(true)
	if bad := tr.Start(Spec{}); bad.Recorded() || tr.InvalidSpecs() != 1 {
		t.Fatalf("an invalid spec: recorded %v, counted %d", bad.Recorded(), tr.InvalidSpecs())
	}
	var rt *Runtime
	if _, err := rt.Status(); CodeOf(err) != CodeUnsupported {
		t.Fatalf("a nil runtime's status: %v", err)
	}
	if rt.Probe() != nil || rt.Running() {
		t.Fatal("a nil runtime is running")
	}
}

// Correlations are opaque, unique and carry nothing but a counter.
func TestCorrelationsAreOpaqueCounters(t *testing.T) {
	tr := NewTracker(newFakeClock())
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		c := tr.NewCorrelation()
		if !ValidID(c) || seen[c] {
			t.Fatalf("correlation %q", c)
		}
		seen[c] = true
	}
}

// A window by wall time returns only the records inside it and continues by
// sequence; until before since is invalid.
func TestEventsByWindow(t *testing.T) {
	c := newFakeClock()
	rt, err := newRuntime(c, "boot-w", "abc", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	start := c.Now().Wall
	for i := 0; i < 5; i++ {
		c.advance(time.Minute)
		if err := rt.Journal.Append(Record{Event: EventOverflow, Count: uint64(i + 1), Rule: RuleVersion}); err != nil {
			t.Fatal(err)
		}
	}
	p, err := rt.Events(Query{Limit: 2}, start.Add(90*time.Second), start.Add(4*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Records) != 2 || p.Records[0].Count != 2 || !p.Coverage.Truncated {
		t.Fatalf("first window page: %+v", p)
	}
	p, err = rt.Events(Query{AfterSeq: p.Coverage.NextAfterSeq}, start.Add(90*time.Second), start.Add(4*time.Minute))
	if err != nil || len(p.Records) != 1 || p.Records[0].Count != 4 || p.Coverage.Truncated {
		t.Fatalf("second window page: %+v %v", p, err)
	}
	if _, err := rt.Events(Query{}, start.Add(time.Hour), start); CodeOf(err) != CodeInvalidField {
		t.Fatalf("until before since: %v", err)
	}
}

// The runtime's Run ends with its context and leaves nothing running; its
// status is not_collected before the first scan and names the boot and
// build.
func TestRuntimeLifetimeAndStatus(t *testing.T) {
	before := runtime.NumGoroutine()
	rt, err := NewRuntime("v1.4.2+dirty/x", 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if rt.Build != "v1_4_2_dirty_x" || !ValidID(rt.BootID) {
		t.Fatalf("build %q boot %q", rt.Build, rt.BootID)
	}
	st, err := rt.Status()
	if err != nil || st.Quality != QualityNotCollected || st.Report != nil || st.ReportAgeNS != nil {
		t.Fatalf("status before a scan: %+v %v", st, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { rt.Run(ctx); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if r, ok := rt.Watchdog.Latest(); ok && r.ScanSeq > 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the runtime never scanned")
		}
		time.Sleep(5 * time.Millisecond)
	}
	st, _ = rt.Status()
	if !st.Running || st.Quality != QualityComplete || st.Report == nil {
		t.Fatalf("status while running: %+v", st)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}
	time.Sleep(20 * time.Millisecond)
	if after := runtime.NumGoroutine(); after > before {
		t.Fatalf("goroutines: %d before, %d after", before, after)
	}
	if SanitizeBuild("") != "unknown" {
		t.Fatal("an empty build")
	}
}
