package healthjournal

import (
	"bufio"
	"io"
	"regexp"
	"strconv"
	"time"

	"github.com/sainteye/clawdline/internal/domain/connectionhealth"
)

// Query asks for one page of history, oldest first.
type Query struct {
	// Cursor is a NextCursor or an entry's Cursor from an earlier page —
	// of this boot or any earlier one; empty starts at the oldest retained
	// entry. Cursors name a segment and a frame offset, both of which never
	// change once written, so a cursor stays valid across restarts until
	// retention deletes its segment, which the page then reports as a gap.
	Cursor string
	// Limit is at most connectionhealth.MaxQueryRecords; zero means that.
	Limit int
}

// Entry kinds.
const (
	EntryRecord = "record"
	EntryLoss   = "loss"
)

// Entry is one thing read back from disk.
type Entry struct {
	// Cursor resumes after this entry.
	Cursor string                   `json:"cursor"`
	Kind   string                   `json:"kind"`
	BootID string                   `json:"boot_id"`
	Record *connectionhealth.Record `json:"record,omitempty"`
	Loss   *Loss                    `json:"loss,omitempty"`
}

// Boot is one producing boot whose entries a page holds.
type Boot struct {
	BootID  string    `json:"boot_id"`
	Build   string    `json:"build_commit"`
	Segment uint64    `json:"first_segment"`
	Created time.Time `json:"segment_created"`
}

// Gap reasons.
const (
	GapPruned  = "pruned"
	GapMissing = "missing_segment"
	GapCorrupt = "corrupt_frames"
	// GapTorn is the unreadable end of a segment found at recovery: a
	// crash's half-written frame, or everything after a corrupt one.
	GapTorn = "unreadable_tail"
)

// Gap says entries between two cursors are not available.
type Gap struct {
	Reason string `json:"reason"`
	// Before is where the reader asked to start (or where reading stopped);
	// Resume is the next readable position.
	Before string `json:"before"`
	Resume string `json:"resume"`
}

// Coverage is what a page can and cannot vouch for.
type Coverage struct {
	Quality    connectionhealth.Quality `json:"quality"`
	Durability string                   `json:"durability"`
	// RetentionFloor is the wall time of the oldest retained record: the
	// actual horizon, which may be far short of the target. Absent when no
	// record is retained, which is not the same as a floor of "now".
	RetentionFloor *time.Time `json:"retention_floor,omitempty"`
	TargetSeconds  int64      `json:"retention_target_seconds"`
	TargetBytes    int64      `json:"retention_target_bytes"`
	OldestCursor   string     `json:"oldest_cursor,omitempty"`
	Gap            *Gap       `json:"gap,omitempty"`
	// Truncated says the page stopped at its row or byte bound or at a gap
	// after some entries; NextCursor continues it either way.
	Truncated  bool   `json:"truncated"`
	NextCursor string `json:"next_cursor"`
	// SkippedInvalid are well-framed payloads in this page's range that
	// failed validation and were not exported.
	SkippedInvalid uint64 `json:"skipped_invalid"`
	// The store's loss totals at the time of the read.
	PrunedRecords uint64 `json:"pruned_records_total"`
	PrunedEarly   uint64 `json:"pruned_before_target_total"`
	LostRecords   uint64 `json:"lost_records_total"`
	Refused       uint64 `json:"refused_total"`
}

// Page is one answer. It holds only validated records, loss markers and boot
// identities: no path, no file name, no operating-system message.
type Page struct {
	Schema       string    `json:"schema"`
	GeneratedAt  time.Time `json:"generated_at"`
	CurrentBoot  string    `json:"current_boot_id"`
	CurrentBuild string    `json:"current_build_commit"`
	Boots        []Boot    `json:"boots"`
	Entries      []Entry   `json:"entries"`
	Coverage     Coverage  `json:"coverage"`
}

var cursorPattern = regexp.MustCompile(`^v1\.([0-9]{1,16})\.([0-9]{1,10})$`)

