package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sainteye/clawdline/internal/app/cloudops"
	"github.com/sainteye/clawdline/internal/transport/cloud"
)

// The identity pairing hands over the account's content key, so a Cloud
// viewer may never start, read, stop or answer one, even though every request
// the Cloud router builds holds this machine's own token. No Cloud operation
// names these routes today; this is the refusal if one ever does.
func TestACloudViewerCannotDriveTheIdentityPairing(t *testing.T) {
	s := newLocalAuthorityStandIn(t)
	router := cloud.Router{Handler: s.handler, Authorize: cloud.LocalAuthorizer(s.local, s.machine)}
	for _, req := range []cloudops.LocalRequest{
		{Method: http.MethodGet, Path: "/v1/cloud/pairing/identity"},
		{Method: http.MethodPost, Path: "/v1/cloud/pairing/identity"},
		{Method: http.MethodDelete, Path: "/v1/cloud/pairing/identity"},
		{Method: http.MethodPost, Path: "/v1/cloud/pairing/identity/decision",
			Body: []byte(`{"pairing_id":"p","approve":true}`)},
	} {
		res, err := router.Do(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		if res.Status != http.StatusForbidden {
			t.Fatalf("%s %s over Cloud answered %d %s", req.Method, req.Path, res.Status, res.Body)
		}
	}
	// This machine's own token over a socket still reaches the route.
	r := httptest.NewRequest(http.MethodGet, "/v1/cloud/pairing/identity", nil)
	r.Host = "127.0.0.1:7757"
	r.Header.Set("Authorization", "Bearer "+s.local)
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, r)
	if rec.Code == http.StatusForbidden || rec.Code == http.StatusUnauthorized {
		t.Fatalf("the local token was refused: %d %s", rec.Code, rec.Body)
	}
}
