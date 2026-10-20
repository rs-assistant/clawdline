/* The Cloud's independent-probe evidence, `GET {api}/su/v1/health/probes` (private contract
 * docs/connection-health-contract.md §17.4, Cloud commit cb153af), read field by field, and the
 * pure reading of one answer into what each host and target can and cannot show.
 *
 * The rules this module keeps, because the evidence invites the wrong reading:
 * - The read is service scope and takes no parameters: it is always the latest retained probe
 *   evidence (24 h), never a window, a pin or an account. The page says so.
 * - A stale target is `unknown`, never offline and never healthy. A delayed spool upload (a fresh
 *   receipt of an old observation) and an observation dated after the read are their own states,
 *   never "current".
 * - Independence is the probe operator's declaration (`operator_declared_unverified`); the wire
 *   cannot carry a verified claim, so an answer claiming more is refused, not shown.
 * - A 2xx from a readiness route proves that route answered from that viewpoint, nothing more:
 *   `does_not_prove` is shown as the Cloud listed it.
 * A list past the Cloud's own bounds is refused (`contract_bound`), never cut.
 * Nothing is imported at run time except stored.ts, so node's strip-types tests load it as it is. */
import type { Envelope } from "./stored.ts"
// @ts-expect-error -- a `.ts` path, so node's strip-types tests can import this module.
import { parseEnvelope, StoredContractError, storedReaders } from "./stored.ts"

const { shape, obj, id, idOrNull, instant, instantOrNull, int, nat, natOrNull, bool, oneOf, list } = storedReaders as typeof import("./stored").storedReaders

/** The Cloud's probe read bounds (§17.3), mirrored as STORED_BOUNDS mirrors §4. */
export const PROBE_BOUNDS = {
  /** `probeTargets`: further (host, target) pairs are omitted by the Cloud. */
  targets: 64,
  /** Registered hosts (4) plus every stored source under `svc:probe` (the 200-row page). */
  hosts: 204,
  /** `does_not_prove` and `unsupported` lists: five and four words today. */
  claims: 8,
} as const

const OPAQUE = /^[A-Za-z0-9_-]{1,64}$/

export const PROBE_STAGES = ["probe_api_readiness", "probe_relay_reachability"] as const
export type ProbeStage = (typeof PROBE_STAGES)[number]
export const PROBE_OUTCOMES = ["ok", "http_error", "timeout", "refused", "network_error", "skipped"] as const
export const PROBE_HTTP_CLASSES = ["2xx", "3xx", "4xx", "5xx"] as const
export const PROBE_INDEPENDENCE = ["declared_separate", "same_fault_domain"] as const
export type ProbeIndependence = (typeof PROBE_INDEPENDENCE)[number]
export const PROBE_TARGET_STATUS = ["answered_2xx", "http_error", "timeout", "refused", "network_error", "unknown"] as const
export type ProbeTargetStatus = (typeof PROBE_TARGET_STATUS)[number]
export const PROBE_CLAIMS = ["api_readiness_route_answered_from_viewpoint", "relay_health_route_answered_from_viewpoint"] as const
export const PROBE_PARTIAL_REASONS = ["scan_cap", "producer_reported_loss"] as const

export type ProbeLoss = { spool_dropped: number; spool_expired: number; upload_rejected: number }

export type ProbeHost = {
  producer_instance: string
  registered: boolean
  registered_viewpoint: string | null
  last_received_at: string | null
  last_receipt_age_ms: number | null
  freshness: "fresh" | "stale" | "missing"
  status: "reporting" | "unknown"
  /** null: the scan was capped, so the loss counters were not all read — unknown, never zero. */
  producer_reported_loss: ProbeLoss | null
}

export type ProbeTarget = {
  producer_instance: string
  target_id: string
  stage: ProbeStage
  viewpoint: string | null
  independence: ProbeIndependence | null
  independence_basis: "operator_declared_unverified"
  latest: {
    event: "probe_completed" | "probe_failed"
    outcome: (typeof PROBE_OUTCOMES)[number] | null
    http_class: (typeof PROBE_HTTP_CLASSES)[number] | null
    elapsed_ms: number | null
    /** Producer wall clock: display only (`producer_wall_unverified`). */
    wall: string
    received_at: string
  }
  evidence_age_ms: number
  /** Negative when the producer dated the observation after the read: unknown, never current. */
  observation_age_ms: number
  observation_clock: "producer_wall_unverified"
  freshness: "fresh" | "stale"
  status: ProbeTargetStatus
  proves: (typeof PROBE_CLAIMS)[number] | null
  does_not_prove: string[]
  results_in_window: number
  failures_in_window: number
  overlaps_skipped_in_window: number
  counts_are_floor: boolean
}

