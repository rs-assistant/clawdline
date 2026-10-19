import { test } from "node:test"
import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
// @ts-expect-error -- Node runs source tests with type stripping.
import { activateCatalog, CATALOG_TAGS, resetCatalogForTest } from "../catalog.ts"
// @ts-expect-error -- Node runs source tests with type stripping.
import { catalogWord } from "../catalog.ts"
import { planSimulationWords } from "./js/net/billing.js"
import { planSimulationRows } from "./js/view/plan.js"
// @ts-expect-error -- a `.ts` path for Node's test runner.
import { bindPlan, PLAN_ELEMENT_IDS } from "./plan-bridge.ts"

function page() {
  const nodes = new Map<string, {
    textContent: string; hidden: boolean; disabled: boolean; dataset: Record<string, string>
    setAttribute(): void; addEventListener(): void; querySelectorAll(): unknown[]
  }>(PLAN_ELEMENT_IDS.map((id) => [id, {
    textContent: "", hidden: false, disabled: false, dataset: {} as Record<string, string>,
    setAttribute() {}, addEventListener() {}, querySelectorAll() { return [] },
  }]))
  return { doc: { documentElement: { lang: "en" }, getElementById: (id: string) => nodes.get(id) ?? null } as unknown as Document, nodes }
}

test("the hosted Plan page reads the signed-in account while the local page makes no billing call", async () => {
  const original = globalThis.fetch
  let promo = false
  const calls: { url: string; credentials: RequestCredentials | undefined }[] = []
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    calls.push({ url, credentials: init?.credentials })
    if (url.endsWith("/v1/entitlements")) return new Response(JSON.stringify(promo ? {
      entitlements: { tier: "pro", max_machines: 5 }, plan_source: "promo",
      promo_expires_at: "2027-01-18T12:00:00.000Z", purchasable_tiers: ["pro"],
    } : {
      entitlements: { tier: "free", max_machines: 1 }, purchasable_tiers: ["pro"],
    }), { status: 200 })
    return new Response("{}", { status: 404 })
  }) as typeof fetch
  try {
    const hosted = page()
    const hostedPlan = bindPlan(hosted.doc, "https://api.example.test")
    await hostedPlan.enter()
    assert.equal(hostedPlan.state(), "free")
    assert.equal(hosted.nodes.get("plan-upgrade")?.hidden, false)
    assert.ok(calls.some((call) => call.url === "https://api.example.test/v1/entitlements" && call.credentials === "include"))
    promo = true
    await hostedPlan.enter()
    assert.equal(hostedPlan.state(), "paid")
    assert.equal(hosted.nodes.get("plan-portal")?.hidden, true)
    assert.match(hosted.nodes.get("plan-say")?.textContent ?? "", /2027-01-18/)
    await hostedPlan.enter({ returning: true })
    assert.equal(hostedPlan.state(), "waiting")
    hostedPlan.leave()

    const local = page()
    const before = calls.length
    const localPlan = bindPlan(local.doc)
    await localPlan.enter()
    assert.equal(localPlan.state(), "not_here")
    assert.equal(calls.length, before)
  } finally {
    globalThis.fetch = original
  }
})

test("plan simulation furniture uses the validated active catalog in all nine languages", async () => {
  const previous = Object.getOwnPropertyDescriptor(globalThis, "document")
  Object.defineProperty(globalThis, "document", { configurable: true, value: { documentElement: { lang: "en", setAttribute() {} } } })
  try {
    for (const tag of CATALOG_TAGS) {
      const catalog = JSON.parse(readFileSync(new URL(`../../public/catalogs/${tag}.json`, import.meta.url), "utf8"))
      assert.equal(activateCatalog(catalog, tag), tag)
      const { doc, nodes } = page()
      await bindPlan(doc).enter()
      assert.equal(nodes.get("plan-simulation-title")?.textContent, catalog["planSimulation.title"])
      for (const mode of ["actual", "free", "pro"] as const) {
        const key = `${mode}Mode`
        assert.equal(nodes.get(`plan-simulation-${mode}`)?.textContent, catalog[`planSimulation.${key}`])
      }
      const lookup = (key: string) => catalogWord("planSimulation", key)
      const values = planSimulationRows({ actual_tier: "free", effective_tier: "pro", override_source: "superuser_browser_session" }, lookup)
      assert.deepEqual(values.map((row: string[]) => row[0]), ["actual", "effective", "source", "actualEvaluated", "set", "cleared", "evaluated", "expires"].map(key => catalog[`planSimulation.${key}`]))
      assert.equal(values[2][1], "superuser_browser_session")
      assert.equal(planSimulationWords(tag, lookup).failed, catalog["planSimulation.failed"])
    }
  } finally {
    resetCatalogForTest()
    if (previous) Object.defineProperty(globalThis, "document", previous)
    else Reflect.deleteProperty(globalThis, "document")
  }
})
