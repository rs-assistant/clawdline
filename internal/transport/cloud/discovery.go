package cloud

import (
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/peerstore"
	peercontract "github.com/sainteye/clawdline/internal/domain/agenthandoff"
)

const DiscoveryReplyLimit = 1

type discoveryFrame struct {
	Type string `json:"type"`
	peercontract.DiscoveryEnvelope
}

type pendingDiscovery struct {
	query peercontract.Discovery
	reply chan []byte
}

// InspectPeer reads current inventory through the machine pair, without a
// viewer identity and without starting a target Session.
func (l *Link) InspectPeer(ctx context.Context, source peercontract.Endpoint, machine, pairID string) ([]byte, error) {
	if l.opts.PeerAdmitTarget == nil || l.opts.PeerAdmitTarget(ctx, source.MachineID, source.SessionID, source.ExecutionGeneration) != nil {
		return nil, errors.New("remote_source_unverified")
	}
	nonce := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	body := []byte(`{"kind":"inventory"}`)
	sum := sha256.Sum256(body)
	query := peercontract.Discovery{RequestID: hex.EncodeToString(nonce), Source: source,
		TargetMachineID: machine, PairID: pairID, BodyDigest: hex.EncodeToString(sum[:])}
	if peercontract.ValidateDiscovery(query, body) != nil {
		return nil, errors.New("remote_discovery_invalid")
	}
	pending := pendingDiscovery{query: query, reply: make(chan []byte, DiscoveryReplyLimit)}
	l.mu.Lock()
	if l.peerDiscovery == nil {
		l.peerDiscovery = make(map[string]pendingDiscovery)
	}
	if len(l.peerDiscovery) >= PeerIngressLimit {
		l.mu.Unlock()
		return nil, errors.New("remote_discovery_busy")
	}
	l.peerDiscovery[query.RequestID] = pending
	l.mu.Unlock()
	defer func() { l.mu.Lock(); delete(l.peerDiscovery, query.RequestID); l.mu.Unlock() }()
	if err := l.publishDiscovery(ctx, query, body); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, errors.New("remote_discovery_timeout")
	case reply := <-pending.reply:
		if l.opts.PeerAdmitTarget(ctx, source.MachineID, source.SessionID, source.ExecutionGeneration) != nil {
			return nil, errors.New("remote_source_unverified")
		}
		return reply, nil
	}
}

// discoveryPair checks both pinned keys against the live Cloud pair. Missing
// authority never turns into an empty inventory or a viewer-login workaround.
func (l *Link) discoveryPair(ctx context.Context, query peercontract.Discovery) (peerstore.Pair, error) {
	if l.keys == nil || l.transport == nil || !l.allowCommands() {
		return peerstore.Pair{}, errors.New("remote_discovery_unavailable")
	}
	st, err := peerstore.Open(l.keys.Dir())
	if err != nil {
		return peerstore.Pair{}, err
	}
	defer st.Close()
	pair, err := st.Pair(ctx, query.PairID)
	if err != nil || pair.SourceMachineID != query.Source.MachineID || pair.TargetMachineID != query.TargetMachineID ||
		pair.TargetSignature == "" || !pair.RevokedAt.IsZero() || !l.opts.Now().Before(pair.ExpiresAt) ||
		(l.identity.MachineID != pair.SourceMachineID && l.identity.MachineID != pair.TargetMachineID) {
		return peerstore.Pair{}, errors.New("remote_pair_inactive")
	}
	live, err := adaptercloud.NewAccountClient(l.settings.APIBase).GetPeerPair(ctx, l.identity.MachineCredential, pair.ID)
	if err != nil {
		return peerstore.Pair{}, errors.New("remote_authority_unavailable")
	}
	expires, expiryErr := time.Parse(time.RFC3339Nano, live.ExpiresAt)
	if expiryErr != nil || !l.opts.Now().Before(expires) {
		return peerstore.Pair{}, errors.New("remote_pair_inactive")
	}
	if live.PairID != pair.ID || live.SourceMachineID != pair.SourceMachineID || live.TargetMachineID != pair.TargetMachineID ||
		live.SourcePublicKey != pair.SourcePublicKey || live.TargetPublicKey != pair.TargetPublicKey ||
		live.SourceKeyFingerprint != pair.SourceFingerprint || live.TargetKeyFingerprint != pair.TargetFingerprint ||
		live.AcceptedAt == nil || live.RevokedAt != nil || live.TargetAcceptSignature == nil || *live.TargetAcceptSignature != pair.TargetSignature ||
		live.SourceOfferSignature != pair.SourceSignature {
		return peerstore.Pair{}, errors.New("remote_pair_inactive")
	}
	return pair, nil
}

