package cloud

// An opt-in, synthetic loopback service. This is not an account API or a roster.
import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pion/ice/v4"
	"github.com/pion/webrtc/v4"
	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
	"github.com/sainteye/clawdline/internal/domain/terminal"
)

type browserFixtureViewer struct {
	public        ed25519.PublicKey
	token         string
	admitted      bool
	peer          *wireBenchPeer
	subscriptions map[string]bool
	highest       uint64
	terminal      string
	direct        *browserFixtureDirect
}
type browserFixture struct {
	sockets                                            map[net.Conn]bool
	closing                                            bool
	t                                                  *testing.T
	mu                                                 sync.Mutex
	signer                                             domaincloud.DeviceKey
	key                                                domaincloud.ContentKey
	viewers                                            map[string]*browserFixtureViewer
	seq                                                uint64
	counters                                           map[string]int64
	output, bundle, sourceHash, binaryHash, bundleHash string
	stop                                               chan struct{}
	once                                               sync.Once
	wg                                                 sync.WaitGroup
}

func newBrowserFixture(t *testing.T, output, bundle string) *browserFixture {
	t.Helper()
	signer, e := domaincloud.NewDeviceKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	key, e := domaincloud.NewContentKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	f := &browserFixture{sockets: map[net.Conn]bool{}, t: t, signer: signer, key: key, viewers: map[string]*browserFixtureViewer{}, counters: map[string]int64{}, output: output, bundle: bundle, stop: make(chan struct{})}
	digest := func(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
	exe, _ := os.Executable()
	b, _ := os.ReadFile(exe)
	f.binaryHash = digest(b)
	b, _ = os.ReadFile(bundle)
	f.bundleHash = digest(b)
	_, sourceFile, _, _ := runtime.Caller(0)
	root := os.Getenv("CLAWDLINE_BROWSER_SOURCE_ROOT")
	if root == "" {
		root = filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../.."))
	}
	h := sha256.New()
	for _, p := range []string{"internal/domain/persona/catalog/performance.md", "internal/transport/cloud/browser_efficiency_fixture_test.go", "web/console/tools/cloud-browser-baseline-entry.mjs", "web/console/tools/cloud-browser-baseline.mjs", "web/console/src/legacy/js/net/cloud-client.js", "web/console/src/legacy/js/net/cloud-crypto.js"} {
		b, e := os.ReadFile(filepath.Join(root, p))
		if e != nil {
			t.Fatal(e)
		}
		h.Write([]byte(p))
		h.Write(b)
	}
	f.sourceHash = hex.EncodeToString(h.Sum(nil))
	return f
}
func (f *browserFixture) close() {
	f.once.Do(func() { close(f.stop) })
	f.mu.Lock()
	f.closing = true
	for c := range f.sockets {
		_ = c.Close()
	}
	viewers := make([]*browserFixtureViewer, 0, len(f.viewers))
	for _, v := range f.viewers {
		viewers = append(viewers, v)
		if v.peer != nil {
			v.peer.c.Close()
		}
	}
	f.mu.Unlock()
	f.wg.Wait()
	for _, v := range viewers {
		if v.direct != nil {
			d := v.direct
			d.mu.Lock()
			d.closed = true
			peers := append([]*webrtc.PeerConnection(nil), d.peers...)
			d.mu.Unlock()
			d.l.closeAllDirectPeers()
			for _, pc := range peers {
				_ = pc.GracefulClose()
			}
			d.l.closeDirectMux()
		}
	}
}
func (f *browserFixture) count(k string, n int64) { f.mu.Lock(); f.counters[k] += n; f.mu.Unlock() }
func (f *browserFixture) snapshot() map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := map[string]int64{}
	for k, v := range f.counters {
		c[k] = v
	}
	userNS, systemNS := wireBenchCPUParts(f.t)
	return map[string]any{"counters": c, "source_sha256": f.sourceHash, "binary_sha256": f.binaryHash, "bundle_sha256": f.bundleHash, "cpu_subject": "combined Go fixture machine crypto + relay + HTTP + Go runtime (not browser)", "process_user_cpu_ns": userNS, "process_system_cpu_ns": systemNS, "browser_cpu": nil, "ws_bytes_boundary": "JSON UTF-8 payload only; excludes WS framing, HTTP headers, TCP/IP"}
}
func (f *browserFixture) json(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func (f *browserFixture) serve(w http.ResponseWriter, r *http.Request) {
	f.count("http_requests", 1)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if local, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr); ok && r.Host != local.String() {
		http.Error(w, "loopback host required", 403)
		return
	}
	origin := "http://" + r.Host
	if r.Header.Get("Origin") != "" && r.Header.Get("Origin") != origin {
		http.Error(w, "origin", 403)
		return
	}
	if r.Method == "GET" {
		switch r.URL.Path {
		case "/", "/frame":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			io.WriteString(w, browserFixtureHTML)
			return
		case "/entry.js":
			w.Header().Set("Content-Type", "text/javascript")
			http.ServeFile(w, r, f.bundle)
			return
		case "/stats":
			f.json(w, f.snapshot())
			return
		case "/v1/connect":
			f.socket(w, r)
			return
		}
	}
	if r.Method != "POST" {
		http.NotFound(w, r)
		return
	}
	if r.Header.Get("Origin") != origin {
		http.Error(w, "origin required", 403)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	defer r.Body.Close()
	switch r.URL.Path {
	case "/register":
		var q struct {
			ID     string `json:"id"`
			Public string `json:"public"`
		}
		if json.NewDecoder(r.Body).Decode(&q) != nil || !strings.HasPrefix(q.ID, "synthetic-") || len(q.ID) > 100 {
			http.Error(w, "registration", 400)
			return
		}
		pub, e := base64.StdEncoding.DecodeString(q.Public)
		if e != nil || len(pub) != 32 {
			http.Error(w, "public key", 400)
			return
		}
		b := make([]byte, 24)
		rand.Read(b)
		token := hex.EncodeToString(b)
		f.mu.Lock()
		_, exists := f.viewers[q.ID]
		if !exists {
			v := &browserFixtureViewer{public: pub, token: token, admitted: true, subscriptions: map[string]bool{}, terminal: string(terminal.NewID())}
			v.direct = f.newDirect(q.ID, v)
			f.viewers[q.ID] = v
		}
		f.mu.Unlock()
		if exists {
			http.Error(w, "duplicate device", 409)
			return
		}
		f.mu.Lock()
		terminalID := f.viewers[q.ID].terminal
		f.mu.Unlock()
		f.json(w, map[string]any{"token": token, "master": base64.StdEncoding.EncodeToString(f.key.Bytes()), "sender": base64.StdEncoding.EncodeToString(f.signer.PublicKey()), "machine": "synthetic-machine", "terminal": terminalID})
	case "/control":
		var q struct {
			Action  string `json:"action"`
			ID      string `json:"id"`
			Session string `json:"session"`
			Canary  string `json:"canary"`
			Seq     uint64 `json:"seq"`
		}
		if json.NewDecoder(r.Body).Decode(&q) != nil {
			http.Error(w, "control", 400)
			return
		}
		f.mu.Lock()
		v := f.viewers[q.ID]
		f.mu.Unlock()
		if v == nil {
			http.Error(w, "unknown synthetic viewer", 404)
			return
		}
		switch q.Action {
		case "direct-drop":
			if v.direct == nil {
				http.Error(w, "no synthetic direct peer", 409)
				return
			}
			v.direct.l.closeDirectPeerOf(q.ID, "fixture controlled peer loss")
			f.count("direct_peer_drops", 1)
		case "disconnect":
			f.mu.Lock()
			p := v.peer
			f.mu.Unlock()
			if p != nil {
				p.c.Close()
			}
		case "revoke", "restore":
			f.mu.Lock()
			v.admitted = q.Action == "restore"
			f.mu.Unlock()
		case "publish", "duplicate":
			if q.Session != "one" && q.Session != "two" {
				http.Error(w, "session", 400)
				return
			}
			if !strings.HasPrefix(q.Canary, "canary-") || len(q.Canary) > 160 {
				http.Error(w, "canary", 400)
				return
			}
			seq, e := f.publish(q.ID, q.Session, map[string]any{"read": "transcript", "body": map[string]any{"canary": q.Canary}}, q.Seq)
			if e != nil {
				http.Error(w, e.Error(), 409)
				return
			}
			f.json(w, map[string]any{"seq": seq})
			return
		default:
			http.Error(w, "action", 400)
			return
		}
		f.json(w, map[string]bool{"ok": true})
	case "/collect":
		// A typed allowlist prevents headers, credentials and arbitrary documents entering raw output.
		var q browserFixtureResult
		d := json.NewDecoder(r.Body)
		d.DisallowUnknownFields()
		if d.Decode(&q) != nil || q.Round < 1 || q.Round > 3 || q.Kind != "native-browser" || len(q.Scenarios) > 40 || len(q.Failure) > 300 || len(q.Environment.UserAgent) > 300 {
			http.Error(w, "measurement schema", 400)
			return
		}
		for _, scenario := range q.Scenarios {
			for _, proof := range scenario.Proofs {
				if !strings.HasPrefix(proof.Context, "synthetic-") || len(proof.Context) > 100 || !strings.HasPrefix(proof.Canary, "canary-") || len(proof.Canary) > 160 || (proof.Session != "one" && proof.Session != "two" && proof.Session != "terminal") {
					http.Error(w, "synthetic proof fields", 400)
					return
				}
			}
			if len(scenario.Proofs) > 16 || len(scenario.Name) > 64 || len(scenario.Before.Counters) > 32 || len(scenario.After.Counters) > 32 {
				http.Error(w, "measurement fields", 400)
				return
			}
			for _, sample := range []browserFixtureSample{scenario.Before, scenario.After} {
				for key := range sample.Counters {
					switch key {
					case "viewer_publish_frames", "duplicate_viewer_publications", "injected_duplicates", "terminal_frame_observed", "http_requests", "ws_out_json_bytes", "ws_out_frames", "ws_in_json_bytes", "ws_in_frames", "subscribe", "unsubscribe", "connections", "bad_hello", "machine_publications", "relay_delivered", "accepted_commands", "invalid_command", "acks", "logical_reads", "admission_refused", "direct_peer_drops", "direct_native_requests", "terminal_requests", "terminal_malformed", "direct_frames_delivered", "terminal_relay_frames_delivered", "native_direct_offers":
					default:
						http.Error(w, "unknown measurement counter", 400)
						return
					}
				}
			}
		}
		b, _ := json.MarshalIndent(map[string]any{"browser": q, "server": f.snapshot()}, "", "  ")
		if e := os.WriteFile(filepath.Join(f.output, fmt.Sprintf("round-%d.json", q.Round)), b, 0600); e != nil {
			http.Error(w, "write", 500)
			return
		}
		f.json(w, map[string]bool{"stored": true})
	case "/stop":
		f.once.Do(func() { close(f.stop) })
		f.json(w, map[string]bool{"ok": true})
	default:
		http.NotFound(w, r)
	}
}

type browserFixtureSample struct {
	Counters  map[string]int64 `json:"counters"`
	UserCPU   int64            `json:"process_user_cpu_ns"`
	SystemCPU int64            `json:"process_system_cpu_ns"`
}
type browserFixtureProof struct {
	Context  string  `json:"context"`
	Canary   string  `json:"canary"`
	Sequence *uint64 `json:"sequence"`
	Session  string  `json:"session"`
}
type browserFixtureScenario struct {
	Proofs          []browserFixtureProof `json:"proofs"`
	NativeRTC       bool                  `json:"native_rtc"`
	SameClient      bool                  `json:"same_client_fallback"`
	DirectThenRelay bool                  `json:"direct_then_relay"`
	Checkpoints     int                   `json:"verified_checkpoints"`
	Before          browserFixtureSample  `json:"fixture_before"`
	After           browserFixtureSample  `json:"fixture_after"`
	Name            string                `json:"name"`
	Viewers         int                   `json:"viewers"`
	Reads           int                   `json:"reads"`
	Delivered       int                   `json:"delivered"`
	Errors          int                   `json:"errors"`
	Duration        float64               `json:"duration_ms"`
	Memory          *float64              `json:"memory_bytes"`
	Pass            bool                  `json:"pass"`
}
type browserFixtureEnvironment struct {
	UserAgent string `json:"user_agent"`
	Secure    bool   `json:"secure_context"`
	Isolated  bool   `json:"cross_origin_isolated"`
}
type browserFixtureResult struct {
	Environment browserFixtureEnvironment `json:"environment"`
	Kind        string                    `json:"kind"`
	Round       int                       `json:"round"`
	Pass        bool                      `json:"pass"`
	Failure     string                    `json:"failure"`
	Scenarios   []browserFixtureScenario  `json:"scenarios"`
}

func (f *browserFixture) send(p *wireBenchPeer, v any) error {
	b, _ := json.Marshal(v)
	f.count("ws_out_json_bytes", int64(len(b)))
	f.count("ws_out_frames", 1)
	return p.send(v)
}
func (f *browserFixture) publish(id, session string, payload any, forced uint64) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v := f.viewers[id]
	if v == nil || v.peer == nil {
		return 0, fmt.Errorf("viewer disconnected")
	}
	if !v.admitted {
		f.counters["admission_refused"]++
		return 0, fmt.Errorf("fixture receiver revoked")
	}
	ch := "t/synthetic-machine/" + session
	if !v.subscriptions[ch] {
		return 0, fmt.Errorf("not subscribed")
	}
	f.seq++
	seq := f.seq
	if forced != 0 {
		f.counters["injected_duplicates"]++
		seq = forced
	}
	b, _ := json.Marshal(payload)
	env, e := domaincloud.Seal(b, domaincloud.SealParams{Ch: ch, Seq: seq, Ts: uint64(time.Now().UnixMilli()), Class: domaincloud.ClassStream, KeyID: "ms-1", Sender: "synthetic-machine", Key: f.key, Signer: f.signer})
	if e != nil {
		return 0, e
	}
	encoded, e := env.CanonicalJSON()
	if e != nil {
		return 0, e
	}
	frame := map[string]any{"type": "envelope", "envelope": json.RawMessage(encoded)}
	raw, _ := json.Marshal(frame)
	f.counters["ws_out_json_bytes"] += int64(len(raw))
	f.counters["ws_out_frames"]++
	f.counters["machine_publications"]++
	e = v.peer.send(frame)
	if e == nil {
		f.counters["relay_delivered"]++
	}
	return seq, e
}

