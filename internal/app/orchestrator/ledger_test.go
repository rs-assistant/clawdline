package orchestrator

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
)

// A writer whose clock ran ahead stamps a row with an updated_at that has
// not happened yet. The snapshot must not then read every head after each
// later honest write until the clock catches up: those writes are stamped
// after the snapshot last synced, and that is where it looks.
func TestAFutureUpdatedAtDoesNotKeepTheLedgerOnFullScans(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	create := func(id string, at time.Time) {
		t.Helper()
		row := store.BrokerRow{ID: id, Project: "/repo", Repository: "/repo", Assistant: "claude",
			State: string(StateBriefed), CreatedAt: at, UpdatedAt: at, Record: json.RawMessage(`{}`)}
		if _, err := st.CreateBrokerTask(ctx, row, nil); err != nil {
			t.Fatal(err)
		}
	}
	create("skewed", time.Now().Add(time.Hour))
	l := &ledgerSnapshot{}
	if err := l.sync(ctx, st); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"honest-1", "honest-2", "honest-3"} {
		create(id, time.Now())
		if err := l.sync(ctx, st); err != nil {
			t.Fatal(err)
		}
		if _, ok := l.rows[id]; !ok {
			t.Fatalf("%s is not in the snapshot after its sync", id)
		}
	}
	if l.fullScans != 1 {
		t.Fatalf("sync read every head %d times, want 1 (the first): each honest write after a future-stamped row fell back to a full scan", l.fullScans)
	}
}
