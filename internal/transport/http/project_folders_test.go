package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/app/cloudops"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/auth"
	"github.com/sainteye/clawdline/internal/domain/icon"
)

func TestProjectFolderPickerListsDirectoriesOnly(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"project", ".private"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("contents"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "project"), filepath.Join(dir, "linked")); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/project-folders?path="+url.QueryEscape(dir), nil)
	out := httptest.NewRecorder()
	(&Server{}).projectFoldersRoute(out, req)
	var page struct {
		Path    string          `json:"path"`
		Folders []projectFolder `json:"folders"`
	}
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &page) != nil {
		t.Fatalf("folders: %d %s", out.Code, out.Body.String())
	}
	if page.Path != dir || len(page.Folders) != 1 || page.Folders[0].Name != "project" {
		t.Fatalf("folders = %#v", page)
	}

	bad := httptest.NewRecorder()
	(&Server{}).projectFoldersRoute(bad, httptest.NewRequest(http.MethodGet, "/v1/project-folders?path=relative", nil))
	if bad.Code != 400 {
		t.Fatalf("relative path = %d %s", bad.Code, bad.Body.String())
	}
}

func TestPairedSenderCanAddProjectWithoutRemovingItsSettings(t *testing.T) {
	home := placesHome(t)
	state := filepath.Join(home, ".config", "clawdline-next")
	dir := filepath.Join(home, "projects", "new")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: config.Config{Dir: state}, icons: &icon.Registry{}}
	request := func(method string, allowed bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/v1/places", strings.NewReader(`{"paths":["`+dir+`"]}`))
		if allowed {
			req = req.WithContext(context.WithValue(req.Context(), accessKey{}, access{verdict: auth.Verdict{Allowed: true, Caps: auth.NewCaps(auth.Read, auth.Send)}}))
		}
		out := httptest.NewRecorder()
		s.placesRoute(out, req)
		return out
	}
	if got := request(http.MethodPost, false); got.Code != 403 {
		t.Fatalf("unpaired add = %d", got.Code)
	}
	if got := request(http.MethodPost, true); got.Code != 200 {
		t.Fatalf("paired add = %d %s", got.Code, got.Body.String())
	}
	if got := request(http.MethodDelete, true); got.Code != 200 {
		t.Fatalf("paired remove = %d %s", got.Code, got.Body.String())
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("removal touched directory: %v", err)
	}
}

func TestCloudProjectFolderChoiceAndAddUseTheSelectedMachine(t *testing.T) {
	s := projectCloudStandIn(t)
	dir := filepath.Join(filepath.Dir(s.server.cfg.Dir), "new-project")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	machine := cloudops.MachineReplySession
	read, _ := s.ask(t, 1, map[string]any{"type": "project-folders", "session": machine, "request": "folders", "path": filepath.Dir(dir)})
	if !read.OK() || !strings.Contains(string(read.Payload), "new-project") {
		t.Fatalf("folder read: %d %s", read.Status, read.Payload)
	}
	add := func(seq uint64) cloudops.Answer {
		answer, _ := s.ask(t, seq, map[string]any{"type": "project-add", "session": machine, "request": "add", "path": dir})
		return answer
	}
	s.bridge.AllowCommands = func() bool { return false }
	if result := add(2); result.OK() {
		t.Fatal("Cloud add passed the command gate while off")
	}
	s.bridge.AllowCommands = func() bool { return true }
	if result := add(3); !result.OK() {
		t.Fatalf("Cloud add: %d %s", result.Status, result.Payload)
	}
	rows, err := s.server.projectReaders().registry.List()
	if err != nil || len(rows) != 1 || rows[0].Path != dir {
		t.Fatalf("registered = %#v, %v", rows, err)
	}
}
