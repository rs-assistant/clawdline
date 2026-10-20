package connectionhealth

import (
	"context"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultCadence is the watchdog's proposed scan interval (plan: 1 second).
// It is a parameter, not a measured budget.
const DefaultCadence = time.Second

// MaxPendingTransitions is the queue between a scan and the journal writer
// (capacity row cloud.health_pending_transitions). A scan never waits on it:
// when it is full the transition is dropped and counted, and the writer
// journals the count as transitions_lost.
const MaxPendingTransitions = 256

// State is the watchdog's reading of one phase.
type State string

const (
	StateProgressing State = "progressing"
	StateSlow        State = "slow"
	StateBlocked     State = "blocked"
	// StateIdle is a long-lived loop waiting for work: not stuck.
	StateIdle State = "idle"
	// StateUnverified is a finite phase that crossed its block deadline in
	// the first scan after the watchdog itself was not scheduled: the gap may
	// be the cause, so it is called blocked only if the next scan agrees.
	StateUnverified State = "unverified_after_gap"
)

// Finding is one phase as the watchdog read it.
type Finding struct {
	PhaseID     uint64 `json:"phase_id"`
	Source      Source `json:"source"`
	Stage       Stage  `json:"stage"`
	Class       Class  `json:"class"`
	Kind        Kind   `json:"kind"`
	Location    string `json:"location"`
	Correlation string `json:"correlation,omitempty"`
	State       State  `json:"state"`
	// AgeNS is time since last progress; ActiveNS since the phase began.
	AgeNS      int64  `json:"age_ns"`
	ActiveNS   int64  `json:"active_ns"`
	DeadlineNS int64  `json:"deadline_ns,omitempty"`
	Progress   uint64 `json:"progress"`
}

// Report is one watchdog scan.
type Report struct {
	Rule     string    `json:"rule"`
	Wall     time.Time `json:"wall"`
	MonoNS   int64     `json:"mono_ns"`
	Findings []Finding `json:"findings"`
	Overflow Overflow  `json:"overflow"`
	// TraceCoverage is partial once any Begin was refused: from then on no
	// exact per-operation claim can be made for the refused classes.
	TraceCoverage Quality `json:"trace_coverage"`
	// GapNS is set when this scan came later than twice the cadence.
	GapNS int64 `json:"gap_ns,omitempty"`
	// ClockBackwardsNS is set when the clock read earlier than the last scan.
	ClockBackwardsNS int64 `json:"clock_backwards_ns,omitempty"`
	// ScanSeq counts this watchdog's scans, so a reader can see it advance
	// even while the journal is stuck.
	ScanSeq uint64 `json:"scan_seq"`
	// Transitions is whether the journal holds every transition this
	// watchdog found: partial once any was dropped, not_collected when it has
	// no journal.
	Transitions Quality `json:"transitions"`
	// TransitionsLost counts transitions dropped because the queue to the
	// journal writer was full; TransitionsPending is that queue's depth.
	TransitionsLost    uint64 `json:"transitions_lost_total"`
	TransitionsPending int    `json:"transitions_pending"`
}

// queued is one transition, the scan instant it was observed at, and how
// many transitions had been dropped when it was queued: the writer journals
// those drops just before it, where they happened.
type queued struct {
	rec        Record
	at         Instant
	lostBefore uint64
}

type seen struct {
	state    State
	progress uint64
}

// Watchdog reads only the tracker's atomics and its own clock. It never calls
// a logger, a status snapshot, a business handler or the journal: a scan's
// transitions go into a bounded queue with a non-blocking send, and a
// separate writer appends them to the journal. A journal held by a slow
// reader, or a writer that is never scheduled, costs dropped and counted
// transitions, never a late scan. Scan is meant to be called from one
// goroutine (Run, or a test).
type Watchdog struct {
	tracker  *Tracker
	journal  *Journal
	clock    Clock
	cadence  time.Duration
	last     Instant
	at       Instant // this scan's reading, the time its transitions carry
	started  bool
	seen     map[uint64]seen // bounded by MaxActiveSlots: pruned every scan
	overflow uint64
	scans    uint64

	pending chan queued
	lost    atomic.Uint64
	wake    chan struct{} // capacity 1: a drop the writer has not journaled yet
	latest  atomic.Pointer[Report]

	writeMu  sync.Mutex // the writer side only; a scan never takes it
	lostSeen uint64     // lost already journaled, under writeMu
}

// NewWatchdog watches tracker and queues transitions for journal (which may
// be nil, in which case it only reports).
func NewWatchdog(tracker *Tracker, journal *Journal, clock Clock, cadence time.Duration) *Watchdog {
	if cadence <= 0 {
		cadence = DefaultCadence
	}
	w := &Watchdog{tracker: tracker, journal: journal, clock: clock, cadence: cadence, seen: map[uint64]seen{}}
	if journal != nil {
		w.pending = make(chan queued, MaxPendingTransitions)
		w.wake = make(chan struct{}, 1)
	}
	return w
}

// Run scans every cadence until ctx ends, publishing each report for Latest,
// and runs the one journal writer beside it. Nothing it calls is supplied by
// the caller, so nothing outside the package can hold a scan. It returns once
// both goroutines have stopped; the writer stops after the Append it may be
// waiting in, so a journal lock held forever holds Run's return, never a scan.
func (w *Watchdog) Run(ctx context.Context) {
	var wg sync.WaitGroup
	if w.journal != nil {
		wg.Add(1)
		go func() { defer wg.Done(); w.write(ctx) }()
	}
	defer wg.Wait()
	t := time.NewTicker(w.cadence)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.Scan()
		}
	}
}

