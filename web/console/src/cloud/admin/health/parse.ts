/* Reads an unknown response into a HealthSnapshot, field for field, or refuses it
 * with a typed code. Nothing is defaulted: a missing number is a refusal, not a
 * zero, and a list longer than its bound is refused rather than silently cut. */
import type {
  Carrier, Component, DependencyWait, Evidence, Gap, HealthSnapshot, Holder, Hypothesis, Incident, Lane, LaneSpan,
  LaneTick, Latency, LocationCode, Machine, Missing, OpenSpan, Probe, Queue, Ratio, RelayStage, Replacement,
  Saturation, SegmentRow, Services, SourceStatus, Trace, TraceStage,
} from "./types.ts"
// @ts-expect-error -- a `.ts` path, so node's strip-types tests can import this module.
import { BASES, CERTAINTIES, FRESHNESS, GAP_REASONS, HEALTH_SCHEMA, INCIDENT_STATES, OUTCOMES, PROBE_RESULTS, QUALITIES, RECOVERY, SCOPES, SEGMENTS, SOURCE_KINDS, SPAN_STATES, STAGES, STAGE_STATES } from "./types.ts"

/** List bounds the page renders. Proposed; the root registers them with the capacity contract. */
export const HEALTH_BOUNDS = {
  sources: 16,
  lanes: 12,
  spansPerLane: 480,
  ticksPerLane: 120,
  segments: 12,
  incidents: 50,
  itemsPerIncident: 100,
  traces: 50,
  stagesPerTrace: 16,
  spansPerTrace: 32,
  locationsPerTrace: 32,
  machines: 20,
  carriers: 64,
  replacements: 64,
  holders: 16,
  components: 16,
  serviceRows: 32,
  probeEdges: 16,
  text: 2000,
} as const

export type HealthContractCode = "contract_schema" | "contract_shape" | "contract_bound"

export class HealthContractError extends Error {
  readonly code: HealthContractCode
  readonly path: string
  constructor(code: HealthContractCode, path: string) {
    super(`${code} at ${path}`)
    this.code = code
    this.path = path
  }
}

type Obj = Record<string, unknown>

const shape = (path: string): never => {
  throw new HealthContractError("contract_shape", path)
}

function obj(v: unknown, path: string): Obj {
  return typeof v === "object" && v !== null && !Array.isArray(v) ? (v as Obj) : shape(path)
}

function str(o: Obj, key: string, path: string): string {
  const v = o[key]
  if (typeof v !== "string" || v.length > HEALTH_BOUNDS.text) return shape(`${path}.${key}`)
  return v
}

function strOrNull(o: Obj, key: string, path: string): string | null {
  return o[key] === null ? null : str(o, key, path)
}

function time(o: Obj, key: string, path: string): string {
  const v = str(o, key, path)
  return Number.isFinite(Date.parse(v)) ? v : shape(`${path}.${key}`)
}

function timeOrNull(o: Obj, key: string, path: string): string | null {
  return o[key] === null ? null : time(o, key, path)
}

function num(o: Obj, key: string, path: string): number {
  const v = o[key]
  return typeof v === "number" && Number.isFinite(v) && v >= 0 ? v : shape(`${path}.${key}`)
}

function numOrNull(o: Obj, key: string, path: string): number | null {
  return o[key] === null ? null : num(o, key, path)
}

function bool(o: Obj, key: string, path: string): boolean {
  const v = o[key]
  return typeof v === "boolean" ? v : shape(`${path}.${key}`)
}

function oneOf<T extends string>(o: Obj, key: string, path: string, allowed: readonly T[]): T {
  const v = o[key]
  return typeof v === "string" && (allowed as readonly string[]).includes(v) ? (v as T) : shape(`${path}.${key}`)
}

function list<T>(o: Obj, key: string, path: string, bound: number, each: (v: unknown, path: string) => T): T[] {
  const v = o[key]
  if (!Array.isArray(v)) return shape(`${path}.${key}`)
  if (v.length > bound) throw new HealthContractError("contract_bound", `${path}.${key}`)
  return v.map((item, i) => each(item, `${path}.${key}[${i}]`))
}

function listOrNull<T>(o: Obj, key: string, path: string, bound: number, each: (v: unknown, path: string) => T): T[] | null {
  return o[key] === null ? null : list(o, key, path, bound, each)
}

function ratio(v: unknown, path: string): Ratio | null {
  if (v === null) return null
  const o = obj(v, path)
  const r = { count: num(o, "count", path), denominator: num(o, "denominator", path) }
  return r.count > r.denominator ? shape(`${path}.count`) : r
}

