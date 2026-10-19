import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- esbuild bundles the TypeScript source for Node's test runner.
import { listViewerDevices, revokeViewerDevice } from "./viewer-devices.ts"

const answer = (status: number, body: unknown) => async () => new Response(JSON.stringify(body), { status })

test("the list leaves out revoked devices and CLI credentials, which are not browsers", async () => {
  const listed = await listViewerDevices("https://api", answer(200, { devices: [
    { id: "a", kind: "browser", name: "Browser", last_seen_at: "2026-10-19T01:00:00Z", created_at: "2026-10-01T00:00:00Z", revoked_at: null },
    { id: "b", kind: "ios", name: "Phone", last_seen_at: null, created_at: "2026-10-02T00:00:00Z", revoked_at: "2026-10-03T00:00:00Z" },
    { id: "c", kind: "cli", name: "cli", last_seen_at: null, created_at: "2026-10-02T00:00:00Z", revoked_at: null },
  ] }))
  assert.equal(listed.kind, "listed")
  assert.deepEqual(listed.devices.map((d: { id: string }) => d.id), ["a"])
})

test("a list the account refuses or nobody can read says which", async () => {
  assert.deepEqual(await listViewerDevices("https://api", answer(401, {})), { kind: "refused", status: 401 })
  assert.deepEqual(await listViewerDevices("https://api", answer(200, { nope: 1 })), { kind: "unreadable", status: 200 })
  assert.deepEqual(await listViewerDevices("https://api", async () => { throw new Error("offline") }), { kind: "unreadable", status: null })
})

test("revoking says revoked, refused, absent, or not known — and never asks twice", async () => {
  let calls = 0
  const once = (status: number, body: unknown) => async () => { calls++; return new Response(JSON.stringify(body), { status }) }
  assert.deepEqual(await revokeViewerDevice("https://api", "a", once(200, { routing: "stopped" })), { kind: "revoked" })
  assert.deepEqual(await revokeViewerDevice("https://api", "a", once(403, { error: { code: "forbidden" } })),
    { kind: "refused", status: 403, code: "forbidden" })
  assert.deepEqual(await revokeViewerDevice("https://api", "a", once(404, {})), { kind: "absent", status: 404, code: "http_404" })
  assert.deepEqual(await revokeViewerDevice("https://api", "a", once(502, {})), { kind: "unreadable", status: 502, code: "http_502" })
  assert.equal(calls, 4)
})
