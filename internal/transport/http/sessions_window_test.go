package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/ownrecords"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

const (
	historyRoot   = "11111111-1111-4111-8111-111111111111"
	historyChild  = "22222222-2222-4222-8222-222222222222"
	historyLive   = "33333333-3333-4333-8333-333333333333"
	historyAbsent = "44444444-4444-4444-8444-444444444444"
)

// sessionHistory is a store of 3,000 finished tasks and 3 live ones, the
// shape of this machine's ledger when the daemon was profiled, with three
// assistant Sessions on screen bound to some of each: a root that dispatched
// most of the history, a terminal holding the child of its newest finished
// task, and a terminal holding a live child. Among the finished tasks are
// pending landings owed by the root and by a conversation not on screen,
// tasks whose child terminal is on screen but whose process is not theirs,
// and tasks whose child terminal is gone. raws are stored as further rows
// exactly as given.
func sessionHistory(tb testing.TB, raws ...string) (*Server, session.Inventory) {
	tb.Helper()
	p := &pane{s: session.Session{ID: "pane-root", Backend: session.BackendTmux, Assistant: session.AssistantClaude,
		State: session.StateIdle}}
	dir := filepath.Join(tb.TempDir(), "clawdline-next")
	st, err := store.Open(dir)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { st.Close() })
	hosts := []ports.TerminalHost{p}
	s := &Server{store: st, terminals: hosts, inventory: app.Inventory{Terminals: hosts, Screen: p},
		broker: &orchestrator.Broker{Store: st, Tasks: taskdir.New(dir), Dir: dir}}
	ctx := context.Background()

	pid := os.Getpid()
	procStart := ownrecords.ProcessStart(pid)
	if procStart.IsZero() {
		tb.Skip("this process's start time cannot be read")
	}
	commit := "0123456789abcdef0123456789abcdef01234567"
	target := "89abcdef0123456789abcdef0123456789abcdef"
	put := func(r orchestrator.Record, notice *store.BrokerNotice, raw []byte) {
		tb.Helper()
		if raw == nil {
			raw = r.Encode()
		}
		row := store.BrokerRow{ID: r.ID, Project: r.ProjectDir, Repository: r.Repository, Assistant: r.Assistant,
			State: string(r.State), CreatedAt: r.CreatedAt, UpdatedAt: r.CreatedAt, SecretHash: "h", Record: raw}
		if err := st.SaveBrokerTaskWithNotice(ctx, row, notice, nil); err != nil {
			tb.Fatal(err)
		}
	}
	start := procStart.Add(-30 * 24 * time.Hour)
	for i := 0; i < 3000; i++ {
		// Several share a second, so tie order is part of the comparison.
		at := start.Add(time.Duration(i/3) * time.Minute)
		r := orchestrator.Record{Protocol: orchestrator.Protocol, ID: fmt.Sprintf("e0000000-0000-4000-8000-%012d", i),
			Assistant: "claude", Title: fmt.Sprint("finished ", i), State: orchestrator.StateSuccess, CreatedAt: at,
			FinishedAt: at.Add(time.Minute), Claims: []string{"internal/app"}, ProjectDir: "/repo", Repository: "/repo",
			Root:    &orchestrator.RootRef{SessionID: historyRoot, Assistant: "claude"},
			Landing: &orchestrator.Landing{State: orchestrator.LandingLanded, Target: "main", Commit: commit, TargetCommit: target, At: at.Add(2 * time.Minute)}}
		switch {
		case i%7 == 0:
			r.State = orchestrator.StateFailure
			r.Landing = &orchestrator.Landing{State: orchestrator.LandingAbandoned, Target: "main", At: at.Add(2 * time.Minute)}
		case i%500 == 1:
			r.Landing = &orchestrator.Landing{State: orchestrator.LandingPending, Target: "main", Note: "owed by the root"}
		case i%500 == 2:
			r.Root = &orchestrator.RootRef{SessionID: historyAbsent, Assistant: "claude"}
			r.Landing = &orchestrator.Landing{State: orchestrator.LandingPending, Target: "main", Note: "owed elsewhere"}
		case i%500 == 3:
			r.Root = nil
			r.Landing = &orchestrator.Landing{State: orchestrator.LandingPending, Note: "owed by a person"}
		}
		switch {
		case i%11 == 0:
			// A child terminal long gone.
			r.ChildTerminalID, r.ChildBackend, r.ChildConversationID = fmt.Sprintf("old-%d", i), "tmux", fmt.Sprintf("c-%d", i)
		case i%97 == 0:
			// The child terminal is on screen again, held by another process.
			r.ChildTerminalID, r.ChildBackend, r.ChildConversationID = "pane-child", "tmux", fmt.Sprintf("c-%d", i)
		}
		var n *store.BrokerNotice
		if i%2 == 0 {
			n = &store.BrokerNotice{ID: "n-" + r.ID, TaskID: r.ID, State: "acknowledged", CreatedAt: at}
		}
		put(r, n, nil)
	}
	// The newest finished task: its child is the process in pane-child.
	put(orchestrator.Record{Protocol: orchestrator.Protocol, ID: "e2222222-2222-4222-8222-000000000000",
		Assistant: "claude", Title: "bound finished", State: orchestrator.StateSuccess,
		CreatedAt: procStart.Add(-time.Minute), FinishedAt: time.Now(), Claims: []string{"internal/app"},
		ProjectDir: "/repo", Repository: "/repo", ChildTerminalID: "pane-child", ChildBackend: "tmux",
		ChildConversationID: historyChild, Root: &orchestrator.RootRef{SessionID: historyRoot, Assistant: "claude"},
		Landing: &orchestrator.Landing{State: orchestrator.LandingLanded, Target: "main", Commit: commit,
			TargetCommit: target, At: time.Now()}}, nil, nil)
	// A finished task told only by its root, its completion notice not yet
	// acknowledged. No Session row reads a notice today (ownTasks carries no
	// CompletionDelivery), so the narrow read leaves it out and the page is
	// the same. Were a row ever to read it, the whole read would carry this
	// task and the narrow one would not, and this comparison would fail.
	put(orchestrator.Record{Protocol: orchestrator.Protocol, ID: "e3333333-3333-4333-8333-000000000000",
		Assistant: "claude", Title: "finished, notice pending", State: orchestrator.StateSuccess,
		CreatedAt: procStart.Add(-2 * time.Minute), FinishedAt: time.Now(), Claims: []string{},
		ProjectDir: "/repo", Repository: "/repo", Root: &orchestrator.RootRef{SessionID: historyRoot, Assistant: "claude"}},
		&store.BrokerNotice{ID: "n-pending", TaskID: "e3333333-3333-4333-8333-000000000000", State: "pending", CreatedAt: time.Now()}, nil)
	// Three live: a child in pane-live, a callback the root waits on, and a
	// task of a root not on screen.
	put(orchestrator.Record{Protocol: orchestrator.Protocol, ID: "e1111111-1111-4111-8111-000000000000",
		Assistant: "claude", Title: "live child", State: orchestrator.StateBriefed,
		CreatedAt: procStart.Add(-time.Minute), Claims: []string{"internal/app"}, ProjectDir: "/repo",
		Repository: "/repo", ChildTerminalID: "pane-live", ChildBackend: "tmux", ChildConversationID: historyLive,
		Root: &orchestrator.RootRef{SessionID: historyRoot, Assistant: "claude"}}, nil, nil)
	put(orchestrator.Record{Protocol: orchestrator.Protocol, ID: "e1111111-1111-4111-8111-000000000001",
		Kind: orchestrator.TaskKindCallback, Assistant: "claude", Title: "go test", State: orchestrator.StateQueued,
		CreatedAt: procStart.Add(-time.Minute), Claims: []string{}, ProjectDir: "/repo", Repository: "/repo",
		Callback: &orchestrator.Callback{Intent: orchestrator.CallbackIntentWait, Argv: []string{"go", "test"}, Dir: "/repo"},
		Root:     &orchestrator.RootRef{SessionID: historyRoot, Assistant: "claude"}}, nil, nil)
	put(orchestrator.Record{Protocol: orchestrator.Protocol, ID: "e1111111-1111-4111-8111-000000000002",
		Assistant: "claude", Title: "elsewhere", State: orchestrator.StateBriefed,
		CreatedAt: procStart.Add(-time.Minute), Claims: []string{"internal/app"}, ProjectDir: "/repo",
		Repository: "/repo", Root: &orchestrator.RootRef{SessionID: historyAbsent, Assistant: "claude"}}, nil, nil)
	for i, raw := range raws {
		id := fmt.Sprintf("c0000000-0000-4000-8000-%012d", i)
		put(orchestrator.Record{ID: id, Assistant: "claude", State: orchestrator.StateSuccess,
			CreatedAt: start.Add(time.Duration(i) * time.Minute), ProjectDir: "/repo"}, nil, []byte(raw))
	}

	row := func(id, conversation, tty string) session.Session {
		return session.Session{ID: id, Backend: session.BackendTmux, Assistant: session.AssistantClaude,
			ConversationID: conversation, Binding: session.BindingCommandLine, State: session.StateIdle,
			PID: pid, TTY: tty, CWD: "/repo"}
	}
	inv := session.Inventory{Complete: true, Provenance: "test", ObservedAt: time.Now(),
		Sources: map[string]bool{"tmux": true},
		Sessions: []session.Session{
			row("pane-root", historyRoot, "/dev/ttys001"),
			row("pane-child", historyChild, "/dev/ttys002"),
			row("pane-live", historyLive, "/dev/ttys003"),
		}}
	return s, inv
}

