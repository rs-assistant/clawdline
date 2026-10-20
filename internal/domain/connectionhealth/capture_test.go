package connectionhealth

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testWriterLock = "test.writer_lock"

var writerLockLine atomic.Int64

// waitOnWriterLock is a real goroutine stuck on a writer lock, the way a
// socket writer waits behind a held Conn lock.
//
//go:noinline
func waitOnWriterLock(mu *sync.Mutex, entered chan<- struct{}) {
	_, _, line, _ := runtime.Caller(0)
	writerLockLine.Store(int64(line + 3))
	close(entered)
	mu.Lock()
	mu.Unlock()
}

func funcName(f any) string {
	return runtime.FuncForPC(reflect.ValueOf(f).Pointer()).Name()
}

// captureRuntime is a runtime on a fake clock whose location map also
// names the test's writer-lock function, with capture on and its worker
// running until the test ends.
func captureRuntime(t *testing.T) (*fakeClock, *Runtime) {
	t.Helper()
	c := newFakeClock()
	rt, err := newRuntime(c, "boot-c", "abc123", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	rt.Capture.locations = func() []LocationEntry {
		return append(Locations(), LocationEntry{Code: testWriterLock, Function: funcName(waitOnWriterLock),
			Stages: []Stage{StageWriterLockWaiting}, Kind: Finite})
	}
	rt.Capture.SetEnabled(true)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go rt.Capture.Run(ctx)
	return c, rt
}

// stall scans a phase at location past its block deadline: one stall.
func stall(t *testing.T, c *fakeClock, rt *Runtime, location string) Handle {
	t.Helper()
	h := rt.Tracker.Start(DaemonSpec(StageWriterLockWaiting, ClassTransport, location, "trace-cap"))
	if !h.Recorded() {
		t.Fatal("the phase was not recorded")
	}
	rt.Watchdog.Scan()
	for i := 0; i < 16; i++ {
		c.advance(time.Second)
		rt.Watchdog.Scan()
	}
	return h
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func journaled(t *testing.T, rt *Runtime) map[Event]int {
	t.Helper()
	rt.Watchdog.Flush()
	p, err := rt.Journal.Read(Query{})
	if err != nil {
		t.Fatal(err)
	}
	n := map[Event]int{}
	for _, r := range p.Records {
		n[r.Event]++
	}
	return n
}

// A goroutine really blocked on a writer lock is captured from the real
// runtime, joined to the location map's function, and mapped to the exact
// line of this build; the stall's recovery is joined in the export.
func TestCaptureJoinsARealBlockedWriterLockToItsExactSource(t *testing.T) {
	c, rt := captureRuntime(t)
	var mu sync.Mutex
	mu.Lock()
	entered := make(chan struct{})
	go waitOnWriterLock(&mu, entered)
	<-entered
	waitFor(t, "the writer to block", func() bool {
		buf := make([]byte, 1<<16)
		return strings.Contains(string(buf[:runtime.Stack(buf, true)]), "waitOnWriterLock")
	})
	h := stall(t, c, rt, testWriterLock)
	waitFor(t, "the capture", func() bool { _, ok := rt.Capture.Latest(); return ok })
	got, _ := rt.Capture.Latest()

	want := "internal/domain/connectionhealth/capture_test.go:" + strconv.FormatInt(writerLockLine.Load(), 10)
	if !got.Join.Mapped || got.Join.Function != funcName(waitOnWriterLock) || got.Join.Goroutines < 1 {
		t.Fatalf("the join did not find the blocked writer: %+v", got.Join)
	}
	if len(got.Join.Sources) != 1 || got.Join.Sources[0] != want {
		t.Fatalf("join sources = %v, want [%s]", got.Join.Sources, want)
	}
	if !got.Goroutines[0].MatchesLocation || !strings.Contains(got.Goroutines[0].State, "sync.Mutex.Lock") {
		t.Fatalf("the matching goroutine is not first or not waiting on the lock: %+v", got.Goroutines[0])
	}
	if got.Trigger.Location != testWriterLock || got.Trigger.Correlation != "trace-cap" || got.Build != "abc123" || got.BootID != "boot-c" {
		t.Fatalf("trigger or build not carried: %+v", got.Trigger)
	}

	ev, err := rt.Export(Query{})
	if err != nil {
		t.Fatal(err)
	}
	if ev.StallCapture == nil || ev.StallCapture.Recovery == nil || ev.StallCapture.Recovery.State != RecoveryStillBlocked {
		t.Fatalf("before release the recovery is not still_blocked: %+v", ev.StallCapture)
	}
	mu.Unlock()
	if err := h.Progress(StageSocketWriteStarted); err != nil {
		t.Fatal(err)
	}
	c.advance(time.Second)
	rt.Watchdog.Scan()
	rt.Watchdog.Flush()
	ev, err = rt.Export(Query{})
	if err != nil {
		t.Fatal(err)
	}
	r := ev.StallCapture.Recovery
	if r.State != RecoveryRecovered || r.RecoveryNS == nil || *r.RecoveryNS != int64(2*time.Second) || r.JournalQuality != QualityComplete {
		t.Fatalf("recovery not joined: %+v", r)
	}

	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	home, _ := os.UserHomeDir()
	for _, leak := range []string{rt.Capture.roots.module, rt.Capture.roots.goroot, "+0x", "(0x", "0xc0"} {
		if leak != "" && strings.Contains(text, leak) {
			t.Errorf("the export carries %q", leak)
		}
	}
	if home != "" && strings.Contains(text, home) {
		t.Error("the export carries the home directory")
	}
}

// The raw dump the sanitizer is given here holds a sentinel in every place
// a secret could ride: a path, an argument, an unlisted module, a goroutine
// label, a header and free text. None of it comes out.
func TestSanitizerDropsEverySentinel(t *testing.T) {
	const s = "SENTINEL7f3a"
	roots := sourceRoots{module: "/Users/" + s + "/code/clawdline/", modulePath: modulePath,
		goroot: "/usr/local/go/src/", deps: []string{"github.com/gorilla/websocket"}}
	raw := strings.Join([]string{
		"goroutine 7 [sync.Mutex.Lock, 3 minutes]:",
		"sync.runtime_SemacquireMutex(0xc000" + s + ", 0x0, 0x1)",
		"\t/usr/local/go/src/runtime/sema.go:95 +0x25",
		"github.com/sainteye/clawdline/internal/adapters/cloud.(*Conn).writeFrame(0xc0001, {0x" + s + ", 0x10})",
		"\t/Users/" + s + "/code/clawdline/internal/adapters/cloud/websocket.go:211 +0x1d",
		"evil.example/" + s + "/pkg.Leak(...)",
		"\t/Users/" + s + "/go/pkg/mod/evil.example/" + s + "@v1.0.0/leak.go:3 +0x9",
		"github.com/gorilla/websocket.(*Conn).WriteMessage(0x1)",
		"\t/Users/" + s + "/go/pkg/mod/github.com/gorilla/websocket@v1.5.3/conn.go:741 +0x1",
		"main.helper({0x" + s + "})",
		"\t/tmp/" + s + "/main.go:9 +0x1",
		"created by github.com/sainteye/clawdline/internal/adapters/cloud.(*Transport).Start in goroutine 1",
		"\t/Users/" + s + "/code/clawdline/internal/adapters/cloud/transport.go:88 +0x2",
		"",
		"goroutine 8 [chan receive]: labels: {\"token\":\"" + s + "\"}",
		"Authorization: Bearer " + s,
		"",
		"goroutine 9 gp=0xc000" + " m=1 [select, " + s + " state]:",
		"runtime.gopark(0x1)",
		"\t../../" + s + "/x.go:1 +0x1",
		"",
	}, "\n")
	got := sanitizeStack([]byte(raw), roots, CaptureJoin{Location: "cloud.websocket.writer_lock", Mapped: true,
		Function: "github.com/sainteye/clawdline/internal/adapters/cloud.(*Conn).writeFrame", Sources: []string{}})
	got.Schema, got.BootID, got.Build = CaptureSchema, "boot-x", "abc"
	if err := got.validate(); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(got)
	if strings.Contains(string(b), s) || strings.Contains(string(b), "/Users/") || strings.Contains(string(b), "Bearer") {
		t.Fatalf("a sentinel survived sanitizing: %s", b)
	}
	if len(got.Join.Sources) != 1 || got.Join.Sources[0] != "internal/adapters/cloud/websocket.go:211" {
		t.Fatalf("module source not mapped: %v", got.Join.Sources)
	}
	g := got.Goroutines[0]
	if g.ID != 7 || g.State != "sync.Mutex.Lock" || g.WaitMinutes != 3 || !g.MatchesLocation {
		t.Fatalf("header not parsed: %+v", g)
	}
	want := []CaptureFrame{
		{"sync.runtime_SemacquireMutex", "runtime/sema.go:95", SourceKindStd},
		{"github.com/sainteye/clawdline/internal/adapters/cloud.(*Conn).writeFrame", "internal/adapters/cloud/websocket.go:211", SourceKindModule},
		{RedactedFunction, "", SourceKindUnavailable},
		{"github.com/gorilla/websocket.(*Conn).WriteMessage", "github.com/gorilla/websocket@v1.5.3/conn.go:741", SourceKindDependency},
		{"main.helper", "", SourceKindUnavailable},
	}
	if !reflect.DeepEqual(g.Frames, want) {
		t.Fatalf("frames = %+v\nwant %+v", g.Frames, want)
	}
	if g.CreatedBy == nil || g.CreatedBy.Source != "internal/adapters/cloud/transport.go:88" {
		t.Fatalf("created-by not sanitized: %+v", g.CreatedBy)
	}
	// The evil module's file is in the module cache, so it maps; its
	// function does not, and the sentinel was in that path: dropped as unsafe.
	if got.Redacted != 1 || got.Unparsed < 2 || got.SourceUnavailable < 2 {
		t.Fatalf("counts: redacted %d unparsed %d unavailable %d", got.Redacted, got.Unparsed, got.SourceUnavailable)
	}
}

// A capture carrying anything outside the allowlist is not published.
func TestValidateRejectsUnsafeCaptureContent(t *testing.T) {
	ok := Capture{Schema: CaptureSchema, BootID: "boot-a", Build: "abc", Goroutines: []CaptureGoroutine{{State: "select", Frames: []CaptureFrame{{Function: "main.main"}}}}}
	if err := ok.validate(); err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string]func(*Capture){
		"absolute source": func(c *Capture) { c.Goroutines[0].Frames[0].Source = "/Users/a/x.go:1" },
		"function args":   func(c *Capture) { c.Goroutines[0].Frames[0].Function = "main.f(0x1, \"tok\")" },
		"state text":      func(c *Capture) { c.Goroutines[0].State = "token=abc" },
		"correlation":     func(c *Capture) { c.Trigger.Correlation = "Bearer x" },
		"build":           func(c *Capture) { c.Build = "1.2/3" },
	} {
		c := ok
		c.Goroutines = []CaptureGoroutine{{State: "select", Frames: []CaptureFrame{{Function: "main.main"}}}}
		bad(&c)
		if c.validate() == nil {
			t.Errorf("%s: validate accepted it", name)
		}
	}
}

// A capture that hangs in the runtime is reported timed out after the
// budget, refuses later triggers as unavailable, and never gets a second
// worker; the stall and the recovery are journaled meanwhile, and an
// export still answers. When it finally returns it is marked over budget
// and the worker takes triggers again.
func TestHungCaptureNeverGetsAReplacementWorker(t *testing.T) {
	c, rt := captureRuntime(t)
	release := make(chan struct{})
	var calls atomic.Int32
	entered := make(chan struct{}, 4)
	rt.Capture.stack = func(b []byte) int {
		calls.Add(1)
		entered <- struct{}{}
		<-release
		return copy(b, "goroutine 1 [select]:\nmain.main()\n\t/nowhere/main.go:1 +0x1\n")
	}
	h := stall(t, c, rt, testWriterLock)
	<-entered
	if st := rt.Capture.Status(); st.State != CaptureRunning {
		t.Fatalf("state = %s, want running", st.State)
	}
	c.advance(CaptureBudget)
	rt.Watchdog.Scan()
	if st := rt.Capture.Status(); st.State != CaptureTimedOut || st.TimedOut != 1 {
		t.Fatalf("after the budget: %+v", st)
	}
	// The recovery of the first phase is journaled while the capture hangs.
	if err := h.Progress(StageSocketWriteStarted); err != nil {
		t.Fatal(err)
	}
	c.advance(time.Second)
	rt.Watchdog.Scan()
	h.End(true)
	// A second stall finds the worker unavailable.
	stall(t, c, rt, LocSocketWrite)
	go rt.Capture.Run(context.Background()) // a second Run must not become a worker
	if st := rt.Capture.Status(); st.RefusedUnavailable != 1 || st.Started != 1 {
		t.Fatalf("while hung: %+v", st)
	}
	if n := journaled(t, rt); n[EventStallDetected] != 2 || n[EventRecovered] != 1 {
		t.Fatalf("stall/recovery not journaled while the capture hung: %v", n)
	}
	if _, err := rt.Export(Query{}); err != nil {
		t.Fatalf("export while hung: %v", err)
	}
	if rt.Capture.workers.Load() != 1 || calls.Load() != 1 {
		t.Fatalf("workers %d, captures %d", rt.Capture.workers.Load(), calls.Load())
	}
	close(release)
	waitFor(t, "the late capture", func() bool { _, ok := rt.Capture.Latest(); return ok })
	got, _ := rt.Capture.Latest()
	waitFor(t, "idle", func() bool { return rt.Capture.Status().State == CaptureIdle })
	if !got.OverBudget || rt.Capture.Status().OverBudget != 1 {
		t.Fatalf("a late capture is not marked over budget: %+v", got)
	}
	ev, err := rt.Export(Query{})
	if err != nil {
		t.Fatal(err)
	}
	if ev.StallCapture.Recovery.State != RecoveryRecovered {
		t.Fatalf("recovery while the capture hung: %+v", ev.StallCapture.Recovery)
	}
	stall(t, c, rt, LocDrainSend)
	<-entered
	if calls.Load() != 2 {
		t.Fatalf("the worker did not take a trigger after finishing: %d", calls.Load())
	}
}

// Off captures nothing and says so; the stall is journaled regardless.
func TestCaptureOffSkipsAndStillJournalsTheStall(t *testing.T) {
	c, rt := captureRuntime(t)
	rt.Capture.SetEnabled(false)
	var calls atomic.Int32
	rt.Capture.stack = func(b []byte) int { calls.Add(1); return 0 }
	stall(t, c, rt, testWriterLock)
	st := rt.Capture.Status()
	if st.State != CaptureOff || st.SkippedOff != 1 || calls.Load() != 0 {
		t.Fatalf("off: %+v, %d captures", st, calls.Load())
	}
	if n := journaled(t, rt); n[EventStallDetected] != 1 {
		t.Fatalf("stall not journaled with capture off: %v", n)
	}
	ev, err := rt.Export(Query{})
	if err != nil {
		t.Fatal(err)
	}
	if ev.StallCapture == nil || ev.StallCapture.Latest != nil || ev.StallCapture.Status.State != CaptureOff {
		t.Fatalf("off export: %+v", ev.StallCapture)
	}
}

// One in flight: a trigger while a capture runs is refused busy; past
// MaxCapturesPerWindow in one window, refused rate limited.
func TestCaptureBusyAndRateLimit(t *testing.T) {
	c, rt := captureRuntime(t)
	release := make(chan struct{})
	rt.Capture.stack = func(b []byte) int { <-release; return 0 }
	trig := CaptureTrigger{PhaseID: 1, Location: testWriterLock}
	if !rt.Capture.Request(trig) {
		t.Fatal("first trigger refused")
	}
	if rt.Capture.Request(trig) || rt.Capture.Status().RefusedBusy != 1 {
		t.Fatalf("a second trigger in flight was taken: %+v", rt.Capture.Status())
	}
	close(release)
	for i := 1; i < MaxCapturesPerWindow; i++ {
		waitFor(t, "idle", func() bool { return rt.Capture.Status().State == CaptureIdle })
		if !rt.Capture.Request(trig) {
			t.Fatalf("trigger %d refused", i+1)
		}
	}
	waitFor(t, "idle", func() bool { return rt.Capture.Status().State == CaptureIdle })
	if rt.Capture.Request(trig) || rt.Capture.Status().RefusedRate != 1 {
		t.Fatalf("past the window bound: %+v", rt.Capture.Status())
	}
	c.advance(CaptureWindow)
	if !rt.Capture.Request(trig) {
		t.Fatal("a new window refused")
	}
}

// A dump bigger than the export bound is trimmed from the end, keeping the
// matching goroutine, and says so.
func TestCaptureExportIsBounded(t *testing.T) {
	var b strings.Builder
	b.WriteString("goroutine 1 [select]:\nmain.target()\n\t/x/main.go:1 +0x1\n\n")
	for i := 2; i < 20000; i++ {
		b.WriteString("goroutine " + strconv.Itoa(i) + " [chan receive]:\nruntime.gopark(0x1)\n\t/usr/local/go/src/runtime/proc.go:1 +0x1\nsync.(*Cond).Wait(0x1)\n\t/usr/local/go/src/sync/cond.go:2 +0x1\n\n")
	}
	got := sanitizeStack([]byte(b.String()), sourceRoots{goroot: "/usr/local/go/src/", modulePath: modulePath},
		CaptureJoin{Location: "x.y", Function: "main.target", Mapped: true, Sources: []string{}})
	fitExport(&got)
	out, _ := json.Marshal(got)
	if len(out) > MaxCaptureExportBytes || !got.Truncated || got.GoroutinesOmitted == 0 || !got.Goroutines[0].MatchesLocation {
		t.Fatalf("bytes %d truncated %v omitted %d", len(out), got.Truncated, got.GoroutinesOmitted)
	}
	if got.GoroutinesTotal != 19999 || got.GoroutinesOmitted+len(got.Goroutines) != got.GoroutinesTotal {
		t.Fatalf("counts do not add up: total %d omitted %d kept %d", got.GoroutinesTotal, got.GoroutinesOmitted, len(got.Goroutines))
	}
}
