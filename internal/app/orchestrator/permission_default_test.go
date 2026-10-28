package orchestrator

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
)

func TestDispatchUsesMachinePermissionUnlessTaskExplicitlyOverrides(t *testing.T) {
	b := &Broker{Dir: t.TempDir()}
	p := Protocol
	claims := []string{}
	root := json.RawMessage(`{"session_id":"10000000-0000-4000-8000-000000000001","assistant":"claude"}`)
	d := draft{Protocol: &p, TaskID: "11111111-1111-4111-8111-111111111111", Assistant: "claude", ProjectDir: t.TempDir(), Title: "Run a focused task", Instructions: "Inspect the parser", Claims: &claims, Root: &root}
	for _, mode := range []string{"full", "ask", "edits"} {
		if _, err := nextconfig.Open(b.Dir).Set(map[string]any{"orchestrator_permission": mode}); err != nil {
			t.Fatal(err)
		}
		got, err := b.admit(d.TaskID, d, false, false)
		if err != nil || got.PermissionMode != mode {
			t.Fatalf("default %s: %s %v", mode, got.PermissionMode, err)
		}
		explicit := d
		explicit.PermissionMode = "ask"
		got, err = b.admit(d.TaskID, explicit, false, false)
		if err != nil || got.PermissionMode != "ask" {
			t.Fatalf("explicit: %s %v", got.PermissionMode, err)
		}
	}
}

// Turning the full-permission default off restores native permission behavior instead
// of introducing a second permission policy over the person's provider settings.
func TestAskDispatchLeavesNativePermissionSettingsUntouched(t *testing.T) {
	for _, assistant := range []string{"claude", "codex"} {
		args := permissionArgs(Record{Assistant: assistant, PermissionMode: "ask"}, "/tmp/fixture", "")
		if len(args) != 0 {
			t.Fatalf("%s overrides native settings: %v", assistant, args)
		}
	}
}

// A brief that names no permission mode, on a machine whose
// `orchestrator_permission` is unset, runs under `ask`. `full` is
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
