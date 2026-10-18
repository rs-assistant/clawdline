import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { bindPromoForm, capturePromoLink, pendingPromoCode, redeemPromo, savePromoCode, clearPromoCode, promoWords } from "./promo.ts"

function storage(): Storage {
  const entries = new Map<string, string>()
  return {
    get length() { return entries.size },
    clear: () => entries.clear(),
    getItem: (key) => entries.get(key) ?? null,
    key: (index) => [...entries.keys()][index] ?? null,
    removeItem: (key) => { entries.delete(key) },
    setItem: (key, value) => { entries.set(key, value) },
  }
}

const code = "clp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopq"

test("a promo fragment is removed before login and retained only in this tab", () => {
  const held = storage()
  let replaced = ""
  const scope = {
    location: { hash: `#page=plan&promo=${code}`, pathname: "/", search: "" },
    history: { state: null, replaceState(_state: unknown, _title: string, url: string) { replaced = url } },
    sessionStorage: held,
  } as unknown as Window
  capturePromoLink(scope)
  assert.equal(replaced, "/#page=plan")
  assert.equal(pendingPromoCode(held), code)
})

test("a named public event code survives the login redirect in canonical form", () => {
  const held = storage()
  let replaced = ""
  capturePromoLink({
    location: { hash: "#page=plan&promo=hacker-news", pathname: "/", search: "" },
    history: { state: null, replaceState(_state: unknown, _title: string, url: string) { replaced = url } },
    sessionStorage: held,
  } as unknown as Window)
  assert.equal(replaced, "/#page=plan")
  assert.equal(pendingPromoCode(held), "HACKER-NEWS")
})

test("retries use one intent and only a server grant establishes success", async () => {
  const held = storage()
  savePromoCode(held, code)
  const keys: string[] = []
  const fetcher = (async (_url: string, init: RequestInit) => {
    keys.push((init.headers as Record<string, string>)["Idempotency-Key"])
    return new Response(JSON.stringify({ status: "rejected", reason: "unavailable" }), { status: 200 })
  }) as typeof fetch
  assert.deepEqual(await redeemPromo("https://api.example", code, held, fetcher), { status: "rejected", reason: "unavailable" })
  await redeemPromo("https://api.example", code, held, fetcher)
  assert.equal(keys[0], keys[1])
  clearPromoCode(held)
  assert.equal(pendingPromoCode(held), "")
})

test("paid conflicts and Taiwan wording remain distinct", async () => {
  const held = storage()
  const fetcher = (async () => new Response(JSON.stringify({ status: "conflict", reason: "paid_entitlement_exists" }),
    { status: 200 })) as typeof fetch
  assert.deepEqual(await redeemPromo("https://api.example", code, held, fetcher),
    { status: "conflict", reason: "paid_entitlement_exists" })
  assert.match(promoWords("zh-Hant-TW").conflict, /付費方案/)
  assert.match(promoWords("zh-Hans").conflict, /paid plan/)
})

test("the Plan form shows the server receipt and rereads entitlements before claiming Pro", async () => {
  const held = storage()
  const originalStorage = globalThis.sessionStorage
  const originalFetch = globalThis.fetch
  Object.defineProperty(globalThis, "sessionStorage", { configurable: true, value: held })
  const nodes = new Map<string, Record<string, unknown>>()
  const handlers: { submit?: (event: Event) => Promise<void> } = {}
  for (const id of ["plan-promo-form", "plan-promo-code", "plan-promo-message", "plan-promo-submit",
    "plan-promo-cloud", "plan-promo-title", "plan-promo-label"]) {
    nodes.set(id, { textContent: "", value: "", disabled: false, hidden: false, href: "",
      addEventListener(_name: string, fn: (event: Event) => Promise<void>) { handlers.submit = fn },
      removeEventListener() { delete handlers.submit } })
  }
  const doc = { documentElement: { lang: "zh-Hant-TW" }, getElementById: (id: string) => nodes.get(id) ?? null } as unknown as Document
  let reads = 0
  globalThis.fetch = (async (_url: RequestInfo | URL, init?: RequestInit) => {
    assert.equal(init?.credentials, "include")
    return new Response(JSON.stringify({ status: "granted", replay: false,
      grant: { expires_at: "2027-01-18T12:00:00.000Z" } }), { status: 201 })
  }) as typeof fetch
  try {
    const unbind = bindPromoForm(doc, "https://api.example", async () => { reads += 1 })
    nodes.get("plan-promo-code")!.value = code
    await handlers.submit?.({ preventDefault() {} } as Event)
    assert.equal(reads, 1)
    assert.match(String(nodes.get("plan-promo-message")?.textContent), /2027-01-18/)
    assert.equal(pendingPromoCode(held), "")
    unbind()
    assert.equal(handlers.submit, undefined)
  } finally {
    globalThis.fetch = originalFetch
    Object.defineProperty(globalThis, "sessionStorage", { configurable: true, value: originalStorage })
  }
})
