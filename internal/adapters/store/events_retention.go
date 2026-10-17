package store

import (
	"context"
	"database/sql"
	"log"
	"strings"
	"sync"
	"time"
)

// The events table is the operational journal: every fact the daemon records
// with Append, AppendWithEffects or a broker transition. Until 2026-10 it only
// grew — 114,403 rows of 108 kinds on the running daemon, about 5,850 more a
// day, none ever deleted — while the only program that reads it back is
// LatestEvents, and only for the newest row of a few kinds per subject.
//
// So the store keeps, forever, the newest row per (kind, subject) of each kind
// in eventKindsReadLatest, and lets everything else go once it is older than
// EventAgeLimit. EventKeepLimit is the backstop: past it the oldest rows that
// are not kept go first, whatever their age.
const (
	// EventAgeLimit is how long a row nobody reads back is kept. The readers
	// need far less: run.issued answers a relay for RelayWindow (one day,
	// internal/domain/work/runs.go) and is refused as expired after it; every
	// other read is of a kept newest row. Two weeks leaves the journal
	// readable by hand after an incident someone noticed late.
	EventAgeLimit = 14 * 24 * time.Hour
	// EventKeepLimit is how many rows the table holds before the prune lets
	// go of the oldest rows that are not kept, whatever their age. Two weeks
	// at the measured rate is about 82,000 rows.
	EventKeepLimit = 150_000
	// EventPruneBatchLimit is how many rows one prune transaction examines,
	// so a first prune of a hundred thousand rows holds the store's one
	// write connection for a moment at a time, never for the whole prune.
	EventPruneBatchLimit = 2_000
)

// eventKindsReadLatest are the kinds a program reads back, always as the
// newest row about a subject (LatestEvents). Their newest row per subject is
// never pruned: each answers a question about a session that still exists —
// the run of its last message, its delivered turn, whether its waiting push
// was already decided — however long ago it was written.
//
//   - run.latest: app.Runs.Latest and the session list (sessions.go).
//   - session.delivered: the session list and its own-record overlay
//     (transport/http ownrecords.go).
//   - session.waiting: app.Waiting.settle, the dedupe of the waiting push.
//
// run.issued is read too (app.Runs.Find), by run id, one row per run; a run is
// only accepted for RelayWindow, so it is not kept past EventAgeLimit.
var eventKindsReadLatest = map[string]bool{
	"run.latest":        true,
	"session.delivered": true,
	"session.waiting":   true,
}

// EventKindKeptLatest reports whether the newest row per subject of kind
// survives every prune. A reader of LatestEvents relies on it.
func EventKindKeptLatest(kind string) bool { return eventKindsReadLatest[kind] }

// EventPrune is what one prune did.
type EventPrune struct {
	// Removed is how many rows were deleted.
	Removed int64
	// Batches is how many write transactions it took.
	Batches int
	// Took is how long the whole prune lasted.
	Took time.Duration
}

// eventPruneStats is this handle's account of its prunes, for the capacity
// register's reading.
type eventPruneStats struct {
	mu      sync.Mutex
	removed int64
	last    time.Time
}

// EventPruneStats is how many rows this handle's prunes have removed, and
// when the newest prune finished.
func (s *Store) EventPruneStats() (removed int64, last time.Time) {
	s.prunes.mu.Lock()
	defer s.prunes.mu.Unlock()
	return s.prunes.removed, s.prunes.last
}

// PruneEvents deletes the rows the retention rule no longer keeps, as of now,
// in transactions of at most EventPruneBatchLimit examined rows. It walks the
// table from its oldest row and stops at the first row that is both young
// enough and not needed to bring the table under EventKeepLimit. A failure
// leaves every batch before it deleted and answers what was done with it.
func (s *Store) PruneEvents(ctx context.Context, now time.Time) (EventPrune, error) {
	return s.pruneEvents(ctx, now, EventAgeLimit, EventKeepLimit, EventPruneBatchLimit)
}

func (s *Store) pruneEvents(ctx context.Context, now time.Time, age time.Duration, keep, batch int) (EventPrune, error) {
	began := time.Now()
	var out EventPrune
	if err := reading(); err != nil {
		return out, err
	}
	var total int64
	if err := s.rd.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&total); err != nil {
		return out, classify(err)
	}
	excess := total - int64(keep)
	cutoff := now.Add(-age).Unix()
	var after int64
	for done := false; !done; {
		if err := ctx.Err(); err != nil {
			return s.notePrune(out, began), err
		}
		var removed int64
		err := s.write(ctx, func(tx *sql.Tx) (int64, error) {
			removed = 0
			rows, err := tx.QueryContext(ctx,
				`SELECT seq, at, kind, subject FROM events WHERE seq > ? ORDER BY seq LIMIT ?`, after, batch)
			if err != nil {
				return 0, err
			}
			type row struct {
				seq, at       int64
				kind, subject string
			}
			var seen []row
			for rows.Next() {
				var r row
				if err := rows.Scan(&r.seq, &r.at, &r.kind, &r.subject); err != nil {
					rows.Close()
					return 0, err
				}
				seen = append(seen, r)
			}
			if err := rows.Close(); err != nil {
				return 0, err
			}
			if len(seen) < batch {
				done = true
			}
			var doomed []any
			for _, r := range seen {
				if r.at >= cutoff && excess-int64(len(doomed)) <= 0 {
					done = true
					break
				}
				after = r.seq
				if eventKindsReadLatest[r.kind] {
					var newest int64
					if err := tx.QueryRowContext(ctx,
						`SELECT MAX(seq) FROM events WHERE kind = ? AND subject = ?`, r.kind, r.subject).
						Scan(&newest); err != nil {
						return 0, err
					}
					if newest == r.seq {
						continue
					}
				}
				doomed = append(doomed, r.seq)
			}
			for len(doomed) > 0 {
				n := min(len(doomed), 500)
				res, err := tx.ExecContext(ctx, `DELETE FROM events WHERE seq IN (?`+
					strings.Repeat(",?", n-1)+`)`, doomed[:n]...)
				if err != nil {
					return 0, err
				}
				affected, _ := res.RowsAffected()
				removed += affected
				doomed = doomed[n:]
			}
			return removed, nil
		})
		if err != nil {
			return s.notePrune(out, began), err
		}
		out.Batches++
		out.Removed += removed
		excess -= removed
	}
	out = s.notePrune(out, began)
	if out.Removed > 0 {
		log.Printf("store: the events prune removed %d row(s) in %d batch(es), %s", out.Removed, out.Batches, out.Took)
	}
	return out, nil
}

func (s *Store) notePrune(out EventPrune, began time.Time) EventPrune {
	out.Took = time.Since(began)
	s.prunes.mu.Lock()
	s.prunes.removed += out.Removed
	s.prunes.last = time.Now()
	s.prunes.mu.Unlock()
	return out
}