func (l *Link) publishDiscovery(ctx context.Context, query peercontract.Discovery, body []byte) error {
	pair, err := l.discoveryPair(ctx, query)
	if err != nil {
		return err
	}
	expected := pair.SourceMachineID
	if query.Reply {
		expected = pair.TargetMachineID
	}
	key, found, err := l.keys.DeviceKey()
	if err != nil || !found || !key.Valid() || l.identity.MachineID != expected || key.Fingerprint() != map[bool]string{false: pair.SourceFingerprint, true: pair.TargetFingerprint}[query.Reply] {
		return errors.New("remote_signer_changed")
	}
	private, err := ecdh.X25519().NewPrivateKey(pair.LocalPrivateKey)
	if err != nil {
		return err
	}
	envelope, err := peercontract.SealDiscovery(query, body, pairKeys(pair), private, ed25519.NewKeyFromSeed(key.Seed()))
	if err != nil {
		return err
	}
	frame, err := json.Marshal(discoveryFrame{Type: "peer_discovery_publish", DiscoveryEnvelope: envelope})
	if err != nil {
		return err
	}
	return l.transport.PublishPeerDiscovery(frame)
}

func (l *Link) receiveDiscovery(ctx context.Context, raw []byte) {
	var frame discoveryFrame
	if json.Unmarshal(raw, &frame) != nil || frame.Type != "peer_discovery_envelope" {
		return
	}
	query := frame.Query
	expected := query.TargetMachineID
	if query.Reply {
		expected = query.Source.MachineID
	}
	if l.identity.MachineID != expected {
		return
	}
	pair, err := l.discoveryPair(ctx, query)
	if err != nil {
		return
	}
	public := pair.SourcePublicKey
	if query.Reply {
		public = pair.TargetPublicKey
	}
	signer, err := base64.StdEncoding.Strict().DecodeString(public)
	if err != nil || len(signer) != ed25519.PublicKeySize {
		return
	}
	private, err := ecdh.X25519().NewPrivateKey(pair.LocalPrivateKey)
	if err != nil {
		return
	}
	body, err := peercontract.OpenDiscovery(frame.DiscoveryEnvelope, pairKeys(pair), private, signer)
	if err != nil {
		return
	}
	if query.Reply {
		l.mu.Lock()
		pending, found := l.peerDiscovery[query.RequestID]
		l.mu.Unlock()
		want := query
		want.Reply = false
		want.BodyDigest = pending.query.BodyDigest
		if !found || want != pending.query {
			return
		}
		select {
		case pending.reply <- body:
		default:
		}
		return
	}
	if string(body) != `{"kind":"inventory"}` || l.opts.PeerInventory == nil {
		return
	}
	reply, err := l.opts.PeerInventory(ctx)
	if err != nil {
		reply, _ = json.Marshal(map[string]string{"error": "remote_inventory_unavailable"})
	}
	if len(reply) == 0 || len(reply) > peercontract.MaxBodyBytes {
		reply = []byte(`{"error":"remote_inventory_too_large"}`)
	}
	sum := sha256.Sum256(reply)
	query.Reply = true
	query.BodyDigest = hex.EncodeToString(sum[:])
	if err := l.publishDiscovery(ctx, query, reply); err != nil {
		l.logf("cloud peer discovery: reply refused: %v", err)
	}
}

// A relay ack proves transport only. Offline and refusal answers can end a
// read immediately, while a delivered ack still waits for an authenticated reply.
func (l *Link) recordDiscoveryStatus(raw []byte) {
	var status struct {
		Type      string `json:"type"`
		RequestID string `json:"request_id"`
		Status    string `json:"status"`
		Code      string `json:"code"`
	}
	if json.Unmarshal(raw, &status) != nil {
		return
	}
	code := ""
	if status.Type == "peer_discovery_ack" && status.Status == "machine_offline" {
		code = "remote_machine_offline"
	}
	if status.Type == "peer_discovery_error" {
		code = status.Code
	}
	if code == "" {
		return
	}
	l.mu.Lock()
	pending, found := l.peerDiscovery[status.RequestID]
	l.mu.Unlock()
	if !found {
		return
	}
	body, _ := json.Marshal(map[string]string{"error": code})
	select {
	case pending.reply <- body:
	default:
	}
}
