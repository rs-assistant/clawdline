package connectionhealth

import (
	"context"
	"sort"
	"time"
)

// DefaultCadence is the watchdog's proposed scan interval (plan: 1 second).
// It is a parameter, not a measured budget.
const DefaultCadence = time.Second

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
}

type seen struct {
	state    State
	progress uint64
}

// Watchdog reads only the tracker's atomics and its own clock. It never calls
// a logger, a status snapshot or a business handler; its only side effect is
// appending transitions to the journal, whose lock no business code holds.
// Scan is meant to be called from one goroutine (Run, or a test).
type Watchdog struct {
	tracker  *Tracker
	journal  *Journal
	clock    Clock
	cadence  time.Duration
	last     Instant
	started  bool
	seen     map[uint64]seen // bounded by MaxActiveSlots: pruned every scan
	overflow uint64
}

// NewWatchdog watches tracker and writes transitions to journal (which may be
// nil, in which case it only reports).
func NewWatchdog(tracker *Tracker, journal *Journal, clock Clock, cadence time.Duration) *Watchdog {
	if cadence <= 0 {
		cadence = DefaultCadence
	}
	return &Watchdog{tracker: tracker, journal: journal, clock: clock, cadence: cadence, seen: map[uint64]seen{}}
}

// Run scans every cadence until ctx ends; the latest report goes to sink,
// which must not block (it is called on the watchdog's own goroutine).
func (w *Watchdog) Run(ctx context.Context, sink func(Report)) {
	t := time.NewTicker(w.cadence)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r := w.Scan()
			if sink != nil {
				sink(r)
			}
		}
	}
}

// Scan reads every active phase once.
func (w *Watchdog) Scan() Report {
	now := w.clock.Now()
	r := Report{Rule: RuleVersion, Wall: now.Wall, MonoNS: int64(now.Mono), TraceCoverage: QualityComplete}
	gap := false
	if w.started {
		switch d := now.Mono - w.last.Mono; {
		case d < 0:
			// A monotonic clock that went backwards is a fault in the
			// clock, not negative elapsed time: age is measured from the
			// last good reading and the anomaly is recorded.
			r.ClockBackwardsNS = int64(-d)
			w.record(Record{Event: EventClockAnomaly, AgeNS: int64(-d), Rule: RuleVersion})
			now.Mono = w.last.Mono
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

func (w *Watchdog) record(r Record) {
	if w.journal != nil {
		// A refused record is counted by the journal; the watchdog does not
		// wait or retry.
		_ = w.journal.Append(r)
	}
}

func nonNegative(d time.Duration) int64 {
	if d < 0 {
		return 0
	}
	return int64(d)
}
