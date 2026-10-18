package cloud

// The task list a Cloud viewer draws the main and child sessions from.
//
// A hosted console groups a child session under the session that dispatched
// it, indents it, and puts the "child session · running" chip on the root —
// and every one of those reads `tasks` out of the machine's `orch/` snapshot
// (`view/derive.js`: `taskOfChild`, `taskShaping`, `tasksOfRoot`,
// `featureRootChip`, `lostIfClosed`; `session/order.ts`: `groupUnderRoots`,
// `visualDepths`). A machine whose snapshot
// carries no `tasks` is drawn as a flat list of sessions, whatever it has
// dispatched. This file is that list, ported from the Swift app's
// `RemoteServer.cloudOrchestratorProjection` (`OrchestratorPersistence.swift`)
// and docs/cloud.md §"The `orch/` snapshot a Cloud viewer is sent" in that
// repository. The hosted console is the Swift app's and is not changed here, so
// the keys and the shape are its own.
//
// **Records.** A task goes out while it is unfinished, or while its child
// terminal is a session this machine has published and not tombstoned. Every
// Cloud reader of a finished task reaches it through its child: the header and
// the row through `taskOfChild` of a listed session, and the chips, indents and
// grouping through `taskShaping`, which is false for a finished task whose
// child is not a listed session. A root's own chip counts its children through
// `taskShaping` too, so the root side keeps nothing. A record with no state, or
// one this build does not know, is kept: one task too many costs bytes, one
// dropped that a viewer still wanted is a row that silently loses its header.
//
// **Fields.** Of a record only the nine paths in cloudTaskFields. The Swift
// app's own list route started as a deny-list — it dropped `summary`,
// `review`, `graph` and `progress` — and everything else rode along: measured
// there on 2026-09-16, a connect snapshot was 202,105 bytes for 91 tasks, of
// which `completion_delivery` was 317 bytes a task, `landing` 276, `executor`
// 240, `verification` 167 and `worktree` 115. No viewer opens one of those. Its
// conclusion was an allow-list of what the viewer reads, checked reader by
// reader, and that is what is ported: a field this daemon adds to a task row
// later does not reach Cloud until somebody adds it here on purpose.
//
// **Bounds** (docs/design-guidelines.md DG-2), measured in tasklist_test.go
// through the real relay's sealing:
//
//   - The rows read are the page the local console's own stream reads: every
//     unfinished task and the newest taskReadLimit finished ones. A viewer is
//     therefore never shown a finished task the console on this machine does
//     not show. A finished task older than that page whose child tab is still
//     open draws on Cloud without its header, as it does locally.
//   - At most TaskListLimit records and TaskListBytesLimit bytes of them go
//     out. Past either, the oldest finished records are left out first and
//     then the oldest unfinished ones — an unfinished task is what the running
//     chip and "lost if closed" warning are drawn from, and a finished one is
//     only an indent. Nothing accumulates: each snapshot is recomputed whole
//     from the route, so being over a bound this pass costs nothing next pass.
//     The log says so when the number left out changes, not every pass.
//   - The normal case is far under both: the broker runs at most 20 tasks at
//     once on a machine, and a finished task is only kept while its child tab
//     is open. Measured through the relay's sealing: a projected record is
//     about 318 bytes with a typical title; 10 tasks add 3,181 bytes to the
//     snapshot (315 → 3,505 plaintext, 680 → 4,932 sealed), 100 add 31,891
//     (32,215 plaintext, 43,212 sealed), and 100 with 200-character titles
//     85,301 (114,424 sealed). The task route's own rows for those 100 are
//     92,209 bytes.
//
// **Cadence.** The list rides on the descriptor, which is compared without
// `at` and re-stated on the heartbeat, so it goes out when what a viewer reads
// in it changed — at most once a pass, five seconds — and every 240 seconds
// otherwise. That is the Swift bridge's rule (change-only, five-second floor,
// a presence re-send every three minutes) on this daemon's clock. An executor's
// `observed_at` or a record's `landing` moving is not a change here, because
// the projection does not carry them.
//
// **The list is read when something moved it, not every pass.** Going out only
// on a change was never the same as finding out only on a change: this file
// read `GET /v1/orchestrator/tasks` on every pass to see whether there was
// anything to send. Measured on the running daemon on 2026-10-10, that was 288
// of the 289 reads the route answered in 1,426 seconds — one every 4.95 s, on
// a machine where no task had changed at all — and every one of them went
// through the gate, the broker's ledger and the Swift store's page. The route
// is the page's too, and the page does not ask for it: a hosted console
// answers `/v1/orchestrator/tasks` from the descriptor it already holds
// (`web/console/src/cloud/relay-reader.ts`, `tasks()`), so a Cloud caller on
// that route is this file and nothing else.
//
// So a read is taken when one of four things is true, and otherwise the rows
// of the last read are projected again over the sessions a viewer now holds:
//
//   - nothing has been read yet — the publisher's first pass;
//   - this machine has written a task row since the last read (TaskListMoved,
//     the broker's `TaskListChanged`). A write, not a change a viewer would
//     notice: a note appended to a task moves no field this projection
//     carries, and one read too many is cheaper than a chip left on the wrong
//     row;
//   - this pass states everything again — a viewer that holds nothing has been
//     heard from, or the socket has just come up. A page that has just opened
//     and a line that has just come back are both that pass;
//   - the last read is TaskListRereadSecondsLimit old. This is the floor, not
//     the cadence: `root.terminalId` is resolved from the machine's own screen
//     rather than from the record (transport/http `ownTaskLinks`), and a
//     second writer on the same store file owes this process no signal at all,
//     so the list cannot be left to a signal that may never come. It is the
//     descriptor's own heartbeat, so the pass that re-reads is the pass that
//     re-states. What a viewer indents a child under does not wait for it:
//     each pass resolves a task's root against the rows it lists, from the
//     record's durable `root.sessionId` (resolveTaskRoots).
//
// The rows a viewer can reach also change when the session inventory changes
// with no task written — a finished task is drawn only while its child's row
// is listed — and that is the projection's half, not the read's: it is redone
// whenever the listed set differs from the one the kept rows were last
// projected over.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/sainteye/clawdline/internal/app/cloudops"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

