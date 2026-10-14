package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// How many times a schedule runs is the person's to decide. `fired_at` keeps
// the clock from running a one-shot's date twice and nothing more: a one-time
// schedule that has run is still run by a press and by its webhook, and a save
// that gives it a new date arms it for that date. Before this, all three
// answered `409 schedule_spent` and told the person to make a new schedule.
func TestAOneTimeScheduleThatRanCanRunAgain(t *testing.T) {
	f := newW4(t)
	ctx := context.Background()
	f.book.Places = func(context.Context) []SchedulePlace {
		return []SchedulePlace{{ID: "p1", Label: "p", Path: f.dir}}
	}
	id := "5c00e001-0000-4000-8000-000000000001"
	body, _ := json.Marshal(map[string]any{
		"clawdline_schedule": 1, "schedule_id": id, "title": "once",
		"when": map[string]any{"at": "09:00", "on": "2026-09-18"},
		"task": map[string]any{"assistant": "claude", "project_dir": f.dir, "title": "once",
			"instructions": "do it", "timeout_minutes": 1},
		"enabled": true, "catch_up_hours": 6,
		"created_at": f.at().Add(-72 * time.Hour).Unix(),
	})
	if err := f.st.CreateScheduleFile(ctx, id, body, f.at().Add(-72*time.Hour), time.Time{}); err != nil {
		t.Fatal(err)
	}
	// Each run ends on its own clock, so the next is refused for nothing but
	// what this test is about.
	finish := func(what string) {
		t.Helper()
		f.set(f.at().Add(2 * time.Minute))
		if p := f.b.Pass(ctx); p.TimedOut != 1 {
			t.Fatalf("after %s the broker timed out %d task(s), want one", what, p.TimedOut)
		}
	}
	fired := func() bool {
		t.Helper()
		record, ok, err := f.book.Detail(ctx, id)
		if err != nil || !ok {
			t.Fatalf("detail: %v %v", ok, err)
		}
		_, has := record["fired_at"]
		return has
	}

	if p := f.book.Beat(ctx); p.Fired != 1 || !fired() {
		t.Fatalf("the date's own run: %+v, fired_at %v", p, fired())
	}
	finish("the clock's run")
	if p := f.book.Beat(ctx); p.Fired != 0 {
		t.Fatalf("the clock ran the same date twice: %+v", p)
	}

	if r := f.book.Run(ctx, id); !r.OK() {
		t.Fatalf("a press after the date ran answered %d %s %s", r.Status, r.Code, r.Message)
	}
	finish("the press")
	if r := f.book.RunWebhook(ctx, id, "e4e00002-0000-4000-8000-000000000002"); !r.OK() {
		t.Fatalf("a webhook after the date ran answered %d %s %s", r.Status, r.Code, r.Message)
	}
	finish("the webhook")

	save := formBody(map[string]any{"title": "once", "on": "2026-09-19"})
	delete(save, "days")
	if r := f.book.Update(ctx, id, save, ScheduleAuthority{}); r.Code != "" {
		t.Fatalf("giving it a new date answered %d %s %s", r.Status, r.Code, r.Message)
	}
	if fired() {
		t.Fatal("the new date carried the old date's fired_at")
	}
	f.set(time.Date(2026, 9, 19, 9, 0, 30, 0, time.UTC))
	if p := f.book.Beat(ctx); p.Fired != 1 {
		t.Fatalf("the new date's beat: %+v, want it run", p)
	}
	if got := f.term.opened(); got != 4 || len(f.runs(t, id)) != 4 {
		t.Fatalf("opened %d sessions and recorded %d runs, want four of each", got, len(f.runs(t, id)))
	}
}
