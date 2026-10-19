package cloud

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"sort"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
)

// efficiencyMeter exercises real reservation, encryption/signing, envelope
// serialization and spool transitions. Frame construction replaces socket I/O;
// settlement is synchronous and synthetic, NOT a measured relay receipt.
type efficiencyMeter struct {
	relay                                    *Relay
	publisher                                *Publisher
	fanout                                   int
	reads, frames, plain, sealed, wire, acks int64
	relayNS, drainNS, ackNS                  int64
}

func (m *efficiencyMeter) publish(ctx context.Context, out Outbound) error {
	start := time.Now()
	_, err := m.relay.PublishTracked(ctx, out)
	m.relayNS += time.Since(start).Nanoseconds()
	if err != nil {
		return err
	}
	m.plain += int64(len(out.Payload))
	start = time.Now()
	d := m.relay.Spool.SendNext()
	if d.Row == nil {
		return fmt.Errorf("benchmark spool did not drain: %+v", d)
	}
	m.frames++
	m.sealed += int64(len(d.Row.Sealed))
	m.wire += int64(len(adaptercloud.PublishFrame(d.Row.Sealed)))
	m.drainNS += time.Since(start).Nanoseconds()
	start = time.Now()
	_, err = m.relay.Spool.Settle(d.Row.Seq, d.Row.Recipient, adaptercloud.SettleDelivered)
	if err != nil {
		return err
	}
	m.publisher.Acked(out.Channel, m.fanout)
	m.acks++
	m.ackNS += time.Since(start).Nanoseconds()
	return nil
}

// BenchmarkPublisherEfficiency reports cost per 300 simulated seconds (60
// five-second passes). Fixture construction and warm-up are outside the timer.
// Each window starts from the same warm state with a fresh in-memory spool.
// The source list is already built, as with Publisher.Sessions in production;
// local screen capture and HTTP, network waits, disk fences, reconnect handshakes,
// multiple browsers, retries and slow acknowledgements are deliberately absent.
func BenchmarkPublisherEfficiency(b *testing.B) {
	for _, workload := range []struct {
		name                       string
		sessions                   int
		unwatched, active, restate bool
	}{
		{"idle_1", 1, false, false, false},
		{"idle_32", 32, false, false, false},
		{"unwatched_32", 32, true, false, false},
		{"active_32", 32, false, true, false},
		{"restate_32", 32, false, false, true},
	} {
		b.Run(workload.name, func(b *testing.B) {
			b.StopTimer()
			ctx := context.Background()
			rows := make([]workingRow, workload.sessions)
			for i := range rows {
				rows[i] = workingRow{id: fmt.Sprintf("%%%d", 10+i), state: "working", verb: "Thinking", activity: 100, tokens: 50, agents: 2}
			}
			bodies := make([][]byte, 61)
			bodies[0] = scanOf(0, rows)
			for step := 1; step <= 60; step++ {
				if workload.active {
					r := &rows[(step-1)%len(rows)]
					if r.state == "working" {
						r.state = "waiting"
					} else {
						r.state = "working"
					}
					r.activity = int64(100 + step*5)
					bodies[step] = scanOf(step*5, rows)
				} else {
					bodies[step] = bodies[0]
				}
			}
			signer, err := domaincloud.NewDeviceKey(rand.Reader)
			if err != nil {
				b.Fatal(err)
			}
			secret, err := domaincloud.NewContentKey(rand.Reader)
			if err != nil {
				b.Fatal(err)
			}
			var total efficiencyMeter
			latencies := make([]int64, 0, b.N*60)
			for iteration := 0; iteration < b.N; iteration++ {
				now := time.Unix(1700000000, 0)
				spool, err := adaptercloud.NewSpool(adaptercloud.DefaultSpoolLimits(), nil, func() time.Time { return now })
				if err != nil {
					b.Fatal(err)
				}
				body := bodies[0]
				p := drivenPublisher(&collector{}, &body, &now)
				m := &efficiencyMeter{relay: &Relay{Transport: &adaptercloud.Transport{}, Spool: spool, MachineID: "mac-01", Signer: signer, Secret: secret, Now: func() time.Time { return now }}, publisher: p, fanout: 1}
				if workload.unwatched {
					m.fanout = 0
				}
				p.Sessions = func(context.Context) ([]byte, bool) { m.reads++; return body, true }
				p.Publish = m.publish
				p.firstPass(ctx)
				if p.unsent != 0 {
					b.Fatalf("warm-up refused %d publications", p.unsent)
				}
				// Advance past the warm receipt's timestamp so zero fanout
				// is newer than the last viewer evidence on an unwatched pass.
				if workload.unwatched {
					now = now.Add(time.Second)
					p.Acked("orch/mac-01", 0)
				}
				m.reads, m.frames, m.plain, m.sealed, m.wire, m.acks, m.relayNS, m.drainNS, m.ackNS = 0, 0, 0, 0, 0, 0, 0, 0, 0
				b.StartTimer()
				for step := 1; step <= 60; step++ {
					now = now.Add(SnapshotInterval)
					body = bodies[step]
					start := time.Now()
					if workload.restate {
						p.restatePass(ctx)
					} else {
						p.Pass(ctx)
					}
					latencies = append(latencies, time.Since(start).Nanoseconds())
					if p.unsent != 0 {
						b.Fatalf("pass %d refused %d publications", step, p.unsent)
					}
				}
				b.StopTimer()
				total.reads += m.reads
				total.frames += m.frames
				total.plain += m.plain
				total.sealed += m.sealed
				total.wire += m.wire
				total.acks += m.acks
				total.relayNS += m.relayNS
				total.drainNS += m.drainNS
				total.ackNS += m.ackNS
			}
			sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
			passes := float64(b.N * 60)
			b.ReportMetric(float64(latencies[len(latencies)/2]), "p50-ns/pass")
			b.ReportMetric(float64(latencies[(len(latencies)-1)*95/100]), "p95-ns/pass")
			for _, metric := range []struct {
				name  string
				value int64
			}{
				{"reads/pass", total.reads}, {"seal/pass", total.frames}, {"synthetic-acks/pass", total.acks},
				{"plain-B/pass", total.plain}, {"sealed-B/pass", total.sealed}, {"frame-B/pass", total.wire},
				{"relay-ns/pass", total.relayNS}, {"drain-ns/pass", total.drainNS}, {"ack-ns/pass", total.ackNS},
			} {
				b.ReportMetric(float64(metric.value)/passes, metric.name)
			}
			b.ReportAllocs()
		})
	}
}

