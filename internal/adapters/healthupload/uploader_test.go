package healthupload

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/healthjournal"
	"github.com/sainteye/clawdline/internal/domain/connectionhealth"
)

type clock struct {
	mu  sync.Mutex
	now connectionhealth.Instant
}

func (c *clock) Now() connectionhealth.Instant {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now.Mono += time.Millisecond
	c.now.Wall = c.now.Wall.Add(time.Millisecond)
	return c.now
}

var t0 = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

// fixture is a real durable store fed by a real foundation journal, the way
// the daemon feeds it.
type fixture struct {
	t     *testing.T
	dir   string
	boot  string
	j     *connectionhealth.Journal
	store *healthjournal.Store
	last  uint64
}

func newFixture(t *testing.T, dir, boot string) *fixture {
	t.Helper()
	j, err := connectionhealth.NewJournal(&clock{now: connectionhealth.Instant{Mono: time.Second, Wall: t0}}, boot, "c9fe745db066")
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, dir: dir, boot: boot, j: j}
	f.open()
	return f
}

func (f *fixture) open() {
	f.t.Helper()
	s, err := healthjournal.Open(healthjournal.Options{Dir: filepath.Join(f.dir, "journal"), BootID: f.boot, Build: "c9fe745db066"})
	if err != nil {
		f.t.Fatal(err)
	}
	f.store = s
	f.t.Cleanup(func() { f.close() })
}

func (f *fixture) close() {
	if f.store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = f.store.Close(ctx)
	<-f.store.Done()
	f.store = nil
}

// add appends n stall records carrying every owner-private field, offers
// what the journal stamped to the store and flushes it.
func (f *fixture) add(n int) {
	f.t.Helper()
	for i := 0; i < n; i++ {
		if err := f.j.Append(connectionhealth.Record{
			Event: connectionhealth.EventStallDetected, Source: connectionhealth.SourceDaemon,
			Stage: connectionhealth.StageHandlerStarted, Class: connectionhealth.ClassRead,
			Kind: connectionhealth.Finite, Location: "cloud.link.read_handler", PhaseID: uint64(i + 1),
			Correlation: "tr-1", AgeNS: 8_000_000_000 + int64(i), DeadlineNS: 7_000_000_000, Rule: connectionhealth.RuleVersion,
		}); err != nil {
			f.t.Fatal(err)
		}
	}
	page, err := f.j.Read(connectionhealth.Query{AfterSeq: f.last})
	if err != nil {
		f.t.Fatal(err)
	}
	for _, r := range page.Records {
		if _, err := f.store.Offer(r); err != nil {
			f.t.Fatal(err)
		}
	}
	f.last = page.Coverage.NextAfterSeq
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := f.store.Flush(ctx); err != nil {
		f.t.Fatal(err)
	}
}

// cloud is a fake ingest with the real contract's idempotency: a batch is
// keyed by batch_id, an exact replay answers 200 with the stored receipt,
// a changed body under the same id answers 409.
type cloud struct {
	mu       sync.Mutex
	stored   map[string]string // batch_id -> body digest
	receipts map[string][]byte
	bodies   [][]byte
	requests int
	// answer, when set, decides the response instead of the ingest.
	answer func(n int, w http.ResponseWriter, r *http.Request, body []byte) bool
	auth   string
}

func newCloud() *cloud {
	return &cloud{stored: map[string]string{}, receipts: map[string][]byte{}, auth: "Bearer machine-secret"}
}

