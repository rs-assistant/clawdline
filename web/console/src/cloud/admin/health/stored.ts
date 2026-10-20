/* The Cloud's stored connection-health reads, `schema_version: 1`, read field by field.
 *
 * This is the explicit adapter between the private Cloud contract
 * (the Cloud repository's docs/connection-health-contract.md §3, §3a, §10) and the screens.
 * It keeps the API's own words: the overall outcome is always `unknown` with
 * `no_presence_contract`, per-group states are only what `cloud-derive/1`
 * derived, `null` is unknown and never zero, and coverage gaps are kept as the
 * server listed them. Only listed fields are copied, so a field the server adds
 * later never reaches a screen or an export by accident. Nothing is imported at
 * run time, so node's strip-types tests load it as it is. */

export const STORED_SCHEMA_VERSION = 1

/** The Cloud's own read bounds (contract §4). A list longer than its bound is refused, not cut. */
export const STORED_BOUNDS = {
  pageRows: 200,
  sources: 200,
  groups: 200,
  ranges: 200,
  gaps: 100,
  bootsPerSource: 8,
  coverageKeys: 16,
  ungroupedKeys: 32,
  byEventKeys: 32,
} as const

/** An opaque id as the Cloud admits it (`^[A-Za-z0-9_-]{1,64}$`), and the longest one the page carries. */
export const STORED_ID_MAX_CHARS = 64
const OPAQUE = /^[A-Za-z0-9_-]{1,64}$/
/** Stored ids that are not producer ids (`he_…`, `hs_…`, machine and account ids) are longer. */
const STORED_ID = /^[A-Za-z0-9_-]{1,128}$/
/** Account ids, and the service tenants `svc:relay` / `svc:api` whose `:` no account id can hold. */
const ACCOUNT = /^[A-Za-z0-9_:-]{1,128}$/

export const QUALITY = ["complete", "partial", "not_collected", "unsupported"] as const
export type StoredQuality = (typeof QUALITY)[number]
export const SEQUENCE = ["exact", "unverified", "not_collected", "not_computed"] as const
export type SequenceState = (typeof SEQUENCE)[number]
/** `service_scope`: Relay and API evidence, on the operator surface only and never attributed to an account (contract §10.4). */
export const COVERAGE_STATES = ["collected", "not_collected", "unsupported", "service_scope"] as const
export type CoverageState = (typeof COVERAGE_STATES)[number]
export const GROUP_OUTCOMES = ["stall_detected", "slow", "recovered", "released_after_stall", "loss", "unsupported"] as const
export type GroupOutcome = (typeof GROUP_OUTCOMES)[number]
export const GROUP_REASONS = ["sequence_gap_within", "sequence_gap_after", "sequence_unverified"] as const
export const FRESHNESS = ["fresh", "stale"] as const
export const BOOT_ORDER = ["single_boot", "unverified"] as const
export const GAP_CAUSES = ["unexplained", "unverified"] as const
/** A service stage's own result (record/2 only, contract §10.2). `pending` means still active, never final. */
export const RECORD_OUTCOMES = ["ok", "error", "timeout", "refused", "zero_target", "threw", "pending"] as const
export type RecordOutcome = (typeof RECORD_OUTCOMES)[number]

export class StoredContractError extends Error {
  readonly code: "contract_schema" | "contract_shape" | "contract_bound"
  readonly path: string
  constructor(code: "contract_schema" | "contract_shape" | "contract_bound", path: string) {
    super(`${code} at ${path}`)
    this.code = code
    this.path = path
  }
}

type Obj = Record<string, unknown>

/* ---- the shapes ----------------------------------------------------------- */

export type SeqRange = { source_id: string; machine_id: string; boot_id: string; from: number; to: number }
export type SeqGap = SeqRange & { count: number | null; cause: (typeof GAP_CAUSES)[number] }

export type Coverage = {
  retention_floor: string | null
  truncated: boolean
  sequence: SequenceState
  sequence_reason: string | null
  /** null means not computed or past a cap: unknown, never "no ranges". */
  seq_ranges: SeqRange[] | null
  gaps: SeqGap[] | null
  gap_edges: string | null
  dropped: { transitions_lost: number | null; overflow: number | null } | null
}

