import { useCallback, useEffect, useRef, useState } from "react"
import { client } from "../../client.js"
import { nextWord } from "../../next-strings.js"
import { readSettings, writeSettings } from "../../pages/settings/api.js"
import { settingsFailureSentence } from "../../pages/settings/window/failure.js"
import { Block, Note, Row, Switch } from "../../pages/settings/window/controls.js"
import { OWNER_STATUS_PATH, retentionLines, senderReading, type SenderReading } from "./retention.js"

/*
 * The local opt-in for owner-sealed diagnostics, under the diagnostics-key block in Settings →
 * Remote. Default off. Turning it off is the daemon's next re-read of the file: no further seal or
 * send happens, and the lines say plainly that what Cloud already stores is not deleted by it.
 */

/** How often the block re-reads the sender's status while it is shown: one local memory read. */
export const OWNER_RETENTION_POLL_MS = 5_000
const STATUS_TIMEOUT_MS = 5_000

async function readSender(): Promise<SenderReading> {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), STATUS_TIMEOUT_MS)
  try {
    const res = await fetch(client.url(OWNER_STATUS_PATH), { signal: controller.signal })
    if (!res.ok) return { kind: "unknown", why: `HTTP ${res.status}` }
    return senderReading(await res.json().catch(() => null))
  } catch {
    return { kind: "unknown", why: "unreachable" }
  } finally {
    clearTimeout(timer)
  }
}

export function OwnerRetentionBlock() {
  const [on, setOn] = useState<boolean | null>(null)
  const [sender, setSender] = useState<SenderReading | null>(null)
  const [busy, setBusy] = useState(false)
  const [said, setSaid] = useState("")
  const mounted = useRef(true)

  const refresh = useCallback(async () => {
    const [settings, reading] = await Promise.all([readSettings().catch(() => null), readSender()])
    if (!mounted.current) return
    if (settings) setOn(settings.cloud_health_owner_retention === true)
    setSender(reading)
  }, [])

  useEffect(() => {
    mounted.current = true
    void refresh()
    const timer = setInterval(() => { void refresh() }, OWNER_RETENTION_POLL_MS)
    return () => {
      mounted.current = false
      clearInterval(timer)
    }
  }, [refresh])

  const change = (next: boolean) => {
    setBusy(true)
    setSaid("")
    writeSettings({ cloud_health_owner_retention: next })
      .then((snapshot) => {
        if (!mounted.current) return
        setOn(snapshot.cloud_health_owner_retention === true)
        return refresh()
      })
      .catch((error) => {
        if (mounted.current) setSaid(nextWord("ownerRetentionSaveFailed", { why: settingsFailureSentence(error) }))
      })
      .finally(() => { if (mounted.current) setBusy(false) })
  }

  // Until the settings file has been read the switch says nothing, rather than "off".
  const lines = on === null || sender === null ? [] : retentionLines(on, sender)
  return (
    <Block label={nextWord("ownerRetentionHead")} hint={nextWord("ownerRetentionHint")}>
      <Row label={nextWord("ownerRetentionLabel")} first>
        <Switch on={on === true} disabled={busy || on === null} label={nextWord("ownerRetentionLabel")} onChange={change} />
      </Row>
      {lines.map((line, i) => (
        <Note key={line.word + i} dot={line.dot}>{nextWord(line.word, line.vars)}</Note>
      ))}
      {said ? <Note dot="warn">{said}</Note> : null}
    </Block>
  )
}
