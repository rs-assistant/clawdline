package ownrecords

import (
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/contract"
)

var (
	ownChildLive = Live{TerminalID: "%5", TTY: "ttys005", Assistant: "claude", PID: 50,
		ProcessStart: time.Unix(1000, 0), ConversationID: "conv-child"}
	ownRootLive = Live{TerminalID: "%6", TTY: "ttys006", Assistant: "claude", PID: 60,
		ProcessStart: time.Unix(900, 0), ConversationID: "conv-root"}
)

func ownCloseInput(l Live) CloseInput {
	return CloseInput{Live: l, TerminalState: "idle", Bound: true, Matches: 1, InventoryComplete: true,
		InventoryObservedAt: time.Now(), Now: time.Now()}
}

func strp(v string) *string { return &v }

func TestIdleCloseabilityUsesEvidenceAndOutstandingWork(t *testing.T) {
	base := Snapshot{}
	clear := ownCloseInput(ownRootLive)
	if got := base.Closeability(clear); got.State != "safe" || got.AttestationID != nil {
		t.Fatalf("idle clear Session needs no attestation: %+v", got)
	}
	open := clear
	open.Extra = []contract.CloseReason{{Code: "session_todo_open", Kind: "obligation", SubjectID: "todo-1"}}
	if got := base.Closeability(open); got.State != "blocked" || got.Reasons[0].SubjectID != "todo-1" {
		t.Fatalf("open to-do did not block: %+v", got)
	}
	more := open
	more.Extra = append(append([]contract.CloseReason(nil), open.Extra...),
		contract.CloseReason{Code: "board_item_open", Kind: "obligation", SubjectID: "board-2"})
	first, added := base.Closeability(open), base.Closeability(more)
	if first.Version == added.Version || added.State != "blocked" {
		t.Fatalf("a new obligation did not invalidate the blocked decision: %s %s", first.Version, added.Version)
	}
	reordered := more
	reordered.Extra = []contract.CloseReason{more.Extra[1], more.Extra[0]}
	if got := base.Closeability(reordered); got.Version != added.Version {
		t.Fatalf("row order changed the same decision: %s %s", got.Version, added.Version)
	}
	working := clear
	working.TerminalState = "working"
	if got := base.Closeability(working); got.State != "blocked" {
		t.Fatalf("working Session = %+v", got)
	}
	unreadable := clear
	unreadable.Bound = false
	if got := base.Closeability(unreadable); got.State != "unknown" {
		t.Fatalf("unbound Session = %+v", got)
	}
	missingState := clear
	missingState.TerminalState = ""
	if got := base.Closeability(missingState); got.State != "unknown" {
		t.Fatalf("missing terminal state = %+v", got)
	}
}

// ownFinishedTask is a task of this daemon's, finished, whose child is
// ownChildLive and whose root is ownRootLive.
func ownFinishedTask() Task {
	fin, start, pid := Seconds(2000), Seconds(1000), int64(50)
	return Task{ID: "abcd1234", State: "success", Title: "Own task", Assistant: "claude", Created: 990,
		FinishedAt: &fin, ResultVerifiedAt: &fin,
		ChildTerminal: strp("%5"), ChildTTY: strp("ttys005"), ChildPID: &pid, ChildProcStart: &start,
		ChildSession: strp("conv-child"), TranscriptProven: true,
		RootSession: strp("conv-root"), RootAssistant: strp("claude")}
}