func cursorOf(seg uint64, off int64) string {
	return "v1." + strconv.FormatUint(seg, 10) + "." + strconv.FormatInt(off, 10)
}

func parseCursor(c string) (uint64, int64, bool) {
	m := cursorPattern.FindStringSubmatch(c)
	if m == nil {
		return 0, 0, false
	}
	seg, err1 := strconv.ParseUint(m[1], 10, 64)
	off, err2 := strconv.ParseInt(m[2], 10, 64)
	return seg, off, err1 == nil && err2 == nil && seg > 0
}

// Read answers q from disk. It holds no lock a producer or the worker needs:
// it copies the segment list, reads each segment through its own handle up to
// the length the worker had published, and validates every frame again.
func (s *Store) Read(q Query) (Page, error) {
	if q.Limit < 0 || q.Limit > connectionhealth.MaxQueryRecords {
		return Page{}, &Error{Code: CodeQueryBound, Field: "limit"}
	}
	if q.Limit == 0 {
		q.Limit = connectionhealth.MaxQueryRecords
	}
	var wantSeg uint64
	var wantOff int64
	if q.Cursor != "" {
		var ok bool
		if wantSeg, wantOff, ok = parseCursor(q.Cursor); !ok {
			return Page{}, fail(CodeInvalidCursor)
		}
	}
	select {
	case s.readers <- struct{}{}:
		defer func() { <-s.readers }()
	default:
		return Page{}, fail(CodeReadersBusy)
	}
	select {
	case <-s.done:
		return Page{}, fail(CodeClosed)
	default:
	}
	segs := s.snapshot()
	p := Page{
		Schema: PageSchema, GeneratedAt: s.clock.Now().Wall,
		CurrentBoot: s.bootID, CurrentBuild: s.build,
		Boots: []Boot{}, Entries: []Entry{},
	}
	c := Coverage{
		Quality: connectionhealth.QualityComplete, Durability: DurabilityDisk,
		TargetSeconds: MaxStoreAgeSeconds, TargetBytes: s.storeBytes,
		PrunedRecords: s.st.prunedRecords.Load(),
		PrunedEarly:   s.st.prunedBytes.Load() + s.st.prunedCount.Load(),
		LostRecords:   s.st.lost.Load(), Refused: s.st.refused.Load(),
	}
	if f := floor(segs); f != nil {
		c.RetentionFloor = f
	}
	if len(segs) > 0 {
		c.OldestCursor = cursorOf(segs[0].index, 0)
	}
	c.NextCursor = q.Cursor
	start := 0
	if q.Cursor != "" {
		for start < len(segs) && segs[start].index < wantSeg {
			start++
		}
		switch {
		case start == len(segs):
			last := segs[len(segs)-1:]
			if len(last) == 0 || wantSeg > last[0].index {
				return Page{}, fail(CodeInvalidCursor)
			}
		case segs[start].index != wantSeg:
			reason := GapMissing
			if start == 0 {
				reason = GapPruned
			}
			c.Gap = &Gap{Reason: reason, Before: q.Cursor, Resume: cursorOf(segs[start].index, 0)}
			wantOff = 0
		case wantOff > segs[start].valid.Load():
			return Page{}, fail(CodeInvalidCursor)
		}
	}
	bytes := 0
	full := false
	for i := start; i < len(segs) && !full; i++ {
		g := segs[i]
		off := int64(0)
		if q.Cursor != "" && i == start && g.index == wantSeg {
			off = wantOff
		}
		res := s.readSegment(g, off, q.Limit-len(p.Entries), connectionhealth.MaxQueryBytes-bytes, &p)
		bytes += res.bytes
		c.SkippedInvalid += res.invalid
		c.NextCursor = cursorOf(g.index, res.next)
		if res.full {
			full = true
			c.Truncated = true
			break
		}
		if res.err != "" {
			if c.Gap == nil {
				c.Gap = &Gap{Reason: res.err, Before: cursorOf(g.index, res.next)}
				if i+1 < len(segs) {
					c.Gap.Resume = cursorOf(segs[i+1].index, 0)
				}
			}
			if i+1 < len(segs) {
				c.NextCursor = cursorOf(segs[i+1].index, 0)
			}
			if len(p.Entries) > 0 && i+1 < len(segs) {
				// Stop at the gap so the next page starts there.
				c.Truncated = true
				break
			}
			continue
		}
		if i+1 < len(segs) && g.sealed.Load() {
			c.NextCursor = cursorOf(segs[i+1].index, 0)
		}
	}
	if c.Gap != nil || c.SkippedInvalid > 0 {
		c.Quality = connectionhealth.QualityPartial
	}
	for _, e := range p.Entries {
		if e.Kind == EntryLoss {
			c.Quality = connectionhealth.QualityPartial
		}
	}
	p.Coverage = c
	return p, nil
}

