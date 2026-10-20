package connectionhealth

import (
	"context"
	"encoding/json"
	"reflect"
	"regexp"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// The stall capture worker (docs/admin-connection-health.md "Instrumentation
// and independent progress capture"). When a scan first calls a finite phase
// blocked, the watchdog hands the capture worker a trigger without waiting:
// one compare-and-swap and a send on an empty channel. The one worker takes
// one goroutine dump into a reused raw buffer, sanitizes it, and publishes an
// immutable result through an atomic pointer. Nothing here touches the
// journal, the logger, the disk or the network, and the raw dump never
// leaves the worker: it is overwritten by the next capture and never
// persisted or exported. Only the sanitized result is.
//
// runtime.Stack cannot be cancelled. A capture that exceeds CaptureBudget is
// marked timed out and the worker unavailable; requests are refused until it
// finishes, and no second worker is ever started. Capture success and
// failure are independent of the stall record and its recovery, which the
// watchdog journals whatever happens here.
const (
	// MaxCaptureRawBytes is the one raw dump buffer (capacity row
	// cloud.health_capture_raw_bytes). A dump that fills it is truncated by
	// the runtime and the result says raw_truncated.
	MaxCaptureRawBytes = 1 << 20
	// MaxCaptureExportBytes is one sanitized result as JSON (row
	// cloud.health_capture_export_bytes): goroutines are dropped from the
	// end, matching ones last, and the result says truncated.
	MaxCaptureExportBytes = 512 << 10
	// MaxCaptureFrames is the frames kept per goroutine; the rest are
	// counted in frames_omitted.
	MaxCaptureFrames = 64
	// MaxCapturesPerWindow is how many captures may start in one
	// CaptureWindow; a trigger past it is refused as rate_limited, so a
	// storm of stalls cannot stop the world over and over.
	MaxCapturesPerWindow = 4
	// MaxCaptureBudgetSeconds is the proposed completion budget. A capture
	// still running after it is reported timed out.
	MaxCaptureBudgetSeconds = 2
)

// CaptureBudget is MaxCaptureBudgetSeconds as a duration.
const CaptureBudget = MaxCaptureBudgetSeconds * time.Second

// CaptureWindow is the interval MaxCapturesPerWindow counts in.
const CaptureWindow = time.Minute

// CaptureSchema names one sanitized capture.
const CaptureSchema = "clawdline.connection-health.capture/1"

// Capture worker states.
const (
	CaptureOff      = "off"
	CaptureIdle     = "idle"
	CaptureRunning  = "running"
	CaptureTimedOut = "timed_out_unavailable"
)

// Frame source kinds.
const (
	SourceKindModule      = "module"
	SourceKindStd         = "std"
	SourceKindDependency  = "dependency"
	SourceKindUnavailable = "unavailable"
)

// CaptureTrigger is the stalled phase a capture was taken for: enums,
// a location code and an opaque id, as the watchdog read them.
type CaptureTrigger struct {
	PhaseID     uint64 `json:"phase_id"`
	Location    string `json:"location"`
	Stage       Stage  `json:"stage"`
	Class       Class  `json:"class"`
	Correlation string `json:"correlation,omitempty"`
	AgeNS       int64  `json:"age_ns"`
	DeadlineNS  int64  `json:"deadline_ns"`
	// RequestedMonoNS and RequestedWall are the scan that asked.
	RequestedMonoNS int64     `json:"requested_mono_ns"`
	RequestedWall   time.Time `json:"requested_wall"`
}

// CaptureFrame is one sanitized frame: an allowlisted function name and a
// source location relative to its module or the Go root, never an absolute
// path, an argument, an address or an offset.
type CaptureFrame struct {
	Function   string `json:"function"`
	Source     string `json:"source,omitempty"`
	SourceKind string `json:"source_kind"`
}

// CaptureGoroutine is one goroutine of the dump.
type CaptureGoroutine struct {
	ID    uint64 `json:"id"`
	State string `json:"state"`
	// WaitMinutes is the runtime's own "N minutes" wait, when it printed one.
	WaitMinutes int `json:"wait_minutes,omitempty"`
	// MatchesLocation says a frame is the trigger location's function.
	MatchesLocation bool           `json:"matches_location,omitempty"`
	Frames          []CaptureFrame `json:"frames"`
	FramesOmitted   int            `json:"frames_omitted,omitempty"`
	CreatedBy       *CaptureFrame  `json:"created_by,omitempty"`
}

// CaptureJoin is the trigger's location code joined to this build: the Go
// function the location map names, and the exact source lines the dump
// found it at.
type CaptureJoin struct {
	Location string `json:"location"`
	Function string `json:"function,omitempty"`
	// Mapped is false when this build's location map has no entry for the
	// code: then nothing below it says anything.
	Mapped     bool     `json:"mapped"`
	Goroutines int      `json:"matched_goroutines"`
	Sources    []string `json:"sources"`
}

// Capture is one sanitized dump. It is immutable once published.
type Capture struct {
	Schema  string         `json:"schema"`
	Seq     uint64         `json:"seq"`
	BootID  string         `json:"boot_id"`
	Build   string         `json:"build_commit"`
	Trigger CaptureTrigger `json:"trigger"`
	// DurationNS is the dump and the sanitizing, by the boot's clock.
	DurationNS int64 `json:"duration_ns"`
	// OverBudget says it finished after CaptureBudget: the status had
	// already reported the worker unavailable.
	OverBudget   bool        `json:"over_budget"`
	RawBytes     int         `json:"raw_bytes"`
	RawTruncated bool        `json:"raw_truncated"`
	Join         CaptureJoin `json:"join"`
	// GoroutinesTotal is how many the dump held; GoroutinesOmitted how many
	// were dropped to fit MaxCaptureExportBytes.
	GoroutinesTotal   int `json:"goroutines_total"`
	GoroutinesOmitted int `json:"goroutines_omitted"`
	FramesOmitted     int `json:"frames_omitted"`
	// Redacted counts functions outside the allowlist; SourceUnavailable
	// frames whose file could not be made relative; Unparsed dump lines the
	// sanitizer did not recognise and dropped.
	Redacted          int                `json:"functions_redacted"`
	SourceUnavailable int                `json:"source_unavailable"`
	Unparsed          int                `json:"lines_unparsed"`
	Truncated         bool               `json:"truncated"`
	Goroutines        []CaptureGoroutine `json:"goroutines"`
}

// CaptureStatus is the worker as atomics read it now.
type CaptureStatus struct {
	State   string `json:"state"`
	Enabled bool   `json:"enabled"`
	// RunningNS is how long the capture in flight has run.
	RunningNS          int64  `json:"running_ns,omitempty"`
	Requested          uint64 `json:"requested_total"`
	Started            uint64 `json:"started_total"`
	Completed          uint64 `json:"completed_total"`
	RefusedBusy        uint64 `json:"refused_busy_total"`
	RefusedUnavailable uint64 `json:"refused_unavailable_total"`
	RefusedRate        uint64 `json:"refused_rate_limited_total"`
	SkippedOff         uint64 `json:"skipped_off_total"`
	TimedOut           uint64 `json:"timed_out_total"`
	OverBudget         uint64 `json:"over_budget_total"`
	// Rejected counts captures the final allowlist check refused to publish.
	Rejected uint64 `json:"rejected_total"`
	// LatestSeq is the newest published capture, 0 before the first.
	LatestSeq uint64 `json:"latest_seq"`
}

// Capturer is the one capture worker of a runtime.
type Capturer struct {
	clock  Clock
	bootID string
	build  string
	// locations is this build's location map (Locations; a test swaps it).
	locations func() []LocationEntry
	// stack fills buf with every goroutine's trace (runtime.Stack).
	stack func(buf []byte) int
	roots sourceRoots

	enabled atomic.Bool
	busy    atomic.Bool
	// startMono is the running capture's start, 0 when none; timedOut is
	// set once for it when it passes the budget.
	startMono atomic.Int64
	timedOut  atomic.Bool
	jobs      chan CaptureTrigger // capacity 1, empty whenever busy is false
	latest    atomic.Pointer[Capture]
	seq       atomic.Uint64
	// workers counts worker loops ever started: one, by construction.
	workers atomic.Int32

	window      atomic.Int64 // start of the current rate window, mono ns
	windowCount atomic.Int32

	requested, started, completed            atomic.Uint64
	refusedBusy, refusedUnavail, refusedRate atomic.Uint64
	skippedOff, timedOutTotal, overBudget    atomic.Uint64
	rejected                                 atomic.Uint64

	raw []byte // the worker's own; never leaves it
}

// NewCapturer makes a worker, off until SetEnabled(true).
func NewCapturer(clock Clock, bootID, build string) *Capturer {
	return &Capturer{clock: clock, bootID: bootID, build: build, locations: Locations,
		stack: func(b []byte) int { return runtime.Stack(b, true) },
		roots: buildRoots(), jobs: make(chan CaptureTrigger, 1)}
}

// SetEnabled turns capture on or off. Off refuses every trigger as
// skipped_off; a capture in flight finishes.
func (c *Capturer) SetEnabled(on bool) {
	if c != nil {
		c.enabled.Store(on)
	}
}

// Request hands a trigger to the worker without waiting. It reports
// whether the worker took it; every refusal is counted by reason.
func (c *Capturer) Request(t CaptureTrigger) bool {
	if c == nil {
		return false
	}
	c.requested.Add(1)
	if !c.enabled.Load() {
		c.skippedOff.Add(1)
		return false
	}
	if !c.busy.CompareAndSwap(false, true) {
		if c.timedOut.Load() {
			c.refusedUnavail.Add(1)
		} else {
			c.refusedBusy.Add(1)
		}
		return false
	}
	now := int64(c.clock.Now().Mono)
	// window holds its start plus one, so a clock that starts at zero is
	// not mistaken for no window.
	if start := c.window.Load(); start == 0 || now+1-start >= int64(CaptureWindow) {
		c.window.Store(now + 1)
		c.windowCount.Store(0)
	}
	if c.windowCount.Add(1) > MaxCapturesPerWindow {
		c.busy.Store(false)
		c.refusedRate.Add(1)
		return false
	}
	c.timedOut.Store(false)
	c.startMono.Store(max(now, 1))
	select {
	case c.jobs <- t:
		return true
	default:
		// Unreachable while busy guards the channel; refuse rather than wait.
		c.startMono.Store(0)
		c.busy.Store(false)
		c.refusedBusy.Add(1)
		return false
	}
}

// observe marks a capture past its budget as timed out, once. The watchdog
// calls it every scan; it reads and writes atomics only.
func (c *Capturer) observe(now Instant) {
	if c == nil {
		return
	}
	start := c.startMono.Load()
	if start == 0 || int64(now.Mono)-start < int64(CaptureBudget) {
		return
	}
	if c.timedOut.CompareAndSwap(false, true) {
		c.timedOutTotal.Add(1)
	}
}

// Run is the one worker loop. It returns when ctx ends and no capture is in
// flight; a capture stuck in the runtime holds only this goroutine, and the
// caller does not wait for it.
func (c *Capturer) Run(ctx context.Context) {
	if c == nil {
		return
	}
	if !c.workers.CompareAndSwap(0, 1) {
		// A second Run would be a second worker; there is only ever one.
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case t := <-c.jobs:
			c.take(t)
		}
	}
}

