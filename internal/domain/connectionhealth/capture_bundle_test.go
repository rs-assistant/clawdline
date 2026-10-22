package connectionhealth

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// wireConn is a stand-in connection for the synthetic bundle: its writer
// lock and a socket write that can be held.
type wireConn struct {
	mu     sync.Mutex
	socket chan struct{}
}

//go:noinline
func (c *wireConn) socketWrite() { <-c.socket }

//go:noinline
func (c *wireConn) writeFrame(entered *sync.WaitGroup) {
	entered.Done()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.socketWrite()
}

// TestWriteSyntheticBundle writes the synthetic evidence bundle the root hands
// to an agent for blind analysis: a real local export of a runtime with one
// injected fault and unrelated activity around it. It runs only with
// CLAWDLINE_SYNTHETIC_BUNDLE_DIR set; the answer is not written here.
func TestWriteSyntheticBundle(t *testing.T) {
	dir := os.Getenv("CLAWDLINE_SYNTHETIC_BUNDLE_DIR")
	if dir == "" {
		t.Skip("set CLAWDLINE_SYNTHETIC_BUNDLE_DIR to write the bundle")
	}
	c := newFakeClock()
	rt, err := newRuntime(c, "boot-5b1e0c2a9d7f4e31", "c0ffee1d2e3f", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	conn := &wireConn{socket: make(chan struct{})}
	rt.Capture.locations = func() []LocationEntry {
		out := []LocationEntry{}
		for _, e := range Locations() {
			switch e.Code {
			case LocWriterLock:
				e.Function = funcName((*wireConn).writeFrame)
			case LocSocketWrite:
				e.Function = funcName((*wireConn).socketWrite)
			}
			out = append(out, e)
		}
		return out
	}
	rt.Capture.SetEnabled(true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go rt.Capture.Run(ctx)

	tr := rt.Tracker
	reader := tr.Start(LoopSpec(LocReadLoop))
	drain := tr.Start(LoopSpec(LocDrainLoop))
	defer reader.End(true)
	defer drain.End(true)
	tick := func(n int) {
		for i := 0; i < n; i++ {
			c.advance(time.Second)
			rt.Watchdog.Scan()
		}
	}
	// Ordinary traffic that completes.
	for i := 0; i < 6; i++ {
		h := tr.Start(DaemonSpec(StageHandlerStarted, ClassRead, LocReadHandler, tr.NewCorrelation()))
		c.advance(120 * time.Millisecond)
		_ = h.Progress(StageHandlerCompleted)
		h.End(true)
	}
	tick(1)
	// A slow handler that finishes after its warn deadline.
	slow := tr.Start(DaemonSpec(StageHandlerStarted, ClassControl, LocCommandHandler, tr.NewCorrelation()))
	tick(6)
	_ = slow.Progress(StageHandlerCompleted)
	tick(1)
	slow.End(true)

	// The fault: one write holds the writer lock inside a socket write that
	// never returns; the next write waits for the lock.
	var entered sync.WaitGroup
	entered.Add(2)
	holder := tr.Start(DaemonSpec(StageSocketWriteStarted, ClassTransport, LocSocketWrite, tr.NewCorrelation()))
	go conn.writeFrame(&entered)
	c.advance(300 * time.Millisecond)
	waiter := tr.Start(DaemonSpec(StageWriterLockWaiting, ClassTransport, LocWriterLock, tr.NewCorrelation()))
	go conn.writeFrame(&entered)
	entered.Wait()
	waitFor(t, "both writers to park", func() bool {
		buf := make([]byte, 1<<16)
		return strings.Count(string(buf[:runtime.Stack(buf, true)]), "wireConn).writeFrame") >= 2
	})
	// Meanwhile reads keep completing.
	for i := 0; i < 4; i++ {
		h := tr.Start(DaemonSpec(StageHandlerStarted, ClassRead, LocReadHandler, tr.NewCorrelation()))
		c.advance(80 * time.Millisecond)
		_ = h.Progress(StageHandlerCompleted)
		h.End(true)
	}
	tick(16)
	waitFor(t, "the capture", func() bool { _, ok := rt.Capture.Latest(); return ok })
	tick(2)
	rt.Watchdog.Flush()
	ev, err := rt.Export(Query{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.MarshalIndent(ev, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "export.json"), append(b, '\n'), 0o444); err != nil {
		t.Fatal(err)
	}
	close(conn.socket)
	holder.End(true)
	waiter.End(true)
}
