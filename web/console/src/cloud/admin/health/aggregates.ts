/* The Cloud's minute-rollup trends, `GET {api}/su/v1/health/aggregates` (private contract
 * docs/connection-health-contract.md §15, Cloud commit 13e7387), read field by field, and the
 * pure mapping from one answer to the trend charts.
 *
 * The rules this module keeps, because the numbers invite the wrong reading:
 * - A bucket the Cloud did not list is unknown (`absent_bucket: "unknown"`), never zero traffic
 *   and never healthy. Time before the retention floor or before `covered_from` (scan cap) is its
 *   own kind of unknown, said so.
 * - Counts are accepted event records by server receipt minute. `count_total` is what the
 *   producer reported (operations for service stage ends, dropped records for loss events) and a
 *   floor whenever a record carried no count. No ratio is computed, least of all for the daemon,
 *   whose denominator is unknown.
 * - Elapsed is each record's own reported time (an aggregate service record's window maximum),
 *   per stage, never summed across stages into an end-to-end time.
 * - Boots are never merged: a bucket whose boot set differs from the previous held bucket is a
 *   restart, and the line is not drawn through it as continuous.
 * A shape past any of the Cloud's own read bounds is refused (`contract_bound`), never cut.
 * Nothing is imported at run time except stored.ts, so node's strip-types tests load it as it is. */
import type { Envelope } from "./stored.ts"
// @ts-expect-error -- a `.ts` path, so node's strip-types tests can import this module.
import { parseEnvelope, StoredContractError, storedReaders } from "./stored.ts"

const { shape, obj, id, idOrNull, instant, nat, natOrNull, bool, oneOf, list } = storedReaders as typeof import("./stored").storedReaders

export const AGGREGATE_RULE = "minute-rollup/1"

/** The Cloud's rollup read bounds (§15.3): refused past them, never cut. */
export const AGGREGATE_BOUNDS = {
  series: 50,
  /** Points per series: the widest window per bucket holds at most 360. */
  points: 360,
  /** Cells per point: the hour document's own cell cap. */
  cells: 64,
  bootIds: 8,
  /** Refusal rows: the Cloud's page row bound. */
  refusals: 200,
  /** Elapsed histogram bounds (11 finite) and buckets (+1 for past the last bound). */
  elapsedBounds: 11,
} as const

/** Trend ranges the page offers, each with the only bucket the Cloud allows for that span. */
export const TREND_RANGES = {
  "6h": { bucket: "1m", bucketMs: 60_000, spanMs: 6 * 3_600_000 },
  "24h": { bucket: "5m", bucketMs: 300_000, spanMs: 24 * 3_600_000 },
  "7d": { bucket: "1h", bucketMs: 3_600_000, spanMs: 7 * 86_400_000 },
  "30d": { bucket: "1d", bucketMs: 86_400_000, spanMs: 30 * 86_400_000 },
} as const
export type TrendRange = keyof typeof TREND_RANGES
export const TREND_RANGE_KEYS = Object.keys(TREND_RANGES) as TrendRange[]
export const DEFAULT_TREND: TrendRange = "30d"

const BUCKETS = ["1m", "5m", "1h", "1d"] as const
const BUCKET_MS: Record<(typeof BUCKETS)[number], number> = { "1m": 60_000, "5m": 300_000, "1h": 3_600_000, "1d": 86_400_000 }
/** `external_probe`: service-scope probe outcomes (Cloud cb153af); their denominator stays unknown. */
const SOURCES = ["daemon", "cloud_api", "relay", "viewer", "collector", "external_probe"] as const
export const DENOMINATORS = ["unknown", "service_reported_operations", "viewer_reported_stage_ends"] as const
export type Denominator = (typeof DENOMINATORS)[number]
/** Service, viewer and external-probe outcomes (`http_error`, `network_error`, `skipped`: contract §17.2). */
const CELL_OUTCOMES = ["ok", "error", "timeout", "refused", "zero_target", "threw", "pending", "aborted", "http_error", "network_error", "skipped"] as const
export const VIEWER_ERRORS = [
  "network_unreachable", "http_4xx", "http_5xx", "auth_refused", "auth_revoked", "timeout", "sse_gap", "sse_resume_refused",
  "carrier_closed", "decrypt_failed", "apply_failed", "replay_rejected", "unclassified",
] as const
const PARTIAL_REASONS = ["scan_cap", "page_bytes", "row_limit", "rollup_refused", "cell_cap", "retention_floor", "producer_reported_loss"] as const
/** The fixed meanings §15.2 states; a server saying anything else means something this page does not draw. */
const SEMANTICS = {
  counts: "accepted_event_records",
  distinct_operations: "not_aggregated",
  elapsed: "per_record_reported_elapsed_not_end_to_end",
  boots: "never_merged_in_storage",
  clock: "server_received_at_minute",
} as const
const ENUM_WORD = /^[a-z][a-z0-9_]{0,63}$/
const BOOT = /^[A-Za-z0-9_-]{1,64}$/
const ACCOUNT = /^[A-Za-z0-9_:-]{1,128}$/

