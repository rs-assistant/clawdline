package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/work"
)

func TestAPlanConvertsOnceIntoExecutableWork(t *testing.T) {
	w := newWorkV2Test(t)
	plan := createWorkV2Test(t, w, work.KindPlan)

	converted, err := w.ConvertKind(context.Background(), plan.Item.ID, ConvertKindV2{
		ExpectedVersion: plan.Item.Version, Kind: work.KindEpic, Actor: "device:phone",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if converted.Item.Kind != work.KindEpic || converted.Item.Area() != "unassigned" ||
		converted.Item.Title != plan.Item.Title || converted.Item.Description != plan.Item.Description {
		t.Fatalf("converted plan = %+v", converted.Item)
	}
	if converted.Item.Version != plan.Item.Version+1 {
		t.Fatalf("version = %d, want %d", converted.Item.Version, plan.Item.Version+1)
	}

	full, err := w.Item(context.Background(), plan.Item.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range full.Events {
		if event.Kind == "item.converted" && event.Actor == "device:phone" &&
			strings.Contains(event.Payload, `"from":"plan"`) && strings.Contains(event.Payload, `"to":"epic"`) {
			found = true
		}
	}
	if !found {
		t.Fatalf("conversion event = %+v", full.Events)
	}

	if _, err := w.ConvertKind(context.Background(), plan.Item.ID, ConvertKindV2{
		ExpectedVersion: converted.Item.Version, Kind: work.KindIssue, Actor: "device:phone",
	}, nil); err == nil || !strings.Contains(err.Error(), "conversion_kind_invalid") {
		t.Fatalf("second conversion = %v", err)
	}
}

func TestAPlanOnlyConvertsIntoAnExecutableKind(t *testing.T) {
	w := newWorkV2Test(t)
	plan := createWorkV2Test(t, w, work.KindPlan)
	for _, kind := range []work.Kind{work.KindPlan, work.Kind("unknown")} {
		if _, err := w.ConvertKind(context.Background(), plan.Item.ID, ConvertKindV2{
			ExpectedVersion: plan.Item.Version, Kind: kind, Actor: "local",
		}, nil); err == nil || !strings.Contains(err.Error(), "conversion_kind_not_executable") {
			t.Fatalf("convert to %q = %v", kind, err)
		}
	}
}

func TestAnEpicOrFeatureCanBecomeAPlanBeforeImplementation(t *testing.T) {
	for _, kind := range []work.Kind{work.KindEpic, work.KindFeature, work.KindRefactor} {
		t.Run(string(kind), func(t *testing.T) {
			w := newWorkV2Test(t)
			created := createWorkV2Test(t, w, kind)
			owned, err := w.Assign(context.Background(), created.Item.ID, AssignWorkV2{
				ExpectedVersion: created.Item.Version, Mode: "existing_session", SessionID: "session-a",
				TerminalID: "terminal-a", Actor: "local",
			}, false, nil)
			if err != nil {
				t.Fatal(err)
			}
			converted, err := w.ConvertKind(context.Background(), created.Item.ID, ConvertKindV2{
				ExpectedVersion: owned.Item.Version, Kind: work.KindPlan, Actor: "device:phone",
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if converted.Item.Kind != work.KindPlan || converted.Item.Phase != work.PhaseCreated ||
				converted.Item.OwnerSession != "" || converted.Item.Area() != "planning" ||
				converted.Item.Title != created.Item.Title || converted.Item.Description != created.Item.Description {
				t.Fatalf("converted item = %+v", converted.Item)
			}
			full, err := w.Item(context.Background(), created.Item.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(full.Assignments) != 1 || full.Assignments[0].State != "released" {
				t.Fatalf("assignment after conversion = %+v", full.Assignments)
			}
			assigned, _, err := w.List(context.Background(), "", "session-a", "open", "")
			if err != nil || len(assigned) != 0 {
				t.Fatalf("old Session still owns the item: %+v %v", assigned, err)
			}
			if _, err := w.ConvertKind(context.Background(), created.Item.ID, ConvertKindV2{
				ExpectedVersion: converted.Item.Version, Kind: kind, Actor: "device:phone",
			}, nil); err != nil {
				t.Fatalf("convert back to %s: %v", kind, err)
			}
		})
	}
}

func TestConversionRefusesAnItemThatStartedImplementation(t *testing.T) {
	w := newWorkV2Test(t)
	created := createWorkV2Test(t, w, work.KindFeature)
	owned, err := w.Assign(context.Background(), created.Item.ID, AssignWorkV2{
		ExpectedVersion: created.Item.Version, Mode: "existing_session", SessionID: "session-a",
		TerminalID: "terminal-a", Actor: "local",
	}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	started, err := w.Advance(context.Background(), created.Item.ID, AdvanceWorkV2{
		ExpectedVersion: owned.Item.Version, SessionID: "session-a", Next: work.PhaseImplementing, Actor: "session-a",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.ConvertKind(context.Background(), created.Item.ID, ConvertKindV2{
		ExpectedVersion: started.Item.Version, Kind: work.KindPlan, Actor: "local",
	}, nil); err == nil || !strings.Contains(err.Error(), "item_not_convertible") {
		t.Fatalf("started item conversion = %v", err)
	}
}

func TestConversionWaitsForAnInFlightReassignment(t *testing.T) {
	w := newWorkV2Test(t)
	created := createWorkV2Test(t, w, work.KindFeature)
	owned, err := w.Assign(context.Background(), created.Item.ID, AssignWorkV2{
		ExpectedVersion: created.Item.Version, Mode: "existing_session", SessionID: "session-a",
		TerminalID: "terminal-a", Actor: "local",
	}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := w.Assign(context.Background(), created.Item.ID, AssignWorkV2{
		ExpectedVersion: owned.Item.Version, Mode: "new_session", Assistant: "codex", Actor: "local",
	}, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.ConvertKind(context.Background(), created.Item.ID, ConvertKindV2{
		ExpectedVersion: pending.Item.Version, Kind: work.KindPlan, Actor: "local",
	}, nil); err == nil || !strings.Contains(err.Error(), "assignment_pending") {
		t.Fatalf("conversion during reassignment = %v", err)
	}
}

// planAnalysis has the shape a Plan's description takes: a list of measured
// facts, a numbered list of points to discuss, and a legend list. None of its
// rows is work.
const planAnalysis = `## Measured problem
- The send path holds the lock for 40 ms on every keystroke.
- Three packages each carry their own copy of the retry loop.
- The cache has no bound.

## Items to discuss
1. Move the retry loop into one package.
2. Bound the cache.
3. Drop the lock before the network call.

## Legend
- **A internal only**: no behaviour changes.
- **B visible**: the person sees a difference.
- **D deferred**: not in this round.`

func TestAPlanTurnedIntoWorkSeedsNoStepsFromItsAnalysis(t *testing.T) {
	ctx := context.Background()
	if got := len(descriptionStepTitles(planAnalysis)); got < 8 {
		t.Fatalf("the analysis parses into %d rows; the test needs the real shape", got)
	}
	newPlan := func(t *testing.T, w *WorkSystemV2) WorkV2View {
		t.Helper()
		plan, err := w.Create(ctx, NewWorkV2{ProjectID: "p", ProjectPath: "/p", Kind: work.KindPlan,
			Title: "Refactor the send path", Description: planAnalysis, Actor: "local"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		return plan
	}
	stored := func(t *testing.T, w *WorkSystemV2, id string) []work.StepV2 {
		t.Helper()
		full, err := w.Item(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return full.Steps
	}

	t.Run("claimed with conversion", func(t *testing.T) {
		w := newWorkV2Test(t)
		plan := newPlan(t, w)
		owned, err := w.Assign(ctx, plan.Item.ID, AssignWorkV2{ExpectedVersion: plan.Item.Version,
			ConvertKind: work.KindRefactor, Mode: "existing_session", SessionID: "session-a",
			TerminalID: "terminal-a", Actor: "user_via_session:run-a", Claim: &work.CreatedViaV2{Run: "run-a"}}, false, nil)
		if err != nil {
			t.Fatal(err)
		}
		if owned.Item.Kind != work.KindRefactor || len(owned.Steps) != 0 {
			t.Fatalf("claim seeded %d steps: %+v", len(owned.Steps), owned.Steps)
		}
		if steps := stored(t, w, plan.Item.ID); len(steps) != 0 {
			t.Fatalf("claim stored %d steps: %+v", len(steps), steps)
		}
	})

	t.Run("converted by the person, then assigned", func(t *testing.T) {
		w := newWorkV2Test(t)
		plan := newPlan(t, w)
		converted, err := w.ConvertKind(ctx, plan.Item.ID, ConvertKindV2{
			ExpectedVersion: plan.Item.Version, Kind: work.KindFeature, Actor: "local"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		owned, err := w.Assign(ctx, plan.Item.ID, AssignWorkV2{ExpectedVersion: converted.Item.Version,
			Mode: "existing_session", SessionID: "session-a", TerminalID: "terminal-a", Actor: "local"}, false, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(owned.Steps) != 0 {
			t.Fatalf("assignment seeded %d steps: %+v", len(owned.Steps), owned.Steps)
		}
		if steps := stored(t, w, plan.Item.ID); len(steps) != 0 {
			t.Fatalf("assignment stored %d steps: %+v", len(steps), steps)
		}
	})

	t.Run("converted, then opened in a new Session", func(t *testing.T) {
		w := newWorkV2Test(t)
		plan := newPlan(t, w)
		converted, err := w.ConvertKind(ctx, plan.Item.ID, ConvertKindV2{
			ExpectedVersion: plan.Item.Version, Kind: work.KindFeature, Actor: "local"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Assign(ctx, plan.Item.ID, AssignWorkV2{ExpectedVersion: converted.Item.Version,
			Mode: "new_session", AssignmentID: "assignment-a", Actor: "local"}, true, nil); err != nil {
			t.Fatal(err)
		}
		active, err := w.FinishAssignment(ctx, plan.Item.ID, FinishAssignmentV2{AssignmentID: "assignment-a",
			SessionID: "session-a", TerminalID: "terminal-a", RootAssignment: "root-a", Actor: "local"})
		if err != nil {
			t.Fatal(err)
		}
		if len(active.Steps) != 0 {
			t.Fatalf("activation seeded %d steps: %+v", len(active.Steps), active.Steps)
		}
		if steps := stored(t, w, plan.Item.ID); len(steps) != 0 {
			t.Fatalf("activation stored %d steps: %+v", len(steps), steps)
		}
	})

	t.Run("a feature that was never a Plan still seeds", func(t *testing.T) {
		w := newWorkV2Test(t)
		feature, err := w.Create(ctx, NewWorkV2{ProjectID: "p", ProjectPath: "/p", Kind: work.KindFeature,
			Title: "Refactor the send path", Description: planAnalysis, Actor: "local"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		owned, err := w.Assign(ctx, feature.Item.ID, AssignWorkV2{ExpectedVersion: feature.Item.Version,
			Mode: "existing_session", SessionID: "session-a", TerminalID: "terminal-a", Actor: "local"}, false, nil)
		if err != nil {
			t.Fatal(err)
		}
		if want := len(descriptionStepTitles(planAnalysis)); len(owned.Steps) != want {
			t.Fatalf("never-Plan feature seeded %d steps, want %d", len(owned.Steps), want)
		}
	})
}
