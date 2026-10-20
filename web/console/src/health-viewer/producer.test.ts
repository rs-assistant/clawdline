// The browser's connection-health producer against a fake Cloud that judges
// each body by the Cloud's own parser rules (`fake-cloud.ts`):
// `node --experimental-strip-types --test web/console/src/health-viewer/producer.test.ts`.
//
// The producer exists so a broken relay still leaves evidence. These tests
// drive the real hook sites — `keepConnected`'s connect loop and the copied
// `CloudClient`'s reads — with fake clocks and transports, and require the
// upload to say what happened, to stay inside its bounds, and to never get in
// the way of the page.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- node runs the source TypeScript directly.
import { ViewerHealthProducer, VIEWER_HEALTH_LIMITS } from "./producer.ts"
// @ts-expect-error -- node runs the source TypeScript directly.
import { startViewerHealth, stopViewerHealth } from "./install.ts"
// @ts-expect-error -- node runs the source TypeScript directly.
import { FakeCloud, parseViewerBody, refused } from "./fake-cloud.ts"
// @ts-expect-error -- node runs the source TypeScript directly.
import { VIEWER_ERRORS, VIEWER_STAGES } from "./record.ts"
import { installViewerHealthObserver, viewerHealthBegin, viewerHealthFailed } from "../legacy/js/net/viewer-health-hook.js"
import { keepConnected } from "../legacy/js/net/cloud-boot.js"
import { CloudClient } from "../legacy/js/net/cloud-client.js"
import { parseEnvelopeChannel } from "../legacy/js/net/cloud-crypto.js"

/** One clock for the wall, the page's uptime and every timer. */
function fakeClock(start = Date.UTC(2026, 9, 20, 12, 0, 0)) {
  let at = start
  let next = 1
  const timers = new Map<number, { due: number; fn: () => void }>()
  return {
    now: () => at,
    monotonic: () => at - start + 1000,
    setTimeout: (fn: () => void, ms: number) => {
      const id = next++
      timers.set(id, { due: at + Math.max(0, ms), fn })
      return id
    },
    clearTimeout: (id: unknown) => { timers.delete(id as number) },
    pending: () => timers.size,
    async advance(ms: number) {
      const end = at + ms
      for (;;) {
        await settle()
        let soonest: [number, { due: number; fn: () => void }] | null = null
        for (const entry of timers) if (entry[1].due <= end && (!soonest || entry[1].due < soonest[1].due)) soonest = entry
        if (!soonest) break
        timers.delete(soonest[0])
        at = soonest[1].due
        soonest[1].fn()
      }
      at = end
      await settle()
    },
  }
}

async function settle() {
  for (let i = 0; i < 30; i++) await new Promise((resolve) => setImmediate(resolve))
}

let randomSeed = 0
function random(n: number) {
  const out = new Uint8Array(n)
  for (let i = 0; i < n; i++) out[i] = (randomSeed++ * 37 + 11) & 0xff
  return out
}

function producer(cloud: FakeCloud, clock = fakeClock(), extra: Record<string, unknown> = {}) {
  const p = new ViewerHealthProducer({
    apiOrigin: "https://api.example.test", fetch: cloud.fetch, now: clock.now, monotonic: clock.monotonic,
    setTimeout: clock.setTimeout, clearTimeout: clock.clearTimeout, random, ...extra,
  })
  return { p, clock }
}

function fill(p: any, n: number, stage = "viewer_connect") {
  for (let i = 0; i < n; i++) p.record({ event: "viewer_stage_started", stage, class: "transport", correlation: "vt-" + i })
}

test("the producer's bounds are the contract's producer fixture", () => {
  // Cloud contract §14.3, frontend producer fixture.
  assert.equal(VIEWER_HEALTH_LIMITS.bodyBytes, 16384)
  assert.equal(VIEWER_HEALTH_LIMITS.batchEvents, 50)
  assert.equal(VIEWER_HEALTH_LIMITS.hourlyRequests, 60)
  assert.equal(VIEWER_HEALTH_LIMITS.hourlyBytes, 262144)
  assert.equal(VIEWER_HEALTH_LIMITS.sendEveryMs, 60000)
  assert.equal(VIEWER_HEALTH_LIMITS.attempts, 4)
  assert.deepEqual([...VIEWER_HEALTH_LIMITS.backoffMs], [5000, 20000, 60000])
  assert.equal(VIEWER_HEALTH_LIMITS.bufferEvents, 200)
  assert.deepEqual([...VIEWER_STAGES], ["viewer_connect", "viewer_reconnect", "viewer_carrier_replace", "viewer_sse_resume", "viewer_sse_gap", "viewer_request_issued", "viewer_response_received", "viewer_decrypt_apply"])
  assert.equal(VIEWER_ERRORS.length, 13)
})

