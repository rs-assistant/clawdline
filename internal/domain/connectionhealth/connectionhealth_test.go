package connectionhealth

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeClock struct {
	mu   sync.Mutex
	mono time.Duration
	wall time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{wall: time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() Instant {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Instant{Mono: c.mono, Wall: c.wall}
}

// advance moves both clocks; jumpWall moves only the wall clock; rewind
// moves the monotonic one backwards, which a real one never should.
func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.mono += d
	c.wall = c.wall.Add(d)
	c.mu.Unlock()
}
func (c *fakeClock) jumpWall(d time.Duration) { c.mu.Lock(); c.wall = c.wall.Add(d); c.mu.Unlock() }
func (c *fakeClock) rewind(d time.Duration)   { c.mu.Lock(); c.mono -= d; c.mu.Unlock() }

func handlerSpec() Spec {
	return Spec{
		Source: SourceDaemon, Stage: StageHandlerStarted, Class: ClassRead, Kind: Finite,
		Location: "cloud.link.read_handler", Correlation: "trace-1",
		Warn: 5 * time.Second, Block: 15 * time.Second,
	}
}

func fixture(t *testing.T) (*fakeClock, *Tracker, *Journal, *Watchdog) {
	t.Helper()
	c := newFakeClock()
	tr := NewTracker(c)
	j, err := NewJournal(c, "boot-a", "abc123")
	if err != nil {
		t.Fatal(err)
	}
	return c, tr, j, NewWatchdog(tr, j, c, time.Second)
}

func events(t *testing.T, j *Journal) []Event {
	t.Helper()
	p, err := j.Read(Query{})
	if err != nil {
		t.Fatal(err)
	}
	var out []Event
	for _, r := range p.Records {
		out = append(out, r.Event)
	}
	return out
}

func finding(t *testing.T, r Report, id uint64) Finding {
	t.Helper()
	for _, f := range r.Findings {
		if f.PhaseID == id {
			return f
		}
	}
	t.Fatalf("phase %d not in report %+v", id, r.Findings)
	return Finding{}
}

// A handler that never returns is blocked once its deadline passes with no
// progress, while the socket-level loop beside it stays idle, not stuck.
func TestAStuckHandlerIsBlockedAndAnIdleReaderIsNot(t *testing.T) {
	c, tr, j, w := fixture(t)
	reader, err := tr.Begin(Spec{Source: SourceDaemon, Stage: StageIdle, Class: ClassTransport, Kind: LongLived, Location: "cloud.transport.read_loop", Critical: true})
	if err != nil {
		t.Fatal(err)
	}
	h, err := tr.Begin(handlerSpec())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 16; i++ {
		c.advance(time.Second)
		w.Scan()
	}
	r := w.Scan()
	got := finding(t, r, h.id)
	if got.State != StateBlocked || got.AgeNS != int64(16*time.Second) || got.DeadlineNS != int64(15*time.Second) {
		t.Errorf("stuck handler: %+v", got)
	}
	if got.Location != "cloud.link.read_handler" || got.Correlation != "trace-1" {
		t.Errorf("finding lost its location or correlation: %+v", got)
	}
	if f := finding(t, r, reader.id); f.State != StateIdle {
		t.Errorf("an idle long-lived loop is not stuck: %+v", f)
	}
	ev := events(t, j)
	if strings.Join(eventStrings(ev), ",") != "boot,slow_detected,stall_detected" {
		t.Errorf("journal events %v", ev)
	}
}

func eventStrings(ev []Event) []string {
	out := make([]string, len(ev))
	for i, e := range ev {
		out[i] = string(e)
	}
	return out
}

// Recovery needs new progress: a blocked phase that progresses is recovered;
// one that is merely released is recorded as released, not as recovered.
func TestRecoveryNeedsProgressNotSilence(t *testing.T) {
	c, tr, j, w := fixture(t)
	h, _ := tr.Begin(handlerSpec())
	other, _ := tr.Begin(handlerSpec())
	for i := 0; i < 16; i++ {
		c.advance(time.Second)
		w.Scan()
	}
	if err := h.Progress(StageHandlerCompleted); err != nil {
		t.Fatal(err)
	}
	r := w.Scan()
	if f := finding(t, r, h.id); f.State != StateProgressing || f.Stage != StageHandlerCompleted || f.Progress != 1 {
		t.Errorf("progressed phase: %+v", f)
	}
	other.End(true)
	w.Scan()
	ev := strings.Join(eventStrings(events(t, j)), ",")
	if ev != "boot,slow_detected,slow_detected,stall_detected,stall_detected,recovered,released_after_stall" {
		t.Errorf("journal events %s", ev)
	}
	h.End(true)
	h.End(true) // a second End is harmless
	if completed, _ := tr.Counts(); completed[ClassRead] != 2 {
		t.Errorf("completions %v", completed)
	}
}

// Under capacity pressure business Begins are refused and counted; reserved
// slots still serve the loops that prove progress; coverage turns partial.
func TestFullTableRefusesBusinessKeepsReservedAndSaysPartial(t *testing.T) {
	_, tr, j, w := fixture(t)
	for i := 0; i < MaxActiveSlots-MaxReservedSlots; i++ {
		if _, err := tr.Begin(handlerSpec()); err != nil {
			t.Fatalf("slot %d: %v", i, err)
		}
	}
	h, err := tr.Begin(handlerSpec())
	if CodeOf(err) != CodeSlotsExhausted || h.Recorded() {
		t.Fatalf("over the bound: %v %v", h, err)
	}
	if err := h.Progress(StageHandlerCompleted); err != nil {
		t.Errorf("a zero handle must accept calls: %v", err)
	}
	h.End(false)
	crit := handlerSpec()
	crit.Critical, crit.Class = true, ClassTransport
	if _, err := tr.Begin(crit); err != nil {
		t.Fatalf("a reserved slot was taken by business work: %v", err)
	}
	r := w.Scan()
	if r.TraceCoverage != QualityPartial || r.Overflow.Total != 1 || r.Overflow.ByClass[ClassRead] != 1 || r.Overflow.FirstMono == nil {
		t.Errorf("overflow not reported: %+v", r)
	}
	if len(r.Findings) != MaxActiveSlots-MaxReservedSlots+1 {
		t.Errorf("findings %d", len(r.Findings))
	}
	if ev := events(t, j); ev[len(ev)-1] != EventOverflow {
		t.Errorf("overflow not journaled: %v", ev)
	}
}

// The tracker under contention: every slot is handed out once, and a
// concurrent watchdog never sees a torn phase.
func TestTheTableHoldsUnderContention(t *testing.T) {
	c, tr, _, w := fixture(t)
	var wg sync.WaitGroup
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				for _, f := range w.Scan().Findings {
					if f.Location == "" || f.Source == "" {
						t.Errorf("torn phase %+v", f)
					}
				}
			}
		}
	}()
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				h, err := tr.Begin(handlerSpec())
				if err != nil {
					continue
				}
				_ = h.Progress(StageHandlerCompleted)
				c.advance(time.Microsecond)
				h.End(true)
			}
		}()
	}
	wg.Wait()
	close(stop)
	if phases, _ := tr.Snapshot(); len(phases) != 0 {
		t.Errorf("slots leaked: %d", len(phases))
	}
}