func (c *Capturer) take(t CaptureTrigger) {
	c.started.Add(1)
	begin := c.clock.Now()
	if c.raw == nil {
		c.raw = make([]byte, MaxCaptureRawBytes)
	}
	n := c.stack(c.raw)
	if n < 0 || n > len(c.raw) {
		n = 0
	}
	got := sanitizeStack(c.raw[:n], c.roots, c.joinFor(t.Location))
	clear(c.raw[:n])
	got.Schema, got.BootID, got.Build, got.Trigger = CaptureSchema, c.bootID, c.build, t
	got.RawBytes, got.RawTruncated = n, n == len(c.raw)
	end := c.clock.Now()
	got.DurationNS = nonNegative(end.Mono - begin.Mono)
	if time.Duration(got.DurationNS) >= CaptureBudget || c.timedOut.Load() {
		got.OverBudget = true
		c.overBudget.Add(1)
	}
	fitExport(&got)
	got.Seq = c.seq.Add(1)
	if got.validate() == nil {
		c.latest.Store(&got)
		c.completed.Add(1)
	} else {
		c.rejected.Add(1)
	}
	c.startMono.Store(0)
	c.timedOut.Store(false)
	c.busy.Store(false)
}

func (c *Capturer) joinFor(location string) CaptureJoin {
	j := CaptureJoin{Location: location, Sources: []string{}}
	for _, e := range c.locations() {
		if e.Code == location {
			j.Function, j.Mapped = e.Function, true
			break
		}
	}
	return j
}

