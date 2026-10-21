// A Cloud viewer-ingest route for tests: the Cloud's parser rules for
// `clawdline.viewer-health.record/1` (contract §14.2, `parseViewerEvent`) and
// its batch idempotency (§2), restated so a producer's real bodies are judged
// by what the server would answer. Synthetic only: no network, no secret.

const PRIVATE_FIELDS = [
  "url", "href", "path", "channel", "channel_id", "session", "session_id", "document", "document_id", "message", "message_id",
  "image", "sdp", "candidate", "error_message", "stack", "payload", "text", "title", "token", "key",
]
const BATCH_KEYS = ["schema_version", "batch_id", "producer_instance", "boot_id", "build_commit", "sent_at", "events", "target_machine_id"]
const EVENT_KEYS = ["schema", "producer_seq", "event_id", "boot_id", "event", "wall", "mono_ns", "stage", "class", "correlation", "count", "outcome", "elapsed_ms", "error"]
const REQUIRED = ["schema", "producer_seq", "event_id", "boot_id", "event", "wall", "mono_ns"]
const STAGES = ["viewer_connect", "viewer_reconnect", "viewer_carrier_replace", "viewer_sse_resume", "viewer_sse_gap", "viewer_request_issued", "viewer_response_received", "viewer_decrypt_apply"]
const EVENTS = ["viewer_stage_started", "viewer_stage_completed", "viewer_stage_failed"]
const OUTCOMES = ["ok", "error", "timeout", "aborted", "pending"]
const ERRORS = ["network_unreachable", "http_4xx", "http_5xx", "auth_refused", "auth_revoked", "timeout", "sse_gap", "sse_resume_refused", "carrier_closed", "decrypt_failed", "apply_failed", "replay_rejected", "unclassified"]
const CLASSES = ["read", "control", "status", "transport", "probe"]
const OPAQUE = /^[A-Za-z0-9_-]{1,64}$/
const ISO = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d{1,9})?Z$/

export class Refusal extends Error {
  readonly status: number
  readonly code: string
  readonly field: string
  constructor(status: number, code: string, field: string) {
    super(code)
    this.status = status
    this.code = code
    this.field = field
  }
}

type Obj = Record<string, unknown>

function check(ok: boolean, code: string, field: string, status = 400): void {
  if (!ok) throw new Refusal(status, code, field)
}

function refusePrivate(source: Obj, prefix: string): void {
  for (const key of PRIVATE_FIELDS) check(!(key in source), "owner_private_plaintext", prefix + key)
}

function int(source: Obj, key: string, field: string, min: number, optional: boolean): void {
  const v = source[key]
  if (optional && (v === undefined || v === null)) return
  check(typeof v === "number" && Number.isSafeInteger(v) && v >= min, "invalid_field", field + key)
}

function enumOf(source: Obj, key: string, values: string[], field: string, optional: boolean): void {
  const v = source[key]
  if (optional && (v === undefined || v === null)) return
  check(typeof v === "string" && values.includes(v), "invalid_field", field + key)
}

