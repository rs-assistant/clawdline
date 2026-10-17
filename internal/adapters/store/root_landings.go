package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
)

// A root landing is the broker's record of a landing no child task carries:
// the owning Session proved one commit on a target branch and on its
// remote-tracking copy (`item phase deploying --commit --target --remote`),
// and this row is that proof. Before it, the proof lived only as a copy inside
// the item's `item.phase_changed` payload, a second truth beside the broker's
// task landings (docs/design-decisions.md D01). The item's event now carries
// only this row's id.
//
// One row per (item, repository, commit, target): the id is derived from those
// four, so the same landing recorded twice — a retry under a new
// Idempotency-Key after a version reread, or Finish after `item phase` — is the
// same row, and the first proof stands.
//
// The events an older daemon wrote keep their `landing` copy, and
// migrateLegacyLandings gives each copy its row. An older binary opening the
// same file reads both, so after a rollback a legacy landing is listed twice;
// only the listing is affected.
const rootLandingSchema = `
CREATE TABLE IF NOT EXISTS broker_root_landings (
  id            TEXT    PRIMARY KEY,
  work_id       TEXT    NOT NULL,
  session_id    TEXT    NOT NULL,
  repository    TEXT    NOT NULL,
  target        TEXT    NOT NULL,
  commit_sha    TEXT    NOT NULL,
  target_commit TEXT    NOT NULL,
  remote        TEXT    NOT NULL,
  remote_commit TEXT    NOT NULL,
  recorded_at   INTEGER NOT NULL,
  UNIQUE (work_id, repository, commit_sha, target)
);
CREATE INDEX IF NOT EXISTS broker_root_landings_work ON broker_root_landings(work_id, recorded_at, id);
`

// WorkV2RootLandingLimit is how many root landings one item keeps. An item re-enters deploying
// once a cycle, so this is many reopenings; at the limit a new landing is
// refused and nothing is evicted.
const WorkV2RootLandingLimit = 64

// ErrRootLandingsFull is an item that already holds WorkV2RootLandingLimit
// root landings.
var ErrRootLandingsFull = errors.New("root landings full")

// RootLanding is one broker_root_landings row.
type RootLanding struct {
	ID           string
	WorkID       string
	SessionID    string
	Repository   string
	Target       string
	Commit       string
	TargetCommit string
	Remote       string
	RemoteCommit string
	RecordedAt   time.Time
}

// RootLandingID is the id a landing of commit on target in repository, for
// item workID, is stored under: the same four always answer the same id.
func RootLandingID(workID, repository, commit, target string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{workID, repository, commit, target}, "\x00")))
	h := hex.EncodeToString(sum[:16])
	return "rl-" + h
}

func openRootLandings(db *sql.DB) error {
	_, err := db.Exec(rootLandingSchema)
	return err
}

// RecordRootLanding writes l in the item's transaction and answers the row as
// stored: the existing one when this landing was recorded before, whatever
// later target or remote tips l carries.
func (t *WorkV2Tx) RecordRootLanding(l RootLanding) (RootLanding, error) {
	l.ID = RootLandingID(l.WorkID, l.Repository, l.Commit, l.Target)
	existing, err := scanRootLanding(t.tx.QueryRowContext(t.ctx,
		`SELECT `+rootLandingColumns+` FROM broker_root_landings WHERE id = ?`, l.ID))
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return RootLanding{}, err
	}
	var n int
	if err := t.tx.QueryRowContext(t.ctx, `SELECT COUNT(*) FROM broker_root_landings WHERE work_id = ?`,
		l.WorkID).Scan(&n); err != nil {
		return RootLanding{}, err
	}
	if n >= WorkV2RootLandingLimit {
		return RootLanding{}, ErrRootLandingsFull
	}
	if _, err := t.tx.ExecContext(t.ctx, `INSERT INTO broker_root_landings
	  (id, work_id, session_id, repository, target, commit_sha, target_commit, remote, remote_commit, recorded_at)
	  VALUES (?,?,?,?,?,?,?,?,?,?)`, l.ID, l.WorkID, l.SessionID, l.Repository, l.Target, l.Commit,
		l.TargetCommit, l.Remote, l.RemoteCommit, l.RecordedAt.Unix()); err != nil {
		return RootLanding{}, err
	}
	t.wrote++
	l.RecordedAt = time.Unix(l.RecordedAt.Unix(), 0)
	return l, nil
}

const rootLandingColumns = `id, work_id, session_id, repository, target, commit_sha, target_commit, remote,
  remote_commit, recorded_at`

func scanRootLanding(sc scanner) (RootLanding, error) {
	var l RootLanding
	var at int64
	if err := sc.Scan(&l.ID, &l.WorkID, &l.SessionID, &l.Repository, &l.Target, &l.Commit, &l.TargetCommit,
		&l.Remote, &l.RemoteCommit, &at); err != nil {
		return RootLanding{}, err
	}
	l.RecordedAt = time.Unix(at, 0)
	return l, nil
}