function latency(v: unknown, path: string): Latency | null {
  if (v === null) return null
  const o = obj(v, path)
  return { p50_ms: numOrNull(o, "p50_ms", path), p95_ms: numOrNull(o, "p95_ms", path), p99_ms: numOrNull(o, "p99_ms", path), samples: num(o, "samples", path) }
}

function queue(v: unknown, path: string): Queue | null {
  if (v === null) return null
  const o = obj(v, path)
  return { used: numOrNull(o, "used", path), limit: numOrNull(o, "limit", path), oldest_s: numOrNull(o, "oldest_s", path), as_of: timeOrNull(o, "as_of", path) }
}

function gap(v: unknown, path: string): Gap {
  const o = obj(v, path)
  return { start: time(o, "start", path), end: timeOrNull(o, "end", path), reason: oneOf(o, "reason", path, GAP_REASONS) }
}

function source(v: unknown, path: string): SourceStatus {
  const o = obj(v, path)
  return {
    id: str(o, "id", path),
    kind: oneOf(o, "kind", path, SOURCE_KINDS),
    state: oneOf(o, "state", path, FRESHNESS),
    last_receipt_at: timeOrNull(o, "last_receipt_at", path),
    expected_every_s: numOrNull(o, "expected_every_s", path),
    retention_s: numOrNull(o, "retention_s", path),
    retention_floor_at: timeOrNull(o, "retention_floor_at", path),
    gaps: list(o, "gaps", path, HEALTH_BOUNDS.spansPerLane, gap),
    dropped: numOrNull(o, "dropped", path),
    schema: strOrNull(o, "schema", path),
  }
}

function span(v: unknown, path: string): LaneSpan {
  const o = obj(v, path)
  const s = { start: time(o, "start", path), end: time(o, "end", path), state: oneOf(o, "state", path, SPAN_STATES) }
  return Date.parse(s.end) < Date.parse(s.start) ? shape(`${path}.end`) : s
}

function tick(v: unknown, path: string): LaneTick {
  const o = obj(v, path)
  return { at: time(o, "at", path), label: str(o, "label", path) }
}

function lane(v: unknown, path: string): Lane {
  const o = obj(v, path)
  return {
    segment: oneOf(o, "segment", path, SEGMENTS),
    spans: list(o, "spans", path, HEALTH_BOUNDS.spansPerLane, span),
    ticks: list(o, "ticks", path, HEALTH_BOUNDS.ticksPerLane, tick),
  }
}

function segmentRow(v: unknown, path: string): SegmentRow {
  const o = obj(v, path)
  return {
    segment: oneOf(o, "segment", path, SEGMENTS),
    outcome: oneOf(o, "outcome", path, OUTCOMES),
    quality: oneOf(o, "quality", path, QUALITIES),
    latency: latency(o.latency, `${path}.latency`),
    errors: ratio(o.errors, `${path}.errors`),
    source_age_s: numOrNull(o, "source_age_s", path),
  }
}

function evidence(v: unknown, path: string): Evidence {
  const o = obj(v, path)
  const forbidden = bool(o, "forbidden", path)
  const statement = strOrNull(o, "statement", path)
  // A forbidden item carries no body; one that does was not filtered by its source.
  if (forbidden && statement !== null) shape(`${path}.statement`)
  return { id: str(o, "id", path), source: str(o, "source", path), statement, observed_at: timeOrNull(o, "observed_at", path), forbidden }
}

function hypothesis(v: unknown, path: string): Hypothesis {
  const o = obj(v, path)
  const forbidden = bool(o, "forbidden", path)
  const statement = strOrNull(o, "statement", path)
  if (forbidden && statement !== null) shape(`${path}.statement`)
  return { id: str(o, "id", path), statement, basis: oneOf(o, "basis", path, BASES), sources: list(o, "sources", path, 16, (s, p) => (typeof s === "string" ? s : shape(p))), forbidden }
}

function missing(v: unknown, path: string): Missing {
  const o = obj(v, path)
  return { id: str(o, "id", path), what: str(o, "what", path), next_step: strOrNull(o, "next_step", path) }
}

