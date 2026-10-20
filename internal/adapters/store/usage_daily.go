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
 project_key TEXT NOT NULL DEFAULT '',
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
		Version    int                          `json:"daily_version"`
		Daily      map[string]transcript.Tokens `json:"daily"`
		Project    string                       `json:"daily_project"`
		ProjectKey string                       `json:"daily_project_key"`
		Cutoff     string                       `json:"daily_cutoff"`
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
		if _, err := tx.ExecContext(ctx, `INSERT INTO usage_daily(assistant,conversation,date,project,input,cache_read,cache_write,output,project_key) VALUES(?,?,?,?,?,?,?,?,?)`, r.Assistant, r.Conversation, date, state.Project, t.Input, t.CacheRead, t.CacheWrite1h+t.CacheWrite5m, t.Output, state.ProjectKey); err != nil {
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
	rows, err := s.rd.QueryContext(ctx, usageProjectSQL+`,
 cwd_prefixes(path,prefix,rest) AS (
 SELECT DISTINCT project,'',replace(project,char(92),'/')||'/' FROM usage_daily WHERE project<>''
 UNION ALL
 SELECT path,prefix||substr(rest,1,instr(rest,'/')),substr(rest,instr(rest,'/')+1)
 FROM cwd_prefixes WHERE instr(rest,'/')>0
 ),
 worktree_paths AS (
 SELECT DISTINCT worktree,replace(worktree,char(92),'/') normalized FROM task_sources WHERE worktree<>''
 ),
 cwd_projects AS (
 SELECT DISTINCT c.path,COALESCE(p.project,'') project FROM cwd_prefixes c
 JOIN worktree_paths w ON w.normalized=rtrim(c.prefix,'/')
 LEFT JOIN resolved_paths p ON p.path=w.worktree
 ),
 lineage(assistant,conversation,ancestor) AS (
 SELECT DISTINCT assistant,conversation,conversation FROM usage_daily WHERE date>=? AND date<?
 UNION SELECT l.assistant,l.conversation,u.parent FROM lineage l
 JOIN usage_transcripts u ON u.assistant=l.assistant AND u.conversation=l.ancestor WHERE u.parent<>''
 ),
 candidates(assistant,conversation,priority,project) AS (
 SELECT l.assistant,l.conversation,0,COALESCE(t.project,'') FROM lineage l
 JOIN usage_transcripts u ON u.assistant=l.assistant AND u.conversation=l.ancestor
 LEFT JOIN task_projects t ON t.id=u.task_id WHERE u.task_id<>''
 UNION ALL
 SELECT l.assistant,l.conversation,1,c.project FROM lineage l
 JOIN conversation_projects c ON c.assistant=l.assistant AND c.conversation=l.ancestor
 UNION ALL
 SELECT l.assistant,l.conversation,2,p.project FROM lineage l
 JOIN usage_daily d ON d.assistant=l.assistant AND d.conversation=l.ancestor
 JOIN cwd_projects p ON p.path=d.project
 UNION ALL
 SELECT l.assistant,l.conversation,3,COALESCE(p.project,'') FROM lineage l
 JOIN usage_daily d ON d.assistant=l.assistant AND d.conversation=l.ancestor
 LEFT JOIN resolved_paths p ON p.path=d.project
 WHERE EXISTS(SELECT 1 FROM path_seeds s WHERE s.path=d.project)
 ),
 priorities AS (SELECT assistant,conversation,MIN(priority) priority FROM candidates GROUP BY assistant,conversation),
 owners AS (
 SELECT c.assistant,c.conversation,CASE WHEN COUNT(DISTINCT c.project)=1 THEN MIN(c.project) ELSE '' END project
 FROM candidates c JOIN priorities p USING(assistant,conversation,priority) GROUP BY c.assistant,c.conversation
 )
 SELECT d.date,d.assistant,d.conversation,u.parent,u.task_id,
 COALESCE(o.project,''),d.input,d.cache_read,d.cache_write,d.output
 FROM usage_daily d JOIN usage_transcripts u USING(assistant,conversation)
 LEFT JOIN owners o ON o.assistant=d.assistant AND o.conversation=d.conversation
 WHERE d.date>=? AND d.date<?
 ORDER BY d.date,d.assistant,d.conversation`, since, until, since, until)
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
	rows, err := s.rd.QueryContext(ctx, usageProjectSQL+` SELECT p.project,t.created_at,json_object('spawned_at',json_extract(t.record,'$.spawned_at'),'finished_at',json_extract(t.record,'$.finished_at'),'result',json_object('status',json_extract(t.record,'$.result.status'))) FROM broker_tasks t JOIN task_projects p ON p.id=t.id WHERE COALESCE(json_extract(t.record,'$.kind'),'')<>'callback' AND ((t.created_at>=? AND t.created_at<?) OR (unixepoch(json_extract(t.record,'$.finished_at'))>=? AND unixepoch(json_extract(t.record,'$.finished_at'))<?)) ORDER BY t.created_at DESC,t.id LIMIT ?`, since.Unix(), until.Unix(), since.Unix(), until.Unix(), UsageOverviewTaskLimit)
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
