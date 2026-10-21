package cloud

// This opt-in local baseline uses production Relay, Spool, Transport, WebSocket
// client and envelope crypto. The relay server and Go viewers are test fixtures.
import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// wireBenchCountConn counts the actual HTTP headers on the client socket,
// excluding any WebSocket frame coalesced into the upgrade response read.
type wireBenchCountConn struct {
	net.Conn
	relay                   *wireBenchRelay
	readHeader, writeHeader []byte
	readDone, writeDone     bool
}

func (c *wireBenchCountConn) count(b []byte, reading bool) {
	header, done := &c.writeHeader, &c.writeDone
	if reading {
		header, done = &c.readHeader, &c.readDone
	}
	if *done {
		return
	}
	*header = append(*header, b...)
	if n := bytes.Index(*header, []byte("\r\n\r\n")); n >= 0 {
		c.relay.mu.Lock()
		c.relay.handshakeBytes += int64(n + 4)
		c.relay.mu.Unlock()
		*done = true
		*header = nil
	}
}
func (c *wireBenchCountConn) Read(b []byte) (int, error) {
	n, e := c.Conn.Read(b)
	c.count(b[:n], true)
	return n, e
}
func (c *wireBenchCountConn) Write(b []byte) (int, error) {
	n, e := c.Conn.Write(b)
	c.count(b[:n], false)
	return n, e
}
func (r *wireBenchRelay) dial(network, address string) (net.Conn, error) {
	c, e := net.Dial(network, address)
	if e != nil {
		return nil, e
	}
	return &wireBenchCountConn{Conn: c, relay: r}, nil
}

type wireBenchPeer struct {
	c  net.Conn
	mu sync.Mutex
}

func (p *wireBenchPeer) send(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	h := []byte{0x81}
	n := len(b)
	if n < 126 {
		h = append(h, byte(n))
	} else if n <= 65535 {
		h = append(h, 126, byte(n>>8), byte(n))
	} else {
		h = append(h, 127)
		var x [8]byte
		binary.BigEndian.PutUint64(x[:], uint64(n))
		h = append(h, x[:]...)
	}
	if _, err = p.c.Write(h); err != nil {
		return err
	}
	_, err = p.c.Write(b)
	return err
}
func wireBenchRead(br *bufio.Reader) ([]byte, int, error) {
	var h [2]byte
	if _, e := io.ReadFull(br, h[:]); e != nil {
		return nil, 0, e
	}
	n := int(h[1] & 127)
	overhead := 6
	if h[1]&128 == 0 {
		return nil, 0, fmt.Errorf("unmasked client")
	}
	if n == 126 {
		var x [2]byte
		if _, e := io.ReadFull(br, x[:]); e != nil {
			return nil, 0, e
		}
		n = int(binary.BigEndian.Uint16(x[:]))
		overhead += 2
	} else if n == 127 {
		var x [8]byte
		if _, e := io.ReadFull(br, x[:]); e != nil {
			return nil, 0, e
		}
		n = int(binary.BigEndian.Uint64(x[:]))
		overhead += 8
	}
	var mask [4]byte
	if _, e := io.ReadFull(br, mask[:]); e != nil {
		return nil, 0, e
	}
	b := make([]byte, n)
	if _, e := io.ReadFull(br, b); e != nil {
		return nil, 0, e
	}
	for i := range b {
		b[i] ^= mask[i%4]
	}
	return b, n + overhead, nil
}

type wireBenchRelay struct {
	server                                                                            *httptest.Server
	signer                                                                            domaincloud.DeviceKey
	mu                                                                                sync.Mutex
	viewers                                                                           []*wireBenchPeer
	machine                                                                           *wireBenchPeer
	published                                                                         [][]byte
	handshakeBytes, frameBytes, ackBytes, envelopeBytes, ciphertextBytes, viewerBytes int64
	connections, acks, retries                                                        int
	delay                                                                             time.Duration
	dropOnce                                                                          bool
}

