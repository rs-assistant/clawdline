package healthupload

import (
	"encoding/json"
	"regexp"
	"time"

	"github.com/sainteye/clawdline/internal/domain/connectionhealth"
)

// The Cloud ingest wire (Cloud's own ingest contract,
// §2): one batch of `clawdline.connection-health.record/1` events of one
// boot, sent to POST /v1/health/ingest with the machine's own credential.
const (
	// WireSchemaVersion is the batch envelope's schema_version.
	WireSchemaVersion = 1
	// ProducerInstance names this daemon among a machine's producers.
	ProducerInstance = "daemon"
	// IngestPath is appended to the identity's API base.
	IngestPath = "/v1/health/ingest"
)

// wireEvent is the allowlist: every field the Cloud record/1 parser accepts
// as content-free metadata, and nothing else. Location, age_ns, deadline_ns
// (owner diagnostics) and anything a future Record field adds stay on this
// machine because they have no field here; there is no map, no embedding and
// no copy of the whole Record. State is the watchdog enum and is set only on
// a state_after_loss record, where Cloud requires it and refuses it anywhere
// else (Cloud contract §12).
type wireEvent struct {
	Schema      string `json:"schema"`
	Seq         uint64 `json:"producer_seq"`
	EventID     string `json:"event_id"`
	BootID      string `json:"boot_id"`
	Event       string `json:"event"`
	Wall        string `json:"wall"`
	MonoNS      int64  `json:"mono_ns"`
	Build       string `json:"build_commit,omitempty"`
	Source      string `json:"source,omitempty"`
	Stage       string `json:"stage,omitempty"`
	Class       string `json:"class,omitempty"`
	Kind        string `json:"kind,omitempty"`
	PhaseID     uint64 `json:"phase_id,omitempty"`
	Correlation string `json:"correlation,omitempty"`
	Count       uint64 `json:"count,omitempty"`
	Rule        string `json:"rule,omitempty"`
	State       string `json:"state,omitempty"`
}

// wireBatch is the envelope. sent_at is deliberately absent: it is optional
// on the wire and part of the server's batch digest, so a batch rebuilt from
// the same records after a lost state file would otherwise answer
// 409 idempotency_conflict instead of replaying its receipt. The cost is
// that Cloud reports clock_offset_ms as null for this producer.
type wireBatch struct {
	SchemaVersion    int         `json:"schema_version"`
	BatchID          string      `json:"batch_id"`
	ProducerInstance string      `json:"producer_instance"`
	BootID           string      `json:"boot_id"`
	BuildCommit      string      `json:"build_commit,omitempty"`
	Events           []wireEvent `json:"events"`
	// LocalLoss is the progress file's settled loss totals (Cloud contract
	// §12.1), set only on a metadata-only batch. It is written into the
	// body once, when the batch is built, and never refreshed on a retry:
	// the body is part of the batch digest. Absent, not null, elsewhere.
	LocalLoss *wireLocalLoss `json:"local_loss,omitempty"`
}

// LocalLossSchema is §12.1's block schema.
const LocalLossSchema = "clawdline.connection-health.local-loss/1"

// wireLocalLoss is §12.1's block: totals of one counter epoch (the life of
// one progress file), never per batch, boot or sequence. Cloud keeps the
// maximum per key within an epoch, so the same totals on every batch never
// add up. It names no sequence and no cause beyond the coarse reason.
type wireLocalLoss struct {
	Schema       string            `json:"schema"`
	CounterEpoch string            `json:"counter_epoch"`
	ObservedAt   string            `json:"observed_at"`
	StorageLost  uint64            `json:"storage_lost_records"`
	StoreGaps    map[string]uint64 `json:"store_gaps"`
	Excluded     map[string]uint64 `json:"excluded"`
}

// UnlistedReason is §12.1's one bucket for an exclusion reason outside
// Cloud's allowlist; a free-text reason is never sent.
const UnlistedReason = "unlisted"

// cloudGapReasons and cloudExcludedReasons are §12.1's allowlists, exactly.
// TestTheLocalLossAllowlistsMatchTheGoReasons fails when a Go reason is
// added without a decision here.
var cloudGapReasons = map[string]bool{
	"pruned": true, "missing_segment": true, "corrupt_frames": true, "unreadable_tail": true, "invalid_cursor": true,
}

