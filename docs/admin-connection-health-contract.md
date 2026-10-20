# Connection health: evidence contract v1

Status: 2026-10-20. The local evidence foundation in `internal/domain/connectionhealth` is
implemented and tested; nothing in the daemon constructs it yet. The Cloud upload, ingest and
query shapes below are a **proposal** to be unified by the root with the Cloud API work; no Cloud
code reads or writes them. Product scope stays as `docs/admin-connection-health.md` states it
(P0–P3, M01–M06); this page is only the wire and evidence contract under it.

## 1. What exists and what does not

| Part | State | Where |
| --- | --- | --- |
| Active-phase table (128 slots, 8 reserved) | implemented | `internal/domain/connectionhealth/active.go` |
| Watchdog (reads atomics and its own clock only) | implemented | `watchdog.go` |
| In-memory journal ring and query page | implemented | `journal.go` |
| Segment truth table, stage progress, local export | implemented | `evidence.go` |
| Capacity rows `cloud.health_*` | registered; measured as a known zero | `internal/domain/capacity/register_connection_health.go`, `internal/transport/http/capacity_measures_connection_health.go` |
| Daemon call sites (frame read, verify, callback, admission, handler, spool, writer lock, socket write) | **not wired** | — |
| Durable segmented on-disk journal, boot markers on disk | **not built**; the ring is memory only and says so (`durability: memory_only_lost_on_restart`) | — |
| Stack capture worker | **not built** | — |
| `clawdline health` CLI and local route | **not built** | — |
| Cloud ingest, stored queries, relay and viewer producers | **not built here** (Cloud API work is separate) | — |
| Per-build stage-to-source location map | **not built**; location codes are stable names only | — |

Every export carries the same list as `wiring[]` (`evidence.go` `Unwired`), so no reader can take
the foundation for the pipeline.

## 2. Identifiers

