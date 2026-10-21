# Isolated cloud wire CPU experiment

Verdict: process attribution achieved in three fresh runs of all six scenarios. This is a measurement harness change, not a production optimization. No CPU budget was supplied, so budget acceptance cannot be decided.

## Evidence groups

1. Earlier root evidence (026c0fe2f / callback a75fa88f / CI 39303405900) is briefing context only, not independently verified or reused as new measurements here.
2. Fresh original combined-process baseline: `combined-{1,2,3}.log`. Transport + relay fixture + viewer + generator share one process, unchanged baseline scenario code. This is not machine-only CPU.
3. Fresh isolated-process experiment: `isolated-{1,2,3}.log`. Parent/generator, machine, fixture relay and viewer group have four distinct PIDs per scenario. Each arm used the same compiled binary and the original six workloads. Do not subtract group 3 machine CPU from group 2 combined CPU and call it an improvement.

## Reproduction and environment

Run `HEAVY_HANDOFF=0 tools/heavy.sh bash artifacts/run.sh` using this task's retained script (its absolute task/worktree paths must exist). `run.sh` builds once with `go test -c`, records the binary SHA-256, executes three separate processes per arm, and runs `tools/check.sh --callers` within the same heavy lease. `analyze.py` parses every raw record, checks distinct PIDs/completion, and emits `raw-results.json` and `summary.json`. Run `python3 artifacts/analyze.py` to reproduce aggregation. All load and compilation ran through one heavy lease.

Go 1.27.1, darwin/arm64; base commit 026c0fe2f83125ec4a7561d4cedb1a39d6f5a65e. Exact measured source is retained in `source.patch` with `source.sha256`. The compiled binary's SHA-256 is in `binary.sha256`, matching all three `round-*.sha256` records. `environment.txt` retains the actual local environment. The binary and task-created Go cache were deleted after exit.

The six workload shapes remain (sessions, viewers, operations): initial_idle (1,1,0); active_one (1,1,12); active_multi_session (16,1,32); independent_viewers (4,4,16); reconnect_unacked (1,1,12); ack_delay (1,1,12). Payload generation exactly matches the baseline: 1024 x characters plus operation/session/current-source JSON. Production 100 ms spool pacing is unchanged; delayed ack is 25 ms; idle warm-up is 200 ms. Each operation waits for its exact settlement sequence and all viewers before the next operation. Keys are fixed synthetic test-only seeds; no real keys, services, daemon, browser or potentially paid APIs are used. Estimated/actual external API requests: zero; cost zero.

## Subjects and lifecycle

`wire_efficiency_bench_test.go:583` starts the same test binary with only `TestWireEfficiencyWorker`, using an explicit worker-role environment variable. JSON stdin/stdout pipes carry control and generated payloads, outside measured WS byte counters. Workers use loopback ephemeral HTTP/WS ports. Every scenario starts relay, viewer, then machine. All viewer sockets are retained immediately after successful dial, so partial initialization closes already-open sockets. Machine cancels its own transport and waits for Run; viewer closes its own sockets and waits for reader goroutines; relay closes its fixture server; parent closes each worker pipe and waits for that exact PID. Cleanup has a bounded kill fallback for its own PID only. All 18 isolated scenarios completed without cleanup errors. Parent/worker PID identities are preserved in every raw result; no worker is intentionally retained.

`wire_efficiency_bench_test.go:638` samples RUSAGE_SELF user/system CPU inside each worker. `wire_efficiency_cpu_unix_test.go:15` owns Unix syscall access; syscall failures fail the test. Unsupported platforms explicitly skip timing before launching workers. `Init`, `Idle`, `Active` maps contain user CPU ns, system CPU ns, wall ns separately for machine, relay, viewer and parent_generator. Machine includes production Relay/Spool/Transport, crypto and pipe command handling, but no payload creation, relay server fanout or viewer verification/decryption. Parent samples SELF, never CHILDREN. The viewer process hosts all configured independent readers, each with its own replay window and expected operation index.

Init starts at test entry, before synthetic keys/worker initialization, and runs to the first phase mark. It excludes Go runtime/OS process startup before test entry. Parent Init includes worker launch/control CPU; child startup CPU before test entry and cleanup CPU after Active are not captured. Idle/Active boundaries are sequential pipe marks, so each worker's actual wall window is separately reported rather than pretending phase clocks are simultaneous. CPU includes that process's test-control overhead; no overhead subtraction is attempted. Active wall includes serial settlement and viewer completion. Parent wall includes its own phase-mark round trips. An empty scenario's small Active values represent coordination overhead, not transport throughput.

