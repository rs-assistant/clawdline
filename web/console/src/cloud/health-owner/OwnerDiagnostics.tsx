import { useCallback, useEffect, useState, useSyncExternalStore } from "react"
import { byteWords, nextWord } from "../../next-strings.js"
import type { NextWord } from "../../next-strings.js"
import { SealedError } from "./crypto.js"
import { ownerDiagnostics, type OpenedDiagnostic, type SealedAttachmentMeta } from "./client.js"
import { evidenceGaps, exportSnapshot } from "./snapshot.js"
import { ownerDiagnosticsSource, watchOwnerDiagnosticsSource } from "./source.js"

/**
 * The paired owner's view of one machine's sealed diagnostics, in the machine sheet.
 *
 * It reads Cloud's own sealed routes, not the business relay, so it works while the machine
 * is offline. Authority is this browser's device proof and the key it holds; it is not behind
 * the superuser gate and never infers anything from `/su`. Decrypting and exporting happen
 * only when the person presses the button.
 */

const INTEGRITY = new Set(["aad_mismatch", "hash_mismatch", "wrong_account", "wrong_machine", "wrong_attachment",
  "wrong_epoch", "tag_mismatch", "bad_response", "snapshot_invalid"])

export function refusalWords(error: unknown): string {
  if (!(error instanceof SealedError)) return nextWord("ownerDiagErrUnavailable")
  const code = error.detail ?? error.code
  if (INTEGRITY.has(error.code)) return nextWord("ownerDiagErrIntegrity", { code: error.code })
  switch (error.code) {
    case "key_handover_required": return nextWord("ownerDiagErrKeyHandover")
    case "device_epoch_unknown": return nextWord("ownerDiagErrDeviceEpoch")
    case "forbidden": return nextWord("ownerDiagErrForbidden", { code })
    case "not_signed_in": return nextWord("ownerDiagErrSignedOut")
    case "unknown_attachment": return nextWord("ownerDiagErrUnknown")
    case "expired": return nextWord("ownerDiagErrExpired")
    case "deleted": return nextWord("ownerDiagErrDeleted")
    case "conflict": return nextWord("ownerDiagErrConflict", { code })
    case "stale": return nextWord("ownerDiagErrStale")
    case "busy": return nextWord("ownerDiagErrBusy")
    default: return nextWord("ownerDiagErrUnavailable")
  }
}

const GAP_WORDS: Record<string, NextWord> = {
  truncated: "ownerDiagGapTruncated", lost: "ownerDiagGapLost", lost_unknown: "ownerDiagGapLostUnknown",
  no_coverage: "ownerDiagGapNoCoverage", coverage_truncated: "ownerDiagGapCoverageTruncated", coverage_lost: "ownerDiagGapCoverageLost",
}

const STATE_WORDS: Record<SealedAttachmentMeta["state"], NextWord> = {
  live: "ownerDiagStateLive", expired: "ownerDiagStateExpired", deleted: "ownerDiagStateDeleted",
}

function when(ms: number | string | null): string {
  if (ms === null) return nextWord("ownerDiagUnknown")
  const n = typeof ms === "string" ? Number(ms) : ms
  if (!Number.isFinite(n)) return nextWord("ownerDiagUnknown")
  try { return new Date(n).toLocaleString() } catch { return String(ms) }
}

/** Nanoseconds as exact text; seconds are shown alongside only when they fit a Number exactly. */
function duration(ns: string | null): string {
  if (ns === null) return nextWord("ownerDiagUnknown")
  const big = BigInt(ns)
  const ms = big / 1_000_000n
  return ms <= BigInt(Number.MAX_SAFE_INTEGER) ? (Number(ms) / 1000).toFixed(1) + " s" : ns + " ns"
}

function useStoreTick(): number {
  const [tick, setTick] = useState(0)
  useEffect(() => ownerDiagnostics.watch(() => setTick((t) => t + 1)), [])
  return tick
}

