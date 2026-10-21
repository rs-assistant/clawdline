// The browsers and phones signed in to this account, and taking one away.
//
// These are the account's viewer devices (`GET /v1/devices`), not its machines.
// Revoking one is account-wide: that browser stops reaching every machine of the
// account at once, because the account holds no per-machine grant to take back.
// The machine sheet says so beside the list rather than implying otherwise.

export interface ViewerDevice {
  id: string
  /** "browser", "ios", "android" or "cli", as the account recorded it. */
  kind: string
  name: string
  lastSeenAt: string | null
  createdAt: string | null
}

export type ViewerDeviceList =
  | { kind: "listed"; devices: ViewerDevice[] }
  | { kind: "refused"; status: number }
  | { kind: "unreadable"; status: number | null }

export type RevokeOutcome =
  | { kind: "revoked" }
  | { kind: "refused"; status: number; code: string }
  | { kind: "absent"; status: number; code: string }
  | { kind: "unreadable"; status: number | null; code: string }

function text(value: unknown): string | null {
  return typeof value === "string" && value ? value : null
}

/** The account's devices that are not revoked; a CLI credential is not a browser and is left out. */
export async function listViewerDevices(apiOrigin: string, get: typeof fetch = fetch): Promise<ViewerDeviceList> {
  let res: Response
  try {
    res = await get(apiOrigin + "/v1/devices", { credentials: "include", cache: "no-store" })
  } catch {
    return { kind: "unreadable", status: null }
  }
  if (res.status === 401 || res.status === 403) return { kind: "refused", status: res.status }
  const body: unknown = await res.json().catch(() => null)
  const rows = res.ok && body && typeof body === "object" ? (body as { devices?: unknown }).devices : null
  if (!Array.isArray(rows)) return { kind: "unreadable", status: res.status }
  const devices: ViewerDevice[] = []
  for (const row of rows) {
    if (!row || typeof row !== "object") continue
    const r = row as Record<string, unknown>
    const id = text(r.id)
    if (!id || r.revoked_at || r.kind === "cli") continue
    devices.push({
      id,
      kind: text(r.kind) ?? "browser",
      name: text(r.name) ?? "",
      lastSeenAt: text(r.last_seen_at),
      createdAt: text(r.created_at),
    })
  }
  return { kind: "listed", devices }
}

/**
 * Revoke one device. Nothing retries: like forgetting a machine, a second
 * DELETE after an answer nobody could read would be a second act on a guess.
 */
export async function revokeViewerDevice(apiOrigin: string, id: string, get: typeof fetch = fetch): Promise<RevokeOutcome> {
  let res: Response
  try {
    res = await get(apiOrigin + "/v1/devices/" + encodeURIComponent(id), { method: "DELETE", credentials: "include" })
  } catch {
    return { kind: "unreadable", status: null, code: "no_answer" }
  }
  const body = (await res.json().catch(() => null)) as { error?: { code?: unknown } } | null
  const code = typeof body?.error?.code === "string" && body.error.code ? body.error.code : "http_" + res.status
  if (res.status === 401 || res.status === 403) return { kind: "refused", status: res.status, code }
  if (res.status === 404) return { kind: "absent", status: res.status, code }
  if (res.status !== 200 && res.status !== 204) return { kind: "unreadable", status: res.status, code }
  return { kind: "revoked" }
}