export type AggElapsed = { records: number; max_ms: number | null; bounds_ms: number[]; buckets: number[] }
export type AggCell = {
  event: string
  stage: string | null
  class: string | null
  outcome: string | null
  error: string | null
  records: number
  /** What producers reported: operations for service stage ends, dropped records for loss events. */
  count_total: number
  /** A record without `count` makes the sum a floor ("at least"), never an exact total. */
  count_is_floor: boolean
  records_without_count: number
  elapsed: AggElapsed | null
}
export type AggPoint = {
  start: string
  boots: number
  boot_ids: string[]
  boot_ids_truncated: boolean
  batches: number
  records: number
  duplicates_refused: number
  producer_wall_behind: number
  producer_wall_ahead: number
  loss_reported: number
  cells_dropped: number
  cells: AggCell[]
}
export type AggSeries = {
  account: string | null
  source_id: string
  machine_id: string
  source: (typeof SOURCES)[number]
  producer_instance: string
  denominator: Denominator
  producer_reported_loss: { storage_lost_records: number; store_gaps: number; excluded: number; counters: "monotonic" | "unverified" }
  points: AggPoint[]
}
export type AggRefusal = { account: string | null; day: string; reason: string; batches: number }
export type AggregatesBody = Omit<Envelope, "window"> & {
  window: { from: string; to: string; bucket: (typeof BUCKETS)[number]; bucket_ms: number }
  capability: { minute: "collected" | "unsupported"; retention_days: number | null }
  rollup: {
    retention_floor: string
    retention_clipped: boolean
    covered_from: string
    refusals: AggRefusal[]
    refusals_truncated: boolean
    cells_dropped: number
    cell_filter: "none" | "applied"
  }
  series: AggSeries[]
  series_total: number
}

function elapsed(v: unknown, path: string): AggElapsed | null {
  if (v === null || v === undefined) return null
  const o = obj(v, path)
  const bounds = list(o.bounds_ms, `${path}.bounds_ms`, AGGREGATE_BOUNDS.elapsedBounds, (x, p) => nat(x, p))
  const buckets = list(o.buckets, `${path}.buckets`, AGGREGATE_BOUNDS.elapsedBounds + 1, (x, p) => nat(x, p))
  if (buckets.length !== bounds.length + 1 && buckets.length !== bounds.length) shape(`${path}.buckets`)
  for (let i = 1; i < bounds.length; i++) if (bounds[i] <= bounds[i - 1]) shape(`${path}.bounds_ms`)
  if (o.basis !== undefined && o.basis !== "per_record_reported_elapsed") shape(`${path}.basis`)
  return { records: nat(o.records, `${path}.records`), max_ms: natOrNull(o.max_ms, `${path}.max_ms`), bounds_ms: bounds, buckets }
}

function cell(v: unknown, path: string): AggCell {
  const o = obj(v, path)
  const records = nat(o.records, `${path}.records`)
  const without = nat(o.records_without_count, `${path}.records_without_count`)
  const floor = bool(o.count_is_floor, `${path}.count_is_floor`)
  // The floor flag is exactly "some record had no count"; disagreement is a server we do not read.
  if (floor !== without > 0 || without > records) shape(`${path}.count_is_floor`)
  return {
    event: id(o.event, `${path}.event`, ENUM_WORD),
    stage: idOrNull(o.stage, `${path}.stage`, ENUM_WORD),
    class: idOrNull(o.class, `${path}.class`, ENUM_WORD),
    outcome: o.outcome === null || o.outcome === undefined ? null : oneOf(o.outcome, CELL_OUTCOMES, `${path}.outcome`),
    error: o.error === null || o.error === undefined ? null : oneOf(o.error, VIEWER_ERRORS, `${path}.error`),
    records,
    count_total: nat(o.count_total, `${path}.count_total`),
    count_is_floor: floor,
    records_without_count: without,
    elapsed: elapsed(o.elapsed, `${path}.elapsed`),
  }
}

