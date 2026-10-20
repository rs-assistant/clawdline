package schedule

import (
	"testing"
	"time"
)

// This file is the first test in this repository, and it is here rather than
// anywhere else because this is where the code has actually hurt: twice, a
// schedule opened a real assistant session that nobody asked for. Both times
// the fire was the first one.
//
// The model changed under it — a wall-clock time and a catch-up window, the
// Swift app's file, instead of an interval — and the incident did not go away
// with the old model: a file with no `created_at` inside its six-hour window is
// the second incident in a new spelling. So the same cells are asked again of
// the new decision.
//
// Everything below states a moment and asks one question of it, so a failure
// names the rule it broke rather than the line it stopped on.

var loc = time.FixedZone("test", 8*3600)

func local(y int, m time.Month, d, h, min int) time.Time {
	return time.Date(y, m, d, h, min, 0, 0, loc)
}

func daily(h, m int) Schedule {
	return Schedule{Enabled: true, When: When{Hour: h, Minute: m, Daily: true},
		CatchUpHours: DefaultCatchUpHours}
}

func decide(s Schedule, now, firstSeen, handled time.Time) Action {
	fire := s.When.LatestFire(now, loc)
	return s.Decide(Occurrence{Now: now, Fire: fire, FirstSeen: firstSeen, Handled: handled})
}

