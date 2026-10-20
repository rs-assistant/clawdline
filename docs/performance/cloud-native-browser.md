# Native-browser Cloud client baseline

Three native Chrome rounds exercise unchanged production browser clients against
an isolated loopback fixture. This fills a measurement gap; it is not a product
optimization, before/after speedup, or production capacity result. Source is
`4f8d11c19` on base `49b4422be83b2f2a42f288e11bd26c5c9f753337`.
Only test/tool files change. No parser, product protocol, error codes, capacity
limits, account pairings or production deployment change.

## Reproduction

Use a disposable checkout of the measured source and a task-owned output directory:

```sh
tools/heavy.sh node web/console/tools/cloud-browser-baseline.mjs build "$BUILD_ARTIFACTS"
CLAWDLINE_BROWSER_RESULTS="$RAW_RESULTS" HEAVY_HANDOFF=0 tools/heavy.sh node web/console/tools/cloud-browser-baseline.mjs start "$BUILD_ARTIFACTS"
```

Open the printed loopback URL in a real browser. Click **Start round** once per
round and observe **DONE / PASS** three times. The fixture persists each round
before the next begins. In another shell, run:

```sh
node web/console/tools/cloud-browser-baseline.mjs summarize "$RAW_RESULTS" "$BUILD_ARTIFACTS"
curl --fail-with-body -X POST -H "Origin: $FIXTURE_ORIGIN" "$FIXTURE_ORIGIN/stop"
```

Set `FIXTURE_ORIGIN` to the printed origin. Wait for the owned starter to exit 0,
verify its listener is gone, and preserve `server-final.json`. Do not use the
production daemon, a real account or external STUN/TURN. Build cache cleanup is
in the builder's finally block. Each measured run uses one binary and bundle;
source hashes, exact binary/bundle hashes and the six-file source aggregate must
match before a summary is accepted. This is not a reproducible build attestation
or a manifest of every compiler/dependency input.

The performance persona is the pinned `internal/domain/persona/catalog/performance.md`
listed in the source manifest. An independent code reviewer checks source and
recomputes the recorded metrics; root operates the actual browser. No usage-priced
external API calls were made.

## Workload and measurement boundaries

Each round has 31 scenarios. Separate native iframe windows at 1, 2 and 4 viewers
use the production `CloudClient`, native WebSocket and nonextractable WebCrypto
signing keys. Ten scenarios at each size cover idle, one-session reads, two-session
reads, independent targeted publications, replay rejection, receiver pin removal
and restoration, fixture sender admission refusal and restoration, and same-client
socket restart. Negative controls at each size target one viewer; their row's
viewer count is therefore one. Multi-viewer is not broadcast or official account
roster behavior. Idle observes 1000 ms followed by a native ping/pong barrier;
its protocol frames are counted and are not claimed to be zero traffic.

The final scenario uses one production `CloudTerminalSession` and
`TerminalChannelTransport`, native `RTCPeerConnection`, production Go Link/Pion
and synthetic terminal permission/inventory. It verifies the direct canary,
closes only the owned server peer, then observes the relay canary after the
production browser's automatic fallback. The same client/session persists.
Two canaries are two acceptance checkpoints; the observed frame count includes
intermediate frames. Its duration covers the whole scenario, not pure outage time.

CPU is SELF user plus system for the **combined Go fixture** (synthetic machine,
crypto, relay, HTTP, Pion and runtime). It excludes browser CPU, which remains
unmeasured. It cannot be subtracted from isolated machine/relay/viewer CPU in
other baselines. `performance.memory` is implementation-specific and may aggregate
iframe heaps; it is not per-viewer RSS. JSON byte counts exclude WebSocket framing,
TCP/IP, TLS and billing boundaries. Cumulative sparse counters omit keys before
first occurrence; absent-before is zero, while disappearing or regressing counters
are rejected. Scenario and round CPU/counter samples must remain monotonic.
Three rounds share one fixture process; they are not isolated process trials.

## Results

All three rounds passed: 93 scenario rows, 84 logical reads, 144 delivered
observations and 27 expected rejection observations. Per round, 28 reads and
48 delivered observations are checked against raw proofs and counters. All
31 groups' median/range and counter deltas are in the
[normalized evidence](cloud-native-browser.json). Context labels and user-agent
metadata are normalized for public delivery; numeric samples are unchanged.

| Scenario | Wall median ms | Three-round range ms | Combined fixture CPU median ms |
| --- | ---: | ---: | ---: |
| active-one-1 | 7.100 | 1.600–11.700 | 1.117 |
| active-one-2 | 1.600 | 1.500–1.900 | 1.004 |
| active-one-4 | 3.000 | 1.800–3.200 | 1.652 |
| multi-session-4 | 2.300 | 1.600–6.800 | 2.589 |
| socket-reconnect-4 | 40.500 | 39.700–48.900 | 4.580 |
| native-direct-auto-fallback | 143.500 | 141.700–163.600 | 9.264 |

These are small local samples on a shared host. No p95/p99, browser CPU,
Internet/NAT/TLS cost, official Cloud revocation propagation or saturation estimate
is inferred. Separate UI-driven rounds are observations of the production client
against synthetic service boundaries, not a full React application acceptance run.

## Evidence admission and compatibility

The first independent review found five accepted malformed inputs in the original
summary command. Original three-round metrics recomputed correctly, but that
command was not safe to land. The correction now requires exact numbered rounds,
unique complete scenarios, correct viewers/reads/deliveries/errors and proof
cardinality, required positive sequence/content/session proofs, native fallback
checkpoints, matching nonempty build identities, finite nonnegative samples and
monotonic CPU/counters across scenarios and rounds. Missing data is refused.

Root ran the corrected source through another three native rounds. Authentic
records were accepted; fourteen independent copied-record mutations were refused:
missing scenario, wrong delivery count, missing native RTC, wrong read count,
negative CPU, duplicate round, missing sequence, missing server identity, missing
read counter, invalid duration, wrong viewer count, wrong canary, regressed counter,
and wrong artifact hash. Originals were not modified. Go peer/Pion smoke and HTTP
startup smoke are separate evidence and are not described as native-browser runs.
The scoped local commit gate passed on the corrected source.

Freshness/order/authority controls require exact decoded canaries and Sessions,
advancing sequences, replay refusal, no accepted read after local pin removal,
positive restoration and same-client reconnect. The fixture admission gate is a
separate synthetic control, not real Cloud account revocation. These semantic
controls are fixed prerequisites for future optimization. Existing measured timing
ranges are comparison evidence, not new product timeouts or a guaranteed SLO.
Future changes must rerun matched workloads at least three times, separate CPU
subjects, and establish any latency tolerance before implementing the candidate.
The identified parser candidate remains paused; this work does not implement it.

Normal shutdown returned 200, the starter exited 0, and the listener was absent.
Owned peers/sockets are closed and joined. Frames close clients but registrations
remain until process exit. Forced parent SIGKILL cleanup was not injected.
Cloud production identity/revocation propagation, WAN/TLS packet totals, actual
phone Safari, full tmux server CPU and saturation remain explicitly unmeasured;
they are not fabricated as completed cells. The separate capacity plan remains
unclaimed. This closes the native-browser gap within the existing local baseline.
