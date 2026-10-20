package connectionhealth

import (
	"encoding/json"
	"strconv"
	"sync"
	"time"
)

// The in-memory journal ring and its query page. Each is a capacity row:
// cloud.health_journal_rows, cloud.health_journal_bytes,
// cloud.health_record_bytes, cloud.health_query_rows, cloud.health_query_bytes.
const (
	MaxJournalRecords = 4096
	MaxJournalBytes   = 4 << 20
	MaxRecordBytes    = 1024
	MaxQueryRecords   = 200
	MaxQueryBytes     = 1 << 20
)

// Event is what a journal record says happened.
type Event string

const (
	// EventBoot is the first record of every journal: which boot and build
	// the sequence numbers that follow belong to.
	EventBoot            Event = "boot"
	EventSlowDetected    Event = "slow_detected"
	EventStallDetected   Event = "stall_detected"
	EventRecovered       Event = "recovered"
	EventReleasedStalled Event = "released_after_stall"
	EventOverflow        Event = "overflow"
	EventSchedulingGap   Event = "scheduling_gap"
	EventClockAnomaly    Event = "clock_anomaly"
	// EventTransitionsLost says Count watchdog transitions were dropped,
	// before this record, because the queue to the journal was full. Only
	// the journal's own writer appends it.
	EventTransitionsLost Event = "transitions_lost"
	// EventStateAfterLoss re-states, right after a transitions_lost record,
	// each phase the watchdog's latest report holds as slow, blocked or
	// unverified. The dropped transition may have been that phase's
	// stall_detected, and the watchdog does not repeat a transition it has
	// already made, so without this a journal could show a loss followed by
	// silence while a handler is still stuck. Only the writer appends it, at
	// most MaxActiveSlots per loss record.
	EventStateAfterLoss Event = "state_after_loss"
)

// producerEvents are the events Append accepts from a caller. EventBoot and
// EventTransitionsLost are the journal's own, so no caller can forge a boot
// boundary or a loss.
var producerEvents = []Event{
	EventSlowDetected, EventStallDetected, EventRecovered, EventReleasedStalled,
	EventOverflow, EventSchedulingGap, EventClockAnomaly,
}

func producerEvent(e Event) bool {
	for _, x := range producerEvents {
		if x == e {
			return true
		}
	}
	return false
}

func knownKind(k Kind) bool { return k == Finite || k == LongLived }

// Record is one journal entry: content-free by construction, since every
// string field is an enum, a location code or an opaque id.
type Record struct {
	Schema string `json:"schema"`
	Seq    uint64 `json:"producer_seq"`
	// EventID is "<boot_id>-<producer_seq>": unique per producer boot, so
	// an uploader can retry a batch without minting new identities.
	EventID  string    `json:"event_id"`
	BootID   string    `json:"boot_id"`
	Event    Event     `json:"event"`
	Wall     time.Time `json:"wall"`
	MonoNS   int64     `json:"mono_ns"`
	Source   Source    `json:"source,omitempty"`
	Stage    Stage     `json:"stage,omitempty"`
	Class    Class     `json:"class,omitempty"`
	Kind     Kind      `json:"kind,omitempty"`
	Location string    `json:"location,omitempty"`
	PhaseID  uint64    `json:"phase_id,omitempty"`
	// Correlation is the phase's logical trace id.
	Correlation string `json:"correlation,omitempty"`
	// AgeNS is the time since the phase last progressed, or the length of a
	// scheduling gap, or how far a clock went backwards.
	AgeNS      int64  `json:"age_ns,omitempty"`
	DeadlineNS int64  `json:"deadline_ns,omitempty"`
	Count      uint64 `json:"count,omitempty"`
	Rule       string `json:"rule,omitempty"`
	Build      string `json:"build_commit,omitempty"`
	// State is the watchdog state a state_after_loss record re-states.
	State State `json:"state,omitempty"`
}

// validate admits only what the journal can vouch for. Every string is an
// enum, a location code or an opaque id, checked against its own rule; the
// stamps (schema, sequence, event id, boot, time) are the journal's and a
// caller that sets any of them is refused, not silently overwritten. own is
// true only for the journal's own records.
func (r Record) validate(own bool) error {
	switch {
	case r.Schema != "":
		return invalid("schema")
	case r.Seq != 0:
		return invalid("producer_seq")
	case r.EventID != "":
		return invalid("event_id")
	case r.BootID != "":
		return invalid("boot_id")
	case !r.Wall.IsZero() || r.MonoNS != 0:
		return invalid("time")
	case !producerEvent(r.Event) && !(own && (r.Event == EventBoot || r.Event == EventTransitionsLost || r.Event == EventStateAfterLoss)):
		return invalid("event")
	case r.State != "" && r.Event != EventStateAfterLoss:
		return invalid("state")
	case r.State != "" && r.State != StateSlow && r.State != StateBlocked && r.State != StateUnverified:
		return invalid("state")
	case r.Rule != "" && r.Rule != RuleVersion:
		return invalid("rule")
	case r.Kind != "" && !knownKind(r.Kind):
		return invalid("kind")
	case r.Build != "" && r.Event != EventBoot:
		// The build belongs to the boot record and the page, not to an event.
		return invalid("build")
	case r.Source != "" && !knownSource(r.Source):
		return invalid("source")
	case r.Stage != "" && !knownStage(r.Stage):
		return invalid("stage")
	case r.Class != "" && classIndex(r.Class) < 0:
		return invalid("class")
	case r.Location != "" && !ValidLocation(r.Location):
		return invalid("location")
	case r.Correlation != "" && !ValidID(r.Correlation):
		return invalid("correlation")
	case r.Build != "" && !ValidID(r.Build):
		return invalid("build")
	}
	return nil
}

