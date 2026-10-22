package healthsealed

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"unicode/utf8"
)

// SnapshotSchema is the plaintext inside an attachment. Cloud never sees it;
// the paired owner's reader (web/console/src/cloud/health-owner) decodes
// the same whitelist.
const SnapshotSchema = "clawdline.health-owner-diagnostics/1"

// SnapshotSchemaV2 is SnapshotSchema plus the sanitized stall capture and
// the stall's recovery. A writer uses it only for a revision that carries
// either, so a reader that knows only /1 still opens every stall revision
// and refuses, rather than half-reads, the richer ones. A /1 snapshot that
// carries a capture or recovery field is refused.
const SnapshotSchemaV2 = "clawdline.health-owner-diagnostics/2"

// Bounds, registered in internal/domain/capacity.
const (
	// MaxSnapshotBytes is one encoded snapshot; the ciphertext adds only the
	// tag, so this is far under MaxCiphertextBytes.
	MaxSnapshotBytes = 16 << 10
	// MaxSnapshotFieldChars is any one string field.
	MaxSnapshotFieldChars = 512
	// MaxSnapshotCoverage is how many coverage entries one snapshot names.
	MaxSnapshotCoverage = 32
	// MaxSnapshotFrames is the frames of the stalled goroutine one /2
	// snapshot carries; the writer trims further to fit MaxSnapshotBytes.
	MaxSnapshotFrames = 24
	// MaxSnapshotSources is the exact source lines one join names.
	MaxSnapshotSources = 8
)

// CaptureFrame is one sanitized frame: an allowlisted function and a
// source relative to its module or the Go root.
type CaptureFrame struct {
	Function   string `json:"function"`
	Source     string `json:"source,omitempty"`
	SourceKind string `json:"source_kind"`
}

// CaptureExcerpt is the owner's part of a local stall capture: the
// location's join to this build, the stalled goroutine's frames, and
// every count that says what is missing. It is never the raw dump.
type CaptureExcerpt struct {
	Seq        Int64 `json:"seq"`
	DurationNS Int64 `json:"duration_ns"`
	OverBudget bool  `json:"over_budget"`
	// Function is the location map's Go function for the location; Mapped
	// false means this build has no entry and Sources says nothing.
	Function          string         `json:"function,omitempty"`
	Mapped            bool           `json:"mapped"`
	MatchedGoroutines Int64          `json:"matched_goroutines"`
	Sources           []string       `json:"sources,omitempty"`
	State             string         `json:"state,omitempty"`
	Frames            []CaptureFrame `json:"frames,omitempty"`
	FramesOmitted     Int64          `json:"frames_omitted"`
	GoroutinesTotal   Int64          `json:"goroutines_total"`
	RawTruncated      bool           `json:"raw_truncated"`
	Truncated         bool           `json:"truncated"`
	Redacted          Int64          `json:"functions_redacted"`
	SourceUnavailable Int64          `json:"source_unavailable"`
}

// Recovery is how the stalled phase ended, as the journal recorded it,
// independent of any capture.
type Recovery struct {
	// State is recovered or released_outcome_unknown.
	State      string `json:"state"`
	RecoveryNS Int64  `json:"recovery_ns"`
}

// Int64 is an int64 that travels as a decimal string, because a JavaScript
// reader cannot hold a nanosecond count above 2^53 exactly. It also reads a
// plain JSON integer, which is how the committed §13.7 vector writes age_ns.
type Int64 struct {
	Value int64
	Set   bool
}

func (n Int64) MarshalJSON() ([]byte, error) {
	if !n.Set {
		return []byte("null"), nil
	}
	return []byte(`"` + strconv.FormatInt(n.Value, 10) + `"`), nil
}

func (n *Int64) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*n = Int64{}
		return nil
	}
	text := string(b)
	if len(b) >= 2 && b[0] == '"' {
		if err := json.Unmarshal(b, &text); err != nil {
			return fail(CodeBadInput)
		}
	}
	v, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return fail(CodeBadInput)
	}
	*n = Int64{Value: v, Set: true}
	return nil
}

// Coverage is what part of the evidence a snapshot covers, and whether some
// of it was lost before it was sealed. Unknown is not zero: a counter that
// could not be read is unset, not 0.
type Coverage struct {
	Source    string `json:"source"`
	FromMS    Int64  `json:"from_ms"`
	ToMS      Int64  `json:"to_ms"`
	Lost      Int64  `json:"lost"`
	Truncated bool   `json:"truncated"`
}