func (c *cloud) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	c.mu.Lock()
	c.requests++
	n := c.requests
	c.bodies = append(c.bodies, body)
	answer := c.answer
	c.mu.Unlock()
	if r.URL.Path != IngestPath || r.Method != http.MethodPost || r.URL.RawQuery != "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if r.Header.Get("Authorization") != c.auth || r.Header.Get("Cookie") != "" {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":"unauthorized","message":"no"}}`))
		return
	}
	if answer != nil && answer(n, w, r, body) {
		return
	}
	c.ingest(w, body)
}

func (c *cloud) ingest(w http.ResponseWriter, body []byte) {
	var b struct {
		BatchID string            `json:"batch_id"`
		Events  []json.RawMessage `json:"events"`
	}
	if json.Unmarshal(body, &b) != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	sum := sha256.Sum256(body)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	c.mu.Lock()
	defer c.mu.Unlock()
	if old, ok := c.stored[b.BatchID]; ok {
		if old != digest {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":{"code":"idempotency_conflict","message":"x","details":{"field":"batch_id"}}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(c.receipts[b.BatchID])
		return
	}
	rc, _ := json.Marshal(map[string]any{"schema_version": 1, "receipt_id": "hr_" + b.BatchID, "batch_id": b.BatchID,
		"digest": digest, "persisted_at": "2026-10-20T12:00:17.000Z", "retain_until": "2026-10-21T12:00:17.000Z",
		"accepted": len(b.Events), "rejected": []any{}, "coverage": map[string]any{}})
	c.stored[b.BatchID] = digest
	c.receipts[b.BatchID] = rc
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write(rc)
}

func (c *cloud) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.requests
}

type harness struct {
	u     *Uploader
	cloud *cloud
	srv   *httptest.Server
	now   time.Time
	cred  string
	f     *fixture
	state string
}

func newHarness(t *testing.T, f *fixture, c *cloud, state string) *harness {
	t.Helper()
	srv := httptest.NewServer(c)
	t.Cleanup(srv.Close)
	h := &harness{cloud: c, srv: srv, now: t0.Add(time.Minute), cred: "machine-secret", f: f, state: state}
	h.u = h.make()
	return h
}

func (h *harness) make() *Uploader {
	u := New(Options{
		StateDir: h.state,
		Identity: func() (Identity, error) { return Identity{APIBase: h.srv.URL, Credential: h.cred}, nil },
		Source: func() Source {
			if h.f.store == nil {
				return nil
			}
			return h.f.store
		},
		Now:               func() time.Time { return h.now },
		AllowLoopbackHTTP: true,
		AttemptTimeout:    300 * time.Millisecond,
	})
	st, _, err := loadState(h.state)
	if err != nil {
		panic(err)
	}
	u.st = st
	return u
}

func (h *harness) step(t *testing.T) time.Duration {
	t.Helper()
	return h.u.Step(context.Background())
}

func TestADroppedAnswerIsResentByteForByteAndReceiptedOnce(t *testing.T) {
	dir := t.TempDir()
	f := newFixture(t, dir, "boot-a")
	f.add(5)
	c := newCloud()
	c.answer = func(n int, w http.ResponseWriter, r *http.Request, body []byte) bool {
		if n != 1 {
			return false
		}
		// Store the batch, then lose the answer on the way back.
		c.ingest(httptest.NewRecorder(), body)
		panic(http.ErrAbortHandler)
	}
	h := newHarness(t, f, c, filepath.Join(dir, "upload"))
	if wait := h.step(t); wait == 0 {
		t.Fatalf("a lost answer must wait before the retry")
	}
	if st := h.u.Status(); st.State != CodeUpstreamUnavailable || st.Pending == nil || st.Pending.Persisted != "unknown" {
		t.Fatalf("after a lost answer: %+v", st)
	}
	h.now = h.now.Add(time.Minute)
	h.step(t)
	if c.count() != 2 || !bytes.Equal(c.bodies[0], c.bodies[1]) {
		t.Fatalf("the retry must resend the same bytes: %d requests", c.count())
	}
	if len(c.stored) != 1 {
		t.Fatalf("Cloud holds %d batches, want one", len(c.stored))
	}
	st := h.u.Status()
	if st.LastReceipt == nil || !st.LastReceipt.Replay || st.Totals.Batches != 1 || st.Pending != nil {
		t.Fatalf("one durable receipt, from the replay: %+v", st)
	}
	// The boot record and five stalls.
	if st.Totals.Receipted != 6 {
		t.Fatalf("receipted %d events, want 6", st.Totals.Receipted)
	}
}

func TestOwnerPrivateFieldsNeverLeaveTheMachine(t *testing.T) {
	dir := t.TempDir()
	f := newFixture(t, dir, "boot-p")
	f.add(3)
	c := newCloud()
	h := newHarness(t, f, c, filepath.Join(dir, "upload"))
	h.step(t)
	if c.count() != 1 {
		t.Fatalf("%d requests", c.count())
	}
	body := c.bodies[0]
	for _, banned := range []string{"location", "age_ns", "deadline_ns", "cloud.link", "read_handler", "8000000000", "7000000000", "owner_sealed", "state\""} {
		if bytes.Contains(body, []byte(banned)) {
			t.Fatalf("the body carries %q", banned)
		}
	}
	// Every key is one Cloud's record/1 parser lists.
	allowed := map[string]bool{}
	for _, k := range []string{"schema", "producer_seq", "event_id", "boot_id", "event", "wall", "mono_ns", "build_commit",
		"source", "stage", "class", "kind", "phase_id", "correlation", "count", "rule"} {
		allowed[k] = true
	}
	var b map[string]json.RawMessage
	if err := json.Unmarshal(body, &b); err != nil {
		t.Fatal(err)
	}
	for k := range b {
		switch k {
		case "schema_version", "batch_id", "producer_instance", "boot_id", "build_commit", "events":
		default:
			t.Fatalf("batch key %q", k)
		}
	}
	var events []map[string]json.RawMessage
	_ = json.Unmarshal(b["events"], &events)
	for _, e := range events {
		for k := range e {
			if !allowed[k] {
				t.Fatalf("event key %q", k)
			}
		}
		var wall string
		_ = json.Unmarshal(e["wall"], &wall)
		if !strings.HasSuffix(wall, "Z") {
			t.Fatalf("wall %q is not UTC", wall)
		}
	}
	if strings.Contains(string(body), "machine-secret") {
		t.Fatal("the credential is in the body")
	}
}

func TestAStateAfterLossRecordIsCountedNotSent(t *testing.T) {
	r := connectionhealth.Record{Schema: connectionhealth.RecordSchema, Seq: 4, EventID: "b-4", BootID: "b",
		Event: connectionhealth.EventStateAfterLoss, Wall: t0, State: connectionhealth.StateBlocked}
	if _, reason := toWire(r); reason != ExcludedStateAfterLoss {
		t.Fatalf("state_after_loss: %q", reason)
	}
	r.Event, r.MonoNS = connectionhealth.EventStallDetected, jsSafeInteger+1
	if _, reason := toWire(r); reason != ExcludedUnsafeInteger {
		t.Fatalf("unsafe mono_ns: %q", reason)
	}
}

func TestEveryGoEnumIsReconciledWithCloud(t *testing.T) {
	for _, e := range []connectionhealth.Event{
		connectionhealth.EventBoot, connectionhealth.EventSlowDetected, connectionhealth.EventStallDetected,
		connectionhealth.EventRecovered, connectionhealth.EventReleasedStalled, connectionhealth.EventOverflow,
		connectionhealth.EventSchedulingGap, connectionhealth.EventClockAnomaly, connectionhealth.EventTransitionsLost,
		connectionhealth.EventStateAfterLoss,
	} {
		_, excluded := excludedEvents[e]
		if cloudEvents[e] == excluded {
			t.Errorf("event %q must be either sent or explicitly excluded", e)
		}
	}
	for _, s := range append(append([]connectionhealth.Stage{}, connectionhealth.Lifecycle...), connectionhealth.StageIdle) {
		if !cloudStages[s] {
			t.Errorf("stage %q is not in Cloud's record/1 list: decide whether Cloud adds it", s)
		}
	}
}

func TestARestartResendsThePendingBatchAndKeepsTheCursor(t *testing.T) {
	dir := t.TempDir()
	f := newFixture(t, dir, "boot-r")
	f.add(4)
	c := newCloud()
	down := true
	c.answer = func(int, http.ResponseWriter, *http.Request, []byte) bool {
		if down {
			panic(http.ErrAbortHandler)
		}
		return false
	}
	h := newHarness(t, f, c, filepath.Join(dir, "upload"))
	h.step(t)
	first := h.u.Status().Pending
	if first == nil {
		t.Fatal("no pending batch")
	}
	// The daemon restarts: the store reopens, more records arrive, and a
	// new uploader reads the progress file.
	f.close()
	f.open()
	f.add(2)
	down = false
	h.now = h.now.Add(time.Minute)
	h.u = h.make()
	h.step(t)
	if c.count() != 2 || !bytes.Equal(c.bodies[0], c.bodies[1]) {
		t.Fatal("after a restart the same batch must be resent byte for byte")
	}
	h.step(t)
	st := h.u.Status()
	if st.Totals.Batches != 2 || st.Totals.Receipted != 7 || st.Pending != nil {
		t.Fatalf("both batches receipted, nothing twice: %+v", st.Totals)
	}
	// Idle: nothing new, no request.
	n := c.count()
	h.u = h.make()
	h.step(t)
	h.step(t)
	if c.count() != n || h.u.Status().State != CodeIdle {
		t.Fatalf("an idle uploader made %d requests", c.count()-n)
	}
}

func TestARevokedCredentialIsNotRetried(t *testing.T) {
	dir := t.TempDir()
	f := newFixture(t, dir, "boot-x")
	f.add(1)
	c := newCloud()
	c.auth = "Bearer another"
	h := newHarness(t, f, c, filepath.Join(dir, "upload"))
	h.step(t)
	for i := 0; i < 5; i++ {
		h.now = h.now.Add(time.Hour)
		h.step(t)
	}
	if c.count() != 1 || h.u.Status().State != CodeAuthRefused {
		t.Fatalf("a refused credential was used %d times", c.count())
	}
	// A new credential is tried once.
	h.cred, c.auth = "rotated", "Bearer rotated"
	h.step(t)
	if c.count() != 2 || h.u.Status().Totals.Batches != 1 {
		t.Fatalf("a new credential must be tried: %+v", h.u.Status())
	}
}

func TestTooManyRequestsWaitsForRetryAfter(t *testing.T) {
	dir := t.TempDir()
	f := newFixture(t, dir, "boot-q")
	f.add(1)
	c := newCloud()
	c.answer = func(n int, w http.ResponseWriter, r *http.Request, body []byte) bool {
		if n > 1 {
			return false
		}
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"code":"health_budget_exhausted","message":"m","details":{"retry_after":120}}}`))
		return true
	}
	h := newHarness(t, f, c, filepath.Join(dir, "upload"))
	if wait := h.step(t); wait != 120*time.Second {
		t.Fatalf("wait %s, want Retry-After", wait)
	}
	h.now = h.now.Add(time.Minute)
	h.step(t)
	if c.count() != 1 || h.u.Status().State != CodeQuotaCloud {
		t.Fatal("a retry before Retry-After")
	}
	h.now = h.now.Add(2 * time.Minute)
	h.step(t)
	if c.count() != 2 || h.u.Status().Totals.Batches != 1 {
		t.Fatal("after Retry-After the batch is resent")
	}
}

