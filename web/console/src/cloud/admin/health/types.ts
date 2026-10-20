/* Connection health: the typed snapshot the six screens render (M01–M06).
 *
 * This is a proposed consumer contract, `clawdline.connection-health.v0-draft`,
 * written while the backend contract was designed in parallel. Every value the
 * screens show comes from a parsed snapshot of this shape; nothing here holds
 * example data. See docs/admin-connection-health-ui-integration.md. */

export const HEALTH_SCHEMA = "clawdline.connection-health.v0-draft"

/** Operational outcome: what happened to the work (docs/admin-connection-health.md, health rules). */
export const OUTCOMES = ["blocked", "offline", "error", "slow", "unknown", "no_requests", "healthy"] as const
export type Outcome = (typeof OUTCOMES)[number]

/** Evidence quality: how much of it the page can see. Always shown apart from the outcome. */
export const QUALITIES = ["complete", "partial", "stale", "not_collected", "unsupported", "forbidden", "no_requests"] as const
export type Quality = (typeof QUALITIES)[number]

/** Who is reading. A Cloud operator and a paired owner are separate authorizations, never borrowed. */
export const SCOPES = ["cloud_operator", "paired_owner"] as const
export type Scope = (typeof SCOPES)[number]

export const SEGMENTS = ["browser", "relay", "machine_receive", "machine_process", "machine_send", "collector"] as const
export type Segment = (typeof SEGMENTS)[number]

export const SOURCE_KINDS = ["viewer", "relay", "machine_snapshot", "collector", "api", "probe"] as const
export type SourceKind = (typeof SOURCE_KINDS)[number]

export const FRESHNESS = ["fresh", "stale", "no_receipt", "unsupported", "forbidden"] as const
export type Freshness = (typeof FRESHNESS)[number]

export const GAP_REASONS = ["not_collected", "not_retained", "unsupported", "forbidden", "stale"] as const
export type GapReason = (typeof GAP_REASONS)[number]

/** A lane span is an outcome, or a reason there is no outcome to draw. */
export const SPAN_STATES = [...OUTCOMES, "not_retained", "forbidden"] as const
export type SpanState = (typeof SPAN_STATES)[number]

/** Each stage is a separate receipt; one never implies the next. */
export const STAGES = [
  "accepted", "queued", "executed", "published", "relay_socket_accepted", "viewer_received", "ui_applied", "acknowledged",
] as const
export type Stage = (typeof STAGES)[number]

export const STAGE_STATES = ["received", "no_receipt", "active", "forbidden", "not_requested"] as const
export type StageState = (typeof STAGE_STATES)[number]

export const CERTAINTIES = ["confirmed", "suspected", "unknown"] as const
export type Certainty = (typeof CERTAINTIES)[number]

export const INCIDENT_STATES = ["active", "recovering", "recovered_awaiting_review", "closed"] as const
export type IncidentState = (typeof INCIDENT_STATES)[number]

export const RECOVERY = ["verified", "unverified", "not_applicable"] as const
export type Recovery = (typeof RECOVERY)[number]

export const BASES = ["source", "ordering", "inference"] as const
export type Basis = (typeof BASES)[number]

export const PROBE_RESULTS = ["passed", "failed", "not_exercised"] as const
export type ProbeResult = (typeof PROBE_RESULTS)[number]

export type Ratio = { count: number; denominator: number }
export type Latency = { p50_ms: number | null; p95_ms: number | null; p99_ms: number | null; samples: number }

export type Gap = { start: string; end: string | null; reason: GapReason }

export type SourceStatus = {
  id: string
  kind: SourceKind
  state: Freshness
  last_receipt_at: string | null
  expected_every_s: number | null
  retention_s: number | null
  /** Oldest instant still retained, when a byte cap cut retention short of retention_s. */
  retention_floor_at: string | null
  gaps: Gap[]
  dropped: number | null
  schema: string | null
}

export type LaneSpan = { start: string; end: string; state: SpanState }
export type LaneTick = { at: string; label: string }
export type Lane = { segment: Segment; spans: LaneSpan[]; ticks: LaneTick[] }

