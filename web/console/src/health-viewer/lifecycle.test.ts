// The hosted page's producer lifecycle (`lifecycle.ts`) and the write path's trace
// (`cloud-client.js` `_publishAcknowledged`), with a fake start and a fake relay.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- node's strip-types runner imports the .ts file directly.
import { ViewerHealthLifecycle, VIEWER_HEALTH_BUILD_FLAG } from "./lifecycle.ts"
import { installViewerHealthObserver } from "../legacy/js/net/viewer-health-hook.js"
import { CloudClient } from "../legacy/js/net/cloud-client.js"

function fakeStart() {
  const calls: string[] = []
  const start = (config: any) => {
    if (!config || config.enabled !== true) return null
    const n = calls.filter((c) => c.startsWith("start")).length
    calls.push(`start:${config.apiOrigin}:${config.targetMachineId ?? "-"}`)
    return {
      status: () => ({}) as any,
      setTargetMachine: (machine: string | null | undefined) => { calls.push(`target#${n}:${machine ?? "-"}`) },
      stop: (state?: string) => { calls.push(`stop#${n}:${state ?? "stopped"}`) },
    }
  }
  return { calls, start }
}

const API = "https://api.example.test"
const page = (over: Record<string, unknown> = {}) =>
  ({ apiOrigin: API, signedIn: true, account: "acct-1", targetMachineId: null, ...over }) as any

test("the build flag is off unless the page was built with it", () => {
  assert.equal(VIEWER_HEALTH_BUILD_FLAG, false)
})

test("off never starts, whatever the page's sign-in says", () => {
  const { calls, start } = fakeStart()
  const life = new ViewerHealthLifecycle({ enabled: false, start: start as any })
  life.update(page())
  life.update(page({ targetMachineId: "mac-a" }))
  assert.deepEqual(calls, [])
  assert.equal(life.running(), false)
})

test("sign-in starts once; a target switch retargets without a restart; sign-out stops", () => {
  const { calls, start } = fakeStart()
  const life = new ViewerHealthLifecycle({ enabled: true, start: start as any })
  life.update(page({ signedIn: false, account: null }))
  assert.deepEqual(calls, [])
  life.update(page())
  life.update(page())
  life.update(page({ targetMachineId: "mac-a" }))
  life.update(page({ targetMachineId: "mac-a" }))
  life.update(page({ targetMachineId: "mac-b" }))
  life.update(page({ signedIn: false, account: null, targetMachineId: "mac-b" }))
  assert.deepEqual(calls, [`start:${API}:-`, "target#0:mac-a", "target#0:mac-b", "stop#0:stopped"])
  assert.equal(life.running(), false)
})

test("another account gets a fresh producer; leaving the Cloud stops it", () => {
  const { calls, start } = fakeStart()
  const life = new ViewerHealthLifecycle({ enabled: true, start: start as any })
  life.update(page({ targetMachineId: "mac-a" }))
  life.update(page({ account: "acct-2", targetMachineId: "mac-c" }))
  life.update(page({ account: "acct-2", apiOrigin: null }))
  assert.deepEqual(calls, [`start:${API}:mac-a`, "stop#0:stopped", `start:${API}:mac-c`, "stop#1:stopped"])
})

test("a revocation stops with the state the page names", () => {
  const { calls, start } = fakeStart()
  const life = new ViewerHealthLifecycle({ enabled: true, start: start as any })
  life.update(page())
  life.stop("session_gone")
  life.update(page({ signedIn: false, account: null }))
  assert.deepEqual(calls, [`start:${API}:-`, "stop#0:session_gone"])
})

/* ---- the write path ---------------------------------------------------------------------- */

class QuietSocket {
  readyState = 1
  onmessage: ((event: { data: string }) => void) | null = null
  onclose: ((event: { code: number }) => void) | null = null
  onerror: (() => void) | null = null
  send() {}
  say(frame: unknown) { setImmediate(() => this.onmessage?.({ data: JSON.stringify(frame) })) }
  close() { this.readyState = 3 }
}

async function writer(relay: (pending: any) => void) {
  let socket: QuietSocket | null = null
  const Socket = class extends QuietSocket { constructor() { super(); socket = this } }
  const client = new CloudClient({
    relayURL: "https://relay.example.test", deviceToken: "token", devicePrivateKey: { extractable: false },
    deviceID: "viewer-1", account: "account-1", allowWrites: true, nextSequence: async () => 1,
    WebSocket: Socket, readTimeoutMs: 300, ackTimeoutMs: 20,
  }) as any
  client._publishCommand = async function (this: any, _m: string, _t: string, _b: unknown, _c: string, pending: any) {
    pending.registered = { seq: 9 }
    this.pendingBySequence.set(9, pending.registered)
    setImmediate(() => relay(pending))
    return { seq: 9 }
  }
  await client.start({ quiet: true })
  socket!.say({ type: "ready", v: 1, role: "viewer", account: "account-1", device: "viewer-1" })
  await client.whenReady()
  return client
}

function collect() {
  const records: any[] = []
  let n = 0
  const uninstall = installViewerHealthObserver({
    record: (o: any) => records.push(o), correlation: () => "vt-" + (n++), monotonic: () => 0,
  })
  return { records, uninstall }
}

const trace = (records: any[]) => records.filter((r) => r.class === "control")
  .map((r) => [r.stage, r.event, r.outcome ?? null, r.error ?? null])

test("a write the relay acknowledges is issued then received, under one correlation", async () => {
  const { records, uninstall } = collect()
  const client = await writer((pending) => pending.ack.settle(null))
  try {
    await client._publishAcknowledged("mac-a", "answer", { session: "s", answer: "y" }, "ctl")
    assert.deepEqual(trace(records), [
      ["viewer_request_issued", "viewer_stage_started", null, null],
      ["viewer_response_received", "viewer_stage_completed", "ok", null],
    ])
    assert.equal(new Set(records.filter((r) => r.class === "control").map((r) => r.correlation)).size, 1)
  } finally { client.stop(); uninstall() }
})

test("a write the relay refuses is a failed response with the fixed word only", async () => {
  const { records, uninstall } = collect()
  const client = await writer((pending) => pending.ack.settle(Object.assign(new Error("Mac offline: secret text"), { code: "offline" })))
  try {
    await assert.rejects(client._publishAcknowledged("mac-a", "answer", { session: "s", answer: "y" }, "ctl"))
    assert.deepEqual(trace(records).at(-1), ["viewer_response_received", "viewer_stage_failed", "error", "carrier_closed"])
    assert.ok(!JSON.stringify(records).includes("secret"))
  } finally { client.stop(); uninstall() }
})

test("a write that heard nothing before the ack timer is pending, never ok", async () => {
  const { records, uninstall } = collect()
  const client = await writer(() => {})
  try {
    await client._publishAcknowledged("mac-a", "answer", { session: "s", answer: "y" }, "ctl")
    assert.deepEqual(trace(records).at(-1), ["viewer_response_received", "viewer_stage_completed", "pending", null])
  } finally { client.stop(); uninstall() }
})
