// The promotion routes against their contract and a fake API:
// `node --experimental-strip-types --test web/console/src/cloud/admin/promo-api.test.ts`.
import { test } from "node:test"
import assert from "node:assert/strict"
import {
  campaignPhase, canIssue, checkCampaignDraft, checkCodeLimit, createCampaign, issueCode, listedFromOverview,
  loadCampaigns, loadCodes, pageWindow, parseCampaign, parseCampaignPage, revokeCampaign, revokeCode,
  // @ts-expect-error -- a `.ts` path, for node; see cloud/forget.test.ts.
} from "./promo-api.ts"

const API = "https://api.example"

function raw(over: Record<string, unknown> = {}) {
  return {
    id: "pc_1", name: "Launch", tier: "pro", duration_calendar_months: 3,
    not_before: "2026-10-01T00:00:00.000Z", expires_at: "2026-12-01T00:00:00.000Z",
    redemption_limit: 10, redeemed_count: 3, conflict_policy: "reject", state: "active",
    created_at: "2026-09-30T08:00:00.000Z",
    ...over,
  }
}

type Seen = { url: string; init: RequestInit }

function fake(status: number, body: unknown, seen: Seen[] = []) {
  return async (url: string, init: RequestInit) => {
    seen.push({ url, init })
    return { status, json: async () => body }
  }
}

test("a campaign page is read field for field, and an unknown state is not taken for active", () => {
  const page = parseCampaignPage({ total: 51, offset: 50, rows: [raw(), raw({ id: "pc_2", state: "paused" })] })
  assert.equal(page.total, 51)
  assert.equal(page.offset, 50)
  assert.equal(page.rows[0].conflict_policy, "reject")
  assert.equal(page.rows[1].state, "unknown")
  assert.throws(() => parseCampaignPage({ total: 1, offset: 0, rows: [raw({ redeemed_count: -1 })] }), { where: "rows[0].redeemed_count" })
  assert.throws(() => parseCampaignPage({ total: 1, offset: 0 }), { where: "rows" })
})

test("the list and the code reads ask for 50 at an offset, with the session cookie and no cache", async () => {
  const seen: Seen[] = []
  const list = await loadCampaigns(fake(200, { total: 0, offset: 100, rows: [] }, seen), API, 100)
  assert.equal(list.kind, "ready")
  assert.equal(seen[0].url, API + "/su/v1/promo/campaigns?offset=100&limit=50")
  assert.equal(seen[0].init.credentials, "include")
  assert.equal(seen[0].init.cache, "no-store")
  const codes = await loadCodes(fake(200, {
    campaign: { id: "pc/1", name: "x", state: "revoked", not_before: "2026-10-01T00:00:00Z", expires_at: "2026-11-01T00:00:00Z", redemption_limit: 5, redeemed_count: 5 },
    total: 1, offset: 0, rows: [{ id: "pk_1", issued_at: "2026-10-02T00:00:00Z", revoked_at: "2026-10-03T00:00:00Z", redemption_limit: 5, redeemed_count: 5 }],
  }, seen), API, "pc/1", 0)
  assert.equal(seen[1].url, API + "/su/v1/promo/campaigns/pc%2F1/codes?offset=0&limit=50")
  assert.equal(codes.kind, "ready")
  assert.equal(codes.value.rows[0].redeemed_count, 5)
  assert.equal(codes.value.rows[0].public_code, null)
  assert.equal(codes.value.rows[0].revoked_at, "2026-10-03T00:00:00Z")
  assert.equal(codes.value.campaign.state, "revoked")
})

