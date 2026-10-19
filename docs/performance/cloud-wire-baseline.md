# Local encrypted WebSocket transport baseline

Verdict: three local rounds completed for the largest connected subpath. This is a baseline, with no optimization, before/after comparison or production capacity verdict. Original requirements remain partially unmeasured: **load-generator CPU is not separated**, production Publisher and browser receiver are not connected, and real Cloud roster revocation is absent. Root delivery must preserve these gaps.

## Path and fixture provenance

Measured: production `Relay.PublishTracked` -> production Spool -> production adapter Transport/drain -> production masked WebSocket client -> local fixture relay -> one/four independent production WebSocket clients -> production DecodeEnvelope, Envelope.Open (Ed25519/AES-GCM), ReplayWindow -> decoded plaintext completion. The fixture ack returns over the real socket and settles the production spool. Completion waits for settlement and every reader's decoded operation. Relay acceptance does not mean a real application operation ran or a page rendered.

Sources actually read and adopted:

- `internal/adapters/cloud/fakerelay_test.go`: real RFC 6455 upgrade, challenge/hello/ready and ack shape. Package-private helper cannot be imported, so its minimal server half is recreated in the claimed file with fanout and lost-ack injection.
- `internal/adapters/cloud/transport_test.go:newTestLineConfigured`: local token endpoint and Transport/Spool configuration, synthetic account/identity. All keys are freshly generated; token and credential are fixture literals.
- `internal/transport/cloud/publish_efficiency_bench_test.go:efficiencyMeter` and `direct_test.go:newDirectFixture`: production Relay/Spool encryption setup. The previous meter only constructs frames and synthesizes settlement; no previous numbers are reused.
- `internal/domain/cloud/envelope.go:Seal`, DecodeEnvelope and Envelope.Open, and `replay.go:ReplayWindow`: production crypto, codec and replay. This does not time recorded protocol vectors.
- `docs/performance/cloud-read-cpu.md`: the previous report excludes encrypted relay/WebSocket costs. No Node/router CPU numbers from it are mixed into this result.

## Reproduce and environment

Base `52b665fba2fbe828f76332356c128291d176cbc2`, Go 1.27.1 darwin/arm64. `environment.txt` records actual version, architecture, source/binary hashes. One binary is reused across the three final rounds. Each scenario has new keys, spool, TCP sockets and replay windows. No warm-up beyond the initialized 200 ms idle interval. One approximately 1.1 KiB operation is queued, acknowledged and received by all readers before the next. Multi-session means channel/session IDs across 16 synthetic IDs, not 16 terminal processes. Four readers each decode/verify/decrypt independently, but share one fanned-out publication: this does **not** represent four independent routed read requests.

Run from the repository root:

```sh
bash <task-artifacts>/rerun.sh "$PWD" <task-artifacts>
```

Here `<task>` is `<task>`. The script compiles through tools/heavy.sh, then runs the same binary three times through tools/heavy.sh, setting CLAWDLINE_WIRE_BENCH=1, empty task-owned CLAWDLINE_NEXT_DIR, `-test.run='^TestWireEfficiency(Baseline|AdmissionControls)$' -test.v -test.count=1`. Six JSON rows are extracted per round. Loopback dynamic ports only; no port 7727 listener, formal daemon, real credentials, browser, external service or paid API. Heavy acquired its slot but reported the sandbox denied its ps memory check and continued. Hardware load was not pinned.

## Three-round distributions

Nearest-rank percentiles ceil(p*N)-1. Completion and stage percentiles pool all operation samples over three rounds; CPU p95 is the maximum of three scenario CPU samples, not a production tail estimate. The zero-operation idle arm's zero load-window entries are not an idle CPU estimate; idle interval CPU is separate below.

