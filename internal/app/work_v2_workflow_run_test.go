package app

import (
	"context"
	"testing"
)

func workflowText(id, title string) string {
	return "Launch message\n\n[Clawdline-Workflow " + id + " " + title + "]"
}

func TestASessionRunsOneWorkflowAtATime(t *testing.T) {
	ctx := context.Background()
	w := newWorkV2Test(t)
	const redesign, review = "f1000001-0000-4000-8000-000000000001", "f1000002-0000-4000-8000-000000000002"
	first, err := w.CreateDirectTodo(ctx, NewDirectTodoV2{SessionID: "session-a", Text: workflowText(redesign, "Redesign"), Actor: "local"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	refused := func(what string, err error) {
		t.Helper()
		if e, ok := err.(*WorkError); !ok || e.Code != "workflow_running" {
			t.Fatalf("%s: %T %v", what, err, err)
		}
	}
	// The same Start pressed again, and a different Workflow, both wait for the first.
	_, err = w.CreateDirectTodo(ctx, NewDirectTodoV2{SessionID: "session-a", Text: workflowText(redesign, "Redesign"), Actor: "local"}, nil)
	refused("same Workflow again", err)
	_, err = w.CreateDirectTodo(ctx, NewDirectTodoV2{SessionID: "session-a", Text: workflowText(review, "Review"), Actor: "local"}, nil)
	refused("another Workflow", err)
	// Ordinary to-dos and other Sessions are not held back.
	if _, err := w.CreateDirectTodo(ctx, NewDirectTodoV2{SessionID: "session-a", Text: "Also fix the icon", Actor: "local"}, nil); err != nil {
		t.Fatalf("plain to-do: %v", err)
	}
	if _, err := w.CreateDirectTodo(ctx, NewDirectTodoV2{SessionID: "session-b", Text: workflowText(review, "Review"), Actor: "local"}, nil); err != nil {
		t.Fatalf("other Session: %v", err)
	}
	// Once the first is done the next one starts; reopening the first is then refused.
	if _, err := w.CompleteDirectTodo(ctx, first.ID, "session-a", "local", true, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := w.CreateDirectTodo(ctx, NewDirectTodoV2{SessionID: "session-a", Text: workflowText(review, "Review"), Actor: "local"}, nil); err != nil {
		t.Fatalf("after done: %v", err)
	}
	_, err = w.ReopenDirectTodo(ctx, first.ID, "session-a", nil)
	refused("reopen while another runs", err)
}
