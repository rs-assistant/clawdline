package orchestrator

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// A scheduled run past its timeout is ended by the clock only once its child
// is no longer at work: at work or waiting on the person, it goes on, and only
// its own result.json ends it. A task a session dispatched is timed as before.
func TestAScheduledRunAtWorkIsNotEndedByItsClock(t *testing.T) {
	b, ctx := newTestBroker(t)
	now := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	b.Clock = func() time.Time { return now }
	b.Launcher = &fakeLauncher{}
	briefed := func(id, pane, schedule string) {
		r := Record{Protocol: Protocol, ID: id, Assistant: "claude", State: StateBriefed,
			CreatedAt: now.Add(-31 * time.Minute), Claims: []string{}, TimeoutMinutes: 30,
			ChildTerminalID: pane, ChildBackend: "tmux", SpawnedAt: now.Add(-31 * time.Minute),
			AcceptedAt: now.Add(-30 * time.Minute), ScheduleID: schedule, ScheduleCloseTab: closeTabOnSuccess}
		if err := b.save(ctx, r, HashSecret("s"), "task.spawned"); err != nil {
			t.Fatal(err)
		}
	}
	record := func(id string) Record {
		r, _, err := b.Record(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	working := "e1111111-1111-4111-8111-111111111111"
	waiting := "e2222222-2222-4222-8222-222222222222"
	resting := "e3333333-3333-4333-8333-333333333333"
	unscheduled := "e4444444-4444-4444-8444-444444444444"
	briefed(working, "%1", "sched-a")
	briefed(waiting, "%2", "sched-a")
	briefed(resting, "%3", "sched-a")
	briefed(unscheduled, "%4", "")
	pane := func(id string, state session.State, conversation string) session.Session {
		return session.Session{ID: id, Backend: session.BackendTmux, Assistant: session.AssistantClaude,
			State: state, ConversationID: conversation}
	}
	inv := session.Inventory{Complete: true, Sources: map[string]bool{"tmux": true}, Sessions: []session.Session{
		pane("%1", session.StateWorking, "c1111111-1111-4111-8111-111111111111"),
		pane("%2", session.StateWaiting, ""),
		pane("%3", session.StateIdle, ""),
		pane("%4", session.StateWorking, "c4444444-4444-4444-8444-444444444444"),
	}}
	b.Reading = func(context.Context) session.Inventory { return inv }

	b.Pass(ctx)
	if got := record(working).State; got != StateBriefed {
		t.Errorf("a scheduled run at work past its timeout is %q, want still briefed", got)
	}
	if got := record(waiting).State; got != StateBriefed {
		t.Errorf("a scheduled run waiting on the person past its timeout is %q, want still briefed", got)
	}
	if got := record(resting).State; got != StateTimeout {
		t.Errorf("a scheduled run at rest past its timeout is %q, want timeout", got)
	}
	if got := record(unscheduled).State; got != StateTimeout {
		t.Errorf("a dispatched task at work past its timeout is %q, want timeout", got)
	}
	if got := record(working).ChildConversationID; got != "c1111111-1111-4111-8111-111111111111" {
		t.Errorf("the scheduled child's conversation is %q, want the one its tab held", got)
	}
	if got := record(unscheduled).ChildConversationID; got != "c4444444-4444-4444-8444-444444444444" {
		t.Errorf("a dispatched task lost the conversation it ran in: %q", got)
	}

	// The child's tab is gone: the clock ends the run as before.
	inv.Sessions = inv.Sessions[1:]
	b.Pass(ctx)
	if got := record(working).State; got != StateTimeout {
		t.Errorf("a scheduled run whose tab has gone is %q, want timeout", got)
	}
}

// The briefing a scheduled child reads does not tell it to stop at the clock.
func TestAScheduledBriefingSaysTheClockIsNotADeadline(t *testing.T) {
	b, _ := newTestBroker(t)
	r := Record{Protocol: Protocol, ID: "e5555555-5555-4555-8555-555555555555", Assistant: "claude",
		Title: "Research queue", Kind: "custom", TimeoutMinutes: 30, ScheduleID: "sched-a", ScheduleTitle: "Research queue"}
	brief := b.ChildBrief(r, "/tmp/project")
	if strings.Contains(brief, "minutes on the wall clock from dispatch") {
		t.Errorf("a scheduled briefing still gives the clock as a deadline:\n%s", brief)
	}
	if !strings.Contains(brief, "That is not a deadline") {
		t.Errorf("a scheduled briefing does not say its time is not a deadline:\n%s", brief)
	}
	r.ScheduleID = ""
	if brief := b.ChildBrief(r, "/tmp/project"); !strings.Contains(brief, "You have 30 minutes on the wall clock from dispatch") {
		t.Errorf("a dispatched briefing lost its deadline:\n%s", brief)
	}
}