export type ProbesBody = Envelope & {
  probe: { schema: string; fresh_within_ms: number; registered_hosts: number; unsupported: string[] }
  hosts: ProbeHost[]
  targets: ProbeTarget[]
}

function loss(v: unknown, path: string): ProbeLoss | null {
  if (v === null) return null
  const o = obj(v, path)
  return { spool_dropped: nat(o.spool_dropped, `${path}.spool_dropped`), spool_expired: nat(o.spool_expired, `${path}.spool_expired`), upload_rejected: nat(o.upload_rejected, `${path}.upload_rejected`) }
}

function host(v: unknown, path: string): ProbeHost {
  const o = obj(v, path)
  const last = instantOrNull(o.last_received_at, `${path}.last_received_at`)
  const age = natOrNull(o.last_receipt_age_ms, `${path}.last_receipt_age_ms`)
  const freshness = oneOf(o.freshness, ["fresh", "stale", "missing"] as const, `${path}.freshness`)
  const status = oneOf(o.status, ["reporting", "unknown"] as const, `${path}.status`)
  // A host is missing exactly when nothing was received, and reports only while fresh.
  if ((freshness === "missing") !== (last === null) || (last === null) !== (age === null)) shape(`${path}.freshness`)
  if (status === "reporting" && freshness !== "fresh") shape(`${path}.status`)
  return {
    producer_instance: id(o.producer_instance, `${path}.producer_instance`, OPAQUE),
    registered: bool(o.registered, `${path}.registered`),
    registered_viewpoint: idOrNull(o.registered_viewpoint, `${path}.registered_viewpoint`, OPAQUE),
    last_received_at: last, last_receipt_age_ms: age, freshness, status,
    producer_reported_loss: loss(o.producer_reported_loss, `${path}.producer_reported_loss`),
  }
}

function target(v: unknown, path: string): ProbeTarget {
  const o = obj(v, path)
  // The wire can only carry a declared, unverified independence; anything stronger is drift.
  if (o.independence_basis !== "operator_declared_unverified") shape(`${path}.independence_basis`)
  if (o.observation_clock !== "producer_wall_unverified") shape(`${path}.observation_clock`)
  const r = obj(o.latest_result, `${path}.latest_result`)
  const rp = `${path}.latest_result`
  const event = oneOf(r.event, ["probe_completed", "probe_failed"] as const, `${rp}.event`)
  const outcome = r.outcome === null ? null : oneOf(r.outcome, PROBE_OUTCOMES, `${rp}.outcome`)
  const httpClass = r.http_class === null ? null : oneOf(r.http_class, PROBE_HTTP_CLASSES, `${rp}.http_class`)
  if (event === "probe_completed" && (outcome !== "ok" || httpClass !== "2xx")) shape(`${rp}.outcome`)
  for (const k of ["dns_phase", "tls_phase"] as const) if (r[k] !== null && r[k] !== "unknown") shape(`${rp}.${k}`)
  const freshness = oneOf(o.freshness, ["fresh", "stale"] as const, `${path}.freshness`)
  const status = oneOf(o.status, PROBE_TARGET_STATUS, `${path}.status`)
  const observationAge = int(o.observation_age_ms, `${path}.observation_age_ms`)
  // Stale evidence is unknown; fresh evidence is never a future observation and never unknown.
  if ((freshness === "stale") !== (status === "unknown") || (freshness === "fresh" && observationAge < 0)) shape(`${path}.status`)
  if (status === "answered_2xx" && event !== "probe_completed") shape(`${path}.status`)
  return {
    producer_instance: id(o.producer_instance, `${path}.producer_instance`, OPAQUE),
    target_id: id(o.target_id, `${path}.target_id`, OPAQUE),
    stage: oneOf(o.stage, PROBE_STAGES, `${path}.stage`),
    viewpoint: idOrNull(o.viewpoint, `${path}.viewpoint`, OPAQUE),
    independence: o.independence === null ? null : oneOf(o.independence, PROBE_INDEPENDENCE, `${path}.independence`),
    independence_basis: "operator_declared_unverified",
    latest: {
      event, outcome, http_class: httpClass, elapsed_ms: natOrNull(r.elapsed_ms, `${rp}.elapsed_ms`),
      wall: instant(r.wall, `${rp}.wall`), received_at: instant(r.received_at, `${rp}.received_at`),
    },
    evidence_age_ms: nat(o.evidence_age_ms, `${path}.evidence_age_ms`),
    observation_age_ms: observationAge,
    observation_clock: "producer_wall_unverified",
    freshness, status,
    proves: o.proves === null ? null : oneOf(o.proves, PROBE_CLAIMS, `${path}.proves`),
    does_not_prove: list(o.does_not_prove, `${path}.does_not_prove`, PROBE_BOUNDS.claims, (x, p) => id(x, p, OPAQUE)),
    results_in_window: nat(o.results_in_window, `${path}.results_in_window`),
    failures_in_window: nat(o.failures_in_window, `${path}.failures_in_window`),
    overlaps_skipped_in_window: nat(o.overlaps_skipped_in_window, `${path}.overlaps_skipped_in_window`),
    counts_are_floor: bool(o.counts_are_floor, `${path}.counts_are_floor`),
  }
}

