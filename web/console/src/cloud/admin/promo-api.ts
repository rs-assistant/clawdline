/*
 * The promotion routes the Cloud API gives its superusers, read and checked.
 *
 * Reads:
 *   GET  {api}/su/v1/promo/campaigns?offset=N&limit=50
 *        → { total, offset, rows: [{ id, name, tier, duration_calendar_months, not_before,
 *            expires_at, redemption_limit, redeemed_count, conflict_policy, state, created_at }] }
 *   GET  {api}/su/v1/promo/campaigns/:id/codes?offset=N&limit=50
 *        → { campaign: { id, name, state, not_before, expires_at, redemption_limit, redeemed_count },
 *            total, offset, rows: [{ id, issued_at, redemption_limit, redeemed_count }] }
 * Writes, each with an `Idempotency-Key`:
 *   POST {api}/su/v1/promo/campaigns              { name, not_before, expires_at, redemption_limit, conflict_policy }
 *   POST {api}/su/v1/promo/campaigns/:id/codes    { redemption_limit } → { code, replay, receipt }
 *   POST {api}/su/v1/promo/campaigns/:id/revoke   {}
 *
 * A read names codes by identifier and counts only. The plaintext of a code
 * exists once, in the answer to the issue that made it; this file hands it to
 * the caller and keeps no copy, and nothing here writes it anywhere else.
 *
 * The API answers a session it does not accept, and an account that is not a
 * superuser, with the 404 an unknown route gets, so a 404 whose error code is
 * the generic `not_found` is "no access", and a 404 that names a code
 * (`unknown_campaign`) is about the thing asked for.
 *
 * Nothing is imported at run time, so `node --test` loads it as it is.
 */

import type { CampaignRow } from "./api.js"

export const PAGE_SIZE = 50

/** What the server accepts (`api/src/routes/promotions.ts`); checked here first so a mistake is named beside its field. */
export const NAME_MAX = 120
export const LIMIT_MAX = 1_000_000
export const WINDOW_MAX_DAYS = 366

export type CampaignState = "active" | "revoked"

export interface PromoCampaign {
  id: string
  name: string
  tier: string | null
  duration_calendar_months: number | null
  not_before: string
  expires_at: string
  redemption_limit: number
  redeemed_count: number
  conflict_policy: string | null
  /** Anything but the two states the contract names is drawn as unknown. */
  state: CampaignState | "unknown"
  created_at: string | null
}

export interface CampaignPage {
  total: number
  offset: number
  rows: PromoCampaign[]
}

export interface PromoCodeRow {
  id: string
  issued_at: string
  redemption_limit: number
  redeemed_count: number
}

export interface CodePage {
  campaign: Pick<PromoCampaign, "id" | "name" | "state" | "not_before" | "expires_at" | "redemption_limit" | "redeemed_count">
  total: number
  offset: number
  rows: PromoCodeRow[]
}

/** The answer was not the contract; `where` names the first field that was wrong. */
export class PromoShapeError extends Error {
  readonly where: string
  constructor(where: string) {
    super("promotions: unusable " + where)
    this.name = "PromoShapeError"
    this.where = where
  }
}

type Bag = Record<string, unknown>

function bag(value: unknown, where: string): Bag {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new PromoShapeError(where)
  return value as Bag
}

function count(value: unknown, where: string): number {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < 0) throw new PromoShapeError(where)
  return value
}

function text(value: unknown, where: string): string {
  if (typeof value !== "string" || !value) throw new PromoShapeError(where)
  return value
}

function instant(value: unknown, where: string): string {
  const s = text(value, where)
  if (Number.isNaN(Date.parse(s))) throw new PromoShapeError(where)
  return s
}

function optionalText(value: unknown): string | null {
  return typeof value === "string" && value ? value : null
}

function state(value: unknown): PromoCampaign["state"] {
  return value === "active" || value === "revoked" ? value : "unknown"
}

function rows(value: unknown, where: string): unknown[] {
  if (!Array.isArray(value)) throw new PromoShapeError(where)
  return value
}

