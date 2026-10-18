package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// Retention: what the outbox and the receipts may forget, and when.
//
// A done effect is read by nothing that looks for work (AdoptEffects,
// OwnOpenEffects, the receipt and pause checks all ask for pending or
// started). Two kinds are read by subject whatever their state — the
// capacity push's latest outcome per row, and a pause's delivery history,
// which stops at its newest done — so for those the newest row of each
// subject is kept. Failed and unknown rows are never pruned: they are what a
// person may still have to look at.
//
// An expired receipt is a tombstone that tells a late resend "expired"
// instead of running it again. Every scope's window is at most a day, and
// the longest resend of a random key is the console's saved card, kept 24
// hours from its press (web/console/src/session/persist.ts KEPT_MS), so a
// week of tombstones covers it with room for a wrong clock. A key derived
// from the request's content can be sent again at any later time and still
// be the same request; those tombstones are kept for good.

const (
	// DoneEffectRetentionSecondsLimit is how long a done outbox row stays
	// after it finished: a week.
	DoneEffectRetentionSecondsLimit = 7 * 24 * 60 * 60
	// ReceiptTombstoneRetentionSecondsLimit is how long an expired receipt
	// with a random key stays after its request was first claimed: a week.
	ReceiptTombstoneRetentionSecondsLimit = 7 * 24 * 60 * 60
	// RetentionPruneBatchLimit is the most rows one prune deletes from one
	// table, so a first prune of a long history never holds the write lock
	// for long; the rest goes at the next maintenance pass.
	RetentionPruneBatchLimit = 500
)

// contentDerivedKeyPrefixes are the Idempotency-Key spellings computed from
// the request itself (cmd/clawdline task.go, task_cancel.go). A tombstone
// with one of them is never pruned.
var contentDerivedKeyPrefixes = []string{"gate-evidence-", "gate-result-", "task-cancel-"}

// PruneDoneEffects deletes at most RetentionPruneBatchLimit done effects that
// finished before now less DoneEffectRetentionSecondsLimit, keeping the newest
// row of each subject for every kind in keepLatest. It answers how many went.
func (s *Store) PruneDoneEffects(ctx context.Context, now time.Time, keepLatest []string) (int64, error) {
	cutoff := now.Add(-DoneEffectRetentionSecondsLimit * time.Second).Unix()
	keep := "0"
	args := []any{cutoff}
	if len(keepLatest) > 0 {
		keep = `SELECT MAX(id) FROM outbox WHERE kind IN (` + placeholders(len(keepLatest)) + `) GROUP BY kind, subject`
		for _, k := range keepLatest {
			args = append(args, k)
		}
	}
	args = append(args, RetentionPruneBatchLimit)
	return s.prune(ctx, `DELETE FROM outbox WHERE id IN (
	  SELECT id FROM outbox WHERE state = 'done' AND finished_at > 0 AND finished_at < ?
	    AND id NOT IN (`+keep+`)
	  ORDER BY id LIMIT ?)`, args...)
}

// PruneReceiptTombstones deletes at most RetentionPruneBatchLimit expired
// receipts first claimed before now less ReceiptTombstoneRetentionSecondsLimit,
// never one whose key is derived from its request's content. It answers how
// many went.
func (s *Store) PruneReceiptTombstones(ctx context.Context, now time.Time) (int64, error) {
	cutoff := now.Add(-ReceiptTombstoneRetentionSecondsLimit * time.Second).Unix()
	var derived strings.Builder
	args := []any{cutoff}
	for _, p := range contentDerivedKeyPrefixes {
		derived.WriteString(` AND substr(key, 1, ?) <> ?`)
		args = append(args, len(p), p)
	}
	args = append(args, RetentionPruneBatchLimit)
	return s.prune(ctx, `DELETE FROM request_receipts WHERE rowid IN (
	  SELECT rowid FROM request_receipts WHERE state = 'expired' AND created_at < ?`+derived.String()+`
	  ORDER BY created_at LIMIT ?)`, args...)
}

func (s *Store) prune(ctx context.Context, query string, args ...any) (int64, error) {
	var n int64
	err := s.write(ctx, func(tx *sql.Tx) (int64, error) {
		res, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return 0, err
		}
		n, err = res.RowsAffected()
		return n, err
	})
	return n, err
}
