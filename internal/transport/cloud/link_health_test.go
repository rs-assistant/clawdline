package cloud

import (
	"reflect"
	"runtime"
	"strings"
	"testing"

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
	if h := off.lanePhase(connectionhealth.ClassRead, connectionhealth.LocReadLane); h.Recorded() {
		t.Fatal("a link without a tracker recorded a phase")
	}
	tr := connectionhealth.NewTracker(connectionhealth.NewSystemClock())
	l := &Link{opts: LinkOptions{Health: tr}}
	a := l.lanePhase(connectionhealth.ClassControl, connectionhealth.LocCommandLane)
	b := l.lanePhase(connectionhealth.ClassRead, connectionhealth.LocReadLane)
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
