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
		if p.send(ack) != nil {
			return
		}
		r.mu.Lock()
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
			defer func() {
				for _, c := range clients {
					_ = c.Close()
				}
				viewerWG.Wait()
			}()
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
