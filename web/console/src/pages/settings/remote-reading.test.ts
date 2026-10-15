import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node strips source TypeScript in the browser-free test.
import { MACHINE_ONLY, remoteReading } from "./remote-reading.ts"

const answer = (status: number, body: unknown = { detail: "x" }) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })

test("a browser refused because only this machine may read the Remote tab is told so, not that reading failed", async () => {
  assert.equal(await remoteReading(answer(403, { error: "forbidden" })), MACHINE_ONLY)
})

test("any other failure is still a failure to read", async () => {
  for (const status of [401, 404, 500, 503]) assert.equal(await remoteReading(answer(status)), null, `status ${status}`)
})

test("an answer is the status itself", async () => {
  assert.deepEqual(await remoteReading(answer(200, { enabled: false, state: "off" })), { enabled: false, state: "off" })
})