function point(v: unknown, path: string): AggPoint {
  const o = obj(v, path)
  const bootIds = list(o.boot_ids, `${path}.boot_ids`, AGGREGATE_BOUNDS.bootIds, (x, p) => id(x, p, BOOT))
  const boots = nat(o.boots, `${path}.boots`)
  const truncated = bool(o.boot_ids_truncated, `${path}.boot_ids_truncated`)
  if (boots < bootIds.length || truncated !== boots > bootIds.length) shape(`${path}.boots`)
  return {
    start: instant(o.start, `${path}.start`),
    boots, boot_ids: bootIds, boot_ids_truncated: truncated,
    batches: nat(o.batches, `${path}.batches`),
    records: nat(o.records, `${path}.records`),
    duplicates_refused: nat(o.duplicates_refused, `${path}.duplicates_refused`),
    producer_wall_behind: nat(o.producer_wall_behind, `${path}.producer_wall_behind`),
    producer_wall_ahead: nat(o.producer_wall_ahead, `${path}.producer_wall_ahead`),
    loss_reported: nat(o.loss_reported, `${path}.loss_reported`),
    cells_dropped: nat(o.cells_dropped, `${path}.cells_dropped`),
    cells: list(o.cells, `${path}.cells`, AGGREGATE_BOUNDS.cells, cell),
  }
}

function series(v: unknown, path: string, win: { from: number; to: number; ms: number }): AggSeries {
  const o = obj(v, path)
  const loss = obj(o.producer_reported_loss, `${path}.producer_reported_loss`)
  const points = list(o.points, `${path}.points`, AGGREGATE_BOUNDS.points, point)
  let last = -Infinity
  points.forEach((p, i) => {
    const at = Date.parse(p.start)
    // Points are bucket-aligned, inside the window and strictly ascending, or they cannot be placed.
    if (at <= last || at < win.from || at >= win.to || (at - win.from) % win.ms !== 0) shape(`${path}.points[${i}].start`)
    last = at
  })
  return {
    account: idOrNull(o.account, `${path}.account`, ACCOUNT),
    source_id: id(o.source_id, `${path}.source_id`),
    machine_id: id(o.machine_id, `${path}.machine_id`),
    source: oneOf(o.source, SOURCES, `${path}.source`),
    producer_instance: id(o.producer_instance, `${path}.producer_instance`, BOOT),
    denominator: oneOf(o.denominator, DENOMINATORS, `${path}.denominator`),
    producer_reported_loss: {
      storage_lost_records: nat(loss.storage_lost_records, `${path}.producer_reported_loss.storage_lost_records`),
      store_gaps: nat(loss.store_gaps, `${path}.producer_reported_loss.store_gaps`),
      excluded: nat(loss.excluded, `${path}.producer_reported_loss.excluded`),
      counters: oneOf(loss.counters, ["monotonic", "unverified"] as const, `${path}.producer_reported_loss.counters`),
    },
    points,
  }
}

/** One aggregates answer. A rule, meaning or bound this page does not read is refused whole. */
export function parseAggregates(body: unknown): AggregatesBody {
  const env = parseEnvelope(body)
  const o = body as Record<string, unknown>
  if (o.rule_version !== AGGREGATE_RULE) throw new StoredContractError("contract_schema", "$.rule_version")
  const sem = obj(o.semantics, "$.semantics")
  for (const [k, want] of Object.entries(SEMANTICS)) if (sem[k] !== want) throw new StoredContractError("contract_schema", `$.semantics.${k}`)
  const w = obj(o.window, "$.window")
  const bucket = oneOf(w.bucket, BUCKETS, "$.window.bucket")
  if (w.bucket_ms !== BUCKET_MS[bucket]) shape("$.window.bucket_ms")
  if (w.basis !== "server_received_at") shape("$.window.basis")
  const from = instant(w.from, "$.window.from")
  const to = instant(w.to, "$.window.to")
  const span = Date.parse(to) - Date.parse(from)
  if (span <= 0 || span % BUCKET_MS[bucket] !== 0) shape("$.window")
  if (span / BUCKET_MS[bucket] > AGGREGATE_BOUNDS.points) throw new StoredContractError("contract_bound", "$.window")
  const c = obj(o.coverage, "$.coverage")
  if (c.absent_bucket !== "unknown") throw new StoredContractError("contract_schema", "$.coverage.absent_bucket")
  const ref = obj(c.refusals, "$.coverage.refusals")
  const cap = obj(o.aggregates, "$.aggregates")
  const minute = oneOf(cap.minute, ["collected", "unsupported"] as const, "$.aggregates.minute")
  if (env.partial_reason !== null) oneOf(env.partial_reason, PARTIAL_REASONS, "$.partial_reason")
  const win = { from: Date.parse(from), to: Date.parse(to), ms: BUCKET_MS[bucket] }
  const rows = list(o.series, "$.series", AGGREGATE_BOUNDS.series, (x, p) => series(x, p, win))
  const total = nat(o.series_total, "$.series_total")
  if (total < rows.length) shape("$.series_total")
  return {
    ...env,
    window: { from, to, bucket, bucket_ms: BUCKET_MS[bucket] },
    capability: { minute, retention_days: natOrNull(cap.retention_days, "$.aggregates.retention_days") },
    rollup: {
      retention_floor: instant(c.retention_floor, "$.coverage.retention_floor"),
      retention_clipped: bool(c.retention_clipped, "$.coverage.retention_clipped"),
      covered_from: instant(c.covered_from, "$.coverage.covered_from"),
      refusals: list(ref.rows, "$.coverage.refusals.rows", AGGREGATE_BOUNDS.refusals, (x, p) => {
        const r = obj(x, p)
        return { account: idOrNull(r.account, `${p}.account`, ACCOUNT), day: instant(r.day, `${p}.day`), reason: id(r.reason, `${p}.reason`, ENUM_WORD), batches: nat(r.batches, `${p}.batches`) }
      }),
      refusals_truncated: bool(ref.truncated, "$.coverage.refusals.truncated"),
      cells_dropped: nat(c.cells_dropped, "$.coverage.cells_dropped"),
      cell_filter: oneOf(c.cell_filter, ["none", "applied"] as const, "$.coverage.cell_filter"),
    },
    series: rows,
    series_total: total,
  }
}

