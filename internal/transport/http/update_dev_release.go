package http

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/release"
	"github.com/sainteye/clawdline/internal/adapters/release/updater"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/contract"
)

// The refusals of POST /v1/update/dev-release.
const (
	codeNotADevBuild      = "not_a_dev_build"
	codeDevProjectMissing = "dev_project_not_found"
	codeDevNotOnMain      = "dev_commit_not_on_main"
)

// updateDevReleaseRoute is POST /v1/update/dev-release: the version panel's
// "Release this version" on a machine running a dev build (`clawdline dev
// install`, docs/updates.md). It does not release anything itself. It finds
// the Project whose repository holds the running commit, requires the commit
// to be on its origin/main, and opens a Root Session there whose brief is to
// cut a release of exactly that commit by docs/releasing.md — leaving the
// release environment's approval and the publish to the person. The same
// commit pressed again answers the Root it already opened.
func (s *Server) updateDevReleaseRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "a release is asked for with POST")
		return
	}
	if !machineAuthed(r) && !maySend(r) {
		writeAuthRefusal(w, http.StatusForbidden, "forbidden", "This device may read, and not send.")
		return
	}
	st := s.updateStatus()
	v, err := release.ParseVersion(st.Running.Version)
	if st.InstallKind != contract.UpdateInstallKindRelease || err != nil || !v.IsDev() {
		writeRefusal(w, http.StatusConflict, codeNotADevBuild,
			"this machine is not running a dev build; a dev build is installed with clawdline dev install")
		return
	}
	commit := st.Running.Stamp
	if len(commit) != 40 {
		writeRefusal(w, http.StatusConflict, codeNotADevBuild, "the running dev build does not name its commit")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*updater.FetchTimeoutSecondsLimit*time.Second)
	defer cancel()
	dir, onMain := s.devReleaseRepository(ctx, commit)
	if dir == "" {
		writeRefusal(w, http.StatusConflict, codeDevProjectMissing,
			"no Project on this machine holds the running commit; open the clawdline checkout as a Project first")
		return
	}
	if !onMain {
		writeRawRefusal(w, http.StatusConflict, codeDevNotOnMain,
			fmt.Sprintf("%s is not on origin/main in %s: push and merge it first, since a release is cut from main", commit[:7], dir))
		return
	}
	req := devReleaseAssignment(dir, commit, v)
	a, _, err := s.broker.OpenRootAssignment(context.WithoutCancel(r.Context()), req.RequestID, req)
	if err != nil {
		writeBrokerError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(contract.UpdateDevReleaseResult{
		Commit: commit, ProjectDir: dir, RootAssignmentID: a.ID, Label: a.Label})
}

// devReleaseRepository is the repository of a known Project that holds
// commit, and whether the commit is on its origin/main after a fetch.
func (s *Server) devReleaseRepository(ctx context.Context, commit string) (string, bool) {
	git := func(dir string, args ...string) (string, error) {
		out, err := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...).Output()
		return strings.TrimSpace(string(out)), err
	}
	seen := map[string]bool{}
	for _, place := range s.squadListedPlaces(ctx) {
		top, err := git(place.Path, "rev-parse", "--show-toplevel")
		if err != nil || seen[top] {
			continue
		}
		seen[top] = true
		if _, err := git(top, "cat-file", "-e", commit+"^{commit}"); err != nil {
			continue
		}
		// A fetch moves only refs/remotes; the checkout is left as it is.
		_, _ = git(top, "fetch", "--quiet", "origin", "main")
		_, err = git(top, "merge-base", "--is-ancestor", commit, "refs/remotes/origin/main")
		return top, err == nil
	}
	return "", false
}

// devReleaseAssignment is the Root's brief. Its request id is derived from
// the commit, so a second press for the same commit is the same assignment.
func devReleaseAssignment(dir, commit string, v release.Version) orchestrator.RootAssignmentRequest {
	sum := sha256.Sum256([]byte("dev-release\x00" + commit))
	id := hex.EncodeToString(sum[:16])
	id = id[:8] + "-" + id[8:12] + "-4" + id[13:16] + "-a" + id[17:20] + "-" + id[20:32]
	base := v
	base.Pre = ""
	return orchestrator.RootAssignmentRequest{
		RequestID: id, Assistant: "claude", Model: "default", ProjectDir: dir,
		Label: "Release " + commit[:7],
		Assignment: orchestrator.Assignment{
			Objective: "Cut a Clawdline release of exactly commit " + commit + ", the dev build the person tested on this machine " +
				"(it runs as " + v.String() + ", over " + base.String() + "). The person pressed \"Release this version\" in the version panel.",
			Scope: "Follow docs/releasing.md for this commit and nothing else: choose the version (propose the next patch after the " +
				"newest published release unless the changes since it call for a minor, and say why), make sure CI is green for the " +
				"commit, write docs/release-notes/<version>.md when the changes deserve it, run the Release workflow from main at " +
				"this commit, run tools/release/gate.sh <version>, and stop before publishing.",
			Constraints: "Do not release any other commit. Approving the `release` environment and publishing (gate.sh --publish) " +
				"are the person's: when you reach either, send them a notification with clawdline notify and wait. Do not install " +
				"the release on this machine; the person goes back to stable from the version panel.",
			RelevantReferences: "docs/releasing.md, docs/updates.md (Development machines, dev builds), .github/workflows/release.yml, " +
				"tools/release/gate.sh.",
			Acceptance: "A draft GitHub Release of " + commit[:7] + " exists with every artifact, its gate.sh scan passed, and the person " +
				"has been told it is ready to approve and publish, with the version and what it contains.",
		},
	}
}

