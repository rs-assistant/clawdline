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
