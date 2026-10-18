package store

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func seedEffect(t *testing.T, s *Store, kind, subject, state string, finished time.Time) int64 {
	t.Helper()
	res, err := s.db.Exec(`INSERT INTO outbox (kind, subject, state, owner, created_at, finished_at)
	  VALUES (?, ?, ?, 'gone', ?, ?)`, kind, subject, state, finished.Add(-time.Second).Unix(), finished.Unix())
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func outboxIDs(t *testing.T, s *Store) map[int64]bool {
	t.Helper()
	rows, err := s.db.Query(`SELECT id FROM outbox`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		out[id] = true
	}
	return out
}

// Done effects a week old go; failed and unknown ones stay, a recent done one
// stays, and for the kinds read by subject the newest row of each subject
// stays, so a pause's history still stops at its newest done row instead of
// surfacing an older unknown.
func TestPruneDoneEffectsKeepsWhatIsStillRead(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	now := time.Now()
	old := now.Add(-8 * 24 * time.Hour)
	recent := now.Add(-2 * 24 * time.Hour)
	const push, pause = "capacity.push", "session.pause.delivery"

	msgOld := seedEffect(t, s, "session.message", "m1", EffectDone, old)
	msgRecent := seedEffect(t, s, "session.message", "m2", EffectDone, recent)
	msgFailed := seedEffect(t, s, "session.message", "m3", EffectFailed, old)
	pushOlder := seedEffect(t, s, push, "row", EffectDone, old.Add(-time.Hour))
	pushNewest := seedEffect(t, s, push, "row", EffectDone, old)
	pushSuperseded := seedEffect(t, s, push, "other", EffectDone, old)
	pushOpen := seedEffect(t, s, push, "other", EffectPending, time.Time{})
	pauseUnknown := seedEffect(t, s, pause, "p1", EffectUnknown, old.Add(-time.Hour))
	pauseNewest := seedEffect(t, s, pause, "p1", EffectDone, old)

	n, err := s.PruneDoneEffects(ctx, now, []string{push, pause})
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("pruned %d effects, want 3", n)
	}
	left := outboxIDs(t, s)
	for id, want := range map[int64]bool{
		msgOld: false, msgRecent: true, msgFailed: true,
		pushOlder: false, pushNewest: true, pushSuperseded: false, pushOpen: true,
		pauseUnknown: true, pauseNewest: true,
	} {
		if left[id] != want {
			t.Errorf("effect %d kept=%v, want %v", id, left[id], want)
		}
	}
	latest, err := s.LatestEffects(ctx, push)
	if err != nil || len(latest) != 2 {
		t.Fatalf("latest capacity pushes after a prune: %v %v", latest, err)
	}
	history, err := s.Effects(ctx, pause, "p1")
	if err != nil || len(history) != 2 || history[1].State != EffectDone {
		t.Fatalf("pause history after a prune: %+v %v", history, err)
	}
}

// A long history goes in bounded batches, one per maintenance pass.
func TestPruneDoneEffectsIsBatched(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	now := time.Now()
	total := RetentionPruneBatchLimit + 120
	for i := 0; i < total; i++ {
		seedEffect(t, s, "session.message", fmt.Sprint("m", i), EffectDone, now.Add(-30*24*time.Hour))
	}
	first, err := s.PruneDoneEffects(ctx, now, nil)
	if err != nil || first != RetentionPruneBatchLimit {
		t.Fatalf("first pass pruned %d (%v), want %d", first, err, RetentionPruneBatchLimit)
	}
	second, err := s.PruneDoneEffects(ctx, now, nil)
	if err != nil || second != 120 {
		t.Fatalf("second pass pruned %d (%v), want 120", second, err)
	}
	if left := len(outboxIDs(t, s)); left != 0 {
		t.Fatalf("%d rows left", left)
	}
}

// Expired receipts with a random key go a week after they were claimed;
// content-derived keys, younger tombstones and live receipts stay, and a
// kept content-derived tombstone still answers expired.
func TestPruneReceiptTombstonesNeverTouchesContentDerivedKeys(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	now := time.Now()
	seed := func(key, state string, created time.Time) {
		t.Helper()
		if _, err := s.db.Exec(`INSERT INTO request_receipts (scope, actor, key, digest, state, created_at)
		  VALUES ('t', 'a', ?, 'd', ?, ?)`, key, state, created.Unix()); err != nil {
			t.Fatal(err)
		}
	}
	old := now.Add(-8 * 24 * time.Hour)
	seed("random-old", "expired", old)
	seed("random-recent", "expired", now.Add(-3*24*time.Hour))
	seed("gate-evidence-0123", "expired", old)
	seed("gate-result-4567", "expired", old)
	seed("task-cancel-89ab", "expired", old)
	seed("complete-old", "complete", old)

	n, err := s.PruneReceiptTombstones(ctx, now)
	if err != nil || n != 1 {
		t.Fatalf("pruned %d tombstones (%v), want 1", n, err)
	}
	rows, err := s.db.Query(`SELECT key FROM request_receipts ORDER BY key`)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for rows.Next() {
		var k string
		_ = rows.Scan(&k)
		keys = append(keys, k)
	}
	rows.Close()
	if got := fmt.Sprint(keys); got != "[complete-old gate-evidence-0123 gate-result-4567 random-recent task-cancel-89ab]" {
		t.Fatalf("receipts left: %s", got)
	}
	c, err := s.ClaimReceipt(ctx, ReceiptKey{Scope: "t", Actor: "a", Key: "gate-result-4567"}, "d", ReceiptPolicy{}, now)
	if err != nil || c.Outcome != ReceiptExpired {
		t.Fatalf("a kept content-derived tombstone answered %+v %v", c, err)
	}
}
