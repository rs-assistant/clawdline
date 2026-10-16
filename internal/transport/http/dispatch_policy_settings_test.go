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

func TestDispatchPolicySettingsKeepsBaseAndRejectsStaleSave(t *testing.T) {
	t.Setenv("CLAWDLINE_NEXT_LEGACY_STORE", "off")
	dir := t.TempDir()
	s := &Server{cfg: config.Config{Dir: dir}}
	call := func(method, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/v1/settings/dispatch-policy", strings.NewReader(body))
		if method == http.MethodPut {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		s.dispatchPolicySettings(rec, req)
		return rec
	}
	var original struct{ Base, Local, Source, Version string }
	if rec := call(http.MethodGet, ""); rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &original) != nil || original.Base == "" || original.Source != "none" {
		t.Fatalf("initial policy: %d %s", rec.Code, rec.Body)
	}
	change, _ := json.Marshal(map[string]string{"local": "My local rule.\n", "version": original.Version})
	if rec := call(http.MethodPut, string(change)); rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	if rec := call(http.MethodPut, string(change)); rec.Code != http.StatusConflict {
		t.Fatalf("stale save: %d %s", rec.Code, rec.Body)
	}
	held, err := os.ReadFile(filepath.Join(dir, "dispatch-policy.local.md"))
	if err != nil || string(held) != "My local rule.\n" {
		t.Fatalf("local file: %q %v", held, err)
	}
	var saved struct{ Base, Local, Source string }
	rec := call(http.MethodGet, "")
	if err := json.Unmarshal(rec.Body.Bytes(), &saved); err != nil || saved.Base != original.Base || saved.Local != string(held) || saved.Source != "next" {
		t.Fatalf("saved policy: %d %s %v", rec.Code, rec.Body, err)
	}
}

func TestDispatchPolicySettingsStaysOnThisMachine(t *testing.T) {
	f, h := newGateFixture(t)
	for _, token := range []string{f.send, f.read} {
		rec := (call{method: http.MethodGet, path: "/v1/settings/dispatch-policy", headers: map[string]string{"Authorization": "Bearer " + token}}).do(h)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("device read: %d %s", rec.Code, rec.Body)
		}
	}
	rec := (call{method: http.MethodGet, path: "/v1/settings/dispatch-policy", headers: map[string]string{"Authorization": "Bearer " + f.local}}).do(h)
	if rec.Code != http.StatusOK {
		t.Fatalf("local read: %d %s", rec.Code, rec.Body)
	}
}
