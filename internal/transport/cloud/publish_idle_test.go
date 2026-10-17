package cloud

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// nobodyWatching is a relay with no viewer socket: every publication is
// acknowledged with a fan-out of zero, as the receipt does in production.
func nobodyWatching(p *Publisher, out *collector) {
	p.Publish = func(ctx context.Context, o Outbound) error {
		_ = out.publish(ctx, o)
		p.Acked(o.Channel, 0)
		return nil
	}
}

func countedSessions(p *Publisher, builds *atomic.Int32) {
	p.Sessions = func(context.Context) ([]byte, bool) {
		builds.Add(1)
		return []byte(completeScan), true
	}
}

// With nobody connected, a pass builds no Session list and sends no row; the
// descriptor still goes out on its heartbeat, and the first viewer heard from
// gets every row again on the next pass.
func TestAPublisherNobodyWatchesKeepsOnlyTheDescriptorHeartbeat(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	out := &collector{}
	var builds atomic.Int32
	p := newPublisher(&fixedRouter{body: completeScan}, out)
	p.Now = func() time.Time { return at }
	countedSessions(p, &builds)
	nobodyWatching(p, out)
	ctx := context.Background()

	p.firstPass(ctx)
	if builds.Load() != 1 || !strings.Contains(strings.Join(out.channels(), " "), "ss/mac-01/") {
		t.Fatalf("the first pass built %d lists and published %v", builds.Load(), out.channels())
	}
	if p.Watched() {
		t.Fatal("every receipt fanned out to nobody, yet the publisher believes it is watched")
	}
	out.reset()

	// Five minutes of passes: no list is built, and the only publication is
	// the descriptor's heartbeat.
	for elapsed := time.Duration(0); elapsed < Heartbeat+time.Minute; elapsed += SnapshotInterval {
		at = at.Add(SnapshotInterval)
		p.Pass(ctx)
	}
	if n := builds.Load(); n != 1 {
		t.Fatalf("an unwatched publisher built the Session list %d more times", n-1)
	}
	if got := strings.Join(out.channels(), " "); got != "orch/mac-01" {
		t.Fatalf("an unwatched publisher published %q, want one descriptor heartbeat", got)
	}
	out.reset()

	// A viewer is heard from: the next pass states everything again.
	p.Seen("phone-01")
	at = at.Add(SnapshotInterval)
	p.Pass(ctx)
	got := strings.Join(out.channels(), " ")
	if builds.Load() != 2 || !strings.Contains(got, "ss/mac-01/") || !strings.Contains(got, InventorySessionID) {
		t.Fatalf("after a viewer was heard from: %d builds, published %q", builds.Load(), got)
	}
}

// A page that connects after a long idle period asks `sessions.snapshot`
// first, and is answered at once with a list built then and every row on its
// channel before the answer names them.
func TestAPageConnectingAfterAnIdlePeriodGetsACompleteSnapshotAtOnce(t *testing.T) {
	out := &collector{}
	var builds atomic.Int32
	p := newPublisher(&fixedRouter{body: completeScan}, out)
	p.Every = 20 * time.Millisecond
	countedSessions(p, &builds)
	nobodyWatching(p, out)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { _ = p.Run(ctx); close(done) }()
	waitFor(t, "the first pass", func() bool { return builds.Load() == 1 && !p.Watched() })
	// Many idle passes.
	time.Sleep(15 * p.Every)
	if n := builds.Load(); n != 1 {
		t.Fatalf("idle passes built the Session list %d more times", n-1)
	}
	out.reset()

	asked := time.Now()
	stated, refusal := p.SnapshotFrom(ctx, "phone-01", true)
	waited := time.Since(asked)
	if refusal != nil {
		t.Fatalf("refused: %+v", *refusal)
	}
	if strings.Join(stated.IDs, ",") != "%19" || !stated.Complete {
		t.Fatalf("answered %+v", stated)
	}
	if waited > time.Second {
		t.Fatalf("the snapshot waited %s", waited)
	}
	if builds.Load() != 2 {
		t.Fatalf("the snapshot was answered from %d builds, want one built for it", builds.Load()-1)
	}
	got := strings.Join(out.channels(), " ")
	for _, want := range []string{"orch/mac-01", "s/mac-01/%2519", "ss/mac-01/", "s/mac-01/" + InventorySessionID} {
		if !strings.Contains(got, want) {
			t.Fatalf("before the answer the machine published %q, missing %s", got, want)
		}
	}
	cancel()
	<-done
}
