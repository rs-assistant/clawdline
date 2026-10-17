package orchestrator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCallbackOutputIsBoundedOwnedAndDoesNotAcknowledgeCompletion(t *testing.T) {
	b, ctx := newTestBroker(t)
	id := "c1000000-0000-4000-8000-000000000001"
	r := Record{Protocol: Protocol, ID: id, Kind: TaskKindCallback, State: StateSuccess, CreatedAt: time.Now(), Dir: t.TempDir(), Root: &RootRef{SessionID: rootConversation}, Callback: &Callback{Argv: []string{"echo", "a b"}}}
	if err := b.save(ctx, r, HashSecret("s"), "task.queued"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(r.Dir, "output.log")
	body := strings.Repeat("completed output line\n", CallbackTailLimit)
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	got, out, err := b.ReadCallbackOutput(ctx, id, rootConversation)
	if err != nil || got.State != StateSuccess || !out.Truncated || len(out.Text) > CallbackTailLimit || !strings.HasSuffix(out.Text, "completed output line\n") {
		t.Fatalf("%+v %v", out, err)
	}
	if CallbackCommand(r) != "echo 'a b'" {
		t.Fatal(CallbackCommand(r))
	}
	if _, _, err := b.ReadCallbackOutput(ctx, id, "another conversation"); err == nil {
		t.Fatal("another Session read the log")
	}
	again, _, err := b.Record(ctx, id)
	if err != nil || again.Notice != nil {
		t.Fatalf("read changed the task: %+v %v", again, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.ReadCallbackOutput(ctx, id, rootConversation); err == nil {
		t.Fatal("expired output was presented as empty output")
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Skip("symlinks unavailable")
	}
	if _, _, err := b.ReadCallbackOutput(ctx, id, rootConversation); err == nil {
		t.Fatal("a symlink was followed")
	}
}

func TestAQueuedCallbackWithoutALogIsNotFinished(t *testing.T) {
	b, ctx := newTestBroker(t)
	id := "c1000000-0000-4000-8000-000000000002"
	r := Record{Protocol: Protocol, ID: id, Kind: TaskKindCallback, State: StateQueued, CreatedAt: time.Now(), Dir: t.TempDir(), Root: &RootRef{SessionID: rootConversation}, Callback: &Callback{Argv: []string{"true"}}}
	if err := b.save(ctx, r, HashSecret("s"), "task.queued"); err != nil {
		t.Fatal(err)
	}
	got, out, err := b.ReadCallbackOutput(ctx, id, rootConversation)
	if err != nil || got.State.Terminal() || out.Text != "" {
		t.Fatalf("%+v %v", out, err)
	}
}
