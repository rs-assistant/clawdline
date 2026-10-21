package healthsealed

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// lossyCloud wraps fakeCloud: the first request is held until release is
// closed, and the first `lose` requests are stored by Cloud but answered
// 503, as a receipt lost on the way back. It records every status Cloud
// itself answered and every body sent per revision.
type lossyCloud struct {
	cloud    *fakeCloud
	release  chan struct{}
	lose     int64
	n        atomic.Int64
	mu       sync.Mutex
	statuses []int
	bodies   map[int64]map[string]bool
}

func (l *lossyCloud) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n := l.n.Add(1)
	if n == 1 && l.release != nil {
		<-l.release
	}
	rec := httptest.NewRecorder()
	raw := readAll(r)
	r2 := r.Clone(r.Context())
	r2.Body = nopBody(raw)
	l.cloud.ServeHTTP(rec, r2)
	var b struct {
		Revision int64 `json:"revision"`
	}
	_ = jsonUnmarshal(raw, &b)
	l.mu.Lock()
	l.statuses = append(l.statuses, rec.Code)
	if l.bodies[b.Revision] == nil {
		l.bodies[b.Revision] = map[string]bool{}
	}
	l.bodies[b.Revision][string(raw)] = true
	l.mu.Unlock()
	if n <= l.lose {
		w.WriteHeader(503)
		_, _ = w.Write([]byte(`{"error":{"code":"health_sealed_deadline","message":"lost"}}`))
		return
	}
	for k, v := range rec.Header() {
		w.Header()[k] = v
	}
	w.WriteHeader(rec.Code)
	_, _ = w.Write(rec.Body.Bytes())
}

// check asserts Cloud never answered a conflict and every revision was
// sent with one exact body, however many times.
func (l *lossyCloud) check(t *testing.T) {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, s := range l.statuses {
		if s == http.StatusConflict {
			t.Fatalf("Cloud answered 409: statuses %v", l.statuses)
		}
	}
	for rev, set := range l.bodies {
		if len(set) != 1 {
			t.Fatalf("revision %d was sent with %d different bodies", rev, len(set))
		}
	}
}

