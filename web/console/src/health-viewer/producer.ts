// The browser's connection-health producer: one bounded buffer, one worker,
// one HTTPS upload to the Cloud API at a time (docs/admin-connection-health-
// contract.md §11).
//
// It is deliberately not on the relay. What it reports is how this page's
// relay connection, reads and decryption went, so it has to be heard when the
// relay is the thing that broke: it posts to `POST /v1/health/viewer-ingest`
// with the session cookie, on its own `fetch`, and never waits on, or is
// waited on by, anything the page does for the person.
//
// What a hook site pays is `record()`: build a small object, push it, maybe
// arm one timer. It never throws, never awaits and never grows past
// `bufferEvents`; at the bound the oldest record goes and is counted.
//
// What the producer keeps of a batch it sent is the exact body string. A
// retry resends those bytes under the same `batch_id` (the Cloud answers an
// identical replay with its original receipt, a changed one with 409), and
// records that arrive meanwhile wait for the next batch.
import type { ViewerBatch, ViewerObservation, ViewerRecord } from "./record.ts"
// @ts-expect-error -- a `.ts` path, so node's strip-types tests can import this module.
import { MACHINE_ID, VIEWER_HEALTH_HEADER, VIEWER_HEALTH_HEADER_VALUE, VIEWER_INGEST_PATH, randomID, viewerRecord } from "./record.ts"

/**
 * Every bound this producer keeps. The Cloud-side numbers are the contract's
 * (§14.3 and its producer fixture); the rest are this page's and registered as
 * `console.viewer_health_*` in internal/domain/capacity.
 */
export const VIEWER_HEALTH_LIMITS = {
  /** One request body, bytes (the Cloud refuses more with 413 before authenticating). */
  bodyBytes: 16384,
  /** Records in one batch. */
  batchEvents: 50,
  /** Records waiting to be sent; at the bound the oldest is dropped and counted. */
  bufferEvents: 200,
  /** The least time between two batches. */
  sendEveryMs: 60_000,
  /** How long the first record waits for others before a batch is cut. */
  coalesceMs: 5_000,
  /** Requests per clock hour, retries included (the Cloud's per-device bound). */
  hourlyRequests: 60,
  /** Request bytes per clock hour, retries included (the Cloud's per-device bound). */
  hourlyBytes: 262_144,
  /** Tries of one batch, the first included; then it is dropped and counted. */
  attempts: 4,
  /** The waits between tries. */
  backoffMs: [5_000, 20_000, 60_000] as readonly number[],
  /** The longest `Retry-After` honored; a longer one waits this long. */
  retryAfterMaxMs: 3_600_000,
  /** One request's deadline. */
  requestTimeoutMs: 10_000,
  /** The Cloud's `elapsed_ms` ceiling; a longer stage is clamped to it. */
  elapsedMaxMs: 3_600_000,
  /** The Cloud's `count` ceiling. */
  countMax: 1_000_000,
} as const

export type ViewerHealthState =
  /** Recording and uploading. */
  | "running"
  /** The Cloud answered 401: the session is gone; a new session starts a new producer. */
  | "session_gone"
  /** 403, or the ingest is disabled: nothing more is sent until the page reloads. */
  | "stopped_until_reload"
  /** Stopped by the page: signed out, revoked, or the transport left the Cloud. */
  | "stopped"

/** What the producer could not deliver, counted on this page only. */
export interface ViewerHealthCounts {
  recorded: number
  /** Records the buffer let go of at its bound. */
  droppedOldest: number
  /** Observations a hook handed in that the Cloud's schema would refuse. */
  refusedLocally: number
  /** Batches given up on: retries exhausted, 409, 400, 413 of one record. */
  droppedBatches: number
  droppedEvents: number
  /** Batches the Cloud receipted (201 or an identical-replay 200). */
  sentBatches: number
  sentEvents: number
  requests: number
  /** Records discarded by a stop (sign-out, revoke, 401). */
  clearedOnStop: number
}

export interface ViewerHealthStatus {
  state: ViewerHealthState
  bootId: string
  buffered: number
  /** The batch in flight or waiting for its retry, as `batch_id`, or null. */
  pendingBatch: string | null
  counts: ViewerHealthCounts
  /** The last refusal's code and field, never its message or the body. */
  lastRefusal: { status: number; code: string | null; field: string | null } | null
}

