/*
 * The operations overview the Cloud API gives its superusers, read and checked.
 *
 * `GET {apiOrigin}/su/v1/operations/overview` answers only a signed-in
 * superuser; everyone else gets the same 404 an unknown route gets
 * (`requireSuperuser`). What comes back is anonymous totals, never a person or
 * a code. The page draws only what this file accepted, so a field the server
 * could not vouch for (`null`) reaches the screen as "unknown", never as a 0.
 *
 * Nothing is imported at run time, so `node --test` loads it as it is.
 */

export const OVERVIEW_SCHEMA = "clawdline.operations.overview.v1"

export interface Comparison {
  current: number
  previous: number
}

export interface DailyCount {
  /** A complete UTC day, `YYYY-MM-DD`. */
  day: string
  count: number
}

export type CampaignStatus =
  | "redeemable"
  | "not_started"
  | "expired"
  | "revoked"
  | "campaign_exhausted"
  | "no_available_code"
  | "unknown"

export interface CampaignRow {
  id: string
  name: string
  status: CampaignStatus
  not_before: string | null
  expires_at: string | null
  redemption_limit: number | null
  redeemed_count: number
  issued_codes: number
  available_codes: number
}

export interface UsageCounters {
  dispatches: number | null
  connect_minutes: number | null
  bytes_in: number | null
  bytes_out: number | null
}

export interface UsagePeriod {
  /** `YYYY-MM`. */
  period: string
  coverage: "available" | "partial"
  active_accounts: number | null
  counters: UsageCounters
}

export interface Overview {
  generated_at: string
  accounts: {
    total: number
    new_7d: Comparison
    new_30d: Comparison
    daily: DailyCount[]
  }
  promotions: {
    campaigns_total: number
    codes_total: number
    redemptions_total: number
    redeemers_total: number
    redeemed_7d: Comparison
    redeemed_30d: Comparison
    daily: DailyCount[]
    campaigns: { total: number; shown: number; rows: CampaignRow[] }
  }
  usage: { current: UsagePeriod; previous: UsagePeriod; active_daily: DailyCount[] | null }
}

/** The answer was not the contract; `where` names the first field that was wrong. */
export class OverviewShapeError extends Error {
  readonly where: string
  constructor(where: string) {
    super("operations overview: unusable " + where)
    this.name = "OverviewShapeError"
    this.where = where
  }
}

type Bag = Record<string, unknown>

function bag(value: unknown, where: string): Bag {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new OverviewShapeError(where)
  return value as Bag
}

function count(value: unknown, where: string): number {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < 0) throw new OverviewShapeError(where)
  return value
}

function countOrNull(value: unknown, where: string): number | null {
  return value === null || value === undefined ? null : count(value, where)
}

function text(value: unknown, where: string): string {
  if (typeof value !== "string" || !value) throw new OverviewShapeError(where)
  return value
}

function instantOrNull(value: unknown, where: string): string | null {
  if (value === null || value === undefined) return null
  const s = text(value, where)
  if (Number.isNaN(Date.parse(s))) throw new OverviewShapeError(where)
  return s
}

const DAY = /^\d{4}-\d{2}-\d{2}$/
const MONTH = /^\d{4}-\d{2}$/

function comparison(value: unknown, where: string): Comparison {
  const b = bag(value, where)
  return { current: count(b.current, where + ".current"), previous: count(b.previous, where + ".previous") }
}

function daily(value: unknown, where: string): DailyCount[] {
  if (!Array.isArray(value)) throw new OverviewShapeError(where)
  return value.map((item, i) => {
    const b = bag(item, `${where}[${i}]`)
    const day = text(b.day, `${where}[${i}].day`)
    if (!DAY.test(day)) throw new OverviewShapeError(`${where}[${i}].day`)
    return { day, count: count(b.count, `${where}[${i}].count`) }
  })
}

const STATUSES: readonly CampaignStatus[] = [
  "redeemable", "not_started", "expired", "revoked", "campaign_exhausted", "no_available_code",
]

function campaign(value: unknown, where: string): CampaignRow {
  const b = bag(value, where)
  const status = STATUSES.includes(b.status as CampaignStatus) ? (b.status as CampaignStatus) : "unknown"
  return {
    id: text(b.id, where + ".id"),
    name: typeof b.name === "string" && b.name.trim() ? b.name.trim() : "",
    status,
    not_before: instantOrNull(b.not_before, where + ".not_before"),
    expires_at: instantOrNull(b.expires_at, where + ".expires_at"),
    redemption_limit: countOrNull(b.redemption_limit, where + ".redemption_limit"),
    redeemed_count: count(b.redeemed_count, where + ".redeemed_count"),
    issued_codes: count(b.issued_codes, where + ".issued_codes"),
    available_codes: count(b.available_codes, where + ".available_codes"),
  }
}

function usage(value: unknown, where: string): UsagePeriod {
  const b = bag(value, where)
  const period = text(b.period, where + ".period")
  if (!MONTH.test(period)) throw new OverviewShapeError(where + ".period")
  // Anything but a plain "available" is drawn as partial: claiming complete
  // coverage the server did not state would be the false zero in another form.
  const coverage = b.coverage === "available" ? "available" : "partial"
  const c = bag(b.counters, where + ".counters")
  return {
    period,
    coverage,
    active_accounts: countOrNull(b.active_accounts, where + ".active_accounts"),
    counters: {
      dispatches: countOrNull(c.dispatches, where + ".counters.dispatches"),
      connect_minutes: countOrNull(c.connect_minutes, where + ".counters.connect_minutes"),
      bytes_in: countOrNull(c.bytes_in, where + ".counters.bytes_in"),
      bytes_out: countOrNull(c.bytes_out, where + ".counters.bytes_out"),
    },
  }
}

