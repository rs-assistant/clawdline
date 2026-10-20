package cloud

// Opt-in process accounting. Inventory, relay and viewers are fixtures; no
// production protocol or scheduling interval is changed by these commands.
import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
)

type publisherCPUEnvelope struct {
	Seq     uint64
	Channel string
	Payload []byte
}
type publisherCPUDriver struct {
	clocks    []int
	settled   []uint64
	p         *Publisher
	relay     *Relay
	rows      []workingRow
	body      []byte
	now       time.Time
	all       []publisherCPUEnvelope
	generated int
}

func publisherCPUInput(sessions int) *publisherCPUDriver {
	d := &publisherCPUDriver{rows: make([]workingRow, sessions), clocks: make([]int, sessions)}
	for i := range d.rows {
		d.rows[i] = workingRow{id: fmt.Sprintf("%%%d", 10+i), state: "working", verb: "Thinking", activity: 100, tokens: 50}
	}
	d.body = scanOf(0, d.rows)
	return d
}
func publisherCPUInit(t *testing.T, body []byte, relay *Relay) *publisherCPUDriver {
	d := &publisherCPUDriver{relay: relay, now: time.Unix(5000, 0), body: body}
	d.p = drivenPublisher(&collector{}, &d.body, &d.now)
	d.p.published = map[string][32]byte{}
	d.p.sent = map[string]time.Time{}
	d.p.inventoried = map[string]inventoriedRow{}
	d.p.Publish = func(ctx context.Context, out Outbound) error {
		d.generated++
		seq, err := relay.PublishTracked(ctx, out)
		if err != nil {
			return err
		}
		d.all = append(d.all, publisherCPUEnvelope{seq, out.Channel, append([]byte(nil), out.Payload...)})
		return nil
	}
	return d
}
func (d *publisherCPUDriver) pass(t *testing.T, ctx context.Context, op int, settled <-chan uint64) []publisherCPUEnvelope {
	t.Helper()
	if op > 0 {
		d.now = d.now.Add(SnapshotInterval)
	}
	before := len(d.all)
	d.p.Pass(ctx)
	if d.p.unsent != 0 || d.generated != len(d.all) {
		t.Fatal("generated/accepted mismatch")
	}
	if op == 0 && before != 0 && len(d.all) != before {
		t.Fatal("idle publication")
	}
	if (op > 0 || before == 0) && len(d.all) == before {
		t.Fatal("missing publication")
	}
	for _, want := range d.all[before:] {
		select {
		case got := <-settled:
			d.settled = append(d.settled, got)
			if got != want.Seq {
				t.Fatalf("settled %d want %d", got, want.Seq)
			}
		case <-time.After(8 * time.Second):
			t.Fatal("publisher settlement timeout")
		}
	}
	return append([]publisherCPUEnvelope(nil), d.all...)
}

// Input generation stays in the parent_generator process, outside machine CPU.
func (d *publisherCPUDriver) input(t *testing.T, op int) []byte {
	if op > 0 {
		row := &d.rows[(op-1)%len(d.rows)]
		if row.state == "working" {
			row.state = "waiting"
		} else {
			row.state = "working"
		}
		row.activity++
		d.clocks[(op-1)%len(d.rows)] = op * 5
		rows := make([]map[string]any, len(d.rows))
		for i, r := range d.rows {
			rows[i] = r.row(d.clocks[i])
		}
		var err error
		d.body, err = json.Marshal(map[string]any{"at": 1000 + op*5, "scan": map[string]any{"complete": true}, "sessions": rows})
		if err != nil {
			t.Fatal(err)
		}
	}
	return append([]byte(nil), d.body...)
}

type publisherCPUViewer struct {
	mu         sync.Mutex
	rows       [][]publisherCPUEnvelope
	duplicates int
}

