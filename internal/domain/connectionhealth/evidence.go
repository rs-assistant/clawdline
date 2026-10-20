package connectionhealth

import "time"

// Freshness is the state of the source a segment's numbers came from.
type Freshness string

const (
	FreshnessFresh       Freshness = "fresh"
	FreshnessStale       Freshness = "stale"
	FreshnessNoReceipt   Freshness = "no_receipt"
	FreshnessUnsupported Freshness = "unsupported"
	FreshnessForbidden   Freshness = "forbidden"
)

// SegmentEvidence is what is known about one segment in one window.
type SegmentEvidence struct {
	Freshness Freshness
	// PresenceExpired is the relay's authenticated presence contract having
	// lapsed; it is a Cloud-side fact and stands even when the machine's own
	// source is silent.
	PresenceExpired bool
	// SocketOpen is recorded but proves only the transport; it never makes a
	// segment healthy.
	SocketOpen bool
	Completed  uint64
	Failed     uint64
	Blocked    bool
	// SlowP95 says the window's p95 is over target with enough samples; the
	// caller decides that, since percentiles live with the histogram.
	SlowP95 bool
	// Gaps says the window holds intervals with no evidence.
	Gaps bool
}

// Verdict is the two answers the page shows side by side.
type Verdict struct {
	Outcome Outcome `json:"outcome"`
	Quality Quality `json:"quality"`
}

// ErrorRateThreshold is the proposed share of failed logical operations that
// makes a segment error (design §5): a proposal, not a measured SLO.
const ErrorRateThreshold = 0.01

// Classify is design §5's truth table. Worst wins in the table's order, and
// nothing the evidence does not establish is called healthy.
func Classify(e SegmentEvidence) Verdict {
	if e.PresenceExpired {
		q := QualityComplete
		if e.Freshness != FreshnessFresh {
			q = QualityPartial
		}
		return Verdict{OutcomeOffline, q}
	}
	switch e.Freshness {
	case FreshnessFresh:
	case FreshnessStale:
		return Verdict{OutcomeUnknown, QualityStale}
	case FreshnessUnsupported:
		return Verdict{OutcomeUnknown, QualityUnsupported}
	case FreshnessForbidden:
		return Verdict{OutcomeUnknown, QualityForbidden}
	default:
		// No receipt at all is not "no requests": nobody said so.
		return Verdict{OutcomeUnknown, QualityNotCollected}
	}
	q := QualityComplete
	if e.Gaps {
		q = QualityPartial
	}
	total := e.Completed + e.Failed
	switch {
	case e.Blocked:
		return Verdict{OutcomeBlocked, q}
	case total > 0 && float64(e.Failed)/float64(total) >= ErrorRateThreshold:
		return Verdict{OutcomeError, q}
	case e.SlowP95:
		return Verdict{OutcomeSlow, q}
	case e.Completed == 0:
		// An open socket with nothing completed is still nothing completed.
		if e.Gaps {
			return Verdict{OutcomeUnknown, QualityPartial}
		}
		return Verdict{OutcomeNoRequests, QualityNoRequests}
	}
	return Verdict{OutcomeHealthy, q}
}

// StageProgress is where one logical operation demonstrably got to.
type StageProgress struct {
	// LastProven is the end of the contiguous run of receipts from the start
	// of the path; empty when even the first stage has no receipt.
	LastProven Stage `json:"last_proven_stage"`
	// Active is set only from an independent active-phase span, never
	// inferred from the receipts.
	Active *Stage `json:"active_stage"`
	// NextUnobserved is the first stage after LastProven with no receipt.
	NextUnobserved Stage `json:"next_unobserved_stage,omitempty"`
	// VisibilityLimitAt is set when NextUnobserved was recorded by a source
	// this scope may not read: its absence then says nothing.
	VisibilityLimitAt Stage `json:"visibility_limit_at,omitempty"`
	// LaterReceipts are receipts past a hole: evidence, but not proof that
	// the hole's stage happened.
	LaterReceipts []Stage `json:"later_receipts,omitempty"`
}

// ProgressAlong reads receipts against path. visible reports whether the
// scope may read a stage's recorder; nil means every stage is visible.
func ProgressAlong(path []Stage, receipts map[Stage]bool, active *Phase, visible func(Stage) bool) StageProgress {
	var p StageProgress
	i := 0
	for ; i < len(path) && receipts[path[i]]; i++ {
		p.LastProven = path[i]
	}
	if i < len(path) {
		p.NextUnobserved = path[i]
		if visible != nil && !visible(path[i]) {
			p.VisibilityLimitAt = path[i]
		}
		for _, s := range path[i+1:] {
			if receipts[s] {
				p.LaterReceipts = append(p.LaterReceipts, s)
			}
		}
	}
	if active != nil {
		s := active.Spec.Stage
		p.Active = &s
	}
	return p
}

// Wiring names one piece of the plan and whether this build has it, so an
// export never implies a pipeline that does not exist.
type Wiring struct {
	Part   string `json:"part"`
	Status string `json:"status"`
}

// Wiring statuses.
const (
	WiringImplemented   = "implemented"
	WiringUnimplemented = "unimplemented"
)

// Unwired is what this foundation does not do yet.
func Unwired() []Wiring {
	return []Wiring{
		{"active_phase_tracker", WiringImplemented},
		{"watchdog", WiringImplemented},
		{"memory_journal", WiringImplemented},
		{"daemon_instrumentation_call_sites", WiringUnimplemented},
		{"durable_segmented_journal", WiringUnimplemented},
		{"stack_capture_worker", WiringUnimplemented},
		{"local_health_cli_and_route", WiringUnimplemented},
		{"cloud_ingest", WiringUnimplemented},
		{"cloud_query", WiringUnimplemented},
		{"relay_and_viewer_producers", WiringUnimplemented},
		{"stage_location_map_per_build", WiringUnimplemented},
	}
}

// Evidence is the local export: a watchdog report, the journal page it
// rests on, and what is not wired, all content-free.
type Evidence struct {
	Schema      string    `json:"schema"`
	GeneratedAt time.Time `json:"generated_at"`
	Watchdog    Report    `json:"watchdog"`
	Journal     Page      `json:"journal"`
	Wiring      []Wiring  `json:"wiring"`
}

// Export assembles Evidence from the watchdog's latest report (taken on the
// watchdog's own goroutine; Scan is not called here). A journal it cannot
// read is a typed failure, never an export with an empty journal that looks
// like a quiet one.
func Export(r *Report, j *Journal, q Query) (Evidence, error) {
	if r == nil || j == nil {
		return Evidence{}, &Error{Code: CodeUnsupported}
	}
	page, err := j.Read(q)
	if err != nil {
		return Evidence{}, err
	}
	return Evidence{Schema: EvidenceSchema, GeneratedAt: r.Wall, Watchdog: *r, Journal: page, Wiring: Unwired()}, nil
}
