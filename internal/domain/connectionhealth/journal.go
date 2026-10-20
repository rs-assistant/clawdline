package connectionhealth

import (
	"encoding/json"
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
)

// Record is one journal entry: content-free by construction, since every
// string field is an enum, a location code or an opaque id.
type Record struct {
	Schema   string    `json:"schema"`
	Seq      uint64    `json:"seq"`
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
	Build      string `json:"build,omitempty"`
}

func (r Record) validate() error {
	switch {
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
}

// NewJournal opens a journal for one boot; its first record is EventBoot.
func NewJournal(clock Clock, bootID, build string) (*Journal, error) {
	return newJournal(clock, bootID, build, MaxJournalRecords, MaxJournalBytes)
}

func newJournal(clock Clock, bootID, build string, rows, bytes int) (*Journal, error) {
	if !ValidID(bootID) {
		return nil, invalid("boot_id")
	}
	if !ValidID(build) {
		return nil, invalid("build")
	}
	j := &Journal{clock: clock, bootID: bootID, build: build, recs: make([]Record, rows), sizes: make([]int, rows), limit: rows, maxB: bytes}
	if err := j.Append(Record{Event: EventBoot, Build: build}); err != nil {
		return nil, err
	}
	return j, nil
}

// Append stamps r with this journal's schema, boot, sequence and time and
// keeps it, evicting the oldest records the bounds require. A record that is
// invalid or larger than MaxRecordBytes is refused and counted.
func (j *Journal) Append(r Record) error {
	if err := r.validate(); err != nil {
		j.mu.Lock()
		j.refused++
		j.mu.Unlock()
		return err
	}
	now := j.clock.Now()
	j.mu.Lock()
	defer j.mu.Unlock()
	j.seq++
	r.Schema, r.Seq, r.BootID = RecordSchema, j.seq, j.bootID
	r.Wall, r.MonoNS = now.Wall, int64(now.Mono)
	b, err := json.Marshal(r)
	if err != nil || len(b) > MaxRecordBytes {
		j.refused++
		return &Error{Code: CodeRecordTooLarge}
	}
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
	Build       string    `json:"build"`
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
		bytes += j.sizes[i]
		c.NextAfterSeq = r.Seq
	}
	p.Coverage = c
	return p, nil
}
