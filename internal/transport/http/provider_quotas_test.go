package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/limits"
	"github.com/sainteye/clawdline/internal/adapters/providerquota"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/contract"
)

func TestProviderQuotaEndpointIsReadOnlyAndUsesThePublishedContract(t *testing.T) {
	home := t.TempDir()
	s := &Server{cfg: config.Config{Dir: t.TempDir()}, providerQuotaService: providerquota.New(home, limits.NewReader(home, nil))}
	read := func() contract.ProviderQuotaSnapshot {
		t.Helper()
		rec := httptest.NewRecorder()
		s.providerQuotas(rec, httptest.NewRequest("GET", "/v1/provider-quotas", nil))
		if rec.Code != 200 {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
		var got contract.ProviderQuotaSnapshot
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	// Off by default: Claude and Codex only, from local files.
	if got := read(); len(got.Providers) != 2 || got.Updating {
		t.Fatalf("with provider_quota_remote unset: %+v", got)
	}
	if _, err := s.settingsFile().Set(map[string]any{"provider_quota_remote": true}); err != nil {
		t.Fatal(err)
	}
	got := read()
	if len(got.Providers) != 4 || got.Updating {
		t.Fatalf("%+v", got)
	}
	for _, r := range got.Providers {
		if len(r.Windows) != 0 {
			t.Fatalf("a missing reading became quota: %+v", r)
		}
	}
	rec := httptest.NewRecorder()
	s.providerQuotas(rec, httptest.NewRequest("POST", "/v1/provider-quotas", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("write accepted: %d", rec.Code)
	}
}
