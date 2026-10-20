package cloud

import (
	"crypto/ed25519"
	"crypto/rand"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
	"github.com/sainteye/clawdline/internal/domain/connectionhealth"
)

// Every location the map assigns to the link names a function of this
// package in this build, read from the function itself.
func TestTheLinkLocationsNameThisPackagesRealFunctions(t *testing.T) {
	name := func(f any) string { return runtime.FuncForPC(reflect.ValueOf(f).Pointer()).Name() }
	real := map[string]bool{name((*Link).lanePhase): true, name((*Link).answerTraced): true}
	n := 0
	for _, e := range connectionhealth.Locations() {
		if !strings.HasPrefix(e.Code, "cloud.link.") {
			continue
		}
		n++
		if !real[e.Function] {
			t.Errorf("%s names %s, which is not a function of this package", e.Code, e.Function)
		}
	}
	if n != 6 {
		t.Fatalf("%d link locations", n)
	}
}

// A request put on a lane gets a phase with a fresh opaque correlation and
// the lane's class; a link without a tracker records nothing.
func TestALanePhaseIsOpaqueAndOptional(t *testing.T) {
	off := &Link{}
	if h := off.lanePhase(Inbound{}, connectionhealth.ClassRead, connectionhealth.LocReadLane); h.Recorded() {
		t.Fatal("a link without a tracker recorded a phase")
	}
	tr := connectionhealth.NewTracker(connectionhealth.NewSystemClock())
	l := &Link{opts: LinkOptions{Health: tr}}
	a := l.lanePhase(Inbound{}, connectionhealth.ClassControl, connectionhealth.LocCommandLane)
	b := l.lanePhase(Inbound{}, connectionhealth.ClassRead, connectionhealth.LocReadLane)
	phases, _ := tr.Snapshot()
	if len(phases) != 2 {
		t.Fatalf("phases: %+v", phases)
	}
	seen := map[string]bool{}
	for _, p := range phases {
		if p.Spec.Stage != connectionhealth.StageLaneQueued || !strings.HasPrefix(p.Spec.Correlation, "op-") ||
			p.Spec.Warn != connectionhealth.DaemonWarn || p.Spec.Block != connectionhealth.DaemonBlock || p.Spec.Critical {
			t.Fatalf("a lane phase: %+v", p.Spec)
		}
		seen[p.Spec.Correlation] = true
	}
	if len(seen) != 2 {
		t.Fatal("two requests shared a correlation")
	}
	_ = a.Step(connectionhealth.StageHandlerStarted, connectionhealth.LocCommandHandler)
	a.End(true)
	b.End(false)
	completed, failed := tr.Counts()
	if completed[connectionhealth.ClassControl] != 1 || failed[connectionhealth.ClassRead] != 1 {
		t.Fatalf("counts: %+v %+v", completed, failed)
	}
}

// One logical operation keeps one correlation from the transport's frame
// phase, through the relay queue, to the lane and handler: the lane does not
// mint a new id, and the id is the tracker's counter, not the envelope's.
func TestOneCorrelationRunsFromTheFrameThroughTheRelayToTheLane(t *testing.T) {
	machine, _ := domaincloud.NewDeviceKey(rand.Reader)
	viewer, _ := domaincloud.NewDeviceKey(rand.Reader)
	secret, _ := domaincloud.NewContentKey(rand.Reader)
	tracker := connectionhealth.NewTracker(connectionhealth.NewSystemClock())
	relay := &Relay{MachineID: "machine", Depth: 4}
	transport, err := adaptercloud.New(adaptercloud.Options{
		RelayURL: "wss://relay.invalid", Token: &adaptercloud.TokenSource{},
		Identity: adaptercloud.Identity{AccountID: "account", MachineID: "machine", MachineCredential: "credential"},
		Signer:   machine, Replay: domaincloud.NewReplayWindow(64), ContentKey: secret,
		PublicKeyFor:  func(sender string) (ed25519.PublicKey, bool) { return viewer.PublicKey(), sender == "viewer" },
		Inbound:       relay.Deliver,
		DirectInbound: relay.DeliverDirect,
		InboundTraced: relay.DeliverTraced,
		Health:        tracker,
	})
	if err != nil {
		t.Fatal(err)
	}
	env, err := domaincloud.Seal([]byte(`{"v":1,"op":"read"}`), domaincloud.SealParams{Ch: "r/machine", Seq: 7,
		Ts: uint64(time.Now().UnixMilli()), Class: domaincloud.ClassCtl, KeyID: adaptercloud.MasterKeyID,
		Sender: "viewer", Key: secret, Signer: viewer})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := env.CanonicalJSON()
	if !transport.AcceptDirect("viewer", raw) {
		t.Fatal("the envelope was not admitted")
	}
	var in Inbound
	select {
	case in = <-relay.Requests():
	default:
		t.Fatal("nothing reached the relay queue")
	}
	if in.Correlation != "op-1" || !in.Direct {
		t.Fatalf("the request carries %q (direct %t), want the frame phase's op-1", in.Correlation, in.Direct)
	}
	l := &Link{opts: LinkOptions{Health: tracker}}
	h := l.lanePhase(in, connectionhealth.ClassRead, connectionhealth.LocReadLane)
	phases, _ := tracker.Snapshot()
	if len(phases) != 1 || phases[0].Spec.Correlation != "op-1" || h.Correlation() != "op-1" {
		t.Fatalf("the lane phase: %+v", phases)
	}
	_ = h.Step(connectionhealth.StageHandlerStarted, connectionhealth.LocReadHandler)
	if phases, _ := tracker.Snapshot(); phases[0].Spec.Correlation != "op-1" || phases[0].Spec.Stage != connectionhealth.StageHandlerStarted {
		t.Fatalf("the handler step: %+v", phases[0])
	}
	h.End(true)
	if next := tracker.NewCorrelation(); next != "op-2" {
		t.Fatalf("the chain minted extra ids; next is %s", next)
	}
}
