package http

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/auth"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
	cloudtransport "github.com/sainteye/clawdline/internal/transport/cloud"
)

type identityPairingLine struct {
	pairingOnlyLine
	identity *cloudtransport.IdentityPairing
}

func (l identityPairingLine) IdentityPairing() *cloudtransport.IdentityPairing { return l.identity }

// newIdentityRouteHarness runs the real IdentityPairing with a scripted run
// that shows a QR and asks for the decision the way the adapter's run does.
func newIdentityRouteHarness(t *testing.T) (*Server, chan error) {
	t.Helper()
	key, err := domaincloud.NewDeviceKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := domaincloud.NewContentKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	outcome := make(chan error, 1)
	identity := &cloudtransport.IdentityPairing{
		AccountID: "usr_test", AppOrigin: "https://app.example.test",
		Client: &adaptercloud.IdentityClient{BaseURL: "http://127.0.0.1:1"},
		Keys:   func() (domaincloud.DeviceKey, domaincloud.ContentKey, error) { return key, secret, nil },
		Run: func(ctx context.Context, m *adaptercloud.MachineIdentityPairing, _ string) (adaptercloud.IdentityPairingResult, error) {
			m.Show(testIdentityQR(t, key).Fragment())
			ok, err := m.Approve(ctx, "AAAA-BBBB", "123456")
			if err == nil && !ok {
				err = domaincloud.ErrPairingNotApproved
			}
			outcome <- err
			if err != nil {
				return adaptercloud.IdentityPairingResult{}, err
			}
			return adaptercloud.IdentityPairingResult{PairingID: "pr_1", ViewerDeviceID: "dev_v", ContentKeyEpoch: 1}, nil
		},
	}
	dir := t.TempDir()
	s := &Server{cfg: config.Config{Dir: dir}}
	SetCloudLine(dir, identityPairingLine{identity: identity})
	t.Cleanup(func() { SetCloudLine(dir, nil) })
	return s, outcome
}

func identityRequest(s *Server, local bool, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), accessKey{},
		access{verdict: auth.Verdict{Allowed: true, Local: local}}))
	rec := httptest.NewRecorder()
	if strings.HasSuffix(path, "/decision") {
		s.cloudIdentityPairingDecisionRoute(rec, req)
	} else {
		s.cloudIdentityPairingRoute(rec, req)
	}
	return rec
}

func TestIdentityPairingRoutesAreThisMachinesOnly(t *testing.T) {
	s, _ := newIdentityRouteHarness(t)
	for _, c := range []struct{ method, path, body string }{
		{http.MethodPost, "/v1/cloud/pairing/identity", ""},
		{http.MethodGet, "/v1/cloud/pairing/identity", ""},
		{http.MethodPost, "/v1/cloud/pairing/identity/decision", `{"pairing_id":"x","approve":true}`},
	} {
		if rec := identityRequest(s, false, c.method, c.path, c.body); rec.Code != http.StatusForbidden {
			t.Fatalf("%s %s from a remote reader answered %d, want 403", c.method, c.path, rec.Code)
		}
	}
}

func TestIdentityPairingNeedsTheLocalDecision(t *testing.T) {
	s, outcome := newIdentityRouteHarness(t)
	rec := identityRequest(s, true, http.MethodPost, "/v1/cloud/pairing/identity", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("begin answered %d: %s", rec.Code, rec.Body)
	}
	var state cloudtransport.IdentityPairingState
	_ = json.Unmarshal(rec.Body.Bytes(), &state)
	if !strings.HasPrefix(state.Link, "https://app.example.test/#pairid=") || state.PairingID != "pr_1" || state.ExpiresAt == 0 {
		t.Fatalf("begin state = %+v", state)
	}
	waitFor(t, func() bool {
		var st cloudtransport.IdentityPairingState
		_ = json.Unmarshal(identityRequest(s, true, http.MethodGet, "/v1/cloud/pairing/identity", "").Body.Bytes(), &st)
		return st.Phase == cloudtransport.IdentityPairingConfirm && st.Code == "123456"
	})
	for _, body := range []string{`{"pairing_id":"pr_1"}`, `{"approve":true}`, `{"pairing_id":"pr_1","approve":true,"auto":1}`} {
		if rec := identityRequest(s, true, http.MethodPost, "/v1/cloud/pairing/identity/decision", body); rec.Code != http.StatusBadRequest {
			t.Fatalf("decision %s answered %d, want 400", body, rec.Code)
		}
	}
	if rec := identityRequest(s, true, http.MethodPost, "/v1/cloud/pairing/identity/decision", `{"pairing_id":"other","approve":true}`); rec.Code != http.StatusNotFound {
		t.Fatalf("a decision for another pairing answered %d, want 404", rec.Code)
	}
	select {
	case err := <-outcome:
		t.Fatalf("the run went on without a decision: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	identityRequest(s, true, http.MethodDelete, "/v1/cloud/pairing/identity", "")
	if err := <-outcome; err == nil {
		t.Fatal("a cancelled pairing completed")
	}
}

func TestIdentityPairingApprovedAndDeniedOnThisMachine(t *testing.T) {
	for _, approve := range []bool{true, false} {
		s, outcome := newIdentityRouteHarness(t)
		identityRequest(s, true, http.MethodPost, "/v1/cloud/pairing/identity", "")
		waitFor(t, func() bool {
			var st cloudtransport.IdentityPairingState
			_ = json.Unmarshal(identityRequest(s, true, http.MethodGet, "/v1/cloud/pairing/identity", "").Body.Bytes(), &st)
			return st.Phase == cloudtransport.IdentityPairingConfirm
		})
		body := `{"pairing_id":"pr_1","approve":false}`
		want := cloudtransport.IdentityPairingDenied
		if approve {
			body, want = `{"pairing_id":"pr_1","approve":true}`, cloudtransport.IdentityPairingPaired
		}
		if rec := identityRequest(s, true, http.MethodPost, "/v1/cloud/pairing/identity/decision", body); rec.Code != http.StatusOK {
			t.Fatalf("decision answered %d: %s", rec.Code, rec.Body)
		}
		<-outcome
		waitFor(t, func() bool {
			var st cloudtransport.IdentityPairingState
			_ = json.Unmarshal(identityRequest(s, true, http.MethodGet, "/v1/cloud/pairing/identity", "").Body.Bytes(), &st)
			return st.Phase == want && st.Code == ""
		})
		// A second answer to the same comparison is refused, not replayed.
		if rec := identityRequest(s, true, http.MethodPost, "/v1/cloud/pairing/identity/decision", body); rec.Code == http.StatusOK {
			t.Fatalf("a repeated decision answered 200")
		}
	}
}

func testIdentityQR(t *testing.T, key domaincloud.DeviceKey) domaincloud.IdentityPairingQR {
	t.Helper()
	nonce := func(b byte) string {
		return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, domaincloud.PairingNonceBytes))
	}
	return domaincloud.IdentityPairingQR{
		PairingID: "pr_1", ClaimNonce: nonce(1), PairingNonce: nonce(2), ExpiresAt: time.Now().Add(10 * time.Minute).UnixMilli(),
		AccountID: "usr_test", MachineID: "mac_1",
		MachineSigningKey:   base64.StdEncoding.EncodeToString(key.PublicKey()),
		MachineFingerprint:  key.Fingerprint(),
		MachineEphemeralKey: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{3}, domaincloud.PairingEphemeralBytes)),
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
