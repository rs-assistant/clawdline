package http

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/contract"
)

// A dev build is told apart from the release it would go back to by ancestry:
// past it, short of it, beside it, or the same commit. Release versions cannot
// say which, since every dev build sorts before its release.
func TestADevBuildIsPlacedAgainstTheReleaseByAncestry(t *testing.T) {
	repo := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@example.invalid",
			"-c", "commit.gpgsign=false"}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	commit := func(msg string) string {
		git("commit", "-q", "--allow-empty", "-m", msg)
		return git("rev-parse", "HEAD")
	}
	git("init", "-q", "-b", "main")
	older := commit("older")
	release := commit("release")
	ahead1 := commit("dev 1")
	ahead2 := commit("dev 2")
	git("checkout", "-q", "-b", "side", older)
	side := commit("side")

	ctx := context.Background()
	for _, c := range []struct {
		name, dev     string
		relation      contract.UpdateDevRelation
		ahead, behind int64
	}{
		{"two commits past the release", ahead2, contract.UpdateDevRelationAhead, 2, 0},
		{"one commit past it", ahead1, contract.UpdateDevRelationAhead, 1, 0},
		{"one commit short of it", older, contract.UpdateDevRelationBehind, 0, 1},
		{"beside it", side, contract.UpdateDevRelationDiverged, 1, 1},
		{"the release's own commit", release, contract.UpdateDevRelationSame, 0, 0},
	} {
		got := devStandingIn(ctx, []string{t.TempDir(), repo}, c.dev, release)
		if got.Relation != c.relation || got.Ahead != c.ahead || got.Behind != c.behind {
			t.Errorf("%s: %+v, want %s +%d -%d", c.name, got, c.relation, c.ahead, c.behind)
		}
	}
	missing := strings.Repeat("0", 40)
	if got := devStandingIn(ctx, []string{repo}, missing, release); got.Relation != contract.UpdateDevRelationUnknown || got.Reason == "" {
		t.Errorf("a commit no Project holds: %+v, want unknown with a reason", got)
	}
}
