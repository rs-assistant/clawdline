package http

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/adapters/swiftstore"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// tasksRoute reads the tasks this daemon knows about, or accepts a new one.
// A new one goes to the broker (orchestrator.go), which is the one way work is
// dispatched on this daemon — a schedule's run included (D07).
func (s *Server) tasksRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.brokerDispatch(w, r)
		return
	}
	s.tasksList(w, r)
}

// tasksList publishes dispatched work in the Swift app's shape
// (OrchestratorTaskList.swift): every unfinished task, then one page of
// finished ones, newest first. The rows are the Swift app's own, read from its
// store, joined by the tasks this daemon dispatched; an id appears once.
//
// The copied console needs the finished ones too: a child whose task ended
// still sits under its root while its tab is open, and the detail header names
// the task that opened a session long after it finished.
func (s *Server) tasksList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	cursor, _ := strconv.Atoi(q.Get("cursor"))
	limit := 50
	if v, err := strconv.Atoi(q.Get("limit")); err == nil {
		limit = v
	}
	var list contract.TaskList
	var err error
	if cursor == 0 && limit == 50 {
		// The first page is the product every stream's `orchestrator` frame
		// reads, when it is not older than one tick (producer.go). The Cloud
		// publisher asks for exactly this page, which is why the cache is
		// shared with it: it used to ask on every five-second pass — 288 of
		// the 289 reads this route answered in 1,426 seconds on the running
		// daemon on 2026-10-10 — and now asks when a task row has been
		// written, when it states everything again, or on the
		// `cloud.task_list_reread_seconds` floor
		// (internal/transport/cloud tasklist.go).
		list, err = s.lists().taskList()
		// A copy, because the cached page is shared; never nil, because an
		// empty list is `[]` on the wire, not `null`.
		list.Tasks = append(make([]contract.TaskRow, 0, len(list.Tasks)), list.Tasks...)
	} else {
		list, err = s.tasksPayload(r.Context(), cursor, limit)
	}
	if err != nil {
		writeRawRefusal(w, http.StatusInternalServerError, "store_unreadable", err.Error())
		return
	}
	if state := q.Get("state"); state != "" {
		kept := list.Tasks[:0]
		for _, row := range list.Tasks {
			if string(row.State) == state {
				kept = append(kept, row)
			}
		}
		list.Tasks = kept
	}
	writeJSON(w, list)
}

// tasksPayload builds the task list the route and the stream's `orchestrator`
// frame both publish.
func (s *Server) tasksPayload(ctx context.Context, cursor, limit int) (contract.TaskList, error) {
	// The broker's own tasks, which live in this daemon's store rather than in
	// the Swift app's. Without this the console would show a child this daemon
	// dispatched only while the Swift app also happened to know about it, which
	// it never does.
	//
	// A broker that could not be read is an error, not an empty section: the
	// list used to drop the whole section silently, which on a phone reads as
	// "your children are gone". And a row it could not decode is listed as
	// `unreadable` — unfinished, so it rides on every page — never skipped
	// (D05 ②).
	//
	// The page is cut from a window, not from the whole history: every task
	// not finished, every unreadable row, and the newest finished tasks up to
	// the end of this page. Thousands of tasks that finished before them are
	// counted, not built into rows that the page would only throw away.
	start := max(cursor, 0)
	window, err := s.broker.TaskWindow(ctx, start+pageWindow(limit))
	if err != nil {
		return contract.TaskList{}, err
	}
	own := s.ownTaskRows(window.Unreadable, append(window.Live, window.Finished...), s.screen(ctx))
	rows, page := swiftstore.TaskPage(own, cursor, limit)
	// TaskPage counted only the finished rows it was given; the page is of
	// all of them.
	end := int(page.Cursor) + len(rows) - int(page.Unfinished)
	page.Finished, page.NextCursor = int64(window.FinishedTotal), 0
	if end < window.FinishedTotal {
		page.NextCursor = int64(end)
	}
	now := time.Now()
	return contract.TaskList{
		At:    now.Unix(),
		Tasks: rows,
		Page:  page,
		// The retired Swift app's store is no longer read (docs/cutover.md
		// §7). `disabled` is the word the contract has for "not read, and
		// known to contribute nothing": the list is this daemon's own tasks,
		// whole, so its source is current.
		Store:  contract.StoreReadingDisabled,
		Source: contract.BearingsSource{ObservedAt: now.Unix(), Provenance: "broker", Freshness: contract.SourceFreshnessCurrent},
	}, nil
}