// A wall-clock jump moves no age; a monotonic clock that goes backwards
// yields no negative age and is recorded; a scheduling gap defers the stall
// verdict by one scan instead of blaming the operation for the pause.
func TestClockJumpsAndSchedulingGaps(t *testing.T) {
	c, tr, j, w := fixture(t)
	h, _ := tr.Begin(handlerSpec())
	w.Scan()
	c.jumpWall(-3 * time.Hour)
	c.advance(time.Second)
	if f := finding(t, w.Scan(), h.id); f.AgeNS != int64(time.Second) || f.State != StateProgressing {
		t.Errorf("wall jump moved the age: %+v", f)
	}
	c.rewind(500 * time.Millisecond)
	r := w.Scan()
	if r.ClockBackwardsNS != int64(500*time.Millisecond) {
		t.Errorf("backwards clock not reported: %+v", r)
	}
	for _, f := range r.Findings {
		if f.AgeNS < 0 || f.ActiveNS < 0 {
			t.Errorf("negative age %+v", f)
		}
	}
	c.advance(20 * time.Second)
	r = w.Scan()
	if r.GapNS == 0 || finding(t, r, h.id).State != StateUnverified {
		t.Errorf("gap scan: %+v", r)
	}
	c.advance(time.Second)
	if f := finding(t, w.Scan(), h.id); f.State != StateBlocked {
		t.Errorf("after the gap the stall stands: %+v", f)
	}
	ev := strings.Join(eventStrings(events(t, j)), ",")
	if ev != "boot,clock_anomaly,scheduling_gap,stall_detected" {
		t.Errorf("journal events %s", ev)
	}
}

