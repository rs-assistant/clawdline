# Publisher process CPU measurement

Verdict: the machine-only Publisher+Relay+Spool+Transport CPU measurement gap is filled for this local synthetic inventory workload. No product optimization or before/after improvement is claimed.

Base commit: `71b99666d96470e55465bbc5d84e780e20e44de9`. Only two test files change; the root preserved the measured source in commit `cf188b21cc2df5228acb1cbd1c66c5c517d753dc`. Patch SHA-256: `8b1aeee84209a2447adabe514d2f2453f3e9a1ddc03a060624c7b3776776a1e2`.
Measured binary SHA-256: `c4b88c8c38afc8638fb15c028b5451f09fcae61df4634a1e39c19d95dbd95e02`. Source and performance persona hashes, platform, Go version, runtime environment and every run command/log hash are in `the same measurement record`.

## Method and accounting

- Three fresh parent processes run the same binary; each starts three fresh workers per scenario (relay, viewer, machine). Five scenarios per round, 15 records total. The viewer worker owns 1 or 4 independent connections, replay windows and delivery streams; it is one process, not four viewer processes.
- Fresh local signing/content keys per scenario are generated in the parent and passed through test-only stdin JSON. No production Cloud, paid API, pairing, existing state, user Session or port 7727 is used by the harness.
- Machine owns production Publisher.Pass, Relay, Spool and Transport. Parent generates synthetic inventory JSON and commands, checks replies and orchestrates processes. Relay serves the reused localhost WebSocket fixture. Viewer verifies signatures, decrypts and compares every unique plaintext/channel/sequence in order, including source metadata. CPU is RUSAGE_SELF user/system per process, not RUSAGE_CHILDREN or a subtraction of combined totals.
- Initialization includes transport setup, one real Publisher.Pass, delivered settlements and viewer validation. It produces 5 publications for 1 Session or 11 for 4 Sessions. Idle contains three unchanged real Pass calls and 200ms wall wait, with zero new publications. Activity has four Pass calls (zero for idle scenario); each changes one Session state/activity/observed_at and produces exactly 3 publications. Only the changed row clock advances; fixture time advances by SnapshotInterval. This is not daemon cadence or full terminal enumeration.
- Every Pass checks generated=accepted=settled, the exact settled sequence list and each independent viewer before the next Pass. Results preserve cumulative pass counts, sequence lists and base64 plaintext. ACK-drop is enabled only after idle; exactly one retry must equal an earlier encrypted envelope, with two connections and one duplicate per viewer. All three rounds satisfy these assertions.
- Each role records StartUnixNS/EndUnixNS, monotonic WallNS, UserNS and SystemNS for each phase. Workers start their initialization windows after test startup; parent starts before spawning. Marks are sequential, not synchronized barriers. Mark response encoding/reset gaps and process startup/shutdown are outside worker phase windows. Windows therefore differ slightly and must not be added as a precise whole-lifecycle total.
- Worker IPC processing, memory allocation, Go runtime/background work and receipt bookkeeping stay charged to their process. This is not pure crypto CPU. Viewer timing includes exact-plaintext validation IPC. Parent CPU includes fixture generation, process startup/control and validation, not child CPU.
- CPU ms below is (user+system)/1e6, per complete phase; wall ms is that same role phase elapsed time. [the committed measurement record](cloud-publisher-cpu.json) separately preserves user/system, wall and 100*(user+system)/wall as one-core CPU percent. No per-publication amortization is implied. n=3 supports median and observed range only, not a reliable tail percentile, thousand-viewer capacity or an optimization percentage.

## Machine process results

