# Multi-reader transcript CPU baseline and attribution

Independent readers repeat material Go CPU work; parser reuse warrants an experiment, while production coalescing and saturation remain unproved. The smallest suggested next experiment is skipping drop-path regex scans when their required literal is absent. No production optimization was implemented.

## Measurement identity

Source HEAD: `ab49653d958167707b760e8b3277fe0676bd1d44`. Source hashes and binary SHA-256 are in provenance.json; measurement-source.diff is the exact test-only delta. Node 24.1.0 / Go 1.27.1 / Darwin arm64. The suite compiles once and uses the same binary across every arm.

Initial fixture: 240 synthetic turns, 92,292 bytes, latest page 200 entries, SHA-256 `94a00406c353c73a31f8d0be36af09e4e4398a4a17cc6cb3537e9118d5b92993`. Every CPU run has exactly 60,953 plaintext answer bytes per successful machine read; the synthetic outer HTTP answer has 61,010 bytes. Separate readers require separate answers. This is not encrypted relay traffic.

The prior root run reports 60,935 payload bytes. Its retained raw record includes counts and signatures, not serialized answer bodies. This run deliberately stabilizes a different synthetic record path and conversation fixture; TranscriptPage.Path is serialized, while file signature/cursor metadata also belong to the run. The exact 18-byte field-level delta cannot be audited from that earlier aggregate record. No cross-run timing/CPU comparison is made, and earlier bytes are not mixed into this run.

## Unprofiled CPU results

Initialized/no-read control Go CPU samples: [0.05, 0.06, 0.07, 0.05, 0.06, 0.05] seconds; median 0.055s. All controls have zero routes. Incremental CPU below subtracts this median once per process. Go process totals include initialization, instrumentation and shutdown; Node CPU is recorded separately.

### fixed-batches

| Readers / callers per batch | Batches | Caller / machine reads | Payload bytes | Go CPU total median / p95 (s) | Incremental Go CPU ms/caller / ms/route / ms/batch | Node CPU ms/caller | Batch wall median / p95 (ms) |
|---|---:|---:|---:|---:|---:|---:|---:|
| 1 / 1 (1) | 1000 | 1000 / 1000 | 60953000 | 7.38 / 7.45 | 7.325 / 7.325 / 7.325 | 1.770 | 8.994 / 20.101 |
| 2 / 2 (2) | 1000 | 2000 / 2000 | 121906000 | 14.23 / 14.34 | 7.087 / 7.087 / 14.175 | 1.385 | 11.107 / 28.465 |
| 4 / 4 (4) | 1000 | 4000 / 4000 | 243812000 | 28.07 / 28.13 | 7.004 / 7.004 / 28.015 | 1.237 | 16.203 / 39.931 |
| 1 / 4 (shared4) | 1000 | 4000 / 1000 | 60953000 | 7.19 / 7.37 | 1.784 / 7.135 / 7.135 | 0.598 | 8.810 / 18.731 |

### fixed-callers

| Readers / callers per batch | Batches | Caller / machine reads | Payload bytes | Go CPU total median / p95 (s) | Incremental Go CPU ms/caller / ms/route / ms/batch | Node CPU ms/caller | Batch wall median / p95 (ms) |
|---|---:|---:|---:|---:|---:|---:|---:|
| 1 / 1 (1) | 4000 | 4000 / 4000 | 243812000 | 28.70 / 29.01 | 7.161 / 7.161 / 7.161 | 1.569 | 8.715 / 20.680 |
| 2 / 2 (2) | 2000 | 4000 / 4000 | 243812000 | 28.35 / 28.77 | 7.074 / 7.074 / 14.148 | 1.352 | 11.008 / 25.685 |
| 4 / 4 (4) | 1000 | 4000 / 4000 | 243812000 | 28.06 / 28.26 | 7.001 / 7.001 / 28.005 | 1.125 | 14.208 / 36.245 |
| 1 / 4 (shared4) | 1000 | 4000 / 1000 | 60953000 | 7.39 / 7.46 | 1.834 / 7.335 / 7.335 | 0.629 | 10.225 / 23.395 |

