package projects

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A person who presses Delete means it. An unlanded, dirty checkout that a
// live Session still uses is removed; only the main checkout is refused, and
// the branch and its commits remain.
func TestManualRemovalDeletesWhatThePersonChoseAndKeepsTheBranch(t *testing.T) {
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
	delivered := strings.TrimSpace(git(checkout, "rev-parse", "HEAD"))
	project, ok := IdentityFor(repo)
	if !ok {
		t.Fatal("project identity unavailable")
	}
	active := true
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
	var id, mainID string
	for _, row := range snap.Rows {
		switch row.Path {
		case checkout:
			id = row.WorktreeID
		case repo:
			mainID = row.WorktreeID
		}
	}
	if id == "" || mainID == "" {
		t.Fatalf("worktree missing from observation: rows=%+v checkout=%q error=%+v", snap.Rows, checkout, snap.Error)
	}
	if _, err := l.Remove(project.ID, mainID); err == nil {
		t.Fatal("removed the main checkout")
	}
	if err := os.WriteFile(filepath.Join(checkout, "late.txt"), []byte("uncommitted\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Remove(project.ID, id); err != nil {
		t.Fatalf("an unlanded, dirty, in-use checkout the person chose to delete was kept: %v", err)
	}
	if _, err := os.Stat(checkout); !os.IsNotExist(err) {
		t.Fatalf("checkout remains: %v", err)
	}
	if got := strings.TrimSpace(git(repo, "rev-parse", "refs/heads/"+branch)); got != delivered {
		t.Fatalf("branch moved or vanished: %q, want %q", got, delivered)
	}
}