// Snapshot is the whole whitelist: where the machine was stuck, for how
// long, against what deadline, on which build and stage, under which
// correlation id, and what it covers. There is deliberately no field for a
// raw payload, a Session, a document, terminal or browser content.
type Snapshot struct {
	Schema        string     `json:"schema"`
	Location      string     `json:"location,omitempty"`
	AgeNS         Int64      `json:"age_ns"`
	DeadlineNS    Int64      `json:"deadline_ns"`
	Build         string     `json:"build,omitempty"`
	Stage         string     `json:"stage,omitempty"`
	CorrelationID string     `json:"correlation_id,omitempty"`
	ObservedAtMS  Int64      `json:"observed_at_ms"`
	Coverage      []Coverage `json:"coverage,omitempty"`
	// Truncated says fields or coverage entries were dropped to fit the
	// bounds; Lost counts evidence the source itself had already lost.
	Truncated bool  `json:"truncated,omitempty"`
	Lost      Int64 `json:"lost"`
	// Capture and Recovery exist only in SnapshotSchemaV2.
	Capture  *CaptureExcerpt `json:"capture,omitempty"`
	Recovery *Recovery       `json:"recovery,omitempty"`
}

// Validate refuses a snapshot outside the whitelist's bounds.
func (s Snapshot) Validate() error {
	switch s.Schema {
	case SnapshotSchema:
		if s.Capture != nil || s.Recovery != nil {
			return fail(CodeBadInput)
		}
	case SnapshotSchemaV2:
		if s.Capture == nil && s.Recovery == nil {
			return fail(CodeBadInput)
		}
	default:
		return fail(CodeBadInput)
	}
	if err := s.Capture.validate(); err != nil {
		return err
	}
	if s.Recovery != nil && s.Recovery.State != "recovered" && s.Recovery.State != "released_outcome_unknown" {
		return fail(CodeBadInput)
	}
	for _, f := range []string{s.Location, s.Build, s.Stage, s.CorrelationID} {
		if !boundedText(f) {
			return fail(CodeTooLarge)
		}
	}
	if len(s.Coverage) > MaxSnapshotCoverage {
		return fail(CodeTooLarge)
	}
	for _, c := range s.Coverage {
		if !boundedText(c.Source) {
			return fail(CodeTooLarge)
		}
	}
	return nil
}

// Encode is the snapshot's plaintext bytes.
func (s Snapshot) Encode() ([]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return nil, fail(CodeBadInput)
	}
	out := bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
	if len(out) > MaxSnapshotBytes {
		return nil, fail(CodeTooLarge)
	}
	return out, nil
}

// DecodeSnapshot reads opened plaintext. An unknown field is refused, so a
// newer writer's extra content never renders as if it were whitelisted.
func DecodeSnapshot(b []byte) (Snapshot, error) {
	if len(b) > MaxSnapshotBytes || !utf8.Valid(b) {
		return Snapshot{}, fail(CodeTooLarge)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var s Snapshot
	if err := dec.Decode(&s); err != nil {
		return Snapshot{}, fail(CodeBadInput)
	}
	if dec.More() {
		return Snapshot{}, fail(CodeBadInput)
	}
	if err := s.Validate(); err != nil {
		return Snapshot{}, err
	}
	return s, nil
}

func boundedText(s string) bool {
	return utf8.ValidString(s) && utf8.RuneCountInString(s) <= MaxSnapshotFieldChars
}

func (c *CaptureExcerpt) validate() error {
	if c == nil {
		return nil
	}
	if len(c.Frames) > MaxSnapshotFrames || len(c.Sources) > MaxSnapshotSources {
		return fail(CodeTooLarge)
	}
	if (c.Function != "" && !safeFunction(c.Function)) || !boundedText(c.State) {
		return fail(CodeBadInput)
	}
	for _, src := range c.Sources {
		if !safeSource(src) {
			return fail(CodeBadInput)
		}
	}
	for _, f := range c.Frames {
		if !safeFunction(f.Function) || (f.Source != "" && !safeSource(f.Source)) {
			return fail(CodeBadInput)
		}
		switch f.SourceKind {
		case "module", "std", "dependency", "unavailable":
		default:
			return fail(CodeBadInput)
		}
	}
	return nil
}

// safeSource is a relative "file:line": never absolute, never climbing.
func safeSource(s string) bool {
	if s == "" || len(s) > 200 || strings.HasPrefix(s, "/") || strings.Contains(s, "..") {
		return false
	}
	i := strings.LastIndexByte(s, ':')
	if i <= 0 || i == len(s)-1 {
		return false
	}
	for _, r := range s[i+1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	for _, r := range s[:i] {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', strings.ContainsRune("_./@!+-", r):
		default:
			return false
		}
	}
	return true
}

// safeFunction is an allowlisted Go symbol, or the sanitizer's redaction
// mark: no argument list, address or free text.
func safeFunction(s string) bool {
	if s == "<redacted>" {
		return true
	}
	if s == "" || len(s) > 200 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', strings.ContainsRune("_./*()[]{},-", r):
		default:
			return false
		}
	}
	return true
}