// Latest is the newest published capture.
func (c *Capturer) Latest() (*Capture, bool) {
	if c == nil {
		return nil, false
	}
	p := c.latest.Load()
	return p, p != nil
}

// Status reads the worker's atomics. A capture running past its budget
// reads timed_out_unavailable even before the next scan marks it.
func (c *Capturer) Status() CaptureStatus {
	if c == nil {
		return CaptureStatus{State: CaptureOff}
	}
	s := CaptureStatus{Enabled: c.enabled.Load(), State: CaptureIdle,
		Requested: c.requested.Load(), Started: c.started.Load(), Completed: c.completed.Load(),
		RefusedBusy: c.refusedBusy.Load(), RefusedUnavailable: c.refusedUnavail.Load(),
		RefusedRate: c.refusedRate.Load(), SkippedOff: c.skippedOff.Load(),
		TimedOut: c.timedOutTotal.Load(), OverBudget: c.overBudget.Load(), Rejected: c.rejected.Load(),
		LatestSeq: c.seq.Load()}
	if start := c.startMono.Load(); start != 0 {
		s.RunningNS = nonNegative(c.clock.Now().Mono - time.Duration(start))
		s.State = CaptureRunning
		if c.timedOut.Load() || time.Duration(s.RunningNS) >= CaptureBudget {
			s.State = CaptureTimedOut
		}
	} else if !s.Enabled {
		s.State = CaptureOff
	}
	return s
}