test("no access, a named refusal, a network failure and a bad body are told apart", async () => {
  assert.deepEqual(await loadCampaigns(fake(404, { error: { code: "not_found" } }), API, 0), { kind: "no_access" })
  assert.deepEqual(await loadCampaigns(fake(401, {}), API, 0), { kind: "no_access" })
  assert.deepEqual(await loadCodes(fake(404, { error: { code: "unknown_campaign" } }), API, "x", 0), { kind: "failed", code: "unknown_campaign", status: 404 })
  assert.deepEqual(await loadCampaigns(fake(429, { error: { code: "admin_pre_auth_rate_limited" } }), API, 0), { kind: "failed", code: "admin_pre_auth_rate_limited", status: 429 })
  assert.deepEqual(await loadCampaigns(fake(502, "<html>"), API, 0), { kind: "failed", code: "http_502", status: 502 })
  assert.deepEqual(await loadCampaigns(async () => { throw new TypeError("offline") }, API, 0), { kind: "failed", code: "network", status: null })
  assert.deepEqual(await loadCampaigns(fake(200, { total: "many" }), API, 0), { kind: "failed", code: "shape:total", status: 200 })
})

test("every write is a POST with its Idempotency-Key and the body the server checks", async () => {
  const seen: Seen[] = []
  const input = { name: "Launch", not_before: "2026-10-01T00:00:00.000Z", expires_at: "2026-11-01T00:00:00.000Z", redemption_limit: 10, conflict_policy: "reject" as const }
  const made = await createCampaign(fake(201, { campaign: raw(), replay: false }, seen), API, input, "k1")
  assert.equal(made.kind, "ready")
  assert.equal(seen[0].url, API + "/su/v1/promo/campaigns")
  assert.equal(seen[0].init.method, "POST")
  assert.equal((seen[0].init.headers as Record<string, string>)["Idempotency-Key"], "k1")
  assert.deepEqual(JSON.parse(seen[0].init.body as string), input)

  await issueCode(fake(201, { code: "clp_secret", replay: false, receipt: { code_id: "pk_1", issued_at: "2026-10-02T00:00:00Z" } }, seen), API, "pc_1", 2, "k2")
  assert.equal(seen[1].url, API + "/su/v1/promo/campaigns/pc_1/codes")
  assert.deepEqual(JSON.parse(seen[1].init.body as string), { redemption_limit: 2 })

  await issueCode(fake(201, { code: "HACKER-NEWS", replay: false, receipt: { code_id: "pk_2" } }, seen), API, "pc_1", 10, "k-public", "HACKER-NEWS")
  assert.deepEqual(JSON.parse(seen[2].init.body as string), { redemption_limit: 10, public_code: "HACKER-NEWS" })

  const stopped = await revokeCode(fake(200, { code_id: "pk/1", revoked_at: "2026-10-03T00:00:00Z", replay: false }, seen), API, "pc/1", "pk/1", "k-stop")
  assert.equal(stopped.kind, "ready")
  assert.equal(seen[3].url, API + "/su/v1/promo/campaigns/pc%2F1/codes/pk%2F1/revoke")
  assert.equal((seen[3].init.headers as Record<string, string>)["Idempotency-Key"], "k-stop")

  const revoked = await revokeCampaign(fake(200, { campaign: raw({ state: "revoked" }), replay: false }, seen), API, "pc_1", "k3")
  assert.equal(seen[4].url, API + "/su/v1/promo/campaigns/pc_1/revoke")
  assert.deepEqual(JSON.parse(seen[4].init.body as string), {})
  assert.equal(revoked.kind === "ready" && revoked.value.campaign.state, "revoked")
})

test("an issue hands back its plaintext once; a replay has none, and a fresh issue without one is refused", async () => {
  const fresh = await issueCode(fake(201, { code: "clp_abc", replay: false, receipt: { code_id: "pk_1", issued_at: "2026-10-02T00:00:00Z" } }), API, "c", 1, "k")
  assert.deepEqual(fresh, { kind: "ready", value: { code: "clp_abc", replay: false, codeId: "pk_1", issuedAt: "2026-10-02T00:00:00Z" } })
  const replay = await issueCode(fake(200, { code: null, replay: true, receipt: { code_id: "pk_1" } }), API, "c", 1, "k")
  assert.equal(replay.kind === "ready" && replay.value.code, null)
  assert.deepEqual(await issueCode(fake(201, { code: null, replay: false }), API, "c", 1, "k"), { kind: "failed", code: "shape:code", status: 201 })
  assert.deepEqual(await issueCode(fake(409, { error: { code: "campaign_unavailable" } }), API, "c", 1, "k"), { kind: "failed", code: "campaign_unavailable", status: 409 })
})