export type Envelope = {
  generated_at: string
  as_of: string
  scope: { kind: "owner" } | { kind: "operator"; account: string | null }
  /** Always "unknown" in v1: there is no presence contract, so silence is never healthy. */
  outcome: "unknown"
  outcome_reason: string | null
  rule_version: string | null
  evidence_quality: StoredQuality
  partial_reason: string | null
  next_cursor: string | null
  clock_uncertainty_ms: number | null
  clock: { basis: string; order: string; producer_wall: string }
  owner_diagnostics: string
  coverage: Coverage
  source_coverage: Record<string, CoverageState>
  window: { from: string; to: string } | null
}

export type StoredEvent = {
  id: string
  account: string | null
  machine_id: string
  source_id: string
  producer_instance: string
  schema: string
  producer_seq: number
  event_id: string
  boot_id: string
  batch_id: string | null
  build_commit: string | null
  event: string
  /** Producer wall clock: display only, never subtracted across hosts. */
  wall: string
  mono_ns: number
  source: string | null
  stage: string | null
  class: string | null
  kind: string | null
  phase_id: number | null
  correlation: string | null
  count: number | null
  rule: string | null
  /** record/2 service rows only; null on a daemon row, which has no such field. */
  outcome: RecordOutcome | null
  /** The service's own elapsed time for that stage (the maximum, for an aggregate). Not end-to-end latency. */
  elapsed_ms: number | null
  received_at: string
  retain_until: string
}

export type StoredBoot = {
  boot_id: string
  server_generation: number
  build_commit: string | null
  first_received_at: string
  last_received_at: string
  last_wall_at: string | null
  last_seq: number
  events_accepted: number
  events_rejected: number
}

export type StoredSource = {
  source_id: string
  account: string | null
  machine_id: string
  producer_instance: string
  source: string | null
  boot_id: string
  boot_basis: string
  boot_order: (typeof BOOT_ORDER)[number]
  build_commit: string | null
  build_provenance: string
  last_received_at: string
  last_wall_at: string | null
  last_receipt_age_ms: number
  /** Evidence age, never machine presence: stale is not offline. */
  freshness: (typeof FRESHNESS)[number]
  expected_every_ms: number | null
  last_seq: number
  events_accepted: number
  events_rejected: number
  clock_offset_ms: number | null
  boots: StoredBoot[]
  boots_evicted: number
  retain_until: string
}

export type StoredGroup = {
  account: string | null
  machine_id: string
  source_id: string
  boot_id: string
  phase_id: number | null
  correlation: string | null
  group_outcome: GroupOutcome
  group_quality: "complete" | "partial"
  group_reason: (typeof GROUP_REASONS)[number] | null
  last_event: string
  last_stage: string | null
  class: string | null
  first_seq: number
  last_seq: number
  events: number
  dropped: number | null
  last_received_at: string
  evidence_age_ms: number
  last_wall: string
  rule_version: string
}

export type OverviewBody = Envelope & {
  events: { total: number | null; total_is_floor: boolean; by_event: Record<string, number> | null }
  sources: { listed: number; more: boolean; fresh: number | null; stale: number | null }
  incidents_coverage: string
}
export type EventsBody = Envelope & { events: StoredEvent[] }
export type SourcesBody = Envelope & { sources: StoredSource[] }
export type OutcomesBody = Envelope & { groups: StoredGroup[] | null; ungrouped: Record<string, number> | null }
export type IncidentsBody = Envelope & { incidents: null }

/* ---- readers ---------------------------------------------------------------- */

