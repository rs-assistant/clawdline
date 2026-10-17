package http

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/skillfile"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/skills"
)

// The clawdline skill, offered from the console (skill.schema.json).
//
// Until now the only way in was `clawdline skill install`, which a person had
// to know existed: a fresh install left every assistant on the machine
// without a word about Notes, dispatch or delivery receipts, and a snippet
// that said 「整理成 Clawdline 便條紙」 reached an assistant that had never
// heard of one. The console can see that and offer it; the press is still the
// person's (DG-10), and it does exactly what the command does, to the two
// states the command would change without asking anything: nothing there, or
// an older stub of ours. A file that was edited, or was never ours, is left
// for the person to move — a button is the wrong place to decide about it.

// skillTargets is where the stub goes on this machine. A test replaces it.
func (s *Server) skillTargets() ([]skillfile.Paths, error) {
	if s.skillPaths != nil {
		return skillfile.Targets(s.skillPaths()), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return skillfile.Targets(skillfile.Paths{
		Home:     home,
		StateDir: s.cfg.Dir,
		// The Swift app's settings directory, spelled as that app always
		// spelled it (cmd/clawdline foreignDirs).
		Foreign: []string{filepath.Join(home, ".config", "clawdline")},
	}), nil
}

func skillAssistant(p skillfile.Paths) string {
	if p.Target == skillfile.TargetAgents {
		return "codex"
	}
	return "claude"
}

func skillTargetPath(p skillfile.Paths) string {
	if p.Target == skillfile.TargetAgents {
		return skillfile.AgentsStubPath(p.Home)
	}
	return skillfile.StubPath(p.Home)
}

func readSkillTarget(p skillfile.Paths, stub []byte) contract.SkillTarget {
	out := contract.SkillTarget{Assistant: skillAssistant(p), Path: skillTargetPath(p), State: "unreadable"}
	if state, err := skillfile.Status(p, stub); err == nil {
		out.State = string(state)
	}
	return out
}

func (s *Server) skillRoute(w http.ResponseWriter, r *http.Request) {
	if !requireSkillLocal(w, r) {
		return
	}
	if r.URL.Path != "/v1/skill" {
		writeNoSuchRoute(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeRefusal(w, http.StatusMethodNotAllowed, "bad_request", "The skill is read with GET and installed with POST /v1/skill/install.")
		return
	}
	targets, err := s.skillTargets()
	if err != nil {
		writeRawRefusal(w, http.StatusInternalServerError, "skill_unreadable", err.Error())
		return
	}
	stub := skills.Stub()
	reply := contract.SkillReply{Targets: []contract.SkillTarget{}}
	for _, p := range targets {
		reply.Targets = append(reply.Targets, readSkillTarget(p, stub))
	}
	writeJSON(w, reply)
}

func (s *Server) skillInstallRoute(w http.ResponseWriter, r *http.Request) {
	if !requireSkillLocal(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "bad_request", "The skill is installed with POST.")
		return
	}
	targets, err := s.skillTargets()
	if err != nil {
		writeRawRefusal(w, http.StatusInternalServerError, "skill_unreadable", err.Error())
		return
	}
	s.skillMu.Lock()
	defer s.skillMu.Unlock()
	stub := skills.Stub()
	reply := contract.SkillReply{Targets: []contract.SkillTarget{}}
	for _, p := range targets {
		// Read again under the lock: the state the person saw may be minutes
		// old, and only the two states a press may change are written.
		before := readSkillTarget(p, stub)
		outcome := "skipped"
		if before.State == string(skillfile.StateMissing) || before.State == string(skillfile.StateStale) {
			done, err := skillfile.Install(p, stub, time.Now())
			switch {
			case err == nil:
				outcome = string(done.Outcome)
			case errors.Is(err, skillfile.ErrChanged):
				// Somebody's file arrived between the read and the write.
				outcome = "skipped"
			default:
				outcome = "failed"
			}
		}
		after := readSkillTarget(p, stub)
		after.Outcome = outcome
		reply.Targets = append(reply.Targets, after)
	}
	writeJSON(w, reply)
}

// requireSkillLocal is requireLocal with this route's own sentence: the
// files are the person's assistant configuration, not a device list.
func requireSkillLocal(w http.ResponseWriter, r *http.Request) bool {
	v := accessOf(r).verdict
	if !v.Allowed {
		writeAuthRefusal(w, http.StatusUnauthorized, "unauthorized", unauthorizedMessage)
		return false
	}
	if !v.Local {
		writeAuthRefusal(w, http.StatusForbidden, "forbidden", "Only this machine's own token may read or install the assistants' skill.")
		return false
	}
	return true
}