func (v *publisherCPUViewer) init(n int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.rows == nil {
		v.rows = make([][]publisherCPUEnvelope, n)
	}
}
func (v *publisherCPUViewer) record(i int, seq uint64, ch string, b []byte, duplicate bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if duplicate {
		v.duplicates++
		return
	}
	v.rows[i] = append(v.rows[i], publisherCPUEnvelope{seq, ch, append([]byte(nil), b...)})
}
func (v *publisherCPUViewer) counts() ([]int, int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	n := make([]int, len(v.rows))
	for i := range n {
		n[i] = len(v.rows[i])
	}
	return n, v.duplicates
}
func (v *publisherCPUViewer) check(t *testing.T, want []publisherCPUEnvelope) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		v.mu.Lock()
		ready := true
		for i, rows := range v.rows {
			if len(rows) < len(want) {
				ready = false
				continue
			}
			if len(rows) != len(want) {
				v.mu.Unlock()
				t.Fatal("excess publication")
			}
			for j, got := range rows {
				w := want[j]
				if got.Seq != w.Seq || got.Channel != w.Channel || !bytes.Equal(got.Payload, w.Payload) {
					v.mu.Unlock()
					t.Fatalf("viewer %d publication %d differs", i, j)
				}
				var payload map[string]json.RawMessage
				if err := json.Unmarshal(got.Payload, &payload); err != nil {
					v.mu.Unlock()
					t.Fatal(err)
				}
				if raw := payload["session"]; len(raw) > 0 {
					var row struct {
						Source struct {
							Freshness  string
							ObservedAt int `json:"observed_at"`
						}
					}
					if err := json.Unmarshal(raw, &row); err != nil || row.Source.Freshness != "current" || row.Source.ObservedAt < 1000 {
						v.mu.Unlock()
						t.Fatal("invalid source metadata")
					}
				}
			}
		}
		v.mu.Unlock()
		if ready {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("publisher viewer delivery timeout")
		}
		time.Sleep(time.Millisecond)
	}
}

type publisherCPUPass struct {
	Generated, Accepted int
	Settled             []uint64
}
type publisherCPUResult struct {
	PassEvidence       []publisherCPUPass
	Arm, Scenario      string
	ParentPID          int
	WorkerPIDs         map[string]int
	Init, Idle, Active map[string]wireIsolatedSample
	PassAccepted       []int
	Publications       []publisherCPUEnvelope
	PerViewer          []int
	Duplicates         int
	Wire               wireBenchResult
}

