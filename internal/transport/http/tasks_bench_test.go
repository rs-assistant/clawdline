package http

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// BenchmarkTaskListWithHistory is the list producer's two-second build — the
// first page of 50 — over 3,000 finished tasks, each with its acknowledged
// notice, and 3 live ones: the shape of this machine's store when the
// daemon was profiled (2,650 rows, 2,591 notices).
func BenchmarkTaskListWithHistory(bm *testing.B) {
	p := &pane{s: session.Session{ID: "pane-1", Backend: session.BackendTmux, Assistant: session.AssistantClaude,
		State: session.StateIdle}}
	dir := filepath.Join(bm.TempDir(), "clawdline-next")
	st, err := store.Open(dir)
	if err != nil {
		bm.Fatal(err)
	}
	bm.Cleanup(func() { st.Close() })
	hosts := []ports.TerminalHost{p}
	s := &Server{store: st, terminals: hosts, inventory: app.Inventory{Terminals: hosts, Screen: p},
		broker: &orchestrator.Broker{Store: st, Tasks: taskdir.New(dir), Dir: dir}}
	ctx := context.Background()
	start := time.Now().Add(-30 * 24 * time.Hour)
	for i := 0; i < 3003; i++ {
		at := start.Add(time.Duration(i) * time.Minute)
		r := orchestrator.Record{Protocol: orchestrator.Protocol, Assistant: "claude", CreatedAt: at,
			Claims: []string{"internal/app"}, ProjectDir: "/repo", Repository: "/repo",
			Root: &orchestrator.RootRef{SessionID: "11111111-1111-4111-8111-111111111111", Assistant: "claude"}}
		var n *store.BrokerNotice
		if i < 3000 {
			r.ID = fmt.Sprintf("e0000000-0000-4000-8000-%012d", i)
			r.Title, r.State, r.FinishedAt = fmt.Sprint("finished ", i), orchestrator.StateSuccess, at.Add(time.Minute)
			n = &store.BrokerNotice{ID: "n-" + r.ID, TaskID: r.ID, State: "acknowledged", CreatedAt: at}
		} else {
			r.ID = fmt.Sprintf("e1111111-1111-4111-8111-%012d", i)
			r.Title, r.State = "live", orchestrator.StateBriefed
		}
		row := store.BrokerRow{ID: r.ID, Project: r.ProjectDir, Repository: r.Repository, Assistant: r.Assistant,
			State: string(r.State), CreatedAt: at, UpdatedAt: at, SecretHash: "h", Record: r.Encode()}
		if err := s.broker.Store.SaveBrokerTaskWithNotice(ctx, row, n, nil); err != nil {
			bm.Fatal(err)
		}
	}
	if list, err := s.tasksPayload(ctx, 0, 50); err != nil || len(list.Tasks) != 53 {
		bm.Fatalf("first page: %d rows, %v", len(list.Tasks), err)
	}
	bm.ReportAllocs()
	bm.ResetTimer()
	for i := 0; i < bm.N; i++ {
		if _, err := s.tasksPayload(ctx, 0, 50); err != nil {
			bm.Fatal(err)
		}
	}
}
