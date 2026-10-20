/* Where the connection-health page is: the screen from the path, and the time
 * scope from the query, so a refresh, a shared link and Back/Forward all land on
 * the same evidence. Ids are opaque and checked against the Cloud's own id rule;
 * anything else in the address is dropped rather than carried along. */

export const HEALTH_BASE = "/admin/health"

/** Windows the page offers. The Cloud refuses a raw read wider than 24 h (`window_too_wide`). */
export const STORED_WINDOWS_S = [900, 3600, 21600, 86400] as const
export type StoredWindow = (typeof STORED_WINDOWS_S)[number]
export const DEFAULT_WINDOW_S: StoredWindow = 3600

const OPAQUE = /^[A-Za-z0-9_-]{1,64}$/
const STORED_ID = /^[A-Za-z0-9_-]{1,128}$/
const INCIDENT_ID = /^hi_[0-9a-f]{32}$/
const SOURCE_ID = /^hs_[0-9a-f]{32}$/
/** Producers the Cloud's events read narrows to (contract §3 `source`). */
export const SOURCE_FILTERS = ["daemon", "cloud_api", "relay", "viewer", "collector"] as const
export type SourceFilter = (typeof SOURCE_FILTERS)[number]
/** Revisions the Cloud keeps per incident (contract §11.5). */
export const MAX_REVISION = 16

export type HealthScreen =
  | { screen: "M01" }
  | { screen: "M02"; machine: string | null }
  /** The Cloud-side events filter; absent is every source. */
  | { screen: "M03"; source?: SourceFilter | null; sourceId?: string | null }
  | { screen: "M04"; trace: string }
  /** `revision` absent or null is the latest; a number is that immutable revision. */
  | { screen: "M05"; incident: string | null; revision?: number | null }
  | { screen: "M06" }
  | { screen: "evidence"; trace: string | null }

/** The time scope. `until` null is live (the window ends at each load); a value pins it. */
export type HealthScope = { window_s: StoredWindow; until: string | null; account: string | null }

export type HealthRoute = HealthScreen & { scope: HealthScope }

function decode(part: string | undefined, pattern: RegExp): string | null | undefined {
  if (part === undefined) return null
  try {
    const v = decodeURIComponent(part)
    return pattern.test(v) ? v : undefined
  } catch {
    return undefined
  }
}

/** The screen a path names, or null when it names none under the health base. */
export function healthScreen(pathname: string): HealthScreen | null {
  if (pathname !== HEALTH_BASE && !pathname.startsWith(HEALTH_BASE + "/")) return null
  const parts = pathname.slice(HEALTH_BASE.length).split("/").filter(Boolean)
  if (parts.length > 2) return null
  const head = parts[0]
  const pattern = head === "machines" ? STORED_ID : head === "incidents" ? INCIDENT_ID : OPAQUE
  const idPart = decode(parts[1], pattern)
  if (idPart === undefined) return null
  switch (head) {
    case undefined: return { screen: "M01" }
    case "machines": return { screen: "M02", machine: idPart }
    case "services": return parts.length === 1 ? { screen: "M03" } : null
    case "monitoring": return parts.length === 1 ? { screen: "M06" } : null
    case "trace": return idPart ? { screen: "M04", trace: idPart } : null
    case "incidents": return { screen: "M05", incident: idPart }
    case "evidence": return { screen: "evidence", trace: idPart }
    default: return null
  }
}

/** The scope from a query string. A malformed value falls back to live/default, never to a guess. */
export function healthScope(search: string, now: number = Date.now()): HealthScope {
  const q = new URLSearchParams(search)
  const w = Number(q.get("window"))
  const window_s = (STORED_WINDOWS_S as readonly number[]).includes(w) ? (w as StoredWindow) : DEFAULT_WINDOW_S
  const rawUntil = q.get("until")
  const t = rawUntil !== null && rawUntil.length <= 40 ? Date.parse(rawUntil) : NaN
  // A pinned end in the future is not a pin; the Cloud would answer about time that has not happened.
  const until = Number.isFinite(t) && t <= now ? new Date(t).toISOString() : null
  const rawAccount = q.get("account")
  const account = rawAccount !== null && STORED_ID.test(rawAccount) ? rawAccount : null
  return { window_s, until, account }
}

