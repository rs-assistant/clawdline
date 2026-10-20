/*
 * The local opt-in for owner-sealed diagnostics, `cloud_health_owner_retention`, as the Settings
 * window shows it (docs/health-identity-handover.md). The daemon re-reads the setting before every
 * seal and every send, so the switch writing `false` is what cancels the next send; this file only
 * decides what the window may truthfully say about it, from two local reads:
 *
 * - `GET /v1/settings`: the switch's value;
 * - `GET /v1/connection-health/status`: `upload.status.owner_diagnostics`, the sender's own
 *   coverage. A status that could not be read is unknown, never "off" or "0 sent".
 *
 * It is consent for encrypted owner evidence only. The metadata switch `cloud_health_upload` is
 * a different setting and is never written here.
 */

export const OWNER_RETENTION_SETTING = "cloud_health_owner_retention"
export const OWNER_STATUS_PATH = "/v1/connection-health/status"

/** `upload.status.owner_diagnostics` (`internal/adapters/healthupload` `OwnerDiagnostics`). */
export interface OwnerDiagnosticsStatus {
  coverage: "off" | "waiting" | "active" | "partial" | "failed" | string
  code?: string
  cloud_code?: string
  stored: number
  refused: number
  dropped: number
  queued: number
  last_at_ms?: number
  attachments: number | null
  store_bytes: number | null
}

/** What the status read said: the sender's status, no sender at all, or no answer. */
export type SenderReading =
  | { kind: "sender"; status: OwnerDiagnosticsStatus }
  | { kind: "no_sender" }
  | { kind: "unknown"; why: string }

const count = (v: unknown): v is number => typeof v === "number" && Number.isSafeInteger(v) && v >= 0
const measured = (v: unknown): number | null => (count(v) ? v : null)

/**
 * The owner-diagnostics part of a status body. A daemon without the sender omits it
 * (`omitempty`); a body that is not a status, or names it in a shape this page does not know, is
 * unknown rather than a guess.
 */
export function senderReading(body: unknown): SenderReading {
  const upload = (body as { upload?: { state?: unknown; status?: { owner_diagnostics?: unknown } } } | null)?.upload
  if (!upload || typeof upload !== "object" || typeof upload.state !== "string") return { kind: "unknown", why: "bad_status" }
  const raw = upload.status?.owner_diagnostics
  if (raw === undefined || raw === null) return { kind: "no_sender" }
  const d = raw as Record<string, unknown>
  if (typeof d.coverage !== "string" || !count(d.stored) || !count(d.refused) || !count(d.dropped)) {
    return { kind: "unknown", why: "bad_status" }
  }
  return {
    kind: "sender",
    status: {
      coverage: d.coverage, code: typeof d.code === "string" ? d.code : undefined,
      cloud_code: typeof d.cloud_code === "string" ? d.cloud_code : undefined,
      stored: d.stored, refused: d.refused, dropped: d.dropped, queued: count(d.queued) ? d.queued : 0,
      last_at_ms: count(d.last_at_ms) ? d.last_at_ms : undefined,
      attachments: measured(d.attachments), store_bytes: measured(d.store_bytes),
    },
  }
}

export type RetentionWord =
  | "ownerRetentionOff" | "ownerRetentionOffKept" | "ownerRetentionWaiting" | "ownerRetentionActive"
  | "ownerRetentionPartial" | "ownerRetentionKeyNeeded" | "ownerRetentionFailed"
  | "ownerRetentionNoSender" | "ownerRetentionUnknown" | "ownerRetentionLocal" | "ownerRetentionLocalUnknown"
  | "ownerRetentionSettingUnknown"

export interface RetentionLine {
  word: RetentionWord
  vars?: Record<string, string | number>
  dot: "live" | "idle" | "warn"
}

