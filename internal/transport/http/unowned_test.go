package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/config"
)

// withoutInheritedNextEnv unsets every CLAWDLINE_NEXT_* variable for the rest
// of the test and restores it afterwards: every Session Clawdline opens carries
// some into whatever it runs. Call it before the test sets the variables it
// means.
func withoutInheritedNextEnv(t *testing.T) {
	t.Helper()
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "CLAWDLINE_NEXT_") {
			t.Setenv(name, "") // registers the restore
			_ = os.Unsetenv(name)
		}
	}
}

// daemonFixture is a daemon built the way `clawdline serve` builds one, with
// its own state directory and this machine's own token, so that what is being
// read is the real routing decision behind the real gate.
func daemonFixture(t *testing.T, cfg config.Config) (http.Handler, string) {
	t.Helper()
	t.Setenv("CLAWDLINE_NEXT_WEB", "")
	cfg.Dir = filepath.Join(t.TempDir(), "next")
	if cfg.Port == 0 {
		cfg.Port = 7757
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.store.Close() })
	local, _, err := s.CloudCredentials()
	if err != nil {
		t.Fatal(err)
	}
	return s.Handler(), local
}

func askUnowned(t *testing.T, h http.Handler, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/a-route-nobody-has-written-yet", nil)
	req.Host = "127.0.0.1:7757"
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// A route this daemon has not taken over says its own name.
func TestAnUnownedRouteNamesItself(t *testing.T) {
	withoutInheritedNextEnv(t)
	h, token := daemonFixture(t, config.Load())

	rec := askUnowned(t, h, token)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("an unowned route answered %d, not 501: %s", rec.Code, rec.Body)
	}
	var refusal struct {
		Error string `json:"error"`
		Route string `json:"route"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &refusal); err != nil {
		t.Fatalf("the refusal is not json: %s", rec.Body)
	}
	if refusal.Error != "not_implemented" {
		t.Errorf("the refusal is %q, not not_implemented: %s", refusal.Error, rec.Body)
	}
	if refusal.Route != "/v1/a-route-nobody-has-written-yet" {
		t.Errorf("the refusal does not name the route: %s", rec.Body)
	}
}

// /v1/sessions is answered here whatever the environment says: the session
// list is the product.
func TestSessionsAreAnsweredByThisDaemon(t *testing.T) {
	withoutInheritedNextEnv(t)
	h, token := daemonFixture(t, config.Load())

	req := httptest.NewRequest(http.MethodGet, "/v1/sessions", nil)
	req.Host = "127.0.0.1:7757"
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/v1/sessions answered %d, not 200: %s", rec.Code, rec.Body)
	}
	var reply map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &reply); err != nil {
		t.Fatalf("/v1/sessions is not json: %s", rec.Body)
	}
	if _, ok := reply["sessions"]; !ok {
		t.Errorf("/v1/sessions answered without a session list: %s", rec.Body)
	}
}
