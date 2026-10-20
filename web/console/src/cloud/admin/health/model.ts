/* Pure view decisions for the connection-health screens: what a number means,
 * where a span sits, which stage was last proven, what the export carries.
 * No React, no fetch, no example data. */
import type { HealthSnapshot, Incident, Lane, Latency, Outcome, Ratio, SourceStatus, SpanState, Stage, Trace } from "./types.ts"
// @ts-expect-error -- a `.ts` path, so node's strip-types tests can import this module.
import { OUTCOMES, STAGES } from "./types.ts"

/** Below this many samples a p99 says too little to print. Proposal from the panel design §4. */
export const P99_MIN_SAMPLES = 20

/** Worst first, in the order OUTCOMES declares. An empty list is unknown, never healthy. */
export function worstOutcome(list: readonly Outcome[]): Outcome {
  if (list.length === 0) return "unknown"
  return list.reduce((worst, o) => (OUTCOMES.indexOf(o) < OUTCOMES.indexOf(worst) ? o : worst))
}

export type LatencyView =
  | { kind: "unknown" }
  | { kind: "values"; p50: number | null; p95: number | null; p99: number | null; tooFewForP99: boolean; samples: number }

/** Empty latency is unknown, never 0 ms. */
export function latencyView(l: Latency | null): LatencyView {
  if (l === null || l.samples === 0 || (l.p50_ms === null && l.p95_ms === null && l.p99_ms === null)) return { kind: "unknown" }
  const tooFew = l.samples < P99_MIN_SAMPLES
  return { kind: "values", p50: l.p50_ms, p95: l.p95_ms, p99: tooFew ? null : l.p99_ms, tooFewForP99: tooFew, samples: l.samples }
}

export type RateView = { kind: "unknown" } | { kind: "no_requests" } | { kind: "rate"; percent: number; count: number; denominator: number }

/** A window with no requests has no rate: zero traffic is not 100 % success. */
export function rateView(r: Ratio | null): RateView {
  if (r === null) return { kind: "unknown" }
  if (r.denominator === 0) return { kind: "no_requests" }
  return { kind: "rate", percent: (r.count / r.denominator) * 100, count: r.count, denominator: r.denominator }
}

/** Seconds since the source's last receipt, as of `now`; null when it never reported. */
export function sourceAge(s: SourceStatus, now: number): number | null {
  if (s.last_receipt_at === null) return null
  return Math.max(0, Math.round((now - Date.parse(s.last_receipt_at)) / 1000))
}

export type PlacedSpan = { left: number; width: number; state: SpanState; start: string; end: string; covered: boolean }

/** Places a lane's spans as percentages of the window. Time the source did not cover becomes an
 *  explicit unknown stretch — never a healthy line, never an empty track. */
export function placeLane(lane: Lane, window: { start: string; end: string }): PlacedSpan[] {
  const w0 = Date.parse(window.start)
  const w1 = Date.parse(window.end)
  const total = w1 - w0
  const pct = (t: number) => ((Math.min(Math.max(t, w0), w1) - w0) / total) * 100
  const sorted = lane.spans
    .map((s) => ({ ...s, a: Date.parse(s.start), b: Date.parse(s.end) }))
    .filter((s) => s.b > w0 && s.a < w1)
    .sort((x, y) => x.a - y.a)
  const out: PlacedSpan[] = []
  let cursor = w0
  const iso = (t: number) => new Date(t).toISOString()
  for (const s of sorted) {
    if (s.a > cursor) out.push({ left: pct(cursor), width: pct(s.a) - pct(cursor), state: "unknown", start: iso(cursor), end: iso(s.a), covered: false })
    const a = Math.max(s.a, cursor)
    if (s.b > a) out.push({ left: pct(a), width: pct(s.b) - pct(a), state: s.state, start: iso(a), end: iso(Math.min(s.b, w1)), covered: true })
    cursor = Math.max(cursor, s.b)
  }
  if (cursor < w1) out.push({ left: pct(cursor), width: 100 - pct(cursor), state: "unknown", start: iso(cursor), end: iso(w1), covered: false })
  return out
}

/** Position of an instant inside the window, 0–100, or null when it falls outside. */
export function placeInstant(at: string, window: { start: string; end: string }): number | null {
  const t = Date.parse(at)
  const w0 = Date.parse(window.start)
  const w1 = Date.parse(window.end)
  return t < w0 || t > w1 ? null : ((t - w0) / (w1 - w0)) * 100
}

