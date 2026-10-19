package app

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOverviewConservesProjectsPartsAndCompletionTaskCohorts(t *testing.T) {
	old := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = old })
	h := newUsageHarness(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	h.u.Now = func() time.Time { return now }
	task := store.BrokerRow{ID: "t", Project: "example", Assistant: "claude", State: "success", CreatedAt: now.AddDate(0, 0, -2), UpdatedAt: now,
		Record: json.RawMessage(`{"spawned_at":"2026-10-17T12:00:00Z","finished_at":"2026-10-18T12:00:00Z","result":{"status":"success"}}`)}
	if err := h.store.SaveBrokerTask(ctx, task, nil); err != nil {
		t.Fatal(err)
	}
	for _, r := range []store.UsageRow{
		{Assistant: "claude", Conversation: "root", TaskID: "t", ReadAt: now, State: json.RawMessage(`{"daily_version":1,"daily":{"2026-10-17":{"input":10,"cache_read":20,"output":3}}}`)},
		{Assistant: "claude", Conversation: "sub", Parent: "root", ReadAt: now, State: json.RawMessage(`{"daily_version":1,"daily":{"2026-10-17":{"input":5,"output":2}}}`)},
		{Assistant: "codex", Conversation: "unowned", ReadAt: now, State: json.RawMessage(`{"daily_version":1,"daily":{"2026-10-18":{"input":7,"cache_write_1h":4},"2026-10-10":{"input":13}}}`)},
		{Assistant: "codex", Conversation: "legacy", ReadAt: now, State: json.RawMessage(`{"offset":100}`)},
	} {
		if err := h.store.SaveUsageRow(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	got, err := h.u.Overview(ctx, 7, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Totals.Tokens == nil || *got.Totals.Tokens != 51 || got.PreviousTotals.Tokens == nil || *got.PreviousTotals.Tokens != 13 {
		t.Fatalf("totals %+v / %+v", got.Totals, got.PreviousTotals)
	}
	p := got.Totals.Parts
	if p.Input+p.CacheRead+p.CacheWrite+p.Output != *got.Totals.Tokens {
		t.Fatal("parts differ")
	}
	sum := 0.0
	for _, p := range got.Projects {
		sum += p.Tokens
	}
	if sum != *got.Totals.Tokens {
		t.Fatal("projects differ")
	}
	if len(got.Daily) != 7 || len(got.PreviousDaily) != 7 || got.Daily[0].Tokens != nil || got.Daily[0].Coverage != "missing" {
		t.Fatalf("days %+v", got.Daily)
	}
	if got.Totals.Dispatches != 1 || got.Totals.DurationSamples != 1 || got.Totals.DurationMedianSeconds == nil || *got.Totals.DurationMedianSeconds != 86400 || got.Daily[5].Dispatches != 1 || got.Daily[5].DurationSamples != 0 || got.Daily[6].DurationSamples != 1 {
		t.Fatalf("cohorts %+v %+v", got.Totals, got.Daily)
	}
	if got.Coverage.State != "partial" || got.Coverage.SessionsRead != 3 || got.Coverage.SessionsTotal != 4 {
		t.Fatalf("coverage %+v", got.Coverage)
	}
	id := overviewProject("example").ID
	filtered, err := h.u.Overview(ctx, 7, id)
	if err != nil {
		t.Fatal(err)
	}
	if filtered.Totals.Tokens == nil || *filtered.Totals.Tokens != 40 || len(filtered.ProjectOptions) != len(got.ProjectOptions) || len(filtered.Projects) != 1 {
		t.Fatalf("filtered %+v", filtered)
	}
	if _, err = h.u.Overview(ctx, 7, "unknown"); err != ErrUsageOverviewProject {
		t.Fatalf("unknown %v", err)
	}
	if _, err = h.u.Overview(ctx, 8, ""); err != ErrUsageOverviewDays {
		t.Fatalf("days %v", err)
	}
	raw, _ := json.Marshal(got)
	var wire map[string]any
	json.Unmarshal(raw, &wire)
	first := wire["daily"].([]any)[0].(map[string]any)
	if first["tokens"] != nil || first["duration_median_seconds"] != nil {
		t.Fatalf("null gap %s", raw)
	}
}

func TestOverviewCompleteCalendarWindowsRespectDST(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	old := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = old })
	h := newUsageHarness(t)
	h.u.Now = func() time.Time { return time.Date(2026, 3, 10, 12, 0, 0, 0, loc) }
	got, err := h.u.Overview(context.Background(), 7, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Until-got.Since != 7*86400-3600 || got.Daily[0].Date != "2026-03-03" || got.Daily[6].Date != "2026-03-09" || got.Totals.Tokens != nil || got.Coverage.State != "missing" {
		t.Fatalf("DST %+v", got)
	}
	long, err := h.u.Overview(context.Background(), 28, "")
	if err != nil || len(long.Daily) != 28 || len(long.PreviousDaily) != 28 {
		t.Fatalf("28 days %v", err)
	}
}

func TestOverviewBackgroundPassReplaysLegacyAndSurvivesRestart(t *testing.T) {
	h := newUsageHarness(t)
	now := time.Now()
	stamp := now.AddDate(0, 0, -1).Format(time.RFC3339)
	line := fmt.Sprintf(`{"type":"assistant","timestamp":%q,"message":{"id":"one","usage":{"input_tokens":100,"output_tokens":10},"content":[]}}`, stamp)
	path := h.session("daily")
	h.write(path, line, line)
	if err := h.store.SaveUsageRow(context.Background(), store.UsageRow{Assistant: "claude", Conversation: "daily", Path: path, ReadAt: now, State: json.RawMessage(`{"offset":9999}`)}); err != nil {
		t.Fatal(err)
	}
	h.pass()
	got, err := h.u.Overview(context.Background(), 7, "")
	if err != nil || got.Totals.Tokens == nil || *got.Totals.Tokens != 110 {
		t.Fatalf("first %+v %v", got.Totals, err)
	}
	h.open()
	h.pass()
	next, err := h.u.Overview(context.Background(), 7, "")
	if err != nil || next.Totals.Tokens == nil || *next.Totals.Tokens != 110 {
		t.Fatalf("restart %+v %v", next.Totals, err)
	}
	// GET remains usable with sources gone and never attempts another read.
	h.u.Home = "/not-a-home"
	next, err = h.u.Overview(context.Background(), 7, "")
	if err != nil || *next.Totals.Tokens != 110 {
		t.Fatal("GET depends on source")
	}
}

func TestOverviewAttributesUnownedLinkedWorktreesToTheirRepository(t *testing.T) {
	h := newUsageHarness(t)
	repo := t.TempDir()
	wt := t.TempDir()
	gitdir := filepath.Join(repo, ".git", "worktrees", "w")
	if err := os.MkdirAll(gitdir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+gitdir+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitdir, "commondir"), []byte("../..\n"), 0600); err != nil {
		t.Fatal(err)
	}
	line := fmt.Sprintf(`{"type":"assistant","cwd":%q,"timestamp":%q,"message":{"id":"one","usage":{"input_tokens":100,"output_tokens":10}}}`, wt, time.Now().AddDate(0, 0, -1).Format(time.RFC3339))
	h.write(h.session("unowned"), line)
	h.pass()
	canonical, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	got, err := h.u.Overview(context.Background(), 7, overviewProject(canonical).ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Totals.Tokens == nil || *got.Totals.Tokens != 110 || len(got.Projects) != 1 {
		t.Fatalf("worktree attribution %+v", got)
	}
}

// Marshal the broker's actual record type so changes to its stored wire cannot
// silently turn completion durations into missing samples.
func TestOverviewIncludesWorkCreatedBeforeTheWindowAndCompletedInsideIt(t *testing.T) {
	h := newUsageHarness(t)
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.Local)
	h.u.Now = func() time.Time { return now }
	finish := now.AddDate(0, 0, -1)
	record := orchestrator.Record{SpawnedAt: finish.Add(-time.Hour), FinishedAt: finish, Result: &taskdir.Result{Status: "success"}}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	err = h.store.SaveBrokerTask(context.Background(), store.BrokerRow{ID: "old-created", Project: "example", Assistant: "claude", State: "success", CreatedAt: now.AddDate(0, 0, -30), UpdatedAt: now, Record: raw}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := h.u.Overview(context.Background(), 7, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Totals.Dispatches != 0 || got.Totals.DurationSamples != 1 || got.Daily[6].DurationSamples != 1 || got.Totals.DurationMedianSeconds == nil || *got.Totals.DurationMedianSeconds != 3600 {
		t.Fatalf("completion cohort: %+v", got)
	}
}
