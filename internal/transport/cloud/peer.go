package cloud

// Peer frames have a separate trust root from viewer envelopes. Only a
// locally pinned, doubly signed machine pair may establish the principal.

import (
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/peerstore"
	peerapp "github.com/sainteye/clawdline/internal/app/agenthandoff"
	peercontract "github.com/sainteye/clawdline/internal/domain/agenthandoff"
)

const (
	PeerIngressLimit          = 16
	PeerAckIngressLimit       = 16
	PeerTaskStartIngressLimit = 16
	PeerProbeIngressLimit     = 16
)

// PeerIngressUsage measures the fuller of the work and relay-ack queues.
// Each has the same registered capacity and both refuse new frames when full.
func (l *Link) PeerIngressUsage() (int, bool) {
	l.mu.Lock()
	requests, acks, starts, probes := l.peerRequests, l.peerAcks, l.peerTaskStarts, l.peerTaskProbes
	pending := len(l.peerDiscovery)
	l.mu.Unlock()
	if requests == nil || acks == nil || starts == nil || probes == nil {
		return 0, false
	}
	return max(len(requests), len(acks), len(starts), len(probes), pending), true
}

type pendingTaskProof struct {
	challenge peercontract.TaskProbe
	reply     chan peercontract.TaskProbe
}

type peerFrame struct {
	Type            string `json:"type"`
	SourcePublicKey string `json:"source_public_key,omitempty"`
	peercontract.Envelope
}

type peerTaskStartFrame struct {
	Type            string `json:"type"`
	SourcePublicKey string `json:"source_public_key,omitempty"`
	peercontract.TaskStartEnvelope
}

type peerTaskProbeFrame struct {
	Type            string                 `json:"type"`
	Probe           peercontract.TaskProbe `json:"probe"`
	SignerPublicKey string                 `json:"signer_public_key,omitempty"`
	SourcePublicKey string                 `json:"source_public_key"`
}

type peerTaskEventFrame struct {
	Type            string `json:"type"`
	SignerPublicKey string `json:"signer_public_key,omitempty"`
	SourcePublicKey string `json:"source_public_key"`
	peercontract.TaskEventEnvelope
}

type peerAck struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	Status    string `json:"status"`
}

func (l *Link) enqueuePeer(frame []byte) {
	if len(frame) > adaptercloud.PeerFrameBytesLimit {
		return
	}
	var header struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(frame, &header) != nil {
		return
	}
	l.mu.Lock()
	queue := l.peerRequests
	switch header.Type {
	case "peer_task_start_envelope":
		queue = l.peerTaskStarts
	case "peer_task_probe_envelope":
		queue = l.peerTaskProbes
	}
	l.mu.Unlock()
	if queue == nil {
		return
	}
	copyFrame := append([]byte(nil), frame...)
	select {
	case queue <- copyFrame:
	default:
		l.logf("cloud peer: inbound queue full; frame outcome unknown")
	}
}

func (l *Link) enqueuePeerAck(frame []byte) {
	if len(frame) > adaptercloud.PeerFrameBytesLimit || l.peerAcks == nil {
		return
	}
	select {
	case l.peerAcks <- append([]byte(nil), frame...):
	default:
		l.logf("cloud peer: ack queue full; relay delivery outcome unknown")
	}
}

func (l *Link) runPeer(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case frame := <-l.peerRequests:
			requestCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			var header struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(frame, &header) == nil && header.Type == "peer_task_event_envelope" {
				l.receivePeerTaskEvent(requestCtx, frame)
			} else if header.Type == "peer_discovery_envelope" {
				l.receiveDiscovery(requestCtx, frame)
			} else {
				l.receivePeer(requestCtx, frame)
			}
			cancel()
		case frame := <-l.peerAcks:
			l.recordPeerAck(ctx, frame)
		}
	}
}

func (l *Link) runTaskStarts(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case frame := <-l.peerTaskStarts:
			requestCtx, cancel := context.WithTimeout(ctx, 180*time.Second)
			l.receivePeerTaskStart(requestCtx, frame)
			cancel()
		}
	}
}

func (l *Link) runTaskProbes(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case frame := <-l.peerTaskProbes:
			requestCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
			l.receivePeerTaskProbe(requestCtx, frame)
			cancel()
		}
	}
}

// taskNoticeEvery is how often the notice loop looks for task finishes and
// messages owed to this machine's Sessions; openPeerStore is how it opens the
// peer store. Both are variables for the tests.
var (
	taskNoticeEvery = 10 * time.Second
	openPeerStore   = peerstore.Open
)

