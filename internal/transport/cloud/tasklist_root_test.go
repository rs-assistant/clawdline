package cloud

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// rootedSessionsBody lists a root and one child. The root's row names its
// conversation only when bound is true: a session row has no `sessionId` for
// the passes where its identity binding is not settled.
func rootedSessionsBody(bound bool) string {
	root := map[string]any{"id": "%8", "assistant": "claude", "tty": "ttys-8", "state": "working"}
	if bound {
		root["sessionId"] = "root-conversation"
	}
	child := map[string]any{"id": "%64", "assistant": "claude", "tty": "ttys-64", "state": "working"}
	body, _ := json.Marshal(map[string]any{"at": 17, "scan": map[string]any{"complete": true},
		"sessions": []map[string]any{root, child}})
	return string(body)
}

// rootedTask is a running child of %8. The route resolves `root.terminalId`
// from the machine's screen at read time; an empty one is a read taken while
// the root's tab was not on screen.
func rootedTask(rootTerminal string) map[string]any {
	return map[string]any{"id": taskID(64), "state": "running", "title": "t", "created": 1,
		"child": map[string]any{"terminalId": "%64"},
		"root":  map[string]any{"sessionId": "root-conversation", "terminalId": rootTerminal}}
}

// **A child stays under its root between two task reads.** The task list is
// read on a write and on the 240-second floor, so whatever the read saw of the
// root's tab is what every pass until the next read projects. A read taken
// while the root was not on screen used to leave the child flat on both
// sources a hosted console nests by — the descriptor's `tasks` for the whole
// window, and ss/ `parent_session_id` for every pass where the root's row also
// had no conversation — and the phone drew it nested or flat depending on
// which source and which pass it was reading.
func TestAChildStaysUnderItsRootBetweenTwoTaskReads(t *testing.T) {
	router := newPathRouter(rootedSessionsBody(true), tasksBody(rootedTask("%8")))
	out := &collector{}
	publisher, wrote := newTaskPublisher(router, out)
	clock := time.Unix(1_789_800_000, 0)
	publisher.Now = func() time.Time { return clock }
	var logged []string
	publisher.Log = func(format string, args ...any) { logged = append(logged, fmt.Sprintf(format, args...)) }

	parentOnStatus := func() any { return out.payload(t, "ss/mac-01/%2564")["parent_session_id"] }
	rootOnDescriptor := func() any {
		tasks, _ := out.payload(t, "orch/mac-01")["tasks"].([]any)
		if len(tasks) != 1 {
			return nil
		}
		root, _ := tasks[0].(map[string]any)["root"].(map[string]any)
		return root["terminalId"]
	}

	publisher.firstPass(context.Background())
	if parentOnStatus() != "%8" || rootOnDescriptor() != "%8" {
		t.Fatalf("the first pass did not nest the child: status=%v descriptor=%v", parentOnStatus(), rootOnDescriptor())
	}

	// A task write is read while the root's tab is off screen and its row has
	// no conversation, and the next passes run without a write.
	router.set("/v1/orchestrator/tasks", tasksBody(rootedTask("")), 0)
	router.set("/v1/sessions", rootedSessionsBody(false), 0)
	wrote()
	for i := 0; i < 47; i++ {
		clock = clock.Add(SnapshotInterval)
		publisher.Pass(context.Background())
		if i == 3 {
			// The root's row names its conversation again.
			router.set("/v1/sessions", rootedSessionsBody(true), 0)
		}
		if got := parentOnStatus(); got != "%8" {
			t.Fatalf("pass %d (%s after the read) published the child with parent %v, want %%8",
				i+1, time.Duration(i+1)*SnapshotInterval, got)
		}
		if got := rootOnDescriptor(); got != "%8" {
			t.Fatalf("pass %d (%s after the read) put the descriptor's task under root %v, want %%8",
				i+1, time.Duration(i+1)*SnapshotInterval, got)
		}
	}
	if reads := router.reads("/v1/orchestrator/tasks"); reads != 2 {
		t.Errorf("the task list was read %d times; keeping the parent must not add reads", reads)
	}
	for _, line := range logged {
		if strings.Contains(line, "has no listed parent") {
			t.Errorf("a parent that was resolved was logged as missing: %s", line)
		}
	}
}

