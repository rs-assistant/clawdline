package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
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
	got, err := st.pruneEvents(context.Background(), now, EventAgeLimit, EventKeptAgeLimit, 10, 7)
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

// The newest row about a subject is kept only while its subject is younger
// than EventKeptAgeLimit; past it the Session or run is gone and so is the row.
func TestAKeptRowGoesOnceItsSubjectIsSilentTooLong(t *testing.T) {
	st := openEventStore(t)
	now := time.Unix(2_000_000_000, 0)
	expired := now.Add(-EventKeptAgeLimit - time.Hour).Unix()
	kept := now.Add(-EventKeptAgeLimit + time.Hour).Unix()
	seedEvents(t, st, [][3]any{
		{expired, "session.delivered", "gone"},
		{expired, "run.latest", "gone"},
		{expired, "session.waiting", "gone|c"},
		{kept, "session.delivered", "still"},
		{kept, "run.latest", "still"},
		{kept, "session.waiting", "still|c"},
	})
	ctx := context.Background()
	got, err := st.PruneEvents(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Removed != 3 {
		t.Fatalf("removed %d, want the three silent subjects", got.Removed)
	}
	for key := range eventRows(t, st) {
		if strings.Contains(key, "gone") {
			t.Fatalf("an expired kept row survived: %v", eventRows(t, st))
		}
	}
	// The rows the walk kept are behind its cursor now; a later prune, once
	// they too are silent past the limit, lets them go without walking again.
	later := now.Add(2 * time.Hour)
	got, err = st.PruneEvents(ctx, later)
	if err != nil {
		t.Fatal(err)
	}
	if got.Removed != 3 || len(eventRows(t, st)) != 0 {
		t.Fatalf("a later prune removed %d, left %v", got.Removed, eventRows(t, st))
	}
}

// A prune starts where the last one stopped: the rows it already kept are not
// walked again, and a kept row behind it that a newer row has superseded since
// still goes.
func TestThePruneResumesWhereTheLastOneStopped(t *testing.T) {
	st := openEventStore(t)
	now := time.Unix(2_000_000_000, 0)
	old := now.Add(-EventAgeLimit - time.Hour).Unix()
	var rows [][3]any
	for i := range 50 {
		rows = append(rows, [3]any{old, "run.latest", fmt.Sprint("c", i)})
	}
	rows = append(rows, [3]any{old, "lease.granted", "x"}, [3]any{now.Unix(), "lease.granted", "y"})
	seedEvents(t, st, rows)
	var lastOld int64
	if err := st.db.QueryRow(`SELECT MAX(seq) FROM events WHERE at = ?`, old).Scan(&lastOld); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := st.PruneEvents(ctx, now); err != nil {
		t.Fatal(err)
	}
	if st.prunes.cursor != lastOld {
		t.Fatalf("the prune stopped at %d, want after the last old row %d", st.prunes.cursor, lastOld)
	}

	// c7 is written again. Its old row is behind the cursor; it goes, and no
	// other kept row is touched.
	if err := st.Append(ctx, Event{Kind: "run.latest", Subject: "c7"}); err != nil {
		t.Fatal(err)
	}
	got, err := st.PruneEvents(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Removed != 1 {
		t.Fatalf("removed %d, want c7's superseded row", got.Removed)
	}
	// It read y and c7's new row, not the kept rows again.
	if got.Walked != 2 {
		t.Fatalf("the resumed prune walked %d rows, want the 2 after where the last one stopped", got.Walked)
	}
	latest, err := st.LatestEvents(ctx, "run.latest", []string{"c7", "c8"})
	if err != nil {
		t.Fatal(err)
	}
	if latest["c7"].Seq <= lastOld || latest["c8"].Seq == 0 || latest["c8"].Seq > lastOld {
		t.Fatalf("after the resumed prune: %+v", latest)
	}
	if n := len(eventRows(t, st)); n != 49+1+1 {
		t.Fatalf("%d rows left, want the 49 other kept rows, c7's new row and y", n)
	}
}

// A prune that fails part way does not move its cursor past rows it did not
// delete: the next one still finds them.
func TestAFailedPruneLeavesItsCursorBehind(t *testing.T) {
	st := openEventStore(t)
	now := time.Unix(2_000_000_000, 0)
	old := now.Add(-EventAgeLimit - time.Hour).Unix()
	seedEvents(t, st, [][3]any{{old, "lease.granted", "x"}, {old, "lease.granted", "y"}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := st.PruneEvents(ctx, now); !errors.Is(err, context.Canceled) {
		t.Fatalf("an ended prune: %v", err)
	}
	if st.prunes.cursor != 0 {
		t.Fatalf("the cursor moved to %d without a deletion", st.prunes.cursor)
	}
	got, err := st.PruneEvents(context.Background(), now)
	if err != nil || got.Removed != 2 {
		t.Fatalf("the next prune: %+v %v", got, err)
	}
}

// The keep-set on a fixture shaped like this machine's ledger: four months of
// sessions, each day a few terminals and conversations, each with its runs,
// deliveries and waiting stops. Kept forever, every subject ever seen stays;
// with EventKeptAgeLimit only the last thirty days' do.
func TestTheKeepSetOnAFourMonthFixture(t *testing.T) {
	now := time.Unix(2_000_000_000, 0)
	var rows [][3]any
	for day := 120; day > 0; day-- {
		at := now.Add(-time.Duration(day) * 24 * time.Hour).Unix()
		for c := range 6 {
			terminal := fmt.Sprintf("t%d-%d", day, c%3)
			conversation := fmt.Sprintf("c%d-%d", day, c)
			for range 3 {
				rows = append(rows, [3]any{at, "run.issued", fmt.Sprintf("r%d-%d", day, len(rows))},
					[3]any{at, "run.latest", conversation}, [3]any{at, "run.latest", terminal},
					[3]any{at, "session.typed", terminal})
			}
			rows = append(rows, [3]any{at, "session.delivered", terminal},
				[3]any{at, "session.waiting", terminal + "|" + conversation},
				[3]any{at, "session.waiting", terminal + "|" + conversation})
		}
	}
	kept := func(keptAge time.Duration) int {
		st := openEventStore(t)
		seedEvents(t, st, rows)
		if _, err := st.pruneEvents(context.Background(), now, EventAgeLimit, keptAge, EventKeepLimit, EventPruneBatchLimit); err != nil {
			t.Fatal(err)
		}
		// The keep-set: the subjects of the kept kinds that still have a row.
		var n int
		if err := st.db.QueryRow(`SELECT COUNT(*) FROM (SELECT DISTINCT kind, subject FROM events
			WHERE kind IN ('run.latest', 'session.delivered', 'session.waiting'))`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	forever, bounded := kept(100*365*24*time.Hour), kept(EventKeptAgeLimit)
	t.Logf("%d rows over 120 days; the keep-set is %d subjects kept forever, %d with EventKeptAgeLimit",
		len(rows), forever, bounded)
	// Each day has 18 subjects: run.latest for six conversations and three
	// terminals, three terminals' deliveries, six waiting stops.
	if want := 120 * 18; forever != want {
		t.Fatalf("kept forever: %d, want every subject, %d", forever, want)
	}
	if want := 30 * 18; bounded != want {
		t.Fatalf("kept with the limit: %d, want the last thirty days' subjects, %d", bounded, want)
	}
}

// A kept row superseded while a long pass is walking is deleted by the next
// pass. The pass takes the top of its window before it walks, so a row
// appended meanwhile is after that mark and the next pass reads it; a mark
// taken after the walk would skip it, and the old row would be kept for good.
func TestARowSupersededDuringAPassGoesOnTheNext(t *testing.T) {
	if testing.Short() {
		t.Skip("writes sixty thousand rows")
	}
	st := openEventStore(t)
	now := time.Now()
	old := now.Add(-EventAgeLimit - time.Hour).Unix()
	rows := [][3]any{{old, "run.latest", "superseded"}}
	for i := range 60_000 {
		rows = append(rows, [3]any{old, "lease.granted", fmt.Sprint("s", i%400)})
	}
	seedEvents(t, st, rows)
	ctx := context.Background()

	done := make(chan error, 1)
	var finished atomic.Bool
	go func() {
		_, err := st.PruneEvents(ctx, now)
		finished.Store(true)
		done <- err
	}()
	time.Sleep(5 * time.Millisecond)
	if err := st.Append(ctx, Event{Kind: "run.latest", Subject: "superseded"}); err != nil {
		t.Fatal(err)
	}
	appendedDuring := !finished.Load()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !appendedDuring {
		t.Skip("the pass ended before the append; this machine is too fast for the fixture")
	}
	if _, err := st.PruneEvents(ctx, now); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM events WHERE kind = 'run.latest' AND subject = 'superseded'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("%d run.latest rows for the subject after two passes, want only the one that superseded the old", n)
	}
}