export type StageProgress = {
  /** End of the contiguous run of receipts from the first stage. */
  last_proven_stage: Stage | null
  /** Only from an independent active-phase span; otherwise null. */
  active_stage: Stage | null
  next_unobserved_stage: Stage | null
  /** The first stage the scope cannot read, when it hides the next one. */
  visibility_limit_at: Stage | null
}

export function stageProgress(trace: Trace): StageProgress {
  const ordered = [...trace.stages].sort((a, b) => STAGES.indexOf(a.stage) - STAGES.indexOf(b.stage))
  let last: Stage | null = null
  let next: Stage | null = null
  let limit: Stage | null = null
  for (const s of ordered) {
    if (s.state === "not_requested") continue
    if (s.state === "received") {
      last = s.stage
      continue
    }
    if (s.state === "forbidden") limit = s.stage
    next = s.stage
    break
  }
  const active = trace.open_spans.find((sp) => next !== null && sp.stage === next)?.stage ?? null
  return { last_proven_stage: last, active_stage: active, next_unobserved_stage: next, visibility_limit_at: limit }
}

/** Milliseconds from the first receipt for each received stage; null for stages without one. */
export function stageOffsets(trace: Trace): Map<Stage, number | null> {
  const times = trace.stages.filter((s) => s.at !== null).map((s) => Date.parse(s.at as string))
  const origin = times.length ? Math.min(...times) : 0
  return new Map(trace.stages.map((s) => [s.stage, s.at === null ? null : Date.parse(s.at) - origin]))
}

/** A logarithmic bar length, 2–100, so a 40 ms queue and a 61 s receipt both stay visible. */
export function logWidth(ms: number, maxMs: number): number {
  if (maxMs <= 0) return 2
  const v = Math.log10(1 + Math.max(0, ms)) / Math.log10(1 + maxMs)
  return Math.max(2, Math.min(100, v * 100))
}

const ACTIVE_STATES = new Set(["active", "recovering"])

/** Active work first, then the worse outcome, then the newer start. Stable for equal keys. */
export function rankIncidents(list: readonly Incident[]): Incident[] {
  return list
    .map((inc, i) => ({ inc, i }))
    .sort((x, y) => {
      const a = ACTIVE_STATES.has(x.inc.state) ? 0 : 1
      const b = ACTIVE_STATES.has(y.inc.state) ? 0 : 1
      if (a !== b) return a - b
      const o = OUTCOMES.indexOf(x.inc.outcome) - OUTCOMES.indexOf(y.inc.outcome)
      if (o !== 0) return o
      const t = Date.parse(y.inc.started_at) - Date.parse(x.inc.started_at)
      return t !== 0 ? t : x.i - y.i
    })
    .map((r) => r.inc)
}

export type Selection = { id: string | null; chosen: boolean; absent: boolean }

/** A live refresh never replaces the investigation: a chosen incident stays chosen, and says so
 *  when the new data no longer contains it. An unchosen selection follows the newest top problem. */
export function keepSelection(prev: Selection, incidents: readonly Incident[]): Selection {
  if (prev.chosen && prev.id !== null) return { id: prev.id, chosen: true, absent: !incidents.some((i) => i.id === prev.id) }
  const top = rankIncidents(incidents).find((i) => ACTIVE_STATES.has(i.state)) ?? null
  return { id: top?.id ?? null, chosen: false, absent: false }
}

/* ---- addresses ------------------------------------------------------------ */

export type HealthTarget =
  | { screen: "M01" }
  | { screen: "M02"; machine: string | null }
  | { screen: "M03" }
  | { screen: "M04"; trace: string }
  | { screen: "M05"; incident: string }
  | { screen: "M06" }
  | { screen: "evidence"; incident: string }

/** Proposed paths under the existing admin shell. Ids are opaque and URL-encoded; nothing else goes in. */
export const HEALTH_BASE = "/admin/health"

export function healthPath(t: HealthTarget): string {
  const e = encodeURIComponent
  switch (t.screen) {
    case "M01": return HEALTH_BASE
    case "M02": return t.machine === null ? `${HEALTH_BASE}/machines` : `${HEALTH_BASE}/machines/${e(t.machine)}`
    case "M03": return `${HEALTH_BASE}/services`
    case "M04": return `${HEALTH_BASE}/trace/${e(t.trace)}`
    case "M05": return `${HEALTH_BASE}/incident/${e(t.incident)}`
    case "M06": return `${HEALTH_BASE}/monitoring`
    case "evidence": return `${HEALTH_BASE}/evidence/${e(t.incident)}`
  }
}

