# Live queue sorting with retained Cloud receipts

The outbound spool retains terminal rows to distinguish late or duplicate acknowledgements from unknown sequences. Its four live-work operations formerly sorted those receipts alongside reserved, ready and sent rows. They now sort only nonterminal rows, while retaining the original receipt map, expiry, caps and acknowledgement correlation. The scan remains O(total rows); sorting becomes O(live rows log live rows). No parallel index, timer, wire field, error code or bound was added.

## Fixed comparison

The baseline source is b8ac05ef8fef903cad7eb0a768672e13a4425fd1, with the benchmark from 6955f043d31adca893fb3a273f812f5dac38edfc. Candidate source is 468662833. Go 1.27.1, darwin/arm64, GOMAXPROCS=1. Each arm used the same compiled binary across three alternating rounds. Each efficiency iteration contains 60 passes over 300 simulated seconds, with 40 iterations per workload. This does not wait 300 real seconds. ReceiptScan varies only retained terminal receipts in an otherwise empty queue.

| Workload | Baseline elapsed | Candidate elapsed | Change |
| --- | ---: | ---: | ---: |
| One idle Session | 9.061 ms | 7.842 ms | -13.45% |
| 32 idle Sessions | 72.151 ms | 72.214 ms | +0.09% |
| 32 unwatched Sessions | 26.891 ms | 14.912 ms | -44.55% |
| 32 active Sessions | 95.173 ms | 76.835 ms | -19.27% |
| 32 Sessions, dense restatement | 2125.205 ms | 782.688 ms | -63.17% |
| Empty SendNext, 2048 receipts | 360.673 us | 55.310 us | -84.66% |

Values are three-round medians, not statistical significance claims. The idle-32 workload is effectively unchanged. Restatement allocations decline from 258226715 to 117254718 bytes per window. Empty SendNext with 2048 receipts declines from 32880 bytes and six allocations per call to zero bytes and allocations.

Reads, seal calls, synthetic acknowledgements, plaintext bytes, sealed bytes and framed bytes match across all six runs for all five workloads (30 metric combinations). The fixture uses real reservation, encryption, signing and spool transitions; frame construction replaces socket I/O, settlement is synchronous and synthetic, and Session JSON is prebuilt. It does not measure actual relay acknowledgements or source-list construction.

Median whole-process user/system CPU for the combined workload command is 90.87/1.00 seconds versus 38.64/0.28 seconds. These INCLUDE fixtures, calibration and warmup. Maximum RSS medians are 33226752 versus 30851072 bytes, also including fixtures; neither is live-queue heap or production usage. Elapsed benchmark time is not CPU time. No production request reduction, network cost or user capacity is established by these results.

## Reproduction

Use isolated baseline and candidate checkouts, with the identical benchmark file above in both. Run compilation and each benchmark through tools/heavy.sh, using task-owned Go caches; remove those caches after all their processes exit. Compile once per arm with `go test -c -o <arm-binary> ./internal/transport/cloud`. Run from that package directory:

```sh
<arm-binary> -test.run='^$' -test.bench='^BenchmarkPublisherEfficiency$' -test.benchtime=40x -test.count=1 -test.cpu=1 -test.benchmem
<arm-binary> -test.run='^$' -test.bench='^BenchmarkPublisherReceiptScan$' -test.benchtime=1s -test.count=1 -test.cpu=1 -test.benchmem
```

Alternate baseline then candidate for three rounds; retain stdout and process CPU/RSS from `/usr/bin/time -lp`. [The machine-readable record](cloud-spool-sorting.json) preserves every arm and median comparison. Do not mix the earlier three-digit synthetic-ID fixture with this two-digit fixture.

## Behavior and review

Observable regression tests cover reserved-head blocking, global sequence order, recipient-specific coalescing, sent-row preservation, ordered in-flight rows, identical resend bytes and metadata, row/byte windows, burn notifications, expiry, receipt cap/retention, and late/duplicate/mismatched/unknown acknowledgements. Mutants that wrongly omit reserved or sent rows fail these tests. TakeBurned still scans the complete receipt map and delivers each burn once.

Independent review of integrated candidate cb5b83631200d821afc0bd57264d8150eb96871d found no compatibility or maintainability blocker. It independently parsed the six raw runs and confirmed the same 30 metric combinations; it did not independently re-time the benchmarks. Both Cloud packages passed uncached runtime regression. A first scoped gate failed because a direct-ICE UDP bind reported address already in use; that failure is retained separately, without attributing it to this change. The subsequent scoped gate with caller compilation passed, including six transitive callers.

Actual Cloud document reading of the corresponding HTML comparison succeeded before installation. This does not establish runtime acceptance of the new spool. Integration, installed running version, public CI and cleanup are recorded separately. Multi-browser, production Cloud, slow acknowledgements and capacity measurements remain outside this benchmark.