export function parseCampaign(value: unknown, where = "campaign"): PromoCampaign {
  const b = bag(value, where)
  const months = b.duration_calendar_months
  return {
    id: text(b.id, where + ".id"),
    name: typeof b.name === "string" ? b.name.trim() : "",
    tier: optionalText(b.tier),
    duration_calendar_months: typeof months === "number" && Number.isSafeInteger(months) && months > 0 ? months : null,
    not_before: instant(b.not_before, where + ".not_before"),
    expires_at: instant(b.expires_at, where + ".expires_at"),
    redemption_limit: count(b.redemption_limit, where + ".redemption_limit"),
    redeemed_count: count(b.redeemed_count, where + ".redeemed_count"),
    conflict_policy: optionalText(b.conflict_policy),
    state: state(b.state),
    created_at: b.created_at === null || b.created_at === undefined ? null : instant(b.created_at, where + ".created_at"),
  }
}

export function parseCampaignPage(raw: unknown): CampaignPage {
  const b = bag(raw, "body")
  return {
    total: count(b.total, "total"),
    offset: count(b.offset, "offset"),
    rows: rows(b.rows, "rows").map((row, i) => parseCampaign(row, `rows[${i}]`)),
  }
}

export function parseCodePage(raw: unknown): CodePage {
  const b = bag(raw, "body")
  const c = bag(b.campaign, "campaign")
  return {
    campaign: {
      id: text(c.id, "campaign.id"),
      name: typeof c.name === "string" ? c.name.trim() : "",
      state: state(c.state),
      not_before: instant(c.not_before, "campaign.not_before"),
      expires_at: instant(c.expires_at, "campaign.expires_at"),
      redemption_limit: count(c.redemption_limit, "campaign.redemption_limit"),
      redeemed_count: count(c.redeemed_count, "campaign.redeemed_count"),
    },
    total: count(b.total, "total"),
    offset: count(b.offset, "offset"),
    rows: rows(b.rows, "rows").map((row, i) => {
      const r = bag(row, `rows[${i}]`)
      return {
        id: text(r.id, `rows[${i}].id`),
        issued_at: instant(r.issued_at, `rows[${i}].issued_at`),
        redemption_limit: count(r.redemption_limit, `rows[${i}].redemption_limit`),
        redeemed_count: count(r.redeemed_count, `rows[${i}].redeemed_count`),
      }
    }),
  }
}

/* ---- what a campaign is, now ------------------------------------------- */

export type CampaignPhase = "open" | "not_started" | "expired" | "revoked" | "limit_reached" | "no_codes" | "unknown"

/** The one state an operator reads first, from the stored state and the clock. */
export function campaignPhase(c: Pick<PromoCampaign, "state" | "not_before" | "expires_at" | "redemption_limit" | "redeemed_count">, now: number): CampaignPhase {
  if (c.state === "revoked") return "revoked"
  if (c.state !== "active") return "unknown"
  if (now >= Date.parse(c.expires_at)) return "expired"
  if (now < Date.parse(c.not_before)) return "not_started"
  if (c.redeemed_count >= c.redemption_limit) return "limit_reached"
  return "open"
}

/**
 * One row of the campaign list, from either source: the paged list route, or,
 * where that route is not served, the overview's latest campaigns. Only the
 * overview knows the code inventory, so `codes` is null from the list route.
 */
export interface ListedCampaign {
  id: string
  name: string
  phase: CampaignPhase
  not_before: string | null
  expires_at: string | null
  redemption_limit: number | null
  redeemed_count: number
  codes: { issued: number; available: number } | null
}

export function listedFromCampaign(c: PromoCampaign, now: number): ListedCampaign {
  return {
    id: c.id, name: c.name, phase: campaignPhase(c, now),
    not_before: c.not_before, expires_at: c.expires_at,
    redemption_limit: c.redemption_limit, redeemed_count: c.redeemed_count, codes: null,
  }
}

const OVERVIEW_PHASE: Record<CampaignRow["status"], CampaignPhase> = {
  redeemable: "open",
  not_started: "not_started",
  expired: "expired",
  revoked: "revoked",
  campaign_exhausted: "limit_reached",
  no_available_code: "no_codes",
  unknown: "unknown",
}

export function listedFromOverview(row: CampaignRow): ListedCampaign {
  return {
    id: row.id, name: row.name, phase: OVERVIEW_PHASE[row.status],
    not_before: row.not_before, expires_at: row.expires_at,
    redemption_limit: row.redemption_limit, redeemed_count: row.redeemed_count,
    codes: { issued: row.issued_codes, available: row.available_codes },
  }
}

/** A campaign can issue codes while it is active and not expired (`issuePromoCode`). */
export function canIssue(c: Pick<PromoCampaign, "state" | "expires_at">, now: number): boolean {
  return c.state === "active" && now < Date.parse(c.expires_at)
}

