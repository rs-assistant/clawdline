package healthjournal

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/crc32"
	"io"
	"strconv"
	"time"

	"github.com/sainteye/clawdline/internal/domain/connectionhealth"
)

// A frame on disk:
//
//	[0:4)  payload length n, little-endian uint32, 1 <= n <= MaxFrameBytes-9
//	[4:8)  CRC-32C over [8:9+n), little-endian
//	[8]    frame type
//	[9:9+n) payload, JSON
//
// A segment is a segment-header frame, then record and loss frames, then at
// most one seal frame. The CRC catches a torn or flipped write; it is not
// authentication, so every payload is also validated field by field when it
// is read back, and a forged but well-framed record is skipped and counted.
const frameHeader = 9

const (
	frameSegment byte = 1
	frameRecord  byte = 2
	frameLoss    byte = 3
	frameSeal    byte = 4
)

var crcTable = crc32.MakeTable(crc32.Castagnoli)

var (
	errTorn    = errors.New("torn frame")
	errCorrupt = errors.New("corrupt frame")
)

func appendFrame(dst []byte, typ byte, payload []byte) []byte {
	var h [frameHeader]byte
	binary.LittleEndian.PutUint32(h[0:4], uint32(len(payload)))
	c := crc32.Update(0, crcTable, []byte{typ})
	c = crc32.Update(c, crcTable, payload)
	binary.LittleEndian.PutUint32(h[4:8], c)
	h[8] = typ
	dst = append(dst, h[:]...)
	return append(dst, payload...)
}

// readFrame reads the next frame. io.EOF is a clean end at a frame boundary;
// errTorn is an end inside a frame; errCorrupt is a frame that cannot be one.
func readFrame(r *bufio.Reader) (byte, []byte, int, error) {
	var h [frameHeader]byte
	n, err := io.ReadFull(r, h[:])
	if err == io.EOF {
		return 0, nil, 0, io.EOF
	}
	if err != nil {
		return 0, nil, n, errTorn
	}
	size := binary.LittleEndian.Uint32(h[0:4])
	typ := h[8]
	if size == 0 || size > MaxFrameBytes-frameHeader || typ < frameSegment || typ > frameSeal {
		return 0, nil, n, errCorrupt
	}
	payload := make([]byte, size)
	m, err := io.ReadFull(r, payload)
	if err != nil {
		return 0, nil, n + m, errTorn
	}
	c := crc32.Update(0, crcTable, []byte{typ})
	if crc32.Update(c, crcTable, payload) != binary.LittleEndian.Uint32(h[4:8]) {
		return 0, nil, n + m, errCorrupt
	}
	return typ, payload, n + m, nil
}

// decodeStrict decodes exactly one JSON value with no field the type does not
// name and nothing after it.
func decodeStrict(p []byte, v any) bool {
	d := json.NewDecoder(bytes.NewReader(p))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		return false
	}
	return d.InputOffset() == int64(len(p))
}

type segmentHeader struct {
	Schema  string    `json:"schema"`
	Index   uint64    `json:"segment"`
	BootID  string    `json:"boot_id"`
	Build   string    `json:"build_commit"`
	Created time.Time `json:"created"`
}

func (h segmentHeader) valid(index uint64) bool {
	return h.Schema == SegmentSchema && h.Index == index &&
		connectionhealth.ValidID(h.BootID) && connectionhealth.ValidID(h.Build) && !h.Created.IsZero()
}

// Loss reasons a loss marker may carry.
const (
	LossQueueFull   = "queue_full"
	LossWriteFailed = "write_failed"
)

// Loss is a marker the worker writes before the next records it manages to
// write: this many records were admitted or offered and never reached disk.
type Loss struct {
	Records uint64    `json:"records"`
	Reason  string    `json:"reason"`
	Wall    time.Time `json:"wall"`
}

func (l Loss) valid() bool {
	return l.Records > 0 && (l.Reason == LossQueueFull || l.Reason == LossWriteFailed) && !l.Wall.IsZero()
}

type sealPayload struct {
	Records uint64 `json:"records"`
}

var events = []connectionhealth.Event{
	connectionhealth.EventBoot, connectionhealth.EventSlowDetected, connectionhealth.EventStallDetected,
	connectionhealth.EventRecovered, connectionhealth.EventReleasedStalled, connectionhealth.EventOverflow,
	connectionhealth.EventSchedulingGap, connectionhealth.EventClockAnomaly, connectionhealth.EventTransitionsLost,
	connectionhealth.EventStateAfterLoss,
}

// restatedStates are the watchdog states a state_after_loss record may carry.
var restatedStates = []connectionhealth.State{
	connectionhealth.StateSlow, connectionhealth.StateBlocked, connectionhealth.StateUnverified,
}

var sources = []connectionhealth.Source{
	connectionhealth.SourceDaemon, connectionhealth.SourceCloudAPI, connectionhealth.SourceRelay,
	connectionhealth.SourceViewer, connectionhealth.SourceCollector,
}

var classes = []connectionhealth.Class{
	connectionhealth.ClassRead, connectionhealth.ClassControl, connectionhealth.ClassStatus,
	connectionhealth.ClassTransport, connectionhealth.ClassProbe,
}

func in[T comparable](x T, set []T) bool {
	for _, y := range set {
		if x == y {
			return true
		}
	}
	return false
}

func knownStage(s connectionhealth.Stage) bool {
	return s == connectionhealth.StageIdle || in(s, connectionhealth.Lifecycle)
}

// validateRecord admits a stamped record of boot (with build on its boot
// record): the same rule on Offer and on every replay, so a file edited or
// forged on disk cannot put anything into an export that Offer would refuse.
// A refusal names the field, never its value.
func validateRecord(r connectionhealth.Record, boot, build string) error {
	bad := func(f string) error { return &Error{Code: CodeInvalidRecord, Field: f} }
	switch {
	case r.Schema != connectionhealth.RecordSchema:
		return bad("schema")
	case r.Seq == 0:
		return bad("producer_seq")
	case r.BootID != boot:
		return bad("boot_id")
	case r.EventID != r.BootID+"-"+strconv.FormatUint(r.Seq, 10):
		return bad("event_id")
	case r.Wall.IsZero() || r.Wall.Year() < 2000 || r.Wall.Year() > 9999 || r.MonoNS < 0:
		return bad("time")
	case !in(r.Event, events):
		return bad("event")
	case r.Rule != "" && r.Rule != connectionhealth.RuleVersion:
		return bad("rule")
	case r.Kind != "" && r.Kind != connectionhealth.Finite && r.Kind != connectionhealth.LongLived:
		return bad("kind")
	case r.Event == connectionhealth.EventBoot && r.Build != build:
		return bad("build")
	case r.Event != connectionhealth.EventBoot && r.Build != "":
		return bad("build")
	case r.Source != "" && !in(r.Source, sources):
		return bad("source")
	case r.Stage != "" && !knownStage(r.Stage):
		return bad("stage")
	case r.Class != "" && !in(r.Class, classes):
		return bad("class")
	case r.Location != "" && !connectionhealth.ValidLocation(r.Location):
		return bad("location")
	case r.Correlation != "" && !connectionhealth.ValidID(r.Correlation):
		return bad("correlation")
	case r.AgeNS < 0 || r.DeadlineNS < 0:
		return bad("duration")
	case r.Event == connectionhealth.EventStateAfterLoss && !in(r.State, restatedStates):
		return bad("state")
	case r.Event != connectionhealth.EventStateAfterLoss && r.State != "":
		return bad("state")
	}
	return nil
}
