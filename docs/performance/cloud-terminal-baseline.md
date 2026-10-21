# Local terminal inventory and carrier baseline

This task adds measurement code only. It does not optimize production code or
establish a capacity limit. Three completed rounds measure the paths below; all limits remain explicit.

## Reproduction

From the task worktree, run:

```sh
HEAVY_HANDOFF=0 tools/heavy.sh bash "$TASK_ARTIFACTS/run-baselines.sh"
python3 "$TASK_ARTIFACTS/summarize.py"
```

Set `TASK_ARTIFACTS` to this task's artifacts directory. The script derives its
own task directory, sets a task-owned `GOCACHE`, empty `CLAWDLINE_NEXT_DIR`,
`GOTOOLCHAIN=local`, `GOPROXY=off`, `GOSUMDB=off`, and `LC_ALL=C`. Each package
is compiled once; the same two test executables are run three times, sequentially
on the same machine. Binary and source hashes, including the persona, are in
`environment-and-source-hashes.txt` and `binary-hashes.txt`. External paid API
requests: **0**. No Cloud probes, live pairing, browser automation, installation,
deployment, parser changes, or production load are involved.

The applied role is `internal/domain/persona/catalog/performance.md`, SHA-256
`3541ec14d21b8401c943fd48f41c948d30be221bdedced43ed4c68f08e1592df`.
The role's binding-constraint skill does not apply: there is no established slow
stage or optimization request in this task.

## Inventory method and boundaries

`Tmux.Inventory` in `internal/adapters/terminal/tmux.go` is exercised against
one private absolute socket per fixture. The absolute executable is a tiny
task-owned proxy which executes `/usr/bin/env -u TMUX -u TMUX_PANE` and the pinned
real tmux. Every command, including cleanup, supplies `-S <private.sock>`. Server
startup uses `-f /dev/null`; panes run only synthetic `sleep 300` commands.
No terminal content or host Sessions are read.

For each of 1, 4 and 16 panes, each round creates a fresh server, checks a
positive inventory, performs five unchanged reads, adds a private pane and reads
again, then removes it and reads again. Every read validates exact count, unique
IDs, CWD, and `Complete`. A stopped server and a separate absent socket must
return the existing complete-empty result; neither is counted as a successful
nonempty inventory sample. The added state has 2, 5 or 17 panes respectively.

Wall time measures the production Inventory call including executable resolution,
the env proxy, real tmux client startup, parsing and result construction. CPU
is **Go `RUSAGE_SELF` user/system only**; it excludes tmux clients and server.
No `RUSAGE_CHILDREN` estimate is substituted. tmux output bytes and observed
command count are **unmeasured**. Full tmux server/client CPU is **unmeasured**.
The env proxy overhead is not subtracted or represented as a product optimization.

Cleanup kills only the exact private socket's server, checks the captured server
PID returns `ESRCH`, checks `has-session` fails on that socket, then removes and
verifies absence of that socket directory. Per-fixture ownership and cleanup
evidence are in each raw log. An environment skip is not a measurement pass.

## Carrier method and boundaries

`Link.sendTerminalFrame` in `internal/transport/cloud/terminal.go` is used for
both carriers. The fixed screen is 24 rows × 80 columns, 24 synthetic text lines,
fixed capture time, eight sequential revision IDs, one viewer and no delta
negotiation. Each frame is received and signature-verified/decrypted with the
connection key; the entire decoded `cloudWireFrame`, terminal ID, connection,
channel and `frame_seq` are checked. Envelope sequences must advance exactly
once. An ACK must settle each frame and establish that same frame as its base
before the next send. No queued-only result is reported as delivery.

Direct uses `newDirectFixture` / `newTestBrowser`: both ends are **Go/Pion in
one process**. The receiver/offer is loopback IPv4 with mDNS disabled; the machine API allows UDP4/UDP6 including loopback. STUN/TURN are absent, with
`directLoopback=true` and `directNoSTUN=true`. `testBrowser` is a fixture name;
this is not Chrome or any real browser. A direct frame must not enter the relay
spool. Direct completion includes the viewer's data-channel ACK. Exact received
data-channel JSON and sent ACK JSON bytes are counted; SCTP/DTLS/UDP/IP bytes,
ICE negotiation byte totals, retransmissions and kernel CPU are **unmeasured**.

Relay disables TerminalDirect, uses the existing `wireBenchRelay` local HTTP /
WebSocket fixture and the production adapter Transport, token client, Spool,
Relay sealing and ACK settlement. A separate Go WebSocket connection receives
and decrypts every frame. Fixture relay `delivered` ACK means socket fanout;
it is **not a viewer application ACK**. This benchmark additionally observes
the viewer's successful full decode before counting completion. Published
frames, relay ACKs and retries are checked. Counted relay bytes distinguish
encrypted envelope JSON, publisher WebSocket frames, viewer WebSocket frames,
relay ACK frames and HTTP upgrade headers. These exclude TCP/IP packet headers,
token HTTP request/response bytes, and network-level retransmissions. Direct
JSON byte totals and relay WebSocket totals are different boundaries and must
not be subtracted as equivalent network cost.