// Latest is the newest report Scan produced, and false before the first.
func (w *Watchdog) Latest() (Report, bool) {
	if r := w.latest.Load(); r != nil {
		return *r, true
	}
	return Report{}, false
}

// write is the journal writer: the only goroutine that waits on the journal.
func (w *Watchdog) write(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case q := <-w.pending:
			w.writeMu.Lock()
			w.append(q)
			w.writeMu.Unlock()
		case <-w.wake:
			w.writeMu.Lock()
			w.drain()
			w.writeMu.Unlock()
		}
	}
}

// Flush appends every queued transition on the caller's goroutine, for a
// caller that scans without Run (tests, a one-shot export). It never blocks
// on the queue, only on the journal.
func (w *Watchdog) Flush() {
	if w.journal == nil {
		return
	}
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	w.drain()
}

// drain appends what is queued now, then the drops after the last of it.
// Under writeMu.
func (w *Watchdog) drain() {
	for {
		select {
		case q := <-w.pending:
			w.append(q)
		default:
			w.noteLost(w.lost.Load(), w.clock.Now())
			return
		}
	}
}

// append journals the drops that came before q, then q. Under writeMu.
func (w *Watchdog) append(q queued) {
	w.noteLost(q.lostBefore, q.at)
	_ = w.journal.appendObserved(q.rec, q.at)
}

// noteLost journals the drops up to upTo not yet journaled. Under writeMu.
func (w *Watchdog) noteLost(upTo uint64, at Instant) {
	if upTo <= w.lostSeen {
		return
	}
	if w.journal.appendLost(upTo-w.lostSeen, at) != nil {
		return
	}
	w.lostSeen = upTo
	w.restate(at)
}

// restate journals, after a loss, every phase the latest published report
// holds as slow, blocked or unverified (EventStateAfterLoss). It reads only
// the atomic Latest, never the scan's own state, so the writer still shares
// nothing with a scan; the report has at most MaxActiveSlots findings. Under
// writeMu.
func (w *Watchdog) restate(at Instant) {
	r := w.latest.Load()
	if r == nil {
		return
	}
	for _, f := range r.Findings {
		switch f.State {
		case StateSlow, StateBlocked, StateUnverified:
		default:
			continue
		}
		_ = w.journal.appendState(Record{
			Source: f.Source, Stage: f.Stage, Class: f.Class, Kind: f.Kind,
			Location: f.Location, PhaseID: f.PhaseID, Correlation: f.Correlation,
			AgeNS: f.AgeNS, DeadlineNS: f.DeadlineNS, Count: f.Progress,
			Rule: RuleVersion, State: f.State,
		}, at)
	}
}