## Active distributions

Each cell is median / nearest-rank p95 over three runs, in ms. With n=3, p95 is the maximum, not a robust tail estimate. Complete separate user/system, Init/Idle/Active and latency distributions are in `summary.json`; each original sample is in `raw-results.json`.

| Scenario | Machine CPU | Fixture relay CPU | Viewer CPU | Parent/generator CPU | Machine active wall |
|---|---:|---:|---:|---:|---:|
| ack_delay | 9.803 / 10.837 | 5.154 / 5.701 | 9.563 / 10.366 | 2.541 / 2.584 | 1226.154 / 1226.746 |
| active_multi_session | 26.111 / 27.387 | 8.511 / 11.503 | 27.123 / 28.323 | 11.234 / 13.709 | 3201.149 / 3201.158 |
| active_one | 9.859 / 13.601 | 3.340 / 6.251 | 9.271 / 10.065 | 2.535 / 2.779 | 1201.129 / 1201.713 |
| independent_viewers | 13.458 / 13.590 | 5.099 / 5.320 | 25.605 / 28.129 | 3.594 / 3.610 | 1601.060 / 1601.943 |
| initial_idle | 0.081 / 0.094 | 0.069 / 0.103 | 0.043 / 0.132 | 0.150 / 0.189 | 0.192 / 0.542 |
| reconnect_unacked | 11.567 / 11.730 | 3.721 / 5.063 | 9.956 / 12.363 | 2.553 / 3.687 | 1453.279 / 1456.368 |

Separate user/system CPU distributions for the same Active windows, ms:

| Scenario | Process | User median / p95 | System median / p95 |
|---|---|---:|---:|
| ack_delay | machine | 5.761 / 5.832 | 4.042 / 5.005 |
| ack_delay | relay | 2.218 / 2.270 | 2.936 / 3.431 |
| ack_delay | viewer | 4.492 / 4.571 | 4.992 / 5.874 |
| ack_delay | parent_generator | 1.232 / 1.268 | 1.273 / 1.352 |
| active_multi_session | machine | 11.930 / 12.131 | 14.181 / 15.256 |
| active_multi_session | relay | 4.108 / 6.127 | 4.403 / 5.376 |
| active_multi_session | viewer | 12.488 / 13.120 | 14.635 / 15.203 |
| active_multi_session | parent_generator | 5.762 / 5.914 | 6.049 / 7.795 |
| active_one | machine | 5.727 / 5.837 | 4.132 / 7.764 |
| active_one | relay | 1.574 / 3.692 | 1.769 / 2.559 |
| active_one | viewer | 4.705 / 4.787 | 4.484 / 5.360 |
| active_one | parent_generator | 1.350 / 1.434 | 1.185 / 1.345 |
| independent_viewers | machine | 6.739 / 6.767 | 6.691 / 6.851 |
| independent_viewers | relay | 2.384 / 2.395 | 2.727 / 2.925 |
| independent_viewers | viewer | 15.337 / 15.743 | 9.862 / 12.792 |
| independent_viewers | parent_generator | 1.745 / 1.764 | 1.830 / 1.907 |
| initial_idle | machine | 0.025 / 0.029 | 0.056 / 0.065 |
| initial_idle | relay | 0.020 / 0.035 | 0.049 / 0.068 |
| initial_idle | viewer | 0.016 / 0.043 | 0.027 / 0.089 |
| initial_idle | parent_generator | 0.060 / 0.079 | 0.090 / 0.110 |
| reconnect_unacked | machine | 4.196 / 5.921 | 5.646 / 7.534 |
| reconnect_unacked | relay | 1.836 / 2.033 | 1.985 / 3.030 |
| reconnect_unacked | viewer | 4.757 / 5.188 | 5.199 / 7.175 |
| reconnect_unacked | parent_generator | 1.300 / 1.529 | 1.253 / 2.158 |

The fresh combined baseline is a separate subject, ms:

| Scenario | Entire process active CPU median / p95 | Entire active wall median / p95 |
|---|---:|---:|
| ack_delay | 18.461 / 19.824 | 1226.518 / 1227.348 |
| active_multi_session | 37.136 / 37.580 | 3201.111 / 3202.292 |
| active_one | 11.242 / 16.927 | 1201.194 / 1201.408 |
| independent_viewers | 32.326 / 33.031 | 1602.339 / 1602.393 |
| initial_idle | 0.001 / 0.010 | 0.002 / 0.015 |
| reconnect_unacked | 14.299 / 15.663 | 1455.520 / 1455.802 |

