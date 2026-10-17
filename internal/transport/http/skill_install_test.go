package http

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/skillfile"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/skills"
)

func skillTestServer(t *testing.T) (*Server, skillfile.Paths) {
	t.Helper()
	root := t.TempDir()
	p := skillfile.Paths{
		Home:     filepath.Join(root, "home"),
		StateDir: filepath.Join(root, "home", ".config", "clawdline-next"),
		Foreign:  []string{filepath.Join(root, "home", ".config", "clawdline")},
	}
	s := &Server{cfg: config.Config{Dir: p.StateDir}, skillPaths: func() skillfile.Paths { return p }}
	return s, p
}

func skillReply(t *testing.T, s *Server, route func(http.ResponseWriter, *http.Request), method, path string) map[string]contract.SkillTarget {
	t.Helper()
	rec := onboardingRequest(t, s, route, method, path, "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s %s = %d %s", method, path, rec.Code, rec.Body)
	}
	var reply contract.SkillReply
	if err := json.Unmarshal(rec.Body.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	out := map[string]contract.SkillTarget{}
	for _, target := range reply.Targets {
		out[string(target.Assistant)] = target
	}
	return out
}

// A paired browser — the phone over Cloud, or a device on the network — can
// neither read nor install: both files are this machine's person's own
// assistant configuration.
func TestOnlyThisMachineReadsOrInstallsTheSkill(t *testing.T) {
	s, p := skillTestServer(t)
	for _, c := range []struct {
		route  func(http.ResponseWriter, *http.Request)
		method string
		path   string
	}{
		{s.skillRoute, http.MethodGet, "/v1/skill"},
		{s.skillInstallRoute, http.MethodPost, "/v1/skill/install"},
	} {
		if rec := onboardingRequest(t, s, c.route, c.method, c.path, "", false); rec.Code != http.StatusForbidden {
			t.Fatalf("paired browser %s %s = %d %s", c.method, c.path, rec.Code, rec.Body)
		}
	}
	if _, err := os.Lstat(skillfile.StubPath(p.Home)); !os.IsNotExist(err) {
		t.Fatalf("a refused install wrote: %v", err)
	}
}

// One press installs where nothing was, and leaves somebody's own file alone.
func TestThePressInstallsOnlyWhatIsMissingOrOurs(t *testing.T) {
	s, p := skillTestServer(t)
	theirs := []byte("somebody's own codex skill\n")
	if err := os.MkdirAll(filepath.Dir(skillfile.AgentsStubPath(p.Home)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillfile.AgentsStubPath(p.Home), theirs, 0o644); err != nil {
		t.Fatal(err)
	}

	before := skillReply(t, s, s.skillRoute, http.MethodGet, "/v1/skill")
	if before["claude"].State != "missing" || before["codex"].State != "other" {
		t.Fatalf("before = %+v", before)
	}
	if _, err := os.Lstat(filepath.Dir(skillfile.StubPath(p.Home))); !os.IsNotExist(err) {
		t.Fatalf("reading made the skill directory: %v", err)
	}

	after := skillReply(t, s, s.skillInstallRoute, http.MethodPost, "/v1/skill/install")
	if c := after["claude"]; c.State != "current" || c.Outcome != "installed" {
		t.Fatalf("claude = %+v", c)
	}
	if c := after["codex"]; c.State != "other" || c.Outcome != "skipped" {
		t.Fatalf("codex = %+v", c)
	}
	if got, _ := os.ReadFile(skillfile.StubPath(p.Home)); string(got) != string(skills.Stub()) {
		t.Fatal("the installed stub is not this build's")
	}
	if got, _ := os.ReadFile(skillfile.AgentsStubPath(p.Home)); string(got) != string(theirs) {
		t.Fatalf("somebody's file was replaced: %q", got)
	}

	again := skillReply(t, s, s.skillInstallRoute, http.MethodPost, "/v1/skill/install")
	if c := again["claude"]; c.State != "current" || c.Outcome != "skipped" {
		t.Fatalf("a second press = %+v", c)
	}
}
