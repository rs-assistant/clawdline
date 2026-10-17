package terminals

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/terminal"
)

type idleTerminalHost struct {
	ports.OwnedTerminals
	offline atomic.Bool
}

type changingTerminalHost struct {
	idleTerminalHost
	revision atomic.Int32
	wake     chan struct{}
}

func (h *changingTerminalHost) Frame(context.Context, terminal.ID) (terminal.Frame, error) {
	rev := "first"
	if h.revision.Load() > 0 {
		rev = "second"
	}
	return terminal.Frame{Rev: rev, At: time.Now(), Cols: 80, Rows: 24, Lines: []string{rev}}, nil
}

func (h *changingTerminalHost) Changed(context.Context, terminal.ID) (<-chan struct{}, func(), error) {
	return h.wake, func() {}, nil
}

func TestDeferredCloudFrameIsRetriedBeforeHeartbeat(t *testing.T) {
	host := &changingTerminalHost{wake: make(chan struct{}, 1)}
	svc := New(host, func(Principal) error { return nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watch, err := svc.Watch(ctx, Principal{Device: "viewer", Cloud: true}, terminal.NewID(), "tab")
	if err != nil {
		t.Fatal(err)
	}
	defer watch.Stop()
	frames := make(chan string, 4)
	done := make(chan error, 1)
	var deferred atomic.Bool
	go func() {
		done <- watch.RunWithFrameHeartbeat(ctx, 3*time.Second, func(e Event) error {
			if e.Kind != EventFrame {
				return nil
			}
			if e.Frame.Rev == "second" && !deferred.Swap(true) {
				return ErrFrameDeferred
			}
			frames <- e.Frame.Rev
			return nil
		})
	}()
	select {
	case <-frames:
	case <-time.After(time.Second):
		t.Fatal("first frame missing")
	}
	host.revision.Store(1)
	host.wake <- struct{}{}
	select {
	case rev := <-frames:
		if rev != "second" {
			t.Fatalf("unexpected frame: %s", rev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("deferred frame waited for heartbeat")
	}
	cancel()
	<-done
}

func (h *idleTerminalHost) Frame(context.Context, terminal.ID) (terminal.Frame, error) {
	if h.offline.Load() {
		return terminal.Frame{}, terminal.Refuse(terminal.CodeUnreachable, "terminal server offline")
	}
	return terminal.Frame{Rev: "still", At: time.Now(), Cols: 80, Rows: 24, Lines: []string{"$ "}}, nil
}

func (*idleTerminalHost) Changed(context.Context, terminal.ID) (<-chan struct{}, func(), error) {
	return make(chan struct{}), func() {}, nil
}

func TestCloudFrameHeartbeatKeepsIdleShellFreshAndStopsOffline(t *testing.T) {
	host := &idleTerminalHost{}
	svc := New(host, func(Principal) error { return nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watch, err := svc.Watch(ctx, Principal{Device: "viewer", Cloud: true}, terminal.NewID(), "tab")
	if err != nil {
		t.Fatal(err)
	}
	defer watch.Stop()
	// The heartbeat is a parameter, so a short one proves the same thing as
	// the production interval: a still screen is sent again on every beat.
	// It stays above pollEvery, which is how often the screen is read.
	const heartbeat = 300 * time.Millisecond
	const beats = 4
	events := make(chan Event, 32)
	done := make(chan error, 1)
	go func() {
		done <- watch.RunWithFrameHeartbeat(ctx, heartbeat, func(e Event) error { events <- e; return nil })
	}()
	var first, last time.Time
	frames := 0
	deadline := time.After(10 * time.Second)
	for frames < beats+1 {
		select {
		case e := <-events:
			if e.Kind != EventFrame {
				continue
			}
			if e.Frame.Rev != "still" {
				t.Fatalf("idle screen changed revision: %q", e.Frame.Rev)
			}
			if !e.Frame.At.After(last) && frames > 0 {
				t.Fatal("heartbeat did not recapture the complete screen")
			}
			if frames == 0 {
				first = e.Frame.At
			}
			last = e.Frame.At
			frames++
		case <-deadline:
			t.Fatalf("idle shell yielded only %d frames in 10 seconds", frames)
		}
	}
	if last.Sub(first) < beats*heartbeat {
		t.Fatalf("did not cover %d heartbeats: %s", beats, last.Sub(first))
	}
	host.offline.Store(true)
	offlineDeadline := time.After(5 * time.Second)
	for {
		select {
		case e := <-events:
			if e.Kind == EventFrame {
				t.Fatal("offline terminal emitted a fresh frame")
			}
			if e.Kind == EventState && e.Status == terminal.Unreachable {
				select {
				case e := <-events:
					if e.Kind == EventFrame {
						t.Fatal("offline terminal emitted a heartbeat")
					}
				case <-time.After(4 * heartbeat):
				}
				cancel()
				<-done
				return
			}
		case <-offlineDeadline:
			t.Fatal("offline terminal did not report unreachable")
		}
	}
}

func TestUnverifiedAccessPausesAStreamInsteadOfRevokingIt(t *testing.T) {
	host := &changingTerminalHost{wake: make(chan struct{}, 1)}
	var unverified atomic.Bool
	svc := New(host, func(Principal) error {
		if unverified.Load() {
			return terminal.Refuse(terminal.CodeBusy, "the device roster could not be read")
		}
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watch, err := svc.Watch(ctx, Principal{Device: "viewer", Cloud: true}, terminal.NewID(), "tab")
	if err != nil {
		t.Fatal(err)
	}
	defer watch.Stop()
	unverified.Store(true)
	events := make(chan Event, 16)
	done := make(chan error, 1)
	go func() { done <- watch.Run(ctx, func(e Event) error { events <- e; return nil }) }()
	svc.Revalidate()
	host.revision.Store(1)
	host.wake <- struct{}{}
	paused := time.After(700 * time.Millisecond)
waiting:
	for {
		select {
		case e := <-events:
			if e.Kind == EventRefusal || e.Kind == EventFrame {
				t.Fatalf("an unverified viewer got a %s event: %+v", e.Kind, e)
			}
		case err := <-done:
			t.Fatalf("an unverified viewer's stream ended: %v", err)
		case <-paused:
			break waiting
		}
	}
	unverified.Store(false)
	host.wake <- struct{}{}
	for {
		select {
		case e := <-events:
			if e.Kind == EventRefusal {
				t.Fatalf("a verified viewer was refused: %+v", e)
			}
			if e.Kind == EventFrame {
				if e.Frame.Rev != "second" {
					t.Fatalf("resumed with %q, want the newest screen", e.Frame.Rev)
				}
				cancel()
				<-done
				return
			}
		case <-time.After(2 * time.Second):
			t.Fatal("the stream did not resume once access was verified")
		}
	}
}

// countingTerminalHost draws a new revision whenever the test says so and
// counts how often the screen is read.
type countingTerminalHost struct {
	idleTerminalHost
	revision atomic.Int32
	reads    atomic.Int32
	wake     chan struct{}
}

func (h *countingTerminalHost) Frame(context.Context, terminal.ID) (terminal.Frame, error) {
	h.reads.Add(1)
	rev := h.revision.Load()
	return terminal.Frame{Rev: string(rune('a' + rev)), At: time.Now(), Cols: 80, Rows: 24}, nil
}

func (h *countingTerminalHost) Changed(context.Context, terminal.ID) (<-chan struct{}, func(), error) {
	return h.wake, func() {}, nil
}

// A Cloud viewer of a terminal whose adapter signals drawing gets every
// change from the signal, and a still screen is not read every pollEvery
// for the sake of the frame heartbeat.
func TestCloudViewerFollowsTheChangeSignalWithoutSampling(t *testing.T) {
	host := &countingTerminalHost{wake: make(chan struct{}, 1)}
	svc := New(host, func(Principal) error { return nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watch, err := svc.Watch(ctx, Principal{Device: "viewer", Cloud: true}, terminal.NewID(), "tab")
	if err != nil {
		t.Fatal(err)
	}
	defer watch.Stop()
	frames := make(chan string, 16)
	done := make(chan error, 1)
	go func() {
		done <- watch.RunWithFrameHeartbeat(ctx, 3*time.Second, func(e Event) error {
			if e.Kind == EventFrame {
				frames <- e.Frame.Rev
			}
			return nil
		})
	}()
	receive := func(want string) {
		t.Helper()
		select {
		case rev := <-frames:
			if rev != want {
				t.Fatalf("frame %q, want %q", rev, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("frame %q missing", want)
		}
	}
	receive("a")
	const changes = 5
	for i := 1; i <= changes; i++ {
		host.revision.Store(int32(i))
		host.wake <- struct{}{}
		receive(string(rune('a' + i)))
	}
	before := host.reads.Load()
	time.Sleep(4 * pollEvery)
	if idle := host.reads.Load() - before; idle != 0 {
		t.Fatalf("a still screen was read %d times in %s", idle, 4*pollEvery)
	}
	if reads := host.reads.Load(); reads != 1+changes {
		t.Fatalf("read the screen %d times for %d changes", reads, changes)
	}
	cancel()
	<-done
}