/** A share of a whole, 0..1, or null when there is no whole to share. */
export function fill(used: number, limit: number): number | null {
  if (limit <= 0) return null
  return Math.min(1, used / limit)
}

export interface PageWindow {
  /** 1-based, inclusive; 0 when the page is empty. */
  from: number
  to: number
  total: number
  previous: number | null
  next: number | null
}

/** Where a page of `shown` rows at `offset` sits in `total`, and the offsets either side of it. */
export function pageWindow(offset: number, shown: number, total: number, size = PAGE_SIZE): PageWindow {
  const from = shown === 0 ? 0 : offset + 1
  const to = offset + shown
  return {
    from,
    to,
    total,
    previous: offset > 0 ? Math.max(0, offset - size) : null,
    next: to < total ? offset + size : null,
  }
}

/* ---- the new-campaign form ----------------------------------------------- */

export interface CampaignDraft {
  name: string
  /** Instants as the form gives them; empty when not filled. */
  notBefore: string
  expiresAt: string
  redemptionLimit: string
}

export type DraftProblem =
  | "name_missing" | "name_too_long"
  | "start_missing" | "end_missing"
  | "end_before_start" | "end_in_past" | "window_too_long"
  | "limit_not_whole" | "limit_too_large"

export interface CampaignInput {
  name: string
  not_before: string
  expires_at: string
  redemption_limit: number
  /** The only policy the service accepts at launch: an account with a paid plan cannot redeem. */
  conflict_policy: "reject"
}

/** A whole number from 1 to `max`, or the reason it is not one. */
function wholeCount(raw: string, max: number): number | "limit_not_whole" | "limit_too_large" {
  const s = raw.trim()
  if (!/^\d+$/.test(s)) return "limit_not_whole"
  const v = Number(s)
  if (!Number.isSafeInteger(v) || v < 1) return "limit_not_whole"
  if (v > max) return "limit_too_large"
  return v
}

/**
 * The draft as the API will take it, or every field's problem. The checks are
 * the server's own, so the form names a mistake beside its field instead of
 * waiting for the server's one code; the server still decides.
 */
export function checkCampaignDraft(draft: CampaignDraft, now: number):
  | { ok: true; input: CampaignInput }
  | { ok: false; problems: Partial<Record<"name" | "notBefore" | "expiresAt" | "redemptionLimit", DraftProblem>> } {
  const problems: Partial<Record<"name" | "notBefore" | "expiresAt" | "redemptionLimit", DraftProblem>> = {}
  const name = draft.name.trim()
  if (!name) problems.name = "name_missing"
  else if (name.length > NAME_MAX) problems.name = "name_too_long"
  const start = draft.notBefore ? Date.parse(draft.notBefore) : NaN
  const end = draft.expiresAt ? Date.parse(draft.expiresAt) : NaN
  if (Number.isNaN(start)) problems.notBefore = "start_missing"
  if (Number.isNaN(end)) problems.expiresAt = "end_missing"
  else if (end <= now) problems.expiresAt = "end_in_past"
  else if (!Number.isNaN(start) && end <= start) problems.expiresAt = "end_before_start"
  else if (!Number.isNaN(start) && end - start > WINDOW_MAX_DAYS * 86_400_000) problems.expiresAt = "window_too_long"
  const limit = wholeCount(draft.redemptionLimit, LIMIT_MAX)
  if (typeof limit === "string") problems.redemptionLimit = limit
  if (Object.keys(problems).length > 0 || typeof limit === "string") return { ok: false, problems }
  return {
    ok: true,
    input: {
      name,
      not_before: new Date(start).toISOString(),
      expires_at: new Date(end).toISOString(),
      redemption_limit: limit,
      conflict_policy: "reject",
    },
  }
}

/** A code's own limit: a whole number no larger than its campaign's. */
export function checkCodeLimit(raw: string, campaignLimit: number): number | "limit_not_whole" | "limit_too_large" {
  return wholeCount(raw, Math.min(LIMIT_MAX, campaignLimit))
}

/* ---- talking to the API -------------------------------------------------- */

export type Failure =
  /** No session the API accepts, or not a superuser: the API answers both with a 404. */
  | { kind: "no_access" }
  /** The request or the answer failed, or the server refused with a reason. */
  | { kind: "failed"; code: string; status: number | null }

export type Read<T> = { kind: "ready"; value: T } | Failure

export type IssueAnswer =
  /** `code` is the plaintext, present once; null when the server replayed an issue already answered. */
  { code: string | null; replay: boolean; codeId: string | null; issuedAt: string | null }