// Scan reads every active phase once.
func (w *Watchdog) Scan() Report {
	now := w.clock.Now()
	w.at = now
	r := Report{Rule: RuleVersion, Wall: now.Wall, MonoNS: int64(now.Mono), TraceCoverage: QualityComplete}
	gap := false
	if w.started {
		switch d := now.Mono - w.last.Mono; {
		case d < 0:
			// A monotonic clock that went backwards is a fault in the
			// clock, not negative elapsed time: age is measured from the
			// last good reading and the anomaly is recorded.
			r.ClockBackwardsNS = int64(-d)
			now.Mono = w.last.Mono
			w.at = now
			w.record(Record{Event: EventClockAnomaly, AgeNS: int64(-d), Rule: RuleVersion})
			r.MonoNS = int64(now.Mono)
		case d > 2*w.cadence:
			gap = true
			r.GapNS = int64(d)
			w.record(Record{Event: EventSchedulingGap, AgeNS: int64(d), Rule: RuleVersion})
		}
	}
	w.started, w.last = true, now

	phases, overflow := w.tracker.Snapshot()
	r.Overflow = overflow
	if overflow.Total > 0 {
		r.TraceCoverage = QualityPartial
	}
	if overflow.Total > w.overflow {
		w.record(Record{Event: EventOverflow, Count: overflow.Total - w.overflow, Rule: RuleVersion})
		w.overflow = overflow.Total
	}

	live := make(map[uint64]bool, len(phases))
	for _, p := range phases {
		live[p.ID] = true
		f := Finding{
			PhaseID: p.ID, Source: p.Spec.Source, Stage: p.Spec.Stage, Class: p.Spec.Class,
			Kind: p.Spec.Kind, Location: p.Spec.Location, Correlation: p.Spec.Correlation,
			AgeNS: nonNegative(now.Mono - p.LastProgress.Mono), ActiveNS: nonNegative(now.Mono - p.Started.Mono),
			Progress: p.Progress,
		}
		prev, had := w.seen[p.ID]
		f.State = classify(p, time.Duration(f.AgeNS), gap)
		switch f.State {
		case StateSlow:
			f.DeadlineNS = int64(p.Spec.Warn)
		case StateBlocked, StateUnverified:
			f.DeadlineNS = int64(p.Spec.Block)
		}
		w.transition(p, f, prev, had)
		w.seen[p.ID] = seen{state: f.State, progress: p.Progress}
		r.Findings = append(r.Findings, f)
	}
	for id, s := range w.seen {
		if !live[id] {
			if s.state == StateBlocked {
				// The slot was released, but the watchdog cannot see how
				// the operation ended: release is not success.
				w.record(Record{Event: EventReleasedStalled, PhaseID: id, Rule: RuleVersion})
			}
			delete(w.seen, id)
		}
	}
	sort.SliceStable(r.Findings, func(i, j int) bool { return r.Findings[i].AgeNS > r.Findings[j].AgeNS })
	if r.Findings == nil {
		r.Findings = []Finding{}
	}
	w.scans++
	r.ScanSeq = w.scans
	r.Transitions = QualityNotCollected
	if w.journal != nil {
		r.TransitionsLost = w.lost.Load()
		r.TransitionsPending = len(w.pending)
		r.Transitions = QualityComplete
		if r.TransitionsLost > 0 {
			r.Transitions = QualityPartial
		}
	}
	published := r
	published.Findings = append([]Finding(nil), r.Findings...)
	w.latest.Store(&published)
	return r
}

func classify(p Phase, age time.Duration, gap bool) State {
	if p.Spec.Kind == LongLived {
		return StateIdle
	}
	switch {
	case age >= p.Spec.Block:
		if gap {
			return StateUnverified
		}
		return StateBlocked
	case age >= p.Spec.Warn:
		return StateSlow
	}
	return StateProgressing
}

func (w *Watchdog) transition(p Phase, f Finding, prev seen, had bool) {
	rec := Record{
		Source: p.Spec.Source, Stage: p.Spec.Stage, Class: p.Spec.Class, Kind: p.Spec.Kind,
		Location: p.Spec.Location, PhaseID: p.ID, Correlation: p.Spec.Correlation,
		AgeNS: f.AgeNS, DeadlineNS: f.DeadlineNS, Count: p.Progress, Rule: RuleVersion,
	}
	switch {
	case f.State == StateBlocked && prev.state != StateBlocked:
		rec.Event = EventStallDetected
	case f.State == StateSlow && (!had || prev.state == StateProgressing):
		rec.Event = EventSlowDetected
	case f.State == StateProgressing && had && (prev.state == StateBlocked || prev.state == StateSlow || prev.state == StateUnverified) && p.Progress > prev.progress:
		// Recovery needs new progress, not merely the stall going quiet.
		rec.Event = EventRecovered
	default:
		return
	}
	w.record(rec)
}

// record queues r for the writer without waiting: a full queue drops it and
// counts the drop, which the writer journals as transitions_lost.
func (w *Watchdog) record(r Record) {
	if w.journal == nil {
		return
	}
	select {
	case w.pending <- queued{rec: r, at: w.at, lostBefore: w.lost.Load()}:
	default:
		w.lost.Add(1)
		select {
		case w.wake <- struct{}{}:
		default:
		}
	}
}

func nonNegative(d time.Duration) int64 {
	if d < 0 {
		return 0
	}
	return int64(d)
}