export type SegmentRow = {
  segment: Segment
  outcome: Outcome
  quality: Quality
  latency: Latency | null
  errors: Ratio | null
  source_age_s: number | null
}

/** A recorded statement. `forbidden` items keep their source and lose their text. */
export type Evidence = { id: string; source: string; statement: string | null; observed_at: string | null; forbidden: boolean }
export type Hypothesis = { id: string; statement: string | null; basis: Basis; sources: string[]; forbidden: boolean }
export type Missing = { id: string; what: string; next_step: string | null }

export type Incident = {
  id: string
  title: string
  state: IncidentState
  outcome: Outcome
  segment: Segment
  started_at: string
  ended_at: string | null
  certainty: Certainty
  recovery: Recovery
  affected: { count: number; lower_bound: boolean } | null
  build: string | null
  trace_id: string | null
  rule_version: string | null
  facts: Evidence[]
  hypotheses: Hypothesis[]
  missing: Missing[]
}

export type TraceStage = { stage: Stage; state: StageState; at: string | null; recorder: string | null; late: boolean }
export type OpenSpan = { stage: Stage; holder: string; started_at: string; last_progress_at: string | null; deadline_ms: number | null }
export type LocationCode = { code: string; path: string | null; line: number | null; symbol: string | null; note: string | null }

export type Trace = {
  id: string
  incident_id: string | null
  operation: string
  attempts: number
  build: string | null
  stages: TraceStage[]
  open_spans: OpenSpan[]
  locations: LocationCode[]
}

export type Carrier = {
  viewer: string
  read_carrier: string
  command_carrier: string
  state: Outcome
  generation: number | null
  last_seen_at: string | null
  replaced: number | null
  refused: number | null
}
export type Replacement = { at: string; viewer: string; from_generation: number | null; to_generation: number | null; reason: string | null }
export type Holder = { stage: string; holder: string | null; age_s: number | null; deadline_s: number | null; last_progress_at: string | null; as_of: string | null }

export type Machine = {
  id: string
  presence: "renewed" | "expired" | "unknown"
  presence_at: string | null
  boot_id: string | null
  boot_at: string | null
  build: string | null
  platform: string | null
  snapshot_at: string | null
  lanes: Lane[]
  carriers: Carrier[] | null
  replacements: Replacement[] | null
  holders: Holder[] | null
}

export type Component = {
  id: string
  outcome: Outcome
  quality: Quality
  build: string | null
  instance: string | null
  source_age_s: number | null
  impact: string | null
}
export type RelayStage = { id: string; p50_ms: number | null; p95_ms: number | null; samples: number; pending: number | null; oldest_pending_s: number | null }
export type DependencyWait = { id: string; p95_ms: number | null; samples: number; errors: Ratio | null }
export type Saturation = { id: string; used: number | null; limit: number | null; oldest_pending_s: number | null }

export type Services = {
  components: Component[]
  lanes: Lane[]
  stages: RelayStage[] | null
  waits: DependencyWait[] | null
  saturation: Saturation[] | null
}

export type Probe = {
  every_s: number
  last_at: string | null
  edges: { edge: string; result: ProbeResult }[]
}

export type Queue = { used: number | null; limit: number | null; oldest_s: number | null; as_of: string | null }

export type HealthSnapshot = {
  schema: typeof HEALTH_SCHEMA
  generated_at: string
  scope: Scope
  window: { start: string; end: string }
  /** True only for an explicitly isolated example source; every view and export says so. */
  example: boolean
  outcome: Outcome
  quality: Quality
  errors: Ratio | null
  latency: Latency | null
  queue: Queue | null
  sources: SourceStatus[]
  lanes: Lane[]
  segments: SegmentRow[]
  incidents: Incident[]
  traces: Trace[]
  machines: Machine[]
  services: Services | null
  probe: Probe | null
}

/** What a screen asks a source for. Ids are opaque; no credentials or content travel in it. */
export type HealthQuery = { scope: Scope; window_s: number; until: string | null }

export const WINDOWS_S = [900, 3600, 21600] as const
