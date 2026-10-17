package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// A root landing is one row per item, repository, commit and target, and an
// item holds at most WorkV2RootLandingLimit of them: the next is refused and
// nothing is evicted.
func TestRootLandingsAreOneRowEachAndBounded(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()
	record := func(commit, tip string) (RootLanding, error) {
		var out RootLanding
		err := s.WriteWorkV2(ctx, func(tx *WorkV2Tx) error {
			var err error
			out, err = tx.RecordRootLanding(RootLanding{WorkID: "w1", SessionID: "s", Repository: "/r", Target: "main",
				Commit: commit, TargetCommit: tip, Remote: "origin", RemoteCommit: tip, RecordedAt: time.Unix(100, 0)})
			return err
		})
		return out, err
	}
	first, err := record("c0", "t1")
	if err != nil {
		t.Fatal(err)
	}
	again, err := record("c0", "t2")
	if err != nil || again != first {
		t.Fatalf("the same landing again = %+v %v, want %+v", again, err, first)
	}
	for n := 1; n < WorkV2RootLandingLimit; n++ {
		if _, err := record(fmt.Sprintf("c%d", n), "t"); err != nil {
			t.Fatalf("row %d: %v", n, err)
		}
	}
	if _, err := record("one-too-many", "t"); !errors.Is(err, ErrRootLandingsFull) {
		t.Fatalf("past the limit: %v", err)
	}
	rows, err := s.RootLandings(ctx, "w1")
	if err != nil || len(rows) != WorkV2RootLandingLimit {
		t.Fatalf("rows = %d %v", len(rows), err)
	}
	kept := false
	for _, r := range rows {
		kept = kept || r == first
	}
	if !kept {
		t.Fatalf("the first landing is gone from %+v", rows)
	}
	// The capacity register reads the fullest item.
	counts, err := s.WorkV2CapacityCounts(ctx)
	if err != nil || counts["root_landings_per_item"] != WorkV2RootLandingLimit {
		t.Fatalf("root landings per item = %d %v", counts["root_landings_per_item"], err)
	}
}

// The landing copies an older daemon wrote into phase events become root
// landing rows when the store opens: one per item, repository, commit and
// target, under the id RecordRootLanding derives, and a second open adds
// nothing.
func TestLegacyLandingCopiesBecomeRootLandingsOnce(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	at := time.Unix(1_790_000_000, 0)
	landing := func(commit, repository string) string {
		repo := ""
		if repository != "" {
			repo = `,"repository":"` + repository + `"`
		}
		return `{"from":"merging","to":"deploying","landing":{"commit":"` + commit + `","target":"main",` +
			`"target_commit":"tip-` + commit + `","remote":"origin","remote_commit":"tip-` + commit + `"` + repo + `}}`
	}
	events := []struct{ work, payload string }{
		{"w-a", landing("c1", "")},
		// The same landing written twice in one history is one row.
		{"w-a", landing("c1", "")},
		{"w-a", landing("c2", "/other")},
		{"w-b", landing("c3", "")},
		// Not a whole proof: never shown as a landing, not made one.
		{"w-b", `{"from":"merging","to":"deploying","landing":{"commit":"c4","target":"main"}}`},
		// Already a row of its own: the event names it.
		{"w-b", `{"from":"merging","to":"deploying","landing_id":"rl-x","landing":{"commit":"c5","target":"main",` +
			`"target_commit":"t","remote":"origin","remote_commit":"t"}}`},
		{"w-b", `{"from":"implementing","to":"merging"}`},
	}
	if err := s.WriteWorkV2(ctx, func(tx *WorkV2Tx) error {
		for _, id := range []string{"w-a", "w-b"} {
			if err := tx.CreateItem(v2Item(id, at), "local", `{}`); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for i, e := range events {
		if _, err := s.db.Exec(`INSERT INTO work_v2_events (work_id, kind, actor, previous_version, next_version, payload, at)
		  VALUES (?, 'item.phase_changed', 'session-old', 1, 1, ?, ?)`, e.work, e.payload, at.Unix()+int64(i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	read := func() map[string][]RootLanding {
		t.Helper()
		s, err := Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		out := map[string][]RootLanding{}
		for _, id := range []string{"w-a", "w-b"} {
			rows, err := s.RootLandings(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			out[id] = rows
		}
		return out
	}
	first := read()
	if len(first["w-a"]) != 2 || len(first["w-b"]) != 1 {
		t.Fatalf("after the first open: %+v", first)
	}
	c1 := first["w-a"][0]
	if c1.ID != RootLandingID("w-a", "/project-a", "c1", "main") || c1.Repository != "/project-a" ||
		c1.TargetCommit != "tip-c1" || c1.Remote != "origin" || c1.RemoteCommit != "tip-c1" ||
		c1.SessionID != "session-old" || !c1.RecordedAt.Equal(at) {
		t.Fatalf("the migrated landing = %+v", c1)
	}
	if other := first["w-a"][1]; other.Repository != "/other" || other.Commit != "c2" {
		t.Fatalf("the other repository's landing = %+v", other)
	}
	second := read()
	if fmt.Sprint(second) != fmt.Sprint(first) {
		t.Fatalf("a second open changed the rows:\n%+v\n%+v", first, second)
	}
}

// An item already at WorkV2RootLandingLimit keeps its rows: a legacy copy is
// left out rather than evicting one, and the open still succeeds.
func TestALegacyLandingCopyDoesNotOverfillAnItem(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	at := time.Unix(1_790_000_000, 0)
	if err := s.WriteWorkV2(ctx, func(tx *WorkV2Tx) error {
		if err := tx.CreateItem(v2Item("w-full", at), "local", `{}`); err != nil {
			return err
		}
		for n := 0; n < WorkV2RootLandingLimit; n++ {
			if _, err := tx.RecordRootLanding(RootLanding{WorkID: "w-full", SessionID: "s", Repository: "/project-a",
				Target: "main", Commit: fmt.Sprintf("c%d", n), TargetCommit: "t", Remote: "origin", RemoteCommit: "t",
				RecordedAt: at}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO work_v2_events (work_id, kind, actor, previous_version, next_version, payload, at)
	  VALUES ('w-full', 'item.phase_changed', 'session-old', 1, 1, ?, ?)`,
		`{"landing":{"commit":"late","target":"main","target_commit":"t","remote":"origin","remote_commit":"t"}}`, at.Unix()); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatalf("a full item stopped the store from opening: %v", err)
	}
	defer s.Close()
	rows, err := s.RootLandings(ctx, "w-full")
	if err != nil || len(rows) != WorkV2RootLandingLimit {
		t.Fatalf("rows = %d %v", len(rows), err)
	}
	for _, r := range rows {
		if r.Commit == "late" {
			t.Fatal("the legacy copy evicted nothing yet was stored past the limit")
		}
	}
}