func (l *Link) runTaskNotices(ctx context.Context) {
	if l.opts.PeerTaskNotice == nil || l.keys == nil {
		return
	}
	// One store for the loop's life, opened by its first pass that can and
	// closed when the line stops: before, each pass opened and closed it
	// twice — a MkdirAll, an OpenFile and a dozen CREATE … IF NOT EXISTS —
	// every ten seconds whether anything was owed or not.
	var st *peerstore.Store
	defer func() {
		if st != nil {
			_ = st.Close()
		}
	}()
	ticker := time.NewTicker(taskNoticeEvery)
	defer ticker.Stop()
	for {
		if st == nil {
			if opened, err := openPeerStore(l.keys.Dir()); err == nil {
				st = opened
			}
		}
		if st != nil {
			l.deliverTaskNotices(ctx, st)
			l.deliverTaskMessages(ctx, st)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (l *Link) deliverTaskNotices(ctx context.Context, st *peerstore.Store) {
	if ctx.Err() != nil || l.opts.PeerTaskNotice == nil {
		return
	}
	rows, err := st.PendingTaskFinishes(ctx)
	if err != nil {
		return
	}
	for i, row := range rows {
		if i >= PeerIngressLimit || ctx.Err() != nil {
			return
		}
		if row.Event.Start.Source.MachineID != l.identity.MachineID || !l.taskNoticeAuthorized(ctx, st, row.Event.Start) {
			continue
		}
		noticeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		err := l.opts.PeerTaskNotice(noticeCtx, row.Event.Start)
		cancel()
		if err == nil {
			if err := st.MarkTaskFinishNotified(ctx, row.Event); err != nil {
				l.logf("cloud peer task notice: could not record task %s: %v", row.Event.Start.TaskID, err)
			}
		}
	}
}

func (l *Link) deliverTaskMessages(ctx context.Context, st *peerstore.Store) {
	if ctx.Err() != nil || l.opts.PeerTaskMessageNotice == nil {
		return
	}
	rows, err := st.PendingTaskMessages(ctx)
	if err != nil {
		return
	}
	for i, row := range rows {
		if i >= PeerIngressLimit || ctx.Err() != nil {
			return
		}
		if row.Event.Recipient.MachineID != l.identity.MachineID || !l.taskNoticeAuthorized(ctx, st, row.Event.Start) {
			continue
		}
		noticeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		err := l.opts.PeerTaskMessageNotice(noticeCtx, row.Event)
		cancel()
		if err == nil {
			if err := st.MarkTaskMessageNotified(ctx, row.Event); err != nil {
				l.logf("cloud peer task message: notice remains pending: %v", err)
			}
		}
	}
}

func (l *Link) taskNoticeAuthorized(ctx context.Context, st *peerstore.Store, start peercontract.TaskStart) bool {
	pair, err := st.Pair(ctx, start.PairID)
	if err != nil || pair.TargetSignature == "" || !pair.RevokedAt.IsZero() || !l.opts.Now().Before(pair.ExpiresAt) {
		return false
	}
	auth, err := adaptercloud.NewAccountClient(l.settings.APIBase).AuthorizePeerTaskStart(ctx, l.identity.MachineCredential, start, pair.SourcePublicKey)
	return err == nil && auth.Authorized && auth.PairID == pair.ID && auth.SourcePublicKey == pair.SourcePublicKey && auth.TargetMachineID == pair.TargetMachineID
}

func (l *Link) recordPeerAck(ctx context.Context, raw []byte) {
	l.recordDiscoveryStatus(raw)
	var ack peerAck
	if json.Unmarshal(raw, &ack) != nil || ack.RequestID == "" ||
		(ack.Status != "delivered" && ack.Status != "machine_offline" && ack.Status != "unknown") {
		return
	}
	st, err := peerstore.Open(l.keys.Dir())
	if err != nil {
		return
	}
	defer st.Close()
	switch ack.Type {
	case "peer_ack":
		if err := st.MarkOutbox(ctx, ack.RequestID, "accepted", ack.Status); err != nil {
			l.logf("cloud peer: could not record ack for %s: %v", ack.RequestID, err)
		}
	case "peer_task_start_ack":
		item, err := st.TaskStartOutboxByRequest(ctx, ack.RequestID)
		if err != nil {
			l.logf("cloud peer task start: could not find ack request %s: %v", ack.RequestID, err)
			return
		}
		if err := st.MarkTaskStartOutbox(ctx, item.Start.TaskID, ack.Status); err != nil {
			l.logf("cloud peer task start: could not record ack for %s: %v", ack.RequestID, err)
		}
	case "peer_task_event_ack":
		record, err := st.TaskEventByRequest(ctx, ack.RequestID)
		if err != nil || record.Direction != "out" {
			return
		}
		if err := st.MarkTaskEventDelivered(ctx, record.Event, ack.Status); err != nil {
			l.logf("cloud peer task event: could not record ack for %s: %v", ack.RequestID, err)
		}
	}
}

// TaskStartSubmission is source-side evidence only. A socket write cannot
// establish relay acceptance or target execution.
type TaskStartSubmission struct {
	TaskID           string `json:"task_id"`
	RequestID        string `json:"request_id"`
	RelayAccepted    string `json:"relay_accepted"`
	RelayDelivered   string `json:"relay_delivered"`
	MachineExecution string `json:"machine_execution"`
}

func (l *Link) PublishTaskStart(ctx context.Context, start peercontract.TaskStart, body []byte) (TaskStartSubmission, error) {
	answer := TaskStartSubmission{TaskID: start.TaskID, RequestID: start.RequestID,
		RelayAccepted: "unknown", RelayDelivered: "unknown", MachineExecution: "unknown"}
	if l.transport == nil || l.keys == nil || start.Source.MachineID != l.identity.MachineID ||
		l.opts.PeerAdmitTarget == nil || !l.allowCommands() {
		return answer, errors.New("peer_task_start_unavailable")
	}
	if err := peercontract.ValidateTaskStart(start, body); err != nil {
		return answer, err
	}
	if err := l.opts.PeerAdmitTarget(ctx, start.Source.MachineID,
		start.Source.SessionID, start.Source.ExecutionGeneration); err != nil {
		return answer, err
	}
	st, err := peerstore.Open(l.keys.Dir())
	if err != nil {
		return answer, err
	}
	defer st.Close()
	if prior, err := st.TaskStartOutbox(ctx, start.TaskID); err == nil {
		if prior.Start != start {
			return answer, errors.New("peer_task_start_outbox_conflict")
		}
		return l.RetryTaskStart(ctx, start.TaskID)
	} else if !errors.Is(err, peerstore.ErrNotFound) {
		return answer, err
	}
	pair, err := st.Pair(ctx, start.PairID)
	if err != nil || pair.SourceMachineID != l.identity.MachineID ||
		pair.TargetMachineID != start.TargetMachineID || pair.TargetSignature == "" ||
		!pair.RevokedAt.IsZero() || !l.opts.Now().Before(pair.ExpiresAt) {
		return answer, errors.New("peer_pair_inactive")
	}
	key, found, err := l.keys.DeviceKey()
	if err != nil || !found || !key.Valid() || key.Fingerprint() != pair.SourceFingerprint {
		return answer, errors.New("peer_signer_changed")
	}
	auth, err := adaptercloud.NewAccountClient(l.settings.APIBase).AuthorizePeerTaskStart(ctx,
		l.identity.MachineCredential, start, pair.SourcePublicKey)
	if err != nil || !auth.Authorized || auth.SourcePublicKey != pair.SourcePublicKey ||
		auth.SourceKeyFingerprint != pair.SourceFingerprint ||
		auth.TargetMachineID != start.TargetMachineID || auth.PairID != start.PairID {
		return answer, errors.New("peer_authority_unavailable")
	}
	private, err := ecdh.X25519().NewPrivateKey(pair.LocalPrivateKey)
	if err != nil {
		return answer, err
	}
	envelope, err := peercontract.SealTaskStart(start, body, pairKeys(pair), private,
		ed25519.NewKeyFromSeed(key.Seed()))
	if err != nil {
		return answer, err
	}
	frame, err := json.Marshal(struct {
		Type string `json:"type"`
		peercontract.TaskStartEnvelope
	}{Type: "peer_task_start_publish", TaskStartEnvelope: envelope})
	if err != nil {
		return answer, err
	}
	if err := st.SaveTaskStartOutbox(ctx, peerstore.TaskStartOutboxItem{Start: start,
		Frame: frame, RelayAccepted: "unknown", RelayDelivered: "unknown", At: l.opts.Now()}); err != nil {
		return answer, err
	}
	if err := l.transport.PublishPeerTaskStart(frame); err != nil {
		return answer, err
	}
	return answer, nil
}

// RetryTaskStart resends the same signed ciphertext after checking the source
// execution and the pair again. It never creates a second task or request ID.
func (l *Link) RetryTaskStart(ctx context.Context, taskID string) (TaskStartSubmission, error) {
	answer := TaskStartSubmission{TaskID: taskID, RelayAccepted: "unknown",
		RelayDelivered: "unknown", MachineExecution: "unknown"}
	if l.transport == nil || l.keys == nil || l.opts.PeerAdmitTarget == nil || !l.allowCommands() {
		return answer, errors.New("peer_task_start_unavailable")
	}
	st, err := peerstore.Open(l.keys.Dir())
	if err != nil {
		return answer, err
	}
	defer st.Close()
	item, err := st.TaskStartOutbox(ctx, taskID)
	if err != nil {
		return answer, err
	}
	start := item.Start
	answer.RequestID = start.RequestID
	answer.RelayAccepted, answer.RelayDelivered = item.RelayAccepted, item.RelayDelivered
	if len(item.Frame) == 0 || start.Source.MachineID != l.identity.MachineID {
		return answer, errors.New("peer_task_start_frame_unavailable")
	}
	if err := l.opts.PeerAdmitTarget(ctx, start.Source.MachineID,
		start.Source.SessionID, start.Source.ExecutionGeneration); err != nil {
		return answer, err
	}
	pair, err := st.Pair(ctx, start.PairID)
	if err != nil || pair.SourceMachineID != l.identity.MachineID ||
		pair.TargetMachineID != start.TargetMachineID || pair.TargetSignature == "" ||
		!pair.RevokedAt.IsZero() || !l.opts.Now().Before(pair.ExpiresAt) {
		return answer, errors.New("peer_pair_inactive")
	}
	key, found, err := l.keys.DeviceKey()
	if err != nil || !found || !key.Valid() || key.Fingerprint() != pair.SourceFingerprint {
		return answer, errors.New("peer_signer_changed")
	}
	auth, err := adaptercloud.NewAccountClient(l.settings.APIBase).AuthorizePeerTaskStart(ctx,
		l.identity.MachineCredential, start, pair.SourcePublicKey)
	if err != nil || !auth.Authorized || auth.SourcePublicKey != pair.SourcePublicKey ||
		auth.SourceKeyFingerprint != pair.SourceFingerprint ||
		auth.TargetMachineID != start.TargetMachineID || auth.PairID != start.PairID {
		return answer, errors.New("peer_authority_unavailable")
	}
	return answer, l.transport.PublishPeerTaskStart(item.Frame)
}

// PublishTaskEvent stores the exact sealed frame before writing to the relay.
// The sender's local Session and task ownership are rechecked at every send.
func (l *Link) PublishTaskEvent(ctx context.Context, event peercontract.TaskEvent,
	body []byte) error {
	if l.transport == nil || l.keys == nil || l.opts.PeerAdmitTarget == nil ||
		!l.allowCommands() || event.Sender.MachineID != l.identity.MachineID {
		return errors.New("peer_task_event_unavailable")
	}
	if err := peercontract.ValidateTaskEvent(event, body); err != nil {
		return err
	}
	if err := l.opts.PeerAdmitTarget(ctx, event.Sender.MachineID,
		event.Sender.SessionID, event.Sender.ExecutionGeneration); err != nil {
		return err
	}
	st, err := peerstore.Open(l.keys.Dir())
	if err != nil {
		return err
	}
	defer st.Close()
	if prior, err := st.TaskEventByRequest(ctx, event.RequestID); err == nil {
		if prior.Direction != "out" || prior.Event != event {
			return errors.New("peer_task_event_request_conflict")
		}
		return l.RetryTaskEvent(ctx, event.RequestID)
	} else if !errors.Is(err, peerstore.ErrNotFound) {
		return err
	}
	pair, err := st.Pair(ctx, event.Start.PairID)
	if err != nil || pair.SourceMachineID != event.Start.Source.MachineID ||
		pair.TargetMachineID != event.Start.TargetMachineID || pair.TargetSignature == "" ||
		!pair.RevokedAt.IsZero() || !l.opts.Now().Before(pair.ExpiresAt) {
		return errors.New("peer_pair_inactive")
	}
	signerFingerprint := pair.TargetFingerprint
	if l.identity.MachineID == pair.SourceMachineID {
		signerFingerprint = pair.SourceFingerprint
		start, err := st.TaskStartOutbox(ctx, event.Start.TaskID)
		if err != nil || start.Start != event.Start || start.TargetSessionID != event.Recipient.SessionID ||
			start.TargetGeneration != event.Recipient.ExecutionGeneration {
			return errors.New("peer_task_target_unverified")
		}
	} else {
		task, err := st.RemoteTask(ctx, event.Start.TaskID)
		stateValid := task.State == "opened" && event.Kind != peercontract.TaskEventFinish
		if event.Kind == peercontract.TaskEventFinish {
			stateValid = task.State == "finished" && task.Result == string(body)
		}
		if err != nil || task.Start != event.Start || !stateValid ||
			task.TargetSessionID != event.Sender.SessionID ||
			task.TargetGeneration != event.Sender.ExecutionGeneration {
			return errors.New("peer_task_source_unverified")
		}
	}
	key, found, err := l.keys.DeviceKey()
	if err != nil || !found || !key.Valid() || key.Fingerprint() != signerFingerprint {
		return errors.New("peer_signer_changed")
	}
	auth, err := adaptercloud.NewAccountClient(l.settings.APIBase).AuthorizePeerTaskStart(ctx,
		l.identity.MachineCredential, event.Start, pair.SourcePublicKey)
	if err != nil || !auth.Authorized || auth.PairID != pair.ID ||
		auth.SourcePublicKey != pair.SourcePublicKey || auth.TargetMachineID != pair.TargetMachineID {
		return errors.New("peer_authority_unavailable")
	}
	private, err := ecdh.X25519().NewPrivateKey(pair.LocalPrivateKey)
	if err != nil {
		return err
	}
	envelope, err := peercontract.SealTaskEvent(event, body, pairKeys(pair), private,
		ed25519.NewKeyFromSeed(key.Seed()))
	if err != nil {
		return err
	}
	frame, err := json.Marshal(struct {
		Type            string `json:"type"`
		SourcePublicKey string `json:"source_public_key"`
		peercontract.TaskEventEnvelope
	}{Type: "peer_task_event_publish", SourcePublicKey: pair.SourcePublicKey,
		TaskEventEnvelope: envelope})
	if err != nil {
		return err
	}
	if err := st.SaveTaskEvent(ctx, peerstore.TaskEventRecord{Event: event, Direction: "out",
		Frame: frame, RelayDelivered: "unknown", At: l.opts.Now()}); err != nil {
		return err
	}
	return l.transport.PublishPeerTaskEvent(frame)
}

func (l *Link) RetryTaskEvent(ctx context.Context, requestID string) error {
	if l.transport == nil || l.keys == nil || l.opts.PeerAdmitTarget == nil || !l.allowCommands() {
		return errors.New("peer_task_event_unavailable")
	}
	st, err := peerstore.Open(l.keys.Dir())
	if err != nil {
		return err
	}
	defer st.Close()
	record, err := st.TaskEventByRequest(ctx, requestID)
	if err != nil || record.Direction != "out" || len(record.Frame) == 0 ||
		record.Event.Sender.MachineID != l.identity.MachineID {
		return errors.New("peer_task_event_outbox_missing")
	}
	event := record.Event
	if err := l.opts.PeerAdmitTarget(ctx, event.Sender.MachineID,
		event.Sender.SessionID, event.Sender.ExecutionGeneration); err != nil {
		return err
	}
	pair, err := st.Pair(ctx, event.Start.PairID)
	if err != nil || pair.SourceMachineID != event.Start.Source.MachineID ||
		pair.TargetMachineID != event.Start.TargetMachineID || pair.TargetSignature == "" ||
		!pair.RevokedAt.IsZero() || !l.opts.Now().Before(pair.ExpiresAt) {
		return errors.New("peer_pair_inactive")
	}
	signerFingerprint := pair.TargetFingerprint
	if l.identity.MachineID == pair.SourceMachineID {
		signerFingerprint = pair.SourceFingerprint
	}
	key, found, err := l.keys.DeviceKey()
	if err != nil || !found || !key.Valid() || key.Fingerprint() != signerFingerprint {
		return errors.New("peer_signer_changed")
	}
	auth, err := adaptercloud.NewAccountClient(l.settings.APIBase).AuthorizePeerTaskStart(ctx,
		l.identity.MachineCredential, event.Start, pair.SourcePublicKey)
	if err != nil || !auth.Authorized || auth.PairID != pair.ID ||
		auth.SourcePublicKey != pair.SourcePublicKey || auth.TargetMachineID != pair.TargetMachineID {
		return errors.New("peer_authority_unavailable")
	}
	return l.transport.PublishPeerTaskEvent(record.Frame)
}

func (l *Link) receivePeerTaskStart(ctx context.Context, raw []byte) {
	if l.keys == nil || l.opts.PeerReceipts == nil || l.opts.PeerTaskStartOpen == nil {
		return
	}
	var frame peerTaskStartFrame
	if json.Unmarshal(raw, &frame) != nil || frame.Type != "peer_task_start_envelope" ||
		frame.Start.TargetMachineID != l.identity.MachineID {
		return
	}
	st, err := peerstore.Open(l.keys.Dir())
	if err != nil {
		return
	}
	defer st.Close()
	pair, err := st.Pair(ctx, frame.Start.PairID)
	if err != nil || pair.TargetMachineID != l.identity.MachineID ||
		pair.SourceMachineID != frame.Start.Source.MachineID || pair.TargetSignature == "" ||
		!pair.RevokedAt.IsZero() || !l.opts.Now().Before(pair.ExpiresAt) {
		return
	}
	signer, err := base64.StdEncoding.Strict().DecodeString(pair.SourcePublicKey)
	if err != nil || len(signer) != ed25519.PublicKeySize {
		return
	}
	private, err := ecdh.X25519().NewPrivateKey(pair.LocalPrivateKey)
	if err != nil {
		return
	}
	body, err := peercontract.OpenTaskStart(frame.TaskStartEnvelope, pairKeys(pair), private, signer)
	if err != nil {
		return
	}
	key, found, err := l.keys.DeviceKey()
	if err != nil || !found || !key.Valid() || key.Fingerprint() != pair.TargetFingerprint {
		return
	}
	challenge, err := peercontract.NewTaskChallenge(frame.Start, ed25519.NewKeyFromSeed(key.Seed()))
	if err != nil {
		return
	}
	reply := make(chan peercontract.TaskProbe, 1)
	l.mu.Lock()
	if len(l.peerTaskPending) >= PeerTaskStartIngressLimit {
		l.mu.Unlock()
		l.logf("cloud peer task start: source proof capacity reached")
		return
	}
	if _, exists := l.peerTaskPending[frame.Start.TaskID]; exists {
		l.mu.Unlock()
		return
	}
	l.peerTaskPending[frame.Start.TaskID] = pendingTaskProof{challenge: challenge, reply: reply}
	l.mu.Unlock()
	defer func() {
		l.mu.Lock()
		delete(l.peerTaskPending, frame.Start.TaskID)
		l.mu.Unlock()
	}()
	probeFrame, err := json.Marshal(struct {
		Type            string                 `json:"type"`
		Probe           peercontract.TaskProbe `json:"probe"`
		SourcePublicKey string                 `json:"source_public_key"`
	}{Type: "peer_task_probe_publish", Probe: challenge, SourcePublicKey: pair.SourcePublicKey})
	if err != nil || l.transport.PublishPeerTaskProbe(probeFrame) != nil {
		return
	}
	select {
	case <-ctx.Done():
		return
	case <-time.After(8 * time.Second):
		l.logf("cloud peer task start: source execution proof unavailable for task %s", frame.Start.TaskID)
		return
	case <-reply:
	}
	principal := peercontract.Principal{MachineID: pair.SourceMachineID,
		KeyFingerprint: pair.SourceFingerprint, PairID: pair.ID, MachinePeer: true}
	facts := func(ctx context.Context, start peercontract.TaskStart,
		principal peercontract.Principal) (peercontract.TaskStartFacts, error) {
		answer, err := l.taskStartFacts(ctx, start, principal)
		answer.SourceCurrent = err == nil
		return answer, err
	}
	if l.opts.PeerTaskStartFacts != nil {
		facts = l.opts.PeerTaskStartFacts
	}
	starter := peerapp.TaskStarter{Receipts: l.opts.PeerReceipts, Facts: facts,
		Open: l.opts.PeerTaskStartOpen, Now: l.opts.Now}
	result := starter.Start(ctx, frame.Start, body, principal)
	l.logf("cloud peer task start: task %s machine execution %s code %s",
		result.TaskID, result.MachineExecution, result.Code)
	if result.Code == "ok" && result.TargetSessionID != "" && result.TargetGeneration != "" {
		openedBody := []byte(`{"state":"opened"}`)
		sum := sha256.Sum256(openedBody)
		event := peercontract.TaskEvent{Start: frame.Start, RequestID: "opened-" + frame.Start.TaskID,
			Kind: peercontract.TaskEventOpened,
			Sender: peercontract.Endpoint{MachineID: l.identity.MachineID,
				SessionID: result.TargetSessionID, ExecutionGeneration: result.TargetGeneration},
			Recipient: frame.Start.Source, BodyDigest: hex.EncodeToString(sum[:])}
		if err := l.PublishTaskEvent(ctx, event, openedBody); err != nil {
			l.logf("cloud peer task start: opened event for task %s remains pending: %v", frame.Start.TaskID, err)
		}
	}
}

// taskStartFacts never substitutes a signed source request for a fresh source
// execution proof. Until the source challenge rail is available, admission
// fails at SourceCurrent even when the pair and Project are valid.
func (l *Link) taskStartFacts(ctx context.Context, start peercontract.TaskStart,
	principal peercontract.Principal) (peercontract.TaskStartFacts, error) {
	facts := peercontract.TaskStartFacts{LocalMachineID: l.identity.MachineID,
		Principal: principal, MachineWritesAllowed: l.allowCommands(), Now: l.opts.Now()}
	if l.keys == nil {
		return facts, errors.New("peer_task_start_unavailable")
	}
	st, err := peerstore.Open(l.keys.Dir())
	if err != nil {
		return facts, err
	}
	defer st.Close()
	pair, err := st.Pair(ctx, start.PairID)
	if err != nil {
		if errors.Is(err, peerstore.ErrNotFound) {
			return facts, nil
		}
		return facts, err
	}
	if pair.SourceMachineID != principal.MachineID ||
		pair.SourceFingerprint != principal.KeyFingerprint || pair.TargetMachineID != l.identity.MachineID ||
		pair.TargetSignature == "" || !pair.RevokedAt.IsZero() || !facts.Now.Before(pair.ExpiresAt) {
		return facts, nil
	}
	auth, err := adaptercloud.NewAccountClient(l.settings.APIBase).AuthorizePeerTaskStart(ctx,
		l.identity.MachineCredential, start, pair.SourcePublicKey)
	if err != nil || !auth.Authorized || auth.SourcePublicKey != pair.SourcePublicKey ||
		auth.SourceKeyFingerprint != pair.SourceFingerprint ||
		auth.TargetMachineID != l.identity.MachineID || auth.PairID != pair.ID {
		return facts, errors.New("peer_authority_unavailable")
	}
	facts.PairActive = true
	if l.opts.PeerTaskProject != nil {
		_, facts.ProjectAvailable = l.opts.PeerTaskProject(ctx, start.TargetProjectID)
	}
	return facts, nil
}

func (l *Link) receivePeerTaskProbe(ctx context.Context, raw []byte) {
	if l.keys == nil || l.opts.PeerAdmitTarget == nil || l.transport == nil {
		return
	}
	var frame peerTaskProbeFrame
	if json.Unmarshal(raw, &frame) != nil || frame.Type != "peer_task_probe_envelope" ||
		peercontract.ValidateTaskStartHeader(frame.Probe.Start) != nil {
		return
	}
	start := frame.Probe.Start
	st, err := peerstore.Open(l.keys.Dir())
	if err != nil {
		return
	}
	defer st.Close()
	pair, err := st.Pair(ctx, start.PairID)
	if err != nil || pair.SourceMachineID != start.Source.MachineID ||
		pair.TargetMachineID != start.TargetMachineID || pair.TargetSignature == "" ||
		!pair.RevokedAt.IsZero() || !l.opts.Now().Before(pair.ExpiresAt) ||
		frame.SourcePublicKey != pair.SourcePublicKey {
		return
	}
	sourceSigner, err := base64.StdEncoding.Strict().DecodeString(pair.SourcePublicKey)
	if err != nil || len(sourceSigner) != ed25519.PublicKeySize {
		return
	}
	switch frame.Probe.Kind {
	case peercontract.TaskProbeChallenge:
		if l.identity.MachineID != pair.SourceMachineID {
			return
		}
		targetSigner, err := base64.StdEncoding.Strict().DecodeString(pair.TargetPublicKey)
		if err != nil || len(targetSigner) != ed25519.PublicKeySize ||
			peercontract.VerifyTaskProbe(frame.Probe, targetSigner) != nil {
			return
		}
		item, err := st.TaskStartOutbox(ctx, start.TaskID)
		if err != nil || item.Start != start {
			return
		}
		if l.opts.PeerAdmitTarget(ctx, start.Source.MachineID,
			start.Source.SessionID, start.Source.ExecutionGeneration) != nil {
			return
		}
		auth, err := adaptercloud.NewAccountClient(l.settings.APIBase).AuthorizePeerTaskStart(ctx,
			l.identity.MachineCredential, start, pair.SourcePublicKey)
		if err != nil || !auth.Authorized || auth.PairID != pair.ID ||
			auth.SourcePublicKey != pair.SourcePublicKey || auth.TargetMachineID != pair.TargetMachineID {
			return
		}
		key, found, err := l.keys.DeviceKey()
		if err != nil || !found || !key.Valid() || key.Fingerprint() != pair.SourceFingerprint {
			return
		}
		proof, err := peercontract.AnswerTaskChallenge(frame.Probe, targetSigner,
			ed25519.NewKeyFromSeed(key.Seed()))
		if err != nil {
			return
		}
		encoded, err := json.Marshal(struct {
			Type            string                 `json:"type"`
			Probe           peercontract.TaskProbe `json:"probe"`
			SourcePublicKey string                 `json:"source_public_key"`
		}{Type: "peer_task_probe_publish", Probe: proof, SourcePublicKey: pair.SourcePublicKey})
		if err == nil {
			_ = l.transport.PublishPeerTaskProbe(encoded)
		}
	case peercontract.TaskProbeProof:
		if l.identity.MachineID != pair.TargetMachineID ||
			peercontract.VerifyTaskProbe(frame.Probe, sourceSigner) != nil {
			return
		}
		l.mu.Lock()
		pending, exists := l.peerTaskPending[start.TaskID]
		l.mu.Unlock()
		if !exists || peercontract.MatchTaskProof(pending.challenge, frame.Probe, sourceSigner) != nil {
			return
		}
		select {
		case pending.reply <- frame.Probe:
		default:
		}
	}
}

func (l *Link) receivePeerTaskEvent(ctx context.Context, raw []byte) {
	if l.keys == nil {
		return
	}
	var frame peerTaskEventFrame
	if json.Unmarshal(raw, &frame) != nil || frame.Type != "peer_task_event_envelope" ||
		peercontract.ValidateTaskEventHeader(frame.Event) != nil ||
		frame.Event.Recipient.MachineID != l.identity.MachineID {
		return
	}
	event := frame.Event
	st, err := peerstore.Open(l.keys.Dir())
	if err != nil {
		return
	}
	defer st.Close()
	pair, err := st.Pair(ctx, event.Start.PairID)
	if err != nil || pair.SourceMachineID != event.Start.Source.MachineID ||
		pair.TargetMachineID != event.Start.TargetMachineID || pair.TargetSignature == "" ||
		!pair.RevokedAt.IsZero() || !l.opts.Now().Before(pair.ExpiresAt) ||
		frame.SourcePublicKey != pair.SourcePublicKey {
		return
	}
	signerKey := pair.TargetPublicKey
	if event.Sender.MachineID == pair.SourceMachineID {
		signerKey = pair.SourcePublicKey
	}
	signer, err := base64.StdEncoding.Strict().DecodeString(signerKey)
	if err != nil || len(signer) != ed25519.PublicKeySize {
		return
	}
	private, err := ecdh.X25519().NewPrivateKey(pair.LocalPrivateKey)
	if err != nil {
		return
	}
	body, err := peercontract.OpenTaskEvent(frame.TaskEventEnvelope, pairKeys(pair), private, signer)
	if err != nil {
		return
	}
	auth, err := adaptercloud.NewAccountClient(l.settings.APIBase).AuthorizePeerTaskStart(ctx,
		l.identity.MachineCredential, event.Start, pair.SourcePublicKey)
	if err != nil || !auth.Authorized || auth.PairID != pair.ID ||
		auth.SourcePublicKey != pair.SourcePublicKey || auth.TargetMachineID != pair.TargetMachineID {
		return
	}
	if l.identity.MachineID == pair.SourceMachineID {
		start, err := st.TaskStartOutbox(ctx, event.Start.TaskID)
		if err != nil || start.Start != event.Start {
			return
		}
		if event.Kind == peercontract.TaskEventOpened {
			if err := st.MarkTaskStartOpened(ctx, event.Start.TaskID,
				event.Sender.SessionID, event.Sender.ExecutionGeneration); err != nil {
				return
			}
		} else if start.TargetSessionID != event.Sender.SessionID ||
			start.TargetGeneration != event.Sender.ExecutionGeneration {
			return
		}
	} else {
		task, err := st.RemoteTask(ctx, event.Start.TaskID)
		if err != nil || task.Start != event.Start || task.State != "opened" ||
			task.TargetSessionID != event.Recipient.SessionID ||
			task.TargetGeneration != event.Recipient.ExecutionGeneration ||
			l.opts.PeerAdmitTarget == nil || l.opts.PeerAdmitTarget(ctx, pair.TargetMachineID,
			event.Recipient.SessionID, event.Recipient.ExecutionGeneration) != nil {
			return
		}
	}
	if err := st.SaveTaskEvent(ctx, peerstore.TaskEventRecord{Event: event, Direction: "in",
		Body: string(body), At: l.opts.Now()}); err != nil {
		l.logf("cloud peer task event: task %s event %s could not be saved: %v",
			event.Start.TaskID, event.RequestID, err)
	}
}

func (l *Link) receivePeer(ctx context.Context, raw []byte) {
	if l.opts.PeerReceipts == nil || l.opts.PeerAdmitTarget == nil || l.opts.PeerExecute == nil {
		return
	}
	var frame peerFrame
	if json.Unmarshal(raw, &frame) != nil || frame.Type != "peer_envelope" ||
		frame.Request.Target.MachineID != l.identity.MachineID {
		return
	}
	st, err := peerstore.Open(l.keys.Dir())
	if err != nil {
		return
	}
	defer st.Close()
	pair, _, err := peerAuthorityRows(ctx, st, frame.Request.GrantID)
	if err != nil || pair.TargetMachineID != l.identity.MachineID ||
		pair.SourceMachineID != frame.Request.Source.MachineID ||
		pair.TargetSignature == "" || !pair.RevokedAt.IsZero() ||
		!l.opts.Now().Before(pair.ExpiresAt) {
		return
	}
	// The relay hint is deliberately ignored; only the local signed pin can
	// supply a signing key or principal.
	signer, err := base64.StdEncoding.Strict().DecodeString(pair.SourcePublicKey)
	if err != nil || len(signer) != ed25519.PublicKeySize {
		return
	}
	private, err := ecdh.X25519().NewPrivateKey(pair.LocalPrivateKey)
	if err != nil {
		return
	}
	keys := pairKeys(pair)
	body, err := peercontract.Open(frame.Envelope, keys, private, signer)
	if err != nil {
		return
	}
	principal := peercontract.Principal{MachineID: pair.SourceMachineID,
		KeyFingerprint: pair.SourceFingerprint, PairID: pair.ID, MachinePeer: true}
	receiver := peerapp.Receiver{Receipts: l.opts.PeerReceipts,
		Facts: func(ctx context.Context, request peercontract.Request, principal peercontract.Principal) (peercontract.Facts, error) {
			return l.peerFacts(ctx, request, principal)
		},
		Execute: l.opts.PeerExecute, Now: l.opts.Now}
	result := receiver.Receive(ctx, frame.Request, body, principal)
	l.logf("cloud peer: request %s machine execution %s code %s", result.RequestID, result.MachineExecution, result.Code)
}

func pairKeys(pair peerstore.Pair) peercontract.PairKeys {
	return peercontract.PairKeys{PairID: pair.ID, SourceMachineID: pair.SourceMachineID,
		TargetMachineID: pair.TargetMachineID, SourceEncryptionKey: pair.SourceEncryptionKey,
		TargetEncryptionKey: pair.TargetEncryptionKey}
}

// A machine pair is the authority for every current Session on its directed
// edge. Older, Session-specific grants remain readable for in-flight work.
func peerAuthorityRows(ctx context.Context, st *peerstore.Store, id string) (peerstore.Pair, *peerstore.Grant, error) {
	pair, err := st.Pair(ctx, id)
	if err == nil {
		return pair, nil, nil
	}
	if !errors.Is(err, peerstore.ErrNotFound) {
		return peerstore.Pair{}, nil, err
	}
	grant, err := st.Grant(ctx, id)
	if err != nil || !grant.RevokedAt.IsZero() {
		return peerstore.Pair{}, nil, errors.New("peer_grant_denied")
	}
	pair, err = st.Pair(ctx, grant.PairID)
	return pair, &grant, err
}

func (l *Link) peerFacts(ctx context.Context, request peercontract.Request,
	principal peercontract.Principal) (peercontract.Facts, error) {
	st, err := peerstore.Open(l.keys.Dir())
	if err != nil {
		return peercontract.Facts{}, err
	}
	defer st.Close()
	pair, grant, err := peerAuthorityRows(ctx, st, request.GrantID)
	if err != nil {
		return peercontract.Facts{}, err
	}
	if pair.ID != principal.PairID || pair.SourceFingerprint != principal.KeyFingerprint ||
		pair.SourceMachineID != principal.MachineID || pair.TargetMachineID != l.identity.MachineID {
		return peercontract.Facts{}, errors.New("peer_pin_mismatch")
	}
	// Cloud is a second revocation authority. Its answer cannot supply the
	// principal, and an unreadable or negative answer closes admission.
	authority, err := adaptercloud.NewAccountClient(l.settings.APIBase).AuthorizePeer(ctx,
		l.identity.MachineCredential, request, pair.SourcePublicKey)
	if err != nil || !authority.Authorized || authority.SourcePublicKey != pair.SourcePublicKey ||
		authority.SourceKeyFingerprint != pair.SourceFingerprint ||
		authority.TargetMachineID != l.identity.MachineID || authority.GrantID != request.GrantID {
		return peercontract.Facts{}, errors.New("peer_authority_unavailable")
	}
	current := l.opts.PeerAdmitTarget(ctx, request.Target.MachineID,
		request.Target.SessionID, request.Target.ExecutionGeneration) == nil
	return peercontract.Facts{LocalMachineID: l.identity.MachineID,
		Principal: principal, PairActive: pair.TargetSignature != "" && pair.RevokedAt.IsZero() && l.opts.Now().Before(pair.ExpiresAt),
		MachineAccess: grant == nil, GrantReadable: grant != nil,
		Grant:          peerGrantFacts(grant),
		PeerCapability: true, LocalCapability: true,
		MachineWritesAllowed: l.allowCommands(), TargetCurrent: current, Now: l.opts.Now()}, nil
}

func peerGrantFacts(grant *peerstore.Grant) peercontract.Grant {
	if grant == nil {
		return peercontract.Grant{}
	}
	return peercontract.Grant{ID: grant.ID, PairID: grant.PairID,
		SourceMachineID: grant.Source.MachineID, SourceSessionID: grant.Source.SessionID,
		SourceGeneration: grant.Source.ExecutionGeneration, TargetMachineID: grant.Target.MachineID,
		TargetSessionID: grant.Target.SessionID, TargetGeneration: grant.Target.ExecutionGeneration,
		PeerKeyFingerprint: grant.SourceKeyFingerprint, AllowMessage: grant.AllowMessage,
		AllowHandoff: grant.AllowHandoff, Revoked: !grant.RevokedAt.IsZero(), ExpiresAt: grant.ExpiresAt}
}

// PublishPeer signs and encrypts a fixed source and target. The result reports
// only the local write to the relay socket; it makes no execution claim.
func (l *Link) PublishPeer(ctx context.Context, request peercontract.Request, body []byte) (peerapp.Result, error) {
	answer := peerapp.Result{RequestID: request.RequestID, MachineExecution: "unknown",
		RelayAccepted: "unknown", RelayDelivered: "unknown", SessionDelivered: "unknown",
		AgentObserved: "unknown", AgentAcknowledged: "unknown"}
	if l.transport == nil || l.keys == nil || request.Source.MachineID != l.identity.MachineID ||
		l.opts.PeerAdmitTarget == nil || !l.allowCommands() || len(body) == 0 || len(body) > peercontract.MaxBodyBytes {
		return answer, errors.New("peer_send_unavailable")
	}
	if err := peercontract.Validate(request, body); err != nil {
		return answer, err
	}
	if err := l.opts.PeerAdmitTarget(ctx, request.Source.MachineID,
		request.Source.SessionID, request.Source.ExecutionGeneration); err != nil {
		return answer, err
	}
	st, err := peerstore.Open(l.keys.Dir())
	if err != nil {
		return answer, err
	}
	defer st.Close()
	pair, grant, err := peerAuthorityRows(ctx, st, request.GrantID)
	if grant != nil && (!l.opts.Now().Before(grant.ExpiresAt) ||
		grant.Source != request.Source || grant.Target != request.Target ||
		(request.Kind == peercontract.Message && !grant.AllowMessage) ||
		(request.Kind == peercontract.Handoff && !grant.AllowHandoff)) {
		return answer, errors.New("peer_grant_denied")
	}
	if err != nil || pair.SourceMachineID != l.identity.MachineID ||
		pair.TargetMachineID != request.Target.MachineID || pair.TargetSignature == "" ||
		(grant != nil && (grant.SourceKeyFingerprint != pair.SourceFingerprint || grant.ExpiresAt.After(pair.ExpiresAt))) ||
		!pair.RevokedAt.IsZero() || !l.opts.Now().Before(pair.ExpiresAt) {
		return answer, errors.New("peer_pair_inactive")
	}
	key, found, err := l.keys.DeviceKey()
	if err != nil || !found || !key.Valid() || key.Fingerprint() != pair.SourceFingerprint {
		return answer, errors.New("peer_signer_changed")
	}
	auth, err := adaptercloud.NewAccountClient(l.settings.APIBase).AuthorizePeer(ctx,
		l.identity.MachineCredential, request, pair.SourcePublicKey)
	if err != nil || !auth.Authorized || auth.SourcePublicKey != pair.SourcePublicKey ||
		auth.SourceKeyFingerprint != pair.SourceFingerprint ||
		auth.TargetMachineID != request.Target.MachineID || auth.GrantID != request.GrantID {
		return answer, errors.New("peer_authority_unavailable")
	}
	private, err := ecdh.X25519().NewPrivateKey(pair.LocalPrivateKey)
	if err != nil {
		return answer, err
	}
	envelope, err := peercontract.Seal(request, body, pairKeys(pair), private,
		ed25519.NewKeyFromSeed(key.Seed()))
	if err != nil {
		return answer, err
	}
	frame, err := json.Marshal(struct {
		Type string `json:"type"`
		peercontract.Envelope
	}{Type: "peer_publish", Envelope: envelope})
	if err != nil {
		return answer, err
	}
	if err := st.SaveOutbox(ctx, peerstore.OutboxItem{Request: request, RelayAccepted: "unknown",
		RelayDelivered: "unknown", At: l.opts.Now()}); err != nil {
		return answer, err
	}
	if err := l.transport.PublishPeer(frame); err != nil {
		return answer, err
	}
	return answer, nil
}