export function OwnerDiagnostics({ machineID }: { machineID: string }) {
  const source = useSyncExternalStore(watchOwnerDiagnosticsSource, ownerDiagnosticsSource, ownerDiagnosticsSource)
  useStoreTick()
  const [loading, setLoading] = useState(false)
  const [listError, setListError] = useState<string | null>(null)
  const [opening, setOpening] = useState<string | null>(null)
  const [openErrors, setOpenErrors] = useState<Record<string, string>>({})
  const [said, setSaid] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    if (!source) return
    setLoading(true)
    setListError(null)
    try { await ownerDiagnostics.refresh(source) } catch (error) { setListError(refusalWords(error)) }
    setLoading(false)
  }, [source])

  useEffect(() => { void refresh() }, [refresh])

  if (!source) return <p className="am-say">{nextWord("ownerDiagErrSignedOut")}</p>
  const list = ownerDiagnostics.snapshotList()
  const rows = (list?.attachments ?? []).filter((a) => a.machineID === machineID)

  const open = async (a: SealedAttachmentMeta, revision: number, epoch: number) => {
    const id = a.attachmentID + "#" + revision
    setOpening(id)
    setOpenErrors((e) => { const next = { ...e }; delete next[id]; return next })
    try { await ownerDiagnostics.open(source, machineID, a.attachmentID, revision, epoch) } catch (error) {
      setOpenErrors((e) => ({ ...e, [id]: refusalWords(error) }))
    }
    setOpening(null)
  }

  const copy = async (opened: OpenedDiagnostic) => {
    const text = exportSnapshot(opened.snapshot, opened)
    try { await navigator.clipboard.writeText(text); setSaid(nextWord("ownerDiagExported")) } catch { setSaid(nextWord("ownerDiagErrUnavailable")) }
  }

  return (
    <div className="am-stack">
      <p className="am-say">{nextWord("ownerDiagLead")}</p>
      <div className="am-pair-buttons">
        <button type="button" className="am-ghost am-small" onClick={() => void refresh()} disabled={loading}>{nextWord("ownerDiagRefresh")}</button>
      </div>
      {loading && !list ? <p className="am-say" role="status">{nextWord("ownerDiagLoading")}</p> : null}
      {listError ? <p className="am-say" role="alert">{listError}</p> : null}
      {list && rows.length === 0 && !listError ? <p className="am-say">{nextWord("ownerDiagEmpty")}</p> : null}
      {list?.truncated ? <p className="am-say">{nextWord("ownerDiagTruncated")}</p> : null}
      {rows.map((a) => {
        const latest = a.revisions.find((r) => r.revision === a.latestRevision) ?? a.revisions[a.revisions.length - 1]
        if (!latest) return null
        const id = a.attachmentID + "#" + latest.revision
        const opened = ownerDiagnostics.openedFor(a.attachmentID, latest.revision)
        const gaps = opened ? evidenceGaps(opened.snapshot) : []
        return (
          <section className="am-card" key={a.attachmentID} aria-label={a.subjectID}>
            <div className="am-card-row">
              <span className="am-card-text">
                <span>{nextWord(STATE_WORDS[a.state])} · {nextWord("ownerDiagRow", { subject: a.subjectKind + " " + a.subjectID, revision: latest.revision, bytes: byteWords(latest.ciphertextBytes), epoch: latest.contentKeyEpoch })}</span>
                <span className="am-sub">{nextWord("ownerDiagReceived", { time: when(latest.receivedAtMs) })} · {nextWord("ownerDiagRetention", { time: when(latest.expiresAtMs) })} · {a.bootID}</span>
              </span>
              {latest.readable && !opened ? (
                <button type="button" className="am-ghost am-small" disabled={opening === id}
                  onClick={() => void open(a, latest.revision, latest.contentKeyEpoch)}>
                  {opening === id ? nextWord("ownerDiagOpening") : nextWord("ownerDiagOpen")}
                </button>
              ) : null}
            </div>
            {openErrors[id] ? <p className="am-say" role="alert">{openErrors[id]}</p> : null}
            {opened ? (
              <div className="am-card-text">
                <span>{nextWord("ownerDiagStage")}: {opened.snapshot.stage || nextWord("ownerDiagUnknown")}</span>
                <span>{nextWord("ownerDiagLocation")}: {opened.snapshot.location || nextWord("ownerDiagUnknown")}</span>
                <span>{nextWord("ownerDiagAge")}: {duration(opened.snapshot.ageNs)} · {nextWord("ownerDiagDeadline")}: {duration(opened.snapshot.deadlineNs)}</span>
                <span>{nextWord("ownerDiagBuild")}: {opened.snapshot.build || nextWord("ownerDiagUnknown")} · {nextWord("ownerDiagCorrelation")}: {opened.snapshot.correlationID || nextWord("ownerDiagUnknown")}</span>
                <span>{nextWord("ownerDiagObserved")}: {when(opened.snapshot.observedAtMs)}</span>
                {opened.snapshot.coverage.map((c, index) => (
                  <span className="am-sub" key={index}>{nextWord("ownerDiagSource")}: {c.source} · {when(c.fromMs)} – {when(c.toMs)}</span>
                ))}
                <span className="am-sub">{gaps.length
                  ? nextWord("ownerDiagGaps", { gaps: gaps.map((g) => nextWord(GAP_WORDS[g] ?? "ownerDiagUnknown")).join(", ") })
                  : nextWord("ownerDiagNoGaps")}</span>
                <button type="button" className="am-ghost am-small" onClick={() => void copy(opened)}>{nextWord("ownerDiagExport")}</button>
              </div>
            ) : null}
          </section>
        )
      })}
      {said ? <p className="am-say" role="status">{said}</p> : null}
    </div>
  )
}
