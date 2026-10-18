// The operations overview against the v1 contract and a fake API:
// `node --experimental-strip-types --test web/console/src/cloud/admin/admin.test.ts`.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see cloud/forget.test.ts.
import { completeDays, loadOverview, parseOverview, trend } from "./api.ts"
// @ts-expect-error -- a `.ts` path, for node.
import { ADMIN_ROUTES, adminLocale, adminPath, adminRoute, adminWords, type AdminWord } from "./words.ts"
// @ts-expect-error -- a `.ts` path, for node.
import { demoOverview } from "./demo.ts"

function body(over: Record<string, unknown> = {}) {
  const counters = { dispatches: 12, connect_minutes: 340, bytes_in: 1_200_000, bytes_out: 5_000 }
  return {
    schema: "clawdline.operations.overview.v1",
    generated_at: "2026-10-18T09:30:00Z",
    timezone: "UTC",
    accounts: {
      total: 42, new_7d: { current: 6, previous: 3 }, new_30d: { current: 20, previous: 0 },
      daily: [{ day: "2026-10-17", count: 2 }, { day: "2026-10-16", count: 1 }],
    },
    promotions: {
      campaigns_total: 51, codes_total: 3, redemptions_total: 9, redeemers_total: 9,
      redeemed_7d: { current: 2, previous: 4 }, redeemed_30d: { current: 9, previous: 9 },
      daily: [],
      campaigns: { total: 51, shown: 1, rows: [{
        id: "c1", name: "Launch", status: "redeemable", not_before: "2026-10-01T00:00:00Z", expires_at: null,
        redemption_limit: 100, redeemed_count: 9, issued_codes: 1, available_codes: 1,
      }] },
    },
    usage: {
      current: { period: "2026-10", coverage: "partial", active_accounts: 7, counters: { ...counters, dispatches: null } },
      previous: { period: "2026-09", coverage: "available", active_accounts: 11, counters },
    },
    ...over,
  }
}

test("a v1 overview is read field for field, and a null counter stays unknown, not zero", () => {
  const o = parseOverview(body())
  assert.equal(o.accounts.total, 42)
  assert.deepEqual(o.accounts.daily.map((d: { day: string }) => d.day), ["2026-10-16", "2026-10-17"])
  assert.equal(o.usage.current.counters.dispatches, null)
  assert.equal(o.usage.current.coverage, "partial")
  assert.equal(o.usage.previous.counters.dispatches, 12)
  assert.equal(o.usage.active_daily, null)
  assert.equal(o.promotions.campaigns.total, 51)
  assert.equal(o.promotions.campaigns.shown, 1)
})

test("illustrative periods agree with their daily charts and campaign totals", () => {
  const o = demoOverview(new Date("2026-10-18T09:30:00Z"))
  const sum = (rows: { count: number }[]) => rows.reduce((total, row) => total + row.count, 0)
  assert.equal(o.accounts.daily.length, 30)
  assert.equal(o.accounts.daily.at(-1)?.day, "2026-10-17")
  assert.equal(o.accounts.new_7d.current, sum(o.accounts.daily.slice(-7)))
  assert.equal(o.accounts.new_30d.current, sum(o.accounts.daily))
  assert.equal(o.promotions.redeemed_7d.current, sum(o.promotions.daily.slice(-7)))
  assert.equal(o.promotions.redeemed_30d.current, sum(o.promotions.daily))
  assert.equal(o.promotions.redemptions_total, o.promotions.campaigns.rows.reduce((total, row) => total + row.redeemed_count, 0))
  assert.equal(o.usage.current.period, "2026-10")
  assert.equal(o.usage.previous.period, "2026-09")
  assert.equal(o.usage.active_daily?.length, 30)
  assert.equal(o.usage.active_daily?.at(-1)?.day, "2026-10-17")
})

test("daily activity accepts the additive API field and excludes an unfinished UTC day", () => {
  const source = body({ usage: { ...body().usage, active_daily: [
    { day: "2026-10-17", count: 4 }, { day: "2026-10-18", count: 99 },
  ] } })
  const o = parseOverview(source)
  assert.deepEqual(o.usage.active_daily, [{ day: "2026-10-17", count: 4 }])
})

test("an answer that is not the contract is refused with the field that was wrong", () => {
  assert.throws(() => parseOverview(body({ schema: "clawdline.operations.overview.v2" })), { where: "schema" })
  const negative = body()
  negative.accounts.total = -1
  assert.throws(() => parseOverview(negative), { where: "accounts.total" })
  const missing = body() as Record<string, unknown>
  delete missing.usage
  assert.throws(() => parseOverview(missing), { where: "usage" })
})

