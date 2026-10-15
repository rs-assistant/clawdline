package app

import (
	"context"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/orchestrator"
)

// A finished run's row in the history sheet names the tab it had and the
// conversation its child had, and the resume route admits that conversation
// in the run's own project and nowhere else.
func TestAFinishedRunIsResumedByItsChildsConversation(t *testing.T) {
	f := newW4(t)
	ctx := context.Background()
	const conversation = "c7777777-7777-4777-8777-777777777777"
	id := f.schedule(t, "5c000007-0000-4000-8000-000000000007", "research queue", nil)
	if fired := f.book.Beat(ctx); fired.Fired != 1 {
		t.Fatalf("the schedule did not fire: %+v", fired)
	}
	task := f.runs(t, id)[0].TaskID
	var asked []string
	f.book.Conversations = RunConversations{
		Find: func(dir string, since time.Time, want map[string]bool) map[string]string {
			asked = append(asked, dir)
			if dir == f.dir && want[task] {
				return map[string]string{task: conversation}
			}
			return nil
		},
		TaskOf: func(dir, conv string) (string, bool) { return task, dir == f.dir && conv == conversation },
		Recorded: func(dir, assistant, conv string) bool {
			return dir == f.dir && assistant == "claude" && conv == conversation
		},
	}
	rowOf := func() map[string]any {
		detail, ok, err := f.book.Detail(ctx, id)
		if err != nil || !ok {
			t.Fatalf("detail: %v %v", ok, err)
		}
		return detail["runs"].([]map[string]any)[0]
	}

	// Still running: its tab may be opened, and nothing is resumed beside it.
	live := rowOf()
	if live["terminal_id"] == nil || live["terminal_id"] == "" {
		t.Errorf("a running run names no tab: %v", live)
	}
	if _, has := live["session_id"]; has || len(asked) != 0 {
		t.Errorf("a running run offered a resume (%v) or its transcripts were read (%v)", live, asked)
	}
	if f.book.ScheduledConversation(ctx, f.dir, "claude", conversation) {
		t.Error("the resume route admitted the conversation of a run still working")
	}

	if _, err := f.b.Settle(ctx, task, orchestrator.StateFailure, "the child said it failed", nil); err != nil {
		t.Fatal(err)
	}
	done := rowOf()
	if done["session_id"] != conversation {
		t.Errorf("a finished run offers %v, want its child's conversation %s", done["session_id"], conversation)
	}
	if done["finished_at"] == nil {
		t.Errorf("a finished run has no finished_at: %v", done)
	}
	if !f.book.ScheduledConversation(ctx, f.dir, "claude", conversation) {
		t.Error("the resume route refused a finished run's own conversation")
	}
	if f.book.ScheduledConversation(ctx, "/somewhere/else", "claude", conversation) {
		t.Error("the resume route admitted the conversation in another project")
	}
	if f.book.ScheduledConversation(ctx, f.dir, "codex", conversation) {
		t.Error("the resume route admitted the conversation for another assistant")
	}
	if f.book.ScheduledConversation(ctx, f.dir, "claude", "c8888888-8888-4888-8888-888888888888") {
		t.Error("the resume route admitted a conversation no run had")
	}
}