// The ring evicts by rows and by bytes, counts what it evicted, and a reader
// whose cursor fell behind is told the exact gap, not handed a complete page.
func TestTheJournalEvictsCountsAndNamesTheGap(t *testing.T) {
	c := newFakeClock()
	j, err := newJournal(c, "boot-a", "abc123", 4, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		if err := j.Append(Record{Event: EventSchedulingGap, AgeNS: int64(i)}); err != nil {
			t.Fatal(err)
		}
	}
	p, err := j.Read(Query{BootID: "boot-a"})
	if err != nil {
		t.Fatal(err)
	}
	c2 := p.Coverage
	if len(p.Records) != 4 || c2.FirstRetainedSeq != 4 || c2.LastSeq != 7 || c2.Evicted != 3 || c2.Quality != QualityPartial || c2.GapFromSeq != 1 || c2.GapToSeq != 3 {
		t.Errorf("page %+v", c2)
	}
	if c2.Durability != DurabilityMemory {
		t.Errorf("durability %q", c2.Durability)
	}
	p, _ = j.Read(Query{AfterSeq: 5, Limit: 1})
	if p.Coverage.Quality != QualityComplete || !p.Coverage.Truncated || p.Coverage.NextAfterSeq != 6 || len(p.Records) != 1 {
		t.Errorf("continuation %+v", p.Coverage)
	}

	small, _ := newJournal(c, "boot-a", "abc123", 100, 600)
	for i := 0; i < 5; i++ {
		_ = small.Append(Record{Event: EventSchedulingGap})
	}
	if p, _ := small.Read(Query{}); p.Coverage.Evicted == 0 || len(p.Records) >= 6 {
		t.Errorf("byte bound not enforced: %+v", p.Coverage)
	}
}

