package http

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/icon"
)

func TestSetupLookupKeepsPlaceListResponsiveWhenAProjectOpenStalls(t *testing.T) {
	blocked := make(chan struct{})
	t.Cleanup(func() { close(blocked) })
	var calls atomic.Int32
	lookup := newSetupLookup(func(path, _ string, _ icon.Source) *contract.ProjectSetup {
		calls.Add(1)
		if path == "/blocked" {
			<-blocked
		}
		return &contract.ProjectSetup{ServerCount: 2}
	})
	keys := []setupLookupKey{{path: "/blocked"}, {path: "/healthy"}}
	start := time.Now()
	got := lookup.Resolve(keys)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("blocked Project setup held the list for %s", elapsed)
	}
	if got[keys[0]] != nil || got[keys[1]] == nil || got[keys[1]].ServerCount != 2 {
		t.Fatalf("optional setup after one blocked read = %#v", got)
	}
	start = time.Now()
	got = lookup.Resolve(keys)
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("repeat request waited on the same blocked read for %s", elapsed)
	}
	if calls.Load() != 2 || got[keys[1]] == nil {
		t.Fatalf("read restarted or completed setup lost: calls=%d, result=%#v", calls.Load(), got)
	}
}

func TestSetupLookupBoundsBlockedReaders(t *testing.T) {
	blocked := make(chan struct{})
	t.Cleanup(func() { close(blocked) })
	var active, peak atomic.Int32
	lookup := newSetupLookup(func(string, string, icon.Source) *contract.ProjectSetup {
		n := active.Add(1)
		for {
			old := peak.Load()
			if n <= old || peak.CompareAndSwap(old, n) {
				break
			}
		}
		<-blocked
		active.Add(-1)
		return nil
	})
	keys := make([]setupLookupKey, MaxProjectSetupLookups)
	for i := range keys {
		keys[i].path = fmt.Sprintf("/blocked-%d", i)
	}
	lookup.Resolve(keys)
	lookup.Resolve(keys)
	if got := peak.Load(); got != MaxProjectSetupReaders {
		t.Fatalf("blocked setup reads reached %d workers; want %d", got, MaxProjectSetupReaders)
	}
	if got := len(lookup.entries); got != MaxProjectSetupLookups {
		t.Fatalf("remembered %d setup paths; want %d", got, MaxProjectSetupLookups)
	}
}

func TestPlacesRouteReturnsAPlaceWhileItsSetupReadStalls(t *testing.T) {
	home := placesHome(t)
	state := filepath.Join(home, ".config", "clawdline-next")
	dir := filepath.Join(home, "projects", "blocked")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	registry := projects.OpenPlaceRegistry(state)
	if _, err := registry.Add([]string{dir}, time.Now()); err != nil {
		t.Fatal(err)
	}
	blocked := make(chan struct{})
	t.Cleanup(func() { close(blocked) })
	s := &Server{cfg: config.Config{Dir: state}, icons: &icon.Registry{}}
	s.projectReaders().setups.read = func(string, string, icon.Source) *contract.ProjectSetup {
		<-blocked
		return &contract.ProjectSetup{}
	}
	start := time.Now()
	rec := httptest.NewRecorder()
	s.placesRoute(rec, httptest.NewRequest(http.MethodGet, "/v1/places", nil))
	if time.Since(start) > 2*time.Second || rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), dir) {
		t.Fatalf("blocked setup stopped place list: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"setup":`) {
		t.Fatalf("unfinished setup was claimed complete: %s", rec.Body.String())
	}
}
