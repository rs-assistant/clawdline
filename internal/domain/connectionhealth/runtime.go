package connectionhealth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync/atomic"
	"time"
)

// Runtime is the one tracker, journal and watchdog of a daemon boot. The
// daemon makes it before anything that could be instrumented starts, runs it
// for the daemon's whole lifetime (with or without a Cloud line, since a
// local query must answer either way), and hands its Tracker to the call
// sites. A nil *Runtime is monitoring turned off: every method answers as
// such and the call sites receive a nil Tracker, whose Start is a no-op.
type Runtime struct {
	Tracker  *Tracker
	Journal  *Journal
	Watchdog *Watchdog
	BootID   string
	Build    string
	clock    Clock
	running  atomic.Bool
}

// NewRuntime makes the per-boot runtime. build is this binary's version or
// commit as the daemon reports it; anything outside the opaque-id alphabet
// (a dot in "1.4.2", a plus in "+dirty") becomes "_", so the stamp stays an
// id rather than free text. The boot id is fresh random bytes, never a time,
// a path or a host name.
func NewRuntime(build string, cadence time.Duration) (*Runtime, error) {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	return newRuntime(NewSystemClock(), "boot-"+hex.EncodeToString(raw), SanitizeBuild(build), cadence)
}

func newRuntime(clock Clock, bootID, build string, cadence time.Duration) (*Runtime, error) {
	j, err := NewJournal(clock, bootID, build)
	if err != nil {
		return nil, err
	}
	t := NewTracker(clock)
	return &Runtime{
		Tracker: t, Journal: j, Watchdog: NewWatchdog(t, j, clock, cadence),
		BootID: bootID, Build: build, clock: clock,
	}, nil
}

// SanitizeBuild turns a version string into an opaque build id.
func SanitizeBuild(build string) string {
	var b strings.Builder
	for _, r := range build {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
		if b.Len() == 64 {
			break
		}
	}
	if b.Len() == 0 {
		return "unknown"
	}
	return b.String()
}

// Probe is the tracker the call sites get: nil when monitoring is off.
func (r *Runtime) Probe() *Tracker {
	if r == nil {
		return nil
	}
	return r.Tracker
}

// Run runs the watchdog and its journal writer until ctx ends, and returns
// once both have stopped.
func (r *Runtime) Run(ctx context.Context) {
	if r == nil {
		return
	}
	r.running.Store(true)
	defer r.running.Store(false)
	r.Watchdog.Run(ctx)
}

// Running reports whether Run is scanning now.
func (r *Runtime) Running() bool { return r != nil && r.running.Load() }

// Status is the local status answer: the latest report, the journal's
// coverage, and what is and is not wired. A watchdog that has not scanned
// yet is reported as such (Report nil, quality not_collected), never as an
// empty healthy list.
type Status struct {
	Schema   string   `json:"schema"`
	BootID   string   `json:"boot_id"`
	Build    string   `json:"build_commit"`
	Running  bool     `json:"running"`
	Quality  Quality  `json:"quality"`
	Report   *Report  `json:"watchdog"`
	Coverage Coverage `json:"journal_coverage"`
	// ReportAgeNS is how old the latest report is, by this boot's clock.
	ReportAgeNS  *int64   `json:"report_age_ns"`
	InvalidSpecs uint64   `json:"invalid_specs"`
	Wiring       []Wiring `json:"wiring"`
}

// StatusSchema names the local status answer.
const StatusSchema = "clawdline.connection-health.status/1"

// Status reads the runtime without calling Scan: the report is the one the
// watchdog's own goroutine published.
func (r *Runtime) Status() (Status, error) {
	if r == nil {
		return Status{}, &Error{Code: CodeUnsupported}
	}
	page, err := r.Journal.Read(Query{Limit: 1, AfterSeq: 0})
	if err != nil {
		return Status{}, err
	}
	s := Status{
		Schema: StatusSchema, BootID: r.BootID, Build: r.Build, Running: r.Running(),
		Quality: QualityNotCollected, Coverage: page.Coverage, InvalidSpecs: r.Tracker.InvalidSpecs(),
		Wiring: Unwired(),
	}
	// The one-row page is only for the coverage numbers.
	s.Coverage.Truncated, s.Coverage.NextAfterSeq = false, 0
	if rep, ok := r.Watchdog.Latest(); ok {
		s.Report = &rep
		age := int64(r.clock.Now().Mono) - rep.MonoNS
		if age < 0 {
			age = 0
		}
		s.ReportAgeNS = &age
		s.Quality = QualityComplete
		if rep.TraceCoverage != QualityComplete || rep.Transitions != QualityComplete {
			s.Quality = QualityPartial
		}
		if !s.Running || time.Duration(age) > 3*r.Watchdog.cadence {
			// A report older than three scans is not today's answer.
			s.Quality = QualityStale
		}
	}
	return s, nil
}

// Export is the local evidence export with this build's location map. A
// runtime whose watchdog has not scanned yet cannot export: unsupported, not
// an empty file.
func (r *Runtime) Export(q Query) (Evidence, error) {
	if r == nil {
		return Evidence{}, &Error{Code: CodeUnsupported}
	}
	rep, ok := r.Watchdog.Latest()
	if !ok {
		return Evidence{}, &Error{Code: CodeUnsupported, Field: "watchdog"}
	}
	ev, err := Export(&rep, r.Journal, q)
	if err != nil {
		return Evidence{}, err
	}
	ev.Locations = &LocationMap{Build: r.Build, Entries: Locations()}
	return ev, nil
}