const (
	// TaskListLimit is how many task records one `orch/` snapshot carries.
	TaskListLimit = 100
	// TaskListBytesLimit is how many bytes those records may take, encoded.
	// With the row limit reached and every title at the broker's 200-character
	// limit the list is 85,301 bytes (tasklist_test.go), so this binds only on
	// records whose titles were not admitted by this daemon.
	TaskListBytesLimit = 128 << 10
	// taskReadLimit is the finished tasks one pass reads: the page the local
	// console's stream reads (transport/http's tasksPayload(ctx, 0, 50)).
	taskReadLimit = 50
	// TaskListRereadSecondsLimit is how long this publisher may go without
	// reading its own task list when nothing has told it the list moved.
	//
	// It is the descriptor's heartbeat (Heartbeat), so the pass that re-reads
	// the list is the pass that re-states the descriptor anyway: a machine
	// nobody is dispatching on reads the route once every four minutes instead
	// of forty-eight times a minute. It is a floor and not a cadence — a task
	// this machine wrote is published on the next pass, as it always was — and
	// it is what keeps the list honest about the two changes nothing here is
	// told about: a row's `root.terminalId`, which the route resolves from the
	// machine's own screen, and a write by a second daemon on the same store.
	TaskListRereadSecondsLimit = 240
)

// cloudTaskFields are the task-record paths a Cloud viewer reads
// (`RemoteServer.cloudTaskFields`, checked there against every reader in the
// hosted console on 2026-09-15): `view/derive.js` reads `state`, `finishedAt`,
// `child.terminalId`, `created`, `root.terminalId`, `title` and `id`;
// `view/list.js` reads `id` and `title`; `view/transcript.js` reads `title`
// and `usage`'s `total` and `costUsd`.
var cloudTaskFields = [][]string{
	{"id"}, {"state"}, {"title"}, {"created"}, {"finishedAt"},
	{"child", "terminalId"}, {"root", "terminalId"},
	{"usage", "total"}, {"usage", "costUsd"},
}