// Test-only bounded reader: tiny fixture messages never need fragmentation.
func browserFixtureRead(br *bufio.Reader) ([]byte, error) {
	h, e := br.Peek(2)
	if e != nil {
		return nil, e
	}
	opcode := h[0] & 15
	if h[0]&128 == 0 {
		return nil, fmt.Errorf("fixture does not accept fragmented messages")
	}
	size := uint64(h[1] & 127)
	if size == 126 {
		h, e = br.Peek(4)
		if e != nil {
			return nil, e
		}
		size = uint64(binary.BigEndian.Uint16(h[2:4]))
	} else if size == 127 {
		h, e = br.Peek(10)
		if e != nil {
			return nil, e
		}
		size = binary.BigEndian.Uint64(h[2:10])
	}
	if size > 1<<20 {
		return nil, fmt.Errorf("fixture message too large")
	}
	b, _, e := wireBenchRead(br)
	if e != nil {
		return nil, e
	}
	if opcode == 8 {
		return nil, io.EOF
	}
	if opcode != 1 {
		return nil, fmt.Errorf("fixture expects text messages")
	}
	return b, nil
}
func (f *browserFixture) socket(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	f.mu.Lock()
	v := f.viewers[id]
	f.mu.Unlock()
	if v == nil || r.Header.Get("Origin") != "http://"+r.Host || !strings.Contains(r.Header.Get("Sec-WebSocket-Protocol"), "clawdline.token."+v.token) {
		http.Error(w, "synthetic authentication", 403)
		return
	}
	c, rw, e := w.(http.Hijacker).Hijack()
	if e != nil {
		return
	}
	f.mu.Lock()
	if f.closing {
		f.mu.Unlock()
		c.Close()
		return
	}
	f.sockets[c] = true
	f.wg.Add(1)
	f.mu.Unlock()
	defer func() { f.mu.Lock(); delete(f.sockets, c); f.mu.Unlock(); f.wg.Done() }()
	defer c.Close()
	c.SetDeadline(time.Now().Add(15 * time.Minute))
	sum := sha1.Sum([]byte(r.Header.Get("Sec-WebSocket-Key") + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Protocol: clawdline.v1\r\nSec-WebSocket-Accept: %s\r\n\r\n", base64.StdEncoding.EncodeToString(sum[:]))
	if rw.Flush() != nil {
		return
	}
	p := &wireBenchPeer{c: c}
	challengeRaw := make([]byte, 32)
	rand.Read(challengeRaw)
	challenge := base64.StdEncoding.EncodeToString(challengeRaw)
	f.send(p, adaptercloud.ChallengeFrame{Type: "challenge", V: 1, Context: adaptercloud.ChallengeContext, Account: "synthetic-account", Device: id, Challenge: challenge, ExpiresInMs: 15000})
	raw, e := browserFixtureRead(rw.Reader)
	if e != nil {
		return
	}
	f.count("ws_in_json_bytes", int64(len(raw)))
	f.count("ws_in_frames", 1)
	var hello adaptercloud.HelloFrame
	json.Unmarshal(raw, &hello)
	sig, _ := base64.StdEncoding.DecodeString(hello.Sig)
	if !ed25519.Verify(v.public, []byte(adaptercloud.ChallengeString("synthetic-account", id, challenge)), sig) {
		f.count("bad_hello", 1)
		return
	}
	f.mu.Lock()
	v.peer = p
	v.subscriptions = map[string]bool{}
	f.counters["connections"]++
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		if v.peer == p {
			v.peer = nil
		}
		f.mu.Unlock()
	}()
	f.send(p, adaptercloud.ReadyFrame{Type: "ready", V: 1, Account: "synthetic-account", Device: id, Role: "viewer", ConnectedAt: time.Now().UnixMilli(), TokenExpiresAt: time.Now().Add(15 * time.Minute).UnixMilli()})
	for {
		raw, e := browserFixtureRead(rw.Reader)
		if e != nil {
			return
		}
		f.count("ws_in_json_bytes", int64(len(raw)))
		f.count("ws_in_frames", 1)
		var q struct {
			Type     string          `json:"type"`
			Channels []string        `json:"channels"`
			Envelope json.RawMessage `json:"envelope"`
		}
		if json.Unmarshal(raw, &q) != nil {
			return
		}
		switch q.Type {
		case "subscribe", "unsubscribe":
			f.mu.Lock()
			for _, ch := range q.Channels {
				if q.Type == "subscribe" {
					v.subscriptions[ch] = true
				} else {
					delete(v.subscriptions, ch)
				}
			}
			chs := []string{}
			for ch := range v.subscriptions {
				chs = append(chs, ch)
			}
			f.counters[q.Type]++
			f.mu.Unlock()
			f.send(p, map[string]any{"type": "subscriptions", "channels": chs})
		case "ping":
			f.send(p, map[string]string{"type": "pong"})
		case "terminal_frame_observed":
			f.count("terminal_frame_observed", 1)
		case "publish":
			f.count("viewer_publish_frames", 1)
			env, e := domaincloud.DecodeEnvelope(q.Envelope)
			if e != nil {
				return
			}
			plain, e := env.Open(f.key, func(s string) (ed25519.PublicKey, bool) { return v.public, s == id })
			if e != nil {
				f.count("invalid_command", 1)
				return
			}
			f.mu.Lock()
			allowed := v.admitted && (env.Ch == "ctl/synthetic-machine" || env.Ch == "termi/synthetic-machine/"+id) && env.Seq > v.highest
			if env.Seq <= v.highest {
				f.counters["duplicate_viewer_publications"]++
			}
			if allowed {
				v.highest = env.Seq
				f.counters["accepted_commands"]++
			}
			f.mu.Unlock()
			if !allowed {
				f.send(p, map[string]any{"type": "publish_error", "ch": env.Ch, "seq": env.Seq, "code": "forbidden"})
				continue
			}
			f.send(p, map[string]any{"type": "ack", "ch": env.Ch, "seq": env.Seq, "status": "delivered"})
			f.count("acks", 1)
			if env.Ch == "termi/synthetic-machine/"+id {

				v.direct.request(plain)
				continue
			}
			var cmd struct {
				Type    string `json:"type"`
				Session string `json:"session"`
			}
			json.Unmarshal(plain, &cmd)
			if cmd.Type == "screen" && (cmd.Session == "one" || cmd.Session == "two") {
				f.count("logical_reads", 1)
				f.publish(id, cmd.Session, map[string]any{"read": "screen", "body": map[string]any{"canary": "canary-screen-" + cmd.Session, "session": cmd.Session}}, 0)
			}
		}
	}
}

const browserFixtureHTML = `<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Synthetic native browser baseline</title><style>body{font:16px system-ui;max-width:1000px;margin:24px auto;padding:12px}button{padding:12px}pre{white-space:pre-wrap;overflow-wrap:anywhere}iframe{width:220px;height:70px;border:1px solid #aaa}</style><h1>Synthetic native browser baseline</h1><p>Loopback fixture only. No account, real sessions, external API or STUN/TURN. Browser results require real native WebSocket + WebCrypto in independent iframes.</p><button id="start">Start round</button><p id="status">Ready; run three rounds.</p><div id="frames"></div><pre id="raw"></pre><script type="module" src="/entry.js"></script></html>`

func TestBrowserEfficiencyFixture(t *testing.T) {
	if os.Getenv("CLAWDLINE_BROWSER_FIXTURE") != "1" {
		t.Skip("opt-in synthetic loopback service")
	}
	out := os.Getenv("CLAWDLINE_BROWSER_OUTPUT")
	bundle := os.Getenv("CLAWDLINE_BROWSER_BUNDLE")
	if out == "" || bundle == "" {
		t.Fatal("explicit output and bundle required")
	}
	if e := os.MkdirAll(out, 0700); e != nil {
		t.Fatal(e)
	}
	f := newBrowserFixture(t, out, bundle)
	s := httptest.NewUnstartedServer(http.HandlerFunc(f.serve))
	l, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	_ = s.Listener.Close() // NewUnstartedServer owns a default listener; replace it without leaking it.
	s.Listener = l
	s.Start()
	ready, _ := json.Marshal(map[string]any{"url": s.URL, "pid": os.Getpid()})
	os.WriteFile(filepath.Join(out, "ready.json"), ready, 0600)
	fmt.Printf("BROWSER_FIXTURE_READY url=%s pid=%d\n", s.URL, os.Getpid())
	os.Stdout.Sync()
	<-f.stop
	s.Listener.Close()
	f.close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	s.Config.Shutdown(ctx)
	s.Close()
	b, _ := json.MarshalIndent(f.snapshot(), "", "  ")
	os.WriteFile(filepath.Join(out, "server-final.json"), b, 0600)
}

// TestBrowserEfficiencyFixtureSmoke is a Go peer smoke, never browser evidence.
func TestBrowserEfficiencyFixtureSmoke(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture process CPU sampling uses the existing Unix helper")
	}
	out := t.TempDir()
	bundle := filepath.Join(out, "entry.js")
	if e := os.WriteFile(bundle, []byte("// synthetic smoke"), 0600); e != nil {
		t.Fatal(e)
	}
	f := newBrowserFixture(t, out, bundle)
	s := httptest.NewServer(http.HandlerFunc(f.serve))
	defer s.Close()
	defer f.close()
	post := func(path string, body any, want int) []byte {
		t.Helper()
		b, _ := json.Marshal(body)
		r, _ := http.NewRequest("POST", s.URL+path, strings.NewReader(string(b)))
		r.Header.Set("Origin", s.URL)
		r.Header.Set("Content-Type", "application/json")
		resp, e := http.DefaultClient.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != want {
			t.Fatalf("%s status %d want %d", path, resp.StatusCode, want)
		}
		return raw
	}
	signer, e := domaincloud.NewDeviceKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	id := "synthetic-go-smoke"
	var config struct {
		Token string `json:"token"`
	}
	json.Unmarshal(post("/register", map[string]string{"id": id, "public": base64.StdEncoding.EncodeToString(signer.PublicKey())}, 200), &config)
	dial := func(token string) (*adaptercloud.Conn, error) {
		return adaptercloud.Dial("ws"+strings.TrimPrefix(s.URL, "http")+"/v1/connect?id="+id, adaptercloud.DialOptions{Header: http.Header{"Origin": []string{s.URL}}, Subprotocols: []string{"clawdline.v1", "clawdline.token." + token}})
	}
	if c, e := dial("wrong-token"); e == nil {
		c.Close()
		t.Fatal("wrong token admitted")
	}
	read := func(c *adaptercloud.Conn) map[string]json.RawMessage {
		t.Helper()
		b, e := c.Read(time.Now().Add(3 * time.Second))
		if e != nil {
			t.Fatal(e)
		}
		var v map[string]json.RawMessage
		if json.Unmarshal(b, &v) != nil {
			t.Fatal("frame JSON")
		}
		return v
	}
	write := func(c *adaptercloud.Conn, v any) {
		t.Helper()
		b, _ := json.Marshal(v)
		if e := c.WriteText(b, time.Now().Add(3*time.Second)); e != nil {
			t.Fatal(e)
		}
	}
	connect := func() *adaptercloud.Conn {
		t.Helper()
		c, e := dial(config.Token)
		if e != nil {
			t.Fatal(e)
		}
		q := read(c)
		var challenge string
		json.Unmarshal(q["challenge"], &challenge)
		sig := signer.Sign([]byte(adaptercloud.ChallengeString("synthetic-account", id, challenge)))
		write(c, map[string]any{"type": "hello", "sig": base64.StdEncoding.EncodeToString(sig)})
		q = read(c)
		if string(q["type"]) != `"ready"` {
			t.Fatal("no ready")
		}
		return c
	}
	c := connect()
	defer c.Close()
	subscribe := func(c *adaptercloud.Conn) {
		write(c, map[string]any{"type": "subscribe", "channels": []string{"t/synthetic-machine/one"}})
		if string(read(c)["type"]) != `"subscriptions"` {
			t.Fatal("no subscriptions")
		}
	}
	subscribe(c)
	open := func(frame map[string]json.RawMessage) domaincloud.Envelope {
		t.Helper()
		env, e := domaincloud.DecodeEnvelope(frame["envelope"])
		if e != nil {
			t.Fatal(e)
		}
		plain, e := env.Open(f.key, func(id string) (ed25519.PublicKey, bool) { return f.signer.PublicKey(), id == "synthetic-machine" })
		if e != nil || !strings.Contains(string(plain), "canary-") {
			t.Fatal("synthetic canary decryption failed")
		}
		return env
	}
	post("/control", map[string]any{"id": id, "action": "publish", "session": "one", "canary": "canary-smoke"}, 200)
	env := open(read(c))
	replay := domaincloud.NewReplayWindow(64)
	if replay.Claim(env.Sender, env.Seq) != domaincloud.ReplayAccepted {
		t.Fatal("positive rejected")
	}
	post("/control", map[string]any{"id": id, "action": "duplicate", "session": "one", "canary": "canary-smoke", "seq": env.Seq}, 200)
	duplicate := open(read(c))
	if replay.Claim(duplicate.Sender, duplicate.Seq) == domaincloud.ReplayAccepted {
		t.Fatal("duplicate accepted")
	}
	command, e := domaincloud.Seal([]byte(`{"type":"screen","session":"one"}`), domaincloud.SealParams{Ch: "ctl/synthetic-machine", Seq: 1, Ts: uint64(time.Now().UnixMilli()), Class: domaincloud.ClassCtl, KeyID: "ms-1", Sender: id, Key: f.key, Signer: signer})
	if e != nil {
		t.Fatal(e)
	}
	commandRaw, e := command.CanonicalJSON()
	if e != nil {
		t.Fatal(e)
	}
	write(c, map[string]any{"type": "publish", "envelope": json.RawMessage(commandRaw)})
	if string(read(c)["type"]) != `"ack"` {
		t.Fatal("no ACK")
	}
	open(read(c))
	post("/control", map[string]any{"id": id, "action": "revoke"}, 200)
	post("/control", map[string]any{"id": id, "action": "publish", "session": "one", "canary": "canary-refused"}, 409)
	post("/control", map[string]any{"id": id, "action": "restore"}, 200)
	post("/control", map[string]any{"id": id, "action": "publish", "session": "one", "canary": "canary-restored"}, 200)
	open(read(c))
	post("/control", map[string]any{"id": id, "action": "disconnect"}, 200)
	c.Close()
	c = connect()
	defer c.Close()
	subscribe(c)
	post("/control", map[string]any{"id": id, "action": "publish", "session": "one", "canary": "canary-reconnected"}, 200)
	open(read(c))
	post("/collect", map[string]any{"kind": "native-browser", "round": 1, "authorization": "must-not-store"}, 400)
	post("/collect", browserFixtureResult{Kind: "native-browser", Round: 1, Pass: true, Scenarios: []browserFixtureScenario{{Name: "go-smoke-not-browser", Pass: true}}}, 200)
	if _, e := os.Stat(filepath.Join(out, "round-1.json")); e != nil {
		t.Fatal(e)
	}
	r, _ := http.NewRequest("POST", s.URL+"/control", strings.NewReader(`{}`))
	r.Header.Set("Origin", "https://unrelated.invalid")
	resp, e := http.DefaultClient.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatal("cross-origin control admitted")
	}
	resp, e = http.Get(s.URL + "/internal/domain/persona/catalog/performance.md")
	if e != nil {
		t.Fatal(e)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatal("repository was served")
	}
	f.mu.Lock()
	counters := map[string]int64{}
	for k, v := range f.counters {
		counters[k] = v
	}
	f.mu.Unlock()
	if counters["connections"] != 2 || counters["accepted_commands"] != 1 || counters["admission_refused"] != 1 || counters["relay_delivered"] != 5 {
		t.Fatalf("unexpected counters: %v", counters)
	}
	t.Logf("GO PEER ONLY: handshake, decrypt, duplicate, revoke/restore, same-viewer reconnect, collector and origin/path negatives passed; counters=%v", counters)
	// Pion-to-Pion smoke exercises the isolated production Link. This is not Chrome.
	d := f.viewers[id].direct
	connectionKey, err := domaincloud.NewContentKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	req := terminalRequest{V: 1, Type: "terminal_request", RequestID: testRequestID, Connection: testConnection, Operation: "open_connection", KeyID: testKeyID, Key: base64.StdEncoding.EncodeToString(connectionKey.Bytes())}
	requestBytes, _ := json.Marshal(req)
	d.request(requestBytes)
	receipt := read(c)
	if string(receipt["type"]) != `"envelope"` {
		t.Fatal("terminal registration receipt missing")
	}
	browser, offer := newTestBrowser(t)
	body := sealDirectOffer(t, connectionKey, testConnection, offer)
	req.Operation = "direct_offer"
	req.Body = body
	requestBytes, _ = json.Marshal(req)
	d.request(requestBytes)
	envAnswer, err := domaincloud.DecodeEnvelope(read(c)["envelope"])
	if err != nil {
		t.Fatal(err)
	}
	answerPlain, err := envAnswer.Open(connectionKey, func(string) (ed25519.PublicKey, bool) { return f.signer.PublicKey(), true })
	if err != nil {
		t.Fatal(err)
	}
	var answer struct {
		Result struct {
			SDP string `json:"sdp"`
		} `json:"result"`
	}
	json.Unmarshal(answerPlain, &answer)
	if answer.Result.SDP == "" {
		t.Fatal("empty native peer answer")
	}
	if strings.Contains(answer.Result.SDP, " typ srflx ") {
		t.Fatal("STUN candidate leaked")
	}
	if err := browser.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: answer.Result.SDP}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-browser.opened:
	case <-time.After(5 * time.Second):
		t.Fatal("Pion peer did not open")
	}
	deadline := time.Now().Add(3 * time.Second)
	for !d.l.directPeerOpen(id) {
		if time.Now().After(deadline) {
			t.Fatal("server peer did not open")
		}
		time.Sleep(time.Millisecond)
	}
	terminalConnection := d.l.getTerminalConnection(id, testConnection)
	terminalConnection.carrier = carrierDirect
	d.frame(terminalConnection)
	message := browser.next(t)
	if message.T != "env" {
		t.Fatal("native direct envelope missing")
	}
	directEnvelope, err := domaincloud.DecodeEnvelope(message.E)
	if err != nil {
		t.Fatal(err)
	}
	directPlain, err := directEnvelope.Open(connectionKey, func(string) (ed25519.PublicKey, bool) { return f.signer.PublicKey(), true })
	if err != nil || !strings.Contains(string(directPlain), "canary-native-direct") {
		t.Fatal("direct canary not decrypted")
	}
	req = terminalRequest{V: 1, Type: "terminal_request", RequestID: "c6000001-0000-4000-8000-000000000002", Connection: testConnection, Operation: "control", TerminalID: d.terminal, Client: "synthetic-go-tab"}
	requestBytes, _ = json.Marshal(req)
	inbound, err := domaincloud.Seal(requestBytes, domaincloud.SealParams{Ch: "termi/synthetic-machine/" + id, Seq: 2, Ts: uint64(time.Now().UnixMilli()), Class: domaincloud.ClassCtl, KeyID: "ms-1", Sender: id, Key: f.key, Signer: signer})
	if err != nil {
		t.Fatal(err)
	}
	inboundRaw, _ := inbound.CanonicalJSON()
	wire, _ := json.Marshal(directMessage{T: "env", E: inboundRaw})
	if err := browser.dc.SendText(string(wire)); err != nil {
		t.Fatal(err)
	}
	if string(read(c)["type"]) != `"envelope"` {
		t.Fatal("native direct request did not produce receipt")
	}
	browser.next(t)
	if d.l.transport.AcceptDirect(id, inboundRaw) {
		t.Fatal("production direct admission accepted duplicate sequence")
	}
	f.mu.Lock()
	nativeRequests := f.counters["direct_native_requests"]
	f.mu.Unlock()
	if nativeRequests != 1 {
		t.Fatalf("direct native request count %d", nativeRequests)
	}
	d.l.closeDirectPeerOf(id, "Go smoke controlled loss")
	if d.l.directPeerOpen(id) {
		t.Fatal("controlled peer loss ineffective")
	}
	t.Log("GO PION ONLY: production Link loopback ICE, sealed offer/answer, direct canary and controlled peer loss passed; native browser fallback remains unexecuted")

}

