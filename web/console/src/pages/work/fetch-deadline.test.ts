import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node strips source TypeScript in the browser-free test.
import { fetchWithDeadline } from "./fetch-deadline.ts"

test("a hosted fetch that ignores abort cannot hold the project page forever", async () => {
  const previous = globalThis.fetch
  globalThis.fetch = () => new Promise<Response>(() => {})
  try {
    await assert.rejects(fetchWithDeadline("/v1/places?machine=one", {}, 1), { name: "AbortError" })
  } finally { globalThis.fetch = previous }
})

test("a read that answers when its deadline fires is waited for, for a bounded time", async () => {
  const previous = globalThis.fetch
  globalThis.fetch = (_input, init) => new Promise<Response>((resolve) => {
    init?.signal?.addEventListener("abort", () => { setTimeout(() => resolve(new Response("held")), 1) })
  })
  try {
    assert.equal(await (await fetchWithDeadline("/v1/sessions", {}, 1, 200)).text(), "held")
    globalThis.fetch = () => new Promise<Response>(() => {})
    await assert.rejects(fetchWithDeadline("/v1/sessions", {}, 1, 5), { name: "AbortError" })
  } finally { globalThis.fetch = previous }
})