// readTasks is this machine's task list as its own route answers it, or false
// when the route could not be read. **A list that could not be read is not an
// empty list** (DG-7): the caller keeps the last one it projected, because a
// snapshot without `tasks` tells a viewer this machine has none running.
func (p *Publisher) readTasks(ctx context.Context) ([]map[string]any, bool) {
	// Counted before the answer, because what the route is asked is the
	// measurement: a read that failed cost the same call.
	p.taskReads.Add(1)
	p.taskReadUnix.Store(p.now().Unix())
	res, err := p.Router.Do(ctx, cloudops.LocalRequest{
		Method: http.MethodGet, Path: "/v1/orchestrator/tasks",
		Query: map[string]string{"limit": strconv.Itoa(taskReadLimit)},
	})
	if err != nil || res.Status != http.StatusOK {
		p.logf("cloud: this machine's own task list could not be read: status=%d err=%v", res.Status, err)
		return nil, false
	}
	var root struct {
		Tasks []map[string]any `json:"tasks"`
	}
	// Numbers are copied as the route wrote them: a projection does not
	// round a timestamp or a token count on its way through.
	decoder := json.NewDecoder(bytes.NewReader(res.Body))
	decoder.UseNumber()
	if err := decoder.Decode(&root); err != nil {
		p.logf("cloud: this machine's own task list was unreadable: %v", err)
		return nil, false
	}
	return root.Tasks, true
}

// projectTasks is the list a viewer is sent: the records it can reach, each cut
// to the fields it reads, in the route's order (unfinished first, then
// finished, each newest first), bounded. It answers how many relevant records
// the bounds left out.
func projectTasks(rows []map[string]any, listed map[string]bool) ([]map[string]any, int) {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if cloudTaskRelevant(row, listed) {
			out = append(out, cloudTaskProjection(row))
		}
	}
	relevant := len(out)
	if len(out) > TaskListLimit {
		out = out[:TaskListLimit]
	}
	// The byte bound counts each record as it will be encoded plus the comma
	// between two, which is what the records add to the snapshot.
	size := 0
	for i, row := range out {
		size += len(mustJSON(row))
		if i > 0 {
			size++
		}
		if size > TaskListBytesLimit {
			out = out[:i]
			break
		}
	}
	return out, relevant - len(out)
}

// cloudTaskRelevant is `RemoteServer.cloudTaskRelevant`: kept while unfinished
// (or of a state this build does not know), otherwise only while its child
// terminal is a session a viewer holds a row for.
func cloudTaskRelevant(row map[string]any, listed map[string]bool) bool {
	state, _ := row["state"].(string)
	if !orchestrator.State(state).Terminal() {
		return true
	}
	child, _ := row["child"].(map[string]any)
	terminal, _ := child["terminalId"].(string)
	return terminal != "" && listed[terminal]
}

// cloudTaskProjection is `RemoteServer.cloudTaskProjection`: only
// cloudTaskFields. A path the record does not have stays absent — `root` with
// no resolved terminal is no `root` at all, and `usage.costUsd` is left out
// rather than zero for work nothing priced — and a value is copied as it is.
func cloudTaskProjection(row map[string]any) map[string]any {
	out := map[string]any{}
	for _, path := range cloudTaskFields {
		if len(path) == 1 {
			if value, ok := row[path[0]]; ok {
				out[path[0]] = value
			}
			continue
		}
		parent, ok := row[path[0]].(map[string]any)
		if !ok {
			continue
		}
		value, ok := parent[path[1]]
		if !ok {
			continue
		}
		nested, _ := out[path[0]].(map[string]any)
		if nested == nil {
			nested = map[string]any{}
			out[path[0]] = nested
		}
		nested[path[1]] = value
	}
	return out
}

// noteListed records which sessions a viewer now holds rows for, from one
// reading of this machine's sessions. A complete reading published the
// inventory, which tombstones every row it does not name, so it replaces the
// set. A partial one — or one past the inventory's limit — published rows and
// no inventory, so a viewer still holds every row it had: it adds to the set.
func (p *Publisher) noteListed(ids []string, authoritative bool) {
	if authoritative || p.listed == nil {
		p.listed = map[string]bool{}
	}
	for _, id := range ids {
		p.listed[id] = true
	}
}

