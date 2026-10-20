package app

import (
	"context"
	"testing"
	"time"
)

func TestScheduleUsesItsNamedTimeZoneInsteadOfTheMachineZone(t *testing.T) {
	f := newW4(t)
	ctx := context.Background()
	f.set(time.Date(2026, 9, 18, 0, 30, 0, 0, time.UTC))
	f.book.Places = func(context.Context) []SchedulePlace {
		return []SchedulePlace{{ID: "p1", Label: "p", Path: f.dir}}
	}
	made := f.book.Create(ctx, map[string]any{
		"title": "Taipei morning", "at": "09:00", "days": "daily", "time_zone": "Asia/Taipei",
		"place_id": "p1", "assistant": "claude", "instructions": "do it", "enabled": true,
	}, ScheduleAuthority{})
	if made.Code != "" {
		t.Fatalf("create refused: %s %s", made.Code, made.Message)
	}
	summary := made.Body["schedule"].(map[string]any)
	want := time.Date(2026, 9, 18, 1, 0, 0, 0, time.UTC).Unix()
	if got := summary["next_fire"]; got != want {
		t.Fatalf("next_fire = %v, want %d (09:00 Asia/Taipei)", got, want)
	}

	f.set(time.Date(2026, 9, 18, 1, 0, 30, 0, time.UTC))
	if pulse := f.book.Beat(ctx); pulse.Due != 1 || pulse.Fired != 1 {
		t.Fatalf("09:00 Asia/Taipei beat = %+v, want one firing", pulse)
	}
}

func TestScheduleTimeZoneIsValidatedAndReturned(t *testing.T) {
	f := newW4(t)
	ctx := context.Background()
	f.book.Places = func(context.Context) []SchedulePlace {
		return []SchedulePlace{{ID: "p1", Label: "p", Path: f.dir}}
	}
	body := map[string]any{"title": "zone", "at": "09:00", "days": "daily", "place_id": "p1",
		"assistant": "claude", "instructions": "do it", "enabled": true}
	body["time_zone"] = "not/a-zone"
	if reply := f.book.Create(ctx, body, ScheduleAuthority{}); reply.Code != "bad_request" {
		t.Fatalf("invalid time zone answered %q %q", reply.Code, reply.Message)
	}
	body["time_zone"] = "Asia/Taipei"
	made := f.book.Create(ctx, body, ScheduleAuthority{})
	if made.Code != "" {
		t.Fatalf("create refused: %s %s", made.Code, made.Message)
	}
	id := made.Body["schedule"].(map[string]any)["id"].(string)
	record, ok, err := f.book.Detail(ctx, id)
	if err != nil || !ok || record["time_zone"] != "Asia/Taipei" {
		t.Fatalf("detail = %v, %v, %v; want named time zone", record, ok, err)
	}
}

func TestChangingOnlyScheduleTimeZoneIsARetime(t *testing.T) {
	f := newW4(t)
	ctx := context.Background()
	f.book.Places = func(context.Context) []SchedulePlace {
		return []SchedulePlace{{ID: "p1", Label: "p", Path: f.dir}}
	}
	body := map[string]any{"title": "zone", "at": "09:00", "days": "daily", "place_id": "p1",
		"assistant": "claude", "instructions": "do it", "enabled": true}
	body["time_zone"] = "UTC"
	made := f.book.Create(ctx, body, ScheduleAuthority{})
	id := made.Body["schedule"].(map[string]any)["id"].(string)
	f.set(f.at().Add(time.Minute))
	body["time_zone"] = "Asia/Taipei"
	if reply := f.book.Update(ctx, id, body, ScheduleAuthority{}); reply.Code != "" {
		t.Fatalf("update refused: %s %s", reply.Code, reply.Message)
	}
	h, ok, err := f.book.named(ctx, id)
	if err != nil || !ok || h.s.WhenChangedAt.IsZero() {
		t.Fatalf("time-zone-only update did not stamp a retime: %+v, %v, %v", h.s, ok, err)
	}
}

func TestOlderScheduleClientDoesNotEraseAStoredTimeZone(t *testing.T) {
	f := newW4(t)
	ctx := context.Background()
	f.book.Places = func(context.Context) []SchedulePlace {
		return []SchedulePlace{{ID: "p1", Label: "p", Path: f.dir}}
	}
	body := map[string]any{"title": "zone", "at": "09:00", "days": "daily", "place_id": "p1",
		"assistant": "claude", "instructions": "do it", "enabled": true, "time_zone": "Asia/Taipei"}
	made := f.book.Create(ctx, body, ScheduleAuthority{})
	id := made.Body["schedule"].(map[string]any)["id"].(string)
	delete(body, "time_zone") // the body sent by a console from before this field existed
	body["title"] = "renamed"
	if reply := f.book.Update(ctx, id, body, ScheduleAuthority{}); reply.Code != "" {
		t.Fatalf("legacy update refused: %s %s", reply.Code, reply.Message)
	}
	record, ok, err := f.book.Detail(ctx, id)
	if err != nil || !ok || record["time_zone"] != "Asia/Taipei" {
		t.Fatalf("legacy update erased the zone: %v, %v, %v", record, ok, err)
	}
}