const shape = (path: string): never => {
  throw new StoredContractError("contract_shape", path)
}
const obj = (v: unknown, path: string): Obj => (v !== null && typeof v === "object" && !Array.isArray(v) ? (v as Obj) : shape(path))
const str = (v: unknown, path: string): string => (typeof v === "string" && v.length > 0 && v.length <= 512 ? v : shape(path))
const strOrNull = (v: unknown, path: string): string | null => (v === null || v === undefined ? null : str(v, path))
const id = (v: unknown, path: string, pattern = STORED_ID): string => (typeof v === "string" && pattern.test(v) ? v : shape(path))
const idOrNull = (v: unknown, path: string, pattern = STORED_ID): string | null => (v === null || v === undefined ? null : id(v, path, pattern))
const instant = (v: unknown, path: string): string => (typeof v === "string" && v.length <= 40 && Number.isFinite(Date.parse(v)) ? v : shape(path))
const instantOrNull = (v: unknown, path: string): string | null => (v === null || v === undefined ? null : instant(v, path))
const int = (v: unknown, path: string): number => (typeof v === "number" && Number.isSafeInteger(v) ? v : shape(path))
const nat = (v: unknown, path: string): number => (int(v, path) >= 0 ? (v as number) : shape(path))
const natOrNull = (v: unknown, path: string): number | null => (v === null || v === undefined ? null : nat(v, path))
const intOrNull = (v: unknown, path: string): number | null => (v === null || v === undefined ? null : int(v, path))
const bool = (v: unknown, path: string): boolean => (typeof v === "boolean" ? v : shape(path))
function oneOf<T extends readonly string[]>(v: unknown, list: T, path: string): T[number] {
  return typeof v === "string" && (list as readonly string[]).includes(v) ? (v as T[number]) : shape(path)
}
function list<T>(v: unknown, path: string, bound: number, item: (x: unknown, p: string) => T): T[] {
  if (!Array.isArray(v)) return shape(path)
  if (v.length > bound) throw new StoredContractError("contract_bound", path)
  return v.map((x, i) => item(x, `${path}[${i}]`))
}
function counts(v: unknown, path: string, bound: number): Record<string, number> {
  const o = obj(v, path)
  const keys = Object.keys(o)
  if (keys.length > bound) throw new StoredContractError("contract_bound", path)
  const out: Record<string, number> = {}
  for (const k of keys) out[id(k, `${path}.key`)] = nat(o[k], `${path}.${k}`)
  return out
}

function range(v: unknown, path: string): SeqRange {
  const o = obj(v, path)
  return { source_id: id(o.source_id, `${path}.source_id`), machine_id: id(o.machine_id, `${path}.machine_id`), boot_id: id(o.boot_id, `${path}.boot_id`, OPAQUE), from: nat(o.from, `${path}.from`), to: nat(o.to, `${path}.to`) }
}

function coverage(v: unknown, path: string): Coverage {
  const o = obj(v, path)
  const dropped = o.dropped === null || o.dropped === undefined ? null : obj(o.dropped, `${path}.dropped`)
  return {
    retention_floor: instantOrNull(o.retention_floor, `${path}.retention_floor`),
    truncated: o.truncated === undefined ? false : bool(o.truncated, `${path}.truncated`),
    sequence: oneOf(o.sequence, SEQUENCE, `${path}.sequence`),
    sequence_reason: idOrNull(o.sequence_reason, `${path}.sequence_reason`),
    seq_ranges: o.seq_ranges === null || o.seq_ranges === undefined ? null : list(o.seq_ranges, `${path}.seq_ranges`, STORED_BOUNDS.ranges, range),
    gaps: o.gaps === null || o.gaps === undefined ? null : list(o.gaps, `${path}.gaps`, STORED_BOUNDS.gaps, (g, p) => {
      const go = obj(g, p)
      return { ...range(g, p), count: natOrNull(go.count, `${p}.count`), cause: oneOf(go.cause, GAP_CAUSES, `${p}.cause`) }
    }),
    gap_edges: idOrNull(o.gap_edges, `${path}.gap_edges`),
    dropped: dropped === null ? null : { transitions_lost: natOrNull(dropped.transitions_lost, `${path}.dropped.transitions_lost`), overflow: natOrNull(dropped.overflow, `${path}.dropped.overflow`) },
  }
}

