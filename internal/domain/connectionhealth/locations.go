package connectionhealth

import "time"

// The deadlines every finite daemon phase is judged by today: the plan's
// proposed 5 s to call it slow and 15 s to call it blocked. Parameters, not
// measured budgets.
const (
	DaemonWarn  = 5 * time.Second
	DaemonBlock = 15 * time.Second
)

// Location codes of the daemon call sites this build instruments. Each one
// is a stable name; Locations says which Go function holds it.
const (
	LocReadLoop        = "cloud.transport.read_loop"
	LocKeepaliveLoop   = "cloud.transport.keepalive_loop"
	LocDrainLoop       = "cloud.transport.drain_loop"
	LocHandleFrame     = "cloud.transport.handle_frame"
	LocSenderLookup    = "cloud.transport.sender_lookup"
	LocSignatureCheck  = "cloud.transport.signature_check"
	LocReplayClaim     = "cloud.transport.replay_claim"
	LocOpenEnvelope    = "cloud.transport.open_envelope"
	LocInboundCallback = "cloud.transport.inbound_callback"
	LocPeerCallback    = "cloud.transport.peer_callback"
	LocSettleReceipt   = "cloud.transport.settle_receipt"
	LocDrainSend       = "cloud.transport.drain_send"
	LocWriterLock      = "cloud.websocket.writer_lock"
	LocSocketWrite     = "cloud.websocket.socket_write"
	LocReadLane        = "cloud.link.read_lane"
	LocCommandLane     = "cloud.link.command_lane"
	LocSessionsLane    = "cloud.link.sessions_lane"
	LocReadHandler     = "cloud.link.read_handler"
	LocCommandHandler  = "cloud.link.command_handler"
	LocSessionsHandler = "cloud.link.sessions_handler"
)

// LocationEntry binds one location code to the function that holds it in
// this build. Function is the Go symbol as runtime.FuncForPC names it, which
// a test in the owning package checks against the real function; there are
// no file lines, because a line would be a guess the moment the file moves.
type LocationEntry struct {
	Code     string  `json:"code"`
	Function string  `json:"function"`
	Stages   []Stage `json:"stages"`
	Kind     Kind    `json:"kind"`
	Critical bool    `json:"critical,omitempty"`
}

// LocationMap is Locations for one build.
type LocationMap struct {
	Build   string          `json:"build_commit"`
	Entries []LocationEntry `json:"entries"`
}

const (
	adapterPkg = "github.com/sainteye/clawdline/internal/adapters/cloud"
	linkPkg    = "github.com/sainteye/clawdline/internal/transport/cloud"
)

// Locations is every location code this build actually wires, in path order.
func Locations() []LocationEntry {
	return []LocationEntry{
		{LocReadLoop, adapterPkg + ".(*Transport).read", []Stage{StageIdle}, LongLived, true},
		{LocKeepaliveLoop, adapterPkg + ".(*Transport).keepalive", []Stage{StageIdle}, LongLived, true},
		{LocDrainLoop, adapterPkg + ".(*Transport).drain", []Stage{StageIdle}, LongLived, true},
		{LocHandleFrame, adapterPkg + ".(*Transport).handle", []Stage{StageFrameRead}, Finite, false},
		{LocSenderLookup, adapterPkg + ".(*Transport).admitTraced", []Stage{StageFrameRead}, Finite, false},
		{LocSignatureCheck, adapterPkg + ".(*Transport).admitTraced", []Stage{StageFrameRead}, Finite, false},
		{LocReplayClaim, adapterPkg + ".(*Transport).admitTraced", []Stage{StageSenderVerified}, Finite, false},
		{LocOpenEnvelope, adapterPkg + ".(*Transport).admitTraced", []Stage{StageSenderVerified}, Finite, false},
		{LocInboundCallback, adapterPkg + ".(*Transport).admitTraced", []Stage{StageLaneQueued}, Finite, false},
		{LocPeerCallback, adapterPkg + ".(*Transport).handleFrame", []Stage{StageLaneQueued}, Finite, false},
		{LocSettleReceipt, adapterPkg + ".(*Transport).handleFrame", []Stage{StageFrameRead}, Finite, false},
		{LocDrainSend, adapterPkg + ".(*Transport).drainPass", []Stage{StageSpoolQueued}, Finite, false},
		{LocWriterLock, adapterPkg + ".(*Conn).writeFrame", []Stage{StageWriterLockWaiting}, Finite, false},
		{LocSocketWrite, adapterPkg + ".(*Conn).writeFrame", []Stage{StageSocketWriteStarted, StageSocketWriteEnded}, Finite, false},
		{LocReadLane, linkPkg + ".(*Link).runService", []Stage{StageLaneQueued}, Finite, false},
		{LocCommandLane, linkPkg + ".(*Link).runService", []Stage{StageLaneQueued}, Finite, false},
		{LocSessionsLane, linkPkg + ".(*Link).runService", []Stage{StageLaneQueued}, Finite, false},
		{LocReadHandler, linkPkg + ".(*Link).answer", []Stage{StageHandlerStarted, StageHandlerCompleted}, Finite, false},
		{LocCommandHandler, linkPkg + ".(*Link).answer", []Stage{StageHandlerStarted, StageHandlerCompleted}, Finite, false},
		{LocSessionsHandler, linkPkg + ".(*Link).answer", []Stage{StageHandlerStarted, StageHandlerCompleted}, Finite, false},
	}
}

// LoopSpec is the critical long-lived phase of one reserved loop.
func LoopSpec(location string) Spec {
	return Spec{Source: SourceDaemon, Stage: StageIdle, Class: ClassTransport, Kind: LongLived, Location: location, Critical: true}
}

// DaemonSpec is a finite daemon phase with the default deadlines.
func DaemonSpec(stage Stage, class Class, location, correlation string) Spec {
	return Spec{Source: SourceDaemon, Stage: stage, Class: class, Kind: Finite, Location: location,
		Correlation: correlation, Warn: DaemonWarn, Block: DaemonBlock}
}