/* ---- the query ------------------------------------------------------------- */

/** The bucket-aligned window for a range ending at `end`: exactly the Cloud's widest for that bucket,
 *  so its floor/ceil quantization can never widen it past `window_too_wide`. */
export function trendWindow(range: TrendRange, end: number): { from: string; to: string; bucket: string } {
  const r = TREND_RANGES[range]
  const to = Math.ceil(end / r.bucketMs) * r.bucketMs
  return { from: new Date(to - r.spanMs).toISOString(), to: new Date(to).toISOString(), bucket: r.bucket }
}

/* ---- the chart model ------------------------------------------------------- */

/** Why a slot holds no figure. Each is unknown; none is zero. */
export type SlotGap = "absent" | "before_retention" | "before_covered" | "after_as_of"
export type SlotSignal = "stall" | "slow" | "failed" | "loss"
export type Slot = {
  start: number
  end: number
  gap: SlotGap | null
  /** Accepted records in this bucket; null when the bucket is unknown. */
  records: number | null
  /** Reported operations (service/viewer stage ends); `floor` when a record carried no count. */
  operations: { n: number; floor: boolean } | null
  failed: number
  signals: SlotSignal[]
  /** The boot set differs from the previous held bucket, or several boots share it: not continuous. */
  restart: boolean
  /** Held, but older than `covered_from` (the scan cap): the figures are a floor, not the bucket's total. */
  incomplete: boolean
  boots: number
  wallSkew: number
  duplicates: number
  loss: number
  cellsDropped: number
}
export type TrendLane = { key: string; series: AggSeries; slots: Slot[]; max: number; totals: { records: number; failed: number; operations: { n: number; floor: boolean } | null; heldBuckets: number; unknownBuckets: number } }

const STAGE_END = new Set(["stage_completed", "stage_failed", "viewer_stage_completed", "viewer_stage_failed"])
const FAILED_EVENT = new Set(["stage_failed", "viewer_stage_failed"])
// A skipped probe check is not a failure: it never ran (contract §17.4).
const FAILED_OUTCOME = new Set(["error", "timeout", "refused", "threw", "aborted", "http_error", "network_error"])

function signalsOf(p: AggPoint): { signals: SlotSignal[]; failed: number; ops: { n: number; floor: boolean } | null } {
  const s = new Set<SlotSignal>()
  let failed = 0
  let opsN = 0
  let opsFloor = false
  let opsSeen = false
  for (const c of p.cells) {
    if (c.event === "stall_detected") s.add("stall")
    if (c.event === "slow_detected") s.add("slow")
    if (c.event === "transitions_lost" || c.event === "overflow") s.add("loss")
    if (FAILED_EVENT.has(c.event) || (c.outcome !== null && FAILED_OUTCOME.has(c.outcome))) { s.add("failed"); failed += c.records }
    if (STAGE_END.has(c.event)) { opsSeen = true; opsN += c.count_total; opsFloor ||= c.count_is_floor }
  }
  if (p.loss_reported > 0) s.add("loss")
  return { signals: [...s], failed, ops: opsSeen ? { n: opsN, floor: opsFloor } : null }
}