// With the Swift store switched off, this daemon's own records alone give the
// child its delivery tick and its tab's name, and the store is not "unknown".
// The control is the same records over an unreadable store, which stays
// unknown.
func TestOwnRecordsAloneDrawTheRow(t *testing.T) {
	base := Snapshot{}
	snap := base.With(Own{Tasks: []Task{ownFinishedTask()}})

	w := snap.Work(ownChildLive, "idle")
	if w.State != "milestone_complete" || w.Disposition == nil || w.Disposition.Scope != "task" ||
		w.Disposition.TaskID != "abcd1234" || w.Disposition.Title != "Own task" {
		t.Fatalf("the child's delivery is not drawn from its own task: %+v %+v", w, w.Disposition)
	}
	if got := snap.TitleOf(ownChildLive, "", []Live{ownChildLive, ownRootLive}).Orchestrator; got != "Own task" {
		t.Fatalf("the tab is not named by its task: %q", got)
	}
	closed := snap.Closeability(ownCloseInput(ownChildLive))
	if closed.State != "safe" {
		t.Fatalf("a finished idle child with no obligations is %s: %+v", closed.State, closed.Reasons)
	}

	// A live child keeps its root waiting and holds the root open.
	running := ownFinishedTask()
	running.ID, running.State, running.FinishedAt, running.ResultVerifiedAt = "live0001", "briefed", nil, nil
	snap = base.With(Own{Tasks: []Task{running}})
	if w := snap.Work(ownRootLive, "idle"); w.State != "waiting_session" {
		t.Fatalf("a root with a live child of its own reads %s", w.State)
	}
	c := snap.Closeability(ownCloseInput(ownRootLive))
	found := false
	for _, r := range c.Reasons {
		found = found || (r.Code == "live_descendant_task" && r.SubjectID == "live0001")
	}
	if c.State != "blocked" || !found {
		t.Fatalf("the root's live child does not hold it open: %s %v", c.State, c.Reasons)
	}
	// Control: without the overlay the same root owes nothing.
	if w := base.Work(ownRootLive, "idle"); w.State == "waiting_session" {
		t.Fatal("control: a root with no records is waiting")
	}
}

// A queued `clawdline heavy --handoff` is the root's own command, not a child:
// the root still reads as waiting on it, but it does not hold the root open.
// The control is the same record without its callback intent, which does.
func TestARootsOwnCallbackDoesNotHoldItOpen(t *testing.T) {
	base := Snapshot{}
	for _, intent := range []string{"heavy", "wait"} {
		queued := Task{ID: "callback1", State: "queued", Kind: "callback", Title: "Queued heavy command finished",
			Assistant: "claude", Created: 990, CallbackIntent: intent,
			RootSession: strp("conv-root"), RootAssistant: strp("claude")}
		snap := base.With(Own{Tasks: []Task{queued}})
		if w := snap.Work(ownRootLive, "idle"); w.State != "waiting_session" {
			t.Fatalf("%s: a root with a queued callback reads %s", intent, w.State)
		}
		if c := snap.Closeability(ownCloseInput(ownRootLive)); c.State != "safe" {
			t.Fatalf("%s: a root's own queued callback holds it open: %s %v", intent, c.State, c.Reasons)
		}

		queued.CallbackIntent = ""
		control := base.With(Own{Tasks: []Task{queued}})
		if c := control.Closeability(ownCloseInput(ownRootLive)); c.State != "blocked" {
			t.Fatalf("control: the same live task without a callback intent is %s", c.State)
		}
	}
}

// The tasks a waiting row names are the ones that made it wait: every live
// task it counted, and none it did not. A row that is not waiting on tasks
// names nothing, so no list can show a wait the state does not have.
func TestAWaitingRootNamesExactlyTheTasksItCounted(t *testing.T) {
	base := Snapshot{}
	heavy := Task{ID: "heavy1", State: "queued", Kind: "callback", Title: "Queued heavy command finished",
		Assistant: "claude", Created: 990, CallbackIntent: "heavy", RootSession: strp("conv-root"), RootAssistant: strp("claude")}
	child := Task{ID: "child1", State: "briefed", Title: "Fix the list", Assistant: "codex", Created: 991,
		RootSession: strp("conv-root"), RootAssistant: strp("claude")}
	done := Task{ID: "done1", State: "success", Title: "Finished", Assistant: "codex", Created: 989,
		RootSession: strp("conv-root"), RootAssistant: strp("claude")}
	other := Task{ID: "other1", State: "briefed", Title: "Someone else's", Assistant: "codex", Created: 992,
		RootSession: strp("conv-other"), RootAssistant: strp("claude")}
	snap := base.With(Own{Tasks: []Task{heavy, child, done, other}})
	w := snap.Work(ownRootLive, "idle")
	var ids []string
	for _, task := range w.Waiting {
		ids = append(ids, task.ID)
	}
	if w.State != "waiting_session" || strings.Join(ids, ",") != "heavy1,child1" {
		t.Fatalf("waiting root: state %s, names %v", w.State, ids)
	}
	if w := snap.Work(ownRootLive, "working"); w.Waiting != nil {
		t.Fatalf("a working root names waits: %v", w.Waiting)
	}
	if w := base.With(Own{Tasks: []Task{done, other}}).Work(ownRootLive, "idle"); w.Waiting != nil || w.State == "waiting_session" {
		t.Fatalf("control: a root with nothing live reads %s and names %v", w.State, w.Waiting)
	}
}

