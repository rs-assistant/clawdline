# Independent Cloud reader baseline

This is a local Node RelayReader/RelayWriter to real Go cloudops/router/HTTP handler baseline. Each viewer has its own client, sender, sequence and reader cache. It does not exercise actual browser contexts, CloudClient cryptography, TLS/WebSocket relay, real roster retirement or billing. No production code changes.

The harness source is dbb980aa214f459d541bb5a7274fe87fac6ccdc5, based on e19c6cb3453a4f7953b8a16f9a3f8aeb6c593891. Node 24.1.0, Go 1.27.1, Darwin arm64. Five measured rounds follow a warmup; reader freshness uses virtual time and durations use monotonic real time. The synthetic record begins with 240 turns and grows to 258; latest pages contain 200 turns, with 256-character text suffixes. Source/server fixtures and their metadata belong to this run; earlier setup runs remain separate evidence.

| Cold independent viewers | Asks / Go route entries per round | Plaintext answer payload bytes | Median caller elapsed |
| --- | ---: | ---: | ---: |
| 1 | 1 / 1 | 60935 | 5.779 ms |
| 2 | 2 / 2 | 121870 | 6.117 ms |
| 4 | 4 / 4 | 243740 | 8.523 ms |

All five rounds have identical route and byte counts within each group. Four same-reader callers cause only one ask/route and 60935 payload bytes. Warm unchanged independent readers cause zero asks/routes and bytes. This contrast identifies duplicate computation, not a measured before/after improvement. Independent callers still require separate authority checks, correlation and answers; sharing parsing would not eliminate the measured output bytes. Small timing samples do not establish production percentiles or saturation.

The [65-group record](cloud-independent-reads.json) contains 309 phase observations summarized over five rounds, with caller elapsed, Node CPU, ask, route and payload counts. It includes changed data, retained-reader versus rebuilt-reader reconnect, freshness ceiling, explicit fresh reads, distinct history cursors, focus/send and cancellation observations. Three deterministic old-response/send/provider-append/fresh-read probes and two negative identity controls also pass; they are outside the phase total. A missing-session control explicitly differs from the fixture.

Synthetic fresh revocation refuses before the router. Warm cache reuse under that synthetic toggle does not prove real revocation behavior: the real CloudClient retirement/logout lifecycle is absent. Existing latest-transcript cancellation returns cached data or completes its admitted read; this benchmark observes that behavior without changing it.

## Reproduce

Use an isolated checkout and task-owned output, working directory and Go cache. Compile through tools/heavy.sh: `go test -c -o <binary> ./internal/transport/http`. Then run Node through heavy with CLAWDLINE_READ_BENCH_OUT, CLAWDLINE_READ_BENCH_BINARY and CLAWDLINE_READ_BENCH_WORK naming those owned paths:

```sh
node --experimental-strip-types web/console/src/cloud/read-efficiency.bench.ts
```

The Node harness starts only the opt-in TestServeCloudReadEfficiency on loopback and shuts it down. The Go server uses synthetic terminals and admission, with real read handlers; it does not touch the running daemon or actual terminal. Preserve raw.json, summary.stdout.json and server output. Remove the owned binary/cache after all processes exit. The normal Go suite skips this opt-in server without its explicit ready-file environment.

## Remaining measurements

Go CPU and parser/admission utilization, real browser/phone, signed roster revocation, encrypted relay bytes and cost remain unmeasured. Before any optimization, measure per-stage Go CPU and fixed-fixture behavior. A possible experiment is coalescing identical in-flight page computation after each viewer's authority/execution admission. Its key and lifetime must separate machine, execution, query/cursor and any permission-dependent projection; post-command fresh reads cannot be settled by pre-command work. Four computations to one is only a theoretical ceiling for that computation, not a 75% measured system saving. No cache lifetime, protocol, bound, error or update rate was changed here.

The root rerun and selected submission gate, including TypeScript, passed. An earlier privacy-guard failure on a three-digit synthetic missing-session ID is retained; the corrected control uses an allowed fixture. Integration, Cloud document delivery and CI are recorded separately.
