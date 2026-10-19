package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestDailySnapshotIsIdempotentAndSurvivesReopen(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "db")
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	row := UsageRow{Assistant: "claude", Conversation: "a", ReadAt: time.Now(), State: json.RawMessage(`{"daily_version":1,"daily_cutoff":"2026-08-24","daily":{"2026-10-18":{"input":10,"cache_read":20,"output":3}}}`)}
	for i := 0; i < 2; i++ {
		if err = s.SaveUsageRow(ctx, row); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rows, err := s.UsageDaily(ctx, "2026-10-01", "2026-10-19")
	if err != nil || len(rows) != 1 || rows[0].Input != 10 {
		t.Fatalf("rows %+v %v", rows, err)
	}
	// An invalid date rolls the entire snapshot/cursor write back.
	row.State = json.RawMessage(`{"daily_version":1,"daily":{"bad":{"input":100}}}`)
	if err = s.SaveUsageRow(ctx, row); err == nil {
		t.Fatal("invalid snapshot committed")
	}
	saved, _, err := s.UsageRow(ctx, "claude", "a")
	if err != nil || string(saved.State) == string(row.State) {
		t.Fatal("cursor advanced on failed daily write")
	}
	rows, err = s.UsageDaily(ctx, "2026-10-01", "2026-10-19")
	if err != nil || len(rows) != 1 || rows[0].Input != 10 {
		t.Fatal("previous snapshot lost")
	}
}

func TestDailyCountsOwnRowsWithoutFoldingParentsTwice(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	parent := UsageRow{Assistant: "claude", Conversation: "parent", Measured: json.RawMessage(`{"input":999}`), State: json.RawMessage(`{"daily_version":1,"daily_project":"/example/repo","daily":{"2026-10-18":{"input":30}}}`)}
	if err = s.SaveUsageRow(ctx, parent); err != nil {
		t.Fatal(err)
	}
	rows, err := s.UsageDaily(ctx, "2026-10-18", "2026-10-19")
	if err != nil {
		t.Fatal(err)
	}
	sum := 0.0
	for _, r := range rows {
		sum += r.Input
	}
	if sum != 30 {
		t.Fatalf("inline fallback %v", sum)
	}
	child := UsageRow{Assistant: "claude", Conversation: "child", Parent: "parent", State: json.RawMessage(`{"daily_version":1,"daily":{"2026-10-18":{"input":30}}}`)}
	if err = s.SaveUsageRow(ctx, child); err != nil {
		t.Fatal(err)
	}
	rows, err = s.UsageDaily(ctx, "2026-10-18", "2026-10-19")
	if err != nil {
		t.Fatal(err)
	}
	sum = 0
	for _, r := range rows {
		sum += r.Input
		if r.Project != "/example/repo" {
			t.Fatalf("unowned project lost %+v", r)
		}
	}
	if sum != 60 {
		t.Fatalf("parent/subagent duplicate %v", sum)
	}
	if err = s.PruneUsageDaily(ctx, time.Date(2027, 1, 1, 0, 0, 0, 0, time.Local)); err != nil {
		t.Fatal(err)
	}
	n, err := s.UsageDailyCount(ctx)
	if err != nil || n != 0 {
		t.Fatalf("idle retention %d %v", n, err)
	}
	if _, ok, err := s.UsageRow(ctx, "claude", "parent"); err != nil || !ok {
		t.Fatal("retention removed cumulative bill")
	}
}

func TestDailyCapacityEvictionIsDurablyVisible(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	// Fill the production bound in one synthetic transaction; no private
	// transcripts or daemon database are used by this test.
	_, err = s.db.ExecContext(ctx, `WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<?) INSERT INTO usage_daily SELECT 'claude','seed-'||x,'2026-10-01','',1,0,0,0 FROM n`, UsageDailyRowLimit)
	if err != nil {
		t.Fatal(err)
	}
	row := UsageRow{Assistant: "claude", Conversation: "new", State: json.RawMessage(`{"daily_version":1,"daily_cutoff":"2026-09-01","daily":{"2026-10-18":{"input":2},"2026-10-19":{"input":3}}}`)}
	if err = s.SaveUsageRow(ctx, row); err != nil {
		t.Fatal(err)
	}
	n, err := s.UsageDailyCount(ctx)
	if err != nil || n != UsageDailyRowLimit {
		t.Fatalf("bound %d %v", n, err)
	}
	_, _, evicted, err := s.UsageDailyCoverage(ctx)
	if err != nil || evicted != 2 {
		t.Fatalf("eviction %d %v", evicted, err)
	}
	rows, err := s.UsageDaily(ctx, "2026-10-18", "2026-10-20")
	if err != nil || len(rows) != 2 {
		t.Fatalf("new summaries %+v %v", rows, err)
	}
	if err = s.SaveUsageRow(ctx, row); err != nil {
		t.Fatal(err)
	}
	_, _, again, err := s.UsageDailyCoverage(ctx)
	if err != nil || again != evicted {
		t.Fatal("repeated snapshot invented evictions")
	}
}
