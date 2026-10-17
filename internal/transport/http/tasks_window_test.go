package http

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/adapters/swiftstore"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// The list's page is cut from a window of the ledger, not from the whole
// history, and it is the page the whole history gave: the same rows in the
// same order, the same counts and the same next cursor, on a store of many
// finished tasks with live ones and unreadable ones among them.
func TestTheTaskListPageIsTheOneTheWholeHistoryGave(t *testing.T) {
	p := &pane{s: session.Session{ID: "pane-1", Backend: session.BackendTmux, Assistant: session.AssistantClaude,
		State: session.StateIdle}}
	s := paneServer(t, p)
	ctx := context.Background()
	st := s.broker.Store
	base := time.Now().Add(-90 * 24 * time.Hour).Truncate(time.Second)
	put := func(r orchestrator.Record, notice *store.BrokerNotice, raw json.RawMessage) {
		t.Helper()
		if raw == nil {
			raw = r.Encode()
		}
		row := store.BrokerRow{ID: r.ID, Project: r.ProjectDir, Assistant: r.Assistant, State: string(r.State),
			CreatedAt: r.CreatedAt, UpdatedAt: r.CreatedAt, SecretHash: "h", Record: raw}
		if err := st.SaveBrokerTaskWithNotice(ctx, row, notice, nil); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 400; i++ {
		id := fmt.Sprintf("a0000000-0000-4000-8000-%012d", i)
		// Several share a second, so the tie order is part of the comparison.
		at := base.Add(time.Duration(i/3) * time.Minute)
		r := orchestrator.Record{Protocol: orchestrator.Protocol, ID: id, Assistant: "claude",
			Title: fmt.Sprint("finished ", i), State: orchestrator.StateSuccess, CreatedAt: at,
			FinishedAt: at.Add(time.Minute), Claims: []string{"a"}, ProjectDir: "/repo"}
		if i%4 == 0 {
			r.State = orchestrator.StateFailure
		}
		var n *store.BrokerNotice
		if i%2 == 0 {
			n = &store.BrokerNotice{ID: "n-" + id, TaskID: id, State: "acknowledged", CreatedAt: at}
		}
		put(r, n, nil)
	}
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("b0000000-0000-4000-8000-%012d", i)
		// One live task is older than most finished ones; it still rides on
		// the first page.
		at := base.Add(time.Duration(i*100) * time.Minute)
		put(orchestrator.Record{Protocol: orchestrator.Protocol, ID: id, Assistant: "claude", Title: "live",
			State: orchestrator.StateBriefed, CreatedAt: at, Claims: []string{}, ProjectDir: "/repo"}, nil, nil)
	}
	// Unreadable rows deep in the history, stored as finished: one is not
	// JSON, the other is JSON of the wrong shape.
	for i, raw := range []string{`{"task_id": "`, `{"task_id": 5}`} {
		id := fmt.Sprintf("c0000000-0000-4000-8000-%012d", i)
		put(orchestrator.Record{ID: id, Assistant: "claude", State: orchestrator.StateSuccess,
			CreatedAt: base.Add(time.Duration(i) * time.Minute), ProjectDir: "/repo"}, nil, json.RawMessage(raw))
	}

	whole := func(cursor, limit int) contract.TaskList {
		t.Helper()
		// A broker of its own, so nothing this one holds stands in for
		// reading the table.
		fresh := &orchestrator.Broker{Store: st, Tasks: s.broker.Tasks, Dir: s.broker.Dir}
		records, unreadable, err := fresh.Records(ctx)
		if err != nil {
			t.Fatal(err)
		}
		rows, page := swiftstore.TaskPage(s.ownTaskRows(unreadable, records, s.screen(ctx)), cursor, limit)
		return contract.TaskList{Tasks: rows, Page: page}
	}
	check := func(what string) {
		t.Helper()
		for _, c := range [][2]int{{0, 50}, {0, 0}, {0, 600}, {50, 50}, {380, 50}, {-3, 50}, {9999, 50}} {
			got, err := s.tasksPayload(ctx, c[0], c[1])
			if err != nil {
				t.Fatal(err)
			}
			want := whole(c[0], c[1])
			if !reflect.DeepEqual(got.Tasks, want.Tasks) || got.Page != want.Page {
				t.Errorf("%s, cursor %d limit %d: page %+v with %d rows, want %+v with %d rows",
					what, c[0], c[1], got.Page, len(got.Tasks), want.Page, len(want.Tasks))
			}
		}
	}
	check("cold")
	first, _ := s.tasksPayload(ctx, 0, 50)
	if first.Page.Unfinished != 5 || first.Page.Finished != 400 || len(first.Tasks) != 55 {
		t.Fatalf("the first page has %d rows, page %+v; want 3 live, 2 unreadable and 50 of 400 finished",
			len(first.Tasks), first.Page)
	}

	// Writes after the first read: a live task finishes, a new one starts,
	// and an old one is rewritten by a writer whose clock is behind — which
	// the heads written since the last read cannot show, and the tally does.
	live, _, err := s.broker.Record(ctx, "b0000000-0000-4000-8000-000000000002")
	if err != nil {
		t.Fatal(err)
	}
	row := store.BrokerRow{ID: live.ID, Project: live.ProjectDir, Assistant: "claude", State: "success",
		CreatedAt: live.CreatedAt, UpdatedAt: time.Now(), SecretHash: "h", Version: 0}
	live.State, live.FinishedAt = orchestrator.StateSuccess, time.Now()
	row.Record = live.Encode()
	if err := st.SaveBrokerTask(ctx, row, nil); err != nil {
		t.Fatal(err)
	}
	put(orchestrator.Record{Protocol: orchestrator.Protocol, ID: "d0000000-0000-4000-8000-000000000000",
		Assistant: "claude", Title: "new", State: orchestrator.StateQueued, CreatedAt: time.Now(),
		Claims: []string{}, ProjectDir: "/repo"}, nil, nil)
	old, _, err := s.broker.Record(ctx, "a0000000-0000-4000-8000-000000000399")
	if err != nil {
		t.Fatal(err)
	}
	old.Title = "rewritten"
	if err := st.SaveBrokerTask(ctx, store.BrokerRow{ID: old.ID, Project: "/repo", Assistant: "claude",
		State: string(old.State), CreatedAt: old.CreatedAt, UpdatedAt: base, SecretHash: "h",
		Record: old.Encode()}, nil); err != nil {
		t.Fatal(err)
	}
	check("after writes")
	after, _ := s.tasksPayload(ctx, 0, 50)
	if after.Page.Finished != 401 || after.Page.Unfinished != 5 {
		t.Errorf("after writes the page is %+v, want 401 finished and 5 unfinished", after.Page)
	}
	seen := ""
	for _, row := range after.Tasks {
		if row.ID == old.ID {
			seen = row.Title
		}
	}
	if seen != "rewritten" {
		t.Errorf("the rewritten task reads %q; the rewrite behind the clock was not seen", seen)
	}
}
