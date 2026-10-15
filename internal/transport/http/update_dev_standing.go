package http

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/release"
	"github.com/sainteye/clawdline/internal/adapters/release/updater"
	"github.com/sainteye/clawdline/internal/contract"
)

// A dev build (`clawdline dev install`) is named v1.0.7-dev.<commit> and sorts
// before v1.0.7, so the release reads as available whether the dev build is
// twelve commits past it or five commits short of it. The version panel and
// the session list's strip need to tell those apart — "go back to v1.0.7"
// drops work, "update to v1.0.7" gains it — and only git can: release
// versions say nothing about ancestry. This is that reading.

// devStandingMemo holds one answer. Two commits' histories never change, so
// the answer for a pair stands until the running build or the latest release
// is another commit; an unknown one is kept too, so a machine where no Project
// holds the commits does not run git on every read of GET /v1/update.
type devStandingMemo struct {
	mu     sync.Mutex
	dev    string
	latest string
	answer contract.UpdateDevStanding
}

// withDevStanding adds `dev` to a release install's status while it runs a
// dev build and the latest release names its commit.
func (s *Server) withDevStanding(st contract.UpdateStatus) contract.UpdateStatus {
	v, err := release.ParseVersion(st.Running.Version)
	if err != nil || !v.IsDev() || st.Latest.Stamp == "" || st.Running.Stamp == "" {
		return st
	}
	m := &s.devStanding
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.dev != st.Running.Stamp || m.latest != st.Latest.Stamp {
		ctx, cancel := context.WithTimeout(context.Background(), updater.FetchTimeoutSecondsLimit*time.Second)
		repos := s.placeRepositories(ctx)
		m.answer = devStandingIn(ctx, repos, st.Running.Stamp, st.Latest.Stamp)
		cancel()
		m.dev, m.latest = st.Running.Stamp, st.Latest.Stamp
	}
	answer := m.answer
	st.Dev = &answer
	return st
}

// placeRepositories is the top of every repository a known Project is in, once.
func (s *Server) placeRepositories(ctx context.Context) []string {
	var tops []string
	seen := map[string]bool{}
	for _, place := range s.squadListedPlaces(ctx) {
		top, err := gitIn(ctx, place.Path, "rev-parse", "--show-toplevel")
		if err != nil || seen[top] {
			continue
		}
		seen[top] = true
		tops = append(tops, top)
	}
	return tops
}

func gitIn(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...).Output()
	return strings.TrimSpace(string(out)), err
}

// devStandingIn is how dev stands against latest in the first of repos that
// holds both commits. It does not fetch: the dev build was built from a
// checkout here, and a release commit that checkout has never seen is
// reported unknown rather than waited for.
func devStandingIn(ctx context.Context, repos []string, dev, latest string) contract.UpdateDevStanding {
	unknown := func(why string) contract.UpdateDevStanding {
		return contract.UpdateDevStanding{Relation: contract.UpdateDevRelationUnknown, Reason: why}
	}
	if dev == latest {
		return contract.UpdateDevStanding{Relation: contract.UpdateDevRelationSame}
	}
	for _, repo := range repos {
		if _, err := gitIn(ctx, repo, "cat-file", "-e", dev+"^{commit}"); err != nil {
			continue
		}
		if _, err := gitIn(ctx, repo, "cat-file", "-e", latest+"^{commit}"); err != nil {
			continue
		}
		// Left is the release's own commits, right the dev build's.
		out, err := gitIn(ctx, repo, "rev-list", "--left-right", "--count", latest+"..."+dev)
		if err != nil {
			return unknown("git rev-list did not answer in " + repo + ": " + err.Error())
		}
		fields := strings.Fields(out)
		if len(fields) != 2 {
			return unknown("git rev-list answered " + strconv.Quote(out) + " in " + repo)
		}
		behind, err1 := strconv.ParseInt(fields[0], 10, 64)
		ahead, err2 := strconv.ParseInt(fields[1], 10, 64)
		if err1 != nil || err2 != nil {
			return unknown("git rev-list answered " + strconv.Quote(out) + " in " + repo)
		}
		standing := contract.UpdateDevStanding{Ahead: ahead, Behind: behind}
		switch {
		case ahead == 0 && behind == 0:
			standing.Relation = contract.UpdateDevRelationSame
		case behind == 0:
			standing.Relation = contract.UpdateDevRelationAhead
		case ahead == 0:
			standing.Relation = contract.UpdateDevRelationBehind
		default:
			standing.Relation = contract.UpdateDevRelationDiverged
		}
		return standing
	}
	return unknown("no Project on this machine holds both the dev build's commit and the release's")
}