// Journal is a bounded ring of records for one boot. At either bound it
// evicts the oldest record and counts it; it never grows and never blocks on
// anything but its own short lock.
type Journal struct {
	mu      sync.Mutex
	clock   Clock
	bootID  string
	build   string
	recs    []Record // ring storage, len <= MaxJournalRecords
	sizes   []int
	head    int // index of the oldest record
	n       int
	bytes   int
	seq     uint64
	evicted uint64
	refused uint64
	limit   int
	maxB    int
	maxRec  int
}

// NewJournal opens a journal for one boot; its first record is EventBoot.
func NewJournal(clock Clock, bootID, build string) (*Journal, error) {
	return newJournal(clock, bootID, build, MaxJournalRecords, MaxJournalBytes)
}

func newJournal(clock Clock, bootID, build string, rows, bytes int) (*Journal, error) {
	return newJournalBounded(clock, bootID, build, rows, bytes, MaxRecordBytes)
}

// newJournalBounded takes the per-record bound as well, so a test can drive
// record_too_large with a valid record: every field of a real one is bounded
// well under MaxRecordBytes, which is what makes the bound a guard rather
// than a path anybody takes.
func newJournalBounded(clock Clock, bootID, build string, rows, bytes, record int) (*Journal, error) {
	if !ValidID(bootID) {
		return nil, invalid("boot_id")
	}
	if !ValidID(build) {
		return nil, invalid("build")
	}
	j := &Journal{clock: clock, bootID: bootID, build: build, recs: make([]Record, rows), sizes: make([]int, rows), limit: rows, maxB: bytes, maxRec: record}
	if err := j.appendStamped(Record{Event: EventBoot, Build: build}); err != nil {
		return nil, err
	}
	return j, nil
}

// Append stamps r with this journal's schema, boot, sequence and time and
// keeps it, evicting the oldest records the bounds require. A record that is
// invalid or larger than MaxRecordBytes is refused and counted, and takes no
// sequence number, so a gap in the sequence is always an eviction.
func (j *Journal) Append(r Record) error { return j.append(r, false, nil) }

// appendStamped is Append for the journal's own boot record.
func (j *Journal) appendStamped(r Record) error { return j.append(r, true, nil) }

// appendObserved is Append for a record observed at an earlier instant: the
// watchdog's writer appends after the scan, and the record keeps the scan's
// time rather than the moment the writer got the lock.
func (j *Journal) appendObserved(r Record, at Instant) error { return j.append(r, false, &at) }

// appendState journals a re-statement of one stuck phase after a loss.
func (j *Journal) appendState(r Record, at Instant) error {
	r.Event = EventStateAfterLoss
	return j.append(r, true, &at)
}

// appendLost journals that n watchdog transitions were dropped before at.
func (j *Journal) appendLost(n uint64, at Instant) error {
	return j.append(Record{Event: EventTransitionsLost, Count: n, Rule: RuleVersion}, true, &at)
}

