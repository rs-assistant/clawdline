package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"time"
)

const usageDailySchema = `
CREATE TABLE IF NOT EXISTS usage_daily_evictions (date TEXT PRIMARY KEY, evicted INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS usage_daily (
 assistant TEXT NOT NULL, conversation TEXT NOT NULL, date TEXT NOT NULL, project TEXT NOT NULL,
 input REAL NOT NULL, cache_read REAL NOT NULL, cache_write REAL NOT NULL, output REAL NOT NULL,
 PRIMARY KEY(assistant,conversation,date));
CREATE INDEX IF NOT EXISTS usage_daily_date ON usage_daily(date);
CREATE TABLE IF NOT EXISTS usage_daily_meta (id INTEGER PRIMARY KEY CHECK(id=1), evicted INTEGER NOT NULL DEFAULT 0);
`

// UsageDailyRowLimit bounds summaries across all transcripts, in addition to
// the parser's per-source retention. Eviction is visible as partial coverage.
const UsageDailyRowLimit = 50000
const UsageOverviewTaskLimit = 10000

// PruneUsageDaily runs even on idle passes, so old summaries expire when no
// transcript changes. Cumulative ledger rows are deliberately unaffected.
func (s *Store) PruneUsageDaily(ctx context.Context, now time.Time) error {
	cutoff := now.In(time.Local).AddDate(0, 0, -(transcript.DailyDayLimit - 1)).Format("2006-01-02")
	return s.write(ctx, func(tx *sql.Tx) (int64, error) {
		if _, err := tx.ExecContext(ctx, `DELETE FROM usage_daily_evictions WHERE date<?`, cutoff); err != nil {
			return 0, err
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM usage_daily WHERE date<?`, cutoff)
		if err != nil {
			return 0, err
		}
		return res.RowsAffected()
	})
}

func saveUsageDaily(ctx context.Context, tx *sql.Tx, r UsageRow) error {
	var state struct {
		Version int                          `json:"daily_version"`
		Daily   map[string]transcript.Tokens `json:"daily"`
		Project string                       `json:"daily_project"`
		Cutoff  string                       `json:"daily_cutoff"`
	}
	if err := json.Unmarshal(r.State, &state); err != nil {
		return nil
	} // old opaque state remains valid
	if state.Version != 1 {
		return nil
	}
	// Snapshot replacement and the cursor commit together. A crash can expose
	// neither half; rescans/restarts replace counts rather than adding them.
	if _, err := tx.ExecContext(ctx, `DELETE FROM usage_daily WHERE assistant=? AND conversation=?`, r.Assistant, r.Conversation); err != nil {
		return err
	}
	for date, t := range state.Daily {
		if _, err := time.Parse("2006-01-02", date); err != nil {
			return err
		}
		if date < state.Cutoff {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO usage_daily VALUES(?,?,?,?,?,?,?,?)`, r.Assistant, r.Conversation, date, state.Project, t.Input, t.CacheRead, t.CacheWrite1h+t.CacheWrite5m, t.Output); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM usage_daily WHERE date<?`, state.Cutoff); err != nil {
		return err
	}
	// Keep date-scoped evidence of dropped rows; unrelated future windows
	// must not inherit a lifetime truncation flag.
	if _, err := tx.ExecContext(ctx, `DELETE FROM usage_daily_evictions WHERE date<?`, state.Cutoff); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO usage_daily_evictions(date,evicted)
 SELECT date,COUNT(*) FROM (SELECT date FROM usage_daily ORDER BY date DESC,assistant,conversation LIMIT -1 OFFSET ?) GROUP BY date
 ON CONFLICT(date) DO UPDATE SET evicted=evicted+excluded.evicted`, UsageDailyRowLimit); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM usage_daily WHERE rowid IN (SELECT rowid FROM usage_daily ORDER BY date DESC,assistant,conversation LIMIT -1 OFFSET ?)`, UsageDailyRowLimit)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n > 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO usage_daily_meta(id,evicted) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET evicted=evicted+excluded.evicted`, n)
	}
	return err
}

type UsageDailyRow struct {
	Date, Assistant, Conversation, Parent, TaskID, Project string
	Input, CacheRead, CacheWrite, Output                   float64
}