| Scenario | Sessions / viewers / operations per round | Completion p50 / p95 / max ms | Reserve+seal+queue p50 / p95 us | Decode+verify+open p50 / p95 us | Whole-process load CPU median / p95 ms |
|---|---:|---:|---:|---:|---:|
| initial_idle | 1 / 1 / 0 | 0.000 / 0.000 / 0.000 | 0.0 / 0.0 | 0.0 / 0.0 | 0.006 / 0.007 |
| active_one | 1 / 1 / 12 | 99.916 / 102.790 / 104.854 | 128.1 / 597.9 | 188.5 / 1097.3 | 12.497 / 20.865 |
| active_multi_session | 16 / 1 / 32 | 100.076 / 102.310 / 105.907 | 125.2 / 779.8 | 191.0 / 473.1 | 32.456 / 40.479 |
| independent_viewers | 4 / 4 / 16 | 99.957 / 101.099 / 104.837 | 126.8 / 245.2 | 182.7 / 393.0 | 26.975 / 29.342 |
| reconnect_unacked | 1 / 1 / 12 | 99.957 / 353.304 / 353.982 | 121.2 / 848.2 | 196.0 / 355.8 | 11.881 / 11.923 |
| ack_delay | 1 / 1 / 12 | 99.940 / 126.472 / 126.889 | 142.8 / 449.4 | 183.9 / 299.0 | 14.219 / 14.462 |

Reserve+seal+queue times Relay.PublishTracked, not isolated AES-GCM. Decode+verify+open excludes replay and payload checks and includes decrypt attempts on retransmission. CPU is actual getrusage(RUSAGE_SELF) user+system delta for Transport, fixture relay, all Go readers, load generation/polling and runtime/GC. Compiler, shell and heavy wrapper are excluded. It is **not daemon-only CPU**. Initialization/token fetch is outside the load interval. InitNS starts after viewer sockets are established and covers the machine token fetch, HTTP upgrade and challenge handshake.

- initial_idle: machine init median 1.381 ms; initialized 200 ms idle CPU median 0.230 ms.
- active_one: machine init median 1.071 ms; initialized 200 ms idle CPU median 0.189 ms.
- active_multi_session: machine init median 0.789 ms; initialized 200 ms idle CPU median 0.140 ms.
- independent_viewers: machine init median 0.970 ms; initialized 200 ms idle CPU median 0.164 ms.
- reconnect_unacked: machine init median 1.302 ms; initialized 200 ms idle CPU median 0.247 ms.
- ack_delay: machine init median 0.958 ms; initialized 200 ms idle CPU median 0.227 ms.

First-operation reconnect samples in ms: [353.981833, 253.43475, 353.303625]. These one-per-round outliers can disappear from pooled p95, so max and explicit samples are retained.

## Byte and receipt accounting

Round 1 below; every round is preserved as raw JSON. Plaintext counts unique operations. Ciphertext/envelope/publish/viewer bytes include retries. Ciphertext is decoded AES-GCM bytes including tag; envelope includes base64, signature/nonce and JSON. Publish WS bytes include masked client headers; viewer and ack bytes include unmasked framing. HTTP bytes count actual upgrade request/response header streams including viewer and reconnect, using a NetDial counting wrapper. They exclude token HTTP, TCP/IP, TLS, challenge/hello/ready/control and close frames. These are separate accounting categories, **not a total billed network byte count**. Fixture writes count sent frames; only plaintext completion establishes receipt by the Go viewer.

| Scenario | HTTP upgrade B | Publish WS B | Viewer WS B | Unique plain / sent ciphertext / sent envelope B | Ack WS B | Acks / exact-byte retries / unique viewer deliveries |
|---|---:|---:|---:|---:|---:|---:|
| initial_idle | 648 | 0 | 0 | 0 / 0 / 0 | 0 | 0 / 0 / 0 |
| active_one | 648 | 21326 | 21290 | 13288 / 13480 / 20870 | 938 | 12 / 0 / 12 |
| active_multi_session | 648 | 56886 | 56790 | 35468 / 35980 / 55670 | 2518 | 32 / 0 / 32 |
| independent_viewers | 1563 | 28438 | 113560 | 17724 / 17980 / 27830 | 1254 | 16 / 0 / 64 |
| reconnect_unacked | 991 | 23103 | 23064 | 13288 / 14603 / 22609 | 938 | 12 / 1 / 12 |
| ack_delay | 648 | 21326 | 21290 | 13288 / 13480 / 20870 | 938 | 12 / 0 / 12 |