| Scenario | Phase | CPU ms median [min,max] | Wall ms median [min,max] |
|---|---|---:|---:|
| idle | Init | 2.604 [2.493, 5.009] | 104.596 [104.277, 105.491] |
| idle | Idle | 2.617 [1.116, 2.926] | 202.561 [202.364, 205.156] |
| idle | Active | 0.054 [0.045, 0.057] | 0.202 [0.200, 0.213] |
| active_one | Init | 2.885 [2.712, 4.958] | 106.279 [105.073, 106.807] |
| active_one | Idle | 1.410 [1.327, 1.557] | 203.133 [203.083, 203.173] |
| active_one | Active | 7.274 [3.129, 7.275] | 395.928 [393.917, 397.811] |
| active_multi_session | Init | 4.891 [3.996, 5.605] | 205.024 [204.167, 206.291] |
| active_multi_session | Idle | 1.248 [0.963, 1.762] | 202.666 [202.562, 203.696] |
| active_multi_session | Active | 5.041 [3.481, 6.548] | 396.424 [394.816, 398.375] |
| independent_viewers | Init | 5.877 [4.395, 6.781] | 207.583 [207.149, 211.612] |
| independent_viewers | Idle | 1.523 [1.410, 4.617] | 203.740 [203.188, 204.424] |
| independent_viewers | Active | 4.421 [4.139, 5.962] | 393.754 [393.091, 394.984] |
| reconnect_unacked | Init | 2.887 [2.846, 3.026] | 105.964 [105.851, 106.598] |
| reconnect_unacked | Idle | 0.857 [0.775, 3.780] | 202.538 [202.291, 203.598] |
| reconnect_unacked | Active | 7.224 [5.047, 8.805] | 649.185 [646.892, 649.226] |

All four roles and all 60 scenario/phase/role cells are in [the committed measurement record](cloud-publisher-cpu.json); the same measurement record contains all 15 raw records. Initialization and short empty activity windows are dominated by setup/control and runtime noise. Larger idle than activity CPU in some samples is not evidence of an optimization or a production bottleneck. No CPU budget was supplied, so no pass/fail performance threshold or optimization recommendation is inferred.

## Reproduce, validation and limits

Build one test binary with `tools/heavy.sh`, using a task-owned GOCACHE: `go test -c -o <owned-output>/cloud.test ./internal/transport/cloud`. Run that same binary in three fresh parent processes from the cloud package directory, setting `CLAWDLINE_PUBLISHER_CPU=1` and passing `-test.run='^TestPublisherWireCPU$' -test.v -test.timeout=60s`. Preserve the emitted PUBLISHER_CPU_RESULT records, source/binary hashes and WIRE_WORKER_JOINED evidence. Run `TestPublisherWireCPUWorkerExit` for the clean early-exit negative control. Remove only task-owned caches/binary/state after all processes exit. No real account, browser or external service is needed.

The performance role ran the explicit same-binary positive/drop experiment and three final rounds. The final selected gate passed. Linux/amd64 and Windows/amd64 test binaries compiled; those platforms were not executed. The original isolated/Publisher compatibility run passed using an earlier binary before the final parent per-Pass assertion. Worker implementation was reported unchanged, but no retained intermediate source snapshot independently establishes that temporal diff; it is not final-binary compatibility evidence. Worker-exit and malformed-diagnostic controls passed in the recorded run; the final local gate also exercised the non-opt-in early-exit test.

An independent reality-checker verified the exact source hashes, 24 artifact hashes, all 15 raw records and three round logs. A fresh Python validator recomputed 180 role/phase samples, 60 summary cells and 1,200 metric statistic fields, and rejected separate mutated count and CPU controls. Forty-five worker join records agree with historical process-absence checks. This review was source/arithmetic/log verification, not an independent Go execution or race-detector run. The measured executable was deleted by its owner, so its recorded hash is historical evidence rather than a newly rehashed binary.

The experiment adds attribution, not a product optimization. Never subtract earlier combined-process CPU or synthetic transport-worker CPU from these new machine measurements as a speedup. Full terminal enumeration, actual browser workloads, direct/fallback application paths and account-control-plane revocation remain separate gaps. No new product protocol, error code, bound, parser behavior, deployment or live pairing changed. Full integration CI is tracked separately.

The root also rebuilt the final integrated source and passed one explicit compatibility run covering the original isolated transport, combined Publisher, new process CPU, worker-exit and malformed-diagnostic modes before the local commit gate. This run is verification, not another pooled measurement round.