// **A child whose parent cannot be resolved says so once, with the cause.**
// The next time a phone draws a dispatched child flat, daemon.log names the
// child and why, one line per change rather than one a pass.
func TestAChildWithNoListedParentIsLoggedOncePerCause(t *testing.T) {
	router := newPathRouter(rootedSessionsBody(false), tasksBody(rootedTask("")))
	out := &collector{}
	publisher := newPublisher(router, out)
	clock := time.Unix(1_789_800_000, 0)
	publisher.Now = func() time.Time { return clock }
	var logged []string
	publisher.Log = func(format string, args ...any) { logged = append(logged, fmt.Sprintf(format, args...)) }
	count := func(fragment string) int {
		n := 0
		for _, line := range logged {
			if strings.Contains(line, fragment) {
				n++
			}
		}
		return n
	}

	publisher.firstPass(context.Background())
	for i := 0; i < 5; i++ {
		clock = clock.Add(SnapshotInterval)
		publisher.Pass(context.Background())
	}
	if _, ok := out.payload(t, "ss/mac-01/%2564")["parent_session_id"]; ok {
		t.Fatal("an unresolvable root invented a parent")
	}
	missing := "cloud: child %64 has no listed parent: the root's tab was not on screen when the task list was read, and no listed row holds its conversation"
	if got := count(missing); got != 1 {
		t.Fatalf("the missing parent was logged %d times, want once; log: %q", got, logged)
	}

	// Two rows hold the root's conversation: a different cause, a new line.
	ambiguous := strings.Replace(rootedSessionsBody(true), `"sessions":[`,
		`"sessions":[{"id":"%9","assistant":"claude","tty":"ttys-9","sessionId":"root-conversation"},`, 1)
	router.set("/v1/sessions", ambiguous, 0)
	clock = clock.Add(SnapshotInterval)
	publisher.Pass(context.Background())
	clock = clock.Add(SnapshotInterval)
	publisher.Pass(context.Background())
	if got := count("cloud: child %64 has no listed parent: more than one listed row holds the root's conversation"); got != 1 {
		t.Fatalf("the ambiguous root was logged %d times, want once; log: %q", got, logged)
	}

	// The root resolves: one line says the child is nested again.
	router.set("/v1/sessions", rootedSessionsBody(true), 0)
	clock = clock.Add(SnapshotInterval)
	publisher.Pass(context.Background())
	clock = clock.Add(SnapshotInterval)
	publisher.Pass(context.Background())
	if out.payload(t, "ss/mac-01/%2564")["parent_session_id"] != "%8" {
		t.Fatal("the resolved root was not published")
	}
	if got := count("cloud: child %64 is listed under %8 again"); got != 1 {
		t.Fatalf("the recovery was logged %d times, want once; log: %q", got, logged)
	}
}

// carriedSessionsBody is one reading of the given rows; complete says
// whether its scan covered every source.
func carriedSessionsBody(complete bool, rows ...map[string]any) string {
	body, _ := json.Marshal(map[string]any{"at": 17, "scan": map[string]any{"complete": complete}, "sessions": rows})
	return string(body)
}

// carriedRow is a listed session; conversation is its `sessionId`, if bound.
func carriedRow(id, conversation string) map[string]any {
	row := map[string]any{"id": id, "assistant": "claude", "tty": "ttys-" + id[1:], "state": "working"}
	if conversation != "" {
		row["sessionId"] = conversation
	}
	return row
}

// carriedRoot is a publisher whose first pass nested %64 under %8, with the
// clock, the write signal and one step of SnapshotInterval.
type carriedRoot struct {
	router *pathRouter
	out    *collector
	pub    *Publisher
	wrote  func()
	step   func()
}

func newCarriedRoot(t *testing.T, sessions, tasks string) carriedRoot {
	t.Helper()
	router := newPathRouter(sessions, tasks)
	out := &collector{}
	publisher, wrote := newTaskPublisher(router, out)
	clock := time.Unix(1_789_800_000, 0)
	publisher.Now = func() time.Time { return clock }
	publisher.firstPass(context.Background())
	return carriedRoot{router: router, out: out, pub: publisher, wrote: wrote, step: func() {
		clock = clock.Add(SnapshotInterval)
		publisher.Pass(context.Background())
	}}
}

// nested is what each source a hosted console nests by says of %64: ss/
// parent_session_id, and the descriptor task's root.terminalId, nil for none.
func (c carriedRoot) nested(t *testing.T) (any, any) {
	t.Helper()
	parent := c.out.payload(t, "ss/mac-01/%2564")["parent_session_id"]
	tasks, _ := c.out.payload(t, "orch/mac-01")["tasks"].([]any)
	if len(tasks) != 1 {
		t.Fatalf("the descriptor carries %d tasks, want 1", len(tasks))
	}
	root, _ := tasks[0].(map[string]any)["root"].(map[string]any)
	// The route sends an unresolved root as an empty terminal id, which a
	// viewer nests under nothing.
	if terminal := root["terminalId"]; terminal != "" {
		return parent, terminal
	}
	return parent, nil
}

// **A reused tab does not keep a child under another conversation.** The root
// was carried from a pass where its row named the task's conversation; its tab
// closed and tmux handed %8 to a different conversation within one pass. The
// carried root names a terminal, not a conversation, so it is dropped once the
// row it names holds a conversation that is not the task's.
func TestAReusedRootTabDoesNotKeepTheChild(t *testing.T) {
	c := newCarriedRoot(t, rootedSessionsBody(true), tasksBody(rootedTask("%8")))
	c.router.set("/v1/orchestrator/tasks", tasksBody(rootedTask("")), 0)
	c.wrote()
	c.step()
	if parent, root := c.nested(t); parent != "%8" || root != "%8" {
		t.Fatalf("the root resolved by conversation was not published: status=%v descriptor=%v", parent, root)
	}
	c.router.set("/v1/sessions", carriedSessionsBody(true, carriedRow("%8", "a-different-conversation"), carriedRow("%64", "")), 0)
	c.step()
	if parent, root := c.nested(t); parent != nil || root != nil {
		t.Fatalf("%%8 now holds another conversation, and the child stayed under it: status=%v descriptor=%v", parent, root)
	}
}

