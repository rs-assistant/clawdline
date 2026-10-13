package http

import (
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/swiftstore"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestWaitingChildNamesUsesTheChildSessionIdentity(t *testing.T) {
	rootID, otherID, assistant := "root-conversation", "other-conversation", "codex"
	childTerminal, wrongTerminal := "child-terminal", "wrong-terminal"
	childConversation := "child-conversation"
	snapshot := swiftstore.Snapshot{Orchestrator: swiftstore.Orchestrator{Tasks: []swiftstore.Task{
		{State: "briefed", RootSession: &rootID, RootAssistant: &assistant, ChildTerminal: &childTerminal, ChildSession: &childConversation, Title: "A task title"},
		{State: "briefed", RootSession: &otherID, RootAssistant: &assistant, ChildTerminal: &wrongTerminal, Title: "Unrelated"},
		{State: "success", RootSession: &rootID, RootAssistant: &assistant, ChildTerminal: &wrongTerminal, Title: "Finished"},
	}}}
	root := session.Session{Assistant: session.AssistantCodex, ConversationID: rootID}
	got := waitingChildNames(root, swiftstore.Live{}, snapshot, map[string]string{childTerminal: "The child Session", wrongTerminal: "Wrong Session"})
	if got != "The child Session" {
		t.Fatalf("waiting child = %q", got)
	}
	if got := waitingChildNames(root, swiftstore.Live{}, snapshot, nil); got != childConversation {
		t.Fatalf("unnamed child should use its conversation identity, got %q", got)
	}
}