// Absent or unsupported evidence is a typed answer, never an empty success.
func TestAbsentAndUnsupportedEvidenceIsNotSuccess(t *testing.T) {
	_, _, j, _ := fixture(t)
	if _, err := j.Read(Query{BootID: "boot-b"}); CodeOf(err) != CodeCursorOtherBoot {
		t.Errorf("another boot's cursor: %v", err)
	}
	if _, err := j.Read(Query{Limit: MaxQueryRecords + 1}); CodeOf(err) != CodeQueryBoundExceeded {
		t.Errorf("over-wide query: %v", err)
	}
	if _, err := Export(nil, j, Query{}); CodeOf(err) != CodeUnsupported {
		t.Errorf("export without a watchdog: %v", err)
	}
	cases := []struct {
		name string
		in   SegmentEvidence
		want Verdict
	}{
		{"never reported", SegmentEvidence{}, Verdict{OutcomeUnknown, QualityNotCollected}},
		{"socket open, source silent", SegmentEvidence{Freshness: FreshnessNoReceipt, SocketOpen: true}, Verdict{OutcomeUnknown, QualityNotCollected}},
		{"old build", SegmentEvidence{Freshness: FreshnessUnsupported}, Verdict{OutcomeUnknown, QualityUnsupported}},
		{"stale", SegmentEvidence{Freshness: FreshnessStale, Completed: 9}, Verdict{OutcomeUnknown, QualityStale}},
		{"forbidden", SegmentEvidence{Freshness: FreshnessForbidden}, Verdict{OutcomeUnknown, QualityForbidden}},
		{"socket open, nothing completed", SegmentEvidence{Freshness: FreshnessFresh, SocketOpen: true}, Verdict{OutcomeNoRequests, QualityNoRequests}},
		{"socket open, handler blocked", SegmentEvidence{Freshness: FreshnessFresh, SocketOpen: true, Completed: 50, Blocked: true}, Verdict{OutcomeBlocked, QualityComplete}},
		{"no traffic with gaps", SegmentEvidence{Freshness: FreshnessFresh, Gaps: true}, Verdict{OutcomeUnknown, QualityPartial}},
		{"errors", SegmentEvidence{Freshness: FreshnessFresh, Completed: 99, Failed: 1}, Verdict{OutcomeError, QualityComplete}},
		{"slow", SegmentEvidence{Freshness: FreshnessFresh, Completed: 30, SlowP95: true}, Verdict{OutcomeSlow, QualityComplete}},
		{"healthy", SegmentEvidence{Freshness: FreshnessFresh, Completed: 30}, Verdict{OutcomeHealthy, QualityComplete}},
		{"offline, machine silent", SegmentEvidence{Freshness: FreshnessNoReceipt, PresenceExpired: true}, Verdict{OutcomeOffline, QualityPartial}},
	}
	for _, c := range cases {
		if got := Classify(c.in); got != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestStageProgressNamesTheHoleAndTheScopeLimit(t *testing.T) {
	path := []Stage{StageLaneQueued, StageHandlerStarted, StageHandlerCompleted, StageSocketWriteEnded, StageEndpointReceived}
	got := ProgressAlong(path, map[Stage]bool{StageLaneQueued: true, StageHandlerStarted: true, StageEndpointReceived: true}, nil, nil)
	if got.LastProven != StageHandlerStarted || got.NextUnobserved != StageHandlerCompleted || got.Active != nil || len(got.LaterReceipts) != 1 {
		t.Errorf("progress %+v", got)
	}
	active := &Phase{Spec: handlerSpec()}
	got = ProgressAlong(path, map[Stage]bool{StageLaneQueued: true, StageHandlerStarted: true, StageHandlerCompleted: true}, active, func(s Stage) bool { return s != StageSocketWriteEnded })
	if got.Active == nil || *got.Active != StageHandlerStarted || got.VisibilityLimitAt != StageSocketWriteEnded {
		t.Errorf("progress %+v", got)
	}
	if got := ProgressAlong(path, nil, nil, nil); got.LastProven != "" || got.NextUnobserved != StageLaneQueued {
		t.Errorf("no receipts %+v", got)
	}
}

// Nothing that looks like a path, URL or credential gets into a phase or a
// record, and the export carries no such field.
func TestExportsAreContentFree(t *testing.T) {
	_, tr, j, w := fixture(t)
	for field, mutate := range map[string]func(*Spec){
		"correlation": func(s *Spec) { s.Correlation = "/Users/someone/secret" },
		"location":    func(s *Spec) { s.Location = "internal/adapters/cloud/transport.go:445" },
		"deadline":    func(s *Spec) { s.Block = s.Warn },
		"kind":        func(s *Spec) { s.Kind = "" },
	} {
		s := handlerSpec()
		mutate(&s)
		if _, err := tr.Begin(s); CodeOf(err) != CodeInvalidField || !strings.Contains(err.Error(), field) || strings.Contains(err.Error(), "secret") {
			t.Errorf("%s: %v", field, err)
		}
	}
	if err := j.Append(Record{Event: EventOverflow, Correlation: "Bearer abc"}); CodeOf(err) != CodeInvalidField {
		t.Errorf("token-shaped correlation accepted: %v", err)
	}
	if _, err := NewJournal(newFakeClock(), "boot a", "x"); CodeOf(err) != CodeInvalidField {
		t.Errorf("boot id with a space: %v", err)
	}
	h, _ := tr.Begin(handlerSpec())
	_ = h
	r := w.Scan()
	ev, err := Export(&r, j, Query{})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(ev)
	for _, bad := range []string{"/Users", "Bearer", ".go:"} {
		if strings.Contains(string(b), bad) {
			t.Errorf("export contains %q", bad)
		}
	}
	unwired := 0
	for _, x := range ev.Wiring {
		if x.Status == WiringUnimplemented {
			unwired++
		}
	}
	if ev.Schema != EvidenceSchema || unwired == 0 {
		t.Errorf("export does not say what is unwired: %+v", ev.Wiring)
	}
	if p, _ := j.Read(Query{}); p.Coverage.Refused != 1 {
		t.Errorf("refused record not counted: %+v", p.Coverage)
	}
}