/** The screen an address names, or null when it names none under the health base. */
export function healthTarget(pathname: string): HealthTarget | null {
  if (pathname !== HEALTH_BASE && !pathname.startsWith(HEALTH_BASE + "/")) return null
  const parts = pathname.slice(HEALTH_BASE.length).split("/").filter(Boolean)
  let id: string | null = null
  try {
    id = parts[1] === undefined ? null : decodeURIComponent(parts[1])
  } catch {
    return null
  }
  if (parts.length > 2) return null
  switch (parts[0]) {
    case undefined: return { screen: "M01" }
    case "machines": return { screen: "M02", machine: id }
    case "services": return parts.length === 1 ? { screen: "M03" } : null
    case "monitoring": return parts.length === 1 ? { screen: "M06" } : null
    case "trace": return id ? { screen: "M04", trace: id } : null
    case "incident": return id ? { screen: "M05", incident: id } : null
    case "evidence": return id ? { screen: "evidence", incident: id } : null
    default: return null
  }
}

/* ---- agent evidence ------------------------------------------------------- */

export const EVIDENCE_SCHEMA = "clawdline.connection-evidence/0.1-draft"
/** The export's own byte bound. Proposed; the panel design's sanitized-export cap. */
export const EVIDENCE_MAX_BYTES = 512 * 1024

export type EvidenceExport = { ok: true; json: string; bytes: number } | { ok: false; reason: "no_incident" | "too_large" }

/** Builds the agent record from the authorized snapshot only. Fields are listed one by one, so a
 *  field the source adds later never reaches the export by accident; forbidden items keep only
 *  their source. Example data says so at the top of the record. */
export function buildEvidence(snap: HealthSnapshot, incidentId: string, exportedAt: string): EvidenceExport {
  const inc = snap.incidents.find((i) => i.id === incidentId)
  if (!inc) return { ok: false, reason: "no_incident" }
  const trace = inc.trace_id === null ? null : snap.traces.find((t) => t.id === inc.trace_id) ?? null
  const record = {
    schema: EVIDENCE_SCHEMA,
    example: snap.example,
    exported_at: exportedAt,
    snapshot_generated_at: snap.generated_at,
    scope: snap.scope,
    window: snap.window,
    incident: {
      id: inc.id, title: inc.title, state: inc.state, outcome: inc.outcome, segment: inc.segment,
      started_at: inc.started_at, ended_at: inc.ended_at, certainty: inc.certainty, recovery: inc.recovery,
      affected: inc.affected, build: inc.build, rule_version: inc.rule_version,
    },
    facts: inc.facts.map((f) => (f.forbidden ? { source: f.source, forbidden: true } : { id: f.id, source: f.source, statement: f.statement, observed_at: f.observed_at })),
    hypotheses: inc.hypotheses.map((h) => (h.forbidden ? { sources: h.sources, forbidden: true } : { id: h.id, statement: h.statement, basis: h.basis, sources: h.sources })),
    missing_evidence: inc.missing.map((m) => ({ id: m.id, what: m.what, next_step: m.next_step })),
    trace: trace === null ? null : {
      id: trace.id, operation: trace.operation, attempts: trace.attempts, build: trace.build,
      stage_progress: stageProgress(trace),
      stages: trace.stages.map((s) => ({ stage: s.stage, state: s.state, at: s.at, recorder: s.recorder, late: s.late })),
      open_spans: trace.open_spans.map((s) => ({ stage: s.stage, holder: s.holder, started_at: s.started_at, last_progress_at: s.last_progress_at, deadline_ms: s.deadline_ms })),
      locations: trace.locations.map((l) => ({ code: l.code, path: l.path, line: l.line, symbol: l.symbol, note: l.note })),
    },
    sources: snap.sources.map((s) => ({
      id: s.id, kind: s.kind, state: s.state, last_receipt_at: s.last_receipt_at, expected_every_s: s.expected_every_s,
      retention_s: s.retention_s, retention_floor_at: s.retention_floor_at, gaps: s.gaps, dropped: s.dropped, schema: s.schema,
    })),
  }
  const json = JSON.stringify(record, null, 2)
  const bytes = new TextEncoder().encode(json).length
  return bytes > EVIDENCE_MAX_BYTES ? { ok: false, reason: "too_large" } : { ok: true, json, bytes }
}
