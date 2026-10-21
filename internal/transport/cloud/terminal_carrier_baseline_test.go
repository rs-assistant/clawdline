package cloud

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/app/terminals"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
	"github.com/sainteye/clawdline/internal/domain/terminal"
)

// This is a carrier comparison, not an optimization claim. All peers, relay,
// crypto and publisher share one Go process; CPU cannot be attributed to a
// publisher alone. testBrowser is a loopback Pion peer, never a real browser.
func TestTerminalCarrierBaseline(t *testing.T) {
	if os.Getenv("CLAWDLINE_TERMINAL_BASELINE") != "1" {
		t.Skip("opt-in local baseline")
	}
	wireBenchCPUParts(t) // Unsupported CPU facilities skip, never report zero.
	for _, mode := range []string{"direct", "relay", "direct_recovery"} {
		t.Run(mode, func(t *testing.T) { terminalCarrierBaseline(t, mode) })
	}
}

func terminalCarrierBaseline(t *testing.T, mode string) {
	t.Helper()
	initAt := time.Now()
	iu, is := wireBenchCPUParts(t)
	f := newDirectFixture(t)
	var peer *testBrowser
	var machinePeer *directPeer
	t.Cleanup(func() {
		f.l.closeAllDirectPeers()
		if machinePeer != nil {
			if err := machinePeer.pc.GracefulClose(); err != nil {
				t.Error(err)
			}
		}
		if peer != nil {
			if err := peer.pc.GracefulClose(); err != nil {
				t.Error(err)
			}
		}
		f.l.closeDirectMux()
		f.l.closeTerminalConnection(f.c)
		select {
		case <-f.retired:
		case <-time.After(3 * time.Second):
			t.Error("retirement worker did not complete")
		}
		t.Log("CLEANUP peers_gracefully_joined=true retirement_observed=true")
	})
	var receiver *adaptercloud.Conn
	var relay *wireBenchRelay
	if mode != "relay" {
		peer = f.connected(t)
		f.l.directMu.Lock()
		machinePeer = f.l.directPeers["viewer"]
		f.l.directMu.Unlock()
		f.l.terminalMu.Lock()
		f.c.carrier = carrierDirect
		f.l.terminalMu.Unlock()
	} else {
		f.l.settings.TerminalDirect = false
		relay = &wireBenchRelay{signer: f.signer}
		var handlers sync.WaitGroup
		relay.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handlers.Add(1)
			defer handlers.Done()
			relay.serve(w, r)
		}))
		t.Cleanup(func() {
			if receiver != nil {
				_ = receiver.Close()
			}
			relay.server.Close()
			relay.mu.Lock()
			viewers := append([]*wireBenchPeer(nil), relay.viewers...)
			machine := relay.machine
			relay.mu.Unlock()
			for _, v := range viewers {
				_ = v.c.Close()
			}
			if machine != nil {
				_ = machine.c.Close()
			}
			handlers.Wait()
			t.Log("CLEANUP relay_handlers_joined=true receiver_closed=true")
		})
		url := "ws" + strings.TrimPrefix(relay.server.URL, "http") + "/v1/connect"
		var err error
		receiver, err = adaptercloud.Dial(url+"?role=viewer", adaptercloud.DialOptions{NetDial: relay.dial})
		if err != nil {
			t.Fatal(err)
		}
		terminalBaselineWait(t, func() bool { relay.mu.Lock(); defer relay.mu.Unlock(); return len(relay.viewers) == 1 })
		tr, err := adaptercloud.New(adaptercloud.Options{RelayURL: url, NetDial: relay.dial,
			Token:    &adaptercloud.TokenSource{Client: adaptercloud.NewAccountClient(relay.server.URL), Credential: "credential"},
			Identity: adaptercloud.Identity{AccountID: "usr_test", MachineID: "mac-01", MachineCredential: "credential"},
			Signer:   f.signer, Spool: f.spool, Replay: domaincloud.NewReplayWindow(0), ContentKey: f.master,
			Jitter: func() float64 { return 0.5 }, OnSettled: f.l.terminalReceiptSettled})
		if err != nil {
			t.Fatal(err)
		}
		f.l.relay.Transport = tr
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { defer close(done); _ = tr.Run(ctx) }()
		t.Cleanup(func() {
			cancel()
			tr.Stop()
			<-done
			t.Log("CLEANUP transport_run_joined=true")
		})
		select {
		case <-tr.Ready():
		case <-time.After(5 * time.Second):
			t.Fatal("local relay handshake timeout")
		}
	}
	u, s := wireBenchCPUParts(t)
	t.Logf("BASELINE carrier=%s phase=init wall_ns=%d combined_user_ns=%d combined_system_ns=%d", mode, time.Since(initAt).Nanoseconds(), u-iu, s-is)
	idleAt := time.Now()
	u, s = wireBenchCPUParts(t)
	time.Sleep(100 * time.Millisecond)
	u1, s1 := wireBenchCPUParts(t)
	t.Logf("BASELINE carrier=%s phase=idle wall_ns=%d combined_user_ns=%d combined_system_ns=%d", mode, time.Since(idleAt).Nanoseconds(), u1-u, s1-s)
	frame := terminal.Frame{At: time.Unix(1700000000, 0), Cols: 80, Rows: 24}
	for row := 0; row < 24; row++ {
		frame.Lines = append(frame.Lines, fmt.Sprintf("%02d %s", row, strings.Repeat("synthetic ", 7)))
	}
	var envelopeBytes, plainBytes, directMessageBytes, directAckBytes int
	var latencies []int64
	var lastEnvelopeSeq uint64
	activity := time.Now()
	u, s = wireBenchCPUParts(t)
	for i := 1; i <= 8; i++ {
		frame.Rev = fmt.Sprint(i)
		start := time.Now()
		if err := f.l.sendTerminalFrame(context.Background(), f.svc, f.p, f.c, f.c.terminalID, frame); err != nil {
			t.Fatal(err)
		}
		var raw []byte
		if peer != nil {
			var message []byte
			select {
			case message = <-peer.msgs:
			case <-time.After(5 * time.Second):
				t.Fatal("no direct frame")
			}
			var m directMessage
			if err := json.Unmarshal(message, &m); err != nil {
				t.Fatal(err)
			}
			if m.T != "env" {
				t.Fatalf("unexpected direct message %s", m.T)
			}
			raw = m.E
			directMessageBytes += len(message)
		} else {
			b, err := receiver.Read(time.Now().Add(5 * time.Second))
			if err != nil {
				t.Fatal(err)
			}
			var wrapper adaptercloud.EnvelopeFrame
			if err := json.Unmarshal(b, &wrapper); err != nil || wrapper.Type != "envelope" {
				t.Fatalf("relay wrapper: %v %s", err, b)
			}
			raw = wrapper.Envelope
		}
		env, err := domaincloud.DecodeEnvelope(raw)
		if err != nil {
			t.Fatal(err)
		}
		if env.Ch != "term/machine/viewer/"+f.c.id || env.Sender != "machine" || env.KeyID != f.c.keyID || (i > 1 && env.Seq != lastEnvelopeSeq+1) {
			t.Fatalf("wrong envelope channel or sequence: %s %d", env.Ch, env.Seq)
		}
		lastEnvelopeSeq = env.Seq
		plain, err := env.Open(f.key, func(string) (ed25519.PublicKey, bool) { return f.signer.PublicKey(), true })
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			Type       string          `json:"type"`
			Connection string          `json:"connection"`
			TerminalID string          `json:"terminal_id"`
			FrameSeq   uint64          `json:"frame_seq"`
			Frame      json.RawMessage `json:"frame"`
		}
		if err := json.Unmarshal(plain, &got); err != nil {
			t.Fatal(err)
		}
		wantFrame, _ := json.Marshal(cloudWireFrame(frame))
		var want, actual any
		_ = json.Unmarshal(wantFrame, &want)
		_ = json.Unmarshal(got.Frame, &actual)
		if got.Type != "terminal_frame" || got.Connection != f.c.id || got.TerminalID != string(f.c.terminalID) || got.FrameSeq != uint64(i) || !reflect.DeepEqual(want, actual) {
			t.Fatalf("wrong full frame: %s", plain)
		}
		if peer != nil {
			if _, ok := f.spool.Row(uint64(i)); ok {
				t.Fatal("direct frame leaked onto relay spool")
			}
			ack, _ := json.Marshal(directMessage{T: "ack", Connection: f.c.id, FrameSeq: uint64(i)})
			directAckBytes += len(ack)
			if err := peer.dc.SendText(string(ack)); err != nil {
				t.Fatal(err)
			}
		}
		terminalBaselineWait(t, func() bool {
			f.l.terminalMu.Lock()
			defer f.l.terminalMu.Unlock()
			return !f.c.framePending && f.c.frameBaseSeq == uint64(i)
		})
		latencies = append(latencies, time.Since(start).Nanoseconds())
		plainBytes += len(plain)
		envelopeBytes += len(raw)
	}
	u1, s1 = wireBenchCPUParts(t)
	t.Logf("BASELINE carrier=%s phase=active frames=8 viewers=1 wall_ns=%d combined_user_ns=%d combined_system_ns=%d plaintext_bytes=%d envelope_bytes=%d direct_message_json_bytes=%d direct_ack_json_bytes=%d latency_ns=%v", mode, time.Since(activity).Nanoseconds(), u1-u, s1-s, plainBytes, envelopeBytes, directMessageBytes, directAckBytes, latencies)
	if relay != nil {
		relay.mu.Lock()
		t.Logf("WIRE carrier=relay publish_ws_bytes=%d viewer_ws_bytes=%d relay_ack_ws_bytes=%d http_upgrade_bytes=%d publishes=%d acks=%d retries=%d", relay.frameBytes, relay.viewerBytes, relay.ackBytes, relay.handshakeBytes, len(relay.published), relay.acks, relay.retries)
		if len(relay.published) != 8 || relay.acks != 8 || relay.retries != 0 {
			t.Error("relay count mismatch")
		}
		relay.mu.Unlock()
		// A queued frame is not a received frame: with the receiver closed no
		// additional viewer-decode success is counted.
		_ = receiver.Close()
	} else {
		select {
		case m := <-peer.msgs:
			t.Fatalf("unexpected duplicate message %s", m)
		case <-time.After(20 * time.Millisecond):
		}
	}
	if mode == "direct" {
		failureAt := time.Now()
		fu, fs := wireBenchCPUParts(t)
		// A real received frame with a wrong ACK remains pending. The typed
		// refusal then prevents the next frame until a correct ACK or retirement.
		if err := f.l.sendTerminalFrame(context.Background(), f.svc, f.p, f.c, f.c.terminalID, frame); err != nil {
			t.Fatal(err)
		}
		_, _ = f.open(t, peer.next(t))
		f.l.directFrameAcked("viewer", f.c.id, 999)
		if err := f.l.sendTerminalFrame(context.Background(), f.svc, f.p, f.c, f.c.terminalID, frame); !errors.Is(err, terminals.ErrFrameDeferred) {
			t.Fatalf("wrong ACK released pending frame: %v", err)
		}
		f.l.terminalMu.Lock()
		f.c.framePendingAt = time.Now().Add(-CloudTerminalDirectAckSecondsLimit * time.Second)
		f.l.terminalMu.Unlock()
		f.l.sweepDirect(f.c)
		if f.l.getTerminalConnection("viewer", f.c.id) != nil {
			t.Fatal("late ACK did not retire connection")
		}
		f.l.directFrameAcked("viewer", f.c.id, 9)
		if err := f.l.sendTerminalFrame(context.Background(), f.svc, f.p, f.c, f.c.terminalID, frame); !errors.Is(err, context.Canceled) {
			t.Fatalf("retired connection accepted frame: %v", err)
		}
		t.Log("FAILURE wrong_ack=deferred late_ack=retired retired_send=canceled recovery=separate_new_connection_subtest; no browser fallback claim")
		fu1, fs1 := wireBenchCPUParts(t)
		t.Logf("BASELINE carrier=direct phase=controlled_failure wall_ns=%d combined_user_ns=%d combined_system_ns=%d", time.Since(failureAt).Nanoseconds(), fu1-fu, fs1-fs)
	}
}

func terminalBaselineWait(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("terminal baseline acknowledgement/registration timeout")
		}
		time.Sleep(time.Millisecond)
	}
}
