package http

import (
	"reflect"
	"testing"

	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestAClosedSessionStopsOnlyItsOwnHeavyCallbacks(t *testing.T) {
	const conversation = "d0000000-0000-4000-8000-000000000001"
	closed := session.Session{ID: "%1", Assistant: session.AssistantClaude, ConversationID: conversation}
	callback := func(id, intent, root string, state orchestrator.State) orchestrator.Record {
		return orchestrator.Record{ID: id, State: state, Callback: &orchestrator.Callback{Intent: intent},
			Root: &orchestrator.RootRef{SessionID: root, Assistant: "claude"}}
	}
	records := []orchestrator.Record{
		callback("queued-heavy", orchestrator.CallbackIntentHeavy, conversation, orchestrator.StateQueued),
		callback("running-heavy", orchestrator.CallbackIntentHeavy, conversation, orchestrator.StateBriefed),
		callback("wait", orchestrator.CallbackIntentWait, conversation, orchestrator.StateQueued),
		callback("finished-heavy", orchestrator.CallbackIntentHeavy, conversation, orchestrator.StateSuccess),
		callback("other-root", orchestrator.CallbackIntentHeavy, "e0000000-0000-4000-8000-000000000001", orchestrator.StateQueued),
		{ID: "child", State: orchestrator.StateBriefed, Root: &orchestrator.RootRef{SessionID: conversation}},
	}

	got := heavyCallbacksOf(records, closed, []session.Session{closed})
	if want := []string{"queued-heavy", "running-heavy"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("stopped %v, want %v", got, want)
	}

	// The same conversation still open in another terminal is still waiting.
	twin := closed
	twin.ID = "%2"
	if got := heavyCallbacksOf(records, closed, []session.Session{closed, twin}); got != nil {
		t.Fatalf("stopped %v while another terminal holds the root", got)
	}

	// A root whose conversation was never read names nothing.
	if got := heavyCallbacksOf(records, session.Session{ID: "%1", Assistant: session.AssistantClaude}, nil); got != nil {
		t.Fatalf("stopped %v for a Session with no conversation", got)
	}
}
