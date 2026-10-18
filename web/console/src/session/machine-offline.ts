/**
 * The one machine this page reads, when the Cloud roster has stopped hearing
 * from it.
 *
 * The page's own `live` is this browser's line to the relay, not the machine:
 * with the line up over a closed MacBook, the list said "connected to the
 * machine, fetching the Session list" and waited for a machine that was off.
 * The Cloud gate hands down the roster's freshness for the machine underneath
 * (`CloudGate.tsx`, the fact the fleet's dot and every send's admission read),
 * and this decides where the page says it.
 */
export interface MachineOffline {
  /** The machine whose last report this page shows. */
  machine: string
  /** The newest signed report this browser opened from it, or null when it has seen none. */
  seenAt: number | null
}

/**
 * `empty`: nothing is drawn, so the empty state is the offline sentence rather
 * than a load that cannot finish. `line`: the Sessions it last reported stay,
 * under a status line that says they are old and cannot be sent to.
 */
export function offlinePlacement(offline: MachineOffline | null, drawn: number): "empty" | "line" | null {
  if (!offline) return null
  return drawn === 0 ? "empty" : "line"
}
