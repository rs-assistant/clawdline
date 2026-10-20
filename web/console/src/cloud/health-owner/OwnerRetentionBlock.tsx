import { useEffect, useRef, useState } from "react"
import { client } from "../../client.js"
import { nextWord } from "../../next-strings.js"
import { readSettings, writeSettings } from "../../pages/settings/api.js"
import { settingsFailureSentence } from "../../pages/settings/window/failure.js"
import { Block, Note, Row, Switch } from "../../pages/settings/window/controls.js"
import {
  OWNER_STATUS_PATH, createRetentionPoller, senderReading, viewLines,
  type RetentionPoller, type RetentionView, type SenderReading,
} from "./retention.js"

/*
 * The local opt-in for owner-sealed diagnostics, under the diagnostics-key block in Settings →
 * Remote. Default off. Turning it off is the daemon's next re-read of the file: no further seal or
 * send happens, and the lines say plainly that what Cloud already stores is not deleted by it.
 */

async function readSender(signal: AbortSignal): Promise<SenderReading> {
  try {
    const res = await fetch(client.url(OWNER_STATUS_PATH), { signal })
    if (!res.ok) return { kind: "unknown", why: `HTTP ${res.status}` }
    return senderReading(await res.json().catch(() => null))
  } catch {
    return { kind: "unknown", why: signal.aborted ? "timeout" : "unreachable" }
  }
}

export function OwnerRetentionBlock() {
  const [view, setView] = useState<RetentionView | null>(null)
  const [busy, setBusy] = useState(false)
  const [said, setSaid] = useState("")
  const poller = useRef<RetentionPoller | null>(null)
  const mounted = useRef(true)

  useEffect(() => {
    mounted.current = true
    const p = createRetentionPoller({ settings: (signal) => readSettings({ signal }), sender: readSender }, setView)
    poller.current = p
    p.start()
    return () => {
      mounted.current = false
      p.stop()
      poller.current = null
    }
  }, [])

  const change = (next: boolean) => {
    const p = poller.current
    if (!p) return
    setBusy(true)
    setSaid("")
    p.beginWrite()
    // A write is never aborted on unmount: whether it reached the file would then be unknown.
    // Its late answer is simply not shown.
    writeSettings({ cloud_health_owner_retention: next })
      .then((snapshot) => {
        p.endWrite(snapshot.cloud_health_owner_retention === true)
      })
      .catch((error) => {
        p.endWrite(null, "write_unconfirmed")
        if (mounted.current) setSaid(nextWord("ownerRetentionSaveFailed", { why: settingsFailureSentence(error) }))
      })
      .finally(() => { if (mounted.current) setBusy(false) })
  }

  // Until the first read has answered the switch says nothing, rather than "off".
  const known = view?.setting.kind === "known" ? view.setting.on : null
  const lines = view === null ? [] : viewLines(view)
  return (
    <Block label={nextWord("ownerRetentionHead")} hint={nextWord("ownerRetentionHint")}>
      <Row label={nextWord("ownerRetentionLabel")} first>
        <Switch on={known === true} disabled={busy || known === null} label={nextWord("ownerRetentionLabel")} onChange={change} />
      </Row>
      {lines.map((line, i) => (
        <Note key={line.word + i} dot={line.dot}>{nextWord(line.word, line.vars)}</Note>
      ))}
      {said ? <Note dot="warn">{said}</Note> : null}
    </Block>
  )
}
