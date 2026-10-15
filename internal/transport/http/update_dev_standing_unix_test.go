//go:build !windows

package http

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/contract"
)

// A Place whose git never answers — a cloud-storage folder did not for twenty
// seconds — does not keep the reading from the checkout that holds the commits.
func TestAStuckPlaceDoesNotHideTheCheckout(t *testing.T) {
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	repo, dev, release := devAndRelease(t)
	stuck := t.TempDir()
	bin := t.TempDir()
	script := "#!/bin/sh\nif [ \"$2\" = \"" + stuck + "\" ]; then exec sleep 60; fi\nexec \"" + realGit + "\" \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got := devStandingIn(ctx, []string{stuck, repo}, dev, release)
	if got.Relation != contract.UpdateDevRelationAhead || got.Ahead != 1 {
		t.Fatalf("with a stuck Place listed first: %+v, want ahead by 1", got)
	}
}
