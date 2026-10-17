package orchestrator

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/git"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// historyBroker is a broker whose store holds the shape this machine's does:
// thousands of finished tasks, each with its acknowledged notice, and a few
// live ones (2,650 rows and 2,591 notices when the beat was profiled).
func historyBroker(tb testing.TB, finished, live int) (*Broker, context.Context) {
	tb.Helper()
	dir := tb.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { _ = st.Close() })
	b := &Broker{
		Store: st, Tasks: taskdir.New(dir), Git: git.New(), Dir: dir,
		ScreenReadEvery: 10 * time.Millisecond,
		Live: func(context.Context) []session.Session {
			return []session.Session{{ID: "%1", Assistant: session.AssistantClaude, ConversationID: rootConversation}}
		},
	}
	ctx := context.Background()
	start := time.Now().Add(-30 * 24 * time.Hour)
	for i := 0; i < finished; i++ {
		id := fmt.Sprintf("e0000000-0000-4000-8000-%012d", i)
		at := start.Add(time.Duration(i) * time.Minute)
		r := Record{Protocol: Protocol, ID: id, Assistant: "claude", Title: fmt.Sprintf("finished %d", i),
			State: StateSuccess, CreatedAt: at, FinishedAt: at.Add(time.Minute), Claims: []string{"internal/app"},
			ProjectDir: "/repo", Repository: "/repo",
			Root: &RootRef{SessionID: rootConversation, Assistant: "claude"}}
		n := &store.BrokerNotice{ID: "n-" + id, TaskID: id, State: "acknowledged",
			CreatedAt: at, AcknowledgedAt: at.Add(2 * time.Minute)}
		if err := b.saveWith(ctx, r, HashSecret("s"), "task.success", n); err != nil {
			tb.Fatal(err)
		}
	}
	for i := 0; i < live; i++ {
		id := fmt.Sprintf("e1111111-1111-4111-8111-%012d", i)
		r := Record{Protocol: Protocol, ID: id, Assistant: "claude", Title: fmt.Sprintf("live %d", i),
			State: StateBriefed, CreatedAt: time.Now().Add(-time.Duration(i) * time.Minute), Claims: []string{},
			ProjectDir: "/repo", Repository: "/repo",
			Root: &RootRef{SessionID: rootConversation, Assistant: "claude"}}
		if err := b.save(ctx, r, HashSecret("s"), "task.briefed"); err != nil {
			tb.Fatal(err)
		}
	}
	return b, ctx
}

// BenchmarkBeatWithHistory is one beat pass over 3,000 finished tasks and 3
// live ones, warm: the decoded cache already holds every row.
func BenchmarkBeatWithHistory(bm *testing.B) {
	b, ctx := historyBroker(bm, 3000, 3)
	b.Pass(ctx)
	bm.ReportAllocs()
	bm.ResetTimer()
	for i := 0; i < bm.N; i++ {
		b.Pass(ctx)
	}
}

// BenchmarkLedgerWithHistory is the whole-history read the task list makes.
func BenchmarkLedgerWithHistory(bm *testing.B) {
	b, ctx := historyBroker(bm, 3000, 3)
	if _, _, err := b.Records(ctx); err != nil {
		bm.Fatal(err)
	}
	bm.ReportAllocs()
	bm.ResetTimer()
	for i := 0; i < bm.N; i++ {
		if _, _, err := b.Records(ctx); err != nil {
			bm.Fatal(err)
		}
	}
}