// The remote terminal inventory/control replies are synthetic fixture service.
// The direct carrier, Pion peer, envelope admission and DC sealing are production Link code.
// No OS terminal, account roster or production metadata endpoint is used.
type browserFixtureDirect struct {
	closed   bool
	peers    []*webrtc.PeerConnection
	mu       sync.Mutex
	f        *browserFixture
	id       string
	l        *Link
	frames   map[string]uint64
	seq      uint64
	terminal string
}

func (f *browserFixture) newDirect(id string, v *browserFixtureViewer) *browserFixtureDirect {
	d := &browserFixtureDirect{f: f, id: id, frames: map[string]uint64{}, terminal: v.terminal}
	d.l = &Link{identity: adaptercloud.Identity{MachineID: "synthetic-machine"}, opts: LinkOptions{Now: time.Now}, settings: adaptercloud.Settings{TerminalDirect: true}, directLoopback: true, directNoSTUN: true, terminalConnections: map[string]*terminalConnection{}, terminalMetadata: func(context.Context, string, *terminalConnection) error { return nil }}
	se := webrtc.SettingEngine{}
	se.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)
	se.SetIncludeLoopbackCandidate(true)
	se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	se.SetIPFilter(func(ip net.IP) bool { return ip.IsLoopback() })
	d.l.directWebRTC = webrtc.NewAPI(webrtc.WithSettingEngine(se))
	d.l.relay = &Relay{Signer: f.signer, Secret: f.key, MachineID: "synthetic-machine"}
	tr, e := adaptercloud.New(adaptercloud.Options{RelayURL: "wss://synthetic.invalid", Token: &adaptercloud.TokenSource{}, Identity: adaptercloud.Identity{AccountID: "synthetic-account", MachineID: "synthetic-machine", MachineCredential: "synthetic-credential"}, Signer: f.signer, Replay: domaincloud.NewReplayWindow(64), ContentKey: f.key, PublicKeyFor: func(sender string) (ed25519.PublicKey, bool) { return v.public, sender == id }, Inbound: func(_ domaincloud.Envelope, plain []byte, _ ed25519.PublicKey) {
		f.count("direct_native_requests", 1)
		d.request(plain)
	}})
	if e != nil {
		f.t.Fatal(e)
	}
	d.l.transport = tr
	return d
}
func (d *browserFixtureDirect) relay(c *terminalConnection, kind string, payload any) error {
	d.seq++
	raw, _ := json.Marshal(payload)
	class := domaincloud.ClassCtl
	if kind == "term" {
		class = domaincloud.ClassStream
	}
	env, e := domaincloud.Seal(raw, domaincloud.SealParams{Ch: kind + "/synthetic-machine/" + d.id + "/" + c.id, Seq: d.seq, Ts: uint64(time.Now().UnixMilli()), Class: class, KeyID: c.keyID, Sender: "synthetic-machine", Key: c.key, Signer: d.f.signer})
	if e != nil {
		return e
	}
	d.f.mu.Lock()
	p := d.f.viewers[d.id].peer
	d.f.mu.Unlock()
	if p == nil {
		return fmt.Errorf("fixture socket lost")
	}
	encoded, e := env.CanonicalJSON()
	if e != nil {
		return e
	}
	return d.f.send(p, map[string]any{"type": "envelope", "envelope": json.RawMessage(encoded)})
}
func (d *browserFixtureDirect) frame(c *terminalConnection) {
	d.frames[c.id]++
	canary := "canary-native-relay"
	if c.carrier == carrierDirect {
		canary = "canary-native-direct"
	}
	now := float64(time.Now().UnixMilli()) / 1000
	payload := map[string]any{"v": 1, "type": "terminal_frame", "connection": c.id, "terminal_id": d.terminal, "frame_seq": d.frames[c.id], "captured_at": now, "frame": map[string]any{"rev": fmt.Sprint(d.frames[c.id]), "at": now, "cols": 80, "rows": 1, "lines": []string{canary}, "cursor": map[string]any{"x": 0, "y": 0, "visible": true}, "modes": map[string]any{"app_cursor": false, "app_keypad": false, "mouse_sgr": false, "alt": false, "mouse": "none"}}}
	if c.carrier == carrierDirect {
		b, _ := json.Marshal(payload)
		if d.l.sendDirectEnvelope(c, "term/synthetic-machine/"+d.id+"/"+c.id, domaincloud.ClassStream, b) == nil {
			d.f.count("direct_frames_delivered", 1)
		}
	} else {
		if d.relay(c, "term", payload) == nil {
			d.f.count("terminal_relay_frames_delivered", 1)
		}
	}
}
func (d *browserFixtureDirect) request(plain []byte) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}
	req, e := decodeTerminalRequest(plain)
	if e != nil {
		d.f.count("terminal_malformed", 1)
		return
	}
	d.f.count("terminal_requests", 1)
	c := d.l.getTerminalConnection(d.id, req.Connection)
	var result any = map[string]any{}
	status, code := "ok", ""
	sendFrame := false
	switch req.Operation {
	case "open_connection", "rekey_connection":
		key, e := parseConnectionKey(req)
		if e != nil {
			return
		}
		c = &terminalConnection{viewer: d.id, id: req.Connection, keyID: req.KeyID, key: key, expires: time.Now().Add(5 * time.Minute), terminalID: terminal.ID(d.terminal), receipts: map[string][]byte{}}
		var body struct {
			Carrier string `json:"carrier"`
			Old     string `json:"old_connection"`
		}
		json.Unmarshal(req.Body, &body)
		if body.Carrier == "direct" {
			if !d.l.directPeerOpen(d.id) {
				status = "refused"
				code = "terminal_direct_unavailable"
			} else {
				c.carrier = carrierDirect
			}
		}
		d.l.terminalMu.Lock()
		d.l.terminalConnections[terminalConnectionID(d.id, c.id)] = c
		d.l.terminalMu.Unlock()
		result = map[string]any{"connection": c.id, "key_id": c.keyID, "expires_at": float64(c.expires.UnixMilli()) / 1000, "machine_incarnation": "synthetic-incarnation"}
		if c.carrier == carrierDirect {
			result.(map[string]any)["carrier"] = "direct"
		}
		sendFrame = req.Operation == "rekey_connection"
	case "direct_offer":
		if c == nil {
			return
		}
		sdp, e := openDirectOffer(c, req.Body)
		if e != nil {
			status = "refused"
			code = "terminal_invalid"
		} else if e = d.l.directOfferAdmitted(d.id); e != nil {
			status = "refused"
			code = "terminal_busy"
		} else {
			lines := []string{}
			for _, line := range strings.Split(sdp, "\n") {
				if strings.HasPrefix(line, "a=candidate:") {
					fields := strings.Fields(line)
					if len(fields) < 5 || net.ParseIP(fields[4]) == nil || !net.ParseIP(fields[4]).IsLoopback() {
						continue
					}
				}
				lines = append(lines, line)
			}
			sdp = strings.Join(lines, "\n")
			answer, e := d.l.answerDirectOffer(d.id, c.id, false, c.id, sdp)
			if e != nil {
				status = "refused"
				code = "terminal_direct_unavailable"
			} else {
				d.l.directMu.Lock()
				if peer := d.l.directPeers[d.id]; peer != nil {
					d.peers = append(d.peers, peer.pc)
				}
				d.l.directMu.Unlock()
				d.l.directAnswerSent(d.id)
				result = map[string]any{"sdp": answer, "direct_receipts": false}
				d.f.count("native_direct_offers", 1)
			}
		}
	case "read", "capture", "control":
		if c == nil {
			return
		}
		result = map[string]any{"control": map[string]any{"held": true, "holder": map[string]any{"same_client": true, "same_device": true}, "epoch": 1, "applied_through": 0, "expires_at": float64(time.Now().Add(time.Minute).UnixMilli()) / 1000}, "machine_incarnation": "synthetic-incarnation"}
		sendFrame = true
	case "activate_connection":
		if c == nil {
			return
		}
		var body struct {
			Old string `json:"old_connection"`
		}
		json.Unmarshal(req.Body, &body)
		result = map[string]any{"connection": c.id, "retired_connection": body.Old}
		if c.carrier == carrierDirect {
			d.l.moveDirectOwner(d.id, c.id)
		}
	case "release_connection":
		if c == nil {
			return
		}
	default:
		status = "refused"
		code = "terminal_unsupported"
	}
	if c == nil {
		return
	}
	receipt := terminalReceipt{V: 1, Type: "terminal_receipt", RequestID: req.RequestID, Connection: req.Connection, Operation: req.Operation, TerminalID: req.TerminalID, Status: status, Error: code, Result: result}
	d.relay(c, "termr", receipt)
	if sendFrame && status == "ok" {
		d.frame(c)
	}
}
