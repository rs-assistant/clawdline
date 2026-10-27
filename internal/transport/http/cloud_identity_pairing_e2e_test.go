package http

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/healthsealed"
	"github.com/sainteye/clawdline/internal/config"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
	cloudtransport "github.com/sainteye/clawdline/internal/transport/cloud"
)

// TestIdentityPairingRoutesEndToEnd is the machine in the local route
// experiment: the person's side is driven through the real local routes
// (POST /v1/cloud/pairing/identity, GET it, POST …/decision) against Cloud's
// real routes, and one diagnostic is then sealed under the recorded epoch.
// Skipped unless the fixture (`identity-route-e2e.ts`) names its config.
//
// Rounds: "deny" (the codes match but the person here answers no) and
// "pair" (approved only when the fixture's browser code equals this
// machine's). The fixture writes <round>.viewer-code; this test writes
// <round>.qr and <round>.result.
func TestIdentityPairingRoutesEndToEnd(t *testing.T) {
	path := os.Getenv("CLAWDLINE_IDENTITY_ROUTE_E2E")
	if path == "" {
		t.Skip("set by the identity-route experiment fixture only")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		BaseURL     string `json:"base_url"`
		AccountID   string `json:"account_id"`
		MachineID   string `json:"machine_id"`
		Credential  string `json:"credential"`
		MachineSeed string `json:"machine_seed"`
		Dir         string `json:"dir"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	seed, _ := base64.StdEncoding.DecodeString(cfg.MachineSeed)
	key, err := domaincloud.NewDeviceKey(bytes.NewReader(seed))
	if err != nil {
		t.Fatal(err)
	}
	master := make([]byte, domaincloud.ContentKeyBytes)
	rand.Read(master)
	secret, err := domaincloud.NewContentKey(bytes.NewReader(master))
	if err != nil {
		t.Fatal(err)
	}
	epochs := adaptercloud.ContentEpochStore{Dir: filepath.Join(cfg.Dir, "machine")}
	identity := &cloudtransport.IdentityPairing{
		AccountID: cfg.AccountID, AppOrigin: "https://app.clawdline.test",
		Client: &adaptercloud.IdentityClient{BaseURL: cfg.BaseURL,
			Credential: func(context.Context) (string, error) { return cfg.Credential, nil }},
		Keys:   func() (domaincloud.DeviceKey, domaincloud.ContentKey, error) { return key, secret, nil },
		Epochs: epochs,
		Log:    t.Logf,
	}
	dir := t.TempDir()
	s := &Server{cfg: config.Config{Dir: dir}}
	SetCloudLine(dir, identityPairingLine{identity: identity})
	t.Cleanup(func() { SetCloudLine(dir, nil) })
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	read := func() cloudtransport.IdentityPairingState {
		var st cloudtransport.IdentityPairingState
		_ = json.Unmarshal(identityRequest(s, true, http.MethodGet, "/v1/cloud/pairing/identity", "").Body.Bytes(), &st)
		return st
	}
	until := func(ok func(cloudtransport.IdentityPairingState) bool) cloudtransport.IdentityPairingState {
		for {
			st := read()
			if ok(st) {
				return st
			}
			select {
			case <-ctx.Done():
				t.Fatalf("state never arrived; last %+v", st)
			case <-time.After(50 * time.Millisecond):
			}
		}
	}
	for _, round := range []string{"deny", "pair"} {
		rec := identityRequest(s, true, http.MethodPost, "/v1/cloud/pairing/identity", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: begin answered %d: %s", round, rec.Code, rec.Body)
		}
		var begun cloudtransport.IdentityPairingState
		_ = json.Unmarshal(rec.Body.Bytes(), &begun)
		fragment := strings.TrimPrefix(begun.Link, "https://app.clawdline.test/#pairid=")
		if fragment == begun.Link || begun.PairingID == "" {
			t.Fatalf("%s: begin state %+v", round, begun)
		}
		os.WriteFile(filepath.Join(cfg.Dir, round+".qr"), []byte(fragment), 0o600)
		confirm := until(func(st cloudtransport.IdentityPairingState) bool {
			return st.Phase == cloudtransport.IdentityPairingConfirm
		})
		viewer, err := waitFile(ctx, filepath.Join(cfg.Dir, round+".viewer-code"))
		if err != nil {
			t.Fatal(err)
		}
		approve := round == "pair" && strings.TrimSpace(string(viewer)) == confirm.Code
		body, _ := json.Marshal(map[string]any{"pairing_id": confirm.PairingID, "approve": approve})
		if rec := identityRequest(s, true, http.MethodPost, "/v1/cloud/pairing/identity/decision", string(body)); rec.Code != http.StatusOK {
			t.Fatalf("%s: decision answered %d: %s", round, rec.Code, rec.Body)
		}
		final := until(func(st cloudtransport.IdentityPairingState) bool {
			return st.Phase != cloudtransport.IdentityPairingConfirm && st.Phase != cloudtransport.IdentityPairingHanding
		})
		out, _ := json.Marshal(final)
		os.WriteFile(filepath.Join(cfg.Dir, round+".result"), out, 0o600)
		_, recorded, _ := epochs.Load(cfg.AccountID, master)
		switch {
		case round == "deny" && (final.Phase != cloudtransport.IdentityPairingDenied || recorded):
			t.Fatalf("deny: final %+v, epoch recorded %v", final, recorded)
		case round == "pair" && (final.Phase != cloudtransport.IdentityPairingPaired || !recorded):
			t.Fatalf("pair: final %+v, epoch recorded %v", final, recorded)
		}
	}

	masterFn := func() ([]byte, bool, error) { return append([]byte(nil), master...), true, nil }
	up := &healthsealed.Uploader{
		Endpoint:          cfg.BaseURL,
		AllowLoopbackHTTP: true,
		Enabled:           func() bool { return true },
		Keys: healthsealed.ProviderSource{
			Identity: func(context.Context) (string, string, string, int64, error) {
				return cfg.AccountID, cfg.MachineID, cfg.Credential, 0, nil
			},
			Master: masterFn,
			ContentEpoch: func() (int64, bool) {
				e, ok, err := epochs.Load(cfg.AccountID, master)
				return e, ok && err == nil
			},
		},
	}
	store, err := healthsealed.OpenStore(filepath.Join(cfg.Dir, "sealed"))
	if err != nil {
		t.Fatal(err)
	}
	up.Store = store
	snap := healthsealed.Snapshot{Schema: healthsealed.SnapshotSchema, Location: "route-e2e.go:1", Stage: "relay_write",
		AgeNS: healthsealed.Int64{Value: 1_500_000_000, Set: true}, ObservedAtMS: healthsealed.Int64{Value: time.Now().UnixMilli(), Set: true}}
	pending, err := up.Seal(ctx, "boot-route-e2e", "evidence", "ev-route-e2e", snap)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	receipt, err := up.Send(ctx, pending.AttachmentID)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	out, _ := json.Marshal(map[string]any{"attachment_id": pending.AttachmentID, "revision": pending.Revision,
		"content_key_epoch": receipt.ContentKeyEpoch, "machine_id": cfg.MachineID})
	if err := os.WriteFile(filepath.Join(cfg.Dir, "uploaded.json"), out, 0o600); err != nil {
		t.Fatal(err)
	}
}

func waitFile(ctx context.Context, path string) ([]byte, error) {
	for {
		if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
			return b, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}
