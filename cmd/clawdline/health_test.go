package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/healthjournal"
	"github.com/sainteye/clawdline/internal/adapters/healthupload"
	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/connectionhealth"
	httptransport "github.com/sainteye/clawdline/internal/transport/http"
)

func healthRunAgainst(t *testing.T, handler http.HandlerFunc, args ...string) (int, string, string, *http.Request) {
	t.Helper()
	srv := httptest.NewServer(handler)
	defer srv.Close()
	var asked *http.Request
	newRequest := func(path string, q url.Values) (*http.Request, error) {
		if len(q) > 0 {
			path += "?" + q.Encode()
		}
		req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		if err == nil {
			req.Header.Set("Authorization", "Bearer local-token")
		}
		asked = req
		return req, err
	}
	var stdout, stderr bytes.Buffer
	code := healthRun(&stdout, &stderr, args, time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC), newRequest, srv.Client().Do)
	return code, stdout.String(), stderr.String(), asked
}

// Usage mistakes exit 2 before any request: an unknown subcommand, a cursor
// that does not fit a sequence number, a limit past the page bound, a time
// that is neither RFC 3339 nor a duration, a correlation outside the opaque
// alphabet.
func TestHealthCommandRefusesBadArgumentsBeforeAsking(t *testing.T) {
	never := func(w http.ResponseWriter, r *http.Request) { t.Errorf("asked %s", r.URL) }
	for _, args := range [][]string{
		nil, {"nope"}, {"events", "--after-seq", "18446744073709551616"}, {"events", "--after-seq", "-1"},
		{"events", "--limit", "201"}, {"export", "--since", "yesterday"}, {"trace", "--correlation", "../etc"},
		{"trace"},
	} {
		if code, _, _, _ := healthRunAgainst(t, never, args...); code != 2 {
			t.Errorf("%v: exit %d", args, code)
		}
	}
}

// The window is sent as RFC 3339 times, a duration counted back from now.
func TestHealthCommandSendsTheWindow(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, `{"records":[]}`) }
	code, out, _, req := healthRunAgainst(t, ok, "events", "--since", "15m", "--until", "2026-10-20T12:00:00Z", "--after-seq", "3", "--limit", "5")
	if code != 0 || !strings.Contains(out, `"records": []`) {
		t.Fatalf("exit %d: %s", code, out)
	}
	q := req.URL.Query()
	if req.URL.Path != "/v1/connection-health/events" || q.Get("since") != "2026-10-20T11:45:00Z" ||
		q.Get("until") != "2026-10-20T12:00:00Z" || q.Get("after_seq") != "3" || q.Get("limit") != "5" {
		t.Fatalf("asked %s", req.URL)
	}
}

// A refusal prints its code and exits 1; an unreachable daemon says the
// state is unknown, never that it is fine.
func TestHealthCommandSaysRefusedOrUnknown(t *testing.T) {
	refuse := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error":"forbidden","detail":"Only this machine's own token may manage devices."}`)
	}
	code, _, errOut, _ := healthRunAgainst(t, refuse, "status")
	if code != 1 || !strings.Contains(errOut, "403 forbidden") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var stderr bytes.Buffer
	down := func(*http.Request) (*http.Response, error) { return nil, errors.New("connection refused") }
	newRequest := func(path string, _ url.Values) (*http.Request, error) {
		return http.NewRequest(http.MethodGet, "http://127.0.0.1:1"+path, nil)
	}
	if code := healthRun(io.Discard, &stderr, []string{"status"}, time.Now(), newRequest, down); code != 1 || !strings.Contains(stderr.String(), "unknown") {
		t.Fatalf("unreachable: exit %d: %s", code, stderr.String())
	}
}

// The status summary leads with the quality and lists only the findings
// that are slow or stuck, with their stage, location and age.
func TestHealthStatusSummary(t *testing.T) {
	body := `{"schema":"clawdline.connection-health.status/1","boot_id":"boot-1","build_commit":"abc","running":true,
	"quality":"complete","journal_coverage":{"quality":"complete","first_retained_seq":1,"last_seq":3,"evicted_total":0,"refused_total":0,"truncated":false,"next_after_seq":0,"durability":"memory_only_lost_on_restart"},
	"watchdog":{"rule":"watchdog/1","scan_seq":9,"trace_coverage":"complete","transitions":"complete","overflow":{"total":0,"critical":0},
	"findings":[{"phase_id":2,"stage":"handler_started","location":"cloud.link.read_handler","state":"blocked","age_ns":16000000000,"correlation":"op-2"},
	{"phase_id":1,"stage":"idle","location":"cloud.transport.read_loop","state":"idle","age_ns":1}]}}`
	code, out, errOut, _ := healthRunAgainst(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, body) }, "status")
	if code != 0 || !strings.Contains(out, "quality complete") || !strings.Contains(out, "cloud.link.read_handler") ||
		!strings.Contains(out, "16s") || strings.Contains(out, "read_loop") {
		t.Fatalf("exit %d: %s %s", code, out, errOut)
	}
}