var cloudExcludedReasons = map[string]bool{
	ExcludedState: true, ExcludedEvent: true, ExcludedStage: true, ExcludedSource: true,
	ExcludedUnsafeInteger: true, ExcludedPattern: true, ExcludedTooLarge: true, UnlistedReason: true,
}

// localLoss builds the block from totals, or nil when it cannot be stated
// truthfully: no counter epoch yet, a value past Cloud's safe integer, or a
// gap reason Cloud does not list (an omitted key would read as 0, which is
// not known). Unknown is not zero.
func localLoss(epoch string, at time.Time, c lossCounters) *wireLocalLoss {
	if !cloudOpaque.MatchString(epoch) || c.StorageLost > jsSafeInteger {
		return nil
	}
	b := &wireLocalLoss{Schema: LocalLossSchema, CounterEpoch: epoch, ObservedAt: at.UTC().Format(time.RFC3339),
		StorageLost: c.StorageLost, StoreGaps: map[string]uint64{}, Excluded: map[string]uint64{}}
	for k, v := range c.Gaps {
		if !cloudGapReasons[k] || v > jsSafeInteger {
			return nil
		}
		b.StoreGaps[k] = v
	}
	for k, v := range c.Excluded {
		if !cloudExcludedReasons[k] {
			k = UnlistedReason
		}
		b.Excluded[k] += v
		if b.Excluded[k] > jsSafeInteger {
			return nil
		}
	}
	return b
}

// Exclusion reasons: a stored record this uploader will not send, counted by
// reason so its absence on Cloud is explained here rather than silent.
const (
	// ExcludedState: a state_after_loss record whose state is not one of
	// Cloud's watchdog states (slow, blocked, unverified_after_gap).
	ExcludedState = "state_unsupported_by_cloud"
	// ExcludedEvent: an event Cloud record/1 does not list.
	ExcludedEvent = "event_unsupported_by_cloud"
	// ExcludedStage: a stage Cloud record/1 does not list.
	ExcludedStage = "stage_unsupported_by_cloud"
	// ExcludedSource: a machine credential may send only source daemon.
	ExcludedSource = "source_not_daemon"
	// ExcludedUnsafeInteger: mono_ns above 2^53-1 (about 104 days of
	// uptime) is refused by Cloud rather than rounded.
	ExcludedUnsafeInteger = "mono_ns_exceeds_safe_integer"
	// ExcludedPattern: an id or rule that fails Cloud's pattern.
	ExcludedPattern = "field_fails_cloud_pattern"
	// ExcludedTooLarge: one event alone would exceed the batch bound.
	ExcludedTooLarge = "event_exceeds_batch_bytes"
)

// cloudEvents and cloudStages are Cloud record/1's lists
// (api/src/domain/connectionHealth.ts HEALTH_EVENTS, HEALTH_STAGES). A Go
// event or stage missing here is excluded and counted, never renamed;
// TestEveryGoEnumIsReconciledWithCloud fails when the Go lists grow, so a
// new value is a decision, not a silent drop.
var cloudEvents = map[connectionhealth.Event]bool{
	connectionhealth.EventBoot: true, connectionhealth.EventSlowDetected: true,
	connectionhealth.EventStallDetected: true, connectionhealth.EventRecovered: true,
	connectionhealth.EventReleasedStalled: true, connectionhealth.EventOverflow: true,
	connectionhealth.EventSchedulingGap: true, connectionhealth.EventClockAnomaly: true,
	connectionhealth.EventTransitionsLost: true, connectionhealth.EventStateAfterLoss: true,
}

// excludedEvents are Go events deliberately not uploaded, with the reason.
// None today; a Go event Cloud refuses goes here rather than being renamed.
var excludedEvents = map[connectionhealth.Event]string{}

// cloudStates is Cloud's HEALTH_WATCHDOG_STATES: the only values a
// state_after_loss record may carry.
var cloudStates = map[connectionhealth.State]bool{
	connectionhealth.StateSlow: true, connectionhealth.StateBlocked: true, connectionhealth.StateUnverified: true,
}

