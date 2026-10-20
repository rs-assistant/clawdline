// Package connectionhealth is the evidence foundation of the connection-health
// product (docs/admin-connection-health.md, contract in
// docs/admin-connection-health-contract.md): which finite operation is in
// which stage right now, how long since it last progressed, what a watchdog
// concluded from that, and a bounded journal of those conclusions that can be
// queried and exported without any payload, path or credential in it.
//
// Three rules shape every type here:
//
//   - Only an actual receipt creates a stage. Nothing is inferred from a
//     socket being open, and a record that is absent is never success.
//   - Unknown is not zero. A reading that could not be taken says so with a
//     typed quality, never with an empty list or a 0.
//   - Instrumentation never waits. The active-phase table is lock-free; the
//     watchdog reads only that table and its own clock and hands transitions
//     to the journal through a bounded queue it never waits on; the journal
//     has its own small lock that no business code holds.
//
// The package keeps no goroutine of its own except Watchdog.Run and the one
// journal writer it starts and joins, whose lifetime is the caller's context.
package connectionhealth

import (
	"errors"
	"fmt"
	"regexp"
)

// Schema names. A reader that sees a schema it does not know reports
// unsupported_schema instead of guessing at the fields.
const (
	RecordSchema   = "clawdline.connection-health.record/1"
	PageSchema     = "clawdline.connection-health.page/1"
	EvidenceSchema = "clawdline.connection-health.evidence/1"
	// RuleVersion names the watchdog rule set that produced a finding.
	RuleVersion = "watchdog/1"
)

// Source is the producer kind that recorded an observation.
type Source string

const (
	SourceDaemon    Source = "daemon"
	SourceCloudAPI  Source = "cloud_api"
	SourceRelay     Source = "relay"
	SourceViewer    Source = "viewer"
	SourceCollector Source = "collector"
)

var sources = []Source{SourceDaemon, SourceCloudAPI, SourceRelay, SourceViewer, SourceCollector}

// Stage is one lifecycle receipt (docs/admin-connection-health.md "Correlated
// signal contract"). The order of Lifecycle is the order a relay-carried
// request passes through them; a direct operation simply has no relay stages.
type Stage string

const (
	StageClientIssued        Stage = "client_issued"
	StageCloudAccepted       Stage = "cloud_accepted"
	StageRelaySocketAccepted Stage = "relay_socket_accepted"
	StageRelayForwarded      Stage = "relay_forwarded"
	StageFrameRead           Stage = "frame_read"
	StageSenderVerified      Stage = "sender_verified"
	StageLaneQueued          Stage = "lane_queued"
	StageHandlerStarted      Stage = "handler_started"
	StageHandlerCompleted    Stage = "handler_completed"
	StageResponseSealed      Stage = "response_sealed"
	StageSpoolQueued         Stage = "spool_queued"
	StageWriterLockWaiting   Stage = "writer_lock_waiting"
	StageSocketWriteStarted  Stage = "socket_write_started"
	StageSocketWriteEnded    Stage = "socket_write_ended"
	StageEndpointReceived    Stage = "endpoint_received"
	StageEndpointDecrypted   Stage = "endpoint_decrypted"
	StageUIApplied           Stage = "ui_applied"
	// StageAcknowledged is a person's own explicit action. A browser applying
	// data (StageUIApplied) is never this.
	StageAcknowledged Stage = "acknowledged"
	// StageIdle is where a long-lived loop sits between pieces of work.
	StageIdle Stage = "idle"
)

// Lifecycle is every stage in path order.
var Lifecycle = []Stage{
	StageClientIssued, StageCloudAccepted, StageRelaySocketAccepted, StageRelayForwarded,
	StageFrameRead, StageSenderVerified, StageLaneQueued, StageHandlerStarted,
	StageHandlerCompleted, StageResponseSealed, StageSpoolQueued, StageWriterLockWaiting,
	StageSocketWriteStarted, StageSocketWriteEnded, StageEndpointReceived,
	StageEndpointDecrypted, StageUIApplied, StageAcknowledged,
}

