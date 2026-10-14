package orchestrator

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
)

func TestCompletionPushUsesSessionAndPreference(t *testing.T) {
	var pushes []string
	b := &Broker{
		PushEnabled: func(key string) bool { return key != "smart_notifications" },
		Push: func(_ context.Context, _, body, terminal, _ string) (int, int, error) {
			pushes = append(pushes, terminal+":"+body)
			return 1, 0, nil
		},
	}
	ctx := context.Background()
	e := store.Effect{Kind: EffectSessionDeliveryPush, Subject: "%1", Payload: []byte(`{"terminal":"%1","tag":"delivery","summary":"The work is ready.","language":"en"}`)}
	if got := runCompletionPush(ctx, b, e); got.state != store.EffectDone || len(pushes) != 1 || !strings.HasPrefix(pushes[0], "%1:") || strings.Contains(pushes[0], "The work is ready") {
		t.Fatalf("generic delivery push = %+v, %v", got, pushes)
	}
	b.PushEnabled = func(string) bool { return true }
	if got := runCompletionPush(ctx, b, e); got.state != store.EffectDone || !strings.Contains(pushes[1], "The work is ready") {
		t.Fatalf("detailed delivery push = %+v, %v", got, pushes)
	}
	b.PushEnabled = func(key string) bool { return key != "push_on_delivery" }
	if got := runCompletionPush(ctx, b, e); got.outcome != "disabled_by_user" || len(pushes) != 2 {
		t.Fatalf("disabled delivery = %+v, %v", got, pushes)
	}
}

func TestSessionDeliveryRecordsThenPushesToItsSession(t *testing.T) {
	b, ctx := newTestBroker(t)
	var terminal, body string
	b.PushEnabled = func(string) bool { return true }
	b.Push = func(_ context.Context, _, pushedBody, pushedTerminal, _ string) (int, int, error) {
		terminal, body = pushedTerminal, pushedBody
		return 1, 0, nil
	}
	if _, err := b.ReportSessionDelivery(ctx, "%1", "The release is ready."); err != nil {
		t.Fatal(err)
	}
	if terminal != "%1" || body != "The release is ready." {
		t.Fatalf("delivery destination and summary = %q %q", terminal, body)
	}
}

func TestBatchPushWaitsForLastChildAndSendsOnce(t *testing.T) {
	b, ctx := newTestBroker(t)
	var pushes []string
	b.Push = func(_ context.Context, _, body, terminal, _ string) (int, int, error) {
		pushes = append(pushes, terminal+":"+body)
		return 1, 0, nil
	}
	b.PushEnabled = func(string) bool { return true }
	for _, id := range []string{"task-one", "task-two"} {
		r := Record{Protocol: Protocol, ID: id, Assistant: "claude", Title: id,
			State: StateBriefed, CreatedAt: time.Now(), Claims: []string{},
			Root:           &RootRef{SessionID: rootConversation, Assistant: "claude"},
			RootTerminalID: "%1", BatchID: "batch-one"}
		if err := b.save(ctx, r, HashSecret("secret"), "task.queued"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := b.Settle(ctx, "task-one", StateSuccess, "done", nil); err != nil {
		t.Fatal(err)
	}
	if len(pushes) != 0 {
		t.Fatalf("batch pushed before its last child: %v", pushes)
	}
	if _, err := b.Settle(ctx, "task-two", StateFailure, "failed", nil); err != nil {
		t.Fatal(err)
	}
	if len(pushes) != 1 || !strings.HasPrefix(pushes[0], "%1:") {
		t.Fatalf("batch completion pushes = %v", pushes)
	}
	if _, err := b.Settle(ctx, "task-two", StateFailure, "failed", nil); err == nil || len(pushes) != 1 {
		t.Fatalf("duplicate settlement pushed again: %v, %v", err, pushes)
	}
}