// scanPages is how many journal pages one filtered read may walk: the whole
// ring in pages of MaxQueryRecords, and one more. It is not a bound of its
// own but the two registered rows (cloud.health_journal_rows,
// cloud.health_query_rows) divided, so it moves when either is lowered.
func scanPages() int { return MaxJournalRecords/MaxQueryRecords + 2 }

// Events is Journal.Read with a wall-clock window: only records whose wall
// time is in [since, until] are returned (a zero bound is open). The cursor
// stays the sequence, so a window is resumable exactly like a plain page.
// It walks at most the whole ring and stops at q.Limit matches.
func (r *Runtime) Events(q Query, since, until time.Time) (Page, error) {
	if r == nil {
		return Page{}, &Error{Code: CodeUnsupported}
	}
	if !since.IsZero() && !until.IsZero() && until.Before(since) {
		return Page{}, invalid("until")
	}
	if since.IsZero() && until.IsZero() {
		return r.Journal.Read(q)
	}
	limit := q.Limit
	if limit < 0 || limit > MaxQueryRecords {
		return Page{}, &Error{Code: CodeQueryBoundExceeded, Field: "limit"}
	}
	if limit == 0 {
		limit = MaxQueryRecords
	}
	var out Page
	first := true
	cursor := q
	cursor.Limit = MaxQueryRecords
	for i := 0; i < scanPages(); i++ {
		p, err := r.Journal.Read(cursor)
		if err != nil {
			return Page{}, err
		}
		if first {
			out = p
			out.Records = []Record{}
			first = false
		} else if p.Coverage.Quality == QualityPartial && out.Coverage.Quality != QualityPartial {
			out.Coverage.Quality = QualityPartial
		}
		out.Coverage.Truncated = false
		for _, rec := range p.Records {
			out.Coverage.NextAfterSeq = rec.Seq
			if (!since.IsZero() && rec.Wall.Before(since)) || (!until.IsZero() && rec.Wall.After(until)) {
				continue
			}
			if len(out.Records) == limit {
				out.Coverage.Truncated = true
				out.Coverage.NextAfterSeq = out.Records[len(out.Records)-1].Seq
				return out, nil
			}
			out.Records = append(out.Records, rec)
			if rec.Event == EventTransitionsLost {
				out.Coverage.Quality = QualityPartial
			}
		}
		if !p.Coverage.Truncated {
			return out, nil
		}
		cursor.AfterSeq = p.Coverage.NextAfterSeq
	}
	out.Coverage.Truncated = true
	return out, nil
}

// TraceSchema names the local trace answer.
const TraceSchema = "clawdline.connection-health.trace/1"

// Trace is everything this boot holds about one logical operation: the
// phases active for it now (from the latest report) and the journal records
// that name it. Absent records are not success: Coverage says how much of the
// journal the search covered.
type Trace struct {
	Schema      string    `json:"schema"`
	BootID      string    `json:"boot_id"`
	Correlation string    `json:"correlation"`
	Active      []Finding `json:"active"`
	Records     []Record  `json:"records"`
	Coverage    Coverage  `json:"coverage"`
	// ReportScanSeq is the scan the active list came from; nil when the
	// watchdog has not scanned, in which case Active says nothing.
	ReportScanSeq *uint64 `json:"report_scan_seq"`
}

// Trace finds one correlation id. The id must be opaque; a value outside
// that alphabet is invalid, not searched for.
func (r *Runtime) Trace(correlation string) (Trace, error) {
	if r == nil {
		return Trace{}, &Error{Code: CodeUnsupported}
	}
	if !ValidID(correlation) {
		return Trace{}, invalid("correlation")
	}
	t := Trace{Schema: TraceSchema, BootID: r.BootID, Correlation: correlation, Active: []Finding{}, Records: []Record{}}
	if rep, ok := r.Watchdog.Latest(); ok {
		seq := rep.ScanSeq
		t.ReportScanSeq = &seq
		for _, f := range rep.Findings {
			if f.Correlation == correlation {
				t.Active = append(t.Active, f)
			}
		}
	}
	cursor := Query{Limit: MaxQueryRecords}
	for i := 0; i < scanPages(); i++ {
		p, err := r.Journal.Read(cursor)
		if err != nil {
			return Trace{}, err
		}
		if i == 0 {
			t.Coverage = p.Coverage
		} else if p.Coverage.Quality == QualityPartial {
			t.Coverage.Quality = QualityPartial
		}
		for _, rec := range p.Records {
			if rec.Correlation == correlation && len(t.Records) < MaxQueryRecords {
				t.Records = append(t.Records, rec)
			} else if rec.Correlation == correlation {
				t.Coverage.Truncated = true
			}
		}
		t.Coverage.NextAfterSeq = p.Coverage.NextAfterSeq
		if !p.Coverage.Truncated {
			break
		}
		cursor.AfterSeq = p.Coverage.NextAfterSeq
	}
	return t, nil
}
