package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/release"
	"github.com/sainteye/clawdline/internal/adapters/updatecheck"
	"github.com/sainteye/clawdline/internal/config"
)

func TestOnlyADevBuildAsksForItsRelease(t *testing.T) {
	f, _ := newGateFixture(t)
	s, _, _ := releaseUpdateServer(t, f.g.dir, "v0.11.0")
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/update/dev-release", s.updateDevReleaseRoute)
	h := f.g.wrap(mux)
	headers := map[string]string{"Authorization": "Bearer " + f.send, "Content-Type": "application/json"}
	if rec := (call{method: http.MethodGet, path: "/v1/update/dev-release", headers: headers}).do(h); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET: %d", rec.Code)
	}
	// A release install running a release, not a dev build.
	if rec := (call{path: "/v1/update/dev-release", headers: headers}).do(h); rec.Code != http.StatusConflict ||
		!strings.Contains(rec.Body.String(), codeNotADevBuild) {
		t.Fatalf("a release asked for a release of itself: %d %s", rec.Code, rec.Body)
	}
	// A source build.
	plain := &Server{cfg: config.Config{Dir: t.TempDir()}, update: &updatecheck.Checker{Disabled: true}}
	plain.updateOnce.Do(func() {})
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/update/dev-release", nil)
	r = r.WithContext(context.WithValue(r.Context(), accessKey{}, access{machine: true}))
	plain.updateDevReleaseRoute(rec, r)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), codeNotADevBuild) {
		t.Fatalf("a source build: %d %s", rec.Code, rec.Body)
	}
}

func TestTheSameCommitIsTheSameReleaseAssignment(t *testing.T) {
	v, _ := release.ParseVersion("v1.0.7-dev.4c41b18")
	commit := "4c41b18ae9f0ecd4daccc1b55dc044fe7b792f80"
	a, b := devReleaseAssignment("/repo", commit, v), devReleaseAssignment("/repo", commit, v)
	if a.RequestID != b.RequestID || !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-a[0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(a.RequestID) {
		t.Fatalf("request id %q / %q", a.RequestID, b.RequestID)
	}
	other := devReleaseAssignment("/repo", strings.Repeat("e", 40), v)
	if other.RequestID == a.RequestID {
		t.Fatal("two commits share one assignment")
	}
	if !strings.Contains(a.Assignment.Objective, commit) || !strings.Contains(a.Assignment.Objective, "v1.0.7") ||
		!strings.Contains(a.Assignment.Constraints, "the person's") {
		t.Fatalf("the brief: %+v", a.Assignment)
	}
}