/** One probe read. A shape the contract cannot produce is refused, so no claim reaches a screen by accident. */
export function parseProbes(body: unknown): ProbesBody {
  const env = parseEnvelope(body)
  const o = body as Record<string, unknown>
  if (env.scope.kind !== "operator") throw new StoredContractError("contract_shape", "$.scope.kind")
  if (env.partial_reason !== null) oneOf(env.partial_reason, PROBE_PARTIAL_REASONS, "$.partial_reason")
  const p = obj(o.probe, "$.probe")
  const hosts = list(o.hosts, "$.hosts", PROBE_BOUNDS.hosts, host)
  const targets = list(o.targets, "$.targets", PROBE_BOUNDS.targets, target)
  return {
    ...env,
    probe: {
      schema: id(p.schema, "$.probe.schema", /^[a-z0-9.-]{1,64}\/[0-9]{1,3}$/),
      fresh_within_ms: nat(p.fresh_within_ms, "$.probe.fresh_within_ms"),
      registered_hosts: nat(p.registered_hosts, "$.probe.registered_hosts"),
      unsupported: list(p.unsupported, "$.probe.unsupported", PROBE_BOUNDS.claims, (x, q) => id(x, q, OPAQUE)),
    },
    hosts,
    targets,
  }
}

/* ---- reading one answer ---------------------------------------------------- */

/** What the whole read can say before any row: probing off, registered but silent, or evidence. */
export type ProbePageState = "off" | "silent" | "evidence"

export function probePageState(b: ProbesBody): ProbePageState {
  if (b.evidence_quality === "unsupported" || (b.probe.registered_hosts === 0 && b.hosts.length === 0 && b.targets.length === 0)) return "off"
  return b.targets.length === 0 && b.hosts.every((h) => h.freshness === "missing") ? "silent" : "evidence"
}

/** One target's evidence, by the reason it can or cannot speak about now. */
export type ProbeEvidence = "current" | "delayed" | "clock_ahead" | "stale"

export function targetEvidence(t: ProbeTarget, freshWithinMs: number): ProbeEvidence {
  if (t.freshness === "fresh") return "current"
  if (t.observation_age_ms < 0) return "clock_ahead"
  // Received recently but observed long ago: a spool replay, never a current observation.
  if (t.evidence_age_ms <= freshWithinMs) return "delayed"
  return "stale"
}

/** What a host's loss counters say: nothing lost, some lost, or not readable. */
export function hostLoss(h: ProbeHost): "none" | "reported" | "unknown" {
  const l = h.producer_reported_loss
  if (l === null) return "unknown"
  return l.spool_dropped + l.spool_expired + l.upload_rejected > 0 ? "reported" : "none"
}