Each cell has three process runs. Process-CPU p95 over three runs is their maximum, not a production tail estimate. Batch wall percentiles pool the recorded individual batches. User and system CPU are separately preserved in every go.time and cpu-summary.json. CPU per caller and per machine route are deliberately different denominators for shared4.

## Stage attribution


Independent simultaneous readers duplicate substantial Go work: with 4,000 successful callers, process CPU is 28.70/28.35/28.06 seconds for 1/2/4 independent readers, versus 7.39 seconds for four callers sharing one reader. Concurrency changes elapsed completion time; it does not remove machine reads. The shared-reader control demonstrates existing behavior, not a new optimization or the expected saving from sharing only parsing.

**Decision:** an internal parser reuse experiment has a material CPU budget, but there is not enough evidence to adopt cross-viewer coalescing or declare a production binding constraint. First consider the smaller pure internal parser fast path below. The synthetic system has no production arrival rate, latency budget, full terminal inventory or real Cloud read-lane queues against which to identify saturation.

## CPU stages

The one-reader profile covers 4,000 machine reads, 47.67 seconds wall, 26.55 seconds sampled CPU (process user+sys 28.38s). The four-reader profile covers 12,000 machine reads, 54.06 seconds wall, 77.05 seconds sampled CPU (process user+sys 82.66s). These are separate profiled runs; primary comparisons use the unprofiled three-round process measurements. Profiles exclude fixture setup. Sampled CPU is not an exact accounting of process CPU, and CPU can exceed wall with multiple running threads.

All following entries are **cumulative sampled CPU**, not mutually exclusive buckets. Do not sum them or use parent-minus-child arithmetic to invent a stage total.

| Stage / function | 1 reader cum CPU | 4 readers cum CPU |
|---|---:|---:|
| Provider page read/parse, ReadClaudeBefore/readBefore | 13.03s / 49.08% | 33.90s / 44.00% |
| Claude entry decoding, claudeEntries (inside parser) | 9.72s / 36.61% | 25.13s / 32.62% |
| Picture marker normalization, canonicalImageContent (inside entry decoding) | 5.38s / 20.26% | 13.54s / 17.57% |
| Complete end cursor scan, completeEnd (inside parser) | 2.60s / 9.79% | 7.21s / 9.36% |
| Execution admission, Server.admitExecution | 2.08s / 7.83% | 5.03s / 6.53% |
| Post-route execution target check, AdmitExecutionTarget (inside admission) | 0.88s / 3.31% | 2.67s / 3.47% |
| Inventory/actions FindForRead | unresolved at sampling granularity | 0.020s / 0.026% |
| Production page JSON, transcriptRoute line 140 | 0.250s / 0.94% | 0.770s / 1.00% |
| Read-rail correlation JSON, contentReadAnswer | 0.60s / 2.26% | 1.35s / 1.75% |
| Production Bridge.answer (including nested settlement) | 0.35s / 1.32% | 1.14s / 1.48% |
| Production Bridge.settle/payload (inside answer) | 0.070s / 0.26% | 0.300s / 0.39% |
| Synthetic adapter answer decode, server line 194 | 0.590s / 2.22% | 1.26s / 1.64% |
| Synthetic adapter reply Encode, server line 130 | 1.67s / 6.29% | 4.73s / 6.14% |

FindForRead's direct stack is small; this does not assign background inventory goroutine CPU to it or establish the cost of production inventory. Inventory already coalesces work, and the inherited zero-TTL/single-row fixture is unlike production. Authority is the synthetic constant-time callback; absence of material samples is not evidence that real roster checks are free. Production page JSON, wrapper JSON and synthetic adapter JSON must remain separate. Runtime/GC background samples cannot be attributed to a caller by adding cumulative rows.

