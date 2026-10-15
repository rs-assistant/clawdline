package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/release"
	"github.com/sainteye/clawdline/internal/contract"
)

// devBuild writes what `clawdline dev install` builds: an unsigned manifest
// whose daemon archive is a file:// URL beside it.
func (f *fixture) devBuild(version, commit string) string {
	f.t.Helper()
	dir := f.t.TempDir()
	archive := daemonArchive(f.t, commit)
	name := "clawdline_" + version + "_linux_amd64.tar.gz"
	if err := os.WriteFile(filepath.Join(dir, name), archive, 0o644); err != nil {
		f.t.Fatal(err)
	}
	sum := sha256.Sum256(archive)
	m := release.Manifest{Version: version, Commit: commit, CommittedAt: "2026-10-15T00:00:00Z", Channel: "dev",
		Artifacts: []release.Artifact{{OS: "linux", Arch: "amd64", Kind: release.KindDaemon, Name: name,
			URL: "file://" + filepath.Join(dir, name), Size: int64(len(archive)), SHA256: hex.EncodeToString(sum[:])}}}
	body, _ := json.Marshal(m)
	path := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		f.t.Fatal(err)
	}
	f.smoke[f.env.Layout.ReleaseDir(version)] = fmt.Sprintf(`{"version":%q,"commit":%q}`, version, commit)
	return path
}

func (f *fixture) finishHealthy(commit string) {
	f.t.Helper()
	f.healthy = commit
	if err := f.env.Finish(context.Background()); err != nil {
		f.t.Fatal(err)
	}
	if got := f.apply(); got.State != contract.UpdateApplyStateHealthy {
		f.t.Fatalf("not healthy: %+v", got)
	}
}

func TestADevBuildGoesOverTheStableReleaseAndTheReleaseIsTheWayBack(t *testing.T) {
	f := newFixture(t, "v1.0.7")
	dev := "v1.0.7-dev.4c41b18"
	p, err := f.env.BeginLocal(f.devBuild(dev, commitOf('d')))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.env.Execute(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if pending, ok, _ := f.env.ReadPending(); !ok || pending.From != "v1.0.7" || pending.To != dev {
		t.Fatalf("the switch to the dev build was not handed over like an update: %+v", pending)
	}
	f.finishHealthy(commitOf('d'))
	if f.current() != dev {
		t.Fatalf("current is %s, want the dev build", f.current())
	}

	if got := ChannelFor(dev); got != ChannelStable {
		t.Fatalf("a dev build follows %s, want stable", got)
	}
	f.publish("v1.0.7", commitOf('a'), daemonArchive(t, commitOf('a')), true, nil)
	if err := f.update(Request{Auto: true}); errCode(err) != CodeDevBuildRunning {
		t.Fatalf("auto-apply took the dev build away: %v", err)
	}
	if err := f.update(Request{}); err != nil {
		t.Fatalf("the stable release it went over is not the way back: %v", err)
	}
	f.finishHealthy(commitOf('a'))
	if f.current() != "v1.0.7" {
		t.Fatalf("current is %s after going back", f.current())
	}
}

func TestOnlyADevBuildIsInstalledWithoutASignature(t *testing.T) {
	f := newFixture(t, "v1.0.7")
	path := f.devBuild("v1.0.8", commitOf('e'))
	if _, err := f.env.BeginLocal(path); errCode(err) != CodeNotADevBuild {
		t.Fatalf("an unsigned release was accepted: %v", err)
	}
	if _, ok, _ := f.env.ReadPending(); ok {
		t.Fatal("a refused dev install left an update pending")
	}
	// The lock was given back: a real dev build may follow.
	if _, err := f.env.BeginLocal(f.devBuild("v1.0.7-dev.abc1234", commitOf('f'))); err != nil {
		t.Fatalf("after a refusal: %v", err)
	}
}
