package cloud

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
)

// A browser the account's roster names, holding this machine's content key,
// but never paired with this machine, is not admitted: the roster is the
// operator's list and may take a device away, never supply one. Until
// 2026-10-22 such a "pre-pin" browser was admitted on the roster's key, so
// whoever could add a roster row and held an old copy of the content key could
// send commands here.
func TestARosterKeyAloneNeverAdmitsASender(t *testing.T) {
	viewer, err := domaincloud.NewDeviceKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	machine, err := domaincloud.NewDeviceKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := domaincloud.NewContentKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	stranger, err := domaincloud.NewDeviceKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	row := adaptercloud.RosterDevice{ID: "viewer", PublicKey: base64.StdEncoding.EncodeToString(viewer.PublicKey())}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"devices": []adaptercloud.RosterDevice{row}})
	}))
	defer server.Close()
	setRow := func(change func(*adaptercloud.RosterDevice)) {
		mu.Lock()
		change(&row)
		mu.Unlock()
	}
	dir := t.TempDir()
	file := nextconfig.Open(dir)
	if _, err := file.Set(map[string]any{adaptercloud.KeyEnabled: true, adaptercloud.KeyCommands: true}); err != nil {
		t.Fatal(err)
	}
	l := &Link{file: file, settings: adaptercloud.Settings{Enabled: true}, opts: LinkOptions{Now: time.Now},
		pinned:           adaptercloud.NewPinnedStore(t.TempDir()),
		roster:           adaptercloud.NewRoster(server.URL, "credential", time.Now),
		identity:         adaptercloud.Identity{MachineID: "machine"},
		terminalRequests: make(chan Inbound, 8), terminalRefusals: make(chan Inbound, 8)}
	if err := l.roster.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	opened := 0
	transport, err := adaptercloud.New(adaptercloud.Options{
		RelayURL: "wss://relay.invalid", Token: &adaptercloud.TokenSource{},
		Identity: adaptercloud.Identity{AccountID: "account", MachineID: "machine", MachineCredential: "credential"},
		Signer:   machine, Replay: domaincloud.NewReplayWindow(64), ContentKey: secret,
		PublicKeyFor: l.publicKeyFor,
		Inbound: func(env domaincloud.Envelope, plain []byte, verifiedKey ed25519.PublicKey) {
			opened++
			l.deliverTerminal(Inbound{Channel: env.Ch, Class: string(env.Class), Sender: env.Sender,
				Sequence: env.Seq, Plaintext: plain, VerifiedKey: verifiedKey})
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	seal := func(seq uint64, signer domaincloud.DeviceKey) []byte {
		t.Helper()
		env, err := domaincloud.Seal([]byte(`{"type":"send","session":"%1","text":"hello"}`),
			domaincloud.SealParams{Ch: "termi/machine/viewer", Seq: seq, Ts: uint64(time.Now().UnixMilli()),
				Class: domaincloud.ClassCtl, KeyID: adaptercloud.MasterKeyID,
				Sender: "viewer", Key: secret, Signer: signer})
		if err != nil {
			t.Fatal(err)
		}
		raw, err := env.CanonicalJSON()
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}

	// Roster row, content key, valid signature — and no pin.
	if transport.AcceptDirect("viewer", seal(1, viewer)) || opened != 0 {
		t.Fatal("a sender only the roster names was admitted on the roster's key")
	}
	if a := l.authority(context.Background(), "viewer", viewer.PublicKey(), true); a.RosterAllowsSender || a.WriteGateAllows {
		t.Fatal("a sender only the roster names was granted command authority")
	}
	if a := l.authority(context.Background(), "viewer", viewer.PublicKey(), false); a.RosterAllowsSender {
		t.Fatal("a sender only the roster names was granted read authority")
	}
	if l.TerminalViewerAllowed("viewer") {
		t.Fatal("a sender only the roster names was granted terminal authority")
	}

	// Paired here: the pin admits, and the roster still agrees.
	if err := l.pinned.Pin("account", adaptercloud.PinnedDevice{DeviceID: "viewer", PublicKey: row.PublicKey}); err != nil {
		t.Fatal(err)
	}
	if !transport.AcceptDirect("viewer", seal(2, viewer)) || opened != 1 {
		t.Fatal("a pinned sender's envelope was refused")
	}
	if a := l.authority(context.Background(), "viewer", viewer.PublicKey(), true); !a.RosterAllowsSender || !a.WriteGateAllows {
		t.Fatalf("a pinned sender was not granted command authority: %+v", a)
	}
	if !l.TerminalViewerAllowed("viewer") {
		t.Fatal("a pinned sender was not granted terminal authority")
	}
	if l.authority(context.Background(), "viewer", stranger.PublicKey(), false).RosterAllowsSender {
		t.Fatal("authority was granted for a key that is not the pin")
	}

	// The roster naming another key for the pinned device is a disagreement,
	// and it denies; it does not become the key this machine verifies with.
	setRow(func(r *adaptercloud.RosterDevice) {
		r.PublicKey = base64.StdEncoding.EncodeToString(stranger.PublicKey())
	})
	if err := l.roster.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if transport.AcceptDirect("viewer", seal(3, stranger)) {
		t.Fatal("a key the roster substituted verified an envelope")
	}
	if l.authority(context.Background(), "viewer", viewer.PublicKey(), true).RosterAllowsSender {
		t.Fatal("a roster that names another key still granted command authority")
	}

	// The roster may take a pinned device away.
	revokedAt := time.Now().UTC().Format(time.RFC3339)
	setRow(func(r *adaptercloud.RosterDevice) {
		r.PublicKey = base64.StdEncoding.EncodeToString(viewer.PublicKey())
		r.RevokedAt = &revokedAt
	})
	if err := l.roster.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if l.authority(context.Background(), "viewer", viewer.PublicKey(), false).RosterAllowsSender {
		t.Fatal("the account's revocation did not deny a pinned device")
	}

	// And a local revocation beats everything.
	setRow(func(r *adaptercloud.RosterDevice) { r.RevokedAt = nil })
	if err := l.roster.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if changed, err := l.pinned.Revoke("viewer", time.Now()); err != nil || !changed {
		t.Fatalf("revoke: changed=%v error=%v", changed, err)
	}
	if transport.AcceptDirect("viewer", seal(4, viewer)) || l.authority(context.Background(), "viewer", viewer.PublicKey(), true).RosterAllowsSender {
		t.Fatal("local revocation did not beat the account roster and machine key")
	}
}
