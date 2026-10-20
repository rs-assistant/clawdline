# Connection health: evidence contract v1

Status: 2026-10-20. The local evidence foundation in `internal/domain/connectionhealth` is
implemented and tested, and the daemon now runs one runtime of it per boot, instruments the relay
transport, the WebSocket writer and the link's lanes, and answers it on a local route and the
`clawdline health` CLI. The durable store keeps the journal across boots, and an opt-in daemon
uploader (§9a) sends its content-free metadata to Cloud's machine ingest over its own HTTPS
client. The query shapes in §9 remain a proposal; Cloud's implemented API is its own
`docs/connection-health-contract.md`. Product scope stays as `docs/admin-connection-health.md` states it
(P0–P3, M01–M06); this page is only the wire and evidence contract under it.

## 1. What exists and what does not

| Part | State | Where |
| --- | --- | --- |
| Active-phase table (128 slots, 8 reserved) | implemented | `internal/domain/connectionhealth/active.go` |
| Watchdog (reads atomics and its own clock only; queues transitions to one journal writer) | implemented | `watchdog.go` |
| In-memory journal ring and query page | implemented | `journal.go` |
| Segment truth table, stage progress, local export | implemented | `evidence.go` |
| One runtime per daemon boot (fresh random `boot_id`, build from the binary's commit), watchdog run for the daemon's lifetime, Cloud on or off | implemented; `CLAWDLINE_CONNECTION_HEALTH=off` turns it off | `runtime.go`, `cmd/clawdline/health.go` `startConnectionHealth` |
| Capacity rows `cloud.health_*` | measured from the tracker snapshot, journal occupancy and transition queue; unmeasured with a reason when monitoring is off | `internal/transport/http/capacity_measures_connection_health.go` |
| Daemon call sites | **partial**: see §4.1 for the exact list. One correlation now runs from the transport's frame phase through the relay queue (`Inbound.Correlation`), the lane and handler, the answer's reservation, seal and spool enqueue (the handler's context, `connectionhealth.WithCorrelation`), the spool row (`SpoolRow.Correlation`, local only) and every drain write and reconnect re-send of that row. Answers published outside a handler (status snapshots, terminal frames, carrier answers) are still uncorrelated | `internal/adapters/cloud/transport.go`, `websocket.go`, `spool.go`, `internal/transport/cloud/link.go`, `relay.go` |
| Local route `GET /v1/connection-health/{status,events,trace,export}` and `clawdline health` | implemented, this machine's own token only, refused through Cloud | `internal/transport/http/connection_health.go`, `cmd/clawdline/health.go` |
| Per-build location map | **partial**: every wired location code names its Go function (checked by `runtime.FuncForPC` in the owning package's tests); no file lines | `locations.go` |
| Durable segmented store, boot markers on disk | implemented and opened by the daemon off the startup path; the watchdog's journal writer offers each stamped record (`Watchdog.Attach`); history route and CLI read it. The ring itself is still memory only and says so | `internal/adapters/healthjournal`, `docs/admin-connection-health-storage.md` |
| Stack capture worker | **not built** | — |
| Daemon uploader to Cloud ingest (opt-in, own HTTPS client, bounded, durable progress) | implemented, off by default (§9a) | `internal/adapters/healthupload`, `cmd/clawdline/health.go` |
| Shutdown: watchdog and writer stopped, uploader stopped, store flushed and closed within 3 s | implemented on SIGTERM/interrupt | `cmd/clawdline/health.go` `stopConnectionHealthOnSignal` |
| Browser viewer producer (opt-in, own HTTPS upload, bounded) | implemented, off by default; installed only by an explicit `startViewerHealth` call (§11) | `web/console/src/health-viewer`, `web/console/src/legacy/js/net/viewer-health-hook.js` |
| Cloud stored queries in a UI, owner sealing | **not built here** (separate tasks) | — |

Every export carries the same list as `wiring[]` (`evidence.go` `Unwired`), so no reader can take
the foundation for the pipeline.

## 2. Identifiers

All strings the package exports are enums, location codes or opaque ids. There is no free-text
field. Validation is at the door (`Tracker.Begin`, `Journal.Append`, `NewJournal`) and a refusal
names the field, never its value. `Journal.Append` checks each field against its own rule —
`event`, `kind`, `rule`, `source`, `stage` and `class` against their allowlists, `location` and the
ids against their patterns — rather than one pattern over all text. The stamps are the journal's:
a record that arrives with its own `schema`, `producer_seq`, `event_id`, `boot_id`, `wall` or
`mono_ns` is refused (`invalid_field` naming `schema`, `producer_seq`, `event_id`, `boot_id` or
`time`), not silently overwritten. `boot` and `transitions_lost` are written only by the journal
itself, `build_commit` only on `boot`, and `rule` is only `watchdog/1`. A refused record takes no
sequence number, so a hole in `producer_seq` is always an eviction.

| Identifier | Rule | Examples |
| --- | --- | --- |
| Opaque id: `boot_id`, `build_commit`, `correlation` | `^[A-Za-z0-9_-]{1,64}$` — no `/`, `.`, `:`, space, so a path, URL or `Bearer x` header is rejected | `boot-7f3a`, `c9fe745db066`, `tr-01HZX3` |
| Location code | `^[a-z][a-z0-9_]*(\.[a-z0-9_]+){0,7}$`, ≤ 96 chars — never a file path or line | `cloud.link.read_handler`, `cloud.transport.read_loop` |
| `event_id` | `<boot_id>-<producer_seq>`; unique per producer boot | `boot-7f3a-3` |
| `phase_id` | process-local counter; correlation only, never authority | `2` |

**Sources** (`source`): `daemon`, `cloud_api`, `relay`, `viewer`, `collector`.

**Stages** (`stage`, path order — `Lifecycle`): `client_issued`, `cloud_accepted`,
`relay_socket_accepted`, `relay_forwarded`, `frame_read`, `sender_verified`, `lane_queued`,
`handler_started`, `handler_completed`, `response_sealed`, `spool_queued`, `writer_lock_waiting`,
`socket_write_started`, `socket_write_ended`, `endpoint_received`, `endpoint_decrypted`,
`ui_applied`, `acknowledged`; plus `idle` for a long-lived loop between pieces of work.
`ui_applied` is a browser applying data; `acknowledged` is only a person's explicit action.

**Classes** (`class`, the only metric dimension): `read`, `control`, `status`, `transport`, `probe`.

**Kinds** (`kind`): `finite` has warn and block deadlines and can be blocked; `long_lived` has
neither and is reported `idle`, never blocked, for waiting for work.

**Watchdog states** (`state`): `progressing`, `slow` (≥ warn), `blocked` (≥ block with no
progress), `idle`, `unverified_after_gap` (crossed block in the first scan after the watchdog was
itself not scheduled; called blocked only if the next scan agrees).

**Journal events** (`event`): `boot`, `slow_detected`, `stall_detected`, `recovered` (new
progress after slow/blocked — silence is not recovery), `released_after_stall` (slot released;
the watchdog cannot see the outcome, so this is not success), `overflow`, `scheduling_gap`,
`clock_anomaly`, `transitions_lost` (`count` watchdog transitions were dropped just before this
record because the queue to the journal was full; the page holding it reads `partial`),
`state_after_loss` (written by the journal's writer right after each `transitions_lost`, once per
phase the latest report holds as `slow`, `blocked` or `unverified_after_gap`, with that `state`:
the dropped transition may have been the phase's `stall_detected`, and the watchdog never repeats a
transition, so without it a journal could read "loss, then quiet" while a handler is still stuck).
`state` appears on no other event, and no caller may append either of the writer's events.

**Outcome** and **quality** are design §5's: outcomes `healthy`, `slow`, `blocked`, `error`,
`offline`, `unknown`, `no_requests`; qualities `complete`, `partial`, `stale`, `not_collected`,
`unsupported`, `forbidden`, `no_requests`. `Classify` is the table; its tests pin that an open
socket with nothing completed is `no_requests`, a source with no receipt is
`unknown`/`not_collected`, and an open socket beside a blocked handler is `blocked`.

## 3. Lifecycle reading

`ProgressAlong(path, receipts, active, visible)` answers, for one logical operation:

- `last_proven_stage` — the end of the contiguous run of receipts from the start of the path;
  empty when the first stage has none.
- `active_stage` — set **only** from an independent active-phase span (a `Phase` in the table);
  `null` otherwise, never inferred.
- `next_unobserved_stage` — the first stage after the proven run without a receipt.
- `visibility_limit_at` — set when that stage's recorder is a source this scope may not read, so
  its absence says nothing.
- `later_receipts` — receipts past the hole: evidence, not proof the hole's stage happened.

## 4. Local evidence export v1 (implemented)

Produced by `Export(&report, journal, query)`. The block below is the exact output of
`ExampleExport` in `internal/domain/connectionhealth/example_test.go`, which the test suite pins:
a read handler that started 40 ms after being queued and then made no progress for 16 s, beside
an idle reader loop. Durations are nanoseconds of the producer's monotonic clock; `wall` is for
display and may jump.

```json
{
  "schema": "clawdline.connection-health.evidence/1",
  "generated_at": "2026-10-20T12:00:16.04Z",
  "watchdog": {
    "rule": "watchdog/1",
    "wall": "2026-10-20T12:00:16.04Z",
    "mono_ns": 16040000000,
    "findings": [
      {
        "phase_id": 1,
        "source": "daemon",
        "stage": "idle",
        "class": "transport",
        "kind": "long_lived",
        "location": "cloud.transport.read_loop",
        "state": "idle",
        "age_ns": 16040000000,
        "active_ns": 16040000000,
        "progress": 0
      },
      {
        "phase_id": 2,
        "source": "daemon",
        "stage": "handler_started",
        "class": "read",
        "kind": "finite",
        "location": "cloud.link.read_handler",
        "correlation": "tr-01HZX3",
        "state": "blocked",
        "age_ns": 16000000000,
        "active_ns": 16040000000,
        "deadline_ns": 15000000000,
        "progress": 1
      }
    ],
    "overflow": {
      "total": 0,
      "critical": 0
    },
    "trace_coverage": "complete",
    "scan_seq": 16,
    "transitions": "complete",
    "transitions_lost_total": 0,
    "transitions_pending": 2
  },
  "journal": {
    "schema": "clawdline.connection-health.page/1",
    "generated_at": "2026-10-20T12:00:16.04Z",
    "boot_id": "boot-7f3a",
    "build_commit": "c9fe745db066",
    "records": [
      {
        "schema": "clawdline.connection-health.record/1",
        "producer_seq": 1,
        "event_id": "boot-7f3a-1",
        "boot_id": "boot-7f3a",
        "event": "boot",
        "wall": "2026-10-20T12:00:00Z",
        "mono_ns": 0,
        "build_commit": "c9fe745db066"
      },
      {
        "schema": "clawdline.connection-health.record/1",
        "producer_seq": 2,
        "event_id": "boot-7f3a-2",
        "boot_id": "boot-7f3a",
        "event": "slow_detected",
        "wall": "2026-10-20T12:00:05.04Z",
        "mono_ns": 5040000000,
        "source": "daemon",
        "stage": "handler_started",
        "class": "read",
        "kind": "finite",
        "location": "cloud.link.read_handler",
        "phase_id": 2,
        "correlation": "tr-01HZX3",
        "age_ns": 5000000000,
        "deadline_ns": 5000000000,
        "count": 1,
        "rule": "watchdog/1"
      },
      {
        "schema": "clawdline.connection-health.record/1",
        "producer_seq": 3,
        "event_id": "boot-7f3a-3",
        "boot_id": "boot-7f3a",
        "event": "stall_detected",
        "wall": "2026-10-20T12:00:15.04Z",
        "mono_ns": 15040000000,
        "source": "daemon",
        "stage": "handler_started",
        "class": "read",
        "kind": "finite",
        "location": "cloud.link.read_handler",
        "phase_id": 2,
        "correlation": "tr-01HZX3",
        "age_ns": 15000000000,
        "deadline_ns": 15000000000,
        "count": 1,
        "rule": "watchdog/1"
      }
    ],
    "coverage": {
      "quality": "complete",
      "first_retained_seq": 1,
      "last_seq": 3,
      "evicted_total": 0,
      "refused_total": 0,
      "truncated": false,
      "next_after_seq": 3,
      "durability": "memory_only_lost_on_restart"
    },
    "newest_age_ns": 1000000000
  },
  "wiring": [
    { "part": "active_phase_tracker", "status": "implemented" },
    { "part": "watchdog", "status": "implemented" },
    { "part": "memory_journal", "status": "implemented" },
    { "part": "daemon_runtime_per_boot", "status": "implemented" },
    { "part": "daemon_instrumentation_call_sites", "status": "partial" },
    { "part": "durable_segmented_journal", "status": "implemented" },
    { "part": "durable_history_route", "status": "implemented" },
    { "part": "stack_capture_worker", "status": "unimplemented" },
    { "part": "local_health_cli_and_route", "status": "implemented" },
    { "part": "cloud_ingest", "status": "unimplemented" },
    { "part": "cloud_query", "status": "unimplemented" },
    { "part": "relay_and_viewer_producers", "status": "unimplemented" },
    { "part": "stage_location_map_per_build", "status": "partial" }
  ]
}
```

(The `wiring` entries are printed one per line here; the pinned output prints each on four.)

### Coverage metadata

| Field | Meaning |
| --- | --- |
| `coverage.quality` | `complete`, or `partial` when records the query covers were evicted or the page holds a `transitions_lost` record |
| `first_retained_seq`, `last_seq` | the ring's retained range when read |
| `gap_from_seq`, `gap_to_seq` | the exact sequence numbers asked for and already evicted |
| `evicted_total`, `refused_total` | lifetime counts of evicted and refused records |
| `truncated`, `next_after_seq` | the page hit a bound; continue with `after_seq = next_after_seq` |
| `durability` | `memory_only_lost_on_restart` until the on-disk journal exists |
| `newest_age_ns` | freshness of the newest retained record |
| `watchdog.trace_coverage` | `partial` once any `Begin` was refused for want of a slot |
| `watchdog.overflow` | refused `Begin`s: total, critical, `by_class`, first and last monotonic offset |
| `watchdog.gap_ns`, `clock_backwards_ns` | the watchdog itself was late, or the clock went backwards |
| `watchdog.scan_seq` | scans so far; it advances even while the journal is held |
| `watchdog.transitions` | `complete`, `partial` once any transition was dropped before the journal, `not_collected` without a journal |
| `watchdog.transitions_lost_total`, `transitions_pending` | dropped transitions, and the queue's depth when the report was made (the example shows 2 because it reports before `Flush`) |

### 4.1 Wired call sites and the local route

Every daemon phase is `source: daemon` with the plan's 5 s / 15 s deadlines; each long-lived loop
holds one of the 8 reserved slots for its socket's life, so three are used per connected line.
Correlations are minted by the tracker (`op-<base36 counter>`), never taken from a frame, channel,
sender, session or document; `phase_id` is the attempt, the correlation the logical operation.

| Location | Stage(s) | Function | Kind |
| --- | --- | --- | --- |
| `cloud.transport.read_loop`, `keepalive_loop`, `drain_loop` | `idle` | `(*Transport).read`, `keepalive`, `drain` | long-lived, reserved |
| `cloud.transport.handle_frame` | `frame_read` | `(*Transport).handle` (and `AcceptDirect`) | finite, one per frame |
| `cloud.transport.settle_receipt` | `frame_read` | `(*Transport).handleFrame` (ack and publish-error settlement) | same phase, stepped |
| `cloud.transport.sender_lookup`, `signature_check` | `frame_read` | `(*Transport).admitTraced` | same phase, stepped |
| `cloud.transport.replay_claim`, `open_envelope` | `sender_verified` | `(*Transport).admitTraced` | same phase, stepped |
| `cloud.transport.inbound_callback`, `peer_callback` | `lane_queued` | `admitTraced`, `handleFrame` | same phase, stepped |
| `cloud.transport.drain_send` | `spool_queued` | `(*Transport).drainPass` (one per drain tick and per reconnect re-send) | finite |
| `cloud.websocket.writer_lock` → `socket_write` | `writer_lock_waiting` → `socket_write_started` → `socket_write_ended` | `(*Conn).writeFrame` | finite, one per frame written |
| `cloud.link.read_lane`, `command_lane`, `sessions_lane` | `lane_queued` | `(*Link).lanePhase` | finite, one per request |
| `cloud.link.read_handler`, `command_handler`, `sessions_handler` | `handler_started` → `handler_completed` | `(*Link).answerTraced` | same phase, stepped |
| `cloud.relay.reserve` → `seal` → `spool_seal` | `handler_completed` → `handler_completed` → `response_sealed` → `spool_queued` | `(*Relay).PublishTracked` | finite, one per answer, class `transport`, under the handler's correlation |
| `cloud.relay.direct_write` | `handler_completed` | `(*Relay).PublishTracked` | finite, one per direct answer; ends at the carrier's write and claims no spool or relay stage |

**One answer, one correlation.** `answerTraced` puts the handler's correlation on the context it
hands `Service.Answer` (`connectionhealth.WithCorrelation`; a nil tracker or no correlation leaves
the context untouched and allocates nothing). `Relay.PublishTracked` begins the answer's own phase
before the spool's lock (`cloud.relay.reserve`), steps it to `cloud.relay.seal` before sealing and to
`cloud.relay.spool_seal` (`response_sealed`) before taking the lock again to queue the bytes, and
ends it at `spool_queued`. The handler's own phase still ends only when `Answer` returns. The spool
keeps the correlation on the row (`Spool.SealCorrelated`), bounded by the row caps and dropped with
the row's bytes when it is acknowledged, refused, burned or coalesced, and gone when the row is
collected; it is never in the sealed envelope, the frame, or any wire field. The drain writes the
row with `Conn.WriteTextTraced`, and a reconnect re-send (`Spool.ResendCorrelated`) does the same:
each write is its own `writer_lock` → `socket_write` phase, with its own `phase_id`, under the one
correlation. Pings, closes, subscribes and untraced `WriteText` callers keep an uncorrelated phase.
The drain pass phase (`cloud.transport.drain_send`) covers many rows and stays uncorrelated.

Nothing here changes a deadline, a return value, an error or a retry; a nil tracker (monitoring
off, or a test or old caller that passes none) records nothing. A full table means a phase is not
recorded and the overflow is counted; the business call proceeds.

`GET /v1/connection-health/status` (latest report, journal coverage, wiring, report age; quality
`not_collected` before the first scan and `stale` when the report is more than three scans old),
`events?boot_id&after_seq&limit&since&until`, `trace?correlation=` (active findings and journal
records naming it) and `export` (§4 plus `locations`, the map above bound to `build_commit`). Only
this machine's own token reads them; a paired device or the machine token is refused, and a request
the Cloud line dispatched in process is refused `forbidden` even though it carries this machine's
token. The route reads atomics and the journal's lock; it never calls `Scan`, the relay, the
spool or a status snapshot. `clawdline health status|events|trace|export` reads it with the local
token (`--since/--until` as RFC 3339 or a duration back from now, `--after-seq`, `--limit`,
`--boot-id`, `--correlation`, `--json`); a daemon it cannot ask is reported unknown, exit 1.
`history?cursor&limit` (and `clawdline health history --cursor --limit`) pages the durable store
across boots; until the store is open it answers `unsupported` naming `opening` or the failure
code. `status` carries a `durable` block: `opening`, `open` with the store's `Status`, or `failed`
with its code.

## 5. Typed failures

`CodeOf(err)` returns one of these; no failure is turned into an empty page.

| Code | When | Caller sees |
| --- | --- | --- |
| `invalid_field` | an enum, id or location fails its rule; `Error.Field` names it | the phase or record is not kept |
| `slots_exhausted` | no free slot | zero `Handle` (every call a no-op); the operation proceeds uninstrumented; counted |
| `record_too_large` | an encoded record over 1 KiB | not kept; `refused_total` +1 |
| `query_bound_exceeded` | `limit` < 0 or > 200 | no page |
| `cursor_other_boot` | `boot_id` in the query is not this journal's boot | no page: the sequence numbers it names do not exist (HTTP 409) |
| `invalid_cursor` | `after_seq` past the newest sequence this boot assigned, or not a number that fits one (the largest `uint64` included) | no page; HTTP 400 |
| `unsupported` | export asked of a missing watchdog report or journal; monitoring off | no export — never a quiet-looking empty one (HTTP 503 before the first scan, 501 when off) |
| `forbidden` | the local route asked through the Cloud line | HTTP 403 |
| `unsupported_schema`, `idempotency_conflict` | reserved for the ingest and query routes below | — |

## 6. Bounds

All are proposals from the plan, registered in `internal/domain/capacity` and lowerable by
`CLAWDLINE_NEXT_CAPACITY`. `TestEveryBoundIsRegistered` was red on all seven constants before the
rows were added, and on `MaxPendingTransitions` and its channel before the eighth.

| Row | Limit | At the limit |
| --- | --- | --- |
| `cloud.health_active_slots` | 128 rows | refuse `Begin`, count by class and interval, coverage partial |
| `cloud.health_reserved_slots` | 8 rows (of the 128) | refuse critical `Begin`, counted as critical |
| `cloud.health_journal_rows` | 4,096 records | evict oldest, count, name the gap |
| `cloud.health_journal_bytes` | 4 MiB | evict oldest, count, name the gap |
| `cloud.health_record_bytes` | 1 KiB per record | refuse, count |
| `cloud.health_pending_transitions` | 256 transitions queued for the journal writer | drop the transition, count it, report `transitions: partial`, journal `transitions_lost` |
| `cloud.health_query_rows` | 200 per page | stop, `truncated`, cursor |
| `cloud.health_query_bytes` | 1 MiB per page | stop, `truncated`, cursor |

Parameters, not capacities: watchdog cadence `DefaultCadence` 1 s; finite deadlines are per
`Spec` (the plan's proposed 5 s / 15 s); scheduling gap is a scan later than 2 × cadence;
`ErrorRateThreshold` 1 %.

### Measured cost

`go test -run '^$' -bench . -benchtime 2s -benchmem ./internal/domain/connectionhealth` on an
Apple M4 Pro, 14 threads, 2026-10-20, rerun after the queued journal writer and the `End` retry:

| Benchmark | Result |
| --- | --- |
| `Begin` + `Progress` + `End`, parallel | 298.3 ns/op, 416 B/op, 2 allocs/op (before: 298.6 ns/op) |
| One `Scan` of 120 active phases, journal attached, no transitions | 26.9 µs/op, 121.3 KB/op, 24 allocs/op (before: 22.4 µs/op, 102.6 KB/op, 22 allocs/op; the difference is the copy of the findings published for `Latest`) |

The call sites, on and off (nil tracker), `go test -run '^$' -bench 'WriteFrame|HandleFrame'
-benchtime 2s -count 3 -benchmem ./internal/adapters/cloud` and `-bench IdleScan` in this package,
same machine, 2026-10-20 (three runs each, middle value):

| Benchmark | Off | On |
| --- | --- | --- |
| `WriteFrame`: one masked text write to a discarding socket | 106 ns/op, 96 B, 2 allocs | 1008 ns/op, 720 B, 5 allocs |
| `HandleFrame`: one peer frame through `handle` to its callback | 204 ns/op, 16 B, 1 alloc | 1194 ns/op, 449 B, 5 allocs |
| `IdleScan`: one scan of an idle line (three loops held), paid once a second | — | 956 ns/op, 3.5 KB, 11 allocs |

So instrumentation adds about 0.9–1.0 µs and three to four allocations to each frame read or
written, most of it clock reads and the immutable phase copy each step makes; the idle watchdog
costs about 1 µs per second. Not measured: daemon CPU under real relay traffic, a scan under load
or with queued transitions, memory of a full ring.

## 7. Concurrency

- `Tracker`: each slot is an `atomic.Bool` claim and an `atomic.Pointer[Phase]` to an immutable
  snapshot. `Progress` swaps in a copy with CAS; `End` clears it only if it still holds its own
  phase, retrying when a concurrent `Progress` replaced the snapshot, so it never leaves a slot
  claimed; a second `End`, or a stale handle after the slot was reused, finds another phase or
  none and does nothing. Nothing blocks. (`TestEndThatRacesProgressStillReleasesTheSlot` puts a
  `Progress` between `End`'s read and its swap; it failed on the single-CAS `End`.)
- `Watchdog.Scan` is called from one goroutine (`Run`, whose lifetime is its context). It reads
  only the tracker's atomics and its clock, and takes no lock: each transition goes into a
  256-row queue with a non-blocking send, a full queue drops and counts it, and the report is
  published with an atomic store for `Latest`. `Run` accepts no caller callback.
- One journal writer goroutine, started and joined by `Run`, is the only code that waits on the
  journal. It appends each transition with the scan's own instant and writes `transitions_lost`
  exactly where drops happened. A journal held forever stops the writer and delays `Run`'s
  return until it is released, never a scan (`TestAScanNeverWaitsForAHeldJournal`,
  `TestRunAdvancesWhileTheJournalIsHeldAndLeavesNothingRunning`; both failed with the old
  synchronous append). `Flush` drains the queue on the caller's goroutine for a caller without
  `Run`.
- `Journal` has its own mutex, held for a copy and one `json.Marshal` of a ≤ 1 KiB record, or for
  one page read. No business code holds it, and it does no I/O.
- `Export` takes the watchdog's latest report rather than calling `Scan` itself, so the watchdog
  stays single-owner.

## 8. Permission classification

Classes the Cloud/owner routes must enforce per field. Superuser never receives owner-private
classes.

| Class | Fields | Cloud operator | Account owner (own machine) | This viewer |
| --- | --- | --- | --- | --- |
| Service metadata | `schema`, `rule`, `source`, `stage`, `class`, `kind`, `event`, `state`, outcome, quality, coverage counts, freshness, bounds | yes | yes | own viewer source only |
| Machine identity | `boot_id`, `build_commit`, presence, last snapshot age | yes (opaque machine id) | yes | no |
| Correlation | `correlation`, `phase_id`, `event_id`, `producer_seq` | yes, as opaque ids; no cross-tenant lookup by guessing | yes | own traces only |
| Owner diagnostics | `location`, `age_ns`, `deadline_ns`, overflow `by_class`, watchdog findings | **no** — aggregate counts only | yes | no |
| Never exported | payloads, paths, URLs, tokens, session or document names, SDP/ICE addresses, stacks | no | no | no |

An item the scope cannot read stays as `{ "source": "<source>", "forbidden": true }` with no body.

## 9. Upload, ingest and query — proposal, not implemented

Aligned with the Cloud plan's proposed v1 so the root can unify them. Field names in the Go
records (`producer_seq`, `event_id`, `boot_id`, `build_commit`) are already the ones below.

**Batch** (`POST /v1/health/ingest`, machine credential over a dedicated HTTPS client, separate
from the relay socket and its writer lock; tenant and machine come from the principal, never the
body):

```json
{
  "schema_version": 1,
  "batch_id": "boot-7f3a-b0001",
  "producer_instance": "daemon",
  "boot_id": "boot-7f3a",
  "events": [
    {
      "schema": "clawdline.connection-health.record/1",
      "producer_seq": 3,
      "event_id": "boot-7f3a-3",
      "boot_id": "boot-7f3a",
      "event": "stall_detected",
      "wall": "2026-10-20T12:00:15.04Z",
      "mono_ns": 15040000000,
      "source": "daemon",
      "stage": "handler_started",
      "class": "read",
      "kind": "finite",
      "location": "cloud.link.read_handler",
      "phase_id": 2,
      "correlation": "tr-01HZX3",
      "age_ns": 15000000000,
      "deadline_ns": 15000000000,
      "count": 1,
      "rule": "watchdog/1"
    }
  ]
}
```

Bounds: 32 KiB per batch; 1 MiB per machine per hour with a reserved 64 KiB and 120 metadata
requests per hour for heartbeats, transitions and loss markers; one batch in flight.

**Receipt** (only after the store committed):

```json
{
  "schema_version": 1,
  "receipt_id": "rcpt-01HZX4",
  "batch_id": "boot-7f3a-b0001",
  "digest": "sha256-<canonical batch digest>",
  "persisted_at": "2026-10-20T12:00:17Z",
  "accepted": 1,
  "rejected": [],
  "coverage": { "first_seq": 3, "last_seq": 3 }
}
```

**Idempotency.** `(tenant, source, boot_id, batch_id)` and `(tenant, source, boot_id, event_id)`
are unique. An exact retry returns the original durable receipt without rewriting it; the same id
with a different canonical digest is `409 idempotency_conflict`; a retry past the dedupe window is
`receipt_expired`, never silently re-accepted. Authority is rechecked on every retry. A producer
that lost the answer resends the same batch — it never mints new ids, and never resends a user
mutation.

**Rejections and failures:** malformed batch → whole batch `400 invalid_field` /
`unsupported_schema`; a valid batch with an unsupported event → indexed `rejected[]` with its
code; quota or store failure → no receipt (not persisted); scope in the body that disagrees with
the principal → `403 forbidden`.

**Query envelope** (operator `/su/v1/health/...`, owner `/v1/health/...`):

```json
{
  "schema_version": 1,
  "generated_at": "2026-10-20T12:01:00Z",
  "as_of": "2026-10-20T12:00:59Z",
  "outcome": "blocked",
  "evidence_quality": "partial",
  "sources": [
    { "source": "daemon", "boot_id": "boot-7f3a", "build_commit": "c9fe745db066",
      "freshness": "fresh", "last_receipt_age_ms": 12000, "expected_every_ms": 30000 },
    { "source": "viewer", "freshness": "no_receipt" }
  ],
  "coverage": { "seq_ranges": [{ "boot_id": "boot-7f3a", "from": 1, "to": 3 }], "gaps": [],
                "retention_floor": "2026-10-19T12:01:00Z", "dropped": 0, "truncated": false },
  "clock_uncertainty_ms": 250,
  "rule_version": "watchdog/1",
  "events": [],
  "next_cursor": null,
  "partial_reason": "viewer_not_collected"
}
```

Bounds: 200 events or 1 MiB per page, 5 s total deadline, raw range ≤ 24 h (wider windows use
minute aggregates). The cursor is bound to scope, filter and window. Stored queries never contact
the machine, relay or account handler. A source with no data is `not_collected` or `unsupported`,
never an empty success; a `null` duration is never drawn as zero.

## 9a. Daemon uploader (implemented)

`internal/adapters/healthupload` posts the durable store's records to Cloud's
`POST <cloud_api_base>/v1/health/ingest` (Cloud contract §2) and nothing else.

**Turning it on.** All three, read from local files on every cycle (30 s), so turning any of them
off stops the next request without a restart:

```json
// <state dir>/config.json; the default state dir is ~/.config/clawdline-next
{ "cloud_enabled": true, "cloud_health_upload": true }
```

`clawdline cloud on` sets `cloud_enabled`. The upload switch is
`clawdline setting set cloud_health_upload on` (and `off`; `get` reads it, absent reads `off`),
written through `/v1/settings` with this machine's local token only — a paired browser cannot
change it. It must be JSON `true`; anything else is off, and nothing turns it on by default.
The machine must also be enrolled. The identity is the machine's
own (`machine-identity-v1.json`), and its `api_base` must equal the settings' `cloud_api_base`
(`adaptercloud.CheckEnvironment`), else `origin_refused`. The base must be `https://`; plain
`http://` to loopback is accepted only when `Options.AllowLoopbackHTTP` is set, which the daemon
never does. Off states make no request at all: `not_opted_in`, `cloud_off`, `no_identity`,
`identity_unreadable`, `origin_refused`, `source_not_open`.

**Wire.** One batch per request, one boot per batch: `schema_version: 1`, `batch_id`
`s<first_seq>-<last_seq>`, `producer_instance: "daemon"`, `boot_id`, `build_commit` from the
store's boot header, `events[]` of record/1. Each event is copied through a fixed struct
(`wireEvent`) with exactly Cloud's keys: `schema, producer_seq, event_id, boot_id, event, wall
(UTC), mono_ns, build_commit, source, stage, class, kind, phase_id, correlation, count, rule,
state`. `location`, `age_ns` and `deadline_ns` have no field there, so they cannot leave. `state`
is set only on `state_after_loss`, the one event where Cloud requires it (`slow`, `blocked` or
`unverified_after_gap`) and refuses it everywhere else. The location codes `sender_lookup`,
`signature_check`, `replay_claim` and `open_envelope` are locations, not stages, and are never
sent in any field.
`sent_at` is omitted on purpose: it is part of Cloud's batch digest, and a batch rebuilt from the
same records after a lost progress file must replay (200), not conflict (409). Cloud therefore
reports `clock_offset_ms: null` for this producer. The bearer is the machine credential in the
`Authorization` header only; redirects are not followed and there is no cookie jar.

**Not sent, counted instead** (`totals.excluded` by reason): a `state_after_loss` whose state is
not one of Cloud's three (`state_unsupported_by_cloud`), an event or stage Cloud does not list, a source other than `daemon`, `mono_ns` or any integer above 2^53−1, an id
or rule that fails Cloud's pattern, and a single record that alone would exceed the body bound.
Durable-store loss markers (`loss` entries) have no producer sequence and cannot be record/1
events; they are counted as `storage_loss_records`, and store gaps by reason as `store_gaps`. Both
stay visible locally only — **wire gap for Cloud**: Cloud has no field for producer-side storage
loss yet. A batch-envelope `local_loss` counter is a proposal awaiting its own receiver; until it is
built the uploader sends none and invents no record to carry the counts, so Cloud reports such a
gap as unexplained. A progress file written before this change may still hold the retired reason
`state_after_loss_unsupported_by_cloud` in its cumulative totals.

**Delivery and retries.** One batch in flight. Its exact body is written to
`<state>/connection-health/upload/upload-state.json` (atomic replace, fsync) before the first
attempt, and each attempt is counted there before it is made. Only a parsed receipt naming this
`batch_id` moves the cursor as delivered (`receipted_events`; `duplicate_events` for Cloud's
`duplicate_event`); a 2xx without one is retried. Committed (the store's fsync), sent, and
receipted are separate counters; nothing here says a person saw anything.

| Cloud answers | The uploader |
| --- | --- |
| 201 / 200 with a receipt for the batch | moves the cursor; keeps the newest 16 receipts |
| network error, timeout (10 s per attempt), 5xx, 2xx without receipt | same body, same `batch_id`, backoff 5 s doubling to 5 min |
| 429 | waits `Retry-After` (≤ 1 h), then the same body |
| 401 / 403 | `auth_refused`; that credential is not used again until the identity changes; the batch waits |
| 400, 409, 413, other 4xx | the batch is counted as refused with Cloud's code and field (never its message) and not resent |
| 6 attempts or 1 h without a receipt | abandoned and counted (`abandoned_events`; Cloud may or may not hold it) |

**Budgets.** At most 120 requests and 960 KiB (1 MiB less Cloud's 64 KiB metadata reserve) per
clock hour, every attempt counted, persisted across restarts; at the bound `quota_local` until the
next hour. Batches are at most 100 events and 32 KiB (a 512 B margin is kept). Idle (nothing new
in the store) is one local identity read and one store page read per 30 s and **no request**
(`TestARestartResendsThePendingBatchAndKeepsTheCursor`). Measured on an Apple M4 Pro: building and
encoding one 97-event, 31.8 KB batch from a 200-entry page costs 206 µs and 333 KB allocated; an
idle cycle 18.5 µs and 35 KB (`go test -bench . ./internal/adapters/healthupload`). At the
hourly cap that is about 25 ms of CPU per hour. Production network cost is not measured.

**Status.** `GET /v1/connection-health/status` carries `upload`: `state`, `last_error`,
`last_refusal {http, code, field}`, `pending {batch_id, events, bytes, attempts, next_attempt,
persisted: no|unknown}`, `hour`, `totals`, `last_receipt`, `receipts_kept`, `state_bytes` (the progress file's size as last read or written, `null` before it is read; it feeds the `cloud.health_upload_state_bytes` capacity row); `clawdline health
status` prints one line of it. Capacity rows `cloud.health_upload_*` measure the hour's spending,
the pending batch and the receipts. Status is a copy under the uploader's own lock: an unreachable
Cloud never makes it, the local query or any business path wait.

**Independence.** The uploader's import graph contains no Cloud line package
(`TestTheUploaderDoesNotDependOnTheCloudLine`): its own `http.Transport` (one connection, 5 s
dial/TLS), its own goroutine, no relay socket, writer lock, spool or lane.

**Not done.** Owner-sealed diagnostics (encryption is the next separate capability; until then the
private fields stay on this machine and no remote investigation is complete); Cloud fields for
storage loss and `state_after_loss`; heartbeat/presence; uploading on a schedule tighter than 30 s.

## 9b. Owner-sealed diagnostic clients (implemented, not wired to a live key)

The private Cloud contract §13 defines the wire. This repository holds both clients:

- Go: `internal/adapters/healthsealed`. `DeriveKey` is HKDF-SHA256 (32 zero-byte salt, info
  `clawdline/health-sealed/v1`), `AAD` is the 13-field canonical JSON, `Seal`/`Open` are
  AES-256-GCM. The plaintext is `clawdline.health-owner-diagnostics/1` (`Snapshot`): a whitelist of
  location, `age_ns`, `deadline_ns`, build, stage, correlation id, observation time, coverage, and
  `truncated`/`lost`. Int64 values are decimal strings; a value that could not be read is `null`,
  never `0`. Each revision's exact body is written atomically to the pending store before the first
  send, so a retry, including one after a restart, sends identical bytes; the receipt's
  `aad_sha256` must equal the client's own or the upload counts as failed. The worker takes
  snapshots through a non-blocking `Offer` (queue 8; full answers `busy`), and a full store
  answers `store_full` at once. Every bound is in `capacity/register_healthsealed.go`.
- PWA: `web/console/src/cloud/health-owner`. It lists `GET /v1/health/sealed`, signs a
  `clawdline.health-sealed-read/1` proof with the registered device key, posts it to
  `POST /v1/health/sealed/read`, and opens the revision only after the AAD, its digest, tenant,
  machine, attachment, revision, epoch, expiry and ciphertext digest all match. Decrypted snapshots
  are held in memory only; a generation counter, bumped on sign-out, account or epoch change,
  keeps a stale fetch or decrypt from repopulating the cache. The panel is the Diagnostics tab of
  the paired machine view, outside `AdminGate`.
- The opt-in is `cloud_health_owner_retention`, off by default and independent of
  `cloud_health_upload`. Off writes nothing and makes no request.

Both sides pin the §13.7 synthetic vector in `internal/adapters/healthsealed/testdata/vector.json`.

**Why nothing can be sealed or opened yet.** No content-key epoch is recorded anywhere in the
product, and the PWA keeps the master secret only as a non-extractable AES-GCM `CryptoKey`
(`legacy/js/net/cloud-crypto.js` `importMasterSecret`), which cannot seed HKDF. Both clients
therefore answer `key_handover_required` rather than guess. Closing it needs the pairing handover
to also keep an HKDF import of the master secret together with its content-key epoch, the device
and identity epochs to be known to the client, and the content-key grant contract. The daemon
does not yet construct the uploader (`cmd/clawdline/health.go`).

**The content-key grant (Cloud §13.9) is not signed by this daemon.** Cloud records a grant only
when the machine's `confirm` on `POST /v1/pairing/identity/phases/confirm` carries a
`handover_attestation`, which the machine may sign only after the viewer's `activate` proved it
holds the master secret of that content-key epoch. This daemon pairs through
`/v1/pairing/invitations/start`, `/poll` and `/v1/pairing/complete`
(`internal/adapters/cloud/pairing.go`), has no identity-phase client, and no viewer sends a key
confirmation in `activate`. Signing without that proof would record a grant Cloud cannot tell
from a true one, so no attestation is sent. Until then a sealed read answers
`403 key_handover_required`, which the panel shows as "pair this device again" and never retries.


## 10. Remaining wiring, in order

1. Done: one runtime per boot, real capacity measures (§1).
2. Done for a handler's answer (§4.1): one correlation from the frame through the lane, handler,
   reservation, seal, spool row and each socket write and re-send. Still uncorrelated: answers
   published outside a handler (status snapshots, terminal frames, carrier answers) and refusals
   sent by `refuseBusy`.
3. Done: local route and CLI (§4.1).
4. Done: durable store opened and fed (`docs/admin-connection-health-storage.md`), and flushed and
   closed at daemon shutdown.
5. Done: the daemon uploader (§9a). Next: Cloud fields for storage loss and `state_after_loss`,
   owner sealing (clients in §9b; key source and daemon wiring still missing),
   stack capture, and M01–M06 on real sources.
6. Done in code, not yet turned on: the browser viewer producer (§11). Next: the hosted page's
   start/stop hookup (§11.4), then `viewer_sse_gap` and the write and direct-carrier paths (§11.3).

## 11. Browser viewer producer (implemented, off by default)

The signed-in hosted console's own evidence: how this page's relay connection, reads and
decryption went, sent as `clawdline.viewer-health.record/1` to Cloud's `POST
/v1/health/viewer-ingest` (Cloud contract §14). Its value is that it does not travel on the relay
it reports on: when the relay socket is down the page still uploads over its own HTTPS `fetch`
with the session cookie (`credentials: "include"`, header `x-clawdline-health: viewer/1`).

### 11.1 Wire

Every enum, pattern and limit is the Cloud parser's (`record.ts`): eight stages, three events,
five outcomes, thirteen error words, the five classes. A record carries only `schema`,
`producer_seq`, `event_id` (`<boot_id>-<seq>`), `boot_id`, `event`, `wall`, `mono_ns`, `stage` and
optionally `class`, `correlation`, `outcome`, `elapsed_ms`, `error`, `count`. `boot_id` (`pg-` + 16
hex) is generated at producer start; each logical attempt gets a fresh random `correlation`
(`vt-` + 12 hex). Neither is derived from a URL, channel, Session, document, message or request id,
and no hook site passes one: the hook API (`viewer-health-hook.js`) takes a stage, a class and an
error word only. An observation the Cloud would refuse (an error word on a non-failed event, an
unknown stage, a non-opaque correlation) is dropped and counted locally (`refusedLocally`), never
sent to cost the batch a 400. `elapsed_ms` and `count` are clamped to the Cloud's ceilings.

### 11.2 Wired sites

| Stage | Where | Events |
| --- | --- | --- |
| `viewer_connect` | `cloud-boot.js` `keepConnected` loop, the loop's first `session.connect()` | started; completed `ok`, failed with the classified error, or completed `aborted` (stopped, pairing, device seat) |
| `viewer_reconnect` | the same loop, every later attempt while no client serves | as above |
| `viewer_carrier_replace` | the same loop, an attempt while a ready client still serves (token renewal, fresh socket after a long hide) | as above |
| `viewer_sse_resume` | `cloud-client.js` `start` → `_becameReady` of a client created with `resumeFrom` | completed `ok` when the predecessor's delivery provably continued (`_continueFrom` taken); failed `sse_resume_refused` when continuity was unproven and rows are asked for again; failed `carrier_closed` if the socket closed first |
| `viewer_request_issued` | `cloud-client.js` `_read`, when the read's waiter is created | started |
| `viewer_response_received` | `cloud-client.js` `_settleRead` | completed `ok`; failed `timeout`, `carrier_closed`, …; an abandoned read is failed `unclassified` with outcome `aborted` |
| `viewer_decrypt_apply` | `_settleRead` success (an answer is only settled opened and applied), and `_receiveEnvelope`'s failure path | completed `ok` under the read's correlation; failed `replay_rejected`, `apply_failed` (probe stage `apply`) or `decrypt_failed` (anything before the payload opened), under its own correlation |

"SSE" in the Cloud's stage names is mapped onto the relay WebSocket the hosted console actually
uses: the hosted console has no EventSource; its stream resume is the successor socket taking over
from its predecessor. Error words come from `classifyViewerError`: a fixed table of the
transport's own codes, anything else `unclassified`, never a guess.

### 11.3 Not wired (stated, not hidden)

- `viewer_sse_gap`: the relay socket has no contiguous per-stream ids for the page to detect a
  skipped range (sequences are per sender across channels), so no record is ever sent. A Cloud
  reader showing no gap rows has **no** evidence that none happened.
- Writes (`_publishAcknowledged`: send, answer, dispatch) and the WebRTC direct carrier
  (`direct-carrier.ts`, `terminal-direct.ts`) record nothing.
- A decrypt or apply failure of an envelope cannot be tied to the read it answered (the read id is
  inside the ciphertext), so it carries its own correlation; the read then shows as a failed
  response (usually `timeout`) under the read's correlation.
- The local console's same-origin daemon and its SSE streams are not Cloud viewers; nothing there
  records.

### 11.4 Turning it on

`startViewerHealth({ enabled: true, apiOrigin, targetMachineId? })` (`install.ts`) starts the page's
one producer and installs it into the hook; any other configuration answers `null` and records
nothing. `handle.setTargetMachine(id)` names the owned machine later batches attribute to.
`handle.stop()` / `stopViewerHealth()` clears the buffer and pending batch, aborts the request in
flight, and uninstalls. **Not hooked up yet:** the hosted page's lifecycle lives in
`CloudGate.tsx`, owned by a concurrent task. The root's hookup is: start it where the Cloud
transport is configured and the session is signed in (behind the page's own explicit setting),
`setTargetMachine` with the Cloud `machines` id of the reader's machine, and `stop()` in the
`revoked`, `sign_in` and transport-leaves-Cloud branches of its `keepConnected` state handler.

### 11.5 Bounds and failure behavior

Rows `console.viewer_health_*` (`register_viewer_health.go`, held to `VIEWER_HEALTH_LIMITS` by
`TestViewerHealthBoundsMatchTheProducer`): body 16 KiB, 50 records per batch, 200 buffered (oldest
dropped and counted), one batch per 60 s at most after a 5 s coalesce, 60 requests and 256 KiB per
clock hour (retries included; at the bound nothing is sent until the hour turns), 4 tries with
5/20/60 s backoff and `Retry-After` honored up to an hour, 10 s per request. One worker: one
timer and at most one request in flight per page.

| Cloud answer | Producer |
| --- | --- |
| 201, 200 (identical replay) | batch done |
| network error, timeout, 429, 5xx (except below) | same body bytes, same `batch_id`, after backoff; dropped and counted after the 4th try |
| 401 | stop and clear: `session_gone` until a new session starts a new producer |
| 403, 503 `health_ingest_disabled` | stop and clear until reload |
| 404 `machine_not_found` | the same records go again with no `target_machine_id` (a new identity, so a new `batch_id`) |
| 413 | split: records back to the buffer front, later batches cut to half |
| 409, 400, other 4xx | batch dropped and counted; only status, `code` and `details.field` are kept |

Records arriving while a batch waits for its retry go to the next batch; the retried body is the
frozen string, never rebuilt. A stop increments a generation so a late answer changes nothing. A
hook or producer exception is swallowed at the hook; the page's connect, read and apply paths
never await the producer.

### 11.6 Measured cost

`node --experimental-strip-types` over the real `viewerRecord`: a typical record is 297 bytes, a
worst-case one 407; 50 typical records are 15,122 bytes (under 16 KiB), 50 worst-case 20,622, so the
body bound cuts those at ~39. Page memory: at most 200 × 407 ≈ 80 KB of records plus one 16 KiB
pending body. Network: at most 256 KiB per hour per page by the hourly bound. Cloud retention is
the Cloud's: 24 h, 4 MiB per device; revoking the device stops new uploads but stored rows stay
until their retention ends (Cloud §14.4). Not measured: real-browser CPU per record, and Cloud
index overhead of viewer rows.