export interface ViewerHealthOptions {
  /** The Cloud API's origin, e.g. `https://api.clawdline.com`; the route is fixed. */
  apiOrigin: string
  /** The owned machine the page talks to, if known (`target_machine_id`). */
  targetMachineId?: string | null
  fetch?: typeof fetch
  /** Wall clock, ms since the epoch (display only, and the clock hour). */
  now?: () => number
  /** Page uptime, ms (`performance.now()`). */
  monotonic?: () => number
  setTimeout?: (fn: () => void, ms: number) => unknown
  clearTimeout?: (timer: unknown) => void
  random?: (bytes: number) => Uint8Array
}

interface Pending {
  batchId: string
  body: string
  bytes: number
  records: ViewerRecord[]
  /** Tries so far; a batch the clock hour's budget holds back waits with 0. */
  attempts: number
}

function emptyCounts(): ViewerHealthCounts {
  return { recorded: 0, droppedOldest: 0, refusedLocally: 0, droppedBatches: 0, droppedEvents: 0,
    sentBatches: 0, sentEvents: 0, requests: 0, clearedOnStop: 0 }
}

const HOUR_MS = 3_600_000

/** Byte length of a string as UTF-8, without allocating it. */
function utf8Bytes(text: string): number {
  let n = 0
  for (let i = 0; i < text.length; i++) {
    const c = text.charCodeAt(i)
    if (c < 0x80) n += 1
    else if (c < 0x800) n += 2
    else if (c >= 0xd800 && c <= 0xdbff) { n += 4; i++ }
    else n += 3
  }
  return n
}

export class ViewerHealthProducer {
  readonly bootId: string
  private readonly url: string
  private readonly get: typeof fetch
  private readonly now: () => number
  private readonly mono: () => number
  private readonly arm: (fn: () => void, ms: number) => unknown
  private readonly disarm: (timer: unknown) => void
  private readonly random?: (bytes: number) => Uint8Array
  private state: ViewerHealthState = "running"
  private target: string | null
  private buffer: ViewerRecord[] = []
  private seq = 0
  private batchSeq = 0
  private pending: Pending | null = null
  private inFlight: AbortController | null = null
  private timer: unknown = null
  private timerDue = Infinity
  private nextSendAt = 0
  private firstWaitingAt: number | null = null
  private hour = -1
  private hourRequests = 0
  private hourBytes = 0
  /** A 413 of a batch: the next batches are cut to this many records. */
  private batchCap: number = VIEWER_HEALTH_LIMITS.batchEvents
  /** Bumped by every stop, so a late answer to a request from before it changes nothing. */
  private generation = 0
  private counts = emptyCounts()
  private lastRefusal: ViewerHealthStatus["lastRefusal"] = null

  constructor(options: ViewerHealthOptions) {
    this.url = options.apiOrigin.replace(/\/+$/, "") + VIEWER_INGEST_PATH
    this.get = options.fetch ?? ((input, init) => globalThis.fetch(input, init))
    this.now = options.now ?? (() => Date.now())
    this.mono = options.monotonic ?? (() => globalThis.performance.now())
    this.arm = options.setTimeout ?? ((fn, ms) => globalThis.setTimeout(fn, ms))
    this.disarm = options.clearTimeout ?? ((timer) => globalThis.clearTimeout(timer as ReturnType<typeof setTimeout>))
    this.random = options.random
    this.bootId = randomID("pg-", 8, this.random)
    this.target = this.validTarget(options.targetMachineId)
  }

  /** A fresh opaque trace id for one logical attempt. */
  correlation(): string {
    return randomID("vt-", 6, this.random)
  }

  /** Page uptime in ms, the basis of every `elapsed_ms`. */
  monotonic(): number {
    return this.mono()
  }

  /** The machine later batches name; an invalid id names none. */
  setTargetMachine(machine: string | null | undefined): void {
    this.target = this.validTarget(machine)
  }

  /**
   * One observation. Never throws and never waits: a failure in here is a
   * record lost and counted, not a business path broken.
   */
  record(observation: ViewerObservation): void {
    try {
      if (this.state !== "running") return
      const record = viewerRecord(observation, this.bootId, this.seq + 1, this.now(), this.mono(), VIEWER_HEALTH_LIMITS)
      if (!record) { this.counts.refusedLocally += 1; return }
      this.seq += 1
      this.counts.recorded += 1
      this.buffer.push(record)
      if (this.buffer.length > VIEWER_HEALTH_LIMITS.bufferEvents) {
        this.buffer.shift()
        this.counts.droppedOldest += 1
      }
      if (this.firstWaitingAt === null) this.firstWaitingAt = this.now()
      this.schedule()
    } catch {
      this.counts.refusedLocally += 1
    }
  }