// sourceRoots are the prefixes a frame's file is made relative to.
type sourceRoots struct {
	module     string // this module's directory, with trailing slash
	modulePath string // "github.com/sainteye/clawdline/"
	goroot     string // the Go root's src directory, with trailing slash
	deps       []string
}

const modulePath = "github.com/sainteye/clawdline/"

func buildRoots() sourceRoots {
	r := sourceRoots{modulePath: modulePath}
	if _, file, _, ok := runtime.Caller(0); ok {
		if i := strings.LastIndex(file, "internal/domain/connectionhealth/"); i > 0 {
			r.module = file[:i]
		}
	}
	pc := reflect.ValueOf(runtime.Gosched).Pointer()
	if fn := runtime.FuncForPC(pc); fn != nil {
		file, _ := fn.FileLine(pc)
		if i := strings.LastIndex(file, "runtime/"); i > 0 {
			r.goroot = file[:i]
		}
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, d := range info.Deps {
			r.deps = append(r.deps, d.Path)
		}
	}
	return r
}

var (
	goroutineHeader = regexp.MustCompile(`^goroutine ([0-9]{1,19}) (?:[a-z]+=[0-9a-fx]+ )*\[([^\]]{1,200})\]:$`)
	safeFunction    = regexp.MustCompile(`^[A-Za-z0-9_./*()\[\]{},-]{1,200}$`)
	safeSource      = regexp.MustCompile(`^[A-Za-z0-9_./@!+-]{1,200}:[0-9]{1,7}$`)
	waitState       = regexp.MustCompile(`^[a-zA-Z][a-zA-Z .()]{0,39}$`)
	waitMinutes     = regexp.MustCompile(`^([0-9]{1,6}) minutes$`)
	safeLocation    = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z0-9_]+){0,7}$`)
)

// sanitizeStack turns a raw runtime.Stack dump into allowlisted frames. A
// line it does not recognise is dropped and counted, never copied.
func sanitizeStack(raw []byte, roots sourceRoots, join CaptureJoin) Capture {
	c := Capture{Join: join, Goroutines: []CaptureGoroutine{}}
	var g *CaptureGoroutine
	var pending *CaptureFrame // a function line waiting for its file line
	flush := func() {
		if g == nil {
			return
		}
		if g.Frames == nil {
			g.Frames = []CaptureFrame{}
		}
		c.Goroutines = append(c.Goroutines, *g)
		g, pending = nil, nil
	}
	for _, line := range strings.Split(string(raw), "\n") {
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "goroutine "):
			flush()
			m := goroutineHeader.FindStringSubmatch(line)
			if m == nil {
				c.Unparsed++
				continue
			}
			id, _ := strconv.ParseUint(m[1], 10, 64)
			g = &CaptureGoroutine{ID: id}
			g.State, g.WaitMinutes = sanitizeState(m[2])
		case g == nil:
			c.Unparsed++
		case strings.HasPrefix(line, "\t"):
			if pending == nil {
				c.Unparsed++
				continue
			}
			pending.Source, pending.SourceKind = roots.relative(line[1:])
			if pending.Function == RedactedFunction {
				// A redacted function's file would name what was redacted.
				pending.Source, pending.SourceKind = "", SourceKindUnavailable
			}
			if pending.SourceKind == SourceKindUnavailable {
				c.SourceUnavailable++
			}
			if join.Mapped && pending.Function == join.Function {
				g.MatchesLocation = true
				if pending.Source != "" && !contains(c.Join.Sources, pending.Source) && len(c.Join.Sources) < MaxCaptureFrames {
					c.Join.Sources = append(c.Join.Sources, pending.Source)
				}
			}
			pending = nil
		case strings.HasPrefix(line, "...") && strings.Contains(line, "frames elided"):
			g.FramesOmitted++
		case strings.HasPrefix(line, "created by "):
			fn := strings.TrimPrefix(line, "created by ")
			if i := strings.Index(fn, " in goroutine "); i >= 0 {
				fn = fn[:i]
			}
			f := CaptureFrame{Function: roots.function(fn, &c.Redacted), SourceKind: SourceKindUnavailable}
			g.CreatedBy = &f
			pending = g.CreatedBy
		default:
			f := CaptureFrame{Function: roots.function(stripArgs(line), &c.Redacted), SourceKind: SourceKindUnavailable}
			if len(g.Frames) >= MaxCaptureFrames {
				g.FramesOmitted++
				pending = &CaptureFrame{Function: f.Function}
				continue
			}
			g.Frames = append(g.Frames, f)
			pending = &g.Frames[len(g.Frames)-1]
		}
	}
	flush()
	c.GoroutinesTotal = len(c.Goroutines)
	for i := range c.Goroutines {
		if c.Goroutines[i].MatchesLocation {
			c.Join.Goroutines++
		}
		c.FramesOmitted += c.Goroutines[i].FramesOmitted
	}
	// Matching goroutines first, so trimming keeps them longest.
	sort.SliceStable(c.Goroutines, func(i, j int) bool {
		return c.Goroutines[i].MatchesLocation && !c.Goroutines[j].MatchesLocation
	})
	return c
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// sanitizeState keeps the runtime's wait reason words and minutes only.
func sanitizeState(s string) (string, int) {
	var words []string
	minutes := 0
	for _, part := range strings.Split(s, ", ") {
		if m := waitMinutes.FindStringSubmatch(part); m != nil {
			minutes, _ = strconv.Atoi(m[1])
			continue
		}
		if waitState.MatchString(part) {
			words = append(words, part)
		}
	}
	if len(words) == 0 {
		return "unrecognised", minutes
	}
	return strings.Join(words, ", "), minutes
}

// stripArgs removes the trailing argument list "(0x1, {0x2, ...})".
func stripArgs(line string) string {
	if !strings.HasSuffix(line, ")") {
		return line
	}
	depth := 0
	for i := len(line) - 1; i >= 0; i-- {
		switch line[i] {
		case ')':
			depth++
		case '(':
			depth--
			if depth == 0 {
				return line[:i]
			}
		}
	}
	return line
}

// RedactedFunction stands for a function outside the allowlist.
const RedactedFunction = "<redacted>"

// function keeps a function name whose package is this module, the
// standard library (or main), or a dependency this binary was built with.
func (r sourceRoots) function(fn string, redacted *int) string {
	if !safeFunction.MatchString(fn) {
		*redacted++
		return RedactedFunction
	}
	slash := strings.Index(fn, "/")
	switch {
	case strings.HasPrefix(fn, r.modulePath):
	case slash < 0 || !strings.Contains(fn[:slash], "."):
		// The standard library or main: its first path element has no dot.
	default:
		ok := false
		for _, d := range r.deps {
			if strings.HasPrefix(fn, d+".") || strings.HasPrefix(fn, d+"/") {
				ok = true
				break
			}
		}
		if !ok {
			*redacted++
			return RedactedFunction
		}
	}
	return fn
}

// relative makes "\t/abs/file.go:12 +0x1d" a location relative to this
// module, the Go root or the module cache, or unavailable.
func (r sourceRoots) relative(line string) (string, string) {
	if i := strings.LastIndex(line, " +0x"); i >= 0 {
		line = line[:i]
	}
	kind := SourceKindUnavailable
	var rel string
	switch {
	case r.module != "" && strings.HasPrefix(line, r.module):
		rel, kind = line[len(r.module):], SourceKindModule
	case strings.HasPrefix(line, r.modulePath):
		rel, kind = line[len(r.modulePath):], SourceKindModule
	case r.goroot != "" && strings.HasPrefix(line, r.goroot):
		rel, kind = line[len(r.goroot):], SourceKindStd
	case strings.Contains(line, "/pkg/mod/"):
		rel, kind = line[strings.Index(line, "/pkg/mod/")+len("/pkg/mod/"):], SourceKindDependency
	case !strings.HasPrefix(line, "/") && strings.Contains(line, "@v"):
		rel, kind = line, SourceKindDependency
	}
	if kind == SourceKindDependency && !r.listedDependency(rel) {
		// Only a module this binary was built with; anything else in a
		// module cache could be any path at all.
		return "", SourceKindUnavailable
	}
	if kind == SourceKindUnavailable || strings.Contains(rel, "..") || strings.HasPrefix(rel, "/") || !safeSource.MatchString(rel) {
		return "", SourceKindUnavailable
	}
	return rel, kind
}

func (r sourceRoots) listedDependency(rel string) bool {
	for _, d := range r.deps {
		if strings.HasPrefix(rel, d+"@") {
			return true
		}
	}
	return false
}

// fitExport drops goroutines from the end until the capture's JSON fits
// MaxCaptureExportBytes, and says so.
func fitExport(c *Capture) {
	size := func() int {
		b, err := json.Marshal(c)
		if err != nil {
			return MaxCaptureExportBytes + 1
		}
		return len(b)
	}
	if size() <= MaxCaptureExportBytes {
		return
	}
	// Estimate per goroutine, then confirm; halve until it fits.
	keep := len(c.Goroutines)
	for keep > 0 && size() > MaxCaptureExportBytes {
		keep = keep * 3 / 4
		c.GoroutinesOmitted += len(c.Goroutines) - keep
		c.Goroutines = c.Goroutines[:keep]
		c.Truncated = true
	}
}

// validate refuses a capture carrying anything outside the allowlist; it is
// checked again before a capture is published, whatever the sanitizer did.
func (c *Capture) validate() error {
	if c.Schema != CaptureSchema || !ValidID(c.BootID) || !ValidID(c.Build) {
		return invalid("capture")
	}
	if c.Trigger.Location != "" && !ValidLocation(c.Trigger.Location) {
		return invalid("capture.trigger.location")
	}
	if c.Trigger.Correlation != "" && !ValidID(c.Trigger.Correlation) {
		return invalid("capture.trigger.correlation")
	}
	if !safeLocation.MatchString(c.Join.Location) && c.Join.Location != "" {
		return invalid("capture.join.location")
	}
	frame := func(f CaptureFrame) error {
		if f.Function != RedactedFunction && !safeFunction.MatchString(f.Function) {
			return invalid("capture.frame.function")
		}
		if f.Source != "" && !validSource(f.Source) {
			return invalid("capture.frame.source")
		}
		return nil
	}
	for _, s := range c.Join.Sources {
		if !validSource(s) {
			return invalid("capture.join.sources")
		}
	}
	for _, g := range c.Goroutines {
		if g.State != "unrecognised" && !validState(g.State) {
			return invalid("capture.goroutine.state")
		}
		for _, f := range g.Frames {
			if err := frame(f); err != nil {
				return err
			}
		}
		if g.CreatedBy != nil {
			if err := frame(*g.CreatedBy); err != nil {
				return err
			}
		}
	}
	return nil
}

func validSource(s string) bool {
	return safeSource.MatchString(s) && !strings.HasPrefix(s, "/") && !strings.Contains(s, "..")
}

func validState(s string) bool {
	for _, part := range strings.Split(s, ", ") {
		if !waitState.MatchString(part) {
			return false
		}
	}
	return true
}

// CaptureRecovery is how the stalled phase a capture was taken for ended,
// read from the journal independently of the capture.
type CaptureRecovery struct {
	// State is recovered, released_outcome_unknown, still_blocked, or
	// unknown (the journal no longer holds, or never got, the answer).
	State string `json:"state"`
	// RecoveredAt and RecoveryNS are set for recovered: the journal's wall
	// time and the monotonic time from the capture's trigger to it.
	RecoveredAt *time.Time `json:"recovered_at,omitempty"`
	RecoveryNS  *int64     `json:"recovery_ns,omitempty"`
	// JournalQuality is the coverage of the journal search.
	JournalQuality Quality `json:"journal_quality"`
}

// Recovery states.
const (
	RecoveryRecovered    = "recovered"
	RecoveryReleased     = "released_outcome_unknown"
	RecoveryStillBlocked = "still_blocked"
	RecoveryUnknown      = "unknown"
)

// StallCapture is the capture section of the local export: the worker's
// status, the latest sanitized capture, and its stalled phase's recovery.
type StallCapture struct {
	Status   CaptureStatus    `json:"status"`
	Latest   *Capture         `json:"latest"`
	Recovery *CaptureRecovery `json:"recovery,omitempty"`
}