func (r *wireBenchRelay) serve(w http.ResponseWriter, req *http.Request) {
	if req.URL.Path == "/v1/tokens/device" {
		_ = json.NewEncoder(w).Encode(adaptercloud.DeviceToken{Token: "fixture-token", TokenType: "Bearer", ExpiresIn: 300, ExpiresAt: time.Now().Add(5 * time.Minute), Kid: "dev"})
		return
	}
	c, rw, e := w.(http.Hijacker).Hijack()
	if e != nil {
		return
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(30 * time.Second))
	sum := sha1.Sum([]byte(req.Header.Get("Sec-WebSocket-Key") + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	response := fmt.Sprintf("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", base64.StdEncoding.EncodeToString(sum[:]))
	_, _ = rw.WriteString(response)
	if rw.Flush() != nil {
		return
	}
	p := &wireBenchPeer{c: c}
	if req.URL.Query().Get("role") == "viewer" {
		r.mu.Lock()
		r.viewers = append(r.viewers, p)
		r.mu.Unlock()
		_, _ = io.Copy(io.Discard, rw)
		return
	}
	challenge := base64.StdEncoding.EncodeToString(make([]byte, 32))
	if p.send(adaptercloud.ChallengeFrame{Type: "challenge", V: 1, Context: adaptercloud.ChallengeContext, Account: "usr_test", Device: "mac-01", Challenge: challenge, ExpiresInMs: 15000}) != nil {
		return
	}
	hello, _, e := wireBenchRead(rw.Reader)
	if e != nil {
		return
	}
	var h adaptercloud.HelloFrame
	if json.Unmarshal(hello, &h) != nil {
		return
	}
	sig, _ := base64.StdEncoding.DecodeString(h.Sig)
	if !ed25519.Verify(r.signer.PublicKey(), []byte(adaptercloud.ChallengeString("usr_test", "mac-01", challenge)), sig) {
		return
	}
	if p.send(adaptercloud.ReadyFrame{Type: "ready", V: 1, Account: "usr_test", Device: "mac-01", Role: "machine", ConnectedAt: time.Now().UnixMilli(), TokenExpiresAt: time.Now().Add(5 * time.Minute).UnixMilli()}) != nil {
		return
	}
	r.mu.Lock()
	r.machine = p
	r.connections++
	r.mu.Unlock()
	for {
		b, n, e := wireBenchRead(rw.Reader)
		if e != nil {
			return
		}
		var frame adaptercloud.EnvelopeFrame
		if json.Unmarshal(b, &frame) != nil {
			return
		}
		if frame.Type != "publish" {
			continue
		}
		env, e := domaincloud.DecodeEnvelope(frame.Envelope)
		if e != nil {
			return
		}
		ct, e := base64.StdEncoding.DecodeString(env.Ct)
		if e != nil {
			return
		}
		r.mu.Lock()
		r.frameBytes += int64(n)
		r.envelopeBytes += int64(len(frame.Envelope))
		r.ciphertextBytes += int64(len(ct))
		for _, old := range r.published {
			if string(old) == string(frame.Envelope) {
				r.retries++
				break
			}
		}
		r.published = append(r.published, append([]byte(nil), frame.Envelope...))
		viewers := append([]*wireBenchPeer(nil), r.viewers...)
		drop := r.dropOnce
		r.dropOnce = false
		r.mu.Unlock()
		for _, v := range viewers {
			vb, _ := json.Marshal(adaptercloud.EnvelopeFrame{Type: "envelope", Envelope: frame.Envelope})
			overhead := 2
			if len(vb) >= 126 {
				overhead = 4
			}
			if len(vb) > 65535 {
				overhead = 10
			}
			r.mu.Lock()
			r.viewerBytes += int64(len(vb) + overhead)
			r.mu.Unlock()
			if v.send(adaptercloud.EnvelopeFrame{Type: "envelope", Envelope: frame.Envelope}) != nil {
				return
			}
		}
		if drop {
			return
		}
		time.Sleep(r.delay)
		ack := adaptercloud.AckFrame{Type: "ack", Ch: env.Ch, Seq: env.Seq, Fanout: len(viewers), Status: adaptercloud.AckDelivered}
		ab, _ := json.Marshal(ack)
		// Settlement may race the socket writer; hold accounting until it finishes.
		r.mu.Lock()
		if p.send(ack) != nil {
			r.mu.Unlock()
			return
		}
		r.acks++
		r.ackBytes += int64(len(ab) + 2)
		r.mu.Unlock()
	}
}

type wireBenchResult struct {
	ViewerWSBytes                                                                                             int64
	Scenario                                                                                                  string `json:"scenario"`
	Sessions, Viewers, Operations, Connections, Acks, Retries, Deliveries                                     int
	HTTPHandshakeBytes, PublishWSBytes, EnvelopeBytes, CiphertextBytes, PlaintextBytes, AckWSBytes            int64
	InitNS, IdleNS, IdleCPUNS, ProcessCPUNS, WallNS, P50NS, P95NS, SealP50NS, SealP95NS, OpenP50NS, OpenP95NS int64
	LatencyNS, SealNS, OpenNS                                                                                 []int64
}

func wireBenchPercentile(v []int64, p int) int64 {
	if len(v) == 0 {
		return 0
	}
	x := append([]int64(nil), v...)
	sort.Slice(x, func(i, j int) bool { return x[i] < x[j] })
	return x[(len(x)*p+99)/100-1]
}

// TestWireEfficiencyBaseline is opt-in so the ordinary gate does not perform
// timed experiments. Every scenario is bounded fixture load, not a product cap.
func TestWireEfficiencyBaseline(t *testing.T) {
	if os.Getenv("CLAWDLINE_WIRE_BENCH") != "1" {
		t.Skip("set CLAWDLINE_WIRE_BENCH=1 for the isolated baseline")
	}
	for _, s := range []struct {
		name                   string
		sessions, viewers, ops int
		delay                  time.Duration
		drop                   bool
	}{
		{"initial_idle", 1, 1, 0, 0, false}, {"active_one", 1, 1, 12, 0, false}, {"active_multi_session", 16, 1, 32, 0, false}, {"independent_viewers", 4, 4, 16, 0, false}, {"reconnect_unacked", 1, 1, 12, 0, true}, {"ack_delay", 1, 1, 12, 25 * time.Millisecond, false},
	} {
		t.Run(s.name, func(t *testing.T) {
			signer, e := domaincloud.NewDeviceKey(rand.Reader)
			if e != nil {
				t.Fatal(e)
			}
			key, e := domaincloud.NewContentKey(rand.Reader)
			if e != nil {
				t.Fatal(e)
			}
			r := &wireBenchRelay{signer: signer, delay: s.delay, dropOnce: s.drop}
			r.server = httptest.NewServer(http.HandlerFunc(r.serve))
			defer r.server.Close()
			url := "ws" + strings.TrimPrefix(r.server.URL, "http") + "/v1/connect"
			var received atomic.Int64
			var openMu sync.Mutex
			var opens []int64
			var viewerWG sync.WaitGroup
			var clients []*adaptercloud.Conn
			defer func() {
				for _, c := range clients {
					_ = c.Close()
				}
				viewerWG.Wait()
			}()
			for i := 0; i < s.viewers; i++ {
				c, e := adaptercloud.Dial(url+"?role=viewer", adaptercloud.DialOptions{NetDial: r.dial})
				if e != nil {
					t.Fatal(e)
				}
				clients = append(clients, c)
				viewerWG.Add(1)
				go func() {
					defer viewerWG.Done()
					replay := domaincloud.NewReplayWindow(0)
					expected := 0
					for {
						b, e := c.Read(time.Now().Add(30 * time.Second))
						if e != nil {
							return
						}
						var f adaptercloud.EnvelopeFrame
						if json.Unmarshal(b, &f) != nil {
							t.Error("bad wrapper")
							return
						}
						start := time.Now()
						env, e := domaincloud.DecodeEnvelope(f.Envelope)
						if e != nil {
							t.Error(e)
							return
						}
						plain, e := env.Open(key, func(id string) (ed25519.PublicKey, bool) { return signer.PublicKey(), id == "mac-01" })
						if e != nil {
							t.Error(e)
							return
						}
						openMu.Lock()
						opens = append(opens, time.Since(start).Nanoseconds())
						openMu.Unlock()
						if replay.Claim(env.Sender, env.Seq) != domaincloud.ReplayAccepted {
							continue
						}
						var payload struct {
							Op     int `json:"op"`
							Source struct {
								Freshness  string `json:"freshness"`
								ObservedAt int    `json:"observed_at"`
							} `json:"source"`
						}
						if json.Unmarshal(plain, &payload) != nil {
							t.Error("bad payload")
							return
						}
						if payload.Op != expected || payload.Source.Freshness != "current" || payload.Source.ObservedAt != expected {
							t.Errorf("out of order or stale payload: got %+v want op %d", payload, expected)
							return
						}
						expected++
						received.Add(1)
					}
				}()
			}
			// Wait for the server-side viewer inventory; client upgrade completion alone
			// does not prove the handler has registered that viewer.
			deadline := time.Now().Add(5 * time.Second)
			for {
				r.mu.Lock()
				n := len(r.viewers)
				r.mu.Unlock()
				if n == s.viewers {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("viewer registration")
				}
				time.Sleep(time.Millisecond)
			}
			spool, e := adaptercloud.NewSpool(adaptercloud.DefaultSpoolLimits(), nil, time.Now)
			if e != nil {
				t.Fatal(e)
			}
			settled := make(chan uint64, 128)
			tr, e := adaptercloud.New(adaptercloud.Options{RelayURL: url, NetDial: r.dial, Token: &adaptercloud.TokenSource{Client: adaptercloud.NewAccountClient(r.server.URL), Credential: "credential"}, Identity: adaptercloud.Identity{AccountID: "usr_test", MachineID: "mac-01", MachineCredential: "credential"}, Signer: signer, Spool: spool, Replay: domaincloud.NewReplayWindow(0), ContentKey: key, Jitter: func() float64 { return 0.5 }, OnSettled: func(_ string, seq uint64, _ adaptercloud.SettleKind) { settled <- seq }})
			if e != nil {
				t.Fatal(e)
			}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			defer func() { cancel(); tr.Stop(); <-done }()
			initStart := time.Now()
			go func() { defer close(done); _ = tr.Run(ctx) }()
			select {
			case <-tr.Ready():
			case <-time.After(5 * time.Second):
				t.Fatal("handshake")
			}
			result := wireBenchResult{Scenario: s.name, Sessions: s.sessions, Viewers: s.viewers, Operations: s.ops, InitNS: time.Since(initStart).Nanoseconds()}
			relay := &Relay{Transport: tr, Spool: spool, MachineID: "mac-01", Signer: signer, Secret: key}
			idleStart, cpuStart := time.Now(), wireBenchCPU(t)
			time.Sleep(200 * time.Millisecond)
			result.IdleNS = time.Since(idleStart).Nanoseconds()
			result.IdleCPUNS = wireBenchCPU(t) - cpuStart
			wallStart, cpuStart := time.Now(), wireBenchCPU(t)
			for i := 0; i < s.ops; i++ {
				payload, _ := json.Marshal(map[string]any{"op": i, "session": fmt.Sprintf("%%%d", 10+i%s.sessions), "source": map[string]any{"freshness": "current", "observed_at": i}, "text": strings.Repeat("x", 1024)})
				start := time.Now()
				seq, e := relay.PublishTracked(ctx, Outbound{Channel: fmt.Sprintf("t/mac-01/%%25%d", 10+i%s.sessions), Payload: payload})
				result.SealNS = append(result.SealNS, time.Since(start).Nanoseconds())
				if e != nil {
					t.Fatal(e)
				}
				result.PlaintextBytes += int64(len(payload))
				select {
				case got := <-settled:
					if got != seq {
						t.Fatalf("settled %d want %d", got, seq)
					}
				case <-time.After(8 * time.Second):
					t.Fatal("settlement timeout")
				}
				want := int64((i + 1) * s.viewers)
				deadline := time.Now().Add(5 * time.Second)
				for received.Load() < want {
					if time.Now().After(deadline) {
						t.Fatal("viewer delivery timeout")
					}
					time.Sleep(time.Millisecond)
				}
				result.LatencyNS = append(result.LatencyNS, time.Since(start).Nanoseconds())
			}
			result.WallNS = time.Since(wallStart).Nanoseconds()
			result.ProcessCPUNS = wireBenchCPU(t) - cpuStart
			r.mu.Lock()
			result.Connections = r.connections
			result.Acks = r.acks
			result.Retries = r.retries
			result.HTTPHandshakeBytes = r.handshakeBytes
			result.PublishWSBytes = r.frameBytes
			result.EnvelopeBytes = r.envelopeBytes
			result.CiphertextBytes = r.ciphertextBytes
			result.AckWSBytes = r.ackBytes
			result.ViewerWSBytes = r.viewerBytes
			r.mu.Unlock()
			result.Deliveries = int(received.Load())
			openMu.Lock()
			result.OpenNS = append([]int64(nil), opens...)
			openMu.Unlock()
			result.P50NS = wireBenchPercentile(result.LatencyNS, 50)
			result.P95NS = wireBenchPercentile(result.LatencyNS, 95)
			result.SealP50NS = wireBenchPercentile(result.SealNS, 50)
			result.SealP95NS = wireBenchPercentile(result.SealNS, 95)
			result.OpenP50NS = wireBenchPercentile(result.OpenNS, 50)
			result.OpenP95NS = wireBenchPercentile(result.OpenNS, 95)
			if result.Acks != s.ops {
				t.Fatalf("ack count %d want %d", result.Acks, s.ops)
			}
			if result.Deliveries != s.ops*s.viewers {
				t.Fatalf("deliveries %d", result.Deliveries)
			}
			if s.drop && (result.Retries != 1 || result.Connections != 2) {
				t.Fatalf("recovery %+v", result)
			}
			if !s.drop && result.Retries != 0 {
				t.Fatal("unexpected retry")
			}
			b, e := json.Marshal(result)
			if e != nil {
				t.Fatal(e)
			}
			t.Log("WIRE_RESULT " + string(b))
		})
	}
}

// Admission controls exercise the production receiver ladder separately from
// socket timing. Revocation here changes the pinned-key resolver, not Cloud's
// account roster or browser pairing state.
func TestWireEfficiencyAdmissionControls(t *testing.T) {
	signer, err := domaincloud.NewDeviceKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := domaincloud.NewContentKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var allowed atomic.Bool
	allowed.Store(true)
	delivered := 0
	tr, err := adaptercloud.New(adaptercloud.Options{RelayURL: "ws://relay.invalid", Token: &adaptercloud.TokenSource{}, Identity: adaptercloud.Identity{AccountID: "usr_test", MachineID: "mac-01", MachineCredential: "credential"}, Signer: signer, ContentKey: key, Replay: domaincloud.NewReplayWindow(0), PublicKeyFor: func(sender string) (ed25519.PublicKey, bool) {
		return signer.PublicKey(), sender == "viewer-1" && allowed.Load()
	}, Inbound: func(_ domaincloud.Envelope, plain []byte, _ ed25519.PublicKey) {
		if string(plain) != `{"type":"sessions"}` {
			t.Error("payload changed")
		}
		delivered++
	}})
	if err != nil {
		t.Fatal(err)
	}
	seal := func(seq uint64) []byte {
		env, err := domaincloud.Seal([]byte(`{"type":"sessions"}`), domaincloud.SealParams{Ch: "r/mac-01", Seq: seq, Ts: uint64(time.Now().UnixMilli()), Class: domaincloud.ClassCtl, KeyID: adaptercloud.MasterKeyID, Sender: "viewer-1", Key: key, Signer: signer})
		if err != nil {
			t.Fatal(err)
		}
		raw, err := env.CanonicalJSON()
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	first := seal(1)
	if !tr.AcceptDirect("viewer-1", first) {
		t.Fatal("fresh request refused")
	}
	if tr.AcceptDirect("viewer-1", first) {
		t.Fatal("duplicate delivered")
	}
	second := seal(2)
	allowed.Store(false)
	if tr.AcceptDirect("viewer-1", second) {
		t.Fatal("revoked sender admitted")
	}
	allowed.Store(true)
	if !tr.AcceptDirect("viewer-1", second) {
		t.Fatal("restored sender refused")
	}
	if delivered != 2 {
		t.Fatalf("delivered %d", delivered)
	}
}

// The isolated arm deliberately retains the original same-process arm above.
// JSON over pipes is test-only control traffic, never part of the relay protocol.
type wireIsolatedCommand struct {
	Publisher                   bool
	Seed, Key                   []byte
	Publications                []publisherCPUEnvelope
	Action, URL, Scenario       string
	Viewers, Op, Sessions, Want int
	Payload                     []byte
	Drop                        bool
	Delay                       time.Duration
}
type wireIsolatedSample struct{ UserNS, SystemNS, WallNS, StartUnixNS, EndUnixNS int64 }
type wireIsolatedReply struct {
	PID                 int
	Generated, Accepted int
	Settled             []uint64
	Publications        []publisherCPUEnvelope
	Duplicates          int
	PerViewer           []int
	URL                 string
	Sample              wireIsolatedSample
	Result              wireBenchResult
}

// Worker stderr can still be copied after reply decoding fails.
type wireBenchDiagnostics struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *wireBenchDiagnostics) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(p)
}
func (b *wireBenchDiagnostics) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

type wireIsolatedWorker struct {
	cmd    *exec.Cmd
	in     io.WriteCloser
	enc    *json.Encoder
	dec    *json.Decoder
	stderr wireBenchDiagnostics
}

func wireIsolatedStart(t *testing.T, role string) *wireIsolatedWorker {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	w := &wireIsolatedWorker{cmd: exec.Command(exe, "-test.run=^TestWireEfficiencyWorker$", "-test.timeout=60s")}
	w.cmd.Env = append(os.Environ(), "CLAWDLINE_WIRE_WORKER="+role)
	w.cmd.Stderr = &w.stderr
	var out io.ReadCloser
	w.in, err = w.cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err = w.cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	w.enc = json.NewEncoder(w.in)
	w.dec = json.NewDecoder(out)
	if err = w.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = w.in.Close()
		done := make(chan error, 1)
		go func() { done <- w.cmd.Wait() }()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("worker %s: %v: %s", role, err, w.stderr.String())
			} else {
				t.Logf("WIRE_WORKER_JOINED role=%s pid=%d", role, w.cmd.Process.Pid)
			}
		case <-time.After(5 * time.Second):
			_ = w.cmd.Process.Kill()
			<-done
			t.Errorf("worker %s cleanup timeout: %s", role, w.stderr.String())
		}
	})
	return w
}