var cloudStages = func() map[connectionhealth.Stage]bool {
	m := map[connectionhealth.Stage]bool{connectionhealth.StageIdle: true}
	for _, s := range []string{
		"client_issued", "cloud_accepted", "relay_socket_accepted", "relay_forwarded", "frame_read", "sender_verified",
		"lane_queued", "handler_started", "handler_completed", "response_sealed", "spool_queued", "writer_lock_waiting",
		"socket_write_started", "socket_write_ended", "endpoint_received", "endpoint_decrypted", "ui_applied", "acknowledged",
	} {
		m[connectionhealth.Stage(s)] = true
	}
	return m
}()

var (
	cloudOpaque = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	cloudRule   = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}/[0-9]{1,4}$`)
	cloudEvent  = regexp.MustCompile(`^[A-Za-z0-9_-]{1,96}$`)
)

// jsSafeInteger is JavaScript's Number.MAX_SAFE_INTEGER.
const jsSafeInteger = 1<<53 - 1

// toWire copies the allowlisted fields of a stamped record, or names why it
// cannot be sent. It never alters a value to make it fit.
func toWire(r connectionhealth.Record) (wireEvent, string) {
	if reason, ok := excludedEvents[r.Event]; ok {
		return wireEvent{}, reason
	}
	switch {
	case !cloudEvents[r.Event]:
		return wireEvent{}, ExcludedEvent
	case r.Stage != "" && !cloudStages[r.Stage]:
		return wireEvent{}, ExcludedStage
	case r.Source != "" && r.Source != connectionhealth.SourceDaemon:
		return wireEvent{}, ExcludedSource
	case r.Event == connectionhealth.EventStateAfterLoss && !cloudStates[r.State]:
		return wireEvent{}, ExcludedState
	case r.MonoNS < 0 || r.MonoNS > jsSafeInteger || r.Seq > jsSafeInteger ||
		r.PhaseID > jsSafeInteger || r.Count > jsSafeInteger:
		return wireEvent{}, ExcludedUnsafeInteger
	case r.Seq < 1 || !cloudOpaque.MatchString(r.BootID) || !cloudEvent.MatchString(r.EventID),
		r.Build != "" && !cloudOpaque.MatchString(r.Build),
		r.Correlation != "" && !cloudOpaque.MatchString(r.Correlation),
		r.Rule != "" && !cloudRule.MatchString(r.Rule):
		return wireEvent{}, ExcludedPattern
	}
	w := wireEvent{
		Schema: connectionhealth.RecordSchema, Seq: r.Seq, EventID: r.EventID, BootID: r.BootID,
		Event: string(r.Event), Wall: r.Wall.UTC().Format(time.RFC3339Nano), MonoNS: r.MonoNS,
		Build: r.Build, Source: string(r.Source), Stage: string(r.Stage), Class: string(r.Class),
		Kind: string(r.Kind), PhaseID: r.PhaseID, Correlation: r.Correlation, Count: r.Count, Rule: r.Rule,
	}
	if r.Event == connectionhealth.EventStateAfterLoss {
		w.State = string(r.State)
	}
	return w, ""
}

// encode is the exact body sent, and resent byte for byte on a retry.
func encode(b wireBatch) ([]byte, error) { return json.Marshal(b) }

// receipt is Cloud's answer to a committed batch (§2).
type receipt struct {
	SchemaVersion int    `json:"schema_version"`
	ReceiptID     string `json:"receipt_id"`
	BatchID       string `json:"batch_id"`
	Digest        string `json:"digest"`
	PersistedAt   string `json:"persisted_at"`
	RetainUntil   string `json:"retain_until"`
	Accepted      *int   `json:"accepted"`
	Rejected      []struct {
		Index   int    `json:"index"`
		EventID string `json:"event_id"`
		Code    string `json:"code"`
	} `json:"rejected"`
}

// refusal is Cloud's error envelope (`{error: {code, message, details}}`,
// api/src/server.ts); only the code and the field name are kept, never the
// message, which may quote a value.
type refusal struct {
	Error struct {
		Code    string `json:"code"`
		Details struct {
			Field string `json:"field"`
		} `json:"details"`
	} `json:"error"`
}