function incident(v: unknown, path: string): Incident {
  const o = obj(v, path)
  const affected = o.affected === null ? null : (() => {
    const a = obj(o.affected, `${path}.affected`)
    return { count: num(a, "count", `${path}.affected`), lower_bound: bool(a, "lower_bound", `${path}.affected`) }
  })()
  const n = HEALTH_BOUNDS.itemsPerIncident
  return {
    id: str(o, "id", path),
    title: str(o, "title", path),
    state: oneOf(o, "state", path, INCIDENT_STATES),
    outcome: oneOf(o, "outcome", path, OUTCOMES),
    segment: oneOf(o, "segment", path, SEGMENTS),
    started_at: time(o, "started_at", path),
    ended_at: timeOrNull(o, "ended_at", path),
    certainty: oneOf(o, "certainty", path, CERTAINTIES),
    recovery: oneOf(o, "recovery", path, RECOVERY),
    affected,
    build: strOrNull(o, "build", path),
    trace_id: strOrNull(o, "trace_id", path),
    rule_version: strOrNull(o, "rule_version", path),
    facts: list(o, "facts", path, n, evidence),
    hypotheses: list(o, "hypotheses", path, n, hypothesis),
    missing: list(o, "missing", path, n, missing),
  }
}

function traceStage(v: unknown, path: string): TraceStage {
  const o = obj(v, path)
  const s = { stage: oneOf(o, "stage", path, STAGES), state: oneOf(o, "state", path, STAGE_STATES), at: timeOrNull(o, "at", path), recorder: strOrNull(o, "recorder", path), late: bool(o, "late", path) }
  // Only a receipt carries a time; a missing or hidden stage never gets one.
  if ((s.state === "received") !== (s.at !== null)) shape(`${path}.at`)
  return s
}

function openSpan(v: unknown, path: string): OpenSpan {
  const o = obj(v, path)
  return { stage: oneOf(o, "stage", path, STAGES), holder: str(o, "holder", path), started_at: time(o, "started_at", path), last_progress_at: timeOrNull(o, "last_progress_at", path), deadline_ms: numOrNull(o, "deadline_ms", path) }
}

function location(v: unknown, path: string): LocationCode {
  const o = obj(v, path)
  return { code: str(o, "code", path), path: strOrNull(o, "path", path), line: numOrNull(o, "line", path), symbol: strOrNull(o, "symbol", path), note: strOrNull(o, "note", path) }
}

function trace(v: unknown, path: string): Trace {
  const o = obj(v, path)
  return {
    id: str(o, "id", path),
    incident_id: strOrNull(o, "incident_id", path),
    operation: str(o, "operation", path),
    attempts: num(o, "attempts", path),
    build: strOrNull(o, "build", path),
    stages: list(o, "stages", path, HEALTH_BOUNDS.stagesPerTrace, traceStage),
    open_spans: list(o, "open_spans", path, HEALTH_BOUNDS.spansPerTrace, openSpan),
    locations: list(o, "locations", path, HEALTH_BOUNDS.locationsPerTrace, location),
  }
}

function carrier(v: unknown, path: string): Carrier {
  const o = obj(v, path)
  return {
    viewer: str(o, "viewer", path), read_carrier: str(o, "read_carrier", path), command_carrier: str(o, "command_carrier", path),
    state: oneOf(o, "state", path, OUTCOMES), generation: numOrNull(o, "generation", path), last_seen_at: timeOrNull(o, "last_seen_at", path),
    replaced: numOrNull(o, "replaced", path), refused: numOrNull(o, "refused", path),
  }
}

function replacement(v: unknown, path: string): Replacement {
  const o = obj(v, path)
  return { at: time(o, "at", path), viewer: str(o, "viewer", path), from_generation: numOrNull(o, "from_generation", path), to_generation: numOrNull(o, "to_generation", path), reason: strOrNull(o, "reason", path) }
}

function holder(v: unknown, path: string): Holder {
  const o = obj(v, path)
  return { stage: str(o, "stage", path), holder: strOrNull(o, "holder", path), age_s: numOrNull(o, "age_s", path), deadline_s: numOrNull(o, "deadline_s", path), last_progress_at: timeOrNull(o, "last_progress_at", path), as_of: timeOrNull(o, "as_of", path) }
}

function machine(v: unknown, path: string): Machine {
  const o = obj(v, path)
  return {
    id: str(o, "id", path),
    presence: oneOf(o, "presence", path, ["renewed", "expired", "unknown"] as const),
    presence_at: timeOrNull(o, "presence_at", path),
    boot_id: strOrNull(o, "boot_id", path),
    boot_at: timeOrNull(o, "boot_at", path),
    build: strOrNull(o, "build", path),
    platform: strOrNull(o, "platform", path),
    snapshot_at: timeOrNull(o, "snapshot_at", path),
    lanes: list(o, "lanes", path, HEALTH_BOUNDS.lanes, lane),
    carriers: listOrNull(o, "carriers", path, HEALTH_BOUNDS.carriers, carrier),
    replacements: listOrNull(o, "replacements", path, HEALTH_BOUNDS.replacements, replacement),
    holders: listOrNull(o, "holders", path, HEALTH_BOUNDS.holders, holder),
  }
}