Parser work is repeated by every successful independent route and has no page-level cache in ReadClaudeBefore. For four perfectly overlapping equivalent reads, avoiding three of four parser computations gives an idealized direct sampled-work budget of 33.0–36.8% of the measured CPU profile (75% of the parser's 44.00–49.08% cumulative share). This is not a measured saving, an end-to-end bound or a production forecast; overlap, mandatory file checks, sharing overhead and runtime/GC effects remain unmeasured. It does not remove the four viewer-specific output payloads or authority/execution checks.

## Minimal candidate and its cost

At internal/adapters/transcript/turns.go:2442, canonicalImageContent scans drop-path regular expressions even for this fixture's plain text. Source attribution places 6.78s cumulative at the drop matcher call and 6.75s at the replacement/trim call in the four-reader profile; these rows are not summed. Every dropPattern match must include the literal `clawdline-` (turns.go:2413). A minimal internal experiment can skip only those drop-pattern match/replacement operations when that necessary substring is absent. Keep unconditional whitespace trimming, image-marker behavior, structured-image counts and picture-only placeholder behavior unchanged. Do not change the regex, protocol, cache lifetime or any bound.

The measured component budget is the 17.57–20.26% cumulative canonicalImageContent share; only part may be removable, and the substring test also costs CPU. Actual gain is unmeasured. Cost is small implementation work plus behavior-equivalence checks for authored text, quoted drop paths, malformed/near markers, mixed structured images, image-only content and whitespace; then remeasure identical plain-text and image-containing fixtures. Drop-path-heavy workloads may gain less or pay an extra substring check. This is a recommendation, not an implemented change.

If that leaves material duplicate parser cost, the larger candidate is active-computation-only parser sharing with the authority, execution, file identity, fresh-command, cancellation/failure and retention conditions in the methods appendix. Cost is medium concurrency and invalidation complexity. A whole-Bridge answer singleflight is unsuitable because answers are viewer-specific.

## Trace interpretation

Trace-derived pprof rendering and limitations are recorded separately below. Trace delay is wait time, not CPU, and aggregate goroutine waits can exceed elapsed wall. The raw runtime trace is retained. The first rendering that supplied an executable to an already symbolized trace profile produced impossible unrelated test names; it is retained as rejected-symbolization text and excluded from conclusions. Correct trace rendering omits the executable. No production queue saturation conclusion is drawn from these synthetic traces.

Corrected trace summaries cover 300 batches / 1,200 machine reads, separately from CPU profiles. Aggregate sync delay is 19.97s (runtime channel receives 16.69s; InventoryReading.Fresh cumulative 0.199s; Server.admitExecution cumulative 0.226s). Aggregate scheduler delay is 5.938s (readBefore.func1 cumulative 1.692s). Syscall delay is 2.12s (ownrecords.ProcessStart for the owned test PID cumulative 1.03s; ReadClaudeBefore cumulative 0.81s). Network wait totals 67.63s, including idle/keep-alive reads and accepts across goroutines; it is not request CPU or a 67-second page load. These are overlapping cumulative wait stacks, not additive stage budgets or proof of a queue bottleneck.


## Existing controls

The original five-round harness completed 309 observations in 65 groups, plus three fresh/send/append races and two identity negatives. verify-controls.mjs compares asks and machine routes for every group with the checked-in root baseline; controls-verification.json records the result. Original cancellation semantics remain unchanged. Revoked fresh reads refuse before the router; a synthetic toggle with warm cache is not proof of real Cloud roster retirement.

## Reproduce and artifacts

From the owned checkout, run `bash <artifacts>/rerun.sh "$PWD" <owned-task-directory>`. rerun.sh acquires heavy once for compile, load, profiles and control verification, with its explicit task-owned Go cache; rerun-worker.sh removes that cache on exit. Scripts use no external service or browser. run-loads.mjs preserves exact arm order and workload. analyze.mjs reproduces pprof/trace text and all numerical summaries.

Primary evidence: measured/*/{cpu-load.json,go.time,go-server.log}; measured/{profile-1,profile-4}/{cpu.pprof,top-flat.txt,top-cum.txt,source-attribution.txt}; measured/trace-4/{runtime.trace,*-top.txt}; measured/original-controls/raw.json; measured/controls-verification.json; measured/cpu-summary.json; provenance.json; measurement-source.diff. The compiled binary is preserved compressed for symbolization, with its digest in provenance.json. measurements.tar.gz contains the complete raw measured directory.


This is a synthetic Node RelayReader/RelayWriter to real Go cloudops/router/transcript baseline, not browser, phone, Cloud relay, production saturation or an optimization acceptance result. No production optimization was implemented. No external API was called.

## Work unit and experiment

One caller read means one successful RelayReader.fetch of the latest 200-entry page. One machine read means one successful admitted Go transcript route. A batch waits for all 1/2/4 independent readers, or four callers of one reader. There are no transcript warmup reads in CPU arms; cold startup is included consistently. Readers are rebuilt every batch; there is no artificial wait or freshness-clock expiry driving repeated routes. Every answer is checked for status, session, 200 entries, latest fixture turn and signature agreement. Every batch checks actual asks equal independent readers; final route count equals successful machine reads.

Three interleaved rounds use orders 1/4/shared4/2/idle, shared4/2/idle/4/1, and 4/idle/1/2/shared4. Denominator order reverses in round 2. Fixed-batches means 1,000 batches per arm (1,000/2,000/4,000 caller reads). Fixed-callers means 4,000 caller reads per arm (4,000/2,000/1,000 independent-reader batches; shared4 has 1,000 batches). Each arm starts a separate owned server and initializes exactly the same synthetic terminal, execution store and 240-turn JSONL fixture. Idle initializes those too, but performs zero transcript reads and has no sleep to manufacture a duration.

The inherited test server configures InventoryReading with TTL 0, unlike the production default InventoryTTL (2 seconds). Its one synthetic terminal row has zero host delay. Real provider parsing and execution-store operations are exercised, but real Cloud transport read-lane scheduling is absent. Inventory CPU here cannot price production terminal scans or their cache-hit rate.

The server is compiled once; no source or binary changes occur between measured arms. /usr/bin/time -p -o records user and system CPU of the Go server process, including Go runtime/background work, fixture/startup, route instrumentation, final metrics serialization and shutdown. The timer excludes Node. A separate Node process.cpuUsage interval records Node CPU; elapsed batch time is real monotonic wall time. Idle CPU is reported separately; incremental CPU subtracts the median initialized/no-read CPU once per server, not once per read. CPU is not inferred from wall time or a sampling percentage. time -lp fails the sandbox's sysctl kern.clockrate query, while -p succeeds; extra resource counters and hardware sysctl are therefore unavailable. time has 0.01-second output resolution.

Primary CPU comparisons are unprofiled. Separate profile runs cover only the load interval after fixture initialization. Go CPU profiles are sampled CPU; cumulative rows overlap and must never be summed. Runtime trace is collected separately and its scheduler/synchronization/syscall/network profiles measure waiting, not CPU. Neither a high cumulative function nor a local CPU/wall ratio establishes a production binding constraint without real arrival rate, service budget and queue evidence.

## Existing reuse, verified in source

* RelayReader.transcript in web/console/src/cloud/relay-reader.ts:1994 reuses held data and in-flight work within one reader. A fresh call waits out a pre-existing flight and asks again; separate readers own separate maps.
* Actions.FindForRead in internal/app/actions.go:161 uses InventoryReading.Fast. InventoryReading.Fast/Fresh in internal/app/inventory_reading.go:306/:518 already reuse snapshots or join scans, with different freshness guarantees. Execution admission still performs its authoritative checks; shared inventory is not permission to share admission decisions.
* ReadClaudeBefore/readBefore in internal/adapters/transcript/pages.go:59/:76 opens/stats the file, allocates a new filter, scans and parses rows per call, then checks the signature again. There is no completed page cache or page singleflight here. Other transcript metadata caches, such as info.go, do not cache this parsed page.
* transcriptRoute/transcriptPageBefore in internal/transport/http/transcript.go:79/:148 resolve the session, run the page parser, project artifacts/entries and encode each route response. admitPinnedTarget may reuse a pinned admission within that same request (execution.go:201). Bridge.route in internal/app/cloudops/cloudops.go:641 rechecks execution and transcript authority after the router, before settling a viewer-specific answer. Synthetic authority is constant-time and does not model roster or crypto cost.

## Smallest possible internal candidate, not implemented

If parser CPU and actual overlap justify it, experimentally share only an immutable parsed provider page for overlapping requests, below per-viewer admission and above provider parsing. Do not share a whole cloudops Answer: sender sequence/correlation, authority, execution checks and output serialization remain individual. Do not add a completed cache, TTL or new retention bound.

A join key must cover machine, session, execution generation, provider/record path and file identity (including replacement identity), observed size and precise modification identity, limit and before/after cursor, parser/projection policy and any permission-dependent content view. Each viewer retains its own pre/post authority and execution recheck. Mutable entry slices/projections must not be shared unsafely.

Freshness is the difficult boundary. A latest read has no new fresh marker on the existing Go wire, so a parser flight cannot simply assume all same-key requests may join. Known local session commands must remove/mark pre-command active work ineligible for new joiners before the command executes, leaving existing waiters' behavior intact; post-command reads must start or join only work admitted after that barrier. This can be scoped to active flights, with no retained epoch cache. External append/replacement must change file identity/version and must be checked again before releasing shared work; preserve the parser's current append retry behavior, without claiming a stronger consistency guarantee. If this barrier cannot be proved without protocol changes, do not adopt cross-request sharing under this task's constraints.

Cancellation must detach one waiter without canceling other admitted viewers; stop shared work only when no eligible waiter remains and the parser supports stopping. Preserve the existing latest-read cancellation semantics observed by the harness. Errors and cancellations must evict the flight immediately; never cache a failure or let a settled flight serve a later request. Append/replacement, changed execution and command barriers make old flights ineligible. Retain data only while that computation and its existing waiters are active, and release all references on every settlement path.

Expected gain is at most the measured parser fraction times 75% for four perfectly overlapping identical machine reads; this is a theoretical CPU ceiling for that component, not a measured system improvement. Output bytes, per-viewer checks and non-parser work remain. Engineering cost is medium: a new internal concurrency lifecycle, file/version handling, immutable data boundaries, command invalidation and failure/cancellation race tests. If profiles show a different dominant stage or overlap is insufficient, defer this candidate and measure that stage first.

## Scope left unmeasured

No before/after production optimization, real terminal process scans, real roster/crypto, encrypted frames, relay/TLS, browser UI, phone, production arrival/queue behavior, billing or saturation is measured. The opt-in loopback adapter decodes and re-encodes an answer for Node; that synthetic JSON cost must be distinguished from production Bridge.answer/payload CPU. The synthetic inventory is one row and cannot price a real large inventory scan. No publish/spool optimization is remeasured.


## Root integration note

Measurement source is preserved by the original source diff and hashes. The root then made only the output-directory narrowing explicit by passing the already validated path into cpuLoad; this does not change workload or accounting. The original 34-process CPU evidence was retained. A separate root smoke run exercised 1/4/shared4/idle arms with three batches, and the original controls ran again. The TypeScript and scoped gate with caller compilation passed after this type correction. The earlier type-check failure remains recorded separately. These smoke samples do not replace the primary three-round measurements.

The reproducible harness lives in `web/console/src/cloud/read-efficiency.bench.ts` and the opt-in server in `internal/transport/http/cloud_read_efficiency_serve_test.go`. Compile the server through heavy with `go test -c -o <owned-binary> ./internal/transport/http`. Run Node through heavy with CLAWDLINE_READ_BENCH_BINARY, CLAWDLINE_READ_BENCH_OUT and CLAWDLINE_READ_BENCH_WORK naming task-owned paths. Set CLAWDLINE_READ_BENCH_CPU_ARM to 1, 2, 4, shared4 or idle, and CLAWDLINE_READ_BENCH_BATCHES to the table's batch count. Set CLAWDLINE_READ_BENCH_ROUND and CLAWDLINE_READ_BENCH_DENOMINATOR for provenance. Use `node --experimental-strip-types web/console/src/cloud/read-efficiency.bench.ts`. Each invocation owns and stops its server; go.time is that server's user/system CPU. Profile and trace output paths are optional CLAWDLINE_READ_BENCH_CPU_PROFILE and CLAWDLINE_READ_BENCH_TRACE. Retain raw files, then remove only the owned binary/cache after all processes exit. No production service or browser is required.

The [machine-readable CPU record](cloud-read-cpu.json) preserves process samples and all denominators. CPU percentages in this document are overlapping cumulative profile samples; they are not additive budgets. No optimization or production speedup was implemented by this measurement change.