test("off is the default: no configuration, no observer, no request", async () => {
  const cloud = new FakeCloud()
  assert.equal(startViewerHealth(null), null)
  assert.equal(startViewerHealth({ enabled: false, apiOrigin: "https://api.example.test", fetch: cloud.fetch } as any), null)
  const span = viewerHealthBegin("viewer_connect", "transport")
  span.fail("timeout")
  viewerHealthFailed("viewer_sse_gap", "sse_gap")
  await settle()
  assert.equal(cloud.requests.length, 0)
})

test("a relay that keeps refusing is still reported over HTTPS, as connect then reconnects", async () => {
  const cloud = new FakeCloud()
  const clock = fakeClock()
  const handle = startViewerHealth({ enabled: true, apiOrigin: "https://api.example.test", fetch: cloud.fetch,
    now: clock.now, monotonic: clock.monotonic, setTimeout: clock.setTimeout, clearTimeout: clock.clearTimeout, random })!
  let connects = 0
  const session = { client: null, async connect() { connects += 1; throw Object.assign(new Error("down"), { code: "offline" }) } }
  const line = keepConnected(session, {
    now: clock.now, setTimeout: clock.setTimeout, clearTimeout: clock.clearTimeout, jitter: () => 0.5,
    document: { hidden: false, visibilityState: "visible", addEventListener() {}, removeEventListener() {} },
    window: null, navigator: { onLine: true }, maximumConnectFailures: 100, onState() {},
  })
  try {
    await clock.advance(70_000)
    assert.ok(connects >= 3, `connects ${connects}`)
    assert.ok(cloud.accepted.length >= 1, "the HTTPS upload went out while the relay was down: " + JSON.stringify(handle.status()) + cloud.requests.map((r: any) => r.body.slice(0, 300)).join(" "))
    const records = cloud.records()
    assert.equal(records[0].stage, "viewer_connect")
    assert.equal(records[0].event, "viewer_stage_started")
    assert.equal(records[1].stage, "viewer_connect")
    assert.equal(records[1].event, "viewer_stage_failed")
    assert.equal(records[1].error, "network_unreachable")
    assert.equal(records[1].correlation, records[0].correlation)
    assert.ok(records.some((r: any) => r.stage === "viewer_reconnect" && r.event === "viewer_stage_failed"))
    for (const request of cloud.requests) {
      assert.equal(request.url, "https://api.example.test/v1/health/viewer-ingest")
      assert.equal(request.init.credentials, "include")
      assert.equal(new Headers(request.init.headers).get("x-clawdline-health"), "viewer/1")
    }
  } finally {
    line.stop()
    handle.stop()
  }
})

test("a retry resends the same bytes under the same batch id while new records wait", async () => {
  const cloud = new FakeCloud()
  const { p, clock } = producer(cloud)
  cloud.answer = (index: number) => index === 0 ? refused(503, "health_store_unavailable", "") : null
  fill(p, 3)
  await clock.advance(5_000)
  assert.equal(cloud.requests.length, 1)
  fill(p, 2, "viewer_reconnect")
  await clock.advance(5_000)
  assert.equal(cloud.requests.length, 2)
  assert.equal(cloud.requests[1].body, cloud.requests[0].body, "the retry's body is byte-identical")
  assert.equal(cloud.accepted[0].batch_id, JSON.parse(cloud.requests[0].body).batch_id)
  assert.equal((cloud.accepted[0].events as unknown[]).length, 3)
  await clock.advance(60_000)
  assert.equal(cloud.accepted.length, 2)
  assert.notEqual(cloud.accepted[1].batch_id, cloud.accepted[0].batch_id)
  // A replay of an accepted body is a 200; a changed body under its id is a 409.
  assert.equal(cloud.ingest(cloud.requests[1].body, cloud.requests[1].init).status, 200)
  const changed = JSON.parse(cloud.requests[1].body)
  changed.events[0].outcome = "pending"
  assert.equal(cloud.ingest(JSON.stringify(changed), cloud.requests[1].init).status, 409)
  p.stop()
})