func floor(segs []*segment) *time.Time {
	for _, g := range segs {
		if w := g.firstWall.Load(); w != 0 {
			t := time.Unix(0, w).UTC()
			return &t
		}
	}
	return nil
}

type segRead struct {
	next    int64
	bytes   int
	invalid uint64
	full    bool
	err     string
}

// readSegment appends g's entries from off until it ends, a bound is hit or a
// frame cannot be read.
func (s *Store) readSegment(g *segment, off int64, rows, budget int, p *Page) segRead {
	res := segRead{next: off}
	valid := g.valid.Load() // never past what the worker finished writing
	f, err := openOwned(g.path, g.info)
	if err != nil {
		res.err = GapMissing
		return res
	}
	defer f.Close()
	br := bufio.NewReaderSize(io.NewSectionReader(f, 0, valid), 32<<10)
	// The header is read and checked on every open: the file must still
	// say it is this segment of this boot.
	typ, payload, n, err := readFrame(br)
	var h segmentHeader
	if err != nil || typ != frameSegment || !decodeStrict(payload, &h) || !h.valid(g.index) || h.BootID != g.boot || h.Build != g.build {
		res.err = GapCorrupt
		return res
	}
	pos := int64(n)
	if off > 0 && off < pos {
		res.err = GapCorrupt
		return res
	}
	if off > pos {
		if _, err := br.Discard(int(off - pos)); err != nil {
			res.err = GapCorrupt
			return res
		}
		pos = off
	}
	res.next = pos
	bootAdded := false
	addBoot := func() {
		if bootAdded {
			return
		}
		bootAdded = true
		if k := len(p.Boots); k > 0 && p.Boots[k-1].BootID == g.boot {
			return
		}
		p.Boots = append(p.Boots, Boot{BootID: g.boot, Build: g.build, Segment: g.index, Created: g.created})
	}
	for {
		typ, payload, n, err := readFrame(br)
		if err == io.EOF {
			if g.torn > 0 {
				res.err = GapTorn
			}
			return res
		}
		if err != nil {
			res.err = GapCorrupt
			return res
		}
		var e Entry
		switch typ {
		case frameRecord:
			var r connectionhealth.Record
			if !decodeStrict(payload, &r) || validateRecord(r, g.boot, g.build) != nil {
				res.invalid++
				pos += int64(n)
				res.next = pos
				continue
			}
			e = Entry{Kind: EntryRecord, BootID: g.boot, Record: &r}
		case frameLoss:
			var l Loss
			if !decodeStrict(payload, &l) || !l.valid() {
				res.invalid++
				pos += int64(n)
				res.next = pos
				continue
			}
			e = Entry{Kind: EntryLoss, BootID: g.boot, Loss: &l}
		case frameSeal:
			pos += int64(n)
			res.next = pos
			return res
		default:
			res.err = GapCorrupt
			return res
		}
		if rows == 0 || res.bytes+len(payload) > budget {
			res.full = true
			return res
		}
		pos += int64(n)
		res.next = pos
		e.Cursor = cursorOf(g.index, pos)
		addBoot()
		p.Entries = append(p.Entries, e)
		res.bytes += len(payload)
		rows--
	}
}