// UsageDaily reads only persisted primitive summaries, never transcript state.
func (s *Store) UsageDaily(ctx context.Context, since, until string) ([]UsageDailyRow, error) {
	rows, err := s.rd.QueryContext(ctx, `SELECT d.date,d.assistant,d.conversation,u.parent,u.task_id,
 COALESCE(NULLIF(t.repository,''),NULLIF(t.project,''),NULLIF(pd.project,''),d.project,''),d.input,d.cache_read,d.cache_write,d.output
 FROM usage_daily d JOIN usage_transcripts u USING(assistant,conversation)
 LEFT JOIN usage_transcripts p ON p.assistant=u.assistant AND p.conversation=u.parent
 LEFT JOIN (SELECT assistant,conversation,MAX(project) project FROM usage_daily WHERE project<>'' GROUP BY assistant,conversation) pd ON pd.assistant=u.assistant AND pd.conversation=u.parent
 LEFT JOIN broker_tasks t ON t.id=CASE WHEN u.parent<>'' THEN p.task_id ELSE u.task_id END
 WHERE d.date>=? AND d.date<?
 ORDER BY d.date,d.assistant,d.conversation`, since, until)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UsageDailyRow{}
	for rows.Next() {
		var r UsageDailyRow
		if err := rows.Scan(&r.Date, &r.Assistant, &r.Conversation, &r.Parent, &r.TaskID, &r.Project, &r.Input, &r.CacheRead, &r.CacheWrite, &r.Output); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// UsageDailyCoverage counts sources independently from token-bearing rows.
func (s *Store) UsageDailyCoverage(ctx context.Context) (read, total, evicted int64, err error) {
	err = s.rd.QueryRowContext(ctx, `SELECT COALESCE(SUM(CASE WHEN json_valid(state) AND json_extract(state,'$.daily_version')=1 AND read_at<>0 AND more=0 AND reason='' AND json_extract(state,'$.daily_replay') IS NULL AND COALESCE(json_extract(state,'$.daily_unknown'),0)=0 THEN 1 ELSE 0 END),0),COUNT(*) FROM usage_transcripts`).Scan(&read, &total)
	if err == nil {
		err = s.rd.QueryRowContext(ctx, `SELECT COALESCE((SELECT evicted FROM usage_daily_meta WHERE id=1),0)`).Scan(&evicted)
	}
	return
}

func (s *Store) UsageDailyCount(ctx context.Context) (n int64, err error) {
	err = s.rd.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_daily`).Scan(&n)
	return
}

type UsageOverviewTask struct {
	Project    string
	CreatedAt  time.Time
	SpawnedAt  time.Time `json:"spawned_at"`
	FinishedAt time.Time `json:"finished_at"`
	Result     *struct {
		Status string `json:"status"`
	} `json:"result"`
}

// UsageOverviewTasks projects only timing and outcome fields: no task text.
func (s *Store) UsageOverviewTasks(ctx context.Context, since, until time.Time) ([]UsageOverviewTask, error) {
	rows, err := s.rd.QueryContext(ctx, `SELECT COALESCE(NULLIF(repository,''),project),created_at,json_object('spawned_at',json_extract(record,'$.spawned_at'),'finished_at',json_extract(record,'$.finished_at'),'result',json_object('status',json_extract(record,'$.result.status'))) FROM broker_tasks WHERE (created_at>=? AND created_at<?) OR (unixepoch(json_extract(record,'$.finished_at'))>=? AND unixepoch(json_extract(record,'$.finished_at'))<?) ORDER BY created_at DESC,id LIMIT ?`, since.Unix(), until.Unix(), since.Unix(), until.Unix(), UsageOverviewTaskLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UsageOverviewTask{}
	for rows.Next() {
		var t UsageOverviewTask
		var created int64
		var raw string
		if err := rows.Scan(&t.Project, &created, &raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &t); err != nil {
			return nil, err
		}
		t.CreatedAt = time.Unix(created, 0)
		out = append(out, t)
	}
	return out, rows.Err()
}

// UsageDailyEvictedInRange excludes loss outside the requested period.
func (s *Store) UsageDailyEvictedInRange(ctx context.Context, since, until string) (n int64, err error) {
	err = s.rd.QueryRowContext(ctx, `SELECT COALESCE(SUM(evicted),0) FROM usage_daily_evictions WHERE date>=? AND date<?`, since, until).Scan(&n)
	return
}
