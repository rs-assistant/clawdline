package cloud

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/cloudkeys"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
)

func TestRuntimeWireIsOptInAndPairingUsesDurableState(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "enabled"}[enabled], func(t *testing.T) {
			keys, err := cloudkeys.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			pins := adaptercloud.NewPinnedStore(keys.Dir())
			l := &Link{keys: keys, pinned: pins, pairing: &Pairing{Pinned: pins}, identityPairing: &IdentityPairing{}, identity: adaptercloud.Identity{AccountID: "account", MachineID: "machine", MachineCredential: "synthetic"}, settings: adaptercloud.Settings{RelayURL: "ws://127.0.0.1:9/v1/connect"}, opts: LinkOptions{ContentKeyRuntime: enabled, Now: time.Now}}
			if err := l.wire(); err != nil {
				t.Fatal(err)
			}
			if !enabled {
				if l.contentEpoch != nil || l.relay.Keys != nil || l.pairing.RevokeContent != nil {
					t.Fatal("legacy path unexpectedly enabled")
				}
				if _, err := os.Stat(filepath.Join(keys.Dir(), "content-key-state-v1")); !os.IsNotExist(err) {
					t.Fatal("legacy path migrated epoch", err)
				}
				return
			}
			if l.contentEpoch == nil || l.relay.Keys != l.contentEpoch || l.pairing.ContentState == nil {
				t.Fatal("runtime fence not wired")
			}
			state, _ := l.contentEpoch.Current()
			next, err := keys.RotateContentKey("synthetic-external-recovery", state.Epoch, rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := l.contentEpoch.Recover(keys.RecoverContentKey, l.retireContent); err != nil {
				t.Fatal(err)
			}
			_, key, err := l.pairingKeys()
			if err != nil || !bytes.Equal(key.Bytes(), next.Key.Bytes()) {
				t.Fatal("pairing read legacy key", err)
			}
			current, err := l.pairing.ContentState()
			if err != nil || current.KeyID != next.KeyID {
				t.Fatal("pairing labels retired epoch", err)
			}
		})
	}
}