function component(v: unknown, path: string): Component {
  const o = obj(v, path)
  return { id: str(o, "id", path), outcome: oneOf(o, "outcome", path, OUTCOMES), quality: oneOf(o, "quality", path, QUALITIES), build: strOrNull(o, "build", path), instance: strOrNull(o, "instance", path), source_age_s: numOrNull(o, "source_age_s", path), impact: strOrNull(o, "impact", path) }
}

function relayStage(v: unknown, path: string): RelayStage {
  const o = obj(v, path)
  return { id: str(o, "id", path), p50_ms: numOrNull(o, "p50_ms", path), p95_ms: numOrNull(o, "p95_ms", path), samples: num(o, "samples", path), pending: numOrNull(o, "pending", path), oldest_pending_s: numOrNull(o, "oldest_pending_s", path) }
}

function wait(v: unknown, path: string): DependencyWait {
  const o = obj(v, path)
  return { id: str(o, "id", path), p95_ms: numOrNull(o, "p95_ms", path), samples: num(o, "samples", path), errors: ratio(o.errors, `${path}.errors`) }
}

function saturation(v: unknown, path: string): Saturation {
  const o = obj(v, path)
  return { id: str(o, "id", path), used: numOrNull(o, "used", path), limit: numOrNull(o, "limit", path), oldest_pending_s: numOrNull(o, "oldest_pending_s", path) }
}

function services(v: unknown, path: string): Services | null {
  if (v === null) return null
  const o = obj(v, path)
  const n = HEALTH_BOUNDS.serviceRows
  return {
    components: list(o, "components", path, HEALTH_BOUNDS.components, component),
    lanes: list(o, "lanes", path, HEALTH_BOUNDS.lanes, lane),
    stages: listOrNull(o, "stages", path, n, relayStage),
    waits: listOrNull(o, "waits", path, n, wait),
    saturation: listOrNull(o, "saturation", path, n, saturation),
  }
}

function probe(v: unknown, path: string): Probe | null {
  if (v === null) return null
  const o = obj(v, path)
  return {
    every_s: num(o, "every_s", path),
    last_at: timeOrNull(o, "last_at", path),
    edges: list(o, "edges", path, HEALTH_BOUNDS.probeEdges, (e, p) => {
      const eo = obj(e, p)
      return { edge: str(eo, "edge", p), result: oneOf(eo, "result", p, PROBE_RESULTS) }
    }),
  }
}

/** Parses one response body. Throws HealthContractError; never returns a partly filled snapshot. */
export function parseHealthSnapshot(body: unknown): HealthSnapshot {
  const o = obj(body, "$")
  if (o.schema !== HEALTH_SCHEMA) throw new HealthContractError("contract_schema", "$.schema")
  const w = obj(o.window, "$.window")
  const window = { start: time(w, "start", "$.window"), end: time(w, "end", "$.window") }
  if (Date.parse(window.end) <= Date.parse(window.start)) shape("$.window.end")
  return {
    schema: HEALTH_SCHEMA,
    generated_at: time(o, "generated_at", "$"),
    scope: oneOf(o, "scope", "$", SCOPES),
    window,
    example: bool(o, "example", "$"),
    outcome: oneOf(o, "outcome", "$", OUTCOMES),
    quality: oneOf(o, "quality", "$", QUALITIES),
    errors: ratio(o.errors, "$.errors"),
    latency: latency(o.latency, "$.latency"),
    queue: queue(o.queue, "$.queue"),
    sources: list(o, "sources", "$", HEALTH_BOUNDS.sources, source),
    lanes: list(o, "lanes", "$", HEALTH_BOUNDS.lanes, lane),
    segments: list(o, "segments", "$", HEALTH_BOUNDS.segments, segmentRow),
    incidents: list(o, "incidents", "$", HEALTH_BOUNDS.incidents, incident),
    traces: list(o, "traces", "$", HEALTH_BOUNDS.traces, trace),
    machines: list(o, "machines", "$", HEALTH_BOUNDS.machines, machine),
    services: services(o.services, "$.services"),
    probe: probe(o.probe, "$.probe"),
  }
}