/** The envelope every read carries. A version or an overall outcome v1 cannot have is refused. */
export function parseEnvelope(body: unknown): Envelope {
  const o = obj(body, "$")
  if (o.schema_version !== STORED_SCHEMA_VERSION) throw new StoredContractError("contract_schema", "$.schema_version")
  // v1 has no presence contract: an overall verdict other than unknown is not something it can say.
  if (o.outcome !== "unknown") throw new StoredContractError("contract_shape", "$.outcome")
  const scope = obj(o.scope, "$.scope")
  const kind = oneOf(scope.kind, ["owner", "operator"] as const, "$.scope.kind")
  const clock = obj(o.clock, "$.clock")
  const sc = obj(o.source_coverage ?? {}, "$.source_coverage")
  const scKeys = Object.keys(sc)
  if (scKeys.length > STORED_BOUNDS.coverageKeys) throw new StoredContractError("contract_bound", "$.source_coverage")
  const sourceCoverage: Record<string, CoverageState> = {}
  for (const k of scKeys) sourceCoverage[id(k, "$.source_coverage.key")] = oneOf(sc[k], COVERAGE_STATES, `$.source_coverage.${k}`)
  const win = o.window === undefined || o.window === null ? null : obj(o.window, "$.window")
  return {
    generated_at: instant(o.generated_at, "$.generated_at"),
    as_of: instant(o.as_of, "$.as_of"),
    scope: kind === "owner" ? { kind } : { kind, account: idOrNull(scope.account, "$.scope.account", ACCOUNT) },
    outcome: "unknown",
    outcome_reason: idOrNull(o.outcome_reason, "$.outcome_reason"),
    rule_version: strOrNull(o.rule_version, "$.rule_version"),
    evidence_quality: oneOf(o.evidence_quality, QUALITY, "$.evidence_quality"),
    partial_reason: idOrNull(o.partial_reason, "$.partial_reason"),
    next_cursor: o.next_cursor === null || o.next_cursor === undefined ? null : (typeof o.next_cursor === "string" && /^[A-Za-z0-9_=-]{1,1024}$/.test(o.next_cursor) ? o.next_cursor : shape("$.next_cursor")),
    clock_uncertainty_ms: natOrNull(o.clock_uncertainty_ms, "$.clock_uncertainty_ms"),
    clock: { basis: id(clock.basis, "$.clock.basis"), order: id(clock.order, "$.clock.order"), producer_wall: id(clock.producer_wall, "$.clock.producer_wall") },
    owner_diagnostics: id(o.owner_diagnostics, "$.owner_diagnostics"),
    coverage: coverage(o.coverage, "$.coverage"),
    source_coverage: sourceCoverage,
    window: win === null ? null : { from: instant(win.from, "$.window.from"), to: instant(win.to, "$.window.to") },
  }
}

export function parseOverview(body: unknown): OverviewBody {
  const env = parseEnvelope(body)
  const o = body as Obj
  const ev = obj(o.events, "$.events")
  const so = obj(o.sources, "$.sources")
  const inc = obj(o.incidents, "$.incidents")
  return {
    ...env,
    events: {
      total: natOrNull(ev.total, "$.events.total"),
      total_is_floor: bool(ev.total_is_floor, "$.events.total_is_floor"),
      by_event: ev.by_event === null || ev.by_event === undefined ? null : counts(ev.by_event, "$.events.by_event", STORED_BOUNDS.byEventKeys),
    },
    sources: { listed: nat(so.listed, "$.sources.listed"), more: bool(so.more, "$.sources.more"), fresh: natOrNull(so.fresh, "$.sources.fresh"), stale: natOrNull(so.stale, "$.sources.stale") },
    incidents_coverage: id(inc.coverage, "$.incidents.coverage"),
  }
}