// A store that cannot be opened — here its directory's parent is a file —
// is a typed failure, and the runtime keeps scanning in memory.
func TestABrokenStoreDirectoryLeavesTheRuntimeRunning(t *testing.T) {
	state := t.TempDir()
	if err := os.WriteFile(filepath.Join(state, "connection-health"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	rt, err := connectionhealth.NewRuntime("abc", 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go rt.Run(ctx)
	store, err := openConnectionHealthStore(rt, state)
	if err == nil || store != nil || healthjournal.CodeOf(err) == "" {
		t.Fatalf("a store under a file opened: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if r, ok := rt.Watchdog.Latest(); ok && r.ScanSeq > 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the runtime stopped scanning after the store failed")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// history pages the durable store with its own cursor and bounded limit.
func TestHealthHistoryAsksTheDurableStore(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, `{"entries":[]}`) }
	code, _, _, req := healthRunAgainst(t, ok, "history", "--cursor", "v1.3.120", "--limit", "50")
	if code != 0 || req.URL.Path != "/v1/connection-health/history" || req.URL.Query().Get("cursor") != "v1.3.120" || req.URL.Query().Get("limit") != "50" {
		t.Fatalf("exit %d, asked %s", code, req.URL)
	}
	never := func(w http.ResponseWriter, r *http.Request) { t.Errorf("asked %s", r.URL) }
	if code, _, _, _ := healthRunAgainst(t, never, "history", "--limit", "500"); code != 2 {
		t.Fatalf("an over-bound limit: exit %d", code)
	}
}

func newHealthServer(t *testing.T) (string, *httptransport.Server) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "next")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	srv, err := httptransport.New(config.Config{Dir: dir, Host: "127.0.0.1", Port: 1})
	if err != nil {
		t.Fatal(err)
	}
	return dir, srv
}

// Stopping flushes and closes the store, frees its lock, and the next boot
// reads this boot's records back from disk.
func TestStoppingFlushesAndClosesTheStoreForTheNextBoot(t *testing.T) {
	dir, srv := newHealthServer(t)
	h := startConnectionHealth(dir, srv)
	if h == nil {
		t.Fatal("monitoring is off")
	}
	deadline := time.Now().Add(5 * time.Second)
	for h.store.Load() == nil {
		if time.Now().After(deadline) {
			t.Fatal("the store did not open")
		}
		time.Sleep(5 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), connectionHealthStopTimeout)
	defer cancel()
	if err := h.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-h.store.Load().Done():
	case <-time.After(time.Second):
		t.Fatal("the store's worker is still running after Stop")
	}
	if st := h.upload.Status(); st.State != healthupload.CodeStopped {
		t.Fatalf("uploader %s after Stop", st.State)
	}
	// The next boot: the lock is free and this boot's boot record is there.
	next, err := healthjournal.Open(healthjournal.Options{Dir: connectionHealthStoreDir(dir), BootID: "boot-next", Build: "abc"})
	if err != nil {
		t.Fatalf("the next boot cannot open the store: %v", err)
	}
	defer func() { _ = next.Close(context.Background()) }()
	page, err := next.Read(healthjournal.Query{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range page.Entries {
		if e.BootID == h.rt.BootID && e.Record != nil && e.Record.Event == connectionhealth.EventBoot {
			found = true
		}
	}
	if !found {
		t.Fatalf("the stopped boot's records are not on disk: %+v", page.Entries)
	}
}

// A shutdown that began while the store was opening, and whose Stop gave up
// waiting: the open still completes and closes its own handle, so the lock
// does not outlive the shutdown.
func TestStoppingWhileTheStoreOpensDoesNotLeakIt(t *testing.T) {
	dir, srv := newHealthServer(t)
	h := startConnectionHealth(dir, srv)
	h.cancel()
	select {
	case <-h.opened:
	case <-time.After(5 * time.Second):
		t.Fatal("the open never finished")
	}
	next, err := healthjournal.Open(healthjournal.Options{Dir: connectionHealthStoreDir(dir), BootID: "boot-next", Build: "abc"})
	if err != nil {
		t.Fatalf("the lock leaked: %v", err)
	}
	_ = next.Close(context.Background())
}

// waitHealthStore waits for the durable store to open.
func waitHealthStore(t *testing.T, h *connectionHealthLife) *healthjournal.Store {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for h.store.Load() == nil {
		if time.Now().After(deadline) {
			t.Fatal("the store did not open")
		}
		time.Sleep(5 * time.Millisecond)
	}
	return h.store.Load()
}

// waitStall starts a phase that is blocked within milliseconds and waits for
// the daemon's own watchdog scan to report it.
func waitStall(t *testing.T, h *connectionHealthLife) {
	t.Helper()
	h.rt.Tracker.Start(connectionhealth.Spec{
		Source: connectionhealth.SourceDaemon, Stage: connectionhealth.StageHandlerStarted,
		Class: connectionhealth.ClassRead, Kind: connectionhealth.Finite,
		Location: "cloud.link.read_handler", Warn: time.Millisecond, Block: 2 * time.Millisecond,
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		if r, ok := h.rt.Watchdog.Latest(); ok {
			for _, f := range r.Findings {
				if f.State == connectionhealth.StateBlocked {
					return
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("the watchdog never reported the stall")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func readStoreEvents(t *testing.T, dir, boot string) []connectionhealth.Event {
	t.Helper()
	next, err := healthjournal.Open(healthjournal.Options{Dir: connectionHealthStoreDir(dir), BootID: "boot-next", Build: "abc"})
	if err != nil {
		t.Fatalf("the next boot cannot open the store: %v", err)
	}
	defer func() { _ = next.Close(context.Background()) }()
	page, err := next.Read(healthjournal.Query{})
	if err != nil {
		t.Fatal(err)
	}
	var out []connectionhealth.Event
	for _, e := range page.Entries {
		if e.BootID == boot && e.Record != nil {
			out = append(out, e.Record.Event)
		}
	}
	return out
}

// A stall the watchdog observed is on disk for the next boot after Stop.
func TestAStallObservedBeforeStopIsOnDiskForTheNextBoot(t *testing.T) {
	dir, srv := newHealthServer(t)
	h := startConnectionHealth(dir, srv)
	if h == nil {
		t.Fatal("monitoring is off")
	}
	waitHealthStore(t, h)
	waitStall(t, h)
	ctx, cancel := context.WithTimeout(context.Background(), connectionHealthStopTimeout)
	defer cancel()
	if err := h.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	got := readStoreEvents(t, dir, h.rt.BootID)
	for _, e := range got {
		if e == connectionhealth.EventStallDetected {
			return
		}
	}
	t.Fatalf("the observed stall is not on disk: %v", got)
}

// A durable sink that never returns holds the watchdog's writer. Stop still
// returns by its deadline, names the held writer, and flushes and closes the
// store so the next boot can open it.
func TestAHeldWatchdogWriterDoesNotHoldStopPastItsDeadline(t *testing.T) {
	dir, srv := newHealthServer(t)
	h := startConnectionHealth(dir, srv)
	if h == nil {
		t.Fatal("monitoring is off")
	}
	store := waitHealthStore(t, h)
	release := make(chan struct{})
	held := make(chan struct{}, 1)
	h.rt.Watchdog.Attach(func(r connectionhealth.Record) {
		if r.Event != connectionhealth.EventStallDetected {
			_, _ = store.Offer(r)
			return
		}
		select {
		case held <- struct{}{}:
		default:
		}
		<-release
	})
	defer func() {
		close(release)
		select {
		case <-h.runDone:
		case <-time.After(5 * time.Second):
			t.Error("the watchdog did not finish after its sink let go")
		}
	}()
	waitStall(t, h)
	<-held
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	began := time.Now()
	err := h.Stop(ctx)
	if took := time.Since(began); took > time.Second {
		t.Fatalf("Stop waited %v on a held writer", took)
	}
	if err == nil || !strings.Contains(err.Error(), "watchdog (writer held;") {
		t.Fatalf("Stop must name the held watchdog writer: %v", err)
	}
	select {
	case <-store.Done():
	case <-time.After(time.Second):
		t.Fatal("the store was not closed while the writer was held")
	}
	readStoreEvents(t, dir, h.rt.BootID)
}

// The settings file takes the opt-in through the same writer the settings
// route uses, and the uploader's identity reads it: absent and false are
// not_opted_in (no request), true moves on to Cloud's own switch.
func TestTheHealthUploadOptInIsASettingTheUploaderReads(t *testing.T) {
	dir := t.TempDir()
	identity := healthUploadIdentity(dir, nil)
	code := func() healthupload.Code {
		_, err := identity()
		var e *healthupload.Error
		if !errors.As(err, &e) {
			t.Fatalf("identity answered %v", err)
		}
		return e.Code
	}
	if c := code(); c != healthupload.CodeNotOptedIn {
		t.Fatalf("absent: %s", c)
	}
	// The settings route admits only a listed key: `clawdline setting` and
	// the console write through it.
	if k, ok := nextconfig.SettableByName(healthUploadKey); !ok || !k.Allows(true) || k.Allows("yes") {
		t.Fatalf("%s is not a bool the settings route accepts", healthUploadKey)
	}
	file := nextconfig.Open(dir)
	if _, err := file.Set(map[string]any{healthUploadKey: false}); err != nil {
		t.Fatal(err)
	}
	if c := code(); c != healthupload.CodeNotOptedIn {
		t.Fatalf("false: %s", c)
	}
	if _, err := file.Set(map[string]any{healthUploadKey: true}); err != nil {
		t.Fatalf("the settings file refuses the opt-in: %v", err)
	}
	if c := code(); c == healthupload.CodeNotOptedIn {
		t.Fatal("true still reads as not opted in")
	}
}