// callErr bounds both pipe encoding and reply decoding. On timeout the worker
// is killed; registered cleanup still Waits it and joins the stderr copier.
func (w *wireIsolatedWorker) callErr(c wireIsolatedCommand) (wireIsolatedReply, error) {
	type outcome struct {
		reply wireIsolatedReply
		err   error
	}
	done := make(chan outcome, 1)
	go func() {
		var r wireIsolatedReply
		err := w.enc.Encode(c)
		if err == nil {
			err = w.dec.Decode(&r)
		}
		done <- outcome{r, err}
	}()
	select {
	case got := <-done:
		return got.reply, got.err
	case <-time.After(15 * time.Second):
		_ = w.cmd.Process.Kill()
		got := <-done
		return got.reply, fmt.Errorf("worker IPC timeout: %v", got.err)
	}
}
func (w *wireIsolatedWorker) call(t *testing.T, c wireIsolatedCommand) wireIsolatedReply {
	t.Helper()
	r, err := w.callErr(c)
	if err != nil {
		t.Fatalf("worker reply: %v: %s", err, w.stderr.String())
	}
	return r
}

// Each worker measures RUSAGE_SELF, never RUSAGE_CHILDREN. Machine accounting
// includes its pipe decoding/control overhead but excludes payload generation,
// fixture relay fanout and viewer signature verification/decryption.
func TestWireEfficiencyWorker(t *testing.T) {
	role := os.Getenv("CLAWDLINE_WIRE_WORKER")
	if role == "" {
		t.Skip("subprocess worker only")
	}
	if role == "publisher-early-exit" {
		return
	}
	if role == "malformed-diagnostics" {
		done := make(chan struct{})
		go func() {
			defer close(done)
			for i := 0; i < 10000; i++ {
				fmt.Fprintln(os.Stderr, "fixture worker diagnostic")
			}
		}()
		fmt.Fprintln(os.Stdout, "invalid worker reply")
		<-done
		return
	}
	u0, s0 := wireBenchCPUParts(t)
	wall0 := time.Now()
	sample := func() wireIsolatedSample {
		u, s := wireBenchCPUParts(t)
		return wireIsolatedSample{UserNS: u - u0, SystemNS: s - s0, WallNS: time.Since(wall0).Nanoseconds(), StartUnixNS: wall0.UnixNano(), EndUnixNS: time.Now().UnixNano()}
	}
	dec, enc := json.NewDecoder(os.Stdin), json.NewEncoder(os.Stdout)
	signer, err := domaincloud.DeviceKeyFromSeed(bytes.Repeat([]byte{17}, 32))
	if err != nil {
		t.Fatal(err)
	}
	key, err := domaincloud.ContentKeyFromBytes(bytes.Repeat([]byte{29}, 32))
	if err != nil {
		t.Fatal(err)
	}
	r := &wireBenchRelay{signer: signer}
	result := wireBenchResult{}
	var received atomic.Int64
	var viewerWG sync.WaitGroup
	var viewerMu sync.Mutex
	var viewerErr error
	var clients []*adaptercloud.Conn
	defer func() {
		for _, c := range clients {
			_ = c.Close()
		}
		viewerWG.Wait()
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var tr *adaptercloud.Transport
	var relay *Relay
	settled := make(chan uint64, 128)
	var transportDone chan struct{}
	var pub *publisherCPUDriver
	var evidence publisherCPUViewer
	defer func() {
		if tr != nil {
			cancel()
			tr.Stop()
			<-transportDone
		}
	}()
	for {
		var c wireIsolatedCommand
		if err := dec.Decode(&c); err == io.EOF {
			return
		} else if err != nil {
			t.Fatal(err)
		}
		reply := wireIsolatedReply{PID: os.Getpid()}
		switch c.Action {
		case "init":
			if c.Publisher {
				signer, err = domaincloud.DeviceKeyFromSeed(c.Seed)
				if err != nil {
					t.Fatal(err)
				}
				key, err = domaincloud.ContentKeyFromBytes(c.Key)
				if err != nil {
					t.Fatal(err)
				}
				r.signer = signer
			}
			switch role {
			case "relay":
				r.delay = c.Delay
				r.dropOnce = c.Drop
				if c.Publisher {
					var handlers sync.WaitGroup
					r.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
						handlers.Add(1)
						defer handlers.Done()
						r.serve(w, req)
					}))
					defer func() {
						r.server.Close()
						r.mu.Lock()
						for _, v := range r.viewers {
							_ = v.c.Close()
						}
						if r.machine != nil {
							_ = r.machine.c.Close()
						}
						r.mu.Unlock()
						handlers.Wait()
					}()
				} else {
					r.server = httptest.NewServer(http.HandlerFunc(r.serve))
					defer r.server.Close()
				}
				reply.URL = r.server.URL
			case "viewer":
				for i := 0; i < c.Viewers; i++ {
					conn, e := adaptercloud.Dial(c.URL+"?role=viewer", adaptercloud.DialOptions{NetDial: r.dial})
					if e != nil {
						t.Fatal(e)
					}
					clients = append(clients, conn)
					viewerID := i
					if c.Publisher {
						evidence.init(c.Viewers)
					}
					viewerWG.Add(1)
					go func() {
						defer viewerWG.Done()
						replay := domaincloud.NewReplayWindow(0)
						expected := 0
						fail := func(e error) { viewerMu.Lock(); viewerErr = e; viewerMu.Unlock() }
						for {
							b, e := conn.Read(time.Now().Add(30 * time.Second))
							if e != nil {
								return
							}
							var f adaptercloud.EnvelopeFrame
							if e = json.Unmarshal(b, &f); e != nil {
								fail(e)
								return
							}
							start := time.Now()
							env, e := domaincloud.DecodeEnvelope(f.Envelope)
							if e != nil {
								fail(e)
								return
							}
							plain, e := env.Open(key, func(id string) (ed25519.PublicKey, bool) { return signer.PublicKey(), id == "mac-01" })
							if e != nil {
								fail(e)
								return
							}
							viewerMu.Lock()
							result.OpenNS = append(result.OpenNS, time.Since(start).Nanoseconds())
							viewerMu.Unlock()
							if c.Publisher {
								evidence.record(viewerID, env.Seq, env.Ch, plain, replay.Claim(env.Sender, env.Seq) != domaincloud.ReplayAccepted)
								continue
							}
							if replay.Claim(env.Sender, env.Seq) != domaincloud.ReplayAccepted {
								continue
							}
							var payload struct {
								Op     int
								Source struct {
									Freshness  string `json:"freshness"`
									ObservedAt int    `json:"observed_at"`
								} `json:"source"`
							}
							if e = json.Unmarshal(plain, &payload); e != nil {
								fail(e)
								return
							}
							if payload.Op != expected || payload.Source.Freshness != "current" || payload.Source.ObservedAt != expected {
								fail(fmt.Errorf("stale/out of order: %+v expected %d", payload, expected))
								return
							}
							expected++
							received.Add(1)
						}
					}()
				}
			case "machine":
				spool, e := adaptercloud.NewSpool(adaptercloud.DefaultSpoolLimits(), nil, time.Now)
				if e != nil {
					t.Fatal(e)
				}
				tr, e = adaptercloud.New(adaptercloud.Options{RelayURL: c.URL, NetDial: r.dial, Token: &adaptercloud.TokenSource{Client: adaptercloud.NewAccountClient("http" + strings.TrimPrefix(strings.TrimSuffix(c.URL, "/v1/connect"), "ws")), Credential: "credential"}, Identity: adaptercloud.Identity{AccountID: "usr_test", MachineID: "mac-01", MachineCredential: "credential"}, Signer: signer, Spool: spool, Replay: domaincloud.NewReplayWindow(0), ContentKey: key, Jitter: func() float64 { return 0.5 }, OnAck: func(a adaptercloud.AckFrame) {
					if pub != nil {
						pub.p.Acked(a.Ch, a.Fanout)
					}
				}, OnSettled: func(_ string, seq uint64, kind adaptercloud.SettleKind) {
					if kind != adaptercloud.SettleDelivered {
						t.Error("non-delivery settlement")
					}
					settled <- seq
				}})
				if e != nil {
					t.Fatal(e)
				}
				transportDone = make(chan struct{})
				go func() { defer close(transportDone); _ = tr.Run(ctx) }()
				select {
				case <-tr.Ready():
				case <-time.After(5 * time.Second):
					t.Fatal("handshake timeout")
				}
				relay = &Relay{Transport: tr, Spool: spool, MachineID: "mac-01", Signer: signer, Secret: key}
				if c.Publisher {
					pub = publisherCPUInit(t, c.Payload, relay)
				}
			default:
				t.Fatal("unknown worker role")
			}
		case "publisher-pass":
			pub.body = c.Payload
			reply.Publications = pub.pass(t, ctx, c.Op, settled)
			reply.Generated = pub.generated
			reply.Accepted = len(pub.all)
			reply.Settled = append([]uint64(nil), pub.settled...)
		case "publisher-delivered":
			evidence.check(t, c.Publications)
		case "drop-next":
			r.mu.Lock()
			r.dropOnce = true
			r.mu.Unlock()
		case "registered":
			deadline := time.Now().Add(5 * time.Second)
			for {
				r.mu.Lock()
				n := len(r.viewers)
				r.mu.Unlock()
				if n == c.Viewers {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("viewer registration timeout")
				}
				time.Sleep(time.Millisecond)
			}
		case "mark":
			// Response ends the previous phase; the following phase starts here.
		case "publish":
			start := time.Now()
			seq, e := relay.PublishTracked(ctx, Outbound{Channel: fmt.Sprintf("t/mac-01/%%25%d", 10+c.Op%c.Sessions), Payload: c.Payload})
			if e != nil {
				t.Fatal(e)
			}
			result.SealNS = append(result.SealNS, time.Since(start).Nanoseconds())
			result.PlaintextBytes += int64(len(c.Payload))
			select {
			case got := <-settled:
				if got != seq {
					t.Fatalf("settled %d want %d", got, seq)
				}
			case <-time.After(8 * time.Second):
				t.Fatal("settlement timeout")
			}
		case "delivered":
			deadline := time.Now().Add(5 * time.Second)
			for received.Load() < int64(c.Want) {
				viewerMu.Lock()
				e := viewerErr
				viewerMu.Unlock()
				if e != nil {
					t.Fatal(e)
				}
				if time.Now().After(deadline) {
					t.Fatal("viewer delivery timeout")
				}
				time.Sleep(time.Millisecond)
			}
		case "result":
			r.mu.Lock()
			result.Connections = r.connections
			result.Acks = r.acks
			result.Retries = r.retries
			result.HTTPHandshakeBytes = r.handshakeBytes
			result.PublishWSBytes = r.frameBytes
			result.EnvelopeBytes = r.envelopeBytes
			result.CiphertextBytes = r.ciphertextBytes
			result.AckWSBytes = r.ackBytes
			result.ViewerWSBytes = r.viewerBytes
			r.mu.Unlock()
			viewerMu.Lock()
			reply.Result = result
			reply.Result.OpenNS = append([]int64(nil), result.OpenNS...)
			e := viewerErr
			viewerMu.Unlock()
			if e != nil {
				t.Fatal(e)
			}
			reply.Result.Deliveries = int(received.Load())
			reply.PerViewer, reply.Duplicates = evidence.counts()
		default:
			t.Fatalf("unknown control action %q", c.Action)
		}
		reply.Sample = sample()
		if err := enc.Encode(reply); err != nil {
			t.Fatal(err)
		}
		if c.Action == "mark" {
			u0, s0 = wireBenchCPUParts(t)
			wall0 = time.Now()
		}
	}
}