func TestScheduleCalendarIntervalSurvivesManualRunAndRetime(t *testing.T) {
	f := newW4(t)
	ctx := context.Background()
	f.set(time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC))
	f.book.Places = func(context.Context) []SchedulePlace { return []SchedulePlace{{ID: "p1", Label: "p", Path: f.dir}} }
	body := map[string]any{"title": "interval", "at": "09:00", "every_days": int64(3), "start": "2026-10-21", "time_zone": "Asia/Taipei", "place_id": "p1", "assistant": "claude", "instructions": "do it", "timeout_minutes": 1}
	made := f.book.Create(ctx, body, ScheduleAuthority{})
	if made.Code != "" {
		t.Fatalf("create: %s %s", made.Code, made.Message)
	}
	id := made.Body["schedule"].(map[string]any)["id"].(string)
	assertNext := func(want time.Time) {
		t.Helper()
		rows, err := f.book.List(ctx)
		if err != nil || len(rows) != 1 || rows[0]["next_fire"] != want.Unix() {
			t.Fatalf("list=%v err=%v want=%s", rows, err, want)
		}
		when, ok := rows[0]["when"].(map[string]any)
		if !ok || when["every_days"] != int64(3) || when["start"] != "2026-10-21" {
			t.Fatalf("lost interval: %v", rows)
		}
	}
	assertNext(time.Date(2026, 10, 21, 1, 0, 0, 0, time.UTC))
	// Running before the first occurrence cannot move its anchor or consume it.
	if r := f.book.Run(ctx, id); !r.OK() {
		t.Fatalf("manual: %+v", r)
	}
	f.set(f.at().Add(2 * time.Minute))
	f.b.Pass(ctx)
	assertNext(time.Date(2026, 10, 21, 1, 0, 0, 0, time.UTC))
	f.set(time.Date(2026, 10, 21, 1, 0, 30, 0, time.UTC))
	if p := f.book.Beat(ctx); p.Fired != 1 {
		t.Fatalf("first: %+v", p)
	}
	f.set(f.at().Add(2 * time.Minute))
	f.b.Pass(ctx)
	assertNext(time.Date(2026, 10, 24, 1, 0, 0, 0, time.UTC))
	if r := f.book.Run(ctx, id); !r.OK() {
		t.Fatalf("manual after occurrence: %+v", r)
	}
	f.set(f.at().Add(2 * time.Minute))
	f.b.Pass(ctx)
	assertNext(time.Date(2026, 10, 24, 1, 0, 0, 0, time.UTC))
	// Several missed dates are not replayed; only the latest due date is caught up.
	f.set(time.Date(2026, 11, 2, 2, 0, 0, 0, time.UTC))
	if p := f.book.Beat(ctx); p.Fired != 1 {
		t.Fatalf("catch-up: %+v", p)
	}
	if p := f.book.Beat(ctx); p.Fired != 0 {
		t.Fatalf("duplicate: %+v", p)
	}
	f.set(f.at().Add(2 * time.Minute))
	f.b.Pass(ctx)
	body["start"] = "2026-11-03"
	if r := f.book.Update(ctx, id, body, ScheduleAuthority{}); r.Code != "" {
		t.Fatalf("retime: %+v", r)
	}
	h, ok, err := f.book.named(ctx, id)
	if err != nil || !ok || h.s.WhenChangedAt.IsZero() {
		t.Fatalf("retime not stamped: %+v %v", h, err)
	}
	if got := h.s.When.NextFire(f.at(), time.UTC); !got.Equal(time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("retimed next: %s", got)
	}
}

func TestScheduleIntervalFormRejectsMixedRules(t *testing.T) {
	f := newW4(t)
	f.book.Places = func(context.Context) []SchedulePlace { return []SchedulePlace{{ID: "p1", Path: f.dir}} }
	for _, extra := range []map[string]any{{"days": "daily"}, {"on": "2026-10-21"}, {"trigger_only": true}, {"start": nil}, {"every_days": int64(0)}} {
		body := map[string]any{"title": "interval", "at": "09:00", "every_days": int64(3), "start": "2026-10-21", "place_id": "p1", "assistant": "claude", "instructions": "do it"}
		for k, v := range extra {
			body[k] = v
		}
		if r := f.book.Create(context.Background(), body, ScheduleAuthority{}); r.Code != "bad_request" {
			t.Fatalf("accepted %v: %+v", extra, r)
		}
	}
}