func TestATimeoutIsBoundedAndTheBatchIsAbandonedAfterItsAttempts(t *testing.T) {
	dir := t.TempDir()
	f := newFixture(t, dir, "boot-t")
	f.add(1)
	c := newCloud()
	c.answer = func(int, http.ResponseWriter, *http.Request, []byte) bool {
		time.Sleep(time.Second)
		return false
	}
	h := newHarness(t, f, c, filepath.Join(dir, "upload"))
	start := time.Now()
	h.step(t)
	if took := time.Since(start); took > 900*time.Millisecond {
		t.Fatalf("an attempt took %s past its deadline", took)
	}
	for i := 0; i < 2*MaxAttempts; i++ {
		h.now = h.now.Add(10 * time.Minute)
		h.step(t)
	}
	st := h.u.Status()
	if c.count() != MaxAttempts || st.Totals.AbandonedBatches != 1 || st.Totals.AbandonedEvents != 2 {
		t.Fatalf("%d attempts, totals %+v", c.count(), st.Totals)
	}
}

func TestNothingIsSentWhenUploadIsOff(t *testing.T) {
	dir := t.TempDir()
	f := newFixture(t, dir, "boot-o")
	f.add(2)
	for _, code := range []Code{CodeNotOptedIn, CodeCloudOff, CodeNoIdentity, CodeIdentityUnread} {
		var calls int
		u := New(Options{
			StateDir: filepath.Join(dir, "upload"),
			Identity: func() (Identity, error) { return Identity{}, &Error{Code: code} },
			Source:   func() Source { return f.store },
			Transport: roundTrip(func(*http.Request) (*http.Response, error) {
				calls++
				return nil, io.EOF
			}),
		})
		u.Step(context.Background())
		if calls != 0 || u.Status().State != code {
			t.Fatalf("%s: %d requests, state %s", code, calls, u.Status().State)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "upload", stateFile)); !os.IsNotExist(err) {
		t.Fatal("an uploader that is off wrote progress")
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOnlyHTTPSOrAnIsolatedLoopbackIsAnOrigin(t *testing.T) {
	u := New(Options{})
	for _, base := range []string{"http://api.clawdline.com", "http://127.0.0.1:9", "ftp://x", "https://u:p@api.clawdline.com", "https://api.clawdline.com?x=1", ""} {
		if _, err := u.endpoint(base); err == nil {
			t.Errorf("%q accepted", base)
		}
	}
	if e, err := u.endpoint("https://api.clawdline.com/"); err != nil || e != "https://api.clawdline.com"+IngestPath {
		t.Fatalf("https refused: %v %s", err, e)
	}
	u = New(Options{AllowLoopbackHTTP: true})
	if _, err := u.endpoint("http://api.clawdline.com"); err == nil {
		t.Fatal("http to a remote host accepted")
	}
}

func TestTheHourlyBudgetStopsRequestsBeforeCloudDoes(t *testing.T) {
	dir := t.TempDir()
	f := newFixture(t, dir, "boot-h")
	f.add(1)
	c := newCloud()
	h := newHarness(t, f, c, filepath.Join(dir, "upload"))
	h.u.st.Hour = hourBudget{Start: h.now.UTC().Truncate(time.Hour), Requests: MaxHourlyRequests}
	wait := h.step(t)
	if c.count() != 0 || h.u.Status().State != CodeQuotaLocal || wait <= 0 || wait > time.Hour {
		t.Fatalf("over budget: %d requests, wait %s", c.count(), wait)
	}
	h.now = h.now.Add(wait)
	h.step(t)
	if c.count() != 1 {
		t.Fatal("the next hour sends")
	}
}

func TestARefusedBatchIsCountedWithItsCodeAndNotResent(t *testing.T) {
	dir := t.TempDir()
	f := newFixture(t, dir, "boot-z")
	f.add(1)
	c := newCloud()
	c.answer = func(n int, w http.ResponseWriter, r *http.Request, body []byte) bool {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":"invalid_field","message":"events[0].wall secret-value","details":{"field":"events[0].wall"}}}`))
		return true
	}
	h := newHarness(t, f, c, filepath.Join(dir, "upload"))
	h.step(t)
	h.step(t)
	st := h.u.Status()
	if c.count() != 1 || st.Totals.RefusedBatches != 1 || st.LastRefusal == nil || st.LastRefusal.Code != "invalid_field" || st.LastRefusal.Field != "events[0].wall" {
		t.Fatalf("%d requests, %+v", c.count(), st)
	}
	raw, _ := json.Marshal(st)
	if bytes.Contains(raw, []byte("secret-value")) {
		t.Fatal("Cloud's message is echoed")
	}
}

func TestABatchRebuiltAfterALostProgressFileReplaysItsReceipt(t *testing.T) {
	dir := t.TempDir()
	f := newFixture(t, dir, "boot-l")
	f.add(3)
	c := newCloud()
	c.answer = func(n int, w http.ResponseWriter, r *http.Request, body []byte) bool {
		if n != 1 {
			return false
		}
		c.ingest(httptest.NewRecorder(), body)
		panic(http.ErrAbortHandler)
	}
	h := newHarness(t, f, c, filepath.Join(dir, "upload"))
	h.step(t)
	if err := os.Remove(filepath.Join(dir, "upload", stateFile)); err != nil {
		t.Fatal(err)
	}
	h.now = h.now.Add(time.Hour)
	h.u = h.make()
	h.step(t)
	st := h.u.Status()
	if c.count() != 2 || st.LastReceipt == nil || !st.LastReceipt.Replay || st.Totals.RefusedBatches != 0 {
		t.Fatalf("the rebuilt batch must replay, not conflict: %+v", st)
	}
}

// TestWriteABodyForTheCloudParser writes the body this uploader sends for
// real stored records, so Cloud's own parser can be run on it
// (HEALTHUPLOAD_BODY_OUT=<file>; skipped otherwise).
func TestWriteABodyForTheCloudParser(t *testing.T) {
	out := os.Getenv("HEALTHUPLOAD_BODY_OUT")
	if out == "" {
		t.Skip("HEALTHUPLOAD_BODY_OUT is not set")
	}
	dir := t.TempDir()
	f := newFixture(t, dir, "boot-7f3a")
	f.add(40)
	c := newCloud()
	h := newHarness(t, f, c, filepath.Join(dir, "upload"))
	h.step(t)
	if c.count() != 1 {
		t.Fatal("no request")
	}
	if err := os.WriteFile(out, c.bodies[0], 0o600); err != nil {
		t.Fatal(err)
	}
}
