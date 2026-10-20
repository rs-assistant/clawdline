package cloud

import (
	"bufio"
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/connectionhealth"
)

func pipeConn(tr *connectionhealth.Tracker) (*Conn, net.Conn) {
	ours, theirs := net.Pipe()
	return &Conn{conn: ours, br: bufio.NewReader(ours), health: tr}, theirs
}

// scanFor advances the clock a second at a time, scanning each second as the
// watchdog's own cadence would, so no reading is a scheduling gap.
func scanFor(clock *stepClock, w *connectionhealth.Watchdog, d time.Duration) {
	for step := time.Duration(0); step < d; step += time.Second {
		clock.advance(time.Second)
		w.Scan()
	}
}

func onlyFinding(t *testing.T, w *connectionhealth.Watchdog) connectionhealth.Finding {
	t.Helper()
	r := w.Scan()
	if len(r.Findings) != 1 {
		t.Fatalf("findings: %+v", r.Findings)
	}
	return r.Findings[0]
}

func waitPhase(t *testing.T, tr *connectionhealth.Tracker, stage connectionhealth.Stage) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		phases, _ := tr.Snapshot()
		for _, p := range phases {
			if p.Spec.Stage == stage {
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("no phase reached %s", stage)
}

// A writer waiting for the connection's write lock and a writer stuck in the
// socket's own write are two different findings: writer_lock_waiting at
// cloud.websocket.writer_lock, and socket_write_started at
// cloud.websocket.socket_write.
func TestTheWriterLockAndTheSocketWriteAreToldApart(t *testing.T) {
	clock, tr, _, w := healthRig(t)

	conn, peer := pipeConn(tr)
	conn.writeMu.Lock() // another writer holds the lock
	done := make(chan error)
	go func() { done <- conn.WriteText([]byte(`{"type":"ping"}`), time.Now().Add(time.Minute)) }()
	waitPhase(t, tr, connectionhealth.StageWriterLockWaiting)
	scanFor(clock, w, 16*time.Second)
	f := onlyFinding(t, w)
	if f.Stage != connectionhealth.StageWriterLockWaiting || f.Location != connectionhealth.LocWriterLock || f.State != connectionhealth.StateBlocked {
		t.Fatalf("a writer behind the lock: %+v", f)
	}
	conn.writeMu.Unlock()
	// Now it holds the lock and the pipe has no reader: stuck in the write.
	waitPhase(t, tr, connectionhealth.StageSocketWriteStarted)
	scanFor(clock, w, 16*time.Second)
	f = onlyFinding(t, w)
	if f.Stage != connectionhealth.StageSocketWriteStarted || f.Location != connectionhealth.LocSocketWrite ||
		f.State != connectionhealth.StateBlocked || f.Progress != 1 {
		t.Fatalf("a writer in the socket write: %+v", f)
	}
	_ = peer.Close()
	if err := <-done; err == nil {
		t.Fatal("a write to a closed pipe succeeded")
	}
	if phases, _ := tr.Snapshot(); len(phases) != 0 {
		t.Fatalf("the write phase outlived the write: %+v", phases)
	}
	_, failed := tr.Counts()
	if failed[connectionhealth.ClassTransport] != 1 {
		t.Fatalf("the failed write was not counted as failed: %+v", failed)
	}
}

// With every business slot taken, a new write is carried out uninstrumented
// and counted as overflow, while the reader loop still gets its reserved slot
// and stays visible. When the socket closes the reader's phase ends with it.
func TestASaturatedTableKeepsTheReaderVisible(t *testing.T) {
	_, tr, _, w := healthRig(t)
	var held []connectionhealth.Handle
	for i := 0; i < connectionhealth.MaxActiveSlots-connectionhealth.MaxReservedSlots; i++ {
		h := tr.Start(connectionhealth.DaemonSpec(connectionhealth.StageHandlerStarted, connectionhealth.ClassRead, connectionhealth.LocReadHandler, ""))
		if !h.Recorded() {
			t.Fatalf("slot %d was refused", i)
		}
		held = append(held, h)
	}
	conn, peer := pipeConn(tr)
	go func() { _, _ = io.Copy(io.Discard, peer) }()
	if err := conn.WriteText([]byte(`{}`), time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	transport := &Transport{opts: Options{Now: time.Now, Status: NewStatusRecorder(time.Now()), Health: tr}, stopped: make(chan struct{})}
	done := make(chan error)
	go func() { done <- transport.read(context.Background(), conn) }()
	waitPhase(t, tr, connectionhealth.StageIdle)
	r := w.Scan()
	if r.Overflow.Total != 1 || r.Overflow.Critical != 0 || r.TraceCoverage != connectionhealth.QualityPartial {
		t.Fatalf("the refused write: %+v", r.Overflow)
	}
	reader := false
	for _, f := range r.Findings {
		reader = reader || (f.Location == connectionhealth.LocReadLoop && f.State == connectionhealth.StateIdle)
	}
	if !reader {
		t.Fatal("the reader loop is not visible beside a full table")
	}
	_ = peer.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the reader did not end with its socket")
	}
	for _, h := range held {
		h.End(true)
	}
	if phases, _ := tr.Snapshot(); len(phases) != 0 {
		t.Fatalf("phases left: %+v", phases)
	}
}
