package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func openEventStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if _, err := st.db.Exec(`DELETE FROM events`); err != nil {
		t.Fatal(err)
	}
	return st
}

// seedEvents writes rows with their own times, in one transaction, in order.
func seedEvents(t *testing.T, st *Store, rows [][3]any) {
	t.Helper()
	tx, err := st.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tx.Prepare(`INSERT INTO events (at, kind, subject, payload) VALUES (?, ?, ?, '')`)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if _, err := stmt.Exec(r[0], r[1], r[2]); err != nil {
			t.Fatal(err)
		}
	}
	stmt.Close()
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func eventRows(t *testing.T, st *Store) map[string]int {
	t.Helper()
	rows, err := st.db.Query(`SELECT kind, subject, at FROM events`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var kind, subject string
		var at int64
		if err := rows.Scan(&kind, &subject, &at); err != nil {
			t.Fatal(err)
		}
		out[fmt.Sprintf("%s/%s/%d", kind, subject, at)]++
	}
	return out
}

// Every row a reader can still ask for survives, however old: the newest row
// per subject of each kind LatestEvents is asked about. Everything else past
// the age limit goes, and nothing younger than it is touched.
func TestThePruneKeepsTheNewestRowEachReaderNeeds(t *testing.T) {
	st := openEventStore(t)
	now := time.Unix(2_000_000_000, 0)
	old := now.Add(-EventAgeLimit - time.Hour).Unix()
	older := old - 3600
	young := now.Add(-time.Hour).Unix()
	seedEvents(t, st, [][3]any{
		{older, "session.waiting", "t1|c1"},
		{older, "session.delivered", "t1"},
		{older, "run.latest", "c1"},
		{older, "run.issued", "r-old"},
		{older, "session.typed", "t1"},
		{old, "session.waiting", "t1|c1"},
		{old, "run.latest", "c1"},
		{old, "lease.granted", "repo:x"},
		{young, "run.latest", "c1"},
		{young, "lease.granted", "repo:x"},
		{young, "session.closed", "t2"},
	})
	ctx := context.Background()
	before := map[string]map[string]Event{}
	for kind, subject := range map[string]string{"session.waiting": "t1|c1", "session.delivered": "t1", "run.latest": "c1"} {
		got, err := st.LatestEvents(ctx, kind, []string{subject})
		if err != nil {
			t.Fatal(err)
		}
		before[kind] = got
	}

	got, err := st.PruneEvents(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{
		fmt.Sprintf("session.waiting/t1|c1/%d", old):  1,
		fmt.Sprintf("session.delivered/t1/%d", older): 1,
		fmt.Sprintf("run.latest/c1/%d", young):        1,
		fmt.Sprintf("lease.granted/repo:x/%d", young): 1,
		fmt.Sprintf("session.closed/t2/%d", young):    1,
	}
	left := eventRows(t, st)
	if fmt.Sprint(left) != fmt.Sprint(want) {
		t.Fatalf("after the prune:\n got %v\nwant %v", left, want)
	}
	if got.Removed != 6 {
		t.Fatalf("removed %d rows, want 6", got.Removed)
	}
	for kind, was := range before {
		for subject, e := range was {
			now, err := st.LatestEvents(ctx, kind, []string{subject})
			if err != nil {
				t.Fatal(err)
			}
			if now[subject].Seq != e.Seq {
				t.Fatalf("%s/%s: the reader's answer changed from %d to %d", kind, subject, e.Seq, now[subject].Seq)
			}
		}
	}
	// A second prune has nothing to do.
	again, err := st.PruneEvents(ctx, now)
	if err != nil || again.Removed != 0 {
		t.Fatalf("a second prune: %+v %v", again, err)
	}
}

// Past the row limit the oldest rows that are not kept go first, even young
// ones, until the table is back under it.
func TestThePruneHoldsTheTableUnderItsRowLimit(t *testing.T) {
	st := openEventStore(t)
	now := time.Unix(2_000_000_000, 0)
	var rows [][3]any
	rows = append(rows, [3]any{now.Unix() - 50, "session.delivered", "t1"})
	for i := range 30 {
		rows = append(rows, [3]any{now.Unix() - 40 + int64(i), "lease.granted", fmt.Sprint("r", i)})
	}
	seedEvents(t, st, rows)
	got, err := st.pruneEvents(context.Background(), now, EventAgeLimit, 10, 7)
	if err != nil {
		t.Fatal(err)
	}
	if got.Removed != 21 {
		t.Fatalf("removed %d, want 21 (31 rows to 10, the kept one included)", got.Removed)
	}
	left := eventRows(t, st)
	if left[fmt.Sprintf("session.delivered/t1/%d", now.Unix()-50)] != 1 {
		t.Fatalf("the kept row went: %v", left)
	}
	for i := 21; i < 30; i++ {
		if left[fmt.Sprintf("lease.granted/r%d/%d", i, now.Unix()-40+int64(i))] != 1 {
			t.Fatalf("a newest row went before an older one: %v", left)
		}
	}
}

// A prune whose context has ended deletes nothing and says so.
func TestAnEndedPruneSaysItFailed(t *testing.T) {
	st := openEventStore(t)
	now := time.Unix(2_000_000_000, 0)
	seedEvents(t, st, [][3]any{{now.Add(-EventAgeLimit * 2).Unix(), "lease.granted", "x"}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := st.PruneEvents(ctx, now)
	if !errors.Is(err, context.Canceled) || got.Removed != 0 {
		t.Fatalf("an ended prune: %+v %v", got, err)
	}
	if n := len(eventRows(t, st)); n != 1 {
		t.Fatalf("%d rows left, want the one", n)
	}
}

// The first prune on a daemon that never pruned: a hundred thousand rows past
// their age. It is done in batches, and an Append that arrives meanwhile is
// not held for the whole of it.
func TestAHundredThousandRowPruneIsBatched(t *testing.T) {
	if testing.Short() {
		t.Skip("writes a hundred thousand rows")
	}
	st := openEventStore(t)
	now := time.Now()
	old := now.Add(-EventAgeLimit - time.Hour).Unix()
	rows := make([][3]any, 0, 100_000)
	for i := range 100_000 {
		kind := []string{"lease.granted", "lease.released", "capacity.refused", "session.typed", "run.latest"}[i%5]
		rows = append(rows, [3]any{old, kind, fmt.Sprint("s", i%400)})
	}
	seedEvents(t, st, rows)

	ctx := context.Background()
	stop := make(chan struct{})
	var wg sync.WaitGroup
	var slowest time.Duration
	var appends int
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			began := time.Now()
			if err := st.Append(ctx, Event{Kind: "session.closed", Subject: "live"}); err != nil {
				t.Error(err)
				return
			}
			slowest = max(slowest, time.Since(began))
			appends++
			time.Sleep(time.Millisecond)
		}
	}()
	got, err := st.PruneEvents(ctx, now)
	close(stop)
	wg.Wait()
	if err != nil {
		t.Fatal(err)
	}
	// run.latest's newest row for each of its 80 subjects is kept.
	if got.Removed != 100_000-80 {
		t.Fatalf("removed %d, want %d", got.Removed, 100_000-80)
	}
	if got.Batches < 100_000/EventPruneBatchLimit {
		t.Fatalf("%d batches for 100,000 rows", got.Batches)
	}
	t.Logf("pruned %d rows in %d batches, %s; %d appends meanwhile, the slowest %s",
		got.Removed, got.Batches, got.Took, appends, slowest)
	if slowest > 2*time.Second {
		t.Fatalf("an append waited %s behind the prune", slowest)
	}
}