/** One lane per series, one slot per bucket of the window; unknown buckets keep their reason. */
export function trendLanes(body: AggregatesBody): TrendLane[] {
  const from = Date.parse(body.window.from)
  const to = Date.parse(body.window.to)
  const ms = body.window.bucket_ms
  const floor = body.rollup.retention_clipped ? Date.parse(body.rollup.retention_floor) : -Infinity
  const covered = Date.parse(body.rollup.covered_from)
  const asOf = Date.parse(body.as_of)
  return body.series.map((series) => {
    const byStart = new Map(series.points.map((p) => [Date.parse(p.start), p]))
    const slots: Slot[] = []
    let prevBoots: string | null = null
    let records = 0
    let failedTotal = 0
    let opsN = 0
    let opsFloor = false
    let opsSeen = false
    let held = 0
    for (let start = from; start < to; start += ms) {
      const end = start + ms
      const p = byStart.get(start)
      const base = { start, end, records: null, operations: null, failed: 0, signals: [], restart: false, incomplete: false, boots: 0, wallSkew: 0, duplicates: 0, loss: 0, cellsDropped: 0 }
      if (!p) {
        const gap: SlotGap = end <= floor ? "before_retention" : end <= covered ? "before_covered" : start > asOf ? "after_as_of" : "absent"
        slots.push({ ...base, gap })
        continue
      }
      const sig = signalsOf(p)
      const bootKey = p.boot_ids.join(",")
      const restart = p.boots > 1 || (prevBoots !== null && bootKey !== prevBoots)
      prevBoots = bootKey
      held++
      records += p.records
      failedTotal += sig.failed
      if (sig.ops) { opsSeen = true; opsN += sig.ops.n; opsFloor ||= sig.ops.floor }
      slots.push({
        ...base, gap: null, records: p.records, operations: sig.ops, failed: sig.failed, signals: sig.signals, restart, incomplete: start < covered, boots: p.boots,
        wallSkew: p.producer_wall_behind + p.producer_wall_ahead, duplicates: p.duplicates_refused, loss: p.loss_reported, cellsDropped: p.cells_dropped,
      })
    }
    const max = Math.max(1, ...slots.map((s) => s.records ?? 0))
    return {
      key: `${series.account ?? ""}|${series.source_id}`, series, slots, max,
      totals: { records, failed: failedTotal, operations: series.denominator === "unknown" || !opsSeen ? null : { n: opsN, floor: opsFloor }, heldBuckets: held, unknownBuckets: slots.length - held },
    }
  })
}

/** Viewer-reported failures by the viewer's own error enum, over every held bucket. */
export function viewerErrors(body: AggregatesBody): { error: string; records: number; reported: number; floor: boolean }[] {
  const by = new Map<string, { records: number; reported: number; floor: boolean }>()
  for (const s of body.series) {
    if (s.source !== "viewer") continue
    for (const p of s.points) for (const c of p.cells) {
      if (c.error === null) continue
      const row = by.get(c.error) ?? { records: 0, reported: 0, floor: false }
      row.records += c.records
      row.reported += c.count_total
      row.floor ||= c.count_is_floor
      by.set(c.error, row)
    }
  }
  return [...by.entries()].map(([error, r]) => ({ error, ...r })).sort((a, b) => b.records - a.records || a.error.localeCompare(b.error))
}

/** Each source × stage's own reported elapsed: the largest, and the per-record histogram. Never summed. */
export type StageElapsed = { source: string; stage: string; records: number; max_ms: number | null; bounds_ms: number[]; buckets: number[] }
export function stageElapsed(body: AggregatesBody): StageElapsed[] {
  const by = new Map<string, StageElapsed>()
  for (const s of body.series) for (const p of s.points) for (const c of p.cells) {
    if (c.elapsed === null || c.stage === null) continue
    const key = `${s.source}|${c.stage}`
    let row = by.get(key)
    if (!row) {
      row = { source: s.source, stage: c.stage, records: 0, max_ms: null, bounds_ms: c.elapsed.bounds_ms, buckets: c.elapsed.buckets.map(() => 0) }
      by.set(key, row)
    }
    row.records += c.elapsed.records
    if (c.elapsed.max_ms !== null) row.max_ms = row.max_ms === null ? c.elapsed.max_ms : Math.max(row.max_ms, c.elapsed.max_ms)
    c.elapsed.buckets.forEach((n, i) => { row!.buckets[i] = (row!.buckets[i] ?? 0) + n })
  }
  return [...by.values()].sort((a, b) => a.source.localeCompare(b.source) || a.stage.localeCompare(b.stage))
}