// BenchmarkPublisherEncoding isolates repeatable serialization stages from
// one synthetic row. These are component costs, not additive pass accounting.
func BenchmarkPublisherEncoding(b *testing.B) {
	var root struct {
		Sessions []map[string]any `json:"sessions"`
	}
	body := scanOf(0, []workingRow{{id: "%10", state: "working", verb: "Thinking", activity: 100, tokens: 50, agents: 2}})
	if err := json.Unmarshal(body, &root); err != nil {
		b.Fatal(err)
	}
	row := root.Sessions[0]
	for _, name := range []string{"wire_body", "identity", "steady", "parse_list"} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				switch name {
				case "wire_body":
					_ = mustJSON(map[string]any{"session": row, "at": json.RawMessage(`1000`), "scan": json.RawMessage(`{"complete":true}`)})
				case "identity":
					_ = mustJSON(withoutFreshness(row))
				case "steady":
					_ = mustJSON(volatileFree(withoutFreshness(row)))
				case "parse_list":
					var value map[string]any
					if err := json.Unmarshal(body, &value); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}

// BenchmarkPublisherReceiptScan holds live queue depth at zero and varies only
// retained terminal receipts. No encryption, network or receipt arrival is
// timed: this isolates the idle flush cost of retaining correlation history.
func BenchmarkPublisherReceiptScan(b *testing.B) {
	for _, receipts := range []int{0, 67, 2048} {
		b.Run(fmt.Sprintf("receipts_%d", receipts), func(b *testing.B) {
			b.StopTimer()
			now := time.Unix(1700000000, 0)
			spool, err := adaptercloud.NewSpool(adaptercloud.DefaultSpoolLimits(), nil, func() time.Time { return now })
			if err != nil {
				b.Fatal(err)
			}
			for i := 0; i < receipts; i++ {
				seq, err := spool.Reserve(adaptercloud.SpoolChannelT, "t/mac-01/receipt-bench", "read", 2)
				if err != nil {
					b.Fatal(err)
				}
				if err := spool.Seal(seq, []byte(`{}`), now); err != nil {
					b.Fatal(err)
				}
				if d := spool.SendNext(); d.Row == nil || d.Row.Seq != seq {
					b.Fatalf("unexpected drain: %+v", d)
				}
				if _, err := spool.Settle(seq, "t/mac-01/receipt-bench", adaptercloud.SettleDelivered); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.StartTimer()
			for i := 0; i < b.N; i++ {
				if d := spool.SendNext(); !d.Idle {
					b.Fatalf("terminal receipts became live: %+v", d)
				}
			}
		})
	}
}