## Counters and completion

Every round retains operation, connection, ack, retry, unique delivery, HTTP upgrade, publish WS, envelope, decoded ciphertext, plaintext, ack WS and viewer WS byte counters. The following count/byte values were stable across all three isolated runs (all three original records remain available).

| Scenario | Ops / ack / retry / deliveries | HTTP upgrade | Publish WS | Envelope | Ciphertext | Plaintext | Ack WS | Viewer WS |
|---|---|---:|---:|---:|---:|---:|---:|---:|
| ack_delay | 12/12/0/12 | 648 | 21326 | 20870 | 13480 | 13288 | 938 | 21290 |
| active_multi_session | 32/32/0/32 | 648 | 56886 | 55670 | 35980 | 35468 | 2518 | 56790 |
| active_one | 12/12/0/12 | 648 | 21326 | 20870 | 13480 | 13288 | 938 | 21290 |
| independent_viewers | 16/16/0/64 | 1563 | 28438 | 27830 | 17980 | 17724 | 1254 | 113560 |
| initial_idle | 0/0/0/0 | 648 | 0 | 0 | 0 | 0 | 0 | 0 |
| reconnect_unacked | 12/12/1/12 | 991 | 23103 | 22609 | 14603 | 13288 | 938 | 23064 |

HTTPHandshakeBytes sums the actual client-side HTTP WS upgrade request/response headers from machine and viewers, including reconnects. It excludes token HTTP endpoint bytes, TCP/IP overhead and TLS (local fixture is plaintext). Publish/Ack/ViewerWS bytes retain the baseline definitions; they are application frames and exclude WS challenge/hello/ready control frames. Envelope and ciphertext totals include the retransmitted envelope; plaintext counts unique operations. Viewer bytes include duplicate wire delivery; Deliveries counts only accepted replay-window entries. These counters are not a complete NIC traffic total. Machine Seal/Open timings retain their baseline meanings. Legacy wireBenchResult CPU/wall fields inside isolated Result are unused zero-valued schema residue; only the explicit phase maps carry isolated CPU/wall measurements.

The existing relay locks ack accounting until the socket write has finished. Retry count increments only when the complete raw envelope bytes match an earlier publish. All three reconnect runs had exactly two machine connections, one byte-identical retry, twelve acks and twelve unique viewer deliveries. Each four-viewer run had sixteen acks and sixty-four independent deliveries. Viewer signature/decryption, current-source, order and replay checks run before counting delivery. The original baseline and admission tests remain intact.

## Interpretation and limits

Ordinary serial per-operation latency remains close to 100 ms, while machine active CPU is only about 10 ms over 1.2 s in active_one. This bounded serial fixture is paced by the unchanged production spool ticker (`internal/adapters/cloud/transport.go:911`), rather than demonstrating CPU saturation. Four viewers increase viewer work, which is now visible separately (25.605 ms median active CPU). The reconnect tail is about 352 ms in the median run's p95 operation latency. This does not establish a production throughput ceiling or identify a production bottleneck under concurrent load. Loopback, synthetic Go viewers, no TLS/WAN/browser, only three rounds and pipe coordination limit external validity.

No optimization is recommended or claimed from this experiment. Changing pacing would alter a deliberately preserved contract and require a separately scoped product decision. Further CPU-isolation profiling could quantify control-pipe overhead but adds harness complexity and is unnecessary to distinguish the four process subjects. More rounds and real browser/WAN work would cost additional time and belong to the remaining acceptance tracks.

## Verification and handoff

Three baseline invocations x six scenarios and three isolated invocations x six scenarios passed using one binary (36 scenario executions). `tools/check.sh --callers` passed every selected check; cross-platform build/vet and broad integration suites were explicitly skipped by the scoped gate and remain CI responsibilities. `git diff --check` passed. No Windows run/compile was newly claimed. Timed tests remain opt-in in the ordinary gate.

The measurement source was subsequently preserved by the root in commit 710c645c5. No production or parser change is included. Integration, review and Cloud delivery are tracked separately.

## Independent review and error-path correction

An independent reviewer recalculated all 36 archived records and 216 process-phase samples, checked distinct PIDs and source integrity, and found no blocking discrepancy. The review did not re-execute the Go measurements. Its stderr synchronization recommendation was addressed with a mutex-protected buffer and a malformed-worker exercise using the real os/exec stderr copier. Earlier successful measurements predate this error-path-only correction and remain separately identified. Control exchanges still rely on worker/outer test timeouts; phase windows exclude mark-response/reset gaps and must not be summed as a complete process-lifetime CPU budget.
