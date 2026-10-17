package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/auth"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// Three readings, each saying what it is worth (work-system-review §5.2, W4).
//
// They were written for the console's "now" page, which was retired on
// 2026-09-26 when the v2 Board replaced it. The words stay because they are the
// wire's, not the page's: a reader that could not read a source must never be
// handed `0`, and it can only print the word the daemon gave it. Before this,
// the daemon had three words and all three answered "did the reading happen" —
// so *read in full and possibly already wrong* had no spelling at all and came
// out as `current`.
//
// Each test here has the control that makes it red (DG-8): the neighbouring
// case that must keep the ordinary word.

// A task list says the retired Swift app's store was not read, and that its
// own list is whole: `disabled` beside `current`. The retired store is no
// longer read at all (docs/cutover.md §7), so neither word may depend on
// what is under ~/.config/clawdline. `missing` is the deliberate absence: this
// route refuses rather than answering with an empty list.
func TestTheTaskListNamesTheRetiredStoreDisabledAndItsOwnListCurrent(t *testing.T) {
	p := &pane{s: session.Session{ID: "pane-1", Backend: session.BackendTmux, Assistant: session.AssistantClaude,
		State: session.StateIdle}}
	s := paneServer(t, p)
	list, err := s.tasksPayload(context.Background(), 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if list.Store != contract.StoreReadingDisabled {
		t.Errorf("store %q, want disabled", list.Store)
	}
	if list.Source.Freshness != contract.SourceFreshnessCurrent {
		t.Errorf("freshness %q, want current", list.Source.Freshness)
	}
	if list.Source.ObservedAt != list.At || list.Source.Provenance != "broker" {
		t.Errorf("source %+v does not name itself at the list's time %d", list.Source, list.At)
	}
}

// A machine with no task at all answers an empty list, not a missing one:
// the schema's `tasks` is an array, and the first page is copied out of the
// shared cache on its way to the wire.
func TestAnEmptyTaskListIsAnArray(t *testing.T) {
	p := &pane{s: session.Session{ID: "pane-1", Backend: session.BackendTmux, Assistant: session.AssistantClaude,
		State: session.StateIdle}}
	s := paneServer(t, p)
	for _, target := range []string{"/v1/orchestrator/tasks", "/v1/orchestrator/tasks?limit=10"} {
		rec := httptest.NewRecorder()
		s.tasksList(rec, httptest.NewRequest(http.MethodGet, target, nil))
		var body map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", target, rec.Code, rec.Body)
		}
		if string(body["tasks"]) != "[]" {
			t.Errorf("%s: tasks %s, want []", target, body["tasks"])
		}
	}
}

// The proposal and decision routes answer with that word on them.
//
// A field a reader is written against and the route does not send is found by
// something breaking on a phone, which is the worst place to find it.
func TestTheWaitingRoutesCarryTheirFreshness(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := &Server{store: st}
	person := access{verdict: auth.Verdict{Allowed: true, Local: true}}
	routes := map[string]http.HandlerFunc{
		"/v1/work/proposals": s.workProposalsRoute,
		"/v1/work/decisions": s.workDecisionsRoute,
	}
	for path, handler := range routes {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req = req.WithContext(context.WithValue(req.Context(), accessKey{}, person))
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
		}
		var body struct {
			Source *contract.BearingsSource `json:"source"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if body.Source == nil {
			t.Fatalf("%s says nothing about what its answer is worth: %s", path, rec.Body)
		}
		if body.Source.Freshness == "" {
			t.Fatalf("%s: an empty freshness is a fifth word: %s", path, rec.Body)
		}
		// An empty board with no sweep is not a board with nothing waiting.
		if !strings.Contains(rec.Body.String(), `"freshness":"unverified"`) {
			t.Fatalf("%s: a stopped clock reads as settled: %s", path, rec.Body)
		}
	}
}

// The landing ledger answers for itself, and no longer borrows the task
// records' word.
//
// This is §1.3's correction made into code. The coordinator was accused of
// ignoring a `stale` that was sitting in front of it; what it was actually
// doing was reporting, honestly, a state it had no word for — `Sources.Landings`
// was literally `src(b.TasksFresh, …)`, the task reading's word under the
// landing ledger's name. The records were readable, so it said `current`, and
// nineteen rows nobody could check were drawn as nineteen rows that had not
// landed yet.
func TestTheLandingLedgerAnswersForItself(t *testing.T) {
	pending := func(target string, settlement orchestrator.LandingSettlement) orchestrator.PendingLanding {
		return orchestrator.PendingLanding{Record: orchestrator.Record{
			Landing: &orchestrator.Landing{State: orchestrator.LandingPending, Target: target, Settlement: settlement},
		}}
	}
	checkable := pending("master", orchestrator.SettlementCarried)
	nameless := pending("", orchestrator.SettlementCarried)
	unreadable := pending("master", orchestrator.SettlementUnreadable)

	cases := []struct {
		name string
		rows []orchestrator.PendingLanding
		want contract.SourceFreshness
	}{
		// The control, and the reason this is not simply "always unverified":
		// a ledger whose every row names a target is a ledger that can be
		// checked, and saying otherwise would make the word mean nothing.
		{"nothing owed", nil, contract.SourceFreshnessCurrent},
		{"every row names a target", []orchestrator.PendingLanding{checkable, checkable},
			contract.SourceFreshnessCurrent},
		{"one row names none", []orchestrator.PendingLanding{checkable, nameless},
			contract.SourceFreshnessUnverified},
		{"one row's branch could not be read", []orchestrator.PendingLanding{checkable, unreadable},
			contract.SourceFreshnessUnverified},
	}
	for _, c := range cases {
		if got := landingsFreshness(c.rows); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// What the branch held reaches the ledger's own rows, not only a task's record.
//
// "Delivered, not recorded" is read from this route, and the difference
// between a row whose branch carries a delivery and one whose branch carried
// nothing is the difference between work waiting to be merged and work that was
// never committed. Those want opposite actions from the person reading them.
func TestTheLedgerRowSaysWhatItsBranchHeld(t *testing.T) {
	row := contract.BrokerPendingLanding{
		ID:         "t1",
		Settlement: contract.BrokerLandingSettlement(orchestrator.SettlementEmpty),
	}
	body, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"settlement":"branch_empty"`) {
		t.Fatalf("a ledger row that does not say what its branch held: %s", body)
	}
	// Nobody asked: a task with no branch of its own. Absent, never a value
	// that reads as an answer.
	bare, err := json.Marshal(contract.BrokerPendingLanding{ID: "t2"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(bare), "settlement") {
		t.Fatalf("a row nobody asked about still carries a settlement: %s", bare)
	}
}
