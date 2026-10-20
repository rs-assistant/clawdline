package cloud

import (
	"net"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/connectionhealth"
)

func correlatedRow(t *testing.T, s *Spool, recipient, correlation string) uint64 {
	t.Helper()
	seq, err := s.Reserve(SpoolChannelT, recipient, recipient, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SealCorrelated(seq, []byte(`{"seq":1}`), s.now(), correlation); err != nil {
		t.Fatal(err)
	}
	return seq
}

// A row keeps the correlation of the request it answers for as long as its
// bytes are held, and lets it go with them: an answered row, a refused one, a
// burned one and a coalesced one keep nothing of it. A value that is not an
// opaque id is never kept, and Seal keeps none.
func TestASpoolRowKeepsItsCorrelationOnlyWhileItsBytesAreHeld(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	limits := DefaultSpoolLimits()
	s, err := NewSpool(limits, nil, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	plain, _ := s.Reserve(SpoolChannelT, "t/m/plain", "t/m/plain", 2)
	if err := s.Seal(plain, []byte(`{}`), now); err != nil {
		t.Fatal(err)
	}
	if row, _ := s.Row(plain); row.Correlation != "" {
		t.Fatalf("Seal kept %q", row.Correlation)
	}
	if row, _ := s.Row(correlatedRow(t, s, "t/m/bad", "t/m/bad session")); row.Correlation != "" {
		t.Fatalf("a channel-shaped value was kept as a correlation: %q", row.Correlation)
	}

	acked := correlatedRow(t, s, "t/m/acked", "op-a")
	refused := correlatedRow(t, s, "t/m/refused", "op-r")
	burned := correlatedRow(t, s, "t/m/burned", "op-b")
	if row, _ := s.Row(acked); row.Correlation != "op-a" {
		t.Fatalf("a sealed row: %+v", row)
	}
	for s.SendNext().Row != nil {
	}
	if sealed, corr, err := s.ResendCorrelated(burned); err != nil || corr != "op-b" || len(sealed) == 0 {
		t.Fatalf("a re-send lost its correlation: %q %v", corr, err)
	}
	if _, err := s.Settle(acked, "t/m/acked", SettleDelivered); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Settle(refused, "t/m/refused", SettlePeerError); err != nil {
		t.Fatal(err)
	}
	now = now.Add(limits.AttemptWindow + time.Second)
	if got := s.BurnExpired(); len(got) == 0 || got[len(got)-1] != burned {
		t.Fatalf("burned %v", got)
	}
	for _, seq := range []uint64{acked, refused, burned} {
		if row, _ := s.Row(seq); row.Correlation != "" || !row.State.IsTerminal() {
			t.Fatalf("a terminal row still holds its correlation: %+v", row)
		}
	}
	if _, _, err := s.ResendCorrelated(acked); err == nil {
		t.Fatal("a settled row was handed out for a re-send")
	}

	// Two never-sent snapshots for one screen: the older is replaced, and
	// its correlation goes with it.
	older, _ := s.Reserve(SpoolChannelS, "s/m/x", "s/m/x", 2)
	_ = s.SealCorrelated(older, []byte(`{}`), now, "op-old")
	newer, _ := s.Reserve(SpoolChannelS, "s/m/x", "s/m/x", 2)
	_ = s.SealCorrelated(newer, []byte(`{}`), now, "op-new")
	d := s.SendNext()
	if d.Row == nil || d.Row.Seq != newer || d.Row.Correlation != "op-new" {
		t.Fatalf("the newer snapshot: %+v", d)
	}
	if row, _ := s.Row(older); row.State != SpoolBurned || row.Correlation != "" {
		t.Fatalf("the coalesced snapshot: %+v", row)
	}

	// Past the retention window every tombstone is collected, and nothing
	// of any correlation is left anywhere in the spool.
	now = now.Add(limits.TombstoneRetention + limits.AttemptWindow + time.Hour)
	s.SendNext()
	for _, seq := range []uint64{acked, refused, burned, older} {
		if _, ok := s.Row(seq); ok {
			t.Fatalf("row %d outlived its retention", seq)
		}
	}
}

// drainPeer reads whole frames off the far end of a pipe until told to stop
// reading, so a writer can be held inside its socket write on purpose.
func drainPeer(peer net.Conn, gate <-chan struct{}) {
	buf := make([]byte, 64<<10)
	for {
		if gate != nil {
			<-gate
		}
		if _, err := peer.Read(buf); err != nil {
			return
		}
	}
}

func writerPhase(t *testing.T, tr *connectionhealth.Tracker) connectionhealth.Phase {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		phases, _ := tr.Snapshot()
		for _, p := range phases {
			if p.Spec.Location == connectionhealth.LocSocketWrite {
				return p
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("no write reached the socket")
	return connectionhealth.Phase{}
}

// The drain writes a row under the row's own correlation, and the reconnect
// re-send of the same row is a second attempt — a different phase, which the
// watchdog judges on its own — of the same logical operation. A frame that
// carries none (a ping, an untraced row) still gets an uncorrelated phase.
func TestADrainAndItsResendAreTwoAttemptsOfOneCorrelation(t *testing.T) {
	tr := connectionhealth.NewTracker(connectionhealth.NewSystemClock())
	s, err := NewSpool(DefaultSpoolLimits(), nil, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	seq := correlatedRow(t, s, "t/m/one", "op-7")
	transport := &Transport{opts: Options{Spool: s, Health: tr, Now: time.Now, Status: NewStatusRecorder(time.Now()),
		Log: func(string) {}}}

	conn, peer := pipeConn(tr)
	gate := make(chan struct{})
	go drainPeer(peer, gate)
	done := make(chan bool)
	go func() { done <- transport.drainOnce(conn) }()
	first := writerPhase(t, tr)
	if first.Spec.Correlation != "op-7" || first.Spec.Stage != connectionhealth.StageSocketWriteStarted {
		t.Fatalf("the first write: %+v", first.Spec)
	}
	if !releaseUntil(gate, done) {
		t.Fatal("the drain failed")
	}
	if row, _ := s.Row(seq); row.State != SpoolSent || row.Correlation != "op-7" {
		t.Fatalf("the written row: %+v", row)
	}

	stop := make(chan struct{})
	finished := make(chan struct{})
	go func() { transport.drain(conn, stop, true); close(finished) }()
	second := writerPhase(t, tr)
	if second.Spec.Correlation != "op-7" || second.ID == first.ID {
		t.Fatalf("the re-send: %+v (first phase %d)", second, first.ID)
	}
	close(stop)
	releaseUntil(gate, finished)

	// A ping held behind the writer lock: its phase names no operation.
	close(gate)
	conn.writeMu.Lock()
	pinged := make(chan error)
	go func() { pinged <- conn.WritePing(time.Now().Add(time.Minute)) }()
	waitPhase(t, tr, connectionhealth.StageWriterLockWaiting)
	phases, _ := tr.Snapshot()
	for _, p := range phases {
		if p.Spec.Location == connectionhealth.LocWriterLock && p.Spec.Correlation != "" {
			t.Fatalf("a ping inherited %q", p.Spec.Correlation)
		}
	}
	conn.writeMu.Unlock()
	if err := <-pinged; err != nil {
		t.Fatal(err)
	}
	_ = peer.Close()
}

// releaseUntil lets the peer read until the writer is through.
func releaseUntil[T any](gate chan<- struct{}, through <-chan T) T {
	for {
		select {
		case gate <- struct{}{}:
		case v := <-through:
			return v
		}
	}
}
