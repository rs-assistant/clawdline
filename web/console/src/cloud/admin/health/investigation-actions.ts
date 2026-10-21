/* Network wiring for agent investigations: the operator's own schedule webhooks, and the start.
 *
 * Starting one is an explicit POST with an Idempotency-Key the caller keeps until the outcome is
 * known: an answer that never arrived is retried with the same key and body, never a new key.
 * Nothing is imported at run time except the loader's own helpers, so node's tests load it as is. */
import type { Refusal, StoredFailure } from "./cloud.ts"
// @ts-expect-error -- a `.ts` path, so node's strip-types tests can import this module.
import { errorInfo, failureFor, HEALTH_UI_LIMITS } from "./cloud.ts"
import type { Investigation } from "./investigations.ts"
// @ts-expect-error -- a `.ts` path, so node's strip-types tests can import this module.
import { parseInvestigation } from "./investigations.ts"

type Obj = Record<string, unknown>
const isObj = (v: unknown): v is Obj => v !== null && typeof v === "object" && !Array.isArray(v)
const REF = /^[A-Za-z0-9_-]{1,128}$/
const ref = (v: unknown): string | null => (typeof v === "string" && REF.test(v) ? v : null)
const nat = (v: unknown): number | null => (typeof v === "number" && Number.isSafeInteger(v) && v >= 0 ? v : null)

/* ---- the operator's own schedule webhooks ---------------------------------- */

/** One hook as `GET /v1/schedule-webhooks` lists it; only what the picker shows. */
export type HookChoice = { hook_id: string; machine_id: string; machine_name?: string; availability: string; generation: number | null; credential_fingerprint: string | null }
export type HooksResult = { ok: true; hooks: HookChoice[]; more: boolean } | { ok: false; code: StoredFailure } | { refused: Refusal }

/** The hooks a start may name: only `active` ones, because Cloud refuses every other (`hook_unavailable`).
 *  The list carries no field saying which hook is bound to an investigation schedule; the page says so. */
export function parseHooks(body: unknown): { hooks: HookChoice[]; more: boolean } | null {
  if (!isObj(body) || body.schema !== "clawdline.schedule_webhook.list.v1" || !Array.isArray(body.hooks)) return null
  const hooks: HookChoice[] = []
  for (const h of body.hooks) {
    if (!isObj(h)) continue
    const id = ref(h.hook_id)
    const machine = ref(h.machine_id)
    if (id === null || machine === null || h.state !== "active" || h.availability !== "active") continue
    hooks.push({ ...(typeof h.machine_name === "string" && h.machine_name.trim() ? { machine_name: h.machine_name.trim() } : {}), hook_id: id, machine_id: machine, availability: "active", generation: nat(h.generation), credential_fingerprint: ref(h.credential_fingerprint) })
  }
  return { hooks, more: typeof body.next_cursor === "string" }
}

async function cappedText(res: Response): Promise<string | null> {
  const text = await res.text()
  return new TextEncoder().encode(text).length > HEALTH_UI_LIMITS.bodyBytes ? null : text
}

export async function fetchHooks(get: typeof fetch, apiOrigin: string, signal: AbortSignal): Promise<HooksResult> {
  let res: Response
  try {
    res = await get(`${apiOrigin}/v1/schedule-webhooks`, { credentials: "include", cache: "no-store", headers: { accept: "application/json" }, signal })
  } catch {
    return { ok: false, code: "network" }
  }
  const text = await cappedText(res).catch(() => null)
  if (res.status === 401) return { refused: "signed_out" }
  if (!res.ok) return { ok: false, code: res.status === 429 ? "rate_limited" : res.status >= 500 ? "unavailable" : "http_error" }
  if (text === null) return { ok: false, code: "body_too_large" }
  try {
    const parsed = parseHooks(JSON.parse(text))
    return parsed === null ? { ok: false, code: "contract_shape" } : { ok: true, ...parsed }
  } catch {
    return { ok: false, code: "contract_shape" }
  }
}

/* ---- starting one --------------------------------------------------------- */

/** The Cloud's stable refusal codes for a start (contract §1), kept as sent so each gets its own words. */
export const START_REFUSALS = [
  "incident_not_found", "incident_expired", "incident_revision_moved", "investigation_open", "investigation_limit",
  "hook_unavailable", "pro_required", "webhook_rate_limited", "webhook_queue_full", "idempotency_conflict",
  "health_store_unavailable", "health_busy", "bad_origin",
] as const
export type StartRefusal = (typeof START_REFUSALS)[number]

export type StartRequest = { hook_id: string; expected_incident_revision: number }
export type StartResult =
  | { ok: true; investigation: Investigation | null; replayed: boolean }
  /** Cloud gave a definite answer: the key is spent and a new attempt takes a new one. */
  | { ok: false; definite: true; code: StartRefusal | StoredFailure }
  /** No definite answer reached the page: retry with the same key and body. */
  | { ok: false; definite: false; code: "network" | "unavailable" | "contract_shape" }
  | { refused: Refusal }

/** A fresh key for one start; the caller keeps it until a definite result. */
export function newIdempotencyKey(random: () => string = () => globalThis.crypto.randomUUID()): string {
  return `inv-${random()}`
}

export async function startInvestigation(get: typeof fetch, apiOrigin: string, incidentId: string, account: string | null, req: StartRequest, key: string, signal: AbortSignal): Promise<StartResult> {
  const q = account !== null ? `?${new URLSearchParams({ account }).toString()}` : ""
  let res: Response
  try {
    res = await get(`${apiOrigin}/su/v1/health/incidents/${encodeURIComponent(incidentId)}/investigations${q}`, {
      method: "POST", credentials: "include", cache: "no-store", signal,
      headers: { accept: "application/json", "content-type": "application/json", "idempotency-key": key },
      body: JSON.stringify({ hook_id: req.hook_id, expected_incident_revision: req.expected_incident_revision }),
    })
  } catch {
    return { ok: false, definite: false, code: "network" }
  }
  const text = await cappedText(res).catch(() => null)
  if (res.status === 202) {
    try {
      const body = JSON.parse(text ?? "") as Obj
      if (body.schema !== "clawdline.health_investigation.started.v1" || typeof body.replayed !== "boolean") return { ok: false, definite: false, code: "contract_shape" }
      return { ok: true, investigation: parseInvestigation(body.investigation), replayed: body.replayed }
    } catch {
      return { ok: false, definite: false, code: "contract_shape" }
    }
  }
  const code = errorInfo(text).code
  if ((START_REFUSALS as readonly string[]).includes(code ?? "")) {
    // Overloaded or unmigrated storage committed nothing: Cloud says so, so a retry may use a new key.
    return { ok: false, definite: true, code: code as StartRefusal }
  }
  if (res.status === 403) return { ok: false, definite: true, code: "http_error" }
  const failure = failureFor(res.status, code)
  if (failure === "signed_out" || failure === "forbidden") return { refused: failure }
  // A 5xx without a typed code may have committed: the outcome is unknown, so the key is kept.
  if (res.status >= 500) return { ok: false, definite: false, code: "unavailable" }
  return { ok: false, definite: true, code: failure }
}
