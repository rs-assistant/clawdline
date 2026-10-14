import { useEffect, useState } from "react"
import type { CloudMachine } from "./copied.js"
import type { CloudReadClient } from "./relay-reader.js"
import { nextWord } from "../next-strings.js"
import { fleetUpdateFacts, readFleetUpdate } from "./fleet-update.js"
import { applyMoving, UPDATE_FOLLOW_EVERY_MS, UPDATE_READ_EVERY_MS, type UpdateRead } from "../machine/update-model.js"
import "./fleet-update.css"

/** Version, update availability, compatibility and last result for one machine. */
export function FleetUpdateRow({ client, machine, appVersion }: {
  client: CloudReadClient | null
  machine: CloudMachine
  appVersion?: string
}) {
  const [read, setRead] = useState<UpdateRead | null>(null)
  useEffect(() => {
    setRead(null)
    if (!client || machine.pairing !== "paired" || machine.freshness !== "current") return
    const controller = new AbortController()
    let next: ReturnType<typeof setTimeout> | undefined
    async function refresh() {
      const answer = await readFleetUpdate(client!, machine.id, controller.signal)
      if (controller.signal.aborted) return
      setRead(answer)
      const moving = answer.kind === "status" && applyMoving(answer.status)
      next = setTimeout(() => void refresh(), moving ? UPDATE_FOLLOW_EVERY_MS : UPDATE_READ_EVERY_MS)
    }
    void refresh()
    return () => {
      controller.abort()
      if (next) clearTimeout(next)
    }
  }, [client, machine.id, machine.pairing, machine.freshness])

  const facts = fleetUpdateFacts(machine, appVersion, read)
  const compatible = facts.compatibility === "supported" ? nextWord("fleetUpdateCompatible")
    : facts.compatibility === "older" ? nextWord("fleetUpdateOlder")
      : facts.compatibility === "offline" ? nextWord("fleetUpdateOffline")
        : facts.compatibility === "unpaired" ? nextWord("fleetUpdateUnpaired")
          : read?.kind === "unreachable" ? nextWord("fleetUpdateUnanswered")
            : read?.kind === "refused" ? nextWord("fleetUpdateRefused")
            : nextWord("fleetUpdateChecking")
  const state = facts.state === "update_available" && facts.latest
    ? nextWord("fleetUpdateAvailable", { version: facts.latest })
    : facts.state === "current" ? nextWord("fleetUpdateCurrent")
      : facts.state === "ahead" ? nextWord("fleetUpdateAhead")
        : facts.state === "unknown" || facts.state === "differs" ? nextWord("fleetUpdateUnknown")
          : null
  const result = facts.result === "healthy" && facts.to
    ? nextWord("fleetUpdateHealthy", { version: facts.to })
    : facts.result === "rolled_back"
      ? nextWord("fleetUpdateRolledBack", { code: facts.errorCode || "unknown" })
      : facts.result === "failed"
        ? nextWord("fleetUpdateFailed", { code: facts.errorCode || "unknown" })
        : facts.result && facts.result !== "idle"
          ? nextWord("fleetUpdateMoving")
          : null

  return (
    <span className="cloud-fleet-update" data-compatibility={facts.compatibility} data-update-result={facts.result ?? undefined}>
      <span>{machine.freshness === "current" ? nextWord("fleetVersion") : nextWord("fleetLastVersion")}: {facts.running || nextWord("updateUnknownVersion")}</span>
      {facts.latest && <span>{nextWord("updateLatest")}: {facts.latest}</span>}
      <span>{nextWord("fleetCompatibility")}: {compatible}</span>
      {state && <span>{state}</span>}
      {result && <span className={facts.result === "rolled_back" || facts.result === "failed" ? "cloud-fleet-update-problem" : ""}>{result}</span>}
    </span>
  )
}