type Answer = Pick<Response, "status" | "json">
type Fetch = (url: string, init: RequestInit) => Promise<Answer>

async function errorCode(response: Answer): Promise<string | null> {
  try {
    const body = (await response.json()) as { error?: { code?: unknown } } | null
    const code = body?.error?.code
    return typeof code === "string" && code ? code : null
  } catch {
    return null
  }
}

async function call<T>(fetcher: Fetch, url: string, init: RequestInit, ok: (status: number) => boolean, parse: (raw: unknown) => T): Promise<Read<T>> {
  let response: Answer
  try {
    response = await fetcher(url, { credentials: "include", cache: "no-store", ...init })
  } catch {
    return { kind: "failed", code: "network", status: null }
  }
  if (!ok(response.status)) {
    const code = await errorCode(response)
    if (response.status === 401 || response.status === 403) return { kind: "no_access" }
    if (response.status === 404 && (code === null || code === "not_found")) return { kind: "no_access" }
    return { kind: "failed", code: code ?? "http_" + response.status, status: response.status }
  }
  let body: unknown
  try {
    body = await response.json()
  } catch {
    return { kind: "failed", code: "not_json", status: response.status }
  }
  try {
    return { kind: "ready", value: parse(body) }
  } catch (error) {
    return { kind: "failed", code: error instanceof PromoShapeError ? "shape:" + error.where : "shape", status: response.status }
  }
}

const reading: RequestInit = { method: "GET", headers: { accept: "application/json" } }

function writing(key: string, body: unknown): RequestInit {
  return {
    method: "POST",
    headers: { accept: "application/json", "content-type": "application/json", "Idempotency-Key": key },
    body: JSON.stringify(body),
  }
}

const is200 = (s: number) => s === 200
const created = (s: number) => s === 200 || s === 201

export function loadCampaigns(fetcher: Fetch, apiOrigin: string, offset: number): Promise<Read<CampaignPage>> {
  return call(fetcher, `${apiOrigin}/su/v1/promo/campaigns?offset=${offset}&limit=${PAGE_SIZE}`, reading, is200, parseCampaignPage)
}

export function loadCodes(fetcher: Fetch, apiOrigin: string, campaignId: string, offset: number): Promise<Read<CodePage>> {
  return call(fetcher, `${apiOrigin}/su/v1/promo/campaigns/${encodeURIComponent(campaignId)}/codes?offset=${offset}&limit=${PAGE_SIZE}`, reading, is200, parseCodePage)
}

export function createCampaign(fetcher: Fetch, apiOrigin: string, input: CampaignInput, key: string): Promise<Read<{ campaign: PromoCampaign; replay: boolean }>> {
  return call(fetcher, `${apiOrigin}/su/v1/promo/campaigns`, writing(key, input), created, (raw) => {
    const b = bag(raw, "body")
    return { campaign: parseCampaign(b.campaign), replay: b.replay === true }
  })
}

export function issueCode(fetcher: Fetch, apiOrigin: string, campaignId: string, redemptionLimit: number, key: string): Promise<Read<IssueAnswer>> {
  return call(fetcher, `${apiOrigin}/su/v1/promo/campaigns/${encodeURIComponent(campaignId)}/codes`, writing(key, { redemption_limit: redemptionLimit }), created, (raw) => {
    const b = bag(raw, "body")
    const replay = b.replay === true
    const code = b.code === null || b.code === undefined ? null : text(b.code, "code")
    // A fresh issue without its plaintext would leave a code nobody can hand out.
    if (!replay && code === null) throw new PromoShapeError("code")
    const receipt = b.receipt && typeof b.receipt === "object" ? (b.receipt as Bag) : {}
    return { code, replay, codeId: optionalText(receipt.code_id), issuedAt: optionalText(receipt.issued_at) }
  })
}

export function revokeCampaign(fetcher: Fetch, apiOrigin: string, campaignId: string, key: string): Promise<Read<{ campaign: PromoCampaign; replay: boolean }>> {
  return call(fetcher, `${apiOrigin}/su/v1/promo/campaigns/${encodeURIComponent(campaignId)}/revoke`, writing(key, {}), is200, (raw) => {
    const b = bag(raw, "body")
    return { campaign: parseCampaign(b.campaign), replay: b.replay === true }
  })
}

/** A fresh key for one intent; a retry of the same intent sends the same key. */
export function newIdempotencyKey(random: () => string = () => globalThis.crypto.randomUUID()): string {
  return "admin-" + random()
}