test("retries are finite, back off 5 s, 20 s, 60 s, honor Retry-After, then drop and count", async () => {
  const cloud = new FakeCloud()
  const { p, clock } = producer(cloud)
  const at: number[] = []
  cloud.answer = (index: number) => {
    at.push(clock.now())
    return index === 1 ? refused(429, "health_budget_exhausted", "", { "retry-after": "30" }) : refused(503, "health_deadline_exceeded", "")
  }
  fill(p, 1)
  await clock.advance(10 * 60_000)
  assert.equal(cloud.requests.length, 4)
  assert.deepEqual(at.slice(1).map((t, i) => t - at[i]), [5_000, 30_000, 60_000])
  assert.equal(new Set(cloud.requests.map((r: any) => r.body)).size, 1)
  const s = p.status()
  assert.equal(s.counts.droppedBatches, 1)
  assert.equal(s.counts.droppedEvents, 1)
  assert.equal(s.pendingBatch, null)
  p.stop()
})

test("a failing Cloud is asked at most sixty times in a clock hour", async () => {
  const cloud = new FakeCloud()
  const { p, clock } = producer(cloud)
  cloud.answer = () => refused(503, "health_store_unavailable", "")
  for (let minute = 0; minute < 59; minute++) {
    fill(p, 5)
    await clock.advance(60_000)
  }
  const hour = Math.floor(clock.now() / 3_600_000)
  const inHour = cloud.requests.length
  assert.ok(inHour <= VIEWER_HEALTH_LIMITS.hourlyRequests, `requests ${inHour}`)
  assert.ok(p.status().buffered <= VIEWER_HEALTH_LIMITS.bufferEvents)
  assert.equal(Math.floor(clock.now() / 3_600_000), hour)
  p.stop()
})

test("a full buffer drops its oldest records and counts them; a body never passes 16 KiB", async () => {
  const cloud = new FakeCloud()
  const { p, clock } = producer(cloud)
  fill(p, 260)
  assert.equal(p.status().buffered, 200)
  assert.equal(p.status().counts.droppedOldest, 60)
  await clock.advance(10 * 60_000)
  for (const request of cloud.requests) {
    assert.ok(new TextEncoder().encode(request.body).length <= 16384)
    parseViewerBody(request.body)
  }
  assert.equal(cloud.records().length, 200)
  assert.equal(cloud.records()[0].producer_seq, 61)
  p.stop()
})

test("the session revoked during an upload: the request is abandoned, everything held is cleared, nothing more is sent", async () => {
  const cloud = new FakeCloud()
  const { p, clock } = producer(cloud)
  let release: (value: any) => void = () => {}
  cloud.answer = () => new Promise((resolve) => { release = resolve })
  fill(p, 3)
  await clock.advance(5_000)
  assert.equal(cloud.requests.length, 1)
  const signal = cloud.requests[0].init.signal as AbortSignal
  fill(p, 2)
  p.stop("stopped")
  assert.equal(signal.aborted, true)
  release({ status: 201, body: "{}", headers: {} })
  await settle()
  fill(p, 2)
  await clock.advance(10 * 60_000)
  assert.equal(cloud.requests.length, 1)
  const s = p.status()
  assert.equal(s.state, "stopped")
  assert.equal(s.buffered, 0)
  assert.equal(s.pendingBatch, null)
  assert.equal(s.counts.clearedOnStop, 5)
  assert.equal(s.counts.sentBatches, 0)
})

test("401 stops until a new session, 403 and a disabled ingest until reload", async () => {
  for (const [status, code, state] of [[401, "no_session", "session_gone"], [403, "bad_origin", "stopped_until_reload"], [503, "health_ingest_disabled", "stopped_until_reload"]] as const) {
    const cloud = new FakeCloud()
    const { p, clock } = producer(cloud)
    cloud.answer = () => refused(status, code, "")
    fill(p, 2)
    await clock.advance(5_000)
    fill(p, 2)
    await clock.advance(10 * 60_000)
    assert.equal(cloud.requests.length, 1, code)
    assert.equal(p.status().state, state)
    assert.equal(p.status().buffered, 0)
    assert.deepEqual(p.status().lastRefusal, { status, code, field: null })
  }
})