  /** Stop, clear everything held, and abort what is in flight. Idempotent. */
  stop(state: Exclude<ViewerHealthState, "running"> = "stopped"): void {
    if (this.state === "running" || state === "stopped_until_reload") this.state = state
    this.generation += 1
    this.counts.clearedOnStop += this.buffer.length + (this.pending ? this.pending.records.length : 0)
    this.buffer = []
    this.pending = null
    this.firstWaitingAt = null
    if (this.timer !== null) { this.disarm(this.timer); this.timer = null; this.timerDue = Infinity }
    const flight = this.inFlight
    this.inFlight = null
    if (flight) { try { flight.abort() } catch { /* nothing to abort */ } }
  }

  status(): ViewerHealthStatus {
    return {
      state: this.state, bootId: this.bootId, buffered: this.buffer.length,
      pendingBatch: this.pending ? this.pending.batchId : null,
      counts: { ...this.counts }, lastRefusal: this.lastRefusal,
    }
  }

  private validTarget(machine: string | null | undefined): string | null {
    return typeof machine === "string" && MACHINE_ID.test(machine) ? machine : null
  }

  /** Arm the one timer for the next moment there is work, or leave the earlier one. */
  private schedule(): void {
    if (this.state !== "running" || this.inFlight) return
    const now = this.now()
    let due: number
    if (this.pending) due = this.pending.attempts === 0 ? now : this.nextSendAt
    else if (this.buffer.length) due = Math.max(this.nextSendAt, (this.firstWaitingAt ?? now) + VIEWER_HEALTH_LIMITS.coalesceMs)
    else return
    if (!this.hourHasRoom(now)) due = Math.max(due, (Math.floor(now / HOUR_MS) + 1) * HOUR_MS)
    if (this.timer !== null && this.timerDue <= due) return
    if (this.timer !== null) this.disarm(this.timer)
    this.timerDue = due
    this.timer = this.arm(() => { this.timer = null; this.timerDue = Infinity; void this.tick() }, Math.max(0, due - now))
  }

  private hourHasRoom(now: number): boolean {
    const hour = Math.floor(now / HOUR_MS)
    if (hour !== this.hour) { this.hour = hour; this.hourRequests = 0; this.hourBytes = 0 }
    const next = this.pending ? this.pending.bytes : 0
    return this.hourRequests < VIEWER_HEALTH_LIMITS.hourlyRequests &&
      this.hourBytes + next <= VIEWER_HEALTH_LIMITS.hourlyBytes
  }

  /** Cut the next batch from the front of the buffer: at most `batchCap` records and `bodyBytes`. */
  private cut(): Pending | null {
    if (!this.buffer.length) return null
    this.batchSeq += 1
    const batchId = `vb-${this.bootId}-${this.batchSeq}`
    const head: Omit<ViewerBatch, "events"> = {
      schema_version: 1, batch_id: batchId, producer_instance: "viewer", boot_id: this.bootId,
      ...(this.target ? { target_machine_id: this.target } : {}),
      sent_at: new Date(this.now()).toISOString().replace(/\.\d+Z$/, "Z"),
    }
    let take = Math.min(this.batchCap, this.buffer.length)
    let body = ""
    let bytes = 0
    for (;;) {
      body = JSON.stringify({ ...head, events: this.buffer.slice(0, take) })
      bytes = utf8Bytes(body)
      if (bytes <= VIEWER_HEALTH_LIMITS.bodyBytes || take === 1) break
      take = Math.max(1, Math.floor(take * VIEWER_HEALTH_LIMITS.bodyBytes / bytes))
    }
    const records = this.buffer.splice(0, take)
    if (bytes > VIEWER_HEALTH_LIMITS.bodyBytes) {
      // One record over the body bound cannot be sent at all.
      this.counts.droppedBatches += 1
      this.counts.droppedEvents += records.length
      return this.cut()
    }
    this.firstWaitingAt = this.buffer.length ? this.now() : null
    return { batchId, body, bytes, records, attempts: 0 }
  }