func newLossy(t *testing.T, lose int64, hold bool) (*lossyCloud, *httptest.Server) {
	v, _ := loadVector(t)
	l := &lossyCloud{cloud: newFakeCloud(v.Tenant, v.MachineID), lose: lose, bodies: map[int64]map[string]bool{}}
	if hold {
		l.release = make(chan struct{})
	}
	srv := httptest.NewServer(l)
	t.Cleanup(srv.Close)
	return l, srv
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// A stall's first upload is held and its receipt lost; the capture and the
// recovery offered meanwhile still become revisions 2 and 3, after the
// original's receipt, and revision 1 is never resent with other bytes.
func TestHeldFirstUploadKeepsCaptureAndRecovery(t *testing.T) {
	l, srv := newLossy(t, MaxAttempts, true)
	var on atomic.Bool
	on.Store(true)
	u := newUploader(t, srv, testKeys(t), &on)
	w := NewWorker(u)
	w.Interval = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	job := func(loc string) Job {
		return Job{BootID: "boot-1", SubjectKind: SubjectIncident, SubjectID: "inc-1", Snapshot: snap(loc)}
	}
	if err := w.Offer(job("stall.go:1")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the first request to be held", func() bool { return l.n.Load() >= 1 })
	for _, loc := range []string{"capture.go:2", "recovery.go:3"} {
		if err := w.Offer(job(loc)); err != nil {
			t.Fatalf("offer %s while the first upload is held: %v", loc, err)
		}
	}
	close(l.release)
	waitFor(t, "three stored revisions", func() bool {
		l.cloud.mu.Lock()
		defer l.cloud.mu.Unlock()
		for _, revs := range l.cloud.revs {
			return len(revs) == 3
		}
		return false
	})
	l.check(t)
	waitFor(t, "status to settle", func() bool { return w.Status().Stored >= 2 && w.Status().Queued == 0 })
	if st := w.Status(); st.Dropped != 0 || st.Refused != 0 {
		t.Fatalf("a revision was lost: %+v", st)
	}
	ids, _ := u.Store.IDs()
	a, _, _ := u.Store.Load(ids[0])
	for _, r := range a.Revisions {
		if r.State != RevisionStored {
			t.Fatalf("revision %d is %s", r.Revision, r.State)
		}
	}
	for i, want := range []string{"stall.go:1", "capture.go:2", "recovery.go:3"} {
		got := openLocation(t, l.cloud, ids[0], int64(i+1))
		if got != want {
			t.Fatalf("revision %d holds %q, want %q", i+1, got, want)
		}
	}
}

// Revisions queued behind a pending one survive a restart with their exact
// bytes, and are delivered in order by the next process.
func TestRestartPreservesQueuedRevisions(t *testing.T) {
	l, srv := newLossy(t, MaxAttempts, false)
	var on atomic.Bool
	on.Store(true)
	u := newUploader(t, srv, testKeys(t), &on)
	ctx := context.Background()
	p, err := u.Seal(ctx, "boot-1", SubjectIncident, "inc-1", snap("stall.go:1"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := u.Send(ctx, p.AttachmentID); CodeOf(err) != CodeUpstream {
		t.Fatalf("lost receipts: %v", err)
	}
	before, _, _ := u.Store.Load(p.AttachmentID)
	p2, err := u.Seal(ctx, "boot-1", SubjectIncident, "inc-1", snap("capture.go:2"))
	if err != nil || p2.Revision != 2 {
		t.Fatalf("capture behind a pending revision: %+v %v", p2, err)
	}
	queued, _, _ := u.Store.Load(p.AttachmentID)
	if string(queued.Revisions[0].Body) != string(before.Revisions[0].Body) {
		t.Fatal("queueing rewrote the pending revision's body")
	}
	if queued.Revisions[1].State != RevisionPending ||
		queued.Revisions[1].Body == nil {
		t.Fatalf("queued revision not on disk: %+v", queued.Revisions[1])
	}
	// Restart: a new store and worker on the same directory.
	store2, err := OpenStore(u.Store.dir)
	if err != nil {
		t.Fatal(err)
	}
	u2 := *u
	u2.Store = store2
	w := NewWorker(&u2)
	w.Refresh()
	w.resume(ctx)
	after, _, _ := store2.Load(p.AttachmentID)
	for _, r := range after.Revisions {
		if r.State != RevisionStored {
			t.Fatalf("after restart revision %d is %s", r.Revision, r.State)
		}
	}
	if string(after.Revisions[1].Body) != string(queued.Revisions[1].Body) {
		t.Fatal("restart changed the queued revision's body")
	}
	l.check(t)
}

// The queue is bounded by MaxRevisions; the next snapshot is refused at
// once and counted as dropped with its code, never retried.
func TestFullOutboxIsExplicitLoss(t *testing.T) {
	_, srv := newLossy(t, 1<<30, false)
	var on atomic.Bool
	on.Store(true)
	u := newUploader(t, srv, testKeys(t), &on)
	w := NewWorker(u)
	ctx := context.Background()
	for i := 0; i < MaxRevisions; i++ {
		if _, err := u.Seal(ctx, "boot-1", SubjectIncident, "inc-1", snap("x.go:1")); err != nil {
			t.Fatalf("seal %d: %v", i+1, err)
		}
	}
	w.handle(ctx, Job{BootID: "boot-1", SubjectKind: SubjectIncident, SubjectID: "inc-1", Snapshot: snap("late.go:9")})
	st := w.Status()
	if st.Dropped != 1 || st.Code != CodeRevisionsExhausted {
		t.Fatalf("a snapshot past the bound was not reported lost: %+v", st)
	}
}

// A permanent refusal of the pending revision refuses the chain behind it
// locally, and no new snapshot is chained to a refused attachment.
func TestRefusedPredecessorRefusesQueue(t *testing.T) {
	l, srv := newLossy(t, 0, false)
	var on atomic.Bool
	on.Store(true)
	u := newUploader(t, srv, testKeys(t), &on)
	ctx := context.Background()
	p, _ := u.Seal(ctx, "boot-1", SubjectIncident, "inc-1", snap("a.go:1"))
	if _, err := u.Seal(ctx, "boot-1", SubjectIncident, "inc-1", snap("b.go:2")); err != nil {
		t.Fatal(err)
	}
	l.cloud.mu.Lock()
	l.cloud.force = []int{410}
	l.cloud.mu.Unlock()
	if _, err := u.Send(ctx, p.AttachmentID); CodeOf(err) != CodeDeleted {
		t.Fatalf("send: %v", err)
	}
	a, _, _ := u.Store.Load(p.AttachmentID)
	if a.Revisions[0].State != RevisionRefused || a.Revisions[1].State != RevisionRefused ||
		a.Revisions[1].Refusal != string(CodePredecessorRefused) {
		t.Fatalf("chain after refusal: %+v", a.Revisions)
	}
	if n := l.n.Load(); n != 1 {
		t.Fatalf("the refused chain was sent: %d requests", n)
	}
	if _, err := u.Seal(ctx, "boot-1", SubjectIncident, "inc-1", snap("c.go:3")); CodeOf(err) != CodeRefused {
		t.Fatalf("seal after refusal: %v", err)
	}
}

// After the content key rotates, queued material sealed under the old
// epoch is refused locally and never sent; with consent off nothing is
// sent and the queue stays as it was.
func TestEpochRotationAndConsentOffSendNothing(t *testing.T) {
	l, srv := newLossy(t, 0, false)
	var on atomic.Bool
	on.Store(true)
	keys := testKeys(t)
	u := newUploader(t, srv, keys, &on)
	ctx := context.Background()
	p, _ := u.Seal(ctx, "boot-1", SubjectIncident, "inc-1", snap("a.go:1"))
	_, _ = u.Seal(ctx, "boot-1", SubjectIncident, "inc-1", snap("b.go:2"))

	on.Store(false)
	if _, err := u.Send(ctx, p.AttachmentID); CodeOf(err) != CodeDisabled {
		t.Fatalf("send while off: %v", err)
	}
	a, _, _ := u.Store.Load(p.AttachmentID)
	if a.Revisions[0].State != RevisionPending || a.Revisions[1].State != RevisionPending {
		t.Fatalf("consent off changed the queue: %+v", a.Revisions)
	}

	on.Store(true)
	keys.mu.Lock()
	keys.keys.ContentKeyEpoch = 2
	keys.mu.Unlock()
	if _, err := u.Send(ctx, p.AttachmentID); CodeOf(err) != CodeWrongEpoch {
		t.Fatalf("send after rotation: %v", err)
	}
	a, _, _ = u.Store.Load(p.AttachmentID)
	if a.Revisions[0].State != RevisionRefused || a.Revisions[1].State != RevisionRefused {
		t.Fatalf("old-epoch material still queued: %+v", a.Revisions)
	}
	if n := l.n.Load(); n != 0 {
		t.Fatalf("%d requests carried old-epoch or opted-out material", n)
	}
}

func readAll(r *http.Request) []byte {
	raw, _ := io.ReadAll(r.Body)
	return raw
}

func nopBody(raw []byte) io.ReadCloser { return io.NopCloser(bytes.NewReader(raw)) }

func jsonUnmarshal(raw []byte, v any) error { return json.Unmarshal(raw, v) }

// openLocation opens one stored revision as the owner would and answers its
// location.
func openLocation(t *testing.T, c *fakeCloud, id string, rev int64) string {
	t.Helper()
	v, _ := loadVector(t)
	got, err := OpenReadResponse(c.read(id, rev), Expect{Tenant: v.Tenant, AttachmentID: id, Revision: rev}, vectorMaster(t, v))
	if err != nil {
		t.Fatalf("open revision %d: %v", rev, err)
	}
	return got.Location
}
