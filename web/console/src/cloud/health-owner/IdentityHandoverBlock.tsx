import { useCallback, useEffect, useRef, useState } from "react"
import { RefusalError, isRefusal } from "@clawdline/core"
import { client } from "../../client.js"
import { nextWord } from "../../next-strings.js"
import { Block, Chip, Note } from "../../pages/settings/window/controls.js"
import { PairingQr } from "../../pages/settings/window/PairingQr.js"
import type { CloudPairing } from "../../pages/settings/cloud.js"
import "./handover.css"

/*
 * This machine's half of the identity handover, in the local Settings window under the pairing
 * card. It is the only place the six-digit comparison can be approved: the routes are this
 * machine's own token only (`internal/transport/http/cloud_identity_pairing.go`), and the daemon
 * hands nothing over until the person here presses "Codes match".
 */

/** `GET/POST/DELETE /v1/cloud/pairing/identity`, as the daemon answers it. */
export type IdentityPairingState = {
  phase: "idle" | "waiting_viewer" | "confirm_code" | "handing_over" | "paired" | "denied" | "failed" | "cancelled"
  pairing_id?: string
  link?: string
  expires_at?: number
  machine_fingerprint?: string
  code?: string
  viewer_fingerprint?: string
  viewer_device_id?: string
  content_key_epoch?: number
  error?: string
}

/** How often the block re-reads a live run: the daemon polls Cloud once a second. */
export const IDENTITY_STATE_POLL_MS = 1_000

async function call(method: string, path: string, body?: unknown): Promise<IdentityPairingState> {
  const res = await fetch(client.url(path), {
    method,
    headers: body === undefined ? undefined : { "content-type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  let parsed: unknown = null
  try { parsed = await res.json() } catch { parsed = null }
  if (!res.ok) {
    if (isRefusal(parsed)) throw new RefusalError(res.status, parsed, path)
    throw new Error(nextWord("handoverLocalFailed", { code: String(res.status) }))
  }
  return parsed as IdentityPairingState
}

const PATH = "/v1/cloud/pairing/identity"
const live = (phase: string) => phase === "waiting_viewer" || phase === "confirm_code" || phase === "handing_over"

function grouped(code: string): string {
  return code.length === 6 ? code.slice(0, 3) + " " + code.slice(3) : code
}

export function IdentityHandoverBlock() {
  const [state, setState] = useState<IdentityPairingState>({ phase: "idle" })
  const [busy, setBusy] = useState(false)
  const [said, setSaid] = useState("")
  const mounted = useRef(true)
  useEffect(() => { mounted.current = true; return () => { mounted.current = false } }, [])

  const run = useCallback((what: () => Promise<IdentityPairingState>) => {
    setBusy(true)
    setSaid("")
    void what().then(
      (next) => { if (mounted.current) { setState(next); setBusy(false) } },
      (error: unknown) => {
        if (!mounted.current) return
        setBusy(false)
        setSaid(error instanceof Error ? error.message : String(error))
      },
    )
  }, [])

  useEffect(() => { run(() => call("GET", PATH)) }, [run])
  useEffect(() => {
    if (!live(state.phase)) return
    const timer = setInterval(() => {
      void call("GET", PATH).then((next) => { if (mounted.current) setState(next) }, () => undefined)
    }, IDENTITY_STATE_POLL_MS)
    return () => clearInterval(timer)
  }, [state.phase])

  const begin = () => run(() => call("POST", PATH))
  const decide = (approve: boolean) => run(() => call("POST", PATH + "/decision", { pairing_id: state.pairing_id, approve }))
  // The QR card reads the legacy invitation's shape; the identity link is drawn the same way.
  const qr: CloudPairing = {
    phase: state.phase === "waiting_viewer" ? "waiting" : state.phase === "failed" ? "failed" : "idle",
    invitation_id: state.pairing_id,
    link: state.link,
    expires_at: state.expires_at,
    error: state.error,
  } as CloudPairing

  return (
    <Block label={nextWord("handoverLocalHead")} hint={nextWord("handoverLocalHint")}>
      {state.phase === "waiting_viewer" ? (
        <>
          <Note dot="live">{nextWord("handoverLocalWaiting")}</Note>
          <PairingQr pairing={qr} busy={busy} onRenew={begin} />
          {state.link ? <Note mono dot="idle">{state.link}</Note> : null}
        </>
      ) : null}
      {state.phase === "confirm_code" && state.code ? (
        <>
          <Note dot="live">{nextWord("handoverLocalCompare")}</Note>
          <p className="am-handover-code" aria-label={nextWord("handoverCodeLabel")}>{grouped(state.code)}</p>
          {state.viewer_fingerprint ? <Note dot="idle">{nextWord("handoverLocalViewerKey", { key: state.viewer_fingerprint })}</Note> : null}
          <div className="sw-note">
            <span className="sw-dot idle" aria-hidden="true" />
            <span className="sw-note-text">
              <Chip disabled={busy} onClick={() => decide(true)}>{nextWord("handoverMatch")}</Chip>
              <Chip disabled={busy} onClick={() => decide(false)}>{nextWord("handoverNoMatch")}</Chip>
            </span>
          </div>
        </>
      ) : null}
      {state.phase === "handing_over" ? <Note dot="live">{nextWord("handoverWorking")}</Note> : null}
      {state.phase === "paired" ? (
        <Note dot="live">{nextWord("handoverLocalDone", { device: state.viewer_device_id ?? "", epoch: state.content_key_epoch ?? 0 })}</Note>
      ) : null}
      {state.phase === "denied" ? <Note dot="warn">{nextWord("handoverLocalDenied")}</Note> : null}
      {state.phase === "failed" ? <Note dot="warn">{nextWord("handoverFailOther", { code: state.error ?? "" })}</Note> : null}
      {said ? <Note dot="warn">{said}</Note> : null}
      <div className="sw-note">
        <span className="sw-dot idle" aria-hidden="true" />
        <span className="sw-note-text">
          <Chip disabled={busy} onClick={begin}>{live(state.phase) ? nextWord("handoverLocalAgain") : nextWord("handoverLocalStart")}</Chip>
          {live(state.phase) ? <Chip disabled={busy} onClick={() => run(() => call("DELETE", PATH))}>{nextWord("handoverCancel")}</Chip> : null}
        </span>
      </div>
    </Block>
  )
}
