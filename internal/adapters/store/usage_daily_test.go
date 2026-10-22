package store

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestDailyProjectMigrationPreservesHistoricalTokens(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.SaveUsageRow(ctx, UsageRow{Assistant: "codex", Conversation: "old", State: json.RawMessage(`{"daily_version":1,"daily_project":"/removed/orphan","daily":{"2026-10-18":{"input":17}}}`)}); err != nil {
		t.Fatal(err)
	}
	// Recreate the previous on-disk shape, rather than only testing a fresh DB.
	if _, err := s.db.Exec(`ALTER TABLE usage_daily DROP COLUMN project_key`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rows, err := s.UsageDaily(ctx, "2026-10-18", "2026-10-19")
	if err != nil || len(rows) != 1 || rows[0].Input != 17 || rows[0].Project != "" {
		t.Fatalf("old rows must survive as unattributed: %+v, %v", rows, err)
	}
}

func TestDailyUsesRecordedRepositoryWhenOlderTaskColumnIsEmpty(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	at := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	if err := s.SaveBrokerTask(ctx, BrokerRow{ID: "legacy", Assistant: "codex", Project: "/removed/source-worktree", CreatedAt: at,
		Record: json.RawMessage(`{"worktree":{"path":"/removed/child","repository":"/example/source"}}`)}, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveUsageRow(ctx, UsageRow{Assistant: "codex", Conversation: "legacy", TaskID: "legacy",
		State: json.RawMessage(`{"daily_version":1,"daily_project":"/removed/child","daily":{"2026-10-18":{"input":19}}}`)}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.UsageDaily(ctx, "2026-10-18", "2026-10-19")
	if err != nil || len(rows) != 1 || rows[0].Project != "/example/source" || rows[0].Input != 19 {
		t.Fatalf("legacy token attribution: %+v, %v", rows, err)
	}
	tasks, err := s.UsageOverviewTasks(ctx, at.Add(-time.Hour), at.Add(time.Hour))
	if err != nil || len(tasks) != 1 || tasks[0].Project != rows[0].Project {
		t.Fatalf("legacy task attribution: %+v, %v", tasks, err)
	}
}

func TestDailyAmbiguousWorktreesAndParentCyclesStayUnattributed(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	for i, project := range []string{"/example/one", "/example/two"} {
		if err := s.SaveBrokerTask(ctx, BrokerRow{ID: fmt.Sprint(i), Assistant: "claude", Project: project, Record: json.RawMessage(`{"child_conversation_id":"ambiguous","worktree":{"path":"/removed/shared"}}`)}, nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"ambiguous", "path-only", "cycle-one", "cycle-two"} {
		parent := ""
		if id == "cycle-one" {
			parent = "cycle-two"
		} else if id == "cycle-two" {
			parent = "cycle-one"
		}
		if err := s.SaveUsageRow(ctx, UsageRow{Assistant: "claude", Conversation: id, Parent: parent, State: json.RawMessage(`{"daily_version":1,"daily_project":"/removed/shared","daily":{"2026-10-18":{"input":11}}}`)}); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := s.UsageDaily(ctx, "2026-10-18", "2026-10-19")
	if err != nil || len(rows) != 4 {
		t.Fatalf("attribution did not terminate: %+v, %v", rows, err)
	}
	for _, row := range rows {
		if row.Project != "" || row.Input != 11 {
			t.Fatalf("ambiguous evidence must not choose a project or multiply tokens: %+v", row)
		}
	}
}

func BenchmarkDailyWorktreeAttribution(b *testing.B) {
	s, err := Open(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	_, err = s.db.ExecContext(ctx, `WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<2000)
 INSERT INTO broker_tasks(id,project,repository,assistant,state,created_at,updated_at,secret_hash,record)
 SELECT 't-'||x,'/example/source','/example/source','claude','success',0,0,'',json_object('worktree',json_object('path','/removed/'||x)) FROM n;
 WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<2000)
 INSERT INTO usage_transcripts(assistant,conversation,path) SELECT 'claude','c-'||x,'' FROM n;
 WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<2000)
 INSERT INTO usage_daily(assistant,conversation,date,project,input,cache_read,cache_write,output)
 SELECT 'claude','c-'||x,'2026-10-19','/removed/'||x||'/subdirectory',10,0,0,0 FROM n;`)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows, err := s.UsageDaily(ctx, "2026-10-19", "2026-10-20")
		if err != nil || len(rows) != 2000 {
			b.Fatalf("rows=%d, %v", len(rows), err)
		}
		for _, row := range rows {
			if row.Project != "/example/source" {
				b.Fatalf("wrong project: %+v", row)
			}
		}
	}
}

func TestDailyCountsOwnRowsWithoutFoldingParentsTwice(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	parent := UsageRow{Assistant: "claude", Conversation: "parent", Measured: json.RawMessage(`{"input":999}`), State: json.RawMessage(`{"daily_version":1,"daily_project":"/example/repo","daily_project_key":"/example/repo","daily":{"2026-10-18":{"input":30}}}`)}
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
	_, err = s.db.ExecContext(ctx, `WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<?) INSERT INTO usage_daily(assistant,conversation,date,project,input,cache_read,cache_write,output) SELECT 'claude','seed-'||x,'2026-10-01','',1,0,0,0 FROM n`, UsageDailyRowLimit)
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
	oldLoss, err := s.UsageDailyEvictedInRange(ctx, "2026-10-01", "2026-10-02")
	if err != nil || oldLoss != 2 {
		t.Fatalf("dated eviction %d %v", oldLoss, err)
	}
	newLoss, err := s.UsageDailyEvictedInRange(ctx, "2026-10-18", "2026-10-20")
	if err != nil || newLoss != 0 {
		t.Fatalf("new period inherits old loss %d %v", newLoss, err)
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

func TestDailyCoverageRequiresCaughtUpReadableSources(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	for i, r := range []UsageRow{
		{State: json.RawMessage(`{"daily_version":1}`)},
		{State: json.RawMessage(`{"daily_version":1,"daily_replay":{"offset":64}}`)},
		{State: json.RawMessage(`{"daily_version":1}`), More: true},
		{State: json.RawMessage(`{"daily_version":1}`), Reason: UsageTranscriptUnreadable},
		{State: json.RawMessage(`{"daily_version":1,"daily_unknown":true}`)},
	} {
		r.Assistant = "claude"
		r.Conversation = fmt.Sprint(i)
		r.ReadAt = time.Now()
		if err := s.SaveUsageRow(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	read, total, _, err := s.UsageDailyCoverage(ctx)
	if err != nil || read != 1 || total != 5 {
		t.Fatalf("caught up %d/%d %v", read, total, err)
	}
}