All strings the package exports are enums, location codes or opaque ids. There is no free-text
field. Validation is at the door (`Tracker.Begin`, `Journal.Append`, `NewJournal`) and a refusal
names the field, never its value.

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
`clock_anomaly`.

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
    "trace_coverage": "complete"
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
    { "part": "daemon_instrumentation_call_sites", "status": "unimplemented" },
    { "part": "durable_segmented_journal", "status": "unimplemented" },
    { "part": "stack_capture_worker", "status": "unimplemented" },
    { "part": "local_health_cli_and_route", "status": "unimplemented" },
    { "part": "cloud_ingest", "status": "unimplemented" },
    { "part": "cloud_query", "status": "unimplemented" },
    { "part": "relay_and_viewer_producers", "status": "unimplemented" },
    { "part": "stage_location_map_per_build", "status": "unimplemented" }
  ]
}
```

(The `wiring` entries are printed one per line here; the pinned output prints each on four.)

### Coverage metadata

| Field | Meaning |
| --- | --- |
| `coverage.quality` | `complete`, or `partial` when records the query covers were evicted |
| `first_retained_seq`, `last_seq` | the ring's retained range when read |
| `gap_from_seq`, `gap_to_seq` | the exact sequence numbers asked for and already evicted |
| `evicted_total`, `refused_total` | lifetime counts of evicted and refused records |
| `truncated`, `next_after_seq` | the page hit a bound; continue with `after_seq = next_after_seq` |
| `durability` | `memory_only_lost_on_restart` until the on-disk journal exists |
| `newest_age_ns` | freshness of the newest retained record |
| `watchdog.trace_coverage` | `partial` once any `Begin` was refused for want of a slot |
| `watchdog.overflow` | refused `Begin`s: total, critical, `by_class`, first and last monotonic offset |
| `watchdog.gap_ns`, `clock_backwards_ns` | the watchdog itself was late, or the clock went backwards |

## 5. Typed failures

`CodeOf(err)` returns one of these; no failure is turned into an empty page.

| Code | When | Caller sees |
| --- | --- | --- |
| `invalid_field` | an enum, id or location fails its rule; `Error.Field` names it | the phase or record is not kept |
| `slots_exhausted` | no free slot | zero `Handle` (every call a no-op); the operation proceeds uninstrumented; counted |
| `record_too_large` | an encoded record over 1 KiB | not kept; `refused_total` +1 |
| `query_bound_exceeded` | `limit` < 0 or > 200 | no page |
| `cursor_other_boot` | `boot_id` in the query is not this journal's boot | no page: the sequence numbers it names do not exist |
| `unsupported` | export asked of a missing watchdog report or journal | no export — never a quiet-looking empty one |
| `unsupported_schema`, `forbidden`, `idempotency_conflict` | reserved for the ingest and query routes below | — |

## 6. Bounds

All are proposals from the plan, registered in `internal/domain/capacity` and lowerable by
`CLAWDLINE_NEXT_CAPACITY`. `TestEveryBoundIsRegistered` was red on all seven constants before the
rows were added.

| Row | Limit | At the limit |
| --- | --- | --- |
| `cloud.health_active_slots` | 128 rows | refuse `Begin`, count by class and interval, coverage partial |
| `cloud.health_reserved_slots` | 8 rows (of the 128) | refuse critical `Begin`, counted as critical |
| `cloud.health_journal_rows` | 4,096 records | evict oldest, count, name the gap |
| `cloud.health_journal_bytes` | 4 MiB | evict oldest, count, name the gap |
| `cloud.health_record_bytes` | 1 KiB per record | refuse, count |
| `cloud.health_query_rows` | 200 per page | stop, `truncated`, cursor |
| `cloud.health_query_bytes` | 1 MiB per page | stop, `truncated`, cursor |

Parameters, not capacities: watchdog cadence `DefaultCadence` 1 s; finite deadlines are per
`Spec` (the plan's proposed 5 s / 15 s); scheduling gap is a scan later than 2 × cadence;
`ErrorRateThreshold` 1 %.

### Measured cost

`go test -bench . -benchtime 2s ./internal/domain/connectionhealth` on an Apple M4 Pro, 14
threads, 2026-10-20:

| Benchmark | Result |
| --- | --- |
| `Begin` + `Progress` + `End`, parallel | 298.6 ns/op, 416 B/op, 2 allocs/op |
| One `Scan` of 120 active phases, journal attached | 22.4 µs/op, 102.6 KB/op, 22 allocs/op |

Nothing else was measured: not daemon CPU or latency with instrumentation enabled, not a 1 s scan
under load, not memory of a full ring.

## 7. Concurrency

- `Tracker`: each slot is an `atomic.Bool` claim and an `atomic.Pointer[Phase]` to an immutable
  snapshot. `Progress` swaps in a copy with CAS; `End` clears it only if it still holds its own
  phase, so a second `End` is harmless. Nothing blocks.
- `Watchdog.Scan` is called from one goroutine (`Run`, whose lifetime is its context). It reads
  only the tracker's atomics and its clock; its only side effect is `Journal.Append`.
- `Journal` has its own mutex, held for a copy and one `json.Marshal` of a ≤ 1 KiB record. No
  business code holds it, and it does no I/O.
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

1. Construct one `Tracker`, `Journal` and `Watchdog` per daemon boot, run `Watchdog.Run` under the
   daemon context, and replace the known-zero capacity measures with `Snapshot` and coverage.
2. Instrument the call sites named in the plan (frame read, sender verify, inbound callback,
   admission, handler, spool, writer-lock acquisition, socket write, receipts), each with a stable
   location code; reserve critical slots for reader, writer, drain and keepalive loops.
3. Local read route and `clawdline health` CLI over `Journal.Read` and `Export`.
4. Durable segmented journal with boot markers and a documented crash-loss window.
5. Uploader over the proposed ingest, then the Cloud stored queries, relay/viewer producers,
   stage-to-source map per build, stack capture, and M01–M06 on real sources.