The lost-ack fixture fans the first envelope out then closes before ack; production reconnect resends the exact sealed bytes. Exactly two machine connections and one byte-identical retransmission are required. Independent replay windows suppress duplicate application delivery. The 25 ms ack-delay arm requires no retries. This is one controlled disconnect per round, not an outage distribution or retry-storm test.

## Constraint and possible next experiment

The roughly 100 ms serial completion cadence is consistent with Transport.drain's existing 100 ms ticker (`internal/adapters/cloud/transport.go:911-928`); measured sealing/decoding is much shorter. The 25 ms ack wait sits within that cadence: the next publication waits for a subsequent tick, so adding 25 ms does not imply every steady-state operation takes 125 ms. This is a local pacing candidate, not evidence of CPU saturation or a production binding constraint. There is no actual arrival-rate, queue utilization, RTT or user budget study here. No production logic changed.

A separately authorized event-driven drain experiment could remove up to one tick of isolated-request queue wait; that is a hypothesis, not a measured improvement. Cost: concurrency and wakeup complexity, with enqueue/reconnect/stop ordering and failure tests. This baseline alone does not justify deploying it.

## Behavior controls and remaining gaps

- Every Go viewer verifies increasing exact operation IDs, freshness=current and matching observed_at metadata after authentication/decryption; this proves synthetic payload preservation/order, **not stale TTL rejection or real terminal freshness**.
- Three rounds require exact deliveries (operations*viewers), no unexpected retries and reconnect's exact two connections/one repeated ciphertext. Restored socket delivery is measured.
- Separate `TestWireEfficiencyAdmissionControls` uses production Transport.AcceptDirect: first admitted, repeated envelope refused, pinned-key resolver revocation refuses a new request, restoration admits that still-unspent request. It passes all three final rounds. This is receiver admission at a different layer from raw Go WS readers; real Cloud roster propagation, pairing revocation and browser denial remain absent.
- CPU separation needs isolated daemon/relay/viewer/load-generator processes and per-process user/system samples. Current whole-process CPU must not be added to the existing router report or labeled encryption CPU.
- Production Publisher.Pass, application Service/router/parser, real terminal inventory, WebRTC direct carrier, hosted console/browser state/rendering, roster refresh, TLS/WAN/mobile, handshake disconnects, ack timeout/burn exhaustion, disk-fence persistence, independent request amplification and concurrency saturation remain unmeasured. This cannot establish thousand-reader capacity.

## Verification and handoff

Three rounds each passed six measured scenarios and the separate admission controls: 252 unique operations, 396 unique viewer deliveries. round-1.log through round-3.log retain outcomes and raw per-operation observations; round-*.json and summary.json retain counts and distributions.

Selected gate passed:

```sh
GOCACHE=<task>/work/go-cache HEAVY_HANDOFF=0 tools/check.sh
```

Paired Go tests, formatting, repository/capacity guards, changed-file privacy/machine guards and console test registration/mirror checks passed. Timing is opt-in and skips in ordinary gate; admission controls run there. Full suite, cross-platform builds, hosted-console/deployment and dev installation remain root/CI responsibilities if required.

Only the declared new test file is changed. Root must commit: CHILD.md explicitly forbids linked Git metadata writes in this sandbox. No push, branch operation, production change, Board mutation or original report edit was performed. Worktrees removed: 0; this active task checkout remains for root integration. Fixed but not yet released (awaiting review): test-only baseline, with the measurement gaps listed above.

## Root portability correction

The root moved getrusage into Unix-specific test support and made unsupported platforms skip only the opt-in timing path. Sampling errors now fail explicitly. The original three-round record predates this test-only portability change; a separate root rerun and gate verify the corrected source. No parser or production logic changed. The combined process CPU remains a measurement gap, not daemon-only CPU.
