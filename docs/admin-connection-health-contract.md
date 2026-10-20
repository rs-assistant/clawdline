# Connection health: evidence contract v1

Status: 2026-10-20. The local evidence foundation in `internal/domain/connectionhealth` is
implemented and tested, and the daemon now runs one runtime of it per boot, instruments the relay
transport, the WebSocket writer and the link's lanes, and answers it on a local route and the
`clawdline health` CLI. Everything is still memory only. The Cloud upload, ingest and
query shapes below are a **proposal** to be unified by the root with the Cloud API work; no Cloud
code reads or writes them. Product scope stays as `docs/admin-connection-health.md` states it
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
| Daemon call sites | **partial**: see §4.1 for the exact list. Response sealing and the spool enqueue inside `internal/transport/cloud/relay.go` are **not wired**, and the correlation does not cross the relay queue between the transport's callback and the link's lane | `internal/adapters/cloud/transport.go`, `websocket.go`, `internal/transport/cloud/link.go` |
| Local route `GET /v1/connection-health/{status,events,trace,export}` and `clawdline health` | implemented, this machine's own token only, refused through Cloud | `internal/transport/http/connection_health.go`, `cmd/clawdline/health.go` |
| Per-build location map | **partial**: every wired location code names its Go function (checked by `runtime.FuncForPC` in the owning package's tests); no file lines | `locations.go` |
| Durable segmented on-disk journal, boot markers on disk | **not built**; the ring is memory only and says so (`durability: memory_only_lost_on_restart`) | — |
| Stack capture worker | **not built** | — |
| Cloud ingest, stored queries, relay and viewer producers, uploader, UI | **not built here** (separate tasks) | — |

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
    { "part": "durable_segmented_journal", "status": "unimplemented" },
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

## 10. Remaining wiring, in order

1. Done: one runtime per boot, real capacity measures (§1).
2. Partly done (§4.1). Still to wire: response sealing and spool enqueue in
   `internal/transport/cloud/relay.go`, and carrying one correlation from the transport's inbound
   callback through the relay queue to the link's lane (it needs a field on `Inbound`).
3. Done: local route and CLI (§4.1).
4. Durable segmented journal with boot markers and a documented crash-loss window.
5. Uploader over the proposed ingest, then the Cloud stored queries, relay/viewer producers,
   stage-to-source map per build, stack capture, and M01–M06 on real sources.