/** The response body, checked against the v1 contract, or an `OverviewShapeError`. */
export function parseOverview(raw: unknown): Overview {
  const root = bag(raw, "body")
  if (root.schema !== OVERVIEW_SCHEMA) throw new OverviewShapeError("schema")
  const generatedAt = instantOrNull(root.generated_at, "generated_at")
  if (!generatedAt) throw new OverviewShapeError("generated_at")
  const a = bag(root.accounts, "accounts")
  const p = bag(root.promotions, "promotions")
  const pc = bag(p.campaigns, "promotions.campaigns")
  const rows = Array.isArray(pc.rows) ? pc.rows : null
  if (!rows) throw new OverviewShapeError("promotions.campaigns.rows")
  const u = bag(root.usage, "usage")
  return {
    generated_at: generatedAt,
    accounts: {
      total: count(a.total, "accounts.total"),
      new_7d: comparison(a.new_7d, "accounts.new_7d"),
      new_30d: comparison(a.new_30d, "accounts.new_30d"),
      daily: completeDays(daily(a.daily, "accounts.daily"), generatedAt),
    },
    promotions: {
      campaigns_total: count(p.campaigns_total, "promotions.campaigns_total"),
      codes_total: count(p.codes_total, "promotions.codes_total"),
      redemptions_total: count(p.redemptions_total, "promotions.redemptions_total"),
      redeemers_total: count(p.redeemers_total, "promotions.redeemers_total"),
      redeemed_7d: comparison(p.redeemed_7d, "promotions.redeemed_7d"),
      redeemed_30d: comparison(p.redeemed_30d, "promotions.redeemed_30d"),
      daily: completeDays(daily(p.daily, "promotions.daily"), generatedAt),
      campaigns: {
        total: count(pc.total, "promotions.campaigns.total"),
        shown: rows.length,
        rows: rows.map((row, i) => campaign(row, `promotions.campaigns.rows[${i}]`)),
      },
    },
    usage: {
      current: usage(u.current, "usage.current"), previous: usage(u.previous, "usage.previous"),
      active_daily: u.active_daily == null ? null : completeDays(daily(u.active_daily, "usage.active_daily"), generatedAt),
    },
  }
}

/**
 * Only whole UTC days before the day the overview was made, oldest first, one
 * row per day. The contract already excludes today; a server that slipped it
 * in would show a half day as if it were a slump, so it is dropped here too.
 */
export function completeDays(days: readonly DailyCount[], generatedAt: string): DailyCount[] {
  const today = new Date(generatedAt).toISOString().slice(0, 10)
  const seen = new Map<string, number>()
  for (const d of days) if (d.day < today) seen.set(d.day, d.count)
  return [...seen.entries()].sort(([x], [y]) => (x < y ? -1 : x > y ? 1 : 0)).map(([day, n]) => ({ day, count: n }))
}

export type Trend =
  | { kind: "up" | "down"; delta: number; percent: number | null }
  | { kind: "flat" }

/**
 * Current against the equal-length period before it. A previous of zero has no
 * percentage — anything over nothing is not "∞%" — so only the delta is given.
 */
export function trend(c: Comparison): Trend {
  const delta = c.current - c.previous
  if (delta === 0) return { kind: "flat" }
  const percent = c.previous === 0 ? null : Math.round((Math.abs(delta) / c.previous) * 100)
  return { kind: delta > 0 ? "up" : "down", delta: Math.abs(delta), percent }
}

/** Redeemers per redemption is meaningful; redemptions per code is not, as a code may serve many. */
export function campaignFill(row: CampaignRow): number | null {
  if (row.redemption_limit === null || row.redemption_limit === 0) return null
  return Math.min(1, row.redeemed_count / row.redemption_limit)
}

export type OverviewResult =
  | { kind: "ready"; overview: Overview }
  /** No session the API accepts: sign in again. */
  | { kind: "signed_out" }
  /** Signed in, but not a superuser (the API answers 404 or 403 alike). */
  | { kind: "forbidden" }
  /** The request or the answer failed; nothing is known, so nothing is drawn as data. */
  | { kind: "failed"; code: string; status: number | null }

type Fetch = (url: string, init: RequestInit) => Promise<Pick<Response, "status" | "json">>

/** One read of the overview, with every failure kept apart from an empty answer. */
export async function loadOverview(fetcher: Fetch, apiOrigin: string): Promise<OverviewResult> {
  let response: Pick<Response, "status" | "json">
  try {
    response = await fetcher(apiOrigin + "/su/v1/operations/overview", {
      credentials: "include",
      cache: "no-store",
      headers: { accept: "application/json" },
    })
  } catch {
    return { kind: "failed", code: "network", status: null }
  }
  if (response.status === 401) return { kind: "signed_out" }
  if (response.status === 403 || response.status === 404) return { kind: "forbidden" }
  if (response.status !== 200) return { kind: "failed", code: "http_" + response.status, status: response.status }
  let body: unknown
  try {
    body = await response.json()
  } catch {
    return { kind: "failed", code: "not_json", status: 200 }
  }
  try {
    return { kind: "ready", overview: parseOverview(body) }
  } catch (error) {
    return { kind: "failed", code: error instanceof OverviewShapeError ? "shape:" + error.where : "shape", status: 200 }
  }
}
