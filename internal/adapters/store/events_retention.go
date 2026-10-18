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
// So the store keeps the newest row per (kind, subject) of each kind in
// eventKindsReadLatest until its subject has been silent for
// EventKeptAgeLimit, and lets everything else go once it is older than
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
	// EventKeptAgeLimit is how long the newest row about a subject is kept
	// once nothing newer has been written about it. Each reader asks about
	// something with a natural end far short of it: a run is relayable for a
	// day (RelayWindow) and run.latest matters only beside the delivery it may
	// supersede; a delivery receipt is drawn for the conversation still on
	// its terminal, which a message, a delivery or a new conversation replaces
	// within days; a waiting stop is decided after ten minutes and ended by
	// the next reading. A subject silent for thirty days is a Session or run
	// that is gone, or one so still that forgetting it costs at most a
	// receipt no longer drawn or, after a restart, a second waiting push a
	// month after the first. All three kinds share it, so a newer run is
	// never forgotten before the older delivery it supersedes.
	EventKeptAgeLimit = 30 * 24 * time.Hour
)

// eventKindsReadLatest are the kinds a program reads back, always as the
// newest row about a subject (LatestEvents). Their newest row per subject
// outlives EventAgeLimit: each answers a question about a session that may
// still exist — the run of its last message, its delivered turn, whether its
// waiting push was already decided — until EventKeptAgeLimit says it is gone.
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
// survives every prune until EventKeptAgeLimit. A reader of LatestEvents
// relies on it.
func EventKindKeptLatest(kind string) bool { return eventKindsReadLatest[kind] }

// EventPrune is what one prune did.
type EventPrune struct {
	// Removed is how many rows were deleted.
	Removed int64
	// Batches is how many write transactions it took.
	Batches int
	// Walked is how many rows the walk read, which starts after the newest
	// row the last prune decided.
	Walked int64
	// Took is how long the whole prune lasted.
	Took time.Duration
}

// eventPruneStats is this handle's account of its prunes, for the capacity
// register's reading, and where the last prune stopped.
type eventPruneStats struct {
	mu      sync.Mutex
	removed int64
	last    time.Time

	// running holds one prune at a time; cursor and mark are its state.
	running sync.Mutex
	// cursor is the newest row the walk has decided. Every row at or before it
	// that is still in the table is a kept newest row, so the next walk starts
	// after it rather than at seq 0. In memory: a restart walks once from the
	// start.
	cursor int64
	// mark is the newest row that existed when the last prune began. A kept
	// kind's row after it may have superseded a kept row behind the cursor.
	mark int64
}

// EventPruneStats is how many rows this handle's prunes have removed, and
// when the newest prune finished.
func (s *Store) EventPruneStats() (removed int64, last time.Time) {
	s.prunes.mu.Lock()
	defer s.prunes.mu.Unlock()
	return s.prunes.removed, s.prunes.last
}

// PruneEvents deletes the rows the retention rule no longer keeps, as of now,
// in transactions of at most EventPruneBatchLimit examined rows. Behind where
// the last prune stopped only kept rows remain: it lets go of those that have
// expired or been superseded since, then walks on from there and stops at the
// first row that is both young enough and not needed to bring the table under
// EventKeepLimit. A failure leaves every batch before it deleted and answers
// what was done with it; the next prune resumes from the last batch that
// committed.
func (s *Store) PruneEvents(ctx context.Context, now time.Time) (EventPrune, error) {
	return s.pruneEvents(ctx, now, EventAgeLimit, EventKeptAgeLimit, EventKeepLimit, EventPruneBatchLimit)
}

