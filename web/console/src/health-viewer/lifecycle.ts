// When the hosted page's connection-health producer runs
// (docs/admin-connection-health-contract.md §11.4).
//
// Two conditions, both explicit. The build must say so: `VITE_VIEWER_HEALTH=on` at build time,
// anything else (including unset) is off, so no deployed page records anything by default. And the
// page must be signed in to an account on the Cloud transport: a sign-out, a revocation or leaving
// the Cloud stops the producer and clears what it held, and another account gets a fresh producer
// rather than the old one's buffer.
import type { ViewerHealthState } from "./producer.ts"
// @ts-expect-error -- a `.ts` path, so node's strip-types tests can import this module.
import { startViewerHealth, type ViewerHealthHandle } from "./install.ts"

function buildFlag(): boolean {
  try {
    const env = (import.meta as unknown as { env?: Record<string, unknown> }).env
    return !!env && env.VITE_VIEWER_HEALTH === "on"
  } catch {
    return false
  }
}

/** The build's opt-in. Off unless the page was built with `VITE_VIEWER_HEALTH=on`. */
export const VIEWER_HEALTH_BUILD_FLAG: boolean = buildFlag()

export interface ViewerHealthPage {
  /** The Cloud API origin, or null when the transport is not the Cloud. */
  apiOrigin: string | null
  signedIn: boolean
  account: string | null
  /** The owned Cloud machine id the page is reading, or null for none. */
  targetMachineId: string | null
}

type Start = typeof startViewerHealth

/** One page's producer, started, switched and stopped as the page's sign-in state changes. */
export class ViewerHealthLifecycle {
  private readonly start: Start
  private readonly enabled: boolean
  private handle: ViewerHealthHandle | null = null
  private runningFor: { account: string; apiOrigin: string } | null = null
  private target: string | null = null

  constructor(options: { enabled: boolean; start?: Start }) {
    this.enabled = options.enabled === true
    this.start = options.start ?? startViewerHealth
  }

  /** Bring the producer in line with the page. Safe to call on every render. */
  update(page: ViewerHealthPage): void {
    const wanted = this.enabled && page.signedIn && !!page.account && !!page.apiOrigin
    if (!wanted) {
      this.stop()
      return
    }
    const account = page.account as string
    const apiOrigin = page.apiOrigin as string
    if (this.handle && this.runningFor && (this.runningFor.account !== account || this.runningFor.apiOrigin !== apiOrigin)) {
      this.stop()
    }
    if (!this.handle) {
      this.handle = this.start({ enabled: true, apiOrigin, targetMachineId: page.targetMachineId ?? undefined })
      if (!this.handle) return
      this.runningFor = { account, apiOrigin }
      this.target = page.targetMachineId
      return
    }
    if (page.targetMachineId !== this.target) {
      this.target = page.targetMachineId
      this.handle.setTargetMachine(page.targetMachineId)
    }
  }

  /** Sign-out, revocation, leaving the Cloud or unmounting: stop and clear. */
  stop(state: Exclude<ViewerHealthState, "running"> = "stopped"): void {
    const handle = this.handle
    this.handle = null
    this.runningFor = null
    this.target = null
    handle?.stop(state)
  }

  running(): boolean {
    return this.handle !== null
  }
}