  private async tick(): Promise<void> {
    if (this.state !== "running" || this.inFlight) return
    const now = this.now()
    if (!this.pending) {
      if (now < this.nextSendAt) { this.schedule(); return }
      this.pending = this.cut()
      if (!this.pending) return
    }
    if (!this.hourHasRoom(now)) { this.schedule(); return }
    const pending = this.pending
    const generation = this.generation
    const controller = new AbortController()
    this.inFlight = controller
    pending.attempts += 1
    this.hourRequests += 1
    this.hourBytes += pending.bytes
    this.counts.requests += 1
    const deadline = this.arm(() => { try { controller.abort() } catch { /* already done */ } }, VIEWER_HEALTH_LIMITS.requestTimeoutMs)
    let status = 0
    let code: string | null = null
    let field: string | null = null
    let retryAfterMs: number | null = null
    try {
      const response = await this.get(this.url, {
        method: "POST",
        credentials: "include",
        headers: { "content-type": "application/json", [VIEWER_HEALTH_HEADER]: VIEWER_HEALTH_HEADER_VALUE },
        body: pending.body,
        signal: controller.signal,
        keepalive: false,
      })
      status = response.status
      const after = Number(response.headers.get("retry-after"))
      if (Number.isFinite(after) && after >= 0) retryAfterMs = after * 1000
      if (status >= 400) {
        try {
          const answer = await response.json() as { error?: { code?: unknown; details?: { field?: unknown } } }
          code = typeof answer?.error?.code === "string" ? answer.error.code.slice(0, 64) : null
          field = typeof answer?.error?.details?.field === "string" ? answer.error.details.field.slice(0, 64) : null
        } catch { /* a refusal without a readable body is still its status */ }
      }
    } catch {
      status = 0
    } finally {
      this.disarm(deadline)
    }
    if (generation !== this.generation) return
    this.inFlight = null
    this.settle(pending, status, code, field, retryAfterMs)
    this.schedule()
  }

  private settle(pending: Pending, status: number, code: string | null, field: string | null, retryAfterMs: number | null): void {
    const now = this.now()
    if (status === 200 || status === 201) {
      this.counts.sentBatches += 1
      this.counts.sentEvents += pending.records.length
      this.pending = null
      this.nextSendAt = now + VIEWER_HEALTH_LIMITS.sendEveryMs
      return
    }
    if (status >= 400) this.lastRefusal = { status, code, field }
    if (status === 401) { this.stop("session_gone"); return }
    if (status === 403 || (status === 503 && code === "health_ingest_disabled")) { this.stop("stopped_until_reload"); return }
    if (status === 404) {
      // `machine_not_found`: the target is not this account's. The same records
      // go again naming no machine — a different batch identity, so a new id.
      this.target = null
      this.pending = null
      this.buffer.unshift(...pending.records)
      this.trimBuffer()
      this.firstWaitingAt = now
      return
    }
    if (status === 413) {
      this.pending = null
      if (pending.records.length > 1) {
        this.batchCap = Math.max(1, Math.floor(pending.records.length / 2))
        this.buffer.unshift(...pending.records)
        this.trimBuffer()
        this.firstWaitingAt = now
      } else this.drop(pending)
      return
    }
    const retryable = status === 0 || status === 429 || status >= 500
    if (!retryable) { this.pending = null; this.drop(pending); this.nextSendAt = now + VIEWER_HEALTH_LIMITS.sendEveryMs; return }
    if (pending.attempts >= VIEWER_HEALTH_LIMITS.attempts) {
      this.pending = null
      this.drop(pending)
      this.nextSendAt = now + VIEWER_HEALTH_LIMITS.sendEveryMs
      return
    }
    const backoff = VIEWER_HEALTH_LIMITS.backoffMs[Math.min(pending.attempts - 1, VIEWER_HEALTH_LIMITS.backoffMs.length - 1)]
    const wait = Math.min(VIEWER_HEALTH_LIMITS.retryAfterMaxMs, Math.max(backoff, retryAfterMs ?? 0))
    this.nextSendAt = now + wait
  }

  private drop(pending: Pending): void {
    this.counts.droppedBatches += 1
    this.counts.droppedEvents += pending.records.length
  }

  private trimBuffer(): void {
    while (this.buffer.length > VIEWER_HEALTH_LIMITS.bufferEvents) {
      this.buffer.shift()
      this.counts.droppedOldest += 1
    }
  }
}
