package projects

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFirstWorktreeReadListsRowsBeforeSlowObservationFinishes(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q", "-b", "main")
	git("config", "user.name", "Test")
	git("config", "user.email", "test@example.invalid")
	if err := os.WriteFile(filepath.Join(repo, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "base.txt")
	git("commit", "-qm", "base")
	checkout := filepath.Join(root, "other")
	git("worktree", "add", "-qb", "other", checkout, "HEAD")
	project, ok := IdentityFor(repo)
	if !ok {
		t.Fatal("project identity unavailable")
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	l := NewLifecycle(Ports{
		ProjectDirectories: func() []string { return []string{repo} },
		Tasks:              func() TaskEvidence { return TaskEvidence{Authoritative: true} },
		Live:               func() LiveEvidence { return LiveEvidence{Complete: true, ObservedAt: time.Now()} },
		StorageBytes: func(string, time.Duration) (int64, *Issue) {
			once.Do(func() { close(entered); <-release })
			return 1024, nil
		},
	})
	first, err := l.Snapshot(project.ID)
	if err != nil || len(first.Rows) != 2 || first.Complete || first.Counts.Rows == nil || *first.Counts.Rows != 2 {
		t.Fatalf("initial listing: %+v, %v", first, err)
	}
	for _, row := range first.Rows {
		if row.Cleanup.Eligible || row.Status.Complete || row.WorktreeID == "" {
			t.Fatalf("preview claimed evidence: %+v", row)
		}
	}
	done := make(chan *Snapshot, 1)
	go func() {
		result, _ := l.Refresh(project.ID)
		done <- result
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("observation did not reach the slow step")
	}
	progress, err := l.Snapshot(project.ID)
	if err != nil || progress.Error == nil || progress.Error.Code != "observation_in_progress" ||
		len(progress.Rows) != 2 || progress.Complete {
		t.Fatalf("progress snapshot: %+v, %v", progress, err)
	}
	close(release)
	select {
	case final := <-done:
		if final.Error != nil || len(final.Rows) != 2 || final.Counts.Rows == nil {
			t.Fatalf("final observation: %+v", final)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("observation did not finish")
	}
}
