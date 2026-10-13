package projects

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestManualRemovalRechecksDirtyBytesAndKeepsTheBranch(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(root, "repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return string(out)
	}
	git(repo, "init", "-q", "-b", "main")
	git(repo, "config", "user.name", "Test")
	git(repo, "config", "user.email", "test@example.invalid")
	if err := os.WriteFile(filepath.Join(repo, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(repo, "add", "base.txt")
	git(repo, "commit", "-qm", "base")
	base := strings.TrimSpace(git(repo, "rev-parse", "HEAD"))
	branch := worktreeBranch(testTask)
	own := filepath.Join(root, "worktrees")
	checkout := filepath.Join(own, testSlug, testTask)
	if err := os.MkdirAll(filepath.Dir(checkout), 0o755); err != nil {
		t.Fatal(err)
	}
	git(repo, "worktree", "add", "-qb", branch, checkout, "HEAD")
	if err := os.WriteFile(filepath.Join(checkout, "delivered.txt"), []byte("delivered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(checkout, "add", "delivered.txt")
	git(checkout, "commit", "-qm", "delivery")
	git(repo, "merge", "-q", "--no-ff", "-m", "land", branch)
	project, ok := IdentityFor(repo)
	if !ok {
		t.Fatal("project identity unavailable")
	}
	active := false
	l := NewLifecycle(Ports{
		ManagedWorktreeRoots: ManagedWorktreeRoots(own),
		ProjectDirectories:   func() []string { return []string{repo} },
		Tasks: func() TaskEvidence {
			return TaskEvidence{Authoritative: true, Tasks: []Task{{
				ID: testTask, State: "success", Worktree: &TaskWorktree{Path: checkout, Branch: branch, Base: base},
			}}}
		},
		Live: func() LiveEvidence {
			evidence := LiveEvidence{Complete: true, ObservedAt: time.Now()}
			if active {
				evidence.Sessions = []LiveSession{{CWD: checkout}}
			}
			return evidence
		},
	})
	snap, err := l.Refresh(project.ID)
	if err != nil {
		t.Fatal(err)
	}
	var id string
	for _, row := range snap.Rows {
		if row.Path == checkout {
			id = row.WorktreeID
		}
	}
	if id == "" {
		t.Fatalf("worktree missing from observation: rows=%+v checkout=%q error=%+v", snap.Rows, checkout, snap.Error)
	}
	// The row was clean when the button appeared. A later edit must veto the press.
	if err := os.WriteFile(filepath.Join(checkout, "late.txt"), []byte("keep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := l.RemoveLanded(project.ID, id); err == nil {
		t.Fatal("removed a checkout changed after the observation")
	}
	if _, err := os.Stat(filepath.Join(checkout, "late.txt")); err != nil {
		t.Fatalf("late edit lost: %v", err)
	}
	if err := os.Remove(filepath.Join(checkout, "late.txt")); err != nil {
		t.Fatal(err)
	}
	active = true
	if _, err := l.RemoveLanded(project.ID, id); err == nil {
		t.Fatal("removed a checkout used by a live Session")
	}
	active = false
	if _, err := l.RemoveLanded(project.ID, id); err != nil {
		t.Fatalf("clean landed checkout was kept: %v", err)
	}
	if _, err := os.Stat(checkout); !os.IsNotExist(err) {
		t.Fatalf("checkout remains: %v", err)
	}
	git(repo, "show-ref", "--verify", "refs/heads/"+branch)
}