/** The per-screen query: the M03 source filter and the M05 revision. Malformed values are dropped. */
function screenQuery(screen: HealthScreen, search: string): HealthScreen {
  const q = new URLSearchParams(search)
  // Only values present are written, so an address without them reads back as the bare screen.
  if (screen.screen === "M03") {
    const source = q.get("source")
    const sourceId = q.get("source_id")
    const out: HealthScreen = { screen: "M03" }
    if (source !== null && (SOURCE_FILTERS as readonly string[]).includes(source)) out.source = source as SourceFilter
    if (sourceId !== null && SOURCE_ID.test(sourceId)) out.sourceId = sourceId
    return out
  }
  if (screen.screen === "M05" && screen.incident !== null) {
    const raw = q.get("revision")
    const n = raw !== null && /^[0-9]{1,2}$/.test(raw) ? Number(raw) : NaN
    return n >= 1 && n <= MAX_REVISION ? { ...screen, revision: n } : screen
  }
  return screen
}

export function healthRoute(pathname: string, search: string, now?: number): HealthRoute | null {
  const screen = healthScreen(pathname)
  return screen === null ? null : { ...screenQuery(screen, search), scope: healthScope(search, now) }
}

/** The address for a screen in a scope. Only non-default scope values are written. */
export function healthHref(screen: HealthScreen, scope: HealthScope): string {
  const e = encodeURIComponent
  let path: string
  switch (screen.screen) {
    case "M01": path = HEALTH_BASE; break
    case "M02": path = screen.machine === null ? `${HEALTH_BASE}/machines` : `${HEALTH_BASE}/machines/${e(screen.machine)}`; break
    case "M03": path = `${HEALTH_BASE}/services`; break
    case "M04": path = `${HEALTH_BASE}/trace/${e(screen.trace)}`; break
    case "M05": path = screen.incident === null ? `${HEALTH_BASE}/incidents` : `${HEALTH_BASE}/incidents/${e(screen.incident)}`; break
    case "M06": path = `${HEALTH_BASE}/monitoring`; break
    case "evidence": path = screen.trace === null ? `${HEALTH_BASE}/evidence` : `${HEALTH_BASE}/evidence/${e(screen.trace)}`; break
  }
  const q = new URLSearchParams()
  if (scope.window_s !== DEFAULT_WINDOW_S) q.set("window", String(scope.window_s))
  if (scope.until !== null) q.set("until", scope.until)
  if (scope.account !== null) q.set("account", scope.account)
  if (screen.screen === "M03" && screen.source) q.set("source", screen.source)
  if (screen.screen === "M03" && screen.sourceId) q.set("source_id", screen.sourceId)
  if (screen.screen === "M05" && screen.incident !== null && screen.revision) q.set("revision", String(screen.revision))
  const s = q.toString()
  return s ? `${path}?${s}` : path
}

/** The window as the Cloud's `from`/`to`, ending at the pin or at `now`. */
export function windowBounds(scope: HealthScope, now: number): { from: string; to: string } {
  const to = scope.until === null ? now : Date.parse(scope.until)
  return { from: new Date(to - scope.window_s * 1000).toISOString(), to: new Date(to).toISOString() }
}

/** Two routes ask for the same evidence when screen, ids and scope agree. */
export function routeKey(r: HealthRoute): string {
  const id = r.screen === "M02" ? r.machine
    : r.screen === "M03" ? `${r.source ?? ""}/${r.sourceId ?? ""}`
    : r.screen === "M04" ? r.trace
    : r.screen === "M05" ? `${r.incident ?? ""}/${r.revision ?? ""}`
    : r.screen === "evidence" ? r.trace : null
  // A changed filter, incident or revision is a new key: the old pages and bytes are dropped.
  return [r.screen, id ?? "", r.scope.window_s, r.scope.until ?? "", r.scope.account ?? ""].join("|")
}
