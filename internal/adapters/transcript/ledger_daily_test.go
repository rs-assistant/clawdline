package transcript

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func dailyClaude(id, at string, output int) string {
	return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"message":{"id":%q,"model":"test","usage":{"input_tokens":10,"cache_read_input_tokens":20,"cache_creation_input_tokens":5,"output_tokens":%d},"content":[]}}`, at, id, output)
}

func TestDailyConservesRepeatedRowsAcrossRestartAndDayBoundary(t *testing.T) {
	old := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = old })
	var s LedgerState
	s.PrepareDaily(time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC))
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	first := dailyClaude("a", "2026-10-17T23:59:59Z", 2)
	second := dailyClaude("a", "2026-10-18T00:00:01Z", 7)
	if err := os.WriteFile(path, []byte(first+"\n"+first+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Feed(path); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(s)
	var next LedgerState
	if err := json.Unmarshal(raw, &next); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.WriteString(second + "\n" + second + "\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, err = next.Feed(path); err != nil {
		t.Fatal(err)
	}
	if next.Daily["2026-10-17"].Total() != 37 || next.Daily["2026-10-18"].Total() != 5 {
		t.Fatalf("daily %+v", next.Daily)
	}
	_, total := next.Totals()
	sum := 0.0
	for _, v := range next.Daily {
		sum += v.Total()
	}
	if sum != total.Total() {
		t.Fatalf("sum %v ledger %v", sum, total.Total())
	}
	if _, err = next.Feed(path); err != nil {
		t.Fatal(err)
	}
	if next.Daily["2026-10-18"].Total() != 5 {
		t.Fatal("idle feed counted twice")
	}
	// Replacement replays a snapshot instead of adding a second copy.
	if err = os.WriteFile(path, []byte(dailyClaude("replacement", "2026-10-18T12:00:00Z", 1)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	res, err := next.Feed(path)
	if err != nil || !res.Restarted {
		t.Fatalf("restart %+v %v", res, err)
	}
	if len(next.Daily) != 1 || next.Daily["2026-10-18"].Total() != 36 {
		t.Fatalf("replacement %+v", next.Daily)
	}
}

func TestDailyCodexDedupUnknownTimestampAndInlineSubagents(t *testing.T) {
	old := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = old })
	var s LedgerState
	s.PrepareDaily(time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC))
	line := `{"timestamp":"2026-10-18T12:00:00Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":100,"cached_input_tokens":20,"output_tokens":10},"total_token_usage":{"total_tokens":110}}}}`
	s.line([]byte(line))
	s.line([]byte(line))
	v := s.Daily["2026-10-18"]
	if v.Input != 80 || v.CacheRead != 20 || v.Output != 10 {
		t.Fatalf("codex %+v", v)
	}
	var claude LedgerState
	claude.PrepareDaily(time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC))
	claude.line([]byte(dailyClaude("unknown", "", 2)))
	if !claude.DailyUnknown || len(claude.Daily) != 0 {
		t.Fatalf("unknown %+v", claude.Daily)
	}
	side := `{"type":"assistant","isSidechain":true,"timestamp":"2026-10-18T12:00:00Z","message":{"id":"side","usage":{"input_tokens":40,"output_tokens":3}}}`
	claude.line([]byte(side))
	if claude.Daily["2026-10-18"].Total() != 43 {
		t.Fatal("inline fallback lost its measured primitives")
	}
}

func TestDailyPartialFeedAndTimezoneReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	first := dailyClaude("a", "2026-10-18T00:00:00Z", 2) + "\n"
	second := dailyClaude("b", "2026-10-18T12:00:00Z", 3) + "\n"
	if err := os.WriteFile(path, []byte(first+second), 0600); err != nil {
		t.Fatal(err)
	}
	var s LedgerState
	s.PrepareDaily(time.Date(2026, 10, 19, 0, 0, 0, 0, time.Local))
	r, err := s.feed(path, int64(len(first)), ledgerLineLimit)
	if err != nil || !r.More {
		t.Fatalf("partial %+v %v", r, err)
	}
	raw, _ := json.Marshal(s)
	var resumed LedgerState
	json.Unmarshal(raw, &resumed)
	if _, err = resumed.Feed(path); err != nil {
		t.Fatal(err)
	}
	sum := 0.0
	for _, v := range resumed.Daily {
		sum += v.Total()
	}
	if sum != 75 {
		t.Fatalf("partial conservation %v", sum)
	}
	resumed.DailyTimezone = "a different timezone"
	resumed.PrepareDaily(time.Now())
	if resumed.Offset != 0 || len(resumed.Daily) != 0 {
		t.Fatal("zone change reused old dates")
	}
}

func TestDailyRetentionAndLegacyReplay(t *testing.T) {
	var s LedgerState
	s.Offset = 123
	s.Measured.Input = 99
	now := time.Date(2026, 10, 19, 0, 0, 0, 0, time.Local)
	s.PrepareDaily(now)
	if s.Offset != 0 || s.Measured.Total() != 0 {
		t.Fatal("legacy cursor kept cumulative-only state")
	}
	for n := 0; n < 100; n++ {
		at := now.AddDate(0, 0, -n).Format(time.RFC3339)
		s.line([]byte(dailyClaude(fmt.Sprint(n), at, 1)))
	}
	if len(s.Daily) != DailyDayLimit {
		t.Fatalf("retained %d", len(s.Daily))
	}
}

func TestDailyBackfillKeepsTheCumulativeBillWhileItsCursorCatchesUp(t *testing.T) {
	old := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = old })
	first := dailyClaude("a", "2026-10-18T00:00:00Z", 2) + "\n"
	second := dailyClaude("b", "2026-10-18T12:00:00Z", 3) + "\n"
	path := filepath.Join(t.TempDir(), "legacy.jsonl")
	if err := os.WriteFile(path, []byte(first+second), 0600); err != nil {
		t.Fatal(err)
	}
	var s LedgerState
	if _, err := s.Feed(path); err != nil {
		t.Fatal(err)
	}
	_, before := s.Totals()
	offset := s.Offset
	s.DailyVersion = 0
	s.Daily = nil
	s.DailyCutoff = ""
	now := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	res, err := s.feedDaily(path, now, int64(len(first)), ledgerLineLimit)
	if err != nil {
		t.Fatalf("backfill %+v %v", res, err)
	}
	_, during := s.Totals()
	if during != before || s.Offset != offset || s.Daily["2026-10-18"].Total() != 37 {
		t.Fatalf("bill rewound: before %+v during %+v daily %+v", before, during, s.Daily)
	}
	if !res.More || s.DailyReplay == nil {
		t.Fatal("unfinished daily backfill was not retained")
	}
	raw, _ := json.Marshal(s)
	var restored LedgerState
	json.Unmarshal(raw, &restored)
	res, err = restored.feedDaily(path, now, int64(len(first)), ledgerLineLimit)
	if err != nil || res.More || restored.DailyReplay != nil {
		t.Fatalf("resumed %+v %v", res, err)
	}
	_, after := restored.Totals()
	if after != before || restored.Daily["2026-10-18"].Total() != 75 {
		t.Fatalf("after %+v %+v", after, restored.Daily)
	}
}

func TestDailyBackfillRecognizesAnAppendedShortFingerprint(t *testing.T) {
	now := time.Date(2026, 10, 19, 0, 0, 0, 0, time.Local)
	first := dailyClaude("a", now.AddDate(0, 0, -1).Format(time.RFC3339), 2) + "\n"
	path := filepath.Join(t.TempDir(), "append.jsonl")
	if err := os.WriteFile(path, []byte(first), 0600); err != nil {
		t.Fatal(err)
	}
	var s LedgerState
	if _, err := s.Feed(path); err != nil {
		t.Fatal(err)
	}
	s.DailyVersion = 0
	s.Daily = nil
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(dailyClaude("b", now.AddDate(0, 0, -1).Format(time.RFC3339), 3) + "\n")
	f.Close()
	res, err := s.FeedDaily(path, now)
	if err != nil || res.More || s.DailyReplay != nil {
		t.Fatalf("fingerprint convergence %+v %v", res, err)
	}
	_, total := s.Totals()
	if total.Total() != 75 {
		t.Fatalf("bill %+v", total)
	}
}

func TestDailyFutureTimestampsCannotGrowTheRetentionWindow(t *testing.T) {
	now := time.Date(2026, 10, 19, 0, 0, 0, 0, time.Local)
	var s LedgerState
	s.PrepareDaily(now)
	for n := 1; n < 100; n++ {
		s.line([]byte(dailyClaude(fmt.Sprint(n), now.AddDate(0, 0, n).Format(time.RFC3339), 2)))
	}
	if len(s.Daily) != 0 || !s.DailyUnknown {
		t.Fatalf("future dates %+v", s.Daily)
	}
}

func TestDailyBackfillFinishesWhenALegacySourceWasEmptied(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.jsonl")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	s := LedgerState{Assistant: "claude", Offset: 100, Measured: Tokens{Input: 100}}
	res, err := s.FeedDaily(path, time.Now())
	if err != nil || res.More || s.DailyReplay != nil {
		t.Fatalf("empty replay %+v %v", res, err)
	}
	if s.Measured.Total() != 0 || len(s.Daily) != 0 {
		t.Fatal("replaced source kept old snapshot")
	}
}

func TestDailyTimezoneChangeWithTheSameNameReplaysWithoutChangingTheBill(t *testing.T) {
	old := time.Local
	t.Cleanup(func() { time.Local = old })
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "same-zone.jsonl")
	line := `{"type":"assistant","timestamp":"2026-10-18T23:30:00Z","message":{"id":"one","usage":{"input_tokens":10,"output_tokens":2},"content":[]}}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0600); err != nil {
		t.Fatal(err)
	}
	time.Local = time.FixedZone("Local", 8*3600)
	s := LedgerState{Assistant: "claude"}
	if _, err := s.FeedDaily(path, now); err != nil {
		t.Fatal(err)
	}
	bill := s.dailyMeasured().Total()
	if s.Daily["2026-10-19"].Total() != 12 {
		t.Fatal(s.Daily)
	}
	time.Local = time.FixedZone("Local", -7*3600)
	if _, err := s.FeedDaily(path, now); err != nil {
		t.Fatal(err)
	}
	if s.Daily["2026-10-18"].Total() != 12 || s.Daily["2026-10-19"].Total() != 0 || s.dailyMeasured().Total() != bill {
		t.Fatalf("regroup: %+v bill %v", s.Daily, s.dailyMeasured())
	}
}
