package connectionhealth

import (
	"strconv"
	"sync/atomic"
	"time"
)

// Instant is one clock reading: Mono is elapsed time on this boot's
// monotonic clock and is what every age and deadline is measured with; Wall is
// only for showing a person when, and may jump.
type Instant struct {
	Mono time.Duration
	Wall time.Time
}

// Clock is where the tracker and the watchdog read time. Tests inject one
// that jumps.
type Clock interface{ Now() Instant }

type systemClock struct{ base time.Time }

// NewSystemClock is a Clock whose Mono is Go's monotonic reading since the
// clock was made, so a wall-clock step cannot move an age.
func NewSystemClock() Clock { return systemClock{base: time.Now()} }

func (c systemClock) Now() Instant {
	now := time.Now()
	return Instant{Mono: now.Sub(c.base), Wall: now.UTC().Round(0)}
}

// The active-phase table. MaxActiveSlots is every slot; the first
// MaxReservedSlots of them only a Critical phase (reader, writer, drain,
// keepalive, watchdog) may take, so business operations cannot starve the
// loops whose progress proves the rest. Both are capacity rows
// (cloud.health_active_slots, cloud.health_reserved_slots).
const (
	MaxActiveSlots   = 128
	MaxReservedSlots = 8
)

// Spec describes a phase as it begins. Every string in it is an enum, a
// location code or an opaque id: no payload, path or token can be carried.
type Spec struct {
	Source   Source
	Stage    Stage
	Class    Class
	Kind     Kind
	Location string
	// Correlation is the logical trace id, if the operation has one.
	Correlation string
	// Critical asks for a reserved slot.
	Critical bool
	// Warn and Block are the no-progress deadlines of a finite phase. A
	// long-lived phase has neither.
	Warn, Block time.Duration
}

func (s Spec) validate() error {
	switch {
	case !knownSource(s.Source):
		return invalid("source")
	case !knownStage(s.Stage):
		return invalid("stage")
	case classIndex(s.Class) < 0:
		return invalid("class")
	case !ValidLocation(s.Location):
		return invalid("location")
	case s.Correlation != "" && !ValidID(s.Correlation):
		return invalid("correlation")
	}
	switch s.Kind {
	case Finite:
		if s.Warn <= 0 || s.Block <= s.Warn {
			return invalid("deadline")
		}
		if s.Stage == StageIdle {
			return invalid("stage")
		}
	case LongLived:
		if s.Warn != 0 || s.Block != 0 {
			return invalid("deadline")
		}
	default:
		return invalid("kind")
	}
	return nil
}

// Phase is one immutable snapshot of an active operation. Progress replaces
// it with a new one; nobody edits it in place, so the watchdog can read it
// without a lock.
type Phase struct {
	ID           uint64
	Slot         int
	Spec         Spec
	Started      Instant
	LastProgress Instant
	// Progress counts the receipts since Started; a watchdog that sees it
	// rise knows the phase moved even when two readings share an instant.
	Progress uint64
}

type slot struct {
	busy  atomic.Bool
	phase atomic.Pointer[Phase]
}

// Tracker is the active-phase table. All of its methods are safe for any
// number of goroutines and none of them blocks.
type Tracker struct {
	clock Clock
	slots [MaxActiveSlots]slot
	ids   atomic.Uint64
	next  atomic.Uint64

	overflowTotal    atomic.Uint64
	overflowCritical atomic.Uint64
	overflowByClass  [5]atomic.Uint64
	overflowFirst    atomic.Int64 // Mono+1 of the first overflow, 0 for none
	overflowLast     atomic.Int64

	completed [5]atomic.Uint64
	failed    [5]atomic.Uint64

	correlations atomic.Uint64
	invalidSpecs atomic.Uint64
}

// NewTracker makes an empty table.
func NewTracker(clock Clock) *Tracker { return &Tracker{clock: clock} }

// Handle is the caller's grip on one phase. The zero Handle, returned when no
// slot was free, accepts every call and records nothing.
type Handle struct {
	t    *Tracker
	slot int
	id   uint64
	// corr is the phase's logical correlation, kept even when no slot was
	// free so the next boundary continues the same operation.
	corr string
}

// Recorded reports whether the phase holds a slot.
func (h Handle) Recorded() bool { return h.t != nil }

// Correlation is the logical operation this phase belongs to, for the next
// boundary to continue rather than mint a new one. It is "" only when the
// phase was started without one or with no tracker.
func (h Handle) Correlation() string { return h.corr }

// Begin publishes a phase before the caller enters the dependency it covers.
// When no slot is free it counts the overflow and returns slots_exhausted
// with a zero Handle: the caller carries on uninstrumented, it never waits.
func (t *Tracker) Begin(s Spec) (Handle, error) {
	if err := s.validate(); err != nil {
		return Handle{}, err
	}
	lo, hi := MaxReservedSlots, MaxActiveSlots
	if s.Critical {
		lo, hi = 0, MaxReservedSlots
	}
	n := hi - lo
	start := int(t.next.Add(1) % uint64(n))
	for i := 0; i < n; i++ {
		idx := lo + (start+i)%n
		if !t.slots[idx].busy.CompareAndSwap(false, true) {
			continue
		}
		now := t.clock.Now()
		p := &Phase{ID: t.ids.Add(1), Slot: idx, Spec: s, Started: now, LastProgress: now}
		t.slots[idx].phase.Store(p)
		return Handle{t: t, slot: idx, id: p.ID, corr: s.Correlation}, nil
	}
	t.countOverflow(s)
	return Handle{corr: s.Correlation}, &Error{Code: CodeSlotsExhausted}
}