// ownTaskRows is the broker's tasks as the list's rows: every unreadable row,
// then the records, in the order given.
func (s *Server) ownTaskRows(unreadable []orchestrator.Unreadable, records []orchestrator.Record, screen []swiftstore.OnScreen) []contract.TaskRow {
	own := []contract.TaskRow{}
	for _, u := range unreadable {
		created := u.CreatedAt.Unix()
		own = append(own, contract.TaskRow{
			ID:         u.ID,
			TaskID:     u.ID,
			Assistant:  contract.Assistant(u.Assistant),
			ProjectDir: u.Project,
			Claims:     []string{},
			State:      contract.TaskState(orchestrator.StateUnreadable),
			Created:    created,
			CreatedAt:  created,
			Dir:        s.broker.Tasks.Path(u.ID),
		})
	}
	for _, t := range records {
		claims := t.Lease()
		created := t.CreatedAt.Unix()
		row := contract.TaskRow{
			ID:             t.ID,
			TaskID:         t.ID,
			Assistant:      contract.Assistant(t.Assistant),
			ProjectDir:     t.ProjectDir,
			Claims:         claims,
			ClaimsDeclared: t.Claims != nil,
			State:          contract.TaskState(t.State),
			Created:        created,
			CreatedAt:      created,
			Dir:            t.Dir,
			Title:          t.Title,
			Kind:           t.Kind,
			CallbackIntent: callbackIntentOf(t),
			ScheduleID:     t.ScheduleID,
			Persona:        t.Persona,
		}
		if !t.FinishedAt.IsZero() {
			row.FinishedAt = t.FinishedAt.Unix()
		}
		ownTaskLinks(&row, t, screen)
		own = append(own, row)
	}
	return own
}

// pageWindow is at least as many finished rows as TaskPage puts on one page
// of limit: it gives 50 for a limit it does not take, and never more than 500.
func pageWindow(limit int) int {
	if limit <= 0 || limit > 500 {
		return 500
	}
	return limit
}

func callbackIntentOf(t orchestrator.Record) string {
	if t.Callback == nil {
		return ""
	}
	return t.Callback.Intent
}

// screen is the sessions on screen, for resolving which tab a task's root is
// in. The session list keeps the last one it built; a task list asked for when
// nobody has read the sessions lately reads the machine itself.
func (s *Server) screen(ctx context.Context) []swiftstore.OnScreen {
	if held := s.lastScreen.Load(); held != nil && time.Since(held.at) < screenMaxAge {
		return held.rows
	}
	inv := s.reading(ctx)
	rows := onScreen(inv.Sessions)
	s.lastScreen.Store(&screenReading{at: time.Now(), rows: rows})
	return rows
}

const screenMaxAge = 10 * time.Second

type screenReading struct {
	at   time.Time
	rows []swiftstore.OnScreen
}

func onScreen(items []session.Session) []swiftstore.OnScreen {
	out := make([]swiftstore.OnScreen, 0, len(items))
	for _, item := range items {
		if !item.IsAssistant() {
			continue
		}
		// A retained row is for drawing only. Stamping it into lastScreen as
		// just seen would let task placement turn an earlier observation into
		// a present-tense identity binding.
		if item.Observation.Freshness == session.FreshnessUnverified || item.Observation.Freshness == session.FreshnessMissing {
			continue
		}
		out = append(out, swiftstore.OnScreen{
			TerminalID:     item.ID,
			Assistant:      string(item.Assistant),
			ConversationID: item.ConversationID,
		})
	}
	return out
}

// strings answers with the localisation catalog, from the console bundle.
//
// The catalog ships beside the console rather than inside this binary because
// it belongs to the screen. Local and hosted consoles use the same catalogs
// under web/console/public/catalogs, including the metadata and partial
// secondary-language policy.
//
// An unavailable English catalog answers a typed 503. Invalid selected
// catalogs answer with the complete English catalog and its actual language.
//
// This is the one route with no generated type, because its keys are the
// catalog's own and a schema listing them would be the catalog.
func (s *Server) strings(w http.ResponseWriter, r *http.Request) {
	values, present := r.URL.Query()["lang"]
	lang := ""
	if present {
		if len(values) != 1 {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "that is not a language tag")
			return
		}
		lang = values[0]
	}
	if !present {
		lang = defaultCatalog
	}
	if !validCatalogTag(lang) {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "that is not a language tag")
		return
	}
	catalog, err := loadCatalog(WebRoot(), nextconfig.ResolveProductLanguage(lang))
	if err != nil {
		writeRefusal(w, http.StatusServiceUnavailable, "catalog_unreadable", "English console catalog is unavailable")
		return
	}
	writeJSON(w, catalog)
}
