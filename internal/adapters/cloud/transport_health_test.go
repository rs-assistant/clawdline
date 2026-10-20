package cloud

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	domain "github.com/sainteye/clawdline/internal/domain/cloud"
	"github.com/sainteye/clawdline/internal/domain/connectionhealth"
)

// stepClock is a connectionhealth.Clock a test moves by hand.
type stepClock struct {
	mu  sync.Mutex
	now connectionhealth.Instant
}

func newStepClock() *stepClock {
	return &stepClock{now: connectionhealth.Instant{Wall: time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)}}
}

func (c *stepClock) Now() connectionhealth.Instant {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *stepClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now.Mono += d
	c.now.Wall = c.now.Wall.Add(d)
	c.mu.Unlock()
}

func healthRig(t *testing.T) (*stepClock, *connectionhealth.Tracker, *connectionhealth.Journal, *connectionhealth.Watchdog) {
	t.Helper()
	c := newStepClock()
	tr := connectionhealth.NewTracker(c)
	j, err := connectionhealth.NewJournal(c, "boot-t", "abc123")
	if err != nil {
		t.Fatal(err)
	}
	return c, tr, j, connectionhealth.NewWatchdog(tr, j, c, time.Second)
}

// An inbound callback that never returns, on a line whose socket stays open,
// is found by a scan that touches nothing of the transport: the phase is at
// lane_queued in cloud.transport.inbound_callback, blocked past 15 s, under
// an opaque correlation the frame did not supply. Releasing the callback ends
// the phase and frees its slot.
func TestABlockedInboundCallbackIsFoundWithItsStageAndLocation(t *testing.T) {
	clock, tr, j, w := healthRig(t)
	viewer, err := domain.NewDeviceKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	master, err := domain.NewContentKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	transport := &Transport{opts: Options{
		Role:         "machine",
		Identity:     Identity{MachineID: "machine"},
		Status:       NewStatusRecorder(time.Now()),
		Replay:       domain.NewReplayWindow(64),
		ContentKey:   master,
		PublicKeyFor: func(string) (ed25519.PublicKey, bool) { return viewer.PublicKey(), true },
		Inbound: func(domain.Envelope, []byte, ed25519.PublicKey) {
			close(entered)
			<-release
		},
		Health: tr,
	}}
	env, err := domain.Seal([]byte(`{"v":1}`), domain.SealParams{Ch: "ctl/machine", Seq: 1, Ts: uint64(time.Now().UnixMilli()),
		Class: domain.ClassCtl, KeyID: MasterKeyID, Sender: "viewer", Key: master, Signer: viewer})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := env.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	frame, _ := json.Marshal(EnvelopeFrame{Type: FrameEnvelope, Envelope: raw})
	done := make(chan error)
	go func() { done <- transport.handle(nil, frame) }()
	<-entered

	clock.advance(16 * time.Second)
	r := w.Scan()
	if len(r.Findings) != 1 {
		t.Fatalf("findings: %+v", r.Findings)
	}
	f := r.Findings[0]
	if f.State != connectionhealth.StateBlocked || f.Stage != connectionhealth.StageLaneQueued ||
		f.Location != connectionhealth.LocInboundCallback || f.AgeNS < int64(15*time.Second) ||
		!strings.HasPrefix(f.Correlation, "op-") || strings.Contains(f.Correlation, "viewer") {
		t.Fatalf("the blocked callback: %+v", f)
	}
	ev, err := connectionhealth.Export(&r, j, connectionhealth.Query{})
	if err != nil || ev.Journal.Build != "abc123" {
		t.Fatalf("export: %v", err)
	}

	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if phases, _ := tr.Snapshot(); len(phases) != 0 {
		t.Fatalf("the phase outlived its frame: %+v", phases)
	}
	completed, _ := tr.Counts()
	if completed[connectionhealth.ClassTransport] != 1 {
		t.Fatalf("completions: %+v", completed)
	}
}

// The sender lookup is its own location: a roster read that hangs is told
// apart from a callback that hangs.
func TestAHangingSenderLookupIsItsOwnLocation(t *testing.T) {
	clock, tr, _, w := healthRig(t)
	viewer, _ := domain.NewDeviceKey(rand.Reader)
	master, _ := domain.NewContentKey(rand.Reader)
	entered, release := make(chan struct{}), make(chan struct{})
	transport := &Transport{opts: Options{
		Role: "machine", Identity: Identity{MachineID: "machine"}, Status: NewStatusRecorder(time.Now()),
		Replay: domain.NewReplayWindow(64), ContentKey: master,
		PublicKeyFor: func(string) (ed25519.PublicKey, bool) { close(entered); <-release; return viewer.PublicKey(), true },
		Inbound:      func(domain.Envelope, []byte, ed25519.PublicKey) {},
		Health:       tr,
	}}
	env, _ := domain.Seal([]byte(`{}`), domain.SealParams{Ch: "ctl/machine", Seq: 1, Ts: uint64(time.Now().UnixMilli()),
		Class: domain.ClassCtl, KeyID: MasterKeyID, Sender: "viewer", Key: master, Signer: viewer})
	raw, _ := env.CanonicalJSON()
	frame, _ := json.Marshal(EnvelopeFrame{Type: FrameEnvelope, Envelope: raw})
	done := make(chan error)
	go func() { done <- transport.handle(nil, frame) }()
	<-entered
	clock.advance(6 * time.Second)
	r := w.Scan()
	if len(r.Findings) != 1 || r.Findings[0].Location != connectionhealth.LocSenderLookup ||
		r.Findings[0].State != connectionhealth.StateSlow || r.Findings[0].Stage != connectionhealth.StageFrameRead {
		t.Fatalf("the hanging lookup: %+v", r.Findings)
	}
	close(release)
	<-done
}

// With no tracker the transport behaves exactly as before: the frame is
// handled and nothing is recorded.
func TestATransportWithoutHealthIsUnchanged(t *testing.T) {
	got := 0
	transport := &Transport{opts: Options{PeerInbound: func([]byte) { got++ }, Status: NewStatusRecorder(time.Now())}}
	if err := transport.handle(nil, []byte(`{"type":"peer_envelope"}`)); err != nil || got != 1 {
		t.Fatalf("handled %d, %v", got, err)
	}
}

// Every location the map assigns to this package names a function that
// exists in this build, read from the function itself rather than typed.
func TestTheLocationMapNamesThisPackagesRealFunctions(t *testing.T) {
	name := func(f any) string { return runtime.FuncForPC(reflect.ValueOf(f).Pointer()).Name() }
	real := map[string]bool{
		name((*Transport).read): true, name((*Transport).keepalive): true, name((*Transport).drain): true,
		name((*Transport).handle): true, name((*Transport).handleFrame): true, name((*Transport).admitTraced): true,
		name((*Transport).drainPass): true, name((*Conn).writeFrame): true,
	}
	n := 0
	for _, e := range connectionhealth.Locations() {
		if !strings.HasPrefix(e.Code, "cloud.transport.") && !strings.HasPrefix(e.Code, "cloud.websocket.") {
			continue
		}
		n++
		if !real[e.Function] {
			t.Errorf("%s names %s, which is not a function of this package", e.Code, e.Function)
		}
		if !connectionhealth.ValidLocation(e.Code) {
			t.Errorf("%s is not a location code", e.Code)
		}
	}
	if n == 0 {
		t.Fatal("no locations checked")
	}
}