func TestNeverFiresOnSight(t *testing.T) {
	now := local(2026, 9, 16, 12, 0)
	nine := daily(9, 0) // three hours ago, inside the six-hour window

	cases := []struct {
		name      string
		firstSeen time.Time
		want      Action
	}{
		// The incident. A row arrives with no stamp of its own — imported,
		// restored, copied — and its last occurrence is inside the window.
		{"seen this instant, no created_at", now, BeforeSighting},
		// A row nobody has claimed to have seen cannot be fired at all.
		{"never seen at all", time.Time{}, BeforeSighting},
		// Seen before its minute: it was here at nine, so nine is owed.
		{"seen before its minute", local(2026, 9, 16, 8, 0), Run},
	}
	for _, c := range cases {
		if got := decide(nine, now, c.firstSeen, time.Time{}); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}

// A schedule made at lunchtime for nine does not run this morning's nine, and
// one retimed from 21:00 to 09:00 at two does not invent a nine o'clock either.
func TestStampsGateTheirOwnOccurrences(t *testing.T) {
	now := local(2026, 9, 16, 14, 0)
	seen := local(2026, 9, 1, 0, 0)

	made := daily(9, 0)
	made.CreatedAt = local(2026, 9, 16, 13, 0)
	if got := decide(made, now, seen, time.Time{}); got != BeforeCreation {
		t.Errorf("made after its minute: %s, want %s", got, BeforeCreation)
	}

	retimed := daily(9, 0)
	retimed.WhenChangedAt = local(2026, 9, 16, 13, 59)
	if got := decide(retimed, now, seen, time.Time{}); got != BeforeRetiming {
		t.Errorf("retimed after its minute: %s, want %s", got, BeforeRetiming)
	}
}

// A restart inside the catch-up window must not run the same occurrence twice,
// and a daemon that was down through an occurrence it had seen coming runs it
// once when it is back — never a batch.
func TestRestartNeitherRepeatsNorForgets(t *testing.T) {
	seen := local(2026, 9, 1, 0, 0)
	nine := daily(9, 0)
	fire := local(2026, 9, 16, 9, 0)

	if got := decide(nine, local(2026, 9, 16, 9, 30), seen, fire); got != AlreadyHandled {
		t.Errorf("handled before the restart: %s, want %s", got, AlreadyHandled)
	}
	if got := decide(nine, local(2026, 9, 16, 9, 30), seen, local(2026, 9, 15, 9, 0)); got != Run {
		t.Errorf("down through today's nine: %s, want %s", got, Run)
	}
	// Down for three days: only the latest occurrence is ever asked about.
	if got := nine.When.LatestFire(local(2026, 9, 19, 9, 30), loc); !got.Equal(local(2026, 9, 19, 9, 0)) {
		t.Errorf("latest after three days down: %v", got)
	}
	if got := decide(nine, local(2026, 9, 16, 16, 0), seen, local(2026, 9, 15, 9, 0)); got != Missed {
		t.Errorf("outside the window: %s, want %s", got, Missed)
	}
}

func TestActiveRunSkipsTheOccurrence(t *testing.T) {
	nine := daily(9, 0)
	o := Occurrence{Now: local(2026, 9, 16, 9, 1), Fire: local(2026, 9, 16, 9, 0),
		FirstSeen: local(2026, 9, 1, 0, 0), Active: true}
	if got := nine.Decide(o); got != Active {
		t.Errorf("a run still working: %s, want %s", got, Active)
	}
}

func TestOnceIsSpentByItsStamp(t *testing.T) {
	obj := map[string]any{"at": "09:00", "on": "2026-09-16"}
	w, err := ParseWhen(obj)
	if err != nil {
		t.Fatal(err)
	}
	s := Schedule{Enabled: true, When: w, CatchUpHours: 6}
	now := local(2026, 9, 16, 9, 30)
	seen := local(2026, 9, 1, 0, 0)
	if got := decide(s, now, seen, time.Time{}); got != Run {
		t.Errorf("a one-shot at its minute: %s, want %s", got, Run)
	}
	s.FiredAt = local(2026, 9, 16, 9, 0)
	if got := decide(s, now, seen, time.Time{}); got != Spent {
		t.Errorf("a one-shot that ran: %s, want %s", got, Spent)
	}
	if next := w.NextFire(now, loc); !next.IsZero() {
		t.Errorf("a one-shot past its minute has a next fire: %v", next)
	}
}

func TestWhenGrammar(t *testing.T) {
	refused := []map[string]any{
		{"at": "09:00"},
		{"at": "09:00", "days": "daily", "on": "2026-09-16"},
		{"at": "9:00", "days": "daily"},
		{"at": "24:00", "days": "daily"},
		{"at": "09:00", "days": []any{}},
		{"at": "09:00", "days": []any{"mon", "mon"}},
		{"at": "09:00", "days": []any{"monday"}},
		{"at": "09:00", "on": "2026-02-30"},
	}
	for _, obj := range refused {
		if _, err := ParseWhen(obj); err == nil {
			t.Errorf("accepted %v", obj)
		}
	}
	w, err := ParseWhen(map[string]any{"at": "07:05", "days": []any{"fri", "mon"}})
	if err != nil {
		t.Fatal(err)
	}
	days, _ := w.Object()["days"].([]string)
	if len(days) != 2 || days[0] != "mon" || days[1] != "fri" {
		t.Errorf("weekdays back out of order: %v", days)
	}
	// Monday 2026-09-14; the next Friday is the 18th.
	if next := w.NextFire(local(2026, 9, 14, 8, 0), loc); !next.Equal(local(2026, 9, 18, 7, 5)) {
		t.Errorf("next after Monday 08:00: %v", next)
	}
}

// A trigger-only schedule has no clock: it is spelled exactly
// `{"trigger_only": true}`, it has no occurrence to run, miss or catch up at
// any moment, and it reads back in its own spelling.
func TestATriggerOnlyScheduleHasNoClock(t *testing.T) {
	w, err := ParseWhen(map[string]any{"trigger_only": true})
	if err != nil || !w.TriggerOnly || w.Once() {
		t.Fatalf("trigger_only: %+v %v", w, err)
	}
	for _, bad := range []map[string]any{
		{"trigger_only": false},
		{"trigger_only": "true"},
		{"trigger_only": true, "at": "09:00"},
		{"trigger_only": true, "days": "daily"},
		{"trigger_only": true, "on": "2026-09-30"},
		{"at": "09:00"},
		{},
	} {
		if _, err := ParseWhen(bad); err == nil {
			t.Errorf("%v was accepted", bad)
		}
	}
	if got := w.Object(); len(got) != 1 || got["trigger_only"] != true {
		t.Fatalf("Object: %v", got)
	}
	timed := When{Hour: 9, Daily: true}
	if w.Same(timed) || timed.Same(w) || !w.Same(When{TriggerOnly: true}) {
		t.Fatal("Same does not tell a trigger-only schedule from a timed one")
	}
	s := Schedule{Enabled: true, When: w, CatchUpHours: MaxCatchUpHours}
	// Across a week of minutes-apart moments, including midnight (Hour and
	// Minute are zero), there is never a latest or a next fire.
	for now := local(2026, 9, 14, 0, 0); now.Before(local(2026, 9, 21, 0, 0)); now = now.Add(97 * time.Minute) {
		if !w.LatestFire(now, loc).IsZero() || !w.NextFire(now, loc).IsZero() {
			t.Fatalf("a trigger-only schedule has an occurrence at %s", now)
		}
	}
	// Even asked directly about a moment, the decision is never run or missed.
	now := local(2026, 9, 16, 12, 0)
	if got := s.Decide(Occurrence{Now: now, Fire: local(2026, 9, 16, 0, 0), FirstSeen: local(2026, 9, 1, 0, 0)}); got != NoClock {
		t.Fatalf("Decide: %s, want %s", got, NoClock)
	}
}

func TestCalendarIntervalOccurrences(t *testing.T) {
	for _, tc := range []struct {
		start                   string
		n                       int64
		zone, now, latest, next string
	}{
		{"2026-10-21", 3, "Asia/Taipei", "2026-10-20T10:00:00+08:00", "", "2026-10-21T09:00:00+08:00"},
		{"2026-10-21", 3, "Asia/Taipei", "2026-10-21T08:59:00+08:00", "", "2026-10-21T09:00:00+08:00"},
		{"2026-10-21", 3, "Asia/Taipei", "2026-10-21T09:00:00+08:00", "2026-10-21T09:00:00+08:00", "2026-10-24T09:00:00+08:00"},
		{"2026-10-21", 3, "Asia/Taipei", "2026-11-02T08:00:00+08:00", "2026-10-30T09:00:00+08:00", "2026-11-02T09:00:00+08:00"},
		{"2028-02-27", 3, "UTC", "2028-03-01T09:00:00Z", "2028-03-01T09:00:00Z", "2028-03-04T09:00:00Z"},
		{"2026-03-06", 3, "America/New_York", "2026-03-09T08:00:00-04:00", "2026-03-06T09:00:00-05:00", "2026-03-09T09:00:00-04:00"},
		{"2026-10-30", 3, "America/New_York", "2026-11-02T09:00:00-05:00", "2026-11-02T09:00:00-05:00", "2026-11-05T09:00:00-05:00"},
		{"2026-01-01", 30, "UTC", "2026-03-05T09:00:00Z", "2026-03-02T09:00:00Z", "2026-04-01T09:00:00Z"},
		{"1970-01-01", 9223372036854775807, "UTC", "2026-01-01T09:00:00Z", "1970-01-01T09:00:00Z", ""},
	} {
		t.Run(tc.now+tc.zone+tc.start, func(t *testing.T) {
			w, err := ParseWhen(map[string]any{"at": "09:00", "every_days": tc.n, "start": tc.start})
			if err != nil {
				t.Fatal(err)
			}
			loc, err := time.LoadLocation(tc.zone)
			if err != nil {
				t.Fatal(err)
			}
			now, err := time.Parse(time.RFC3339, tc.now)
			if err != nil {
				t.Fatal(err)
			}
			for name, got := range map[string]time.Time{"latest": w.LatestFire(now, loc), "next": w.NextFire(now, loc)} {
				want := tc.latest
				if name == "next" {
					want = tc.next
				}
				if want == "" {
					if !got.IsZero() {
						t.Fatalf("%s=%s", name, got)
					}
					continue
				}
				parsed, _ := time.Parse(time.RFC3339, want)
				if !got.Equal(parsed) {
					t.Fatalf("%s=%s want %s", name, got, want)
				}
			}
			round, err := ParseWhen(w.Object())
			if err != nil || !w.Same(round) {
				t.Fatalf("round trip: %+v %v", round, err)
			}
		})
	}
}

func TestCalendarIntervalRejectsAmbiguousOrInvalidRules(t *testing.T) {
	for _, patch := range []map[string]any{
		{"every_days": 0}, {"every_days": -3}, {"every_days": "3"}, {"every_days": 1.5},
		{"start": "2026-02-30"}, {"start": "1969-12-31"}, {"days": "daily"}, {"on": "2026-10-21"}, {"trigger_only": true},
	} {
		obj := map[string]any{"at": "09:00", "every_days": int64(3), "start": "2026-10-21"}
		for k, v := range patch {
			obj[k] = v
		}
		if _, err := ParseWhen(obj); err == nil {
			t.Fatalf("accepted %v", obj)
		}
	}
	for _, obj := range []map[string]any{{"at": "09:00", "every_days": int64(3)}, {"at": "09:00", "start": "2026-10-21"}} {
		if _, err := ParseWhen(obj); err == nil {
			t.Fatalf("accepted %v", obj)
		}
	}
	a, _ := ParseWhen(map[string]any{"at": "09:00", "every_days": int64(3), "start": "2026-10-21"})
	for _, obj := range []map[string]any{{"at": "09:00", "every_days": int64(4), "start": "2026-10-21"}, {"at": "09:00", "every_days": int64(3), "start": "2026-10-22"}} {
		b, _ := ParseWhen(obj)
		if a.Same(b) {
			t.Fatal("retime not detected")
		}
	}
}

func TestCalendarIntervalUsesExistingCatchUpAndSightingRules(t *testing.T) {
	w, err := ParseWhen(map[string]any{"at": "09:00", "every_days": int64(3), "start": "2026-10-21"})
	if err != nil {
		t.Fatal(err)
	}
	s := Schedule{Enabled: true, When: w, CatchUpHours: 6}
	seen := local(2026, 10, 20, 0, 0)
	for _, tc := range []struct {
		now, first, handled time.Time
		want                Action
	}{
		{local(2026, 10, 24, 10, 0), seen, time.Time{}, Run},
		{local(2026, 10, 24, 16, 0), seen, time.Time{}, Missed},
		{local(2026, 10, 24, 10, 0), local(2026, 10, 24, 10, 0), time.Time{}, BeforeSighting},
		{local(2026, 10, 24, 10, 0), seen, local(2026, 10, 24, 9, 0), AlreadyHandled},
	} {
		if got := decide(s, tc.now, tc.first, tc.handled); got != tc.want {
			t.Fatalf("at %s: %s want %s", tc.now, got, tc.want)
		}
	}
	one, _ := ParseWhen(map[string]any{"at": "09:00", "every_days": int64(1), "start": "2026-01-01"})
	daily := When{Hour: 9, Daily: true}
	for day := 1; day <= 365; day++ {
		now := local(2026, 1, day, 10, 0)
		if !one.NextFire(now, loc).Equal(daily.NextFire(now, loc)) || !one.LatestFire(now, loc).Equal(daily.LatestFire(now, loc)) {
			t.Fatalf("one-day interval differs from daily at %s", now)
		}
	}
}