// BenchmarkSessionListWithHistory is the Session list's two-second build over
// that history.
func BenchmarkSessionListWithHistory(bm *testing.B) {
	s, inv := sessionHistory(bm)
	ctx := context.Background()
	if snap := s.sessionsPayloadFrom(ctx, inv); len(snap.Sessions) != 3 {
		bm.Fatalf("%d rows", len(snap.Sessions))
	}
	bm.ReportAllocs()
	bm.ResetTimer()
	for i := 0; i < bm.N; i++ {
		s.sessionsPayloadFrom(ctx, inv)
	}
}

// wholeLedger is the read the list made before it read SessionTasks: every
// task the broker holds, from a broker of its own, so nothing the server's
// broker holds stands in for reading the table.
func wholeLedger(s *Server) sessionLedgerRead {
	fresh := &orchestrator.Broker{Store: s.broker.Store, Tasks: s.broker.Tasks, Dir: s.broker.Dir}
	return func(ctx context.Context, _ []session.Session) ([]orchestrator.Record, []orchestrator.Unreadable, error) {
		return fresh.Records(ctx)
	}
}

// sameMomentJSON is a snapshot as JSON with the build's own counters and
// clocks taken out: two builds a moment apart differ in those and nothing
// else.
func sameMomentJSON(t *testing.T, snap sessionsSnapshotWire) []byte {
	t.Helper()
	snap.At, snap.Scan.Generation, snap.Scan.Completed.Sequence = 0, 0, 0
	rows := make([]sessionRowWire, len(snap.Sessions))
	for i, row := range snap.Sessions {
		row.Closeability.SessionGeneration, row.Closeability.ObservedAt = 0, 0
		rows[i] = row
	}
	snap.Sessions = rows
	out, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// The Session list reads only the tasks its rows can be told by, and it is
// the list the whole history gave: byte for byte, over 3,000 finished tasks
// and 3 live ones with terminals on screen bound to some of each — and, with
// an unreadable row deep in the history, the same "unknown" the whole
// history gave.
func TestTheSessionListIsTheOneTheWholeHistoryGave(t *testing.T) {
	for _, tc := range []struct {
		name string
		raws []string
		want []string
	}{
		{name: "readable", want: []string{
			// The newest finished task's child, by its delivery.
			`"taskId":"e2222222-2222-4222-8222-000000000000"`,
			// The root's live callback, as a shell of its own.
			`"id":"` + callbackShellPrefix + "e1111111-1111-4111-8111-000000000001",
			// The live child the root waits on.
			`"code":"live_descendant_task"`,
			// The pending landings the root owes, deep in the history.
			`"code":"landing"`,
			// The live child's terminal, named by its task.
			`"label":"live child"`,
		}},
		{name: "unreadable", raws: []string{`{"task_id": "`}, want: []string{`"own_records_unreadable"`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, inv := sessionHistory(t, tc.raws...)
			ctx := context.Background()
			before := sameMomentJSON(t, s.sessionsPayloadRead(ctx, inv, wholeLedger(s)))
			after := sameMomentJSON(t, s.sessionsPayloadFrom(ctx, inv))
			if !bytes.Equal(before, after) {
				t.Fatalf("the narrow read changed the list\nwhole:  %s\nnarrow: %s", before, after)
			}
			for _, want := range tc.want {
				if !strings.Contains(string(after), want) {
					t.Errorf("the fixture does not exercise %s:\n%s", want, after)
				}
			}
			if len(inv.Sessions) != 3 {
				t.Fatalf("%d sessions", len(inv.Sessions))
			}
		})
	}
}

// The window is what it says: of 3,000 finished tasks, only the ones a row
// can be told by.
func TestSessionTasksLeavesTheRestOfTheHistoryUnread(t *testing.T) {
	s, inv := sessionHistory(t)
	ctx := context.Background()
	whole, _, err := s.broker.Records(ctx)
	if err != nil {
		t.Fatal(err)
	}
	narrow, bad, err := s.sessionTasks(ctx, inv.Sessions)
	if err != nil || len(bad) != 0 {
		t.Fatalf("read: %v, %d unreadable", err, len(bad))
	}
	if len(whole) != 3005 || len(narrow) == 0 || len(narrow) > len(whole)/2 {
		t.Fatalf("whole %d, narrow %d", len(whole), len(narrow))
	}
	t.Logf("whole %d, narrow %d", len(whole), len(narrow))
}