func queryRootLandings(ctx context.Context, q querier, workID string) ([]RootLanding, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+rootLandingColumns+` FROM broker_root_landings
	  WHERE work_id = ? ORDER BY recorded_at, id LIMIT ?`, workID, WorkV2RootLandingLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RootLanding{}
	for rows.Next() {
		l, err := scanRootLanding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// RootLandings is every root landing recorded for one item, oldest first.
func (s *Store) RootLandings(ctx context.Context, workID string) ([]RootLanding, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	return queryRootLandings(ctx, s.rd, workID)
}

// RootLandings is RootLandings inside the item's transaction.
func (t *WorkV2Tx) RootLandings(workID string) ([]RootLanding, error) {
	return queryRootLandings(t.ctx, t.tx, workID)
}

// migrateLegacyLandings gives every landing copy an older daemon wrote into an
// `item.phase_changed` payload — before root landings were rows of their own —
// the broker_root_landings row it would have been: the same item, repository,
// commit and target, so the same id RecordRootLanding derives. The events
// themselves are history and are not rewritten.
//
// It runs at every open and is idempotent: a copy whose four keys already
// name a row is not selected, so a second open inserts nothing, and a copy
// recorded both ways keeps the row that was there first. A copy that is not a
// whole proof was never shown as a landing and is not made one. An item
// already at WorkV2RootLandingLimit keeps what it has; the copies left over
// are counted in the log line, not evicted for.
func migrateLegacyLandings(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.Query(`SELECT e.work_id, e.actor, e.at,
	    COALESCE(NULLIF(json_extract(e.payload, '$.landing.repository'), ''), i.project_path),
	    json_extract(e.payload, '$.landing.target'), json_extract(e.payload, '$.landing.commit'),
	    json_extract(e.payload, '$.landing.target_commit'), json_extract(e.payload, '$.landing.remote'),
	    json_extract(e.payload, '$.landing.remote_commit')
	  FROM work_v2_events e JOIN work_v2_items i ON i.id = e.work_id
	  WHERE e.kind = 'item.phase_changed' AND json_valid(e.payload)
	    AND json_type(e.payload, '$.landing') = 'object' AND json_type(e.payload, '$.landing_id') IS NULL
	    AND (json_type(e.payload, '$.landing.repository') IS NULL OR json_type(e.payload, '$.landing.repository') IN ('text', 'null'))
	    AND json_type(e.payload, '$.landing.commit') = 'text' AND json_extract(e.payload, '$.landing.commit') != ''
	    AND json_type(e.payload, '$.landing.target') = 'text' AND json_extract(e.payload, '$.landing.target') != ''
	    AND json_type(e.payload, '$.landing.target_commit') = 'text' AND json_extract(e.payload, '$.landing.target_commit') != ''
	    AND json_type(e.payload, '$.landing.remote') = 'text' AND json_extract(e.payload, '$.landing.remote') != ''
	    AND json_type(e.payload, '$.landing.remote_commit') = 'text' AND json_extract(e.payload, '$.landing.remote_commit') != ''
	    AND NOT EXISTS (SELECT 1 FROM broker_root_landings r WHERE r.work_id = e.work_id
	      AND r.repository = COALESCE(NULLIF(json_extract(e.payload, '$.landing.repository'), ''), i.project_path)
	      AND r.commit_sha = json_extract(e.payload, '$.landing.commit')
	      AND r.target = json_extract(e.payload, '$.landing.target'))
	  ORDER BY e.seq`)
	if err != nil {
		return fmt.Errorf("legacy landings: %w", err)
	}
	var copies []RootLanding
	for rows.Next() {
		var l RootLanding
		var at int64
		if err := rows.Scan(&l.WorkID, &l.SessionID, &at, &l.Repository, &l.Target, &l.Commit,
			&l.TargetCommit, &l.Remote, &l.RemoteCommit); err != nil {
			rows.Close()
			return fmt.Errorf("legacy landings: %w", err)
		}
		l.RecordedAt = time.Unix(at, 0)
		copies = append(copies, l)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("legacy landings: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("legacy landings: %w", err)
	}
	if len(copies) == 0 {
		return nil
	}
	held := map[string]int{}
	moved, full := 0, 0
	for _, l := range copies {
		l.ID = RootLandingID(l.WorkID, l.Repository, l.Commit, l.Target)
		n, ok := held[l.WorkID]
		if !ok {
			if err := tx.QueryRow(`SELECT COUNT(*) FROM broker_root_landings WHERE work_id = ?`, l.WorkID).Scan(&n); err != nil {
				return fmt.Errorf("legacy landings: %w", err)
			}
		}
		if n >= WorkV2RootLandingLimit {
			held[l.WorkID] = n
			full++
			continue
		}
		// OR IGNORE: two copies of one landing in one item's history are one row.
		res, err := tx.Exec(`INSERT OR IGNORE INTO broker_root_landings
		  (id, work_id, session_id, repository, target, commit_sha, target_commit, remote, remote_commit, recorded_at)
		  VALUES (?,?,?,?,?,?,?,?,?,?)`, l.ID, l.WorkID, l.SessionID, l.Repository, l.Target, l.Commit,
			l.TargetCommit, l.Remote, l.RemoteCommit, l.RecordedAt.Unix())
		if err != nil {
			return fmt.Errorf("legacy landings: %w", err)
		}
		if added, _ := res.RowsAffected(); added > 0 {
			n++
			moved++
		}
		held[l.WorkID] = n
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("legacy landings: %w", err)
	}
	// Every open rescans; only an open that moved something says so, so an
	// item already full does not repeat its line at every start.
	if moved > 0 {
		log.Printf("store: %d legacy landing copies became root landings; %d left out because their item already holds %d",
			moved, full, WorkV2RootLandingLimit)
	}
	return nil
}

// WorkV2Tasks is WorkV2Tx.Tasks outside a write: the broker tasks bound to
// one item, for a read.
func (s *Store) WorkV2Tasks(ctx context.Context, workID string) ([]BrokerRow, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	return queryWorkV2Tasks(ctx, s.rd, workID)
}
