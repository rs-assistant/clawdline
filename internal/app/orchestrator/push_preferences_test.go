package orchestrator

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/store"
)

func TestDisabledEventPreferencesDoNotCallThePushService(t *testing.T) {
	called := 0
	b := &Broker{
		PushEnabled: func(string) bool { return false },
		Push: func(context.Context, string, string, string, string) (int, int, error) {
			called++
			return 1, 0, nil
		},
	}
	rows := []struct {
		name string
		kind string
		body any
		run  func(context.Context, *Broker, store.Effect) effectResult
	}{
		{"waiting", EffectWaitingPush, WaitingPush{Key: "stop", Title: "answer", Body: "question"}, runWaitingPush},
		{"board done", EffectWorkItemCompletedPush, WorkItemCompletedPush{WorkID: "item", Title: "done", Body: "finished"}, runWorkItemCompletedPush},
		{"capacity", EffectCapacityPush, CapacityPush{Name: "queue", Title: "full", Body: "limit"}, runCapacityPush},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			payload, err := json.Marshal(row.body)
			if err != nil {
				t.Fatal(err)
			}
			e := store.Effect{Kind: row.kind, Payload: payload}
			if row.kind == EffectWorkItemCompletedPush {
				e.Subject = "item"
			}
			if got := row.run(context.Background(), b, e); got.state != store.EffectDone || got.outcome != "disabled_by_user" {
				t.Fatalf("disabled event = %+v", got)
			}
		})
	}
	if called != 0 {
		t.Fatalf("push service called %d times", called)
	}
}