test("404 drops the target machine and sends the same records unattributed; 413 splits; 400 drops and keeps only code and field", async () => {
  const cloud = new FakeCloud()
  const { p, clock } = producer(cloud, fakeClock(), { targetMachineId: "mac_other" })
  cloud.answer = (index: number, body: string) => {
    if (index === 0) return refused(404, "machine_not_found", "")
    if (index === 1) { assert.equal(JSON.parse(body).target_machine_id, undefined); return refused(413, "payload_too_large", "") }
    return null
  }
  fill(p, 4)
  await clock.advance(5 * 60_000)
  assert.equal(cloud.records().length, 4)
  assert.deepEqual(cloud.accepted.map((b: any) => b.events.length), [2, 2])
  p.stop()

  const bad = new FakeCloud()
  const second = producer(bad)
  bad.answer = () => refused(400, "unknown_field", "events[0].stack")
  fill(second.p, 1)
  await second.clock.advance(5_000)
  assert.equal(second.p.status().counts.droppedBatches, 1)
  assert.deepEqual(second.p.status().lastRefusal, { status: 400, code: "unknown_field", field: "events[0].stack" })
  second.p.stop()
})

test("a record the Cloud would refuse is dropped here, never sent", async () => {
  const cloud = new FakeCloud()
  const { p, clock } = producer(cloud)
  p.record({ event: "viewer_stage_failed", stage: "viewer_connect" })
  p.record({ event: "viewer_stage_completed", stage: "viewer_connect", error: "timeout" })
  p.record({ event: "viewer_stage_started", stage: "viewer_somewhere" as any })
  p.record({ event: "viewer_stage_started", stage: "viewer_connect", correlation: "https://relay/x" })
  p.record({ event: "viewer_stage_completed", stage: "viewer_connect", elapsedMs: 9e9, outcome: "ok" })
  await clock.advance(5_000)
  assert.equal(p.status().counts.refusedLocally, 4)
  assert.equal(cloud.records().length, 1)
  assert.equal(cloud.records()[0].elapsed_ms, 3_600_000)
  p.stop()
})

/* ---- the copied CloudClient's read path ------------------------------------------------- */

const MACHINE = "mac-a"
const REPLY = "t/" + MACHINE + "/__clawdline_machine__"

class FakeSocket {
  static open: FakeSocket[] = []
  readyState = 1
  onmessage: ((event: { data: string }) => void) | null = null
  onclose: ((event: { code: number }) => void) | null = null
  onerror: (() => void) | null = null
  sent: any[] = []
  constructor() { FakeSocket.open.push(this) }
  send(data: string) {
    const frame = JSON.parse(data)
    this.sent.push(frame)
    if (frame.type === "subscribe") this.say({ type: "subscriptions", channels: frame.channels })
    if (frame.type === "publish") {
      this.say({ type: "ack", ch: frame.envelope.ch, seq: 1, fanout: 1, status: "delivered" })
      this.say({ type: "envelope", envelope: { ch: REPLY, seq: this.sent.length, ts: 1, sender: "mac-sender",
        payload: { read: "read:" + frame.envelope.body.request, body: { answered: frame.envelope.body.type } } } })
    }
  }
  say(frame: unknown) { setImmediate(() => this.onmessage?.({ data: JSON.stringify(frame) })) }
  close() { this.readyState = 3 }
}

