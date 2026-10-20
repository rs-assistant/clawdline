package cloud

// Opt-in production Publisher.Pass -> Relay -> Spool -> Transport -> local WS
// baseline. Inventory and relay are fixtures; all CPU is combined-process.
import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPublisherWireBaseline(t *testing.T) {
	if os.Getenv("CLAWDLINE_PUBLISHER_WIRE_BENCH") != "1" {
		t.Skip("set CLAWDLINE_PUBLISHER_WIRE_BENCH=1 for the isolated baseline")
	}
	for _, s := range []struct {
		name                   string
		sessions, viewers, ops int
		delay                  time.Duration
		drop                   bool
	}{
		{"idle", 1, 1, 0, 0, false}, {"active_one", 1, 1, 4, 0, false}, {"active_multi_session", 4, 1, 4, 0, false}, {"independent_viewers", 4, 4, 4, 0, false}, {"reconnect_unacked", 1, 1, 4, 0, true},
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
			r := &wireBenchRelay{signer: signer, delay: s.delay, dropOnce: false}
			var handlers atomic.Int64
			r.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				handlers.Add(1)
				defer handlers.Add(-1)
				r.serve(w, req)
			}))
			defer func() {
				r.server.Close()
				deadline := time.Now().Add(5 * time.Second)
				for handlers.Load() != 0 {
					if time.Now().After(deadline) {
						t.Errorf("fixture handlers remain: %d", handlers.Load())
						return
					}
					time.Sleep(time.Millisecond)
				}
				t.Log("PUBLISHER_WIRE_CLEANUP transport stopped, viewer readers joined, HTTP server closed, no fixture handlers remain")
			}()
			url := "ws" + strings.TrimPrefix(r.server.URL, "http") + "/v1/connect"
			var received atomic.Int64
			perViewer := make([]atomic.Int64, s.viewers)
			var duplicates atomic.Int64
			var expectedMu sync.Mutex
			type publication struct {
				seq uint64
				out Outbound
			}
			var expectedPublications []publication
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
				go func(viewer int) {
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
							duplicates.Add(1)
							continue
						}
						expectedMu.Lock()
						if expected >= len(expectedPublications) {
							expectedMu.Unlock()
							t.Error("unexpected publication")
							return
						}
						want := expectedPublications[expected]
						expectedMu.Unlock()
						if env.Seq != want.seq || env.Ch != want.out.Channel || !bytes.Equal(plain, want.out.Payload) {
							t.Errorf("publication %d changed channel, sequence or plaintext", expected)
							return
						}
						var payload map[string]json.RawMessage
						if err := json.Unmarshal(plain, &payload); err != nil {
							t.Error(err)
							return
						}
						if raw := payload["session"]; len(raw) > 0 {
							var row struct {
								Source struct {
									Freshness  string
									ObservedAt int `json:"observed_at"`
								}
							}
							if err := json.Unmarshal(raw, &row); err != nil || row.Source.Freshness != "current" || row.Source.ObservedAt < 1000 {
								t.Errorf("missing source metadata: %s", raw)
								return
							}
						}
						expected++
						perViewer[viewer].Add(1)
						received.Add(1)
					}
				}(i)
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
			now := time.Unix(5000, 0)
			rows := make([]workingRow, s.sessions)
			for i := range rows {
				rows[i] = workingRow{id: fmt.Sprintf("%%%d", 10+i), state: "working", verb: "Thinking", activity: 100, tokens: 50}
			}
			body := scanOf(0, rows)
			p := drivenPublisher(&collector{}, &body, &now)
			p.published = map[string][32]byte{}
			p.sent = map[string]time.Time{}
			p.inventoried = map[string]inventoriedRow{}
			tr, e := adaptercloud.New(adaptercloud.Options{RelayURL: url, NetDial: r.dial, Token: &adaptercloud.TokenSource{Client: adaptercloud.NewAccountClient(r.server.URL), Credential: "credential"}, Identity: adaptercloud.Identity{AccountID: "usr_test", MachineID: "mac-01", MachineCredential: "credential"}, Signer: signer, Spool: spool, Replay: domaincloud.NewReplayWindow(0), ContentKey: key, Jitter: func() float64 { return 0.5 }, OnAck: func(a adaptercloud.AckFrame) { p.Acked(a.Ch, a.Fanout) }, OnSettled: func(_ string, seq uint64, kind adaptercloud.SettleKind) {
				if kind != adaptercloud.SettleDelivered {
					t.Errorf("non-delivery settlement: %v", kind)
				}
				settled <- seq
			}})
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
			var passes, generated, accepted int
			var passNS, passCompletionNS []int64
			p.Publish = func(ctx context.Context, out Outbound) error {
				generated++
				expectedMu.Lock()
				defer expectedMu.Unlock()
				start := time.Now()
				seq, err := relay.PublishTracked(ctx, out)
				result.SealNS = append(result.SealNS, time.Since(start).Nanoseconds())
				if err != nil {
					return err
				}
				out.Payload = append([]byte(nil), out.Payload...)
				expectedPublications = append(expectedPublications, publication{seq, out})
				accepted++
				result.PlaintextBytes += int64(len(out.Payload))
				return nil
			}
			runPass := func() {
				t.Helper()
				before := accepted
				start := time.Now()
				p.Pass(ctx)
				passes++
				passNS = append(passNS, time.Since(start).Nanoseconds())
				if p.unsent != 0 || generated != accepted {
					t.Fatalf("Publisher refused publications: %d", p.unsent)
				}
				for j := before; j < accepted; j++ {
					select {
					case seq := <-settled:
						if seq != expectedPublications[j].seq {
							t.Fatalf("settled %d want %d", seq, expectedPublications[j].seq)
						}
					case <-time.After(8 * time.Second):
						t.Fatal("settlement timeout")
					}
				}
				want := int64(accepted * s.viewers)
				deadline := time.Now().Add(5 * time.Second)
				for received.Load() < want {
					if t.Failed() || time.Now().After(deadline) {
						t.Fatalf("viewer delivery %d want %d", received.Load(), want)
					}
					time.Sleep(time.Millisecond)
				}
				if received.Load() != want {
					t.Fatalf("excess viewer delivery")
				}
				passCompletionNS = append(passCompletionNS, time.Since(start).Nanoseconds())
			}
			initCPU := wireBenchCPU(t)
			initPassStart := time.Now()
			runPass()
			initPassNS := time.Since(initPassStart).Nanoseconds()
			initPassCPU := wireBenchCPU(t) - initCPU
			initPublications := accepted
			if initPublications < s.sessions {
				t.Fatal("initial inventory did not publish rows")
			}
			idleStart, cpuStart := time.Now(), wireBenchCPU(t)
			for i := 0; i < 3; i++ {
				runPass()
			}
			time.Sleep(200 * time.Millisecond)
			result.IdleNS = time.Since(idleStart).Nanoseconds()
			result.IdleCPUNS = wireBenchCPU(t) - cpuStart
			if accepted != initPublications {
				t.Fatal("unchanged idle pass published unexpectedly")
			}
			r.mu.Lock()
			r.dropOnce = s.drop
			r.mu.Unlock()
			wallStart, cpuStart := time.Now(), wireBenchCPU(t)
			for i := 0; i < s.ops; i++ {
				now = now.Add(SnapshotInterval)
				row := &rows[i%len(rows)]
				if row.state == "working" {
					row.state = "waiting"
				} else {
					row.state = "working"
				}
				row.activity++
				body = scanOf((i+1)*5, rows)
				before := accepted
				runPass()
				if accepted <= before {
					t.Fatal("changed session did not publish")
				}
			}
			result.WallNS = time.Since(wallStart).Nanoseconds()
			result.ProcessCPUNS = wireBenchCPU(t) - cpuStart
			result.Operations = accepted
			result.LatencyNS = passCompletionNS

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
			if result.Acks != accepted {
				t.Fatalf("ack count %d want %d", result.Acks, accepted)
			}
			if result.Deliveries != accepted*s.viewers {
				t.Fatalf("deliveries %d", result.Deliveries)
			}
			if s.drop && (result.Retries != 1 || result.Connections != 2) {
				t.Fatalf("recovery %+v", result)
			}
			if !s.drop && result.Retries != 0 {
				t.Fatal("unexpected retry")
			}
			if duplicates.Load() != int64(result.Retries*s.viewers) {
				t.Fatal("replay dedup count mismatch")
			}
			viewerDeliveries := make([]int64, len(perViewer))
			for i := range perViewer {
				viewerDeliveries[i] = perViewer[i].Load()
				if viewerDeliveries[i] != int64(accepted) {
					t.Fatalf("viewer %d delivered %d want %d", i, viewerDeliveries[i], accepted)
				}
			}
			b, e := json.Marshal(struct {
				Wire                                          wireBenchResult
				Passes, Generated, Accepted, InitPublications int
				Duplicates                                    int64
				InitPassNS, InitPassCombinedCPUNS             int64
				PassNS, PassCompletionNS                      []int64
				PerViewerDeliveries                           []int64
			}{result, passes, generated, accepted, initPublications, duplicates.Load(), initPassNS, initPassCPU, passNS, passCompletionNS, viewerDeliveries})
			if e != nil {
				t.Fatal(e)
			}
			t.Log("PUBLISHER_WIRE_RESULT " + string(b))
		})
	}
}