function event(v: unknown, path: string): StoredEvent {
  const o = obj(v, path)
  return {
    id: id(o.id, `${path}.id`),
    account: idOrNull(o.account, `${path}.account`, ACCOUNT),
    machine_id: id(o.machine_id, `${path}.machine_id`),
    source_id: id(o.source_id, `${path}.source_id`),
    producer_instance: id(o.producer_instance, `${path}.producer_instance`, OPAQUE),
    schema: str(o.schema, `${path}.schema`),
    producer_seq: nat(o.producer_seq, `${path}.producer_seq`),
    event_id: id(o.event_id, `${path}.event_id`),
    boot_id: id(o.boot_id, `${path}.boot_id`, OPAQUE),
    batch_id: idOrNull(o.batch_id, `${path}.batch_id`, OPAQUE),
    build_commit: idOrNull(o.build_commit, `${path}.build_commit`, OPAQUE),
    event: id(o.event, `${path}.event`),
    wall: instant(o.wall, `${path}.wall`),
    mono_ns: nat(o.mono_ns, `${path}.mono_ns`),
    source: idOrNull(o.source, `${path}.source`),
    stage: idOrNull(o.stage, `${path}.stage`),
    class: idOrNull(o.class, `${path}.class`),
    kind: idOrNull(o.kind, `${path}.kind`),
    phase_id: intOrNull(o.phase_id, `${path}.phase_id`),
    correlation: idOrNull(o.correlation, `${path}.correlation`, OPAQUE),
    count: natOrNull(o.count, `${path}.count`),
    rule: strOrNull(o.rule, `${path}.rule`),
    outcome: o.outcome === null || o.outcome === undefined ? null : oneOf(o.outcome, RECORD_OUTCOMES, `${path}.outcome`),
    elapsed_ms: natOrNull(o.elapsed_ms, `${path}.elapsed_ms`),
    received_at: instant(o.received_at, `${path}.received_at`),
    retain_until: instant(o.retain_until, `${path}.retain_until`),
  }
}

export function parseEvents(body: unknown): EventsBody {
  const env = parseEnvelope(body)
  return { ...env, events: list((body as Obj).events, "$.events", STORED_BOUNDS.pageRows, event) }
}

export function parseSources(body: unknown): SourcesBody {
  const env = parseEnvelope(body)
  const sources = list((body as Obj).sources, "$.sources", STORED_BOUNDS.sources, (v, p): StoredSource => {
    const o = obj(v, p)
    return {
      source_id: id(o.source_id, `${p}.source_id`),
      account: idOrNull(o.account, `${p}.account`, ACCOUNT),
      machine_id: id(o.machine_id, `${p}.machine_id`),
      producer_instance: id(o.producer_instance, `${p}.producer_instance`, OPAQUE),
      source: idOrNull(o.source, `${p}.source`),
      boot_id: id(o.boot_id, `${p}.boot_id`, OPAQUE),
      boot_basis: id(o.boot_basis, `${p}.boot_basis`),
      boot_order: oneOf(o.boot_order, BOOT_ORDER, `${p}.boot_order`),
      build_commit: idOrNull(o.build_commit, `${p}.build_commit`, OPAQUE),
      build_provenance: id(o.build_provenance, `${p}.build_provenance`),
      last_received_at: instant(o.last_received_at, `${p}.last_received_at`),
      last_wall_at: instantOrNull(o.last_wall_at, `${p}.last_wall_at`),
      last_receipt_age_ms: nat(o.last_receipt_age_ms, `${p}.last_receipt_age_ms`),
      freshness: oneOf(o.freshness, FRESHNESS, `${p}.freshness`),
      expected_every_ms: natOrNull(o.expected_every_ms, `${p}.expected_every_ms`),
      last_seq: nat(o.last_seq, `${p}.last_seq`),
      events_accepted: nat(o.events_accepted, `${p}.events_accepted`),
      events_rejected: nat(o.events_rejected, `${p}.events_rejected`),
      clock_offset_ms: intOrNull(o.clock_offset_ms, `${p}.clock_offset_ms`),
      boots: list(o.boots, `${p}.boots`, STORED_BOUNDS.bootsPerSource, (b, bp): StoredBoot => {
        const bo = obj(b, bp)
        return {
          boot_id: id(bo.boot_id, `${bp}.boot_id`, OPAQUE),
          server_generation: nat(bo.server_generation, `${bp}.server_generation`),
          build_commit: idOrNull(bo.build_commit, `${bp}.build_commit`, OPAQUE),
          first_received_at: instant(bo.first_received_at, `${bp}.first_received_at`),
          last_received_at: instant(bo.last_received_at, `${bp}.last_received_at`),
          last_wall_at: instantOrNull(bo.last_wall_at, `${bp}.last_wall_at`),
          last_seq: nat(bo.last_seq, `${bp}.last_seq`),
          events_accepted: nat(bo.events_accepted, `${bp}.events_accepted`),
          events_rejected: nat(bo.events_rejected, `${bp}.events_rejected`),
        }
      }),
      boots_evicted: nat(o.boots_evicted, `${p}.boots_evicted`),
      retain_until: instant(o.retain_until, `${p}.retain_until`),
    }
  })
  return { ...env, sources }
}