func (j *Journal) append(r Record, own bool, at *Instant) error {
	if err := r.validate(own); err != nil {
		j.mu.Lock()
		j.refused++
		j.mu.Unlock()
		return err
	}
	var now Instant
	if at != nil {
		now = *at
	} else {
		now = j.clock.Now()
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	seq := j.seq + 1
	r.Schema, r.Seq, r.BootID = RecordSchema, seq, j.bootID
	r.EventID = j.bootID + "-" + strconv.FormatUint(seq, 10)
	r.Wall, r.MonoNS = now.Wall, int64(now.Mono)
	b, err := json.Marshal(r)
	if err != nil || len(b) > j.maxRec {
		j.refused++
		return &Error{Code: CodeRecordTooLarge}
	}
	j.seq = seq
	for j.n > 0 && (j.n == j.limit || j.bytes+len(b) > j.maxB) {
		j.bytes -= j.sizes[j.head]
		j.recs[j.head] = Record{}
		j.head = (j.head + 1) % j.limit
		j.n--
		j.evicted++
	}
	i := (j.head + j.n) % j.limit
	j.recs[i], j.sizes[i] = r, len(b)
	j.n++
	j.bytes += len(b)
	return nil
}

// Query asks for the records after a sequence number of a named boot.
type Query struct {
	// BootID, when set, must be this journal's boot: a cursor from an
	// earlier boot names sequence numbers that no longer exist.
	BootID   string
	AfterSeq uint64
	// Limit is at most MaxQueryRecords; zero means MaxQueryRecords.
	Limit int
}

// Coverage is what a page can and cannot vouch for.
type Coverage struct {
	// Quality is partial when records the query covers were evicted, or
	// when the page holds a transitions_lost record.
	Quality Quality `json:"quality"`
	// FirstRetainedSeq and LastSeq are the journal's retained range when
	// the page was read; zero only for an empty journal, which cannot
	// happen once the boot record is written.
	FirstRetainedSeq uint64 `json:"first_retained_seq"`
	LastSeq          uint64 `json:"last_seq"`
	// GapFromSeq..GapToSeq were asked for and are gone: evicted before this
	// read. Both are zero when nothing asked for was lost.
	GapFromSeq uint64 `json:"gap_from_seq,omitempty"`
	GapToSeq   uint64 `json:"gap_to_seq,omitempty"`
	Evicted    uint64 `json:"evicted_total"`
	Refused    uint64 `json:"refused_total"`
	// Truncated says the page stopped at its row or byte bound; NextAfterSeq
	// continues it.
	Truncated    bool   `json:"truncated"`
	NextAfterSeq uint64 `json:"next_after_seq"`
	// Durability names what survives a crash: this ring is memory only.
	Durability string `json:"durability"`
}

// Page is one query answer.
type Page struct {
	Schema      string    `json:"schema"`
	GeneratedAt time.Time `json:"generated_at"`
	BootID      string    `json:"boot_id"`
	Build       string    `json:"build_commit"`
	Records     []Record  `json:"records"`
	Coverage    Coverage  `json:"coverage"`
	// NewestAgeNS is how long ago the newest retained record was written.
	NewestAgeNS int64 `json:"newest_age_ns"`
}

// DurabilityMemory is the only durability this package provides today: the
// on-disk segmented journal of the plan is not built.
const DurabilityMemory = "memory_only_lost_on_restart"

// Read answers q. A page never claims to be complete when records it covers
// were evicted, and a cursor from another boot is refused, not answered with
// an empty list.
func (j *Journal) Read(q Query) (Page, error) {
	if q.Limit < 0 || q.Limit > MaxQueryRecords {
		return Page{}, &Error{Code: CodeQueryBoundExceeded, Field: "limit"}
	}
	if q.Limit == 0 {
		q.Limit = MaxQueryRecords
	}
	if q.BootID != "" && !ValidID(q.BootID) {
		return Page{}, invalid("boot_id")
	}
	now := j.clock.Now()
	j.mu.Lock()
	defer j.mu.Unlock()
	if q.BootID != "" && q.BootID != j.bootID {
		return Page{}, &Error{Code: CodeCursorOtherBoot}
	}
	if q.AfterSeq > j.seq {
		// Past the newest sequence this boot has assigned. Checked before
		// any arithmetic on it, so the largest uint64 cannot wrap the gap
		// computation below into a page that looks complete.
		return Page{}, &Error{Code: CodeInvalidCursor, Field: "after_seq"}
	}
	p := Page{Schema: PageSchema, GeneratedAt: now.Wall, BootID: j.bootID, Build: j.build, Records: []Record{}}
	c := Coverage{Quality: QualityComplete, Evicted: j.evicted, Refused: j.refused, Durability: DurabilityMemory, NextAfterSeq: q.AfterSeq}
	if j.n > 0 {
		c.FirstRetainedSeq = j.recs[j.head].Seq
		newest := j.recs[(j.head+j.n-1)%j.limit]
		c.LastSeq = newest.Seq
		if age := int64(now.Mono) - newest.MonoNS; age > 0 {
			p.NewestAgeNS = age
		}
	}
	if q.AfterSeq+1 < c.FirstRetainedSeq {
		c.GapFromSeq, c.GapToSeq = q.AfterSeq+1, c.FirstRetainedSeq-1
		c.Quality = QualityPartial
	}
	bytes := 0
	for k := 0; k < j.n; k++ {
		i := (j.head + k) % j.limit
		r := j.recs[i]
		if r.Seq <= q.AfterSeq {
			continue
		}
		if len(p.Records) == q.Limit || bytes+j.sizes[i] > MaxQueryBytes {
			c.Truncated = true
			break
		}
		p.Records = append(p.Records, r)
		if r.Event == EventTransitionsLost {
			// The page covers a moment the watchdog's transitions did not
			// all reach the journal.
			c.Quality = QualityPartial
		}
		bytes += j.sizes[i]
		c.NextAfterSeq = r.Seq
	}
	p.Coverage = c
	return p, nil
}
