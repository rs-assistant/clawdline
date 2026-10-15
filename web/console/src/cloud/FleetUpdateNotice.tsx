import { useEffect, useState } from "react"
import type { CloudMachine } from "./copied.js"
import type { CloudReadClient } from "./relay-reader.js"
import { nextWord } from "../next-strings.js"
import { UPDATE_READ_EVERY_MS, updateBannerVersion, type UpdateRead } from "../machine/update-model.js"
import { readFleetUpdate } from "./fleet-update.js"
import "../machine/update.css"

function dismissedKey(machine: string): string {
  return `clawdline.cloud-update-banner.dismissed.${machine}`
}

function dismissedVersion(machine: string): string | null {
  try { return sessionStorage.getItem(dismissedKey(machine)) } catch { return null }
}

/** The all-machine list still announces a signed release for each readable machine. */
export function FleetUpdateNotice({ client, machine, onChoose }: {
  client: CloudReadClient | null
  machine: CloudMachine
  onChoose: (machine: CloudMachine) => void
}) {
  const [read, setRead] = useState<UpdateRead | null>(null)
  const [dismissed, setDismissed] = useState(() => dismissedVersion(machine.id))

  useEffect(() => {
    setRead(null)
    if (!client || machine.pairing !== "paired" || machine.freshness !== "current") return
    const controller = new AbortController()
    let checking = false
    const refresh = async () => {
      if (document.visibilityState === "hidden" || checking) return
      checking = true
      try {
        const answer = await readFleetUpdate(client, machine.id, controller.signal, true)
        if (!controller.signal.aborted) setRead(answer)
      } finally {
        checking = false
      }
    }
    const visible = () => { if (document.visibilityState !== "hidden") void refresh() }
    void refresh()
    const timer = window.setInterval(() => void refresh(), UPDATE_READ_EVERY_MS)
    document.addEventListener("visibilitychange", visible)
    window.addEventListener("focus", visible)
    return () => {
      controller.abort()
      window.clearInterval(timer)
      document.removeEventListener("visibilitychange", visible)
      window.removeEventListener("focus", visible)
    }
  }, [client, machine.id, machine.pairing, machine.freshness])

  const version = updateBannerVersion(read?.kind === "status" ? read.status : null, dismissed)
  if (!version) return null
  const name = machine.name || machine.label || machine.id
  return <div className="update-strip cloud-fleet-update-notice" role="status" data-machine={machine.id} data-version={version}>
    <span className="update-strip-tag is-new">{nextWord("updateStripTagNew")}</span>
    <span className="update-strip-say">{nextWord("cloudFleetUpdateNotice", { machine: name, version })}</span>
    <button className="update-strip-link is-go" type="button" title={nextWord("cloudFleetUpdateOpen")}
      aria-label={nextWord("cloudFleetUpdateOpen")} onClick={() => onChoose(machine)}>
      {nextWord("updateStripOpen")}
    </button>
    <button className="update-strip-x" type="button" aria-label={nextWord("updateBannerDismiss")} title={nextWord("updateBannerDismiss")}
      onClick={() => {
        setDismissed(version)
        try { sessionStorage.setItem(dismissedKey(machine.id), version) } catch { /* this view only */ }
      }}>×</button>
  </div>
}