test("a draft is checked as the server checks it, and every bad field is named", () => {
  const now = Date.parse("2026-10-18T00:00:00Z")
  const ok = checkCampaignDraft({ name: "  Launch ", notBefore: "2026-10-18T00:00:00Z", expiresAt: "2026-11-18T00:00:00Z", redemptionLimit: "25" }, now)
  assert.deepEqual(ok, { ok: true, input: { name: "Launch", not_before: "2026-10-18T00:00:00.000Z", expires_at: "2026-11-18T00:00:00.000Z", redemption_limit: 25, conflict_policy: "reject" } })
  const bad = checkCampaignDraft({ name: "", notBefore: "", expiresAt: "2026-10-17T00:00:00Z", redemptionLimit: "2.5" }, now)
  assert.deepEqual(bad, { ok: false, problems: { name: "name_missing", notBefore: "start_missing", expiresAt: "end_in_past", redemptionLimit: "limit_not_whole" } })
  const long = checkCampaignDraft({ name: "x".repeat(121), notBefore: "2026-10-18T00:00:00Z", expiresAt: "2027-10-20T00:00:00Z", redemptionLimit: "1000001" }, now)
  assert.deepEqual(long, { ok: false, problems: { name: "name_too_long", expiresAt: "window_too_long", redemptionLimit: "limit_too_large" } })
  const backwards = checkCampaignDraft({ name: "a", notBefore: "2026-11-01T00:00:00Z", expiresAt: "2026-10-20T00:00:00Z", redemptionLimit: "1" }, now)
  assert.deepEqual(backwards, { ok: false, problems: { expiresAt: "end_before_start" } })
  assert.equal(checkCodeLimit("3", 10), 3)
  assert.equal(checkCodeLimit("11", 10), "limit_too_large")
  assert.equal(checkCodeLimit("0", 10), "limit_not_whole")
})

test("a campaign's phase comes from its state and the clock, revoked first", () => {
  const now = Date.parse("2026-10-18T00:00:00Z")
  const campaign = (over: Record<string, unknown> = {}) => parseCampaign(raw(over))
  assert.equal(campaignPhase(campaign(), now), "open")
  assert.equal(campaignPhase(campaign({ state: "revoked", expires_at: "2026-01-01T00:00:00Z" }), now), "revoked")
  assert.equal(campaignPhase(campaign({ expires_at: "2026-10-17T00:00:00Z" }), now), "expired")
  assert.equal(campaignPhase(campaign({ not_before: "2026-10-19T00:00:00Z" }), now), "not_started")
  assert.equal(campaignPhase(campaign({ redeemed_count: 10 }), now), "limit_reached")
  assert.equal(campaignPhase(campaign({ state: "paused" }), now), "unknown")
  assert.equal(canIssue(campaign({ not_before: "2026-10-19T00:00:00Z" }), now), true)
  assert.equal(canIssue(campaign({ state: "revoked" }), now), false)
  assert.equal(listedFromOverview({
    id: "c", name: "", status: "no_available_code", not_before: null, expires_at: null,
    redemption_limit: null, redeemed_count: 0, issued_codes: 2, available_codes: 0,
  }).phase, "no_codes")
})

test("a page says where it sits in the whole, and an empty or single page offers no moves", () => {
  assert.deepEqual(pageWindow(0, 50, 120), { from: 1, to: 50, total: 120, previous: null, next: 50 })
  assert.deepEqual(pageWindow(100, 20, 120), { from: 101, to: 120, total: 120, previous: 50, next: null })
  assert.deepEqual(pageWindow(0, 0, 0), { from: 0, to: 0, total: 0, previous: null, next: null })
})