// **A partial scan does not forget the carried root.** A reading of only some
// sources says nothing about a row it did not reach — the viewer keeps that
// row — so the root it did not list is not gone. Forgetting it there drew the
// child flat on the next whole pass whose root row had no conversation.
func TestAPartialScanKeepsTheCarriedRoot(t *testing.T) {
	c := newCarriedRoot(t, rootedSessionsBody(true), tasksBody(rootedTask("%8")))
	c.router.set("/v1/orchestrator/tasks", tasksBody(rootedTask("")), 0)
	c.router.set("/v1/sessions", rootedSessionsBody(false), 0)
	c.wrote()
	c.step()
	if parent, root := c.nested(t); parent != "%8" || root != "%8" {
		t.Fatalf("after the read: status=%v descriptor=%v, want %%8 on both", parent, root)
	}
	c.router.set("/v1/sessions", carriedSessionsBody(false, carriedRow("%64", "")), 0)
	c.step()
	if parent, root := c.nested(t); parent != "%8" || root != "%8" {
		t.Fatalf("a partial scan without the root: status=%v descriptor=%v, want %%8 on both", parent, root)
	}
	c.router.set("/v1/sessions", rootedSessionsBody(false), 0)
	c.step()
	if parent, root := c.nested(t); parent != "%8" || root != "%8" {
		t.Fatalf("the whole scan after it: status=%v descriptor=%v, want %%8 on both", parent, root)
	}
}

// **A closed root never keeps a child indented.** A whole reading that does
// not list the carried root's tab forgets it, on both sources.
func TestAClosedRootDoesNotKeepTheChildIndented(t *testing.T) {
	c := newCarriedRoot(t, rootedSessionsBody(true), tasksBody(rootedTask("%8")))
	c.router.set("/v1/orchestrator/tasks", tasksBody(rootedTask("")), 0)
	c.router.set("/v1/sessions", rootedSessionsBody(false), 0)
	c.wrote()
	c.step()
	if parent, root := c.nested(t); parent != "%8" || root != "%8" {
		t.Fatalf("after the read: status=%v descriptor=%v, want %%8 on both", parent, root)
	}
	c.router.set("/v1/sessions", carriedSessionsBody(true, carriedRow("%64", "")), 0)
	c.step()
	if parent, root := c.nested(t); parent != nil || root != nil {
		t.Fatalf("the root's tab closed and the child stayed under it: status=%v descriptor=%v", parent, root)
	}
}

// **A root found with no read and no change to the listed set is published.**
// The task was read while the root was off screen and nothing carried it; the
// root's row later names its conversation. The listed set is the same and the
// task list is not read again, so only the change of resolution moves the
// descriptor.
func TestARootResolvedWithoutAReadIsRepublished(t *testing.T) {
	c := newCarriedRoot(t, rootedSessionsBody(false), tasksBody(rootedTask("")))
	if parent, root := c.nested(t); parent != nil || root != nil {
		t.Fatalf("an unresolvable root was published: status=%v descriptor=%v", parent, root)
	}
	c.router.set("/v1/sessions", rootedSessionsBody(true), 0)
	c.step()
	if parent, root := c.nested(t); parent != "%8" || root != "%8" {
		t.Fatalf("the root's row named its conversation: status=%v descriptor=%v, want %%8 on both", parent, root)
	}
	if reads := c.router.reads("/v1/orchestrator/tasks"); reads != 1 {
		t.Fatalf("the task list was read %d times; the case is a resolution with no read", reads)
	}
}

// **The resolved root is inside the byte bound.** A record the route sent
// without a root gains one when it is resolved against the listed rows, so
// the bound is measured on the record as it will be sent.
func TestAResolvedRootIsCountedInTheByteBound(t *testing.T) {
	rows := []map[string]any{
		{"id": taskID(0), "state": "briefed", "title": "a", "created": 2},
		{"id": taskID(1), "state": "briefed", "title": "", "created": 1},
	}
	measured, _ := projectTasks(rows, nil, nil)
	// Pad the second record until the two fill the bound exactly.
	rows[1]["title"] = strings.Repeat("x", TaskListBytesLimit-(len(mustJSON(measured))-2))
	if filled, omitted := projectTasks(rows, nil, nil); omitted != 0 || len(mustJSON(filled))-2 != TaskListBytesLimit {
		t.Fatalf("the fixture does not fill the bound: %d bytes, %d left out", len(mustJSON(filled))-2, omitted)
	}
	publisher := newPublisher(newPathRouter("", ""), &collector{})
	publisher.taskRows = rows
	publisher.taskRoots = map[string]string{taskID(0): "%8", taskID(1): "%8"}
	publisher.projectKeptTasks()
	if size := len(mustJSON(publisher.tasks)) - 2; size > TaskListBytesLimit {
		t.Fatalf("the records take %d bytes with their resolved roots; the bound is %d", size, TaskListBytesLimit)
	}
}
