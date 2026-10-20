// Turning the browser's connection-health producer on for one signed-in page
// (docs/admin-connection-health-contract.md §11.4).
//
// Off is the default: nothing records and nothing is sent unless the page's
// configuration says `enabled: true` and names the Cloud API origin. The one
// running producer is installed into the copied transport's hook
// (`legacy/js/net/viewer-health-hook.js`); starting another stops the first,
// and stopping clears what it held.
import type { ViewerHealthOptions, ViewerHealthState, ViewerHealthStatus } from "./producer.ts"
// @ts-expect-error -- a `.ts` path, so node's strip-types tests can import this module.
import { ViewerHealthProducer } from "./producer.ts"
import { installViewerHealthObserver } from "../legacy/js/net/viewer-health-hook.js"

export interface ViewerHealthConfig extends ViewerHealthOptions {
  /** Explicitly on. Anything else is off. */
  enabled: boolean
}

export interface ViewerHealthHandle {
  /** The producer's own account of what it holds and lost. */
  status(): ViewerHealthStatus
  setTargetMachine(machine: string | null | undefined): void
  /** Sign-out, revocation or leaving the Cloud: stop, clear, uninstall. */
  stop(state?: Exclude<ViewerHealthState, "running">): void
}

let current: { producer: ViewerHealthProducer; uninstall: () => void } | null = null

/** Start the page's producer, or answer null when the configuration does not turn it on. */
export function startViewerHealth(config: ViewerHealthConfig | null | undefined): ViewerHealthHandle | null {
  stopViewerHealth()
  if (!config || config.enabled !== true || typeof config.apiOrigin !== "string" || !/^https?:\/\//.test(config.apiOrigin)) {
    return null
  }
  const producer: ViewerHealthProducer = new ViewerHealthProducer(config)
  const uninstall = installViewerHealthObserver(producer)
  const entry = { producer, uninstall }
  current = entry
  return {
    status: () => producer.status(),
    setTargetMachine: (machine) => producer.setTargetMachine(machine),
    stop: (state) => {
      producer.stop(state)
      uninstall()
      if (current === entry) current = null
    },
  }
}

/** Stop and clear whatever producer this page runs; nothing running is fine. */
export function stopViewerHealth(state: Exclude<ViewerHealthState, "running"> = "stopped"): void {
  const entry = current
  current = null
  if (!entry) return
  entry.producer.stop(state)
  entry.uninstall()
}