Initialization (fixture keys, negotiation/handshake), 100 ms idle, and eight-frame
activity are separate intervals. CPU user/system is **combined Go process**:
publisher, crypto, relay fixture, Go receiver / Pion peers and test assertions.
It cannot be subtracted from the prior four-process Publisher measurements.
Wall time includes 1 ms ACK-state polling and assertion/decode work; this is a
local closed-loop test, not a throughput or production latency estimate.

The direct failure control receives an additional real encrypted frame, invokes
the existing ACK handler with a wrong sequence, and requires the existing typed
`ErrFrameDeferred` refusal. It then backdates the pending timestamp to the
existing deadline, invokes `sweepDirect`, verifies retirement and a canceled
send, and confirms a late ACK cannot reactivate it. This is controlled logical
time, not a measurement of waiting out the production deadline. A later
`direct_recovery` subtest creates entirely new synthetic Link/connection/peers
and delivers eight more frames. This establishes a fresh-connection positive
control, **not same-Link reconnect or automatic browser relay fallback**.

Production peer shutdown is followed by Pion `GracefulClose` on both captured
peer connections to join their workers; the retirement notification is observed.
Transport Run is canceled, stopped and joined; receiver sockets and hijacked
relay connections are closed; relay handlers are joined. Raw logs retain these
cleanup markers. The tiny notification goroutine has no externally exposed join
handle; observing its channel send is the fixture's completion boundary.

## Statistical scope

Three rounds are reported as median and round range, without p99 or capacity
claims. For unchanged inventory, each round's five reads are reduced to that
round's median before combining the three rounds; all samples remain in
[the committed raw record](cloud-terminal-baseline.json) and raw logs. Eight per-frame latency samples remain attached to
each activity row. The hardware is shared; scheduling and other host load are
not controlled. `ps` and hardware `sysctl` reads are denied by this sandbox,
so model, core count, memory and host utilization are not inferred. The heavy
wrapper's denied memory read is recorded in `run.log`; no policy was bypassed.

## Results

**Verdict: both requested local paths are measured, three rounds passed.**
No performance target was supplied, so whether either route meets a product
latency or CPU budget cannot be decided. No improvement claim is made.

Official measurement and scoped gate ran on 2026-10-20, finishing at
02:14:43 UTC. Base tree: `2486cc99382fbe9d024e359968cd858c5b62c8d5`.
Environment: Go 1.27.1, Darwin arm64, tmux 3.6a.

| User-visible work | Wall median ms | Round range ms | Self CPU median ms |
| --- | ---: | ---: | ---: |
| Unchanged inventory, 1 pane | 12.621 | 11.589–13.360 | 1.156 |
| Unchanged inventory, 4 panes | 11.924 | 11.676–12.000 | 0.891 |
| Unchanged inventory, 16 panes | 15.905 | 15.448–16.602 | 1.326 |
| Fresh read after adding to 1 pane | 13.614 | 11.337–14.497 | 1.176 |
| Fresh read after adding to 4 panes | 12.447 | 11.491–12.832 | 1.243 |
| Fresh read after adding to 16 panes | 16.327 | 13.258–17.717 | 1.382 |
| Direct, eight fully decoded and acknowledged frames | 12.890 | 12.141–13.823 | 9.342 |
| Relay, eight fully decoded frames plus relay settlement | 800.418 | 799.496–801.684 | 10.333 |
| Fresh direct connection, eight decoded and acknowledged frames | 12.451 | 11.915–13.832 | 7.430 |
| Direct initialization | 14.466 | 14.127–17.112 | 11.475 |
| Relay initialization | 1.320 | 1.238–1.533 | 1.654 |

Inventory CPU is Go only; carrier CPU is the entire combined test process.
CPU totals are calculated per round before taking a median (not by adding
independently calculated user/system medians). The complete initialization,
idle, activity, add/remove, positive and failure tables are in the same committed summary and
the same committed summary. Raw samples and per-frame latency arrays are in [the committed raw record](cloud-terminal-baseline.json).

The relay activity's approximately 100 ms per frame was sufficiently flat to
recheck the setup: fixture `delay` is zero, all eight relay ACKs arrive, all
eight screens decode, and retries are zero in all three rounds. Production
`internal/adapters/cloud/transport.go:911` creates a 100 ms spool writer ticker;
the loop waits for it before `SendNext` at line 924. The sequential send/ACK
workload consistently exposes that existing cadence. This source observation
and the distributions support polling wait as the main explanation in this
setup; they do not prove a fleet-wide bottleneck or an optimization result.
The polling interval was not changed and no counterfactual interval was tested.
CPU activity remains about 9–10 ms for either eight-frame route, while wall
times differ substantially; the measurement does not support interpreting the
wall difference as cryptographic CPU savings.

