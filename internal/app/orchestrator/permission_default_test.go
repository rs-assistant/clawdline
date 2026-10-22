package orchestrator

import (
	"slices"
	"testing"
)

// A brief that names no permission mode runs under `ask`. `full` is
// bypassPermissions for Claude and --ask-for-approval never for Codex, so it
// is only ever what a caller asked for by name.
func TestABriefThatNamesNoPermissionModeAsks(t *testing.T) {
	b, _ := newTestBroker(t)
	project := t.TempDir()
	id := "b8000000-0000-4000-8000-0000000000a1"
	for _, assistant := range []string{"claude", "codex"} {
		writeBrief(t, b, id, project, map[string]any{"assistant": assistant})
		r, err := b.ReadDraft(id)
		if err != nil {
			t.Fatalf("%s: %v", assistant, err)
		}
		if r.PermissionMode != "ask" {
			t.Fatalf("%s: an unnamed mode became %q", assistant, r.PermissionMode)
		}
		args := permissionArgs(r, project, "")
		if slices.Contains(args, "bypassPermissions") || slices.Contains(args, "never") {
			t.Fatalf("%s: an unnamed mode launched with %v", assistant, args)
		}
	}
	writeBrief(t, b, id, project, map[string]any{"assistant": "claude", "permission_mode": "full"})
	if r, err := b.ReadDraft(id); err != nil || r.PermissionMode != "full" {
		t.Fatalf("a named full was not kept: %q (%v)", r.PermissionMode, err)
	}
}