export function parseOutcomes(body: unknown): OutcomesBody {
  const env = parseEnvelope(body)
  const o = body as Obj
  const groups = o.groups === null || o.groups === undefined ? null : list(o.groups, "$.groups", STORED_BOUNDS.groups, (v, p): StoredGroup => {
    const g = obj(v, p)
    const basis = obj(g.basis, `${p}.basis`)
    return {
      account: idOrNull(g.account, `${p}.account`, ACCOUNT),
      machine_id: id(g.machine_id, `${p}.machine_id`),
      source_id: id(g.source_id, `${p}.source_id`),
      boot_id: id(g.boot_id, `${p}.boot_id`, OPAQUE),
      phase_id: intOrNull(g.phase_id, `${p}.phase_id`),
      correlation: idOrNull(g.correlation, `${p}.correlation`, OPAQUE),
      group_outcome: oneOf(g.group_outcome, GROUP_OUTCOMES, `${p}.group_outcome`),
      group_quality: oneOf(g.group_quality, ["complete", "partial"] as const, `${p}.group_quality`),
      group_reason: g.group_reason === null ? null : oneOf(g.group_reason, GROUP_REASONS, `${p}.group_reason`),
      last_event: id(g.last_event, `${p}.last_event`),
      last_stage: idOrNull(g.last_stage, `${p}.last_stage`),
      class: idOrNull(g.class, `${p}.class`),
      first_seq: nat(g.first_seq, `${p}.first_seq`),
      last_seq: nat(g.last_seq, `${p}.last_seq`),
      events: nat(g.events, `${p}.events`),
      dropped: natOrNull(g.dropped, `${p}.dropped`),
      last_received_at: instant(g.last_received_at, `${p}.last_received_at`),
      evidence_age_ms: nat(g.evidence_age_ms, `${p}.evidence_age_ms`),
      last_wall: instant(g.last_wall, `${p}.last_wall`),
      rule_version: str(basis.rule_version, `${p}.basis.rule_version`),
    }
  })
  return { ...env, groups, ungrouped: o.ungrouped === null || o.ungrouped === undefined ? null : counts(o.ungrouped, "$.ungrouped", STORED_BOUNDS.ungroupedKeys) }
}

export function parseIncidents(body: unknown): IncidentsBody {
  const env = parseEnvelope(body)
  if ((body as Obj).incidents !== null) return shape("$.incidents")
  return { ...env, incidents: null }
}

/* ---- what a service stage means (contract §10.3) --------------------------- */

/** `downstream`: the service has handed work on and is waiting for it, so time here is not its own;
 *  `handoff`: a socket send that did not throw, never an endpoint receipt; `final`: where an
 *  operation ends; `own`: the service's own work. Unlisted stages (the daemon's) have no role. */
export type StageRole = "own" | "downstream" | "handoff" | "final"
const STAGE_ROLES: Record<string, StageRole> = {
  worker_admitted: "own",
  account_dispatch_started: "downstream",
  account_dispatch_returned: "own",
  outer_settled: "final",
  do_entered: "own",
  do_runtime_ready: "own",
  storage_started: "downstream",
  storage_completed: "own",
  account_pinned: "own",
  do_handler_returned: "final",
  fanout_sent: "handoff",
  api_admitted: "own",
  api_handler_started: "own",
  api_dependency_started: "downstream",
  api_dependency_completed: "own",
  api_handler_completed: "own",
  api_response_completed: "final",
}

/** Service stages in the contract's order (§10.3): worker, then Durable Object, then API. */
export const STAGE_ORDER: readonly string[] = Object.keys(STAGE_ROLES)

export function stageRole(stage: string | null): StageRole | null {
  return stage !== null && Object.hasOwn(STAGE_ROLES, stage) ? STAGE_ROLES[stage] : null
}

/** Service producers (contract §10.1): their rows are service scope, never an account's. */
export function isServiceSource(source: string | null): boolean {
  return source === "relay" || source === "cloud_api"
}