For each eight-frame activity interval, both carriers have exactly **17,496
plaintext bytes** and **25,832 encrypted-envelope JSON bytes**. Direct receives
26,008 data-channel JSON bytes and sends 552 ACK JSON bytes. Relay records
26,136 publisher WebSocket bytes, 26,112 viewer WebSocket bytes, 848 relay-ACK
WebSocket bytes, and 648 HTTP upgrade bytes. These counts match across all
three rounds. Relay has eight publishes, eight relay ACKs, zero retries. The
boundaries differ as described above; these are not equivalent IP wire totals.

| Coverage cell | Outcome |
| --- | --- |
| Real isolated tmux inventory at 1/4/16 panes | Measured, 3/3 rounds |
| Repeated unchanged inventory and fresh add/remove | Measured, exact IDs/CWD/Complete checked |
| Stopped/absent private socket | Existing complete-empty semantics checked |
| tmux Go self CPU | Measured; clients/server excluded |
| tmux output bytes / observed command count / server CPU | Unmeasured |
| Pion direct full decode, sequence, ACK, no relay enqueue | Measured, 3/3 rounds |
| Real local relay transport and independent Go WS receiver | Measured, 3/3 rounds |
| Wrong ACK, deferred next send, deadline retirement, late ACK refusal | Checked, 3/3 rounds |
| Fresh independent Link/connection after the failed fixture | Measured, 3/3 rounds |
| Same-Link reconnect, real browser and automatic relay fallback | Unmeasured |
| Four viewers / packet-level carrier bytes / isolated publisher CPU | Unmeasured |
| Production Cloud, WAN/NAT/STUN/TURN and load capacity | Not exercised |
| Peer, server, socket and Transport cleanup | Explicit cleanup markers in all measured logs |

The existing direct fixture exposes one registered viewer/connection and one
signaling peer. Adding four viewers sharing one Link requires additional
connection/identity/signaling orchestration; four independent Links would answer
a different question. This task therefore makes no multiviewer claim. No product
optimization is recommended from three local rounds. A follow-up for packet
totals would need a test-owned counted UDP boundary and symmetric accounting;
its cost is new fixture complexity and independent validation. Full tmux CPU
would require a per-server process accounting method, separately from Go self
CPU; its cost is platform-specific observation. Neither requires a new product
bound or protocol.

## Root integration and independent validation

Source commit `15ed701f0f71615fddcc17c86dcac14f698a0f83` preserves the measured three-file test addition. Source SHA-256 values are `c2e179ddbd181b48db1a5ef566f4f67b46e61bf62dcbe09f0f65a606712ffa9c` (Unix inventory), `e583ded8bd2221ed8453c32f80f6ce8ff44048b865f2c5895c064998d8215dbb` (other-platform skip), and `ae9c6ce18f8aa618e3724f718ef69ff3f97071eb52b5565a06bf4cc11a6c03f3` (carrier). The historical inventory binary hash is `907edd6587425fa2b7ad286b842114ca228cf2b625f99a2fc573eb1e9e06e349`; carrier binary hash is `6030d1066c2d3078568e60a658f7e9b1cbfc706b71efa3a614f1430b7d528381`. They were removed after testing, so later review cannot rehash those executables.

An independent reality-checker verified the exact clean source and derived 105 raw records and 23 summary groups from the six full experiment logs, without executing the original summarizer. Counts, bytes, per-frame latency, user/system totals and every rendered table agree. Twenty-six manifest entries match; controlled bad frame count and CPU mutations are rejected. Verdict: safe to land, no demonstrated blocking defect. The reviewer did not rerun Go or the race detector.

All six explicitly enabled experiment invocations passed without environmental skips. The scoped local gate passed; its terminal-package runtime tests ran, while the carrier package selected the opt-in baseline, which skips without the environment flag. That gate is not broad carrier runtime evidence. Full runtime and foreign-platform integration checks are tracked separately.

The inventory fixture installs its normal teardown after wrapper creation, server startup and PID capture. A failure in that earlier setup interval can retain a directory/server; successful measured fixtures all have explicit exit/absence evidence. This is an acknowledged test setup failure-path limit, not a claim that an untested failure cleans itself up. Peer-close notification has no separate join handle; observing its channel message is the declared boundary. No product, parser, capacity, protocol, error-code, pairing or deployment change is included.

To reproduce without private task artifacts, compile each package once through `tools/heavy.sh` with a task-owned GOCACHE: `go test -c -o <owned>/inventory.test ./internal/adapters/terminal` and `go test -c -o <owned>/carrier.test ./internal/transport/cloud`. Run those exact binaries three times with `CLAWDLINE_TERMINAL_BASELINE=1`, selecting `^TestTmuxInventoryBaseline$` and `^TestTerminalCarrierBaseline$` respectively, from their package directories. Use an empty task-owned CLAWDLINE_NEXT_DIR. Preserve stdout, source/binary hashes and cleanup evidence; remove only that task's cache/binaries after they exit. Private tmux servers must be stopped by their explicit socket before deleting socket directories. Other-platform skips are not completed measurements.
