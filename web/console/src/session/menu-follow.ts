// @ts-expect-error -- Node's strip-types runner uses source extensions.
import { FOLLOW_MS, FOLLOW_FAST_MS, FOLLOW_LATER_MS, FOLLOW_WINDOW_MS } from "./transcript-follow.ts"

export const MENU_FOLLOW = "clawdline:menu-follow"
export const MENU_READ = "clawdline:menu-read"

/** A read follows a menu transition, never a second keystroke. */
export class MenuFollow {
  private clock: () => number
  constructor(clock: () => number = Date.now) { this.clock = clock }

  active: { session: string; execution: string | undefined; before: string; hold: boolean; at: number; due: number; busy: boolean } | null = null

  start(session: string, execution: string | undefined, before: string, now: number, hold = true): void {
    this.active = { session, execution, before, hold, at: now, due: now, busy: false }
  }

  observe(session: string | undefined, execution: string | undefined, menu: string, now: number, waiting = true): void {
    const active = this.active
    if (active && (session !== active.session || execution !== active.execution ||
      menu !== "" && menu !== active.before || !active.hold && !waiting)) this.active = null
  }

  expired(now: number): boolean { return !!this.active && now - this.active.at >= FOLLOW_WINDOW_MS }

  /** One read at a time; a slow answer cannot multiply reads. */
  async read(now: number, visible: boolean, read: (session: string) => Promise<void>, changed: () => void): Promise<void> {
    const active = this.active
    if (!active || !visible || active.busy || now < active.due || now - active.at >= FOLLOW_WINDOW_MS) return
    active.busy = true
    try { await read(active.session) } catch { /* Leave the manual read live; retry at the shared follow pace. */ }
    finally {
      active.busy = false
      active.due = this.clock() + (now - active.at < FOLLOW_FAST_MS ? FOLLOW_MS : FOLLOW_LATER_MS)
      if (this.active === active) changed()
    }
  }
}
