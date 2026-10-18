package orchestrator

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
)

// ledgerSnapshot is every task head this broker has read, each with the
// record decoded at that head's version, newest first.
//
// It is not a second copy of a fact. It is used only after the store's tally
// says the table is exactly what the snapshot was taken from, and a row is
// used from here only while the store still says it is at this version — so a
// reader holding one of these holds exactly what it would have read, minus
// the reading. Rows are never deleted from the table, so it is bounded by it.
//
// Bringing it up to date costs the tally, then the heads written since the
// last one it saw, then the records whose version moved (G33). A tally that
// still disagrees after that — a writer whose clock was behind, a migration
// that moved a version and not updated_at — falls back to every head. One
// whose clock was ahead does not: the heads are read from when the last
// tally was asked for, if that is earlier than the newest updated_at.
type ledgerSnapshot struct {
	mu     sync.Mutex
	synced bool
	tally  store.BrokerTally
	rows   map[string]decodedRecord
	// order is every id in rows, as BrokerTaskHeads orders them: newest
	// created first, then descending id.
	order []string
	// fullScans is how many times sync read every head, the first time
	// included; a test holds the cost of a fallback with it.
	fullScans int
	// at is this machine's clock when the tally the snapshot holds was asked
	// for. A write stamped by an honest clock after it is stamped at or after
	// it, so sync looks from here when the newest updated_at is later —
	// which only a writer whose clock ran ahead can make it.
	at time.Time
}

type decodedRecord struct {
	head   store.BrokerHead
	record Record
	err    error
	// row is the stored row an unreadable record came from, kept for its
	// lifted columns. Nil for a record that decoded.
	row *store.BrokerRow
}

// sync brings the snapshot up to the table. The caller holds l.mu.
func (l *ledgerSnapshot) sync(ctx context.Context, st *store.Store) error {
	now := time.Now()
	tally, err := st.BrokerTaskTally(ctx)
	if err != nil {
		return err
	}
	if l.synced && tally == l.tally {
		l.at = now
		return nil
	}
	if l.synced {
		// The earlier of the two, so a future updated_at costs re-reading
		// its own row, not every head until the clock reaches it.
		since := min(l.tally.Through, l.at.Unix())
		heads, err := st.BrokerTaskHeadsSince(ctx, time.Unix(since, 0))
		if err != nil {
			return err
		}
		if err := l.merge(ctx, st, heads, false); err != nil {
			return err
		}
		if l.tallied() == tally {
			l.tally, l.at = tally, now
			return nil
		}
	}
	heads, err := st.BrokerTaskHeads(ctx)
	if err != nil {
		return err
	}
	l.fullScans++
	if err := l.merge(ctx, st, heads, true); err != nil {
		return err
	}
	// The tally of what was read, not of what was asked about first: a write
	// between the two is then a disagreement the next sync settles.
	l.tally, l.synced, l.at = l.tallied(), true, now
	return nil
}

// merge takes heads into the snapshot, fetching and decoding only the rows
// whose version it does not hold. With whole set, heads is every row, and
// the snapshot becomes exactly them.
func (l *ledgerSnapshot) merge(ctx context.Context, st *store.Store, heads []store.BrokerHead, whole bool) error {
	stale := []string{}
	for _, h := range heads {
		if got, ok := l.rows[h.ID]; !ok || got.head.Version != h.Version {
			stale = append(stale, h.ID)
		}
	}
	fetched := map[string]store.BrokerRow{}
	if len(stale) > 0 {
		var err error
		if fetched, err = st.BrokerTaskRecords(ctx, stale); err != nil {
			return err
		}
	}
	next := l.rows
	if whole || next == nil {
		next = make(map[string]decodedRecord, len(heads))
	}
	added := false
	for _, h := range heads {
		if row, ok := fetched[h.ID]; ok {
			r, err := Decode(row.Record)
			// The row's own columns, which are newer than the head's if a
			// write landed between the two reads.
			d := decodedRecord{record: r, err: err, head: store.BrokerHead{
				ID: row.ID, Project: row.Project, Repository: row.Repository, Assistant: row.Assistant,
				State: row.State, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, Version: row.Version}}
			if err != nil {
				kept := row
				d.row = &kept
			}
			if _, ok := next[h.ID]; !ok {
				added = true
			}
			next[h.ID] = d
			continue
		}
		if got, ok := l.rows[h.ID]; ok && got.head.Version == h.Version {
			next[h.ID] = got
		}
		// Otherwise gone between the two reads: it is not in this snapshot,
		// and its tally will say so.
	}
	l.rows = next
	if whole {
		l.order = make([]string, 0, len(heads))
		for _, h := range heads {
			if _, ok := next[h.ID]; ok {
				l.order = append(l.order, h.ID)
			}
		}
		return nil
	}
	if added {
		l.order = l.order[:0]
		for id := range next {
			l.order = append(l.order, id)
		}
		sort.Slice(l.order, func(i, j int) bool {
			a, b := next[l.order[i]].head, next[l.order[j]].head
			if !a.CreatedAt.Equal(b.CreatedAt) {
				return a.CreatedAt.After(b.CreatedAt)
			}
			return a.ID > b.ID
		})
	}
	return nil
}

// tallied is the tally of the rows the snapshot holds.
func (l *ledgerSnapshot) tallied() store.BrokerTally {
	t := store.BrokerTally{Rows: int64(len(l.rows))}
	for _, d := range l.rows {
		t.Versions += d.head.Version
		if u := d.head.UpdatedAt.Unix(); u > t.Through {
			t.Through = u
		}
	}
	return t
}