test("today's unfinished UTC day never reaches the chart", () => {
  const days = completeDays(
    [{ day: "2026-10-18", count: 1 }, { day: "2026-10-16", count: 4 }, { day: "2026-10-17", count: 0 }],
    "2026-10-18T00:05:00Z",
  )
  assert.deepEqual(days, [{ day: "2026-10-16", count: 4 }, { day: "2026-10-17", count: 0 }])
})

test("the change against the previous period has no percentage over nothing", () => {
  assert.deepEqual(trend({ current: 6, previous: 3 }), { kind: "up", delta: 3, percent: 100 })
  assert.deepEqual(trend({ current: 2, previous: 4 }), { kind: "down", delta: 2, percent: 50 })
  assert.deepEqual(trend({ current: 20, previous: 0 }), { kind: "up", delta: 20, percent: null })
  assert.deepEqual(trend({ current: 9, previous: 9 }), { kind: "flat" })
})

test("a refusal, a failure and a bad body are told apart, and none of them is data", async () => {
  const answer = (status: number, json: () => Promise<unknown>) => async () => ({ status, json })
  let asked: { url: string; init: RequestInit } | null = null
  const ok = await loadOverview(async (url: string, init: RequestInit) => {
    asked = { url, init }
    return { status: 200, json: async () => body() }
  }, "https://api.example")
  assert.equal(ok.kind, "ready")
  assert.equal(asked!.url, "https://api.example/su/v1/operations/overview")
  assert.equal(asked!.init.credentials, "include")
  assert.equal(asked!.init.cache, "no-store")

  assert.deepEqual(await loadOverview(answer(404, async () => ({})), "x"), { kind: "forbidden" })
  assert.deepEqual(await loadOverview(answer(403, async () => ({})), "x"), { kind: "forbidden" })
  assert.deepEqual(await loadOverview(answer(401, async () => ({})), "x"), { kind: "signed_out" })
  assert.deepEqual(await loadOverview(answer(503, async () => ({})), "x"), { kind: "failed", code: "http_503", status: 503 })
  assert.deepEqual(await loadOverview(async () => { throw new TypeError("offline") }, "x"), { kind: "failed", code: "network", status: null })
  assert.deepEqual(await loadOverview(answer(200, async () => { throw new SyntaxError("x") }), "x"), { kind: "failed", code: "not_json", status: 200 })
  assert.deepEqual(await loadOverview(answer(200, async () => ({ schema: "nope" })), "x"), { kind: "failed", code: "shape:schema", status: 200 })
})

test("Traditional Chinese preferences read Taiwan copy; every other language reads English", () => {
  assert.equal(adminLocale(["zh-TW"]), "zh-TW")
  assert.equal(adminLocale(["zh-Hant-HK", "en"]), "zh-TW")
  assert.equal(adminLocale(["ja-JP", "zh-TW"]), "zh-TW")
  assert.equal(adminLocale(["fr-FR"]), "en")
  assert.equal(adminLocale(["en-GB", "zh-TW"]), "en")
  assert.equal(adminWords("zh-TW")("campaignsCapped", { shown: 50, total: 51 }), "共 51 個活動，只顯示最新的 50 個。")
})

test("each page has its own address, and an address names one page or none", () => {
  assert.deepEqual(ADMIN_ROUTES.map(adminPath), ["/admin", "/admin/accounts", "/admin/promotions", "/admin/usage"])
  for (const route of ADMIN_ROUTES) assert.equal(adminRoute(adminPath(route)), route)
  assert.equal(adminRoute("/admin/"), "overview")
  assert.equal(adminRoute("/admin/promotions/"), "promotions")
  assert.equal(adminRoute("/admin/overview"), null)
  assert.equal(adminRoute("/admin/nope"), null)
  assert.equal(adminRoute("/administrator"), null)
  assert.equal(adminRoute("/"), null)
})

// The tables are typed against each other; what types cannot see is a key the
// pages compose from a route or a phase, and a placeholder one language lost.
test("both languages have every composed sentence, with the same placeholders", () => {
  const en = adminWords("en")
  const zh = adminWords("zh-TW")
  const marks = (s: string) => [...s.matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort().join(",")
  // Every page, phase and refusal the pages draw by a composed key resolves in both.
  const composed: AdminWord[] = [
    ...ADMIN_ROUTES.flatMap((r: string) => [`nav_${r}`, `hint_${r}`, `lead_${r}`] as AdminWord[]),
    ...["open", "not_started", "expired", "revoked", "limit_reached", "no_codes", "unknown"].map((p) => `phase_${p}` as AdminWord),
  ]
  for (const key of composed) {
    assert.ok(en(key).length > 0, key)
    assert.ok(zh(key).length > 0, key)
    assert.equal(marks(zh(key)), marks(en(key)), key)
  }
  assert.equal(zh("pageRange", { from: 51, to: 100, total: 230 }), "第 51–100 筆，共 230 筆")
})
