package healthsealed

import (
	"bytes"
	"encoding/json"
	"strconv"
	"unicode/utf8"
)

// SnapshotSchema is the plaintext inside an attachment. Cloud never sees it;
// the paired owner's reader (web/console/src/cloud/health-owner) decodes
// the same whitelist.
const SnapshotSchema = "clawdline.health-owner-diagnostics/1"

// Bounds, registered in internal/domain/capacity.
const (
	// MaxSnapshotBytes is one encoded snapshot; the ciphertext adds only the
	// tag, so this is far under MaxCiphertextBytes.
	MaxSnapshotBytes = 16 << 10
	// MaxSnapshotFieldChars is any one string field.
	MaxSnapshotFieldChars = 512
	// MaxSnapshotCoverage is how many coverage entries one snapshot names.
	MaxSnapshotCoverage = 32
)

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
}

// Validate refuses a snapshot outside the whitelist's bounds.
func (s Snapshot) Validate() error {
	if s.Schema != SnapshotSchema {
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
