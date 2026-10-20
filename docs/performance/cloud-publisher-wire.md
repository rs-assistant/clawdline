# Publisher to encrypted WebSocket baseline

This record helps maintainers distinguish production Publisher cost from the earlier [transport-only experiment](cloud-wire-baseline.md). It adds a missing measurement, not an optimization. There is no defensible before/after speedup or production capacity verdict.

## Subject and reproducibility

Production `Publisher.Pass` generates each actual Outbound, which goes through `Relay.PublishTracked`, Spool, production Transport/drain and a real masked local WebSocket. One or four independent Go WebSocket readers decode, verify signatures, decrypt and apply separate replay windows. Each compares the full plaintext, channel and sequence with the actual generated publication. Production ACK handling feeds Publisher.Acked; every accepted sequence must settle as delivered and reach every viewer before the next Pass.

The inventory and relay fanout are fixtures. This does not measure terminal enumeration, Publisher.Run scheduling, a complete daemon, browser rendering, production Cloud policy, TLS or WAN. All CPU is combined-process SELF user plus system CPU, including Publisher, transport, relay, viewers, generator and polling. Machine-only CPU remains missing; do not subtract the prior isolated synthetic-worker CPU.

Source commit `e668206a245bbe15be76acc8890f971467bbcdbf`, source SHA-256 `d3ebbdc1d88c0c57c278bf51f2011501ed5284ca2194ae7e229b178fc303b0a5`; measured base `66955d3299dd8a97217b18a8ae26488a5f24e378`. Go 1.27.1 darwin/arm64, scheduler not pinned. Three fresh invocations used one binary, SHA-256 `079943612998c4bea0dbdcbd90f68c59df7a28dbbd5cc24e46837f3875c1396f`. [Raw records and summary](cloud-publisher-wire.json) retain all 15 final scenario records. Earlier diagnostic groups are excluded.

To repeat, wrap a task-owned shell script in `tools/heavy.sh`: set a fresh task-owned GOCACHE and empty CLAWDLINE_NEXT_DIR, build `go test -c -o <owned-output>/cloud.test ./internal/transport/cloud`, then run that same binary three times from `internal/transport/cloud`, each with `CLAWDLINE_PUBLISHER_WIRE_BENCH=1 -test.run='^TestPublisherWireBaseline$' -test.v -test.timeout=90s`. Preserve stdout and binary/source hashes; remove only that task's cache, binary and empty state after every process exits. CPU timing is explicitly skipped on unsupported platforms. Normal tests skip this opt-in workload.

## Workload and measurements

Each scenario has one initialization Pass, three unchanged idle Passes and a 200 ms observation, then four activity Passes except the idle arm. Activity toggles one Session working/waiting and advances its fixture observed_at; four-Session arms rotate through Sessions. An injected clock advances five seconds per activity Pass, without waiting five real seconds. Idle must generate zero additional publications.

| Scenario | Initial publications | Activity publications | Activity completion ms median (min–max), n=12 | Combined activity CPU ms median (min–max), n=3 |
| --- | ---: | ---: | ---: | ---: |
| idle | 5 | 0 | No activity | Empty-window overhead only |
| active_one | 5 | 12 | 100.059 (88.874–109.608) | 7.997 (7.567–11.366) |
| active_multi_session | 11 | 12 | 99.679 (72.308–127.054) | 6.727 (4.813–7.265) |
| independent_viewers | 11 | 12 | 99.772 (98.947–100.128) | 12.229 (10.893–15.713) |
| reconnect_unacked | 5 | 12 | 100.535 (97.048–351.130) | 5.771 (4.496–10.795) |

Pass completion includes synchronous Pass, settlement and every reader's completed delivery; it is not per-envelope latency. CPU covers the full four-Pass activity window, not one publication. Initial publication completion takes about 103 ms for one Session and 202–204 ms for four. The roughly 100 ms activity cadence is consistent with the existing drain ticker, but this experiment does not establish a production bottleneck. Three rounds do not establish a credible p99.

Counts per round: idle 5 accepted/settled publications, active_one 17, active_multi_session 23, independent_viewers 23, reconnect_unacked 17. Four viewers independently receive 23 each, not 23 in total. Every unchanged idle phase emits zero. The controlled reconnect is armed after idle: exactly two machine connections, one byte-identical sealed envelope resend and one replay-suppressed duplicate per viewer are required; all unique deliveries remain intact. The maximum observed activity completion is 351.130 ms in that arm.

Bytes in JSON include initialization and activity. HTTP counts actual upgrade headers, excluding token HTTP. Publish WS includes masked framing; viewer/ACK WS includes unmasked framing. Envelope/ciphertext/WS include resends; plaintext counts unique publications. Viewer bytes count fixture writes, while exact decrypted delivery is separate receipt evidence. These overlapping categories exclude control frames, TCP/IP, TLS and kernel retransmits; never sum them as billed traffic.

## Validation and boundaries

The performance role produced the baseline and retained commands, environment, hashes and final logs. An independent reality-checker reviewed the exact clean source and independently recomputed all 15 raw records, compared them with the logs and every summary count/distribution/byte field, and confirmed an altered ACK count is rejected. Verdict: safe to land, no proven blocker. This was source/evidence and arithmetic review, not an independent Go or race-detector rerun.

Three explicit final experiment runs and the selected local commit gate passed. Cleanup markers plus final test PASS verify transport stop, viewer reader joins, HTTP server closure and zero handlers. Viewer read errors after the final required delivery are not a separately tested connection-lifetime failure. Broad CI remains an integration responsibility. No product parser, protocol, capacity, error code, deployment or live pairing changed. No external product API calls were made.

Remaining baseline gaps include Publisher machine/generator CPU separation, real inventory and independent browser workloads, terminal direct/fallback paths and application-level freshness/revocation controls. Prior HTML live acceptance proves its own scoped document journey, not this workload's throughput or account-wide revocation. Capacity saturation belongs to its separate plan; this result cannot establish thousand-user support.

[Publisher process CPU measurements](cloud-publisher-cpu.md) now separate Publisher+transport, relay, viewer and load-generator costs. They retain synthetic inventory and sequential-window boundaries; they are not a before/after optimization of this combined-process run.