async function cloudClient(options: { applyFails?: boolean } = {}) {
  let sequence = 1
  const client = new CloudClient({
    relayURL: "https://relay.example.test", deviceToken: "token", devicePrivateKey: { extractable: false },
    deviceID: "viewer-1", account: "account-1", allowWrites: true, nextSequence: async () => sequence++,
    WebSocket: FakeSocket, readTimeoutMs: 300,
  }) as any
  client._publishCommand = async function (this: any, machine: string, type: string, body: Record<string, unknown>) {
    this._send({ type: "publish", envelope: { ch: "ctl/" + machine, body: { type, ...body } } })
  }
  // The real `_receiveEnvelope` runs (and its failure path); only opening is stood in for.
  client._openEnvelopeFrame = async function (this: any, envelope: any, realign: boolean, probe: any) {
    probe.stage = "apply"
    if (options.applyFails) throw Object.assign(new Error("apply"), { code: "bad_payload" })
    this._applySnapshot(parseEnvelopeChannel(envelope.ch), envelope.payload, envelope, realign)
  }
  await client.start({ quiet: true })
  FakeSocket.open.at(-1)!.say({ type: "ready", v: 1, role: "viewer", account: "account-1", device: "viewer-1" })
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

test("a read records issued, response and decrypt-apply under one correlation", async () => {
  const { records, uninstall } = collect()
  const client = await cloudClient()
  try {
    const answer = await client._machineRequest(MACHINE, "projects", {}, "read")
    assert.deepEqual(answer, { answered: "projects" })
    const trace = records.filter((r) => r.class === "read")
    assert.deepEqual(trace.map((r) => [r.stage, r.event]), [
      ["viewer_request_issued", "viewer_stage_started"],
      ["viewer_response_received", "viewer_stage_completed"],
      ["viewer_decrypt_apply", "viewer_stage_completed"],
    ])
    assert.equal(new Set(trace.map((r) => r.correlation)).size, 1)
  } finally {
    client.stop()
    uninstall()
  }
})

test("an answer that arrives and cannot be applied is a failed decrypt-apply, then a timed-out response", async () => {
  const { records, uninstall } = collect()
  const client = await cloudClient({ applyFails: true })
  try {
    await assert.rejects(client._machineRequest(MACHINE, "projects", {}, "read"))
    const applied = records.find((r) => r.stage === "viewer_decrypt_apply")
    assert.equal(applied.event, "viewer_stage_failed")
    assert.equal(applied.error, "apply_failed")
    const response = records.find((r) => r.stage === "viewer_response_received")
    assert.equal(response.event, "viewer_stage_failed")
    assert.equal(response.error, "timeout")
    assert.equal(response.outcome, "timeout")
    const issued = records.find((r) => r.stage === "viewer_request_issued")
    assert.equal(response.correlation, issued.correlation)
  } finally {
    client.stop()
    uninstall()
  }
})

test("an observer that throws changes nothing for the page", async () => {
  const uninstall = installViewerHealthObserver({
    record() { throw new Error("observer broke") }, correlation() { throw new Error("no ids") }, monotonic() { throw new Error("no clock") },
  })
  const client = await cloudClient()
  try {
    assert.deepEqual(await client._machineRequest(MACHINE, "projects", {}, "read"), { answered: "projects" })
    const span = viewerHealthBegin("viewer_connect", "transport")
    span.complete()
    span.fail("timeout")
    viewerHealthFailed("viewer_sse_gap", "sse_gap")
  } finally {
    client.stop()
    uninstall()
  }
  // And a producer whose fetch throws synchronously stays a producer.
  const { p, clock } = producer({ fetch: () => { throw new Error("no fetch") } } as any)
  fill(p, 1)
  await clock.advance(5_000)
  assert.equal(p.status().state, "running")
  p.stop()
})

test("a replacement client reports whether the relay's delivery resumed", async () => {
  const { records, uninstall } = collect()
  const first = await cloudClient()
  try {
    first.continuityUnproven = true
    const second = new CloudClient({
      relayURL: "https://relay.example.test", deviceToken: "token", devicePrivateKey: { extractable: false },
      deviceID: "viewer-1", account: "account-1", allowWrites: true, nextSequence: async () => 1,
      WebSocket: FakeSocket, readTimeoutMs: 300, resumeFrom: first,
    }) as any
    await second.start({ quiet: true })
    FakeSocket.open.at(-1)!.say({ type: "ready", v: 1, role: "viewer", account: "account-1", device: "viewer-1" })
    await second.whenReady()
    const resume = records.filter((r) => r.stage === "viewer_sse_resume")
    assert.deepEqual(resume.map((r) => [r.event, r.error]), [["viewer_stage_started", undefined], ["viewer_stage_failed", "sse_resume_refused"]])
    second.stop()
  } finally {
    first.stop()
    uninstall()
  }
  stopViewerHealth()
})
