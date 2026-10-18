package http

import (
	"context"
	"fmt"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/ownrecords"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
)

func TestAWaitTargetSaysWhatTheWorkIsNotWhatItWillSayWhenDone(t *testing.T) {
	strp := func(s string) *string { return &s }
	tasks := []*ownrecords.Task{
		{ID: "heavy-running", Title: "Queued heavy command finished", CallbackIntent: orchestrator.CallbackIntentHeavy},
		{ID: "heavy-queued", Title: "Queued heavy command finished", CallbackIntent: orchestrator.CallbackIntentHeavy},
		{ID: "wait", Title: "CI finished", CallbackIntent: orchestrator.CallbackIntentWait},
		{ID: "named", Title: "A task title", ChildTerminal: strp("%9"), ChildSession: strp("conv-child")},
		{ID: "unnamed", Title: "A task title", ChildSession: strp("conv-child")},
		{ID: "unstarted", Title: "Fix the list"},
	}
	places := map[string]heavySlotPlace{
		"heavy-running": {reason: "go test ./...", phase: "running"},
		"heavy-queued":  {reason: "npm run build", phase: "queued"},
	}
	got, omitted := (&Server{}).waitTargets(context.Background(), tasks, map[string]string{"%9": "The child Session"}, places)
	want := []string{
		"heavy-running heavy running go test ./...",
		"heavy-queued heavy queued npm run build",
		"wait callback  CI finished",
		"named session  The child Session",
		"unnamed session  conv-child",
		"unstarted task  Fix the list",
	}
	if omitted != 0 || len(got) != len(want) {
		t.Fatalf("targets %+v omitted %d", got, omitted)
	}
	for i, target := range got {
		if line := fmt.Sprintf("%s %s %s %s", target.TaskID, target.Kind, target.Phase, target.Label); line != want[i] {
			t.Fatalf("target %d = %q, want %q", i, line, want[i])
		}
	}
}

func TestAWaitTargetListKeepsItsBoundAndCountsTheRest(t *testing.T) {
	var tasks []*ownrecords.Task
	for i := 0; i < MaximumWaitTargets+3; i++ {
		tasks = append(tasks, &ownrecords.Task{ID: fmt.Sprint("t", i), Title: "child"})
	}
	got, omitted := (&Server{}).waitTargets(context.Background(), tasks, nil, nil)
	if len(got) != MaximumWaitTargets || omitted != 3 || got[0].TaskID != "t0" {
		t.Fatalf("got %d targets, omitted %d, first %+v", len(got), omitted, got[0])
	}
}

func TestAHeavyCallbackAwayFromTheSlotIsNamedByItsCommand(t *testing.T) {
	for _, c := range []struct {
		argv []string
		want string
	}{
		{[]string{"/bin/clawdline", "heavy", "--reason", "web build", "--max-wait", "1h", "--", "npm", "run", "build"}, "web build"},
		{[]string{"/bin/clawdline", "heavy", "--reason=web build", "--", "npm"}, "web build"},
		{[]string{"/bin/clawdline", "heavy", "--max-wait", "1h", "--", "go", "test", "./..."}, "go test ./..."},
		{[]string{"/bin/clawdline", "heavy"}, ""},
	} {
		if got := heavyArgvReason(c.argv); got != c.want {
			t.Fatalf("%v -> %q, want %q", c.argv, got, c.want)
		}
	}
}