// refreshTasks projects the task list for this pass, reading this machine's
// own list only when something could have moved it (the header's *Cadence*).
// A failed read keeps the last projection and stays due, so the next pass asks
// again rather than waiting out the floor. rows is this pass's reading of the
// sessions, or nil when the pass did not read them; each task's root is
// resolved against it (resolveTaskRoots).
func (p *Publisher) refreshTasks(ctx context.Context, rows []map[string]any) {
	p.taskPasses.Add(1)
	// Asked on every pass and not only when a read is otherwise due, because
	// taking the fact clears it: a write that happened while a read was
	// failing, or between two passes, must not be forgotten.
	if p.TaskListMoved != nil && p.TaskListMoved() {
		p.taskRowsStale = true
	}
	if p.taskReadDue() {
		if read, ok := p.readTasks(ctx); ok {
			p.taskRows, p.taskReadAt, p.taskRowsStale = read, p.now(), false
			// New rows: the kept projection is of other ones.
			p.taskListed = nil
		}
	}
	if rows != nil {
		p.resolveTaskRoots(sessionRowsByID(rows))
	}
	p.projectKeptTasks()
}

// sessionRowsByID is one reading's rows by their wire id.
func sessionRowsByID(sessions []map[string]any) map[string]map[string]any {
	rows := make(map[string]map[string]any, len(sessions))
	for _, row := range sessions {
		if id, _ := row["id"].(string); id != "" {
			rows[id] = row
		}
	}
	return rows
}

// Why a task's root was not found among the listed rows. Each is a sentence
// for daemon.log; none of them carries a conversation id or a title.
const (
	rootCauseOffScreen  = "the root's tab was not on screen when the task list was read, and no listed row holds its conversation"
	rootCauseNotListed  = "the root's tab %s is not a listed row, and no listed row holds its conversation"
	rootCauseAmbiguous  = "more than one listed row holds the root's conversation"
	rootCauseNoSession  = ""
	rootCauseIsTheChild = "the root's tab is the child's own"
)

// conversationOwners maps each conversation a listed row names to that row,
// or to "" when more than one row names it: an ambiguous conversation places
// nothing.
func conversationOwners(rows map[string]map[string]any) map[string]string {
	owners := make(map[string]string)
	for id, row := range rows {
		if conversation, _ := row["sessionId"].(string); conversation != "" {
			if _, exists := owners[conversation]; exists {
				owners[conversation] = ""
			} else {
				owners[conversation] = id
			}
		}
	}
	return owners
}

// taskRootIn is the listed row a task's root is in, or "" with the cause.
//
// The route's `root.terminalId` is the machine's screen as it stood when the
// list was read, and the list is read on a write and on a 240-second floor
// (the header's *Cadence*): between two reads it can name a tab that was off
// screen then and is listed now, or name nothing. The record's own
// `root.sessionId` is durable, so it is matched against the rows listed now,
// as the local Session list does (web/console/src/session/order.ts
// `visualParents`). A cause of rootCauseNoSession is a task with no session
// root at all — a scheduled run — which is not a missing parent.
func taskRootIn(task map[string]any, rows map[string]map[string]any, owners map[string]string) (string, string) {
	root, _ := task["root"].(map[string]any)
	terminal, _ := root["terminalId"].(string)
	conversation, _ := root["sessionId"].(string)
	if terminal != "" && rows[terminal] != nil {
		return terminal, ""
	}
	if conversation == "" && terminal == "" {
		return "", rootCauseNoSession
	}
	owner, named := owners[conversation]
	switch {
	case conversation != "" && owner != "":
		return owner, ""
	case conversation != "" && named:
		return "", rootCauseAmbiguous
	case terminal != "":
		return "", fmt.Sprintf(rootCauseNotListed, terminal)
	default:
		return "", rootCauseOffScreen
	}
}

// resolveTaskRoots finds each kept task's root among this pass's rows, and
// keeps the last one found while that tab is still listed.
//
// The keeping is for the pass where neither the read nor the rows can say: the
// root's tab was off screen at the read and its row has no conversation this
// pass. That root has not moved, and a child drawn flat for a pass and nested
// the next is the flicker this is for. A tab that is no longer listed is
// forgotten, so a closed root never keeps a child indented.
func (p *Publisher) resolveTaskRoots(rows map[string]map[string]any) {
	owners := conversationOwners(rows)
	roots := make(map[string]string, len(p.taskRows))
	for _, task := range p.taskRows {
		id, _ := task["id"].(string)
		if id == "" {
			continue
		}
		owner, _ := taskRootIn(task, rows, owners)
		if owner == "" {
			if kept := p.taskRoots[id]; kept != "" && rows[kept] != nil {
				owner = kept
			}
		}
		if owner != "" {
			roots[id] = owner
		}
	}
	p.taskRoots = roots
}