func (t *Tracker) countOverflow(s Spec) {
	at := int64(t.clock.Now().Mono) + 1
	t.overflowTotal.Add(1)
	if s.Critical {
		t.overflowCritical.Add(1)
	}
	t.overflowByClass[classIndex(s.Class)].Add(1)
	t.overflowFirst.CompareAndSwap(0, at)
	t.overflowLast.Store(at)
}

// Start is Begin for a call site: it never returns an error to a business
// path. A nil tracker (monitoring off, or a test that wired none) and a full
// table both give the zero Handle, whose every call is a no-op; the refusal
// was already counted by Begin. A Spec that fails validation is a bug in the
// call site, which the location tests pin, and is counted as invalid.
func (t *Tracker) Start(s Spec) Handle {
	if t == nil {
		return Handle{}
	}
	h, err := t.Begin(s)
	if err != nil && CodeOf(err) == CodeInvalidField {
		t.invalidSpecs.Add(1)
	}
	return h
}

// InvalidSpecs is how many Start calls were refused for an invalid Spec.
func (t *Tracker) InvalidSpecs() uint64 {
	if t == nil {
		return 0
	}
	return t.invalidSpecs.Load()
}

// NewCorrelation is a fresh opaque logical-operation id ("op-1k"): a
// counter, never derived from a payload, channel, session or viewer, so it
// carries nothing but its own uniqueness within this boot. The journal's
// boot_id makes it unique across boots. A nil tracker answers "".
func (t *Tracker) NewCorrelation() string {
	if t == nil {
		return ""
	}
	return "op-" + strconv.FormatUint(t.correlations.Add(1), 36)
}

// Progress records a receipt: the phase is now in stage, and it moved now.
// A handle whose phase has ended is a no-op.
func (h Handle) Progress(stage Stage) error { return h.move(stage, "") }

// Step is Progress that also moves the phase to the code location it is now
// waiting in, so a phase that crosses several functions (sender lookup,
// signature check, inbound callback) names the one it is stuck in.
func (h Handle) Step(stage Stage, location string) error {
	if h.t != nil && !ValidLocation(location) {
		return invalid("location")
	}
	return h.move(stage, location)
}

func (h Handle) move(stage Stage, location string) error {
	if h.t == nil {
		return nil
	}
	if !knownStage(stage) {
		return invalid("stage")
	}
	s := &h.t.slots[h.slot]
	for {
		old := s.phase.Load()
		if old == nil || old.ID != h.id {
			return nil
		}
		p := *old
		p.Spec.Stage = stage
		if location != "" {
			p.Spec.Location = location
		}
		p.LastProgress = h.t.clock.Now()
		p.Progress++
		if s.phase.CompareAndSwap(old, &p) {
			return nil
		}
	}
}

// testHookEndLoaded runs inside End between reading the slot's phase and
// clearing it, so a test can put a concurrent Progress exactly there. It is
// nil outside tests.
var testHookEndLoaded func()

// End releases the slot and counts the outcome by class. Ending twice is
// harmless: the second call finds another phase or none and does nothing. A
// Progress racing End only makes End retry; it never leaves the slot held.
func (h Handle) End(ok bool) {
	if h.t == nil {
		return
	}
	s := &h.t.slots[h.slot]
	var old *Phase
	for {
		old = s.phase.Load()
		if testHookEndLoaded != nil {
			testHookEndLoaded()
		}
		if old == nil || old.ID != h.id {
			// Already ended, or the slot now holds a newer phase.
			return
		}
		// A failed swap means a Progress replaced our own snapshot; the
		// phase is still ours, so read it again rather than leave the slot
		// claimed forever.
		if s.phase.CompareAndSwap(old, nil) {
			break
		}
	}
	i := classIndex(old.Spec.Class)
	if ok {
		h.t.completed[i].Add(1)
	} else {
		h.t.failed[i].Add(1)
	}
	s.busy.Store(false)
}

// Overflow is what the table could not record.
type Overflow struct {
	Total    uint64           `json:"total"`
	Critical uint64           `json:"critical"`
	ByClass  map[Class]uint64 `json:"by_class,omitempty"`
	// First and Last are monotonic offsets of the first and latest refused
	// Begin; both are nil when nothing overflowed.
	FirstMono *time.Duration `json:"first_mono_ns,omitempty"`
	LastMono  *time.Duration `json:"last_mono_ns,omitempty"`
}

// Snapshot is every active phase and the overflow, read from atomics only.
func (t *Tracker) Snapshot() ([]Phase, Overflow) {
	var out []Phase
	for i := range t.slots {
		if p := t.slots[i].phase.Load(); p != nil {
			out = append(out, *p)
		}
	}
	o := Overflow{Total: t.overflowTotal.Load(), Critical: t.overflowCritical.Load()}
	for i, c := range classes {
		if n := t.overflowByClass[i].Load(); n > 0 {
			if o.ByClass == nil {
				o.ByClass = map[Class]uint64{}
			}
			o.ByClass[c] = n
		}
	}
	if f := t.overflowFirst.Load(); f > 0 {
		first, last := time.Duration(f-1), time.Duration(t.overflowLast.Load()-1)
		o.FirstMono, o.LastMono = &first, &last
	}
	return out, o
}

// Counts is completions and failures per class since the tracker was made.
func (t *Tracker) Counts() (completed, failed map[Class]uint64) {
	completed, failed = map[Class]uint64{}, map[Class]uint64{}
	for i, c := range classes {
		completed[c] = t.completed[i].Load()
		failed[c] = t.failed[i].Load()
	}
	return completed, failed
}
