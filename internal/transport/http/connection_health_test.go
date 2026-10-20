package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/healthjournal"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/connectionhealth"
	"github.com/sainteye/clawdline/internal/transport/cloud"
)

func connectionHealthFixture(t *testing.T, rt *connectionhealth.Runtime) (*gateFixture, http.Handler) {
	t.Helper()
	f, _ := newGateFixture(t)
	s := &Server{cfg: config.Config{Dir: t.TempDir(), Port: 7757}}
	s.SetConnectionHealth(rt)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/connection-health/", s.connectionHealthRoute)
	return f, f.g.wrap(mux)
}

func refusalError(t *testing.T, body string) string {
	t.Helper()
	var r struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		t.Fatalf("not a refusal: %s", body)
	}
	return r.Error
}

// Only this machine's own token reads connection health: no token, a paired
// device and the machine token are refused, and a request the Cloud line
// dispatched in process — which carries this machine's token — is refused
// too, before the runtime is touched.
func TestConnectionHealthIsReadOnlyByThisMachineAndNeverThroughCloud(t *testing.T) {
	rt, err := connectionhealth.NewRuntime("c9fe745db066", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	rt.Watchdog.Scan()
	f, h := connectionHealthFixture(t, rt)
	for _, tc := range []struct {
		name    string
		headers map[string]string
		want    int
	}{
		{"no token", nil, http.StatusUnauthorized},
		{"a paired device", map[string]string{"Authorization": "Bearer " + f.send}, http.StatusForbidden},
		{"the machine token", map[string]string{"X-Clawdline-Orchestrator": f.machine}, http.StatusForbidden},
		{"this machine's token", map[string]string{"Authorization": "Bearer " + f.local}, http.StatusOK},
	} {
		rec := call{method: http.MethodGet, path: "/v1/connection-health/status", headers: tc.headers}.do(h)
		if rec.Code != tc.want {
			t.Errorf("%s: %d %s", tc.name, rec.Code, rec.Body)
		}
		if tc.want != http.StatusOK && strings.Contains(rec.Body.String(), rt.BootID) {
			t.Errorf("%s: the boot id leaked in a refusal", tc.name)
		}
	}

	router := cloud.Router{Handler: h, Authorize: cloud.LocalAuthorizer(f.local, f.machine)}
	res, err := router.Do(context.Background(), cloudops.LocalRequest{Method: http.MethodGet, Path: "/v1/connection-health/status"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != http.StatusForbidden || refusalError(t, string(res.Body)) != "forbidden" || strings.Contains(string(res.Body), rt.BootID) {
		t.Fatalf("a Cloud-dispatched read with this machine's token: %d %s", res.Status, res.Body)
	}
}

// Monitoring off is unsupported, not an empty healthy page; a cursor past
// the journal, or past uint64, is invalid_cursor; another boot's cursor is
// cursor_other_boot.
func TestConnectionHealthRefusalsAreTyped(t *testing.T) {
	f, off := connectionHealthFixture(t, nil)
	auth := map[string]string{"Authorization": "Bearer " + f.local}
	rec := call{method: http.MethodGet, path: "/v1/connection-health/status", headers: auth}.do(off)
	if rec.Code != http.StatusNotImplemented || refusalError(t, rec.Body.String()) != "unsupported" {
		t.Fatalf("monitoring off: %d %s", rec.Code, rec.Body)
	}

	rt, err := connectionhealth.NewRuntime("c9fe745db066", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	f, h := connectionHealthFixture(t, rt)
	auth = map[string]string{"Authorization": "Bearer " + f.local}
	rec = call{method: http.MethodGet, path: "/v1/connection-health/export", headers: auth}.do(h)
	if rec.Code != http.StatusServiceUnavailable || refusalError(t, rec.Body.String()) != "unsupported" {
		t.Fatalf("export before the first scan: %d %s", rec.Code, rec.Body)
	}
	rt.Watchdog.Scan()
	for _, tc := range []struct {
		query, code string
		status      int
	}{
		{"after_seq=2", "invalid_cursor", http.StatusBadRequest},
		{"after_seq=18446744073709551615", "invalid_cursor", http.StatusBadRequest},
		{"after_seq=18446744073709551616", "invalid_cursor", http.StatusBadRequest},
		{"after_seq=-1", "invalid_cursor", http.StatusBadRequest},
		{"limit=201", "query_bound_exceeded", http.StatusBadRequest},
		{"since=yesterday", "invalid_field", http.StatusBadRequest},
		{"boot_id=boot-other", "cursor_other_boot", http.StatusConflict},
	} {
		rec := call{method: http.MethodGet, path: "/v1/connection-health/events?" + tc.query, headers: auth}.do(h)
		if rec.Code != tc.status || refusalError(t, rec.Body.String()) != tc.code {
			t.Errorf("%s: %d %s", tc.query, rec.Code, rec.Body)
		}
	}
	rec = call{method: http.MethodGet, path: "/v1/connection-health/trace?correlation=../x", headers: auth}.do(h)
	if rec.Code != http.StatusBadRequest || refusalError(t, rec.Body.String()) != "invalid_field" {
		t.Fatalf("a correlation outside the opaque alphabet: %d %s", rec.Code, rec.Body)
	}
	rec = call{method: http.MethodGet, path: "/v1/connection-health/after_seq=1", headers: auth}.do(h)
	if rec.Code == http.StatusOK {
		t.Fatalf("an unknown sub-route answered: %s", rec.Body)
	}
}

// A handler stuck while the line is up is visible through the local route:
// its stage, its age and its location, with the build it belongs to, read
// from the watchdog's own report and the journal.
func TestConnectionHealthShowsAStuckHandlerWithItsLocation(t *testing.T) {
	rt, err := connectionhealth.NewRuntime("c9fe745db066", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	tr := rt.Probe()
	corr := tr.NewCorrelation()
	h := tr.Start(connectionhealth.DaemonSpec(connectionhealth.StageLaneQueued, connectionhealth.ClassRead,
		connectionhealth.LocReadLane, corr))
	_ = h.Step(connectionhealth.StageHandlerStarted, connectionhealth.LocReadHandler)
	rt.Watchdog.Scan()
	f, mux := connectionHealthFixture(t, rt)
	auth := map[string]string{"Authorization": "Bearer " + f.local}

	rec := call{method: http.MethodGet, path: "/v1/connection-health/trace?correlation=" + corr, headers: auth}.do(mux)
	var trace connectionhealth.Trace
	if err := json.Unmarshal(rec.Body.Bytes(), &trace); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("trace: %d %s", rec.Code, rec.Body)
	}
	if len(trace.Active) != 1 || trace.Active[0].Stage != connectionhealth.StageHandlerStarted ||
		trace.Active[0].Location != connectionhealth.LocReadHandler || trace.ReportScanSeq == nil {
		t.Fatalf("the active handler is not in its trace: %+v", trace)
	}

	rec = call{method: http.MethodGet, path: "/v1/connection-health/export", headers: auth}.do(mux)
	var ev connectionhealth.Evidence
	if err := json.Unmarshal(rec.Body.Bytes(), &ev); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("export: %d %s", rec.Code, rec.Body)
	}
	if ev.Locations == nil || ev.Locations.Build != "c9fe745db066" || ev.Journal.Build != "c9fe745db066" {
		t.Fatalf("the export is not bound to its build: %+v", ev.Locations)
	}
	found := false
	for _, e := range ev.Locations.Entries {
		if e.Code == connectionhealth.LocReadHandler && strings.HasSuffix(e.Function, ".(*Link).answerTraced") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the read handler's location is not mapped: %+v", ev.Locations.Entries)
	}
	h.End(true)
}

// With a runtime the capacity rows are measured from it; without one they
// are a known zero whose note says monitoring is off.
func TestConnectionHealthCapacityRowsAreMeasured(t *testing.T) {
	off := (&Server{}).connectionHealthCapacityMeasures()
	if r := off["cloud.health_active_slots"](); !r.Known || r.Used != 0 || !strings.Contains(r.Note, "off") {
		t.Fatalf("monitoring off: %+v", r)
	}
	rt, err := connectionhealth.NewRuntime("b", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{}
	s.SetConnectionHealth(rt)
	m := s.connectionHealthCapacityMeasures()
	loop := rt.Tracker.Start(connectionhealth.LoopSpec(connectionhealth.LocReadLoop))
	op := rt.Tracker.Start(connectionhealth.DaemonSpec(connectionhealth.StageFrameRead, connectionhealth.ClassTransport, connectionhealth.LocHandleFrame, ""))
	if r := m["cloud.health_active_slots"](); !r.Known || r.Used != 2 {
		t.Fatalf("active slots: %+v", r)
	}
	if r := m["cloud.health_reserved_slots"](); !r.Known || r.Used != 1 {
		t.Fatalf("reserved slots: %+v", r)
	}
	if r := m["cloud.health_journal_rows"](); !r.Known || r.Used != 1 {
		t.Fatalf("journal rows hold the boot record: %+v", r)
	}
	if r := m["cloud.health_record_bytes"](); !r.Known || r.Used <= 0 || r.Used > connectionhealth.MaxRecordBytes {
		t.Fatalf("record bytes: %+v", r)
	}
	if r := m["cloud.health_pending_transitions"](); !r.Known {
		t.Fatalf("pending transitions: %+v", r)
	}
	loop.End(true)
	op.End(true)
}

// The durable history route reads the store the journal writer fed: the
// first boot's records are read back in the next boot, with that boot named
// (that every writer transition is handed on is pinned in the foundation's
// TestAttachHandsOnEveryStampedRecordOnce); a
// store that is not open is unsupported with its state, never an empty page.
func TestConnectionHealthHistoryOutlivesARestart(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "connection-health", "journal")
	boot := func() (*connectionhealth.Runtime, *healthjournal.Store) {
		rt, err := connectionhealth.NewRuntime("c9fe745db066", time.Second)
		if err != nil {
			t.Fatal(err)
		}
		st, err := healthjournal.Open(healthjournal.Options{Dir: dir, BootID: rt.BootID, Build: rt.Build})
		if err != nil {
			t.Fatal(err)
		}
		rt.Watchdog.Attach(func(r connectionhealth.Record) { _, _ = st.Offer(r) })
		return rt, st
	}
	first, store := boot()
	first.Watchdog.Scan()
	if err := first.Journal.Append(connectionhealth.Record{Event: connectionhealth.EventOverflow, Count: 1, Rule: connectionhealth.RuleVersion}); err != nil {
		t.Fatal(err)
	}
	// A record a caller appends directly is not offered (only the writer
	// hands records on).
	first.Probe().Start(connectionhealth.DaemonSpec(connectionhealth.StageHandlerStarted, connectionhealth.ClassRead, connectionhealth.LocReadHandler, "op-x"))
	first.Watchdog.Flush()
	if _, err := store.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-store.Done()

	second, store2 := boot()
	defer func() { _ = store2.Close(context.Background()) }()
	f, h := connectionHealthFixture(t, second)
	auth := map[string]string{"Authorization": "Bearer " + f.local}
	rec := call{method: http.MethodGet, path: "/v1/connection-health/history", headers: auth}.do(h)
	if rec.Code != http.StatusServiceUnavailable || refusalError(t, rec.Body.String()) != "unsupported" {
		t.Fatalf("history before the store is set: %d %s", rec.Code, rec.Body)
	}
	srv := &Server{}
	srv.SetConnectionHealth(second)
	srv.SetConnectionHealthStore(nil, nil, true)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/connection-health/", srv.connectionHealthRoute)
	h = f.g.wrap(mux)
	rec = call{method: http.MethodGet, path: "/v1/connection-health/history", headers: auth}.do(h)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "opening") {
		t.Fatalf("history while opening: %d %s", rec.Code, rec.Body)
	}
	srv.SetConnectionHealthStore(store2, nil, false)
	rec = call{method: http.MethodGet, path: "/v1/connection-health/history?limit=200", headers: auth}.do(h)
	var page healthjournal.Page
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("history: %d %s", rec.Code, rec.Body)
	}
	events := map[connectionhealth.Event]int{}
	for _, e := range page.Entries {
		if e.Record != nil && e.Record.BootID == first.BootID {
			events[e.Record.Event]++
		}
	}
	if events[connectionhealth.EventBoot] != 1 || events[connectionhealth.EventOverflow] != 0 {
		t.Fatalf("the first boot's records read back: %+v (%s)", events, rec.Body)
	}
	named := false
	for _, b := range page.Boots {
		named = named || b.BootID == first.BootID
	}
	if !named {
		t.Fatalf("the first boot is not named: %+v", page.Boots)
	}
	rec = call{method: http.MethodGet, path: "/v1/connection-health/history?cursor=v9.x", headers: auth}.do(h)
	if rec.Code != http.StatusBadRequest || refusalError(t, rec.Body.String()) != "invalid_cursor" {
		t.Fatalf("a forged cursor: %d %s", rec.Code, rec.Body)
	}
	rec = call{method: http.MethodGet, path: "/v1/connection-health/status", headers: auth}.do(h)
	if !strings.Contains(rec.Body.String(), `"durable":{"state":"open","status":{`) {
		t.Fatalf("status without the durable store: %s", rec.Body)
	}
	srv.SetConnectionHealthStore(nil, errors.New("x"), false)
	m := srv.connectionHealthCapacityMeasures()
	if r := m["cloud.health_store_bytes"](); r.Known || r.Err == "" {
		t.Fatalf("a failed store reads as measured: %+v", r)
	}
	srv.SetConnectionHealthStore(store2, nil, false)
	m = srv.connectionHealthCapacityMeasures()
	if r := m["cloud.health_store_bytes"](); !r.Known || r.Used <= 0 {
		t.Fatalf("store bytes: %+v", r)
	}
	if r := m["cloud.health_store_segments"](); !r.Known || r.Used < 1 {
		t.Fatalf("store segments: %+v", r)
	}
}