type wireIsolatedResult struct {
	Arm, Scenario       string
	ParentPID           int
	Generated, Accepted int
	Settled             []uint64
	WorkerPIDs          map[string]int
	Init, Idle, Active  map[string]wireIsolatedSample
	Result              wireBenchResult
}

func TestWireEfficiencyIsolated(t *testing.T) {
	if os.Getenv("CLAWDLINE_WIRE_ISOLATED") != "1" {
		t.Skip("set CLAWDLINE_WIRE_ISOLATED=1")
	}
	wireBenchCPUParts(t) // Unsupported platforms skip before spawning workers.
	for _, s := range []struct {
		name                   string
		sessions, viewers, ops int
		delay                  time.Duration
		drop                   bool
	}{
		{"initial_idle", 1, 1, 0, 0, false}, {"active_one", 1, 1, 12, 0, false}, {"active_multi_session", 16, 1, 32, 0, false}, {"independent_viewers", 4, 4, 16, 0, false}, {"reconnect_unacked", 1, 1, 12, 0, true}, {"ack_delay", 1, 1, 12, 25 * time.Millisecond, false},
	} {
		t.Run(s.name, func(t *testing.T) {
			u0, s0 := wireBenchCPUParts(t)
			wall0 := time.Now()
			parentSample := func() wireIsolatedSample {
				u, v := wireBenchCPUParts(t)
				return wireIsolatedSample{UserNS: u - u0, SystemNS: v - s0, WallNS: time.Since(wall0).Nanoseconds(), StartUnixNS: wall0.UnixNano(), EndUnixNS: time.Now().UnixNano()}
			}
			out := wireIsolatedResult{Arm: "isolated-processes-v1", Scenario: s.name, ParentPID: os.Getpid(), WorkerPIDs: map[string]int{}, Init: map[string]wireIsolatedSample{}, Idle: map[string]wireIsolatedSample{}, Active: map[string]wireIsolatedSample{}}
			workers := map[string]*wireIsolatedWorker{}
			rw := wireIsolatedStart(t, "relay")
			workers["relay"] = rw
			rr := rw.call(t, wireIsolatedCommand{Action: "init", Drop: s.drop, Delay: s.delay})
			out.WorkerPIDs["relay"] = rr.PID
			url := "ws" + strings.TrimPrefix(rr.URL, "http") + "/v1/connect"
			for _, role := range []string{"viewer", "machine"} {
				w := wireIsolatedStart(t, role)
				workers[role] = w
				r := w.call(t, wireIsolatedCommand{Action: "init", URL: url, Viewers: s.viewers})
				out.WorkerPIDs[role] = r.PID
			}
			rw.call(t, wireIsolatedCommand{Action: "registered", Viewers: s.viewers})
			for _, role := range []string{"relay", "viewer", "machine"} {
				out.Init[role] = workers[role].call(t, wireIsolatedCommand{Action: "mark"}).Sample
			}
			out.Init["parent_generator"] = parentSample()
			u0, s0 = wireBenchCPUParts(t)
			wall0 = time.Now()
			time.Sleep(200 * time.Millisecond)
			for _, role := range []string{"relay", "viewer", "machine"} {
				out.Idle[role] = workers[role].call(t, wireIsolatedCommand{Action: "mark"}).Sample
			}
			out.Idle["parent_generator"] = parentSample()
			u0, s0 = wireBenchCPUParts(t)
			wall0 = time.Now()
			result := wireBenchResult{Scenario: s.name, Sessions: s.sessions, Viewers: s.viewers, Operations: s.ops}
			for i := 0; i < s.ops; i++ {
				payload, e := json.Marshal(map[string]any{"op": i, "session": fmt.Sprintf("%%%d", 10+i%s.sessions), "source": map[string]any{"freshness": "current", "observed_at": i}, "text": strings.Repeat("x", 1024)})
				if e != nil {
					t.Fatal(e)
				}
				start := time.Now()
				workers["machine"].call(t, wireIsolatedCommand{Action: "publish", Payload: payload, Op: i, Sessions: s.sessions})
				workers["viewer"].call(t, wireIsolatedCommand{Action: "delivered", Want: (i + 1) * s.viewers})
				result.LatencyNS = append(result.LatencyNS, time.Since(start).Nanoseconds())
			}
			for _, role := range []string{"relay", "viewer", "machine"} {
				out.Active[role] = workers[role].call(t, wireIsolatedCommand{Action: "mark"}).Sample
			}
			out.Active["parent_generator"] = parentSample()
			rr = rw.call(t, wireIsolatedCommand{Action: "result"})
			mr := workers["machine"].call(t, wireIsolatedCommand{Action: "result"})
			vr := workers["viewer"].call(t, wireIsolatedCommand{Action: "result"})
			latency := result.LatencyNS
			result = rr.Result
			result.Scenario = s.name
			result.Sessions = s.sessions
			result.Viewers = s.viewers
			result.Operations = s.ops
			result.LatencyNS = latency
			result.PlaintextBytes = mr.Result.PlaintextBytes
			result.SealNS = mr.Result.SealNS
			result.OpenNS = vr.Result.OpenNS
			result.Deliveries = vr.Result.Deliveries
			result.HTTPHandshakeBytes = mr.Result.HTTPHandshakeBytes + vr.Result.HTTPHandshakeBytes
			result.P50NS = wireBenchPercentile(result.LatencyNS, 50)
			result.P95NS = wireBenchPercentile(result.LatencyNS, 95)
			result.SealP50NS = wireBenchPercentile(result.SealNS, 50)
			result.SealP95NS = wireBenchPercentile(result.SealNS, 95)
			result.OpenP50NS = wireBenchPercentile(result.OpenNS, 50)
			result.OpenP95NS = wireBenchPercentile(result.OpenNS, 95)
			if result.Acks != s.ops || result.Deliveries != s.ops*s.viewers {
				t.Fatalf("incomplete serial delivery %+v", result)
			}
			if s.drop && (result.Retries != 1 || result.Connections != 2) {
				t.Fatalf("retry byte equality/reconnect failed %+v", result)
			}
			if !s.drop && result.Retries != 0 {
				t.Fatal("unexpected retry")
			}
			out.Result = result
			b, e := json.Marshal(out)
			if e != nil {
				t.Fatal(e)
			}
			t.Log("WIRE_ISOLATED_RESULT " + string(b))
		})
	}
}

// Exercise the actual os/exec stderr copier during a malformed reply.
func TestWireEfficiencyMalformedWorkerDiagnostics(t *testing.T) {
	w := wireIsolatedStart(t, "malformed-diagnostics")
	var reply wireIsolatedReply
	if err := w.dec.Decode(&reply); err == nil {
		t.Fatal("malformed reply accepted")
	}
	for i := 0; i < 1000; i++ {
		_ = w.stderr.String()
	}
}
