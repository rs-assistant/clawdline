/** Accept only this console's own address from its service worker. */
export function notificationAddress(raw: unknown, here: string): string | null {
  if (typeof raw !== "string") return null
  try {
    const target = new URL(raw, here)
    const current = new URL(here)
    if (target.origin !== current.origin || target.pathname !== current.pathname || !target.hash) return null
    return target.href
  } catch {
    return null
  }
}