func knownStage(s Stage) bool {
	if s == StageIdle {
		return true
	}
	for _, x := range Lifecycle {
		if x == s {
			return true
		}
	}
	return false
}

// Class is the finite operation class a phase belongs to. Metric dimensions
// use these, never a route, path or channel name.
type Class string

const (
	ClassRead      Class = "read"
	ClassControl   Class = "control"
	ClassStatus    Class = "status"
	ClassTransport Class = "transport"
	ClassProbe     Class = "probe"
)

var classes = []Class{ClassRead, ClassControl, ClassStatus, ClassTransport, ClassProbe}

func classIndex(c Class) int {
	for i, x := range classes {
		if x == c {
			return i
		}
	}
	return -1
}

// Kind separates a finite operation, which has deadlines and can be stuck,
// from a long-lived loop, which may legitimately wait forever for work and is
// never called blocked for doing so.
type Kind string

const (
	Finite    Kind = "finite"
	LongLived Kind = "long_lived"
)

// Outcome is the operational answer for a segment (design §5).
type Outcome string

const (
	OutcomeHealthy    Outcome = "healthy"
	OutcomeSlow       Outcome = "slow"
	OutcomeBlocked    Outcome = "blocked"
	OutcomeError      Outcome = "error"
	OutcomeOffline    Outcome = "offline"
	OutcomeUnknown    Outcome = "unknown"
	OutcomeNoRequests Outcome = "no_requests"
)

// Quality is how much of the answer the evidence supports, kept apart from
// the outcome so "we cannot see" never reads as "fine".
type Quality string

const (
	QualityComplete     Quality = "complete"
	QualityPartial      Quality = "partial"
	QualityStale        Quality = "stale"
	QualityNotCollected Quality = "not_collected"
	QualityUnsupported  Quality = "unsupported"
	QualityForbidden    Quality = "forbidden"
	QualityNoRequests   Quality = "no_requests"
)

// Code is a stable failure a caller can branch on.
type Code string

const (
	CodeInvalidField        Code = "invalid_field"
	CodeUnsupportedSchema   Code = "unsupported_schema"
	CodeSlotsExhausted      Code = "slots_exhausted"
	CodeRecordTooLarge      Code = "record_too_large"
	CodeQueryBoundExceeded  Code = "query_bound_exceeded"
	CodeCursorOtherBoot     Code = "cursor_other_boot"
	CodeUnsupported         Code = "unsupported"
	CodeForbidden           Code = "forbidden"
	CodeIdempotencyConflict Code = "idempotency_conflict"
)

// Error is every failure this package returns.
type Error struct {
	Code Code
	// Field names the offending field for invalid_field; never its value,
	// which may be the very thing that must not be repeated.
	Field string
}

func (e *Error) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("connectionhealth: %s: %s", e.Code, e.Field)
	}
	return "connectionhealth: " + string(e.Code)
}

// CodeOf is err's code, and "" for an error this package did not make.
func CodeOf(err error) Code {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func invalid(field string) error { return &Error{Code: CodeInvalidField, Field: field} }

// opaqueID is a correlation, boot or build identifier. It admits no slash,
// dot run, space or colon, so a path, a URL or a "Bearer x" header cannot be
// smuggled through one; 64 characters is longer than a commit or a UUID.
var opaqueID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// locationCode is a stable stage location ("cloud.transport.read_frame"):
// lower-case words joined by dots, never a file path.
var locationCode = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z0-9_]+){0,7}$`)

// ValidID reports whether s may be exported as an opaque identifier.
func ValidID(s string) bool { return opaqueID.MatchString(s) }

// ValidLocation reports whether s is a stage location code.
func ValidLocation(s string) bool { return len(s) <= 96 && locationCode.MatchString(s) }

func knownSource(s Source) bool {
	for _, x := range sources {
		if x == s {
			return true
		}
	}
	return false
}
