// The browser's own connection-health record, `clawdline.viewer-health.record/1`
// (Cloud connection-health contract §14.2; summarized in
// docs/admin-connection-health-contract.md §11).
//
// Every enum and limit here is the Cloud's, copied from its parser; nothing is
// guessed. A record names a stage, an event, an outcome and a fixed error
// word. It never carries a URL, a channel, a session, a document, a message or
// an exception's text: the Cloud refuses those keys by name, and this file has
// no way to set them.

export const VIEWER_RECORD_SCHEMA = "clawdline.viewer-health.record/1"
export const VIEWER_INGEST_PATH = "/v1/health/viewer-ingest"
export const VIEWER_HEALTH_HEADER = "x-clawdline-health"
export const VIEWER_HEALTH_HEADER_VALUE = "viewer/1"

export const VIEWER_STAGES = [
  "viewer_connect", "viewer_reconnect", "viewer_carrier_replace", "viewer_sse_resume", "viewer_sse_gap",
  "viewer_request_issued", "viewer_response_received", "viewer_decrypt_apply",
] as const
export const VIEWER_EVENTS = ["viewer_stage_started", "viewer_stage_completed", "viewer_stage_failed"] as const
export const VIEWER_OUTCOMES = ["ok", "error", "timeout", "aborted", "pending"] as const
export const VIEWER_ERRORS = [
  "network_unreachable", "http_4xx", "http_5xx", "auth_refused", "auth_revoked", "timeout", "sse_gap",
  "sse_resume_refused", "carrier_closed", "decrypt_failed", "apply_failed", "replay_rejected", "unclassified",
] as const
export const VIEWER_CLASSES = ["read", "control", "status", "transport", "probe"] as const

export type ViewerStage = (typeof VIEWER_STAGES)[number]
export type ViewerEvent = (typeof VIEWER_EVENTS)[number]
export type ViewerOutcome = (typeof VIEWER_OUTCOMES)[number]
export type ViewerError = (typeof VIEWER_ERRORS)[number]
export type ViewerClass = (typeof VIEWER_CLASSES)[number]

/** The Cloud's opaque-id pattern (`HEALTH_OPAQUE`). */
export const OPAQUE_ID = /^[A-Za-z0-9_-]{1,64}$/
/** The Cloud's machine-id pattern for `target_machine_id`. */
export const MACHINE_ID = /^[A-Za-z0-9_-]{1,128}$/

/** One record, in the key set the Cloud allows (`VIEWER_EVENT_KEYS`) and no other. */
export interface ViewerRecord {
  schema: typeof VIEWER_RECORD_SCHEMA
  producer_seq: number
  event_id: string
  boot_id: string
  event: ViewerEvent
  wall: string
  mono_ns: number
  stage: ViewerStage
  class?: ViewerClass
  correlation?: string
  outcome?: ViewerOutcome
  elapsed_ms?: number
  error?: ViewerError
  count?: number
}

/** One upload, in the batch key set the Cloud allows for a viewer. */
export interface ViewerBatch {
  schema_version: 1
  batch_id: string
  producer_instance: "viewer"
  boot_id: string
  target_machine_id?: string
  sent_at: string
  events: ViewerRecord[]
}

/** What a hook site hands the producer: the stage facts, never identifiers or text. */
export interface ViewerObservation {
  event: ViewerEvent
  stage: ViewerStage
  class?: ViewerClass
  correlation?: string
  outcome?: ViewerOutcome
  elapsedMs?: number
  error?: ViewerError
  count?: number
}

function oneOf<T extends readonly string[]>(values: T, value: unknown): value is T[number] {
  return typeof value === "string" && (values as readonly string[]).includes(value)
}

/**
 * The observation as a record the Cloud will accept, or null when it would be
 * refused. A refused record is dropped here rather than costing the whole
 * batch a `400`: the error word belongs to a failed stage and only to it.
 */
export function viewerRecord(
  observation: ViewerObservation, bootId: string, seq: number, wallMs: number, monoMs: number,
  limits: { elapsedMaxMs: number; countMax: number },
): ViewerRecord | null {
  const o = observation
  if (!oneOf(VIEWER_EVENTS, o.event) || !oneOf(VIEWER_STAGES, o.stage)) return null
  if ((o.event === "viewer_stage_failed") !== (o.error !== undefined)) return null
  if (o.error !== undefined && !oneOf(VIEWER_ERRORS, o.error)) return null
  if (o.outcome !== undefined && !oneOf(VIEWER_OUTCOMES, o.outcome)) return null
  if (o.class !== undefined && !oneOf(VIEWER_CLASSES, o.class)) return null
  if (o.correlation !== undefined && !OPAQUE_ID.test(o.correlation)) return null
  const mono = Math.round(monoMs * 1e6)
  if (!Number.isSafeInteger(mono) || mono < 0 || !Number.isFinite(wallMs)) return null
  const record: ViewerRecord = {
    schema: VIEWER_RECORD_SCHEMA,
    producer_seq: seq,
    event_id: `${bootId}-${seq}`,
    boot_id: bootId,
    event: o.event,
    wall: new Date(wallMs).toISOString(),
    mono_ns: mono,
    stage: o.stage,
  }
  if (o.class !== undefined) record.class = o.class
  if (o.correlation !== undefined) record.correlation = o.correlation
  if (o.outcome !== undefined) record.outcome = o.outcome
  if (o.error !== undefined) record.error = o.error
  if (o.elapsedMs !== undefined && Number.isFinite(o.elapsedMs)) {
    record.elapsed_ms = Math.min(limits.elapsedMaxMs, Math.max(0, Math.round(o.elapsedMs)))
  }
  if (o.count !== undefined && Number.isFinite(o.count)) {
    record.count = Math.min(limits.countMax, Math.max(0, Math.round(o.count)))
  }
  return record
}

/** A random opaque id with a fixed prefix; never derived from anything the page shows. */
export function randomID(prefix: string, bytes: number, random?: (n: number) => Uint8Array): string {
  const raw = random ? random(bytes) : globalThis.crypto.getRandomValues(new Uint8Array(bytes))
  let hex = ""
  for (const b of raw) hex += b.toString(16).padStart(2, "0")
  return `${prefix}${hex}`
}