func (s *Store) pruneEvents(ctx context.Context, now time.Time, age, keptAge time.Duration, keep, batch int) (EventPrune, error) {
	s.prunes.running.Lock()
	defer s.prunes.running.Unlock()
	began := time.Now()
	var out EventPrune
	if err := reading(); err != nil {
		return out, err
	}
	var total, top int64
	if err := s.rd.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(MAX(seq), 0) FROM events`).Scan(&total, &top); err != nil {
		return out, classify(err)
	}
	cutoff := now.Add(-age).Unix()
	keptCutoff := now.Add(-keptAge).Unix()
	deleted := func(removed int64) {
		out.Batches++
		out.Removed += removed
		total -= removed
	}

	// The kept rows behind the cursor whose subject has been silent too long.
	// They are in seq order, which is time order, so the expired ones are the
	// head of the table.
	for s.prunes.cursor > 0 {
		if err := ctx.Err(); err != nil {
			return s.notePrune(out, began), err
		}
		var removed int64
		more := false
		err := s.write(ctx, func(tx *sql.Tx) (int64, error) {
			seqs, err := querySeqs(ctx, tx, `SELECT seq FROM events WHERE seq <= ? AND at < ? ORDER BY seq LIMIT ?`,
				s.prunes.cursor, keptCutoff, batch)
			if err != nil {
				return 0, err
			}
			more = len(seqs) == batch
			removed, err = deleteSeqs(ctx, tx, seqs)
			return removed, err
		})
		if err != nil {
			return s.notePrune(out, began), err
		}
		deleted(removed)
		if !more {
			break
		}
	}

	// The kept rows behind the cursor that a newer row about the same subject,
	// written since the last prune began, has superseded.
	if s.prunes.cursor > 0 && top > s.prunes.mark {
		newest, err := s.newestKeptSince(ctx, s.prunes.mark, top)
		if err != nil {
			return s.notePrune(out, began), err
		}
		for len(newest) > 0 {
			if err := ctx.Err(); err != nil {
				return s.notePrune(out, began), err
			}
			n := min(len(newest), batch)
			var removed int64
			err := s.write(ctx, func(tx *sql.Tx) (int64, error) {
				removed = 0
				for _, k := range newest[:n] {
					res, err := tx.ExecContext(ctx,
						`DELETE FROM events WHERE kind = ? AND subject = ? AND seq <= ? AND seq < ?`,
						k.kind, k.subject, s.prunes.cursor, k.seq)
					if err != nil {
						return 0, err
					}
					affected, _ := res.RowsAffected()
					removed += affected
				}
				return removed, nil
			})
			if err != nil {
				return s.notePrune(out, began), err
			}
			deleted(removed)
			newest = newest[n:]
		}
	}
	s.prunes.mark = top

	// The walk, from the cursor on.
	for done := false; !done; {
		if err := ctx.Err(); err != nil {
			return s.notePrune(out, began), err
		}
		after := s.prunes.cursor
		excess := total - int64(keep)
		var removed, walked int64
		err := s.write(ctx, func(tx *sql.Tx) (int64, error) {
			rows, err := tx.QueryContext(ctx,
				`SELECT seq, at, kind, subject FROM events WHERE seq > ? ORDER BY seq LIMIT ?`, s.prunes.cursor, batch)
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
			done = len(seen) < batch
			walked = int64(len(seen))
			var doomed []int64
			for _, r := range seen {
				if r.at >= cutoff && excess-int64(len(doomed)) <= 0 {
					done = true
					break
				}
				after = r.seq
				if eventKindsReadLatest[r.kind] && r.at >= keptCutoff {
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
			removed, err = deleteSeqs(ctx, tx, doomed)
			return removed, err
		})
		if err != nil {
			return s.notePrune(out, began), err
		}
		s.prunes.cursor = after
		out.Walked += walked
		deleted(removed)
	}
	out = s.notePrune(out, began)
	if out.Removed > 0 {
		log.Printf("store: the events prune removed %d row(s) in %d batch(es), %s", out.Removed, out.Batches, out.Took)
	}
	return out, nil
}

// keptSubject is one subject of a kept kind and its newest row.
type keptSubject struct {
	kind, subject string
	seq           int64
}

// newestKeptSince is, for each subject of a kept kind with a row in
// (after, through], its newest such row.
func (s *Store) newestKeptSince(ctx context.Context, after, through int64) ([]keptSubject, error) {
	kinds := make([]any, 0, len(eventKindsReadLatest)+2)
	kinds = append(kinds, after, through)
	for kind := range eventKindsReadLatest {
		kinds = append(kinds, kind)
	}
	rows, err := s.rd.QueryContext(ctx, `SELECT kind, subject, MAX(seq) FROM events
		WHERE seq > ? AND seq <= ? AND kind IN (?`+strings.Repeat(",?", len(eventKindsReadLatest)-1)+`)
		GROUP BY kind, subject`, kinds...)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	var out []keptSubject
	for rows.Next() {
		var k keptSubject
		if err := rows.Scan(&k.kind, &k.subject, &k.seq); err != nil {
			return nil, classify(err)
		}
		out = append(out, k)
	}
	return out, classify(rows.Err())
}

func querySeqs(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var seq int64
		if err := rows.Scan(&seq); err != nil {
			return nil, err
		}
		out = append(out, seq)
	}
	return out, rows.Err()
}

func deleteSeqs(ctx context.Context, tx *sql.Tx, seqs []int64) (int64, error) {
	var removed int64
	for len(seqs) > 0 {
		n := min(len(seqs), 500)
		args := make([]any, n)
		for i, seq := range seqs[:n] {
			args[i] = seq
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM events WHERE seq IN (?`+strings.Repeat(",?", n-1)+`)`, args...)
		if err != nil {
			return 0, err
		}
		affected, _ := res.RowsAffected()
		removed += affected
		seqs = seqs[n:]
	}
	return removed, nil
}

func (s *Store) notePrune(out EventPrune, began time.Time) EventPrune {
	out.Took = time.Since(began)
	s.prunes.mu.Lock()
	s.prunes.removed += out.Removed
	s.prunes.last = time.Now()
	s.prunes.mu.Unlock()
	return out
}
