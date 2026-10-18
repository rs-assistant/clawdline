package updater

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/install"
	"github.com/sainteye/clawdline/internal/adapters/release"
	"github.com/sainteye/clawdline/internal/contract"
)

// installedApp is the macOS app setup installed, or "" when there is none.
func (e Env) installedApp() string {
	if e.GOOS != "darwin" || e.AppsDir == "" {
		return ""
	}
	app := filepath.Join(e.AppsDir, AppBundleName)
	if info, err := os.Stat(app); err == nil && info.IsDir() {
		return app
	}
	return ""
}

// stageApp downloads and unpacks the release's app bundle into
// releases/<v>/app/ when this machine has the app installed and the release
// carries one. It is swapped in only after the new daemon is healthy, and
// only while the app is not running (swapApp).
func (e Env) stageApp(ctx context.Context, m release.Manifest, dir string) (string, error) {
	if e.installedApp() == "" {
		return "", nil
	}
	a, err := m.Artifact(e.GOOS, e.GOARCH, release.KindApp)
	if err != nil {
		// A release without an app for this machine leaves the app as it is.
		return "", nil
	}
	archive, err := e.download(ctx, a)
	if err != nil {
		return "", err
	}
	defer os.Remove(archive)
	if err := e.unpackRelease(archive, dir, "app"); err != nil {
		return "", err
	}
	staged := filepath.Join(dir, "app", AppBundleName)
	if info, err := os.Stat(staged); err != nil || !info.IsDir() {
		return "", fail(CodeArchiveUnsafe, "%s has no %s at its root", a.Name, AppBundleName)
	}
	if install.SameAppShell(e.installedApp(), staged) {
		return "", nil
	}
	return staged, nil
}

// appRunning says whether any process runs from inside bundle.
func (e Env) appRunning(ctx context.Context, bundle string) (bool, error) {
	pids, err := e.appProcesses(ctx, bundle)
	return len(pids) > 0, err
}

// appProcesses lists the pids of the processes running from inside bundle.
func (e Env) appProcesses(ctx context.Context, bundle string) ([]int, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := e.Run(ctx, "ps", "-axo", "pid=,command=")
	if err != nil {
		return nil, err
	}
	prefix := bundle + "/Contents/"
	var pids []int
	for _, line := range strings.Split(string(out), "\n") {
		pid, command, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok || !strings.HasPrefix(strings.TrimSpace(command), prefix) {
			continue
		}
		if n, err := strconv.Atoi(pid); err == nil {
			pids = append(pids, n)
		}
	}
	return pids, nil
}

// appWatch answers whether the app runs for a daemon that asks every
// AppSwapPollSecondsLimit. It lists processes once and then asks the kernel
// about the pids it found, listing again when every one of them is gone or
// the listing is AppRelistSecondsLimit old. A quit and a reopen are still
// seen within one poll: the old pids are gone, so the next look lists.
type appWatch struct {
	env    Env
	alive  func(pid int) bool
	bundle string
	pids   []int
	listed time.Time
}

func (w *appWatch) running(ctx context.Context, bundle string) (bool, error) {
	now := time.Now()
	if w.env.Now != nil {
		now = w.env.Now()
	}
	if bundle == w.bundle && now.Sub(w.listed) < AppRelistSecondsLimit*time.Second {
		for _, pid := range w.pids {
			if w.alive(pid) {
				return true, nil
			}
		}
	}
	pids, err := w.env.appProcesses(ctx, bundle)
	if err != nil {
		w.pids = nil
		return false, err
	}
	w.bundle, w.pids, w.listed = bundle, pids, now
	return len(pids) > 0, nil
}

// swapApp puts the staged bundle in place of the installed one when nothing
// runs from it, keeping the replaced bundle in releases/<from>/app. It
// answers the path still staged, "" once swapped. A running app keeps its
// bundle: SettleApp swaps it after the app quits.
func (e Env) swapApp(ctx context.Context, staged, from string, running func(context.Context, string) (bool, error)) (string, error) {
	app := e.installedApp()
	if staged == "" || app == "" {
		return "", nil
	}
	if _, err := os.Stat(staged); err != nil {
		return "", nil
	}
	busy, err := running(ctx, app)
	if err != nil || busy {
		return staged, err
	}
	keep := filepath.Join(e.Layout.ReleaseDir(from), "app", AppBundleName)
	if err := os.MkdirAll(filepath.Dir(keep), 0o755); err != nil {
		return staged, err
	}
	os.RemoveAll(keep)
	if err := os.Rename(app, keep); err != nil {
		return staged, err
	}
	if err := os.Rename(staged, app); err != nil {
		// Put the old one back rather than leave no app at all.
		if rerr := os.Rename(keep, app); rerr != nil {
			return staged, errors.Join(err, rerr)
		}
		return staged, err
	}
	return "", nil
}

// SettleApp swaps in an app bundle a healthy update left staged because the
// app was running then. It answers true once the bundle is in place. It
// takes update.lock, so it never races an update; a held lock waits for the
// next call.
func (e Env) SettleApp(ctx context.Context) (bool, error) {
	return e.settleApp(ctx, e.appRunning)
}

func (e Env) settleApp(ctx context.Context, running func(context.Context, string) (bool, error)) (bool, error) {
	a, err := e.ReadApply()
	if err != nil {
		return false, err
	}
	if a.State != contract.UpdateApplyStateHealthy || a.StagedApp == "" {
		return false, nil
	}
	unlock, err := e.lock()
	if err != nil {
		return false, nil
	}
	staged, err := e.swapApp(ctx, a.StagedApp, a.From, running)
	unlock()
	if err != nil {
		return false, err
	}
	if staged != "" {
		return false, nil
	}
	if err := e.record(contract.UpdateApplyStateHealthy, a.From, a.To, nil, ""); err != nil {
		return true, err
	}
	return true, e.pruneReleases(a.To, "")
}
