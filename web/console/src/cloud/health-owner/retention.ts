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