func TestPublisherWireCPU(t *testing.T) {
	if os.Getenv("CLAWDLINE_PUBLISHER_CPU") != "1" {
		t.Skip("set CLAWDLINE_PUBLISHER_CPU=1")
	}
	wireBenchCPUParts(t)
	for _, s := range []struct {
		name                   string
		sessions, viewers, ops int
		drop                   bool
	}{{"idle", 1, 1, 0, false}, {"active_one", 1, 1, 4, false}, {"active_multi_session", 4, 1, 4, false}, {"independent_viewers", 4, 4, 4, false}, {"reconnect_unacked", 1, 1, 4, true}} {
		t.Run(s.name, func(t *testing.T) {
			u0, s0 := wireBenchCPUParts(t)
			wall0 := time.Now()
			parent := func() wireIsolatedSample {
				u, v := wireBenchCPUParts(t)
				return wireIsolatedSample{UserNS: u - u0, SystemNS: v - s0, WallNS: time.Since(wall0).Nanoseconds(), StartUnixNS: wall0.UnixNano(), EndUnixNS: time.Now().UnixNano()}
			}
			input := publisherCPUInput(s.sessions)
			seed, key := make([]byte, 32), make([]byte, 32)
			if _, e := rand.Read(seed); e != nil {
				t.Fatal(e)
			}
			if _, e := rand.Read(key); e != nil {
				t.Fatal(e)
			}
			out := publisherCPUResult{Arm: "publisher-processes-v1", Scenario: s.name, ParentPID: os.Getpid(), WorkerPIDs: map[string]int{}, Init: map[string]wireIsolatedSample{}, Idle: map[string]wireIsolatedSample{}, Active: map[string]wireIsolatedSample{}}
			workers := map[string]*wireIsolatedWorker{}
			for _, role := range []string{"relay", "viewer", "machine"} {
				workers[role] = wireIsolatedStart(t, role)
			}
			rr := workers["relay"].call(t, wireIsolatedCommand{Action: "init", Publisher: true, Seed: seed, Key: key})
			url := "ws" + rr.URL[len("http"):] + "/v1/connect"
			out.WorkerPIDs["relay"] = rr.PID
			for _, role := range []string{"viewer", "machine"} {
				r := workers[role].call(t, wireIsolatedCommand{Action: "init", Publisher: true, Seed: seed, Key: key, URL: url, Viewers: s.viewers, Sessions: s.sessions, Payload: input.body})
				out.WorkerPIDs[role] = r.PID
			}
			workers["relay"].call(t, wireIsolatedCommand{Action: "registered", Viewers: s.viewers})
			pass := func(op int) {
				r := workers["machine"].call(t, wireIsolatedCommand{Action: "publisher-pass", Op: op, Payload: input.input(t, op)})
				want := 2*s.sessions + 3 + op*3
				if r.Accepted != want {
					t.Fatalf("pass %d accepted %d want %d", op, r.Accepted, want)
				}
				workers["viewer"].call(t, wireIsolatedCommand{Action: "publisher-delivered", Publications: r.Publications})
				out.PassAccepted = append(out.PassAccepted, len(r.Publications))
				if r.Generated != r.Accepted || r.Accepted != len(r.Publications) || len(r.Settled) != r.Accepted {
					t.Fatal("pass receipt mismatch")
				}
				out.PassEvidence = append(out.PassEvidence, publisherCPUPass{r.Generated, r.Accepted, r.Settled})
				out.Publications = r.Publications
			}
			mark := func(dst map[string]wireIsolatedSample) {
				for _, role := range []string{"relay", "viewer", "machine"} {
					dst[role] = workers[role].call(t, wireIsolatedCommand{Action: "mark"}).Sample
				}
				dst["parent_generator"] = parent()
				u0, s0 = wireBenchCPUParts(t)
				wall0 = time.Now()
			}
			pass(0)
			mark(out.Init)
			for i := 0; i < 3; i++ {
				pass(0)
			}
			time.Sleep(200 * time.Millisecond)
			mark(out.Idle)
			if s.drop {
				workers["relay"].call(t, wireIsolatedCommand{Action: "drop-next"})
			}
			for op := 1; op <= s.ops; op++ {
				pass(op)
			}
			mark(out.Active)
			out.Wire = workers["relay"].call(t, wireIsolatedCommand{Action: "result"}).Result
			vr := workers["viewer"].call(t, wireIsolatedCommand{Action: "result"})
			out.PerViewer = vr.PerViewer
			out.Duplicates = vr.Duplicates
			for _, n := range out.PerViewer {
				if n != len(out.Publications) {
					t.Fatal("viewer count mismatch")
				}
			}
			retries := 0
			if s.drop {
				retries = 1
			}
			if out.Wire.Acks != len(out.Publications) || out.Wire.Retries != retries || out.Duplicates != retries*s.viewers || out.Wire.Connections != 1+retries {
				t.Fatalf("wire evidence mismatch %+v", out.Wire)
			}
			b, e := json.Marshal(out)
			if e != nil {
				t.Fatal(e)
			}
			t.Log("PUBLISHER_CPU_RESULT " + string(b))
		})
	}
}

// The worker exits cleanly before reading its command. A test-level PASS on
// stdout must never count as a valid IPC reply; cleanup still Waits the PID.
func TestPublisherWireCPUWorkerExit(t *testing.T) {
	w := wireIsolatedStart(t, "publisher-early-exit")
	if _, err := w.callErr(wireIsolatedCommand{Action: "mark"}); err == nil {
		t.Fatal("early exit accepted as success")
	}
}