/** The Cloud's verdict on one body: throws a `Refusal`, or answers the parsed batch. */
export function parseViewerBody(text: string): Obj {
  check(new TextEncoder().encode(text).length <= 16384, "payload_too_large", "body", 413)
  const batch = JSON.parse(text) as Obj
  check(batch.schema_version === 1, "unsupported_schema", "schema_version")
  refusePrivate(batch, "")
  for (const key of Object.keys(batch)) check(BATCH_KEYS.includes(key), "unknown_field", key)
  check(batch.producer_instance === "viewer", "forbidden", "producer_instance", 403)
  const events = batch.events
  check(Array.isArray(events) && events.length >= 1 && events.length <= 50, "invalid_field", "events")
  check(typeof batch.boot_id === "string" && OPAQUE.test(batch.boot_id), "invalid_field", "boot_id")
  check(typeof batch.batch_id === "string" && OPAQUE.test(batch.batch_id), "invalid_field", "batch_id")
  if (batch.sent_at !== undefined) check(typeof batch.sent_at === "string" && ISO.test(batch.sent_at), "invalid_field", "sent_at")
  if (batch.target_machine_id !== undefined) check(typeof batch.target_machine_id === "string" && /^[A-Za-z0-9_-]{1,128}$/.test(batch.target_machine_id), "invalid_field", "target_machine_id")
  ;(events as Obj[]).forEach((event, index) => {
    const f = `events[${index}].`
    refusePrivate(event, f)
    for (const key of Object.keys(event)) check(EVENT_KEYS.includes(key), "unknown_field", f + key)
    for (const key of REQUIRED) check(key in event, "invalid_field", f + key)
    check(event.schema === "clawdline.viewer-health.record/1", "unsupported_schema", f + "schema")
    int(event, "producer_seq", f, 1, false)
    check(typeof event.event_id === "string" && /^[A-Za-z0-9_-]{1,96}$/.test(event.event_id), "invalid_field", f + "event_id")
    enumOf(event, "event", EVENTS, f, false)
    check(typeof event.wall === "string" && ISO.test(event.wall), "invalid_field", f + "wall")
    int(event, "mono_ns", f, 0, false)
    enumOf(event, "stage", STAGES, f, false)
    enumOf(event, "class", CLASSES, f, true)
    if (event.correlation !== undefined && event.correlation !== null) check(typeof event.correlation === "string" && OPAQUE.test(event.correlation), "invalid_field", f + "correlation")
    int(event, "count", f, 0, true)
    enumOf(event, "outcome", OUTCOMES, f, true)
    int(event, "elapsed_ms", f, 0, true)
    enumOf(event, "error", ERRORS, f, true)
    if (typeof event.elapsed_ms === "number") check(event.elapsed_ms <= 3_600_000, "invalid_field", f + "elapsed_ms")
    if (typeof event.count === "number") check(event.count <= 1_000_000, "invalid_field", f + "count")
    check((event.event === "viewer_stage_failed") === (event.error !== undefined && event.error !== null), "invalid_field", f + "error")
    check(event.boot_id === batch.boot_id, "invalid_field", f + "boot_id")
    check(event.event_id === `${event.boot_id}-${event.producer_seq}`, "invalid_field", f + "event_id")
  })
  return batch
}

export interface Ingested {
  status: number
  body: string
  headers: Record<string, string>
}

/**
 * A fake ingest: validates, keeps receipts by `(target, batch_id)` and events by
 * `event_id`, and answers as §2 does. `answer` can take over any request first.
 */
export class FakeCloud {
  requests: { url: string; init: RequestInit; body: string }[] = []
  accepted: Obj[] = []
  receipts = new Map<string, string>()
  eventsById = new Map<string, string>()
  /** Return a response to take over a request, or null to let the route answer. */
  answer: (index: number, body: string) => Ingested | Promise<Ingested> | null = () => null

  fetch = async (url: string | URL | Request, init?: RequestInit): Promise<Response> => {
    const body = String(init?.body ?? "")
    this.requests.push({ url: String(url), init: init ?? {}, body })
    if (init?.signal?.aborted) throw Object.assign(new Error("aborted"), { name: "AbortError" })
    const override = await this.answer(this.requests.length - 1, body)
    const result = override ?? this.ingest(body, init)
    return new Response(result.body, { status: result.status, headers: result.headers })
  }

  ingest(body: string, init?: RequestInit): Ingested {
    const headers = new Headers(init?.headers)
    if (headers.get("x-clawdline-health") !== "viewer/1") return refused(403, "csrf_required", "")
    if (init?.credentials !== "include") return refused(401, "no_session", "")
    let batch: Obj
    try { batch = parseViewerBody(body) } catch (e) {
      if (e instanceof Refusal) return refused(e.status, e.code, e.field)
      return refused(400, "invalid_json", "body")
    }
    const identity = `${batch.target_machine_id ?? "viewer_unattributed"}|${batch.boot_id}|${batch.batch_id}`
    const prior = this.receipts.get(identity)
    if (prior !== undefined) return prior === body ? { status: 200, body: "{}", headers: {} } : refused(409, "idempotency_conflict", "")
    for (const event of batch.events as Obj[]) {
      const seen = this.eventsById.get(String(event.event_id))
      if (seen !== undefined && seen !== JSON.stringify(event)) return refused(409, "idempotency_conflict", "")
    }
    this.receipts.set(identity, body)
    for (const event of batch.events as Obj[]) this.eventsById.set(String(event.event_id), JSON.stringify(event))
    this.accepted.push(batch)
    return { status: 201, body: JSON.stringify({ schema_version: 1, batch_id: batch.batch_id, accepted: (batch.events as unknown[]).length, rejected: [] }), headers: {} }
  }

  /** Every accepted record, in order. */
  records(): Obj[] {
    return this.accepted.flatMap((batch) => batch.events as Obj[])
  }
}

export function refused(status: number, code: string, field: string, headers: Record<string, string> = {}): Ingested {
  return { status, body: JSON.stringify({ error: { code, message: "refused", details: field ? { field } : undefined } }), headers }
}