// taskReadDue is whether this pass reads this machine's own task list.
func (p *Publisher) taskReadDue() bool {
	switch {
	case p.taskRows == nil || p.taskReadAt.IsZero():
		return true
	case p.taskRowsStale:
		return true
	case p.restating():
		return true
	default:
		return p.now().Sub(p.taskReadAt) >= TaskListRereadSecondsLimit*time.Second
	}
}

// restating is whether this pass will state everything this machine has again.
//
// The pass clears every memory of what it last published when a viewer that
// holds nothing has been heard from, and `Run` starts with those memories
// empty on a new socket (publish.go). So the descriptor having no entry in
// that memory is the one fact in hand that says "this pass is a first
// statement" — a page that has just opened, a relay eviction, a reconnect —
// and it is also true after a descriptor that did not leave, which is a pass
// that owes the same read.
func (p *Publisher) restating() bool {
	_, stated := p.published[descriptorKey]
	return !stated
}

// projectKeptTasks projects the rows of the last read over the sessions a
// viewer now holds rows for, and leaves the projection alone when neither has
// changed. A changed number of records left out is logged.
//
// This is the half that has to run after a read it did not take: a finished
// task is carried only while its child's row is listed (cloudTaskRelevant), so
// a tab closing takes it off the list with nothing written and nothing read.
func (p *Publisher) projectKeptTasks() {
	if p.taskRows == nil {
		return
	}
	if p.taskListed != nil && sameListed(p.taskListed, p.listed) && sameRoots(p.taskRootsProjected, p.taskRoots) {
		return
	}
	projected, omitted := projectTasks(p.taskRows, p.listed)
	if omitted != p.omitted {
		if omitted > 0 {
			p.logf("cloud: the orch snapshot carries %d task records and leaves out the %d oldest, past %d records or %d bytes",
				len(projected), omitted, TaskListLimit, TaskListBytesLimit)
		} else {
			p.logf("cloud: the orch snapshot carries every task record a viewer can reach again (%d)", len(projected))
		}
		p.omitted = omitted
	}
	// The root a viewer indents a child under is the one resolved against
	// the rows it holds, not the screen as it stood at the read.
	for _, row := range projected {
		id, _ := row["id"].(string)
		owner := p.taskRoots[id]
		if owner == "" {
			continue
		}
		root, _ := row["root"].(map[string]any)
		if root == nil {
			root = map[string]any{}
			row["root"] = root
		}
		root["terminalId"] = owner
	}
	p.tasks = projected
	p.taskListed = make(map[string]bool, len(p.listed))
	for id := range p.listed {
		p.taskListed[id] = true
	}
	p.taskRootsProjected = p.taskRoots
}

// sameRoots is whether two resolutions place every task alike.
func sameRoots(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for id, owner := range a {
		if b[id] != owner {
			return false
		}
	}
	return true
}

// sameListed is whether two readings name the same sessions.
func sameListed(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for id := range a {
		if !b[id] {
			return false
		}
	}
	return true
}

// TaskListReading is the capacity register's `cloud.task_list_reread_seconds`
// row as this publisher can answer it: how long ago it last asked for its own
// task list, against the floor, and what the asking has cost — reads taken
// over passes made since this line came up. On the running daemon before this
// change those two numbers were equal; a machine nobody is dispatching on now
// reads once per floor.
//
// It is read from the diagnostics route's goroutine, so it touches only the
// three atomics and never the pass's own fields.
func (p *Publisher) TaskListReading() capacity.Reading {
	reads, passes := p.taskReads.Load(), p.taskPasses.Load()
	cost := fmt.Sprintf("%d read(s) of this machine's own task list in %d pass(es)", reads, passes)
	at := p.taskReadUnix.Load()
	if at == 0 {
		return capacity.Reading{Known: true, Note: "the task list has not been read yet; " + cost}
	}
	read := time.Unix(at, 0)
	return capacity.Reading{Known: true, Used: int64(p.now().Sub(read).Seconds()),
		OldestAt: read, Note: cost}
}
