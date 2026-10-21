// What an operator has to act on: the contract checks, the bounded read and the shared reader.
// `node --experimental-strip-types --test web/console/src/cloud/admin/health/attention.test.ts`.
// Every body below is synthetic.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node.
import { attentionCount, attentionWatch, parseAttention, readAttention } from "./attention.ts"
// @ts-expect-error -- a `.ts` path, for node.
import { healthWords } from "./words.ts"

const INC = "hi_" + "a".repeat(32)
const item = (over: Record<string, unknown> = {}) => ({
  id: "att-1", incident_id: INC, incident_revision: 2, action: "investigate",
  reason: "Messages to one machine stall after it reconnects.", href: `/admin/health/incidents/${INC}`, ...over,
})
const ok = (items: unknown[] | null, over: Record<string, unknown> = {}) => ({ schema: "clawdline.health_attention.v1", status: "ok", items, complete: true, ...over })

function fakeFetch(answer: () => Response | Promise<Response>) {
  const calls: string[] = []
  const get = (async (url: string) => { calls.push(String(url)); return answer() }) as unknown as typeof fetch
  return { get, calls }
}
const json = (body: unknown, status = 200) => () => new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } })

test("each known action is kept, in the Cloud's order, and counted", () => {
  const a = parseAttention(ok([item(), item({ id: "att-2", action: "review_fix" }), item({ id: "att-3", action: "review_result" })]))
  assert.equal(a?.state, "ok")
  assert.deepEqual(a.items.map((i: { action: string }) => i.action), ["investigate", "review_fix", "review_result"])
  assert.equal(attentionCount(a), 3)
})

test("an untrustworthy body is a failure, never zero items", () => {
  const bad = [
    ok([item({ action: "approve" })]),
    ok([item({ href: `https://evil.example/admin/health/incidents/${INC}` })]),
    ok([item({ href: `//evil.example/admin/health/incidents/${INC}` })]),
    ok([item({ href: `/admin/health/incidents/${INC}?x=1` })]),
    ok([item({ href: "/admin/health/incidents/hi_" + "b".repeat(32) })]),
    ok([item({ incident_id: "../x", href: "/admin/health/incidents/..%2Fx" })]),
    ok([item({ incident_revision: 0 })]),
    ok([item(), item()]),
    ok([item()], { schema: "clawdline.health_attention.v2" }),
    ok([item()], { complete: "yes" }),
    ok(null),
    { ...ok([item()]), status: "fine" },
  ]
  for (const body of bad) {
    assert.equal(parseAttention(body), null, JSON.stringify(body))
    assert.equal(attentionCount(parseAttention(body)), null)
  }
})

test("unavailable and an empty list are different answers", () => {
  const unavailable = parseAttention({ schema: "clawdline.health_attention.v1", status: "unavailable", items: null, complete: false })
  assert.equal(unavailable?.state, "unavailable")
  assert.equal(attentionCount(unavailable), null)
  const none = parseAttention(ok([]))
  assert.equal(none?.state, "ok")
  assert.equal(attentionCount(none), 0)
})

test("a read keeps refusals and failures typed", async () => {
  assert.deepEqual(await readAttention("https://api.test", fakeFetch(json({}, 401)).get), { state: "failed", code: "signed_out" })
  assert.deepEqual(await readAttention("https://api.test", fakeFetch(json({ code: "not_found" }, 404)).get), { state: "failed", code: "forbidden" })
  assert.deepEqual(await readAttention("https://api.test", fakeFetch(json({}, 503)).get), { state: "failed", code: "http_error" })
  assert.deepEqual(await readAttention("https://api.test", fakeFetch(() => { throw new TypeError("offline") }).get), { state: "failed", code: "network" })
  assert.deepEqual(await readAttention("https://api.test", fakeFetch(() => new Response("not json")).get), { state: "failed", code: "contract" })
  const f = fakeFetch(json(ok([item()])))
  const a = await readAttention("https://api.test", f.get)
  assert.equal(a.state, "ok")
  assert.deepEqual(f.calls, ["https://api.test/su/v1/health/attention"])
})

const doc = (state = "visible") => {
  const fns = new Set<() => void>()
  return {
    visibilityState: state,
    addEventListener: (_t: string, fn: () => void) => fns.add(fn),
    removeEventListener: (_t: string, fn: () => void) => fns.delete(fn),
    show() { this.visibilityState = "visible"; for (const fn of fns) fn() },
  }
}
const tick = (ms: number) => new Promise((r) => setTimeout(r, ms))

test("nothing is read until someone watches, and many watchers share one read", async () => {
  const f = fakeFetch(json(ok([item()])))
  const w = attentionWatch("https://api.test", f.get, doc(), 60_000)
  await tick(5)
  assert.equal(f.calls.length, 0)
  const seen: unknown[] = []
  const stopA = w.subscribe((a: unknown) => seen.push(a))
  const stopB = w.subscribe(() => {})
  await tick(10)
  assert.equal(f.calls.length, 1)
  assert.equal(attentionCount(w.current()), 1)
  stopA(); stopB()
})

test("a hidden page does not read until it is shown", async () => {
  const f = fakeFetch(json(ok([])))
  const d = doc("hidden")
  const stop = attentionWatch("https://api.test", f.get, d, 60_000).subscribe(() => {})
  await tick(10)
  assert.equal(f.calls.length, 0)
  d.show()
  await tick(10)
  assert.equal(f.calls.length, 1)
  stop()
})

test("a failure waits the whole interval before reading again", async () => {
  const f = fakeFetch(json({}, 503))
  const stop = attentionWatch("https://api.test", f.get, doc(), 80).subscribe(() => {})
  await tick(40)
  assert.equal(f.calls.length, 1)
  await tick(80)
  assert.equal(f.calls.length, 2)
  stop()
  await tick(120)
  assert.equal(f.calls.length, 2)
})

test("every state and action has words in both languages", () => {
  for (const locale of ["en", "zh-TW"] as const) {
    const say = healthWords(locale)
    for (const key of ["at_title", "at_loading", "at_failed", "at_unavailable", "at_none", "at_noneIncomplete", "at_incomplete",
      "at_action_investigate", "at_action_review_fix", "at_action_review_result", "at_incident"]) {
      assert.notEqual(say(key as never), key, `${locale} ${key}`)
    }
  }
  // An action is never described as approving anything: there is no approval to give.
  assert.doesNotMatch(healthWords("en")("at_action_review_fix"), /approve/i)
  assert.doesNotMatch(healthWords("zh-TW")("at_action_review_fix"), /核可|批准/)
})