func TestAgentNamedBoardConversationOutranksTaskFromReusedTerminal(t *testing.T) {
	live := Live{TerminalID: "%5", Assistant: "codex", ConversationID: "new-conversation"}
	old := Task{ID: "old-task", State: "success", Title: "Old task title", ChildTerminal: strp("%5")}
	snap := Snapshot{Orchestrator: Orchestrator{Tasks: []Task{old}}, BoardTitles: []BoardTitle{{Assistant: "codex", ConversationID: "new-conversation", Label: "New item name", AgentNamed: true}}}
	got := snap.TitleOf(live, "", []Live{live})
	if got.Orchestrator != "New item name" || !got.AgentNamedBoard {
		t.Fatalf("reused terminal showed %+v instead of the new conversation name", got)
	}
}

// A delivery the session reported here is its tick; one reported by another
// conversation in the same terminal is not. A wait registered here parks the
// waiter and is drawn on both rows.
func TestAnOwnDeliveryAndAnOwnWaitAreDrawn(t *testing.T) {
	base := Snapshot{}
	start, pid := Seconds(1000), int64(50)
	delivery := SessionDelivery{Identity: Identity{TerminalID: "%5", TTY: "ttys005", Assistant: strp("claude"),
		PID: &pid, ProcessStart: &start, ConversationID: strp("conv-child")}, Summary: "shipped", ReportedAt: 3000}

	w := base.With(Own{Deliveries: []SessionDelivery{delivery}}).Work(ownChildLive, "idle")
	if w.State != "milestone_complete" || w.Disposition == nil || w.Disposition.Scope != "session" ||
		w.Disposition.Title != "shipped" {
		t.Fatalf("the session's own delivery is not its tick: %+v", w)
	}
	other := delivery
	other.ConversationID = strp("somebody-else")
	if w := base.With(Own{Deliveries: []SessionDelivery{other}}).Work(ownChildLive, "idle"); w.State == "milestone_complete" {
		t.Fatal("control: another conversation's delivery was drawn on this row")
	}

	wait := CoordinationWait{ID: "w1", Repository: "/r", Paths: []string{"a.go"}, OwnerSessionID: "%6", Created: 100,
		Waiters: []CoordinationWaiter{{SessionID: "%5", Reason: "needs a.go", Created: 100}}}
	snap := base.With(Own{Waits: []CoordinationWait{wait}})
	if w := snap.Work(ownChildLive, "idle"); w.State != "waiting_session" {
		t.Fatalf("the waiter reads %s", w.State)
	}
	label := func(id string) string { return map[string]string{"%5": "child", "%6": "root"}[id] }
	if c := snap.Coordination("%5", label); c == nil || c.State != "waiting_on_session" || c.WaitingOn[0].OwnerLabel != "root" {
		t.Fatalf("the waiter's coordination: %+v", c)
	}
	if c := snap.Coordination("%6", label); c == nil || c.State != "has_waiters" || c.WaitedOnBy[0].WaiterLabel != "child" {
		t.Fatalf("the owner's coordination: %+v", c)
	}
	released := wait
	released.Waiters = []CoordinationWaiter{{SessionID: "%5", Created: 100, ReleaseDeliveredAt: &start}}
	if c := base.With(Own{Waits: []CoordinationWait{released}}).Coordination("%5", label); c != nil {
		t.Fatalf("control: a released waiter still waits: %+v", c)
	}
}

// The merge never writes into the reading it was given: that reading is a
// cached one, shared with every other caller.
func TestWithLeavesTheReadingAlone(t *testing.T) {
	tasks := make([]Task, 1, 4)
	tasks[0] = Task{ID: "swift"}
	base := Snapshot{Orchestrator: Orchestrator{Tasks: tasks}}
	merged := base.With(Own{Tasks: []Task{{ID: "own"}, {ID: "swift"}}})
	if len(merged.Tasks) != 2 || merged.Tasks[1].ID != "own" {
		t.Fatalf("merged tasks: %+v", merged.Tasks)
	}
	if len(base.Tasks) != 1 || tasks[:2][1].ID == "own" {
		t.Fatal("the merge wrote into the cached reading's array")
	}
}