func TestRuntimePairingHandoverCarriesRotatedKeyAndID(t *testing.T) {
	l, _, _ := runtimeFixture(t)
	old, _ := l.contentEpoch.Current()
	next, err := l.keys.RotateContentKey("synthetic-handover", old.Epoch, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	h := newPairingHarness(t)
	h.pairing.ContentState = func() (domaincloud.ContentKeyState, error) { return next, nil }
	if _, err := h.pairing.Complete(context.Background(), h.viewer.offer.Fragment(), adaptercloud.ScopeReadOnly); err != nil {
		t.Fatal(err)
	}
	h.plane.mu.Lock()
	raw := bytes.Clone(h.plane.delivered)
	h.plane.mu.Unlock()
	wrapper, err := domaincloud.DecodePairingWrapper(raw)
	if err != nil {
		t.Fatal(err)
	}
	handover, err := domaincloud.OpenPairingHandover(wrapper, h.viewer.offer, h.viewer.ephemeral, h.plane.machineID, time.Now().UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if handover.KeyID != next.KeyID || handover.MasterSecret != base64.StdEncoding.EncodeToString(next.Key.Bytes()) {
		t.Fatal("pairing disclosed a stale or mislabeled key")
	}
}

func TestRuntimePairingFinalPinCannotRacePastRevocation(t *testing.T) {
	l, _, _ := runtimeFixture(t)
	h := newPairingHarness(t)
	l.pinned = h.pinned
	h.pairing.Authority = &l.contentAuthority.mu
	h.pairing.Generation = &l.contentAuthority.generation
	h.pairing.ContentState = l.contentEpoch.Current
	entered := make(chan struct{})
	release := make(chan struct{})
	h.pairing.Keys = func() (domaincloud.DeviceKey, domaincloud.ContentKey, error) {
		close(entered)
		<-release
		return h.key, h.secret, nil
	}
	paired := make(chan error, 1)
	go func() {
		_, err := h.pairing.Complete(context.Background(), h.viewer.offer.Fragment(), adaptercloud.ScopeReadOnly)
		paired <- err
	}()
	<-entered
	type outcome struct {
		changed bool
		err     error
	}
	revoked := make(chan outcome, 1)
	go func() { changed, err := l.revokeContentViewer(h.viewer.deviceID); revoked <- outcome{changed, err} }()
	select {
	case got := <-revoked:
		close(release)
		<-paired
		t.Fatalf("revocation escaped the final-pin barrier: %+v", got)
	case <-time.After(25 * time.Millisecond):
	}
	close(release)
	if err := <-paired; err != nil {
		t.Fatal(err)
	}
	if got := <-revoked; got.err != nil || !got.changed {
		t.Fatalf("revocation after handover: %+v", got)
	}
	if refused, err := h.pinned.Refused(h.viewer.deviceID); err != nil || !refused {
		t.Fatal("final pin resurrected revoked viewer", err)
	}
}

func TestPairingFromRetiredGenerationCannotDeliver(t *testing.T) {
	var generation atomic.Uint64
	p := &Pairing{Generation: &generation}
	ctx := p.withGeneration(context.Background())
	generation.Add(1)
	p.Keys = func() (domaincloud.DeviceKey, domaincloud.ContentKey, error) {
		t.Fatal("stale pairing reached key disclosure")
		return domaincloud.DeviceKey{}, domaincloud.ContentKey{}, nil
	}
	if _, err := p.deliver(ctx, domaincloud.PairingOffer{}); err != domaincloud.ErrRekey {
		t.Fatalf("stale generation: %v", err)
	}
}

func TestRuntimeAnswerDeliveryPrecedesRevocation(t *testing.T) {
	l, _, b := runtimeFixture(t)
	raw, _ := runtimeOffer(t, "B", b, time.Now().UnixMilli())
	entered := make(chan struct{})
	release := make(chan struct{})
	answerDone := make(chan error, 1)
	go func() { answerDone <- l.answerRekey(raw, func([]byte) error { close(entered); <-release; return nil }) }()
	<-entered
	revoked := make(chan error, 1)
	go func() { _, err := l.revokeContentViewer("B"); revoked <- err }()
	select {
	case err := <-revoked:
		t.Fatalf("revocation returned before key delivery left authority sequence: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(release)
	if err := <-answerDone; err != nil {
		t.Fatal(err)
	}
	if err := <-revoked; err != nil {
		t.Fatal(err)
	}
	if err := l.answerRekey(raw, func([]byte) error { t.Fatal("revoked B received late answer"); return nil }); err == nil {
		t.Fatal("revoked offer accepted")
	}
}

func runtimeFixture(t *testing.T) (*Link, domaincloud.DeviceKey, domaincloud.DeviceKey) {
	t.Helper()
	dir := t.TempDir()
	keys, err := cloudkeys.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	machine, err := domaincloud.LoadOrCreateDeviceKey(keys, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_ = machine
	state, err := keys.LoadOrCreateContentKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	epoch, err := domaincloud.NewKeyEpoch(state)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := domaincloud.NewDeviceKey(rand.Reader)
	b, _ := domaincloud.NewDeviceKey(rand.Reader)
	pins := adaptercloud.NewPinnedStore(keys.Dir())
	for id, key := range map[string]domaincloud.DeviceKey{"A": a, "B": b} {
		if err := pins.Pin("account", adaptercloud.PinnedDevice{DeviceID: id, PublicKey: base64.StdEncoding.EncodeToString(key.PublicKey()), Scope: adaptercloud.ScopeReadOnly, PairedAt: 1}); err != nil {
			t.Fatal(err)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"devices": []adaptercloud.RosterDevice{
			{ID: "A", PublicKey: base64.StdEncoding.EncodeToString(a.PublicKey()), Caps: []string{"read_transcript"}},
			{ID: "B", PublicKey: base64.StdEncoding.EncodeToString(b.PublicKey()), Caps: []string{"read_transcript"}},
		}})
	}))
	t.Cleanup(server.Close)
	spool, err := adaptercloud.NewSpool(adaptercloud.DefaultSpoolLimits(), nil, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	l := &Link{keys: keys, pinned: pins, contentEpoch: epoch, contentAuthority: &runtimeAuthority{}, opts: LinkOptions{Now: time.Now}, identity: adaptercloud.Identity{AccountID: "account", MachineID: "machine"}, roster: adaptercloud.NewRoster(server.URL, "synthetic", time.Now), relay: &Relay{Spool: spool}}
	return l, a, b
}

func runtimeOffer(t *testing.T, id string, key domaincloud.DeviceKey, now int64) ([]byte, []byte) {
	t.Helper()
	private, err := domaincloud.NewX25519PrivateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	public, err := domaincloud.X25519PublicKey(private)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, 32)
	_, _ = rand.Read(nonce)
	raw, err := domaincloud.SignRekeyOffer(domaincloud.RekeyOffer{Type: "content_key_offer", Version: 1, AccountID: "account", MachineID: "machine", ViewerDeviceID: id, OperationID: "synthetic-op", FromEpoch: 1, FromKeyID: "ms-1", ViewerEphemeralKey: base64.StdEncoding.EncodeToString(public), Nonce: base64.StdEncoding.EncodeToString(nonce), ExpiresAtMS: now + 60000}, key, now)
	if err != nil {
		t.Fatal(err)
	}
	return raw, private
}

func TestRuntimeRevokedCannotReadFutureAndReadonlyRecovers(t *testing.T) {
	l, a, b := runtimeFixture(t)
	old, _ := l.contentEpoch.Current()
	changed, err := l.revokeContentViewer("A")
	if err != nil || !changed {
		t.Fatalf("revoke %v %v", changed, err)
	}
	next, err := l.contentEpoch.Current()
	if err != nil || next.Epoch != old.Epoch+1 {
		t.Fatalf("next %v %v", next, err)
	}
	changed, err = l.revokeContentViewer("A")
	if err != nil || changed {
		t.Fatalf("retry %v %v", changed, err)
	}
	retry, _ := l.contentEpoch.Current()
	if retry.KeyID != next.KeyID {
		t.Fatal("retry minted another key")
	}
	now := time.Now().UnixMilli()
	rejected, _ := runtimeOffer(t, "A", a, now)
	sent := false
	if err := l.answerRekey(rejected, func([]byte) error { sent = true; return nil }); err == nil || sent {
		t.Fatal("revoked A received a key")
	}
	offer, private := runtimeOffer(t, "B", b, now)
	var answer []byte
	if err := l.answerRekey(offer, func(raw []byte) error { answer = raw; return nil }); err != nil {
		t.Fatal(err)
	}
	machine, _, _ := l.keys.DeviceKey()
	recovered, err := domaincloud.OpenRekeyAnswer(answer, offer, domaincloud.RekeyAuthority{AccountID: "account", MachineID: "machine", ViewerDeviceID: "B", ActiveViewerKey: b.PublicKey()}, private, machine.PublicKey(), 1, "ms-1", time.Now().UnixMilli())
	if err != nil || !bytes.Equal(recovered.Key.Bytes(), next.Key.Bytes()) {
		t.Fatalf("readonly recovery %v", err)
	}
	// Exercise real AEAD, not just state fields: old A cannot open future data.
	envelope, err := domaincloud.Seal([]byte("future-canary"), domaincloud.SealParams{Ch: "s/machine/session", Seq: 1, Ts: uint64(now), Class: domaincloud.ClassStream, KeyID: next.KeyID, Sender: "machine", Key: next.Key, Signer: machine})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := envelope.Open(old.Key, func(string) (ed25519.PublicKey, bool) { return machine.PublicKey(), true }); err == nil {
		t.Fatal("revoked old key opened future canary")
	}
	opened, err := envelope.Open(recovered.Key, func(string) (ed25519.PublicKey, bool) { return machine.PublicKey(), true })
	if err != nil || string(opened) != "future-canary" {
		t.Fatalf("B canary %q %v", opened, err)
	}
}

func TestRuntimeRestartResumesExactRevocation(t *testing.T) {
	l, _, _ := runtimeFixture(t)
	before, _ := l.contentEpoch.Current()
	r, err := l.keys.BeginRevocation("A", before.Epoch)
	if err != nil {
		t.Fatal(err)
	}
	// Crash after durable rotation, before queue retirement and completion.
	durable, err := l.keys.RotateContentKey(r.Operation, r.ExpectedEpoch, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := cloudkeys.Open(l.keys.Dir() + "/..")
	if err != nil {
		t.Fatal(err)
	}
	l.keys = reopened
	if err := l.resumeRevocation(r); err != nil {
		t.Fatal(err)
	}
	after, _ := l.contentEpoch.Current()
	if after.KeyID != durable.KeyID {
		t.Fatal("restart reminted key")
	}
	if refused, err := l.pinned.Refused("A"); err != nil || !refused {
		t.Fatal("persistent deny missing", err)
	}
	if _, pending, err := l.keys.PendingRevocation(); err != nil || pending {
		t.Fatal("journal incomplete", err)
	}
}

func TestRuntimeRejectsUnknownAndWrongSigningPin(t *testing.T) {
	l, a, _ := runtimeFixture(t)
	for _, id := range []string{"unknown", "B"} {
		raw, _ := runtimeOffer(t, id, a, time.Now().UnixMilli())
		if err := l.answerRekey(raw, func([]byte) error { t.Fatal("unverified pin received key"); return nil }); err == nil {
			t.Fatal("unverified offer accepted")
		}
	}
}

func TestRuntimeReadonlyReservationCannotObtainAKey(t *testing.T) {
	l, _, b := runtimeFixture(t)
	// A new readonly row is a pre-delivery ceiling, not a completed pairing.
	if err := l.pinned.ReserveReadOnly("account", adaptercloud.PinnedDevice{DeviceID: "pending", PublicKey: base64.StdEncoding.EncodeToString(b.PublicKey()), PairedAt: 1}); err != nil {
		t.Fatal(err)
	}
	raw, _ := runtimeOffer(t, "pending", b, time.Now().UnixMilli())
	if err := l.answerRekey(raw, func([]byte) error { t.Fatal("unfinished pairing got content key"); return nil }); err == nil {
		t.Fatal("reservation admitted as completed pin")
	}
}

func TestRuntimeRetiredDirectFrameIsRefusedAtSend(t *testing.T) {
	l, _, _ := runtimeFixture(t)
	old, _ := l.contentEpoch.Current()
	if _, err := l.revokeContentViewer("A"); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(directMessage{T: "env", E: json.RawMessage(`{"key_id":"` + old.KeyID + `"}`)})
	peer := &directPeer{l: l, viewer: "B"}
	if err := peer.send(raw); err != domaincloud.ErrKeyEpochRegress {
		t.Fatalf("retired frame reached carrier: %v", err)
	}
}
