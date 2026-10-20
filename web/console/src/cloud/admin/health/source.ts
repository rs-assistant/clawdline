/* The one boundary between the screens and wherever health data comes from.
 *
 * A HealthSource answers one query with an unknown body; loadHealth bounds it in
 * time, parses it and turns every failure into a typed code. There is no default
 * address: the root injects a source once the backend routes exist, and an
 * example source exists only in tests or an explicit development harness. */
import type { HealthQuery, HealthSnapshot } from "./types.ts"
// @ts-expect-error -- a `.ts` path, so node's strip-types tests can import this module.
import { HealthContractError, parseHealthSnapshot } from "./parse.ts"

export type HealthSource = {
  /** Answers one query. Must honour `signal`; must not retry on its own. */
  load(query: HealthQuery, signal: AbortSignal): Promise<unknown>
}

export type HealthFailure =
  | "unauthorized" | "forbidden" | "unsupported" | "rate_limited" | "unavailable" | "network" | "timeout"
  | "contract_schema" | "contract_shape" | "contract_bound" | "http_error"

export type HealthResult = { ok: true; snapshot: HealthSnapshot } | { ok: false; code: HealthFailure } | { ok: false; code: "aborted" }

/** A source failure that already knows its typed code (an HTTP status, a refusal). */
export class HealthSourceError extends Error {
  readonly code: HealthFailure
  constructor(code: HealthFailure) {
    super(code)
    this.code = code
  }
}

/** Proposed query deadline (docs/admin-connection-health.md, Query: 5-second deadline). */
export const HEALTH_DEADLINE_MS = 5_000
/** Proposed dashboard polling cadence: every 15 s, one request outstanding. */
export const HEALTH_REFRESH_MS = 15_000

/** One bounded load. `outer` aborting means the caller moved on: the result is "aborted", which a
 *  caller drops rather than showing. The deadline aborting is "timeout", which a caller shows. */
export async function loadHealth(source: HealthSource, query: HealthQuery, outer: AbortSignal, deadlineMs = HEALTH_DEADLINE_MS): Promise<HealthResult> {
  const inner = new AbortController()
  let timedOut = false
  const timer = setTimeout(() => {
    timedOut = true
    inner.abort()
  }, deadlineMs)
  const forward = () => inner.abort()
  if (outer.aborted) inner.abort()
  else outer.addEventListener("abort", forward, { once: true })
  try {
    const body = await Promise.race([
      source.load(query, inner.signal),
      new Promise<never>((_, reject) => inner.signal.addEventListener("abort", () => reject(new Error("aborted")), { once: true })),
    ])
    if (outer.aborted) return { ok: false, code: "aborted" }
    return { ok: true, snapshot: parseHealthSnapshot(body) }
  } catch (err) {
    if (outer.aborted) return { ok: false, code: "aborted" }
    if (timedOut) return { ok: false, code: "timeout" }
    if (err instanceof HealthContractError) return { ok: false, code: err.code }
    if (err instanceof HealthSourceError) return { ok: false, code: err.code }
    return { ok: false, code: "network" }
  } finally {
    clearTimeout(timer)
    outer.removeEventListener("abort", forward)
  }
}

/** Maps an HTTP status to the typed failure the screens explain. */
export function failureForStatus(status: number): HealthFailure {
  if (status === 401) return "unauthorized"
  if (status === 403) return "forbidden"
  if (status === 404 || status === 501) return "unsupported"
  if (status === 429) return "rate_limited"
  if (status === 503 || status === 502 || status === 504) return "unavailable"
  return "http_error"
}

/** A fetch-backed source for a route the root names. `address` is required: this module invents
 *  no production endpoint. The session cookie is the page's own; no token is attached here. */
export function httpHealthSource(address: (query: HealthQuery) => string, get: typeof fetch = fetch): HealthSource {
  return {
    async load(query, signal) {
      const res = await get(address(query), { credentials: "include", headers: { accept: "application/json" }, signal })
      if (!res.ok) throw new HealthSourceError(failureForStatus(res.status))
      return res.json()
    },
  }
}

/* ---- what the page holds between loads ------------------------------------ */

export type HealthState =
  | { status: "loading"; snapshot: null; failure: null; refreshing: true }
  | { status: "failed"; snapshot: null; failure: HealthFailure; refreshing: boolean }
  | { status: "ready"; snapshot: HealthSnapshot; failure: HealthFailure | null; refreshing: boolean; receivedAt: number }

export type HealthAction = { type: "start" } | { type: "result"; result: HealthResult; at: number } | { type: "reset" }

export const initialHealthState: HealthState = { status: "loading", snapshot: null, failure: null, refreshing: true }

/** A failed refresh keeps the last confirmed snapshot with its failure, so the page shows dated
 *  figures marked stale. A first-load failure shows no figures at all. An aborted load changes nothing. */
export function healthReducer(state: HealthState, action: HealthAction): HealthState {
  switch (action.type) {
    case "reset":
      return initialHealthState
    case "start":
      return state.status === "loading" ? state : { ...state, refreshing: true }
    case "result": {
      const r = action.result
      if (r.ok) return { status: "ready", snapshot: r.snapshot, failure: null, refreshing: false, receivedAt: action.at }
      if (r.code === "aborted") return state.status === "loading" ? state : { ...state, refreshing: false }
      if (state.status === "ready") return { ...state, failure: r.code, refreshing: false }
      return { status: "failed", snapshot: null, failure: r.code, refreshing: false }
    }
  }
}