function bytesText(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KiB`
  return `${(n / (1024 * 1024)).toFixed(1)} MiB`
}

/**
 * The lines under the switch. `on` is the setting as the file has it (absent is off). Whatever
 * else stops a send (Cloud off, no key yet) is the sender's own coverage and code, not a guess here.
 */
export function retentionLines(on: boolean, sender: SenderReading): RetentionLine[] {
  const lines: RetentionLine[] = []
  if (sender.kind === "unknown") {
    lines.push({ word: on ? "ownerRetentionUnknown" : "ownerRetentionOff", vars: { why: sender.why }, dot: on ? "warn" : "idle" })
    if (!on) lines.push({ word: "ownerRetentionUnknown", vars: { why: sender.why }, dot: "warn" })
    return lines
  }
  if (sender.kind === "no_sender") {
    lines.push({ word: on ? "ownerRetentionNoSender" : "ownerRetentionOff", dot: on ? "warn" : "idle" })
    return lines
  }
  const st = sender.status
  // Anything the sender ever had receipted, or still holds, may be in Cloud: off then says what
  // the switch does not do, rather than letting "off" read as "deleted".
  const anything = st.stored > 0 || (st.attachments ?? 0) > 0
  if (!on) {
    lines.push({ word: anything ? "ownerRetentionOffKept" : "ownerRetentionOff", dot: "idle" })
  } else if (st.coverage === "off") {
    // The file says on and the sender has not read it yet: its next pass re-reads the file.
    lines.push({ word: "ownerRetentionWaiting", dot: "idle" })
  } else if (st.coverage === "waiting") {
    lines.push({ word: "ownerRetentionWaiting", dot: "idle" })
  } else if (st.coverage === "active") {
    lines.push({ word: "ownerRetentionActive", vars: { stored: st.stored }, dot: "live" })
  } else if (st.coverage === "partial") {
    lines.push({ word: "ownerRetentionPartial", vars: { stored: st.stored, dropped: st.dropped, refused: st.refused }, dot: "warn" })
  } else if (st.coverage === "failed" && st.code === "key_handover_required") {
    lines.push({ word: "ownerRetentionKeyNeeded", dot: "warn" })
  } else {
    const code = st.cloud_code ? `${st.code ?? "failed"}: ${st.cloud_code}` : st.code ?? st.coverage
    lines.push({ word: "ownerRetentionFailed", vars: { code }, dot: "warn" })
  }
  if (st.attachments === null || st.store_bytes === null) {
    if (on || anything) lines.push({ word: "ownerRetentionLocalUnknown", dot: "idle" })
  } else if (st.attachments > 0) {
    lines.push({ word: "ownerRetentionLocal", vars: { count: st.attachments, bytes: bytesText(st.store_bytes) }, dot: "idle" })
  }
  return lines
}

/**
 * The block's two bounds (internal/domain/capacity register_owner_retention.go;
 * TestOwnerRetentionBoundsMatchTheBlock holds both sides together):
 *
 * - `pollMs`: how often a shown block reads again. A tick that finds a read still out is skipped,
 *   so there is never more than one refresh, and never a pile of them behind a hung daemon.
 * - `requestMs`: one refresh, both reads together. Past it both are aborted and whatever has not
 *   answered is shown as unknown ("timeout").
 */
export const OWNER_RETENTION_LIMITS = {
  pollMs: 5_000,
  requestMs: 5_000,
} as const

/** The switch's value as the settings file has it, or why it could not be read. */
export type SettingReading = { kind: "known"; on: boolean } | { kind: "unknown"; why: string }

export interface RetentionView {
  setting: SettingReading
  sender: SenderReading
}

/**
 * The lines for one view. A switch whose value could not be read says so, and the sender's lines
 * that depend on it are not guessed: only the sender's own unknown, which does not, is added.
 */
export function viewLines(view: RetentionView): RetentionLine[] {
  if (view.setting.kind === "known") return retentionLines(view.setting.on, view.sender)
  const lines: RetentionLine[] = [{ word: "ownerRetentionSettingUnknown", vars: { why: view.setting.why }, dot: "warn" }]
  if (view.sender.kind === "unknown") lines.push({ word: "ownerRetentionUnknown", vars: { why: view.sender.why }, dot: "warn" })
  return lines
}

export interface RetentionReaders {
  /** `GET /v1/settings`; it must give up when `signal` aborts. */
  settings(signal: AbortSignal): Promise<{ cloud_health_owner_retention?: boolean | null }>
  /** `GET /v1/connection-health/status`, already folded into a reading; it must not throw. */
  sender(signal: AbortSignal): Promise<SenderReading>
}

export interface RetentionTimers {
  setTimeout(fn: () => void, ms: number): unknown
  clearTimeout(handle: unknown): void
  setInterval(fn: () => void, ms: number): unknown
  clearInterval(handle: unknown): void
}

const realTimers: RetentionTimers = {
  setTimeout: (fn, ms) => globalThis.setTimeout(fn, ms),
  clearTimeout: (h) => globalThis.clearTimeout(h as ReturnType<typeof globalThis.setTimeout>),
  setInterval: (fn, ms) => globalThis.setInterval(fn, ms),
  clearInterval: (h) => globalThis.clearInterval(h as ReturnType<typeof globalThis.setInterval>),
}

export interface RetentionPoller {
  /** First read now, then one every `pollMs`. */
  start(): void
  /** Unmounted: aborts the read in flight and says nothing more, even if it answers later. */
  stop(): void
  /** Read again as soon as the slot is free; one request coalesces with any already waiting. */
  refresh(): void
  /**
   * A switch write is starting: the read in flight is aborted and made obsolete, and no tick reads
   * until `endWrite`, so a read from before the write cannot overwrite the written value.
   */
  beginWrite(): void
  /** The write answered with the file's new value, or failed (`null`): either way read again. */
  endWrite(written: boolean | null, why?: string): void
}

/**
 * One block's reads, single-flight and bounded. The underlying reads are aborted at the deadline,
 * on `stop` and on `beginWrite`, but the slot stays taken until both have actually settled: a
 * read that ignores its signal blocks further reads (the view says unknown) rather than leaving a
 * second one running beside it.
 */
export function createRetentionPoller(
  readers: RetentionReaders,
  show: (view: RetentionView) => void,
  options: { pollMs?: number; requestMs?: number; timers?: RetentionTimers } = {},
): RetentionPoller {
  const pollMs = options.pollMs ?? OWNER_RETENTION_LIMITS.pollMs
  const requestMs = options.requestMs ?? OWNER_RETENTION_LIMITS.requestMs
  const timers = options.timers ?? realTimers
  let generation = 0
  let inFlight: AbortController | null = null
  let pending = false
  let writing = false
  let stopped = false
  let interval: unknown = null
  let setting: SettingReading = { kind: "unknown", why: "not_read" }
  let sender: SenderReading = { kind: "unknown", why: "not_read" }
  let shown = false

  const emit = () => {
    if (stopped) return
    shown = true
    show({ setting, sender })
  }

  const run = () => {
    if (stopped || writing) return
    if (inFlight) {
      pending = true
      return
    }
    pending = false
    const mine = ++generation
    const controller = new AbortController()
    inFlight = controller
    let settingDone = false
    let senderDone = false
    const current = () => mine === generation && !stopped
    const deadline = timers.setTimeout(() => {
      if (!current()) return
      // Whatever has not answered by now is unknown; nothing older is kept as if it were current.
      if (!settingDone) setting = { kind: "unknown", why: "timeout" }
      if (!senderDone) sender = { kind: "unknown", why: "timeout" }
      controller.abort()
      emit()
    }, requestMs)
    const settingRead = readers.settings(controller.signal).then(
      (snapshot) => {
        settingDone = true
        if (current() && !controller.signal.aborted) setting = { kind: "known", on: snapshot?.cloud_health_owner_retention === true }
      },
      (error: unknown) => {
        settingDone = true
        if (current() && !controller.signal.aborted) setting = { kind: "unknown", why: failureCode(error) }
      },
    )
    const senderRead = Promise.resolve()
      .then(() => readers.sender(controller.signal))
      .then(
        (reading) => {
          senderDone = true
          if (current() && !controller.signal.aborted) sender = reading
        },
        () => {
          senderDone = true
          if (current() && !controller.signal.aborted) sender = { kind: "unknown", why: "unreachable" }
        },
      )
    void Promise.all([settingRead, senderRead]).then(() => {
      timers.clearTimeout(deadline)
      if (current() && !controller.signal.aborted) emit()
      if (inFlight === controller) inFlight = null
      if (pending && !stopped && !writing) run()
    })
  }

  const obsolete = () => {
    generation++
    inFlight?.abort()
  }

  return {
    start() {
      if (stopped || interval !== null) return
      run()
      interval = timers.setInterval(() => {
        // A tick is a request like any other: skipped, not queued, while a read is still out.
        if (!inFlight) run()
      }, pollMs)
    },
    stop() {
      stopped = true
      pending = false
      if (interval !== null) timers.clearInterval(interval)
      interval = null
      obsolete()
    },
    refresh() {
      run()
    },
    beginWrite() {
      writing = true
      pending = false
      obsolete()
    },
    endWrite(written, why) {
      writing = false
      // A failed write may or may not have reached the file; until the next read says, unknown.
      setting = written === null ? { kind: "unknown", why: why ?? "write_failed" } : { kind: "known", on: written }
      if (shown || written !== null) emit()
      if (inFlight) pending = true
      else run()
    },
  }
}

/** A short, stable reason for a failed settings read; never the error's own text. */
function failureCode(error: unknown): string {
  const name = (error as { name?: unknown } | null)?.name
  if (name === "RefusalError") {
    const code = (error as { code?: unknown }).code
    return typeof code === "string" && /^[a-z0-9_]{1,64}$/.test(code) ? code : "refused"
  }
  return "unreachable"
}
