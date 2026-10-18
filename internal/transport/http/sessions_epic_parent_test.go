package http

import (
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/ownrecords"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestSessionRowCarriesEpicPresentationParentOnlyForAnIndependentRoot(t *testing.T) {
	s := gapServer(t)
	item := session.Session{ID: "terminal-child", Assistant: session.AssistantCodex,
		ConversationID: "conversation-child", State: session.StateIdle}
	live := liveOf(item)
	conversation := item.ConversationID
	snapshot := ownrecords.Snapshot{Orchestrator: ownrecords.Orchestrator{RootAssignments: []ownrecords.RootAssignment{{
		ID: "root-child", Label: "Feature", State: "briefed",
		Identity: &ownrecords.RootAssignmentIdentity{TerminalID: item.ID, Assistant: "codex", ConversationID: &conversation},
	}}}}
	parent := store.EpicRootParent{OwnerSession: "original-owner", EpicID: "epic-a"}
	got := s.sessionRow(rowInput{item: item, live: live, swift: snapshot, epicParent: parent}).EpicParent
	if got == nil || got.OwnerSessionID != parent.OwnerSession || got.EpicID != parent.EpicID {
		t.Fatalf("independent Root Epic ancestry = %+v", got)
	}
	if got := s.sessionRow(rowInput{item: item, live: live, epicParent: parent}).EpicParent; got == nil {
		t.Fatal("Board ancestry disappeared when the Root overlay could not be read")
	}
	if got := s.sessionRow(rowInput{item: item, live: live}).EpicParent; got != nil {
		t.Fatalf("ordinary session received Epic ancestry: %+v", got)
	}
}
