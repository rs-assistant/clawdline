import assert from "node:assert/strict"
import test from "node:test"
import type { DirectChannelLike, DirectPeerLike } from "./terminal-direct.js"
// @ts-expect-error -- esbuild bundles the TypeScript source for Node's test runner.
import { DirectCarrier } from "./direct-carrier.ts"
// @ts-expect-error -- esbuild bundles the TypeScript source for Node's test runner.
import { directHistory, failingSince, recordDirectAttempt, HISTORY_HOURS, HISTORY_MACHINES } from "./direct-history.ts"
import { bytesBase64 } from "../legacy/js/net/cloud-crypto.js"

// The machine sheet's "try direct now": what each way an attempt can end is
// called, that the person's attempt is not held back by the background retry
// bound, and what this browser's record of attempts keeps.

class FakeChannel implements DirectChannelLike {
  readyState = "connecting"
  onopen: ((event?: unknown) => void) | null = null
  onclose: ((event?: unknown) => void) | null = null
  onerror: ((event?: unknown) => void) | null = null
  onmessage: ((event: { data: unknown }) => void) | null = null
  send(): void {}
  close(): void { this.readyState = "closed" }
}
class FakePeer implements DirectPeerLike {
  localDescription: { sdp: string } | null = null
  iceGatheringState = "complete"
  channel = new FakeChannel()
  onicegatheringstatechange: ((event?: unknown) => void) | null = null
  onconnectionstatechange: ((event?: unknown) => void) | null = null
  createDataChannel(): DirectChannelLike { return this.channel }
  async createOffer(): Promise<{ type: string; sdp?: string }> { return { type: "offer", sdp: "v=0 offer" } }
  async setLocalDescription(d: { type: string; sdp?: string }): Promise<void> { this.localDescription = { sdp: d.sdp ?? "" } }
  async setRemoteDescription(): Promise<void> { queueMicrotask(() => { this.channel.readyState = "open"; this.channel.onopen?.() }) }
  close(): void { this.channel.close() }
}

/** The machine's half of a carrier offer: the answer SDP sealed under the key the page minted. */
async function sealAnswer(offer: { connection: string; key: Uint8Array }, sdp: string): Promise<string> {
  const key = await crypto.subtle.importKey("raw", offer.key.slice().buffer as ArrayBuffer, "AES-GCM", false, ["encrypt"])
  const iv = crypto.getRandomValues(new Uint8Array(12))
  const sealed = new Uint8Array(await crypto.subtle.encrypt({ name: "AES-GCM", iv,
    additionalData: new TextEncoder().encode(`clawdline-direct-answer-v1/${offer.connection}`) }, key, new TextEncoder().encode(sdp)))
  const out = new Uint8Array(12 + sealed.length)
  out.set(iv, 0)
  out.set(sealed, 12)
  return bytesBase64(out)
}

let n = 0
function carrierFor(answer: (offer: { connection: string; key: Uint8Array }) => Promise<string>, supported = true) {
  const machine = `machine_try_${++n}`
  const offers: unknown[] = []
  const client = {
    deviceID: "viewer", ready: true, retired: false,
    publishCarrierOffer: async (_m: string, offer: { connection: string; key: Uint8Array }) => {
      offers.push(offer)
      return { sdpSealed: await answer(offer), directReceipts: false }
    },
    receiveCarrierEnvelope: () => {},
    carrierSupported: () => supported,
  }
  return { machine, offers, carrier: DirectCarrier.for(client, machine, () => new FakePeer()) }
}

const fail = (code: string) => async () => { throw Object.assign(new Error(code), { code }) }
const total = (machine: string) => directHistory(machine).reduce((sum: { ok: number; failed: number }, h: { ok: number; failed: number }) =>
  ({ ok: sum.ok + h.ok, failed: sum.failed + h.failed }), { ok: 0, failed: 0 })

test("an attempt the machine answers and the networks complete is open, and is counted as opened", async () => {
  const { machine, carrier } = carrierFor((offer) => sealAnswer(offer, "v=0 answer"))
  const outcome = await carrier.tryNow()
  assert.equal(outcome.kind, "open")
  assert.equal(carrier.open, true)
  assert.deepEqual(total(machine), { ok: 1, failed: 0 })
})

test("an offer nobody answers is no_answer; an answer that cannot become a channel is not_connected", async () => {
  const silent = carrierFor(fail("request_timeout"))
  assert.deepEqual(await silent.carrier.tryNow(), { kind: "no_answer", code: "request_timeout" })
  assert.deepEqual(total(silent.machine), { ok: 0, failed: 1 })

  // The machine answered, so both sides exchanged addresses; what came back did not open.
  const answered = carrierFor(async () => bytesBase64(new Uint8Array(40)))
  const outcome = await answered.carrier.tryNow()
  assert.equal(outcome.kind, "not_connected")
  assert.deepEqual(total(answered.machine), { ok: 0, failed: 1 })
})

test("the person's attempt is not held back by the background retry bound", async () => {
  let refuse = true
  const { carrier, offers } = carrierFor(async (offer) => {
    if (refuse) throw Object.assign(new Error("x"), { code: "request_timeout" })
    return sealAnswer(offer, "v=0 answer")
  })
  assert.equal(await carrier.ensure(), false)
  refuse = false
  // The background path waits out its bound and does not ask again...
  assert.equal(await carrier.ensure(), false)
  assert.equal(offers.length, 1, "the background path retried inside its bound")
  // ...and the person asking is the reason to.
  assert.equal((await carrier.tryNow()).kind, "open")
  assert.equal(offers.length, 2)
})

test("a machine that says not now is busy and is not counted against this network", async () => {
  const { machine, carrier } = carrierFor(fail("terminal_busy"))
  assert.deepEqual(await carrier.tryNow(), { kind: "busy" })
  assert.deepEqual(total(machine), { ok: 0, failed: 0 })
})

test("a machine that does not open carriers is unsupported, and nothing is offered to it", async () => {
  const { carrier, offers } = carrierFor((offer) => sealAnswer(offer, "v=0 answer"), false)
  assert.deepEqual(await carrier.tryNow(), { kind: "unsupported" })
  assert.equal(offers.length, 0)
})

test("two attempts at once make one offer", async () => {
  const { carrier, offers } = carrierFor((offer) => sealAnswer(offer, "v=0 answer"))
  const [a, b] = await Promise.all([carrier.tryNow(), carrier.ensure()])
  assert.equal(a.kind, "open")
  assert.equal(b, true)
  assert.equal(offers.length, 1)
})

function mapStore() {
  const held = new Map<string, string>()
  return { getItem: (k: string) => held.get(k) ?? null, setItem: (k: string, v: string) => { held.set(k, v) } }
}

test("the record keeps 24 hours per machine and says since when the direct path has failed", () => {
  const store = mapStore()
  const hour = 3_600_000
  const now = 1_000 * hour
  recordDirectAttempt("m", true, now - 30 * hour, store)
  recordDirectAttempt("m", true, now - 10 * hour, store)
  recordDirectAttempt("m", false, now - 8 * hour, store)
  recordDirectAttempt("m", false, now - 1 * hour, store)
  recordDirectAttempt("m", false, now, store)
  const hours = directHistory("m", now, store)
  assert.equal(hours.length, HISTORY_HOURS)
  assert.equal(hours.reduce((s: number, h: { ok: number }) => s + h.ok, 0), 1, "an hour older than the window was kept")
  // Hours with no attempt between failures do not end the run.
  assert.equal(failingSince(hours), now - 8 * hour)
  recordDirectAttempt("m", true, now, store)
  assert.equal(failingSince(directHistory("m", now, store)), null, "an hour that opened still reads as failing")
})

test("past the machine bound, the machine used longest ago is dropped", () => {
  const store = mapStore()
  const hour = 3_600_000
  for (let i = 0; i <= HISTORY_MACHINES; i++) recordDirectAttempt(`m${i}`, true, (500 + i) * hour, store)
  const now = (500 + HISTORY_MACHINES) * hour
  const kept = Object.keys(JSON.parse(store.getItem("clawdline.direct-history.v1")!))
  assert.equal(kept.length, HISTORY_MACHINES)
  assert.equal(kept.includes("m0"), false, "the machine used longest ago was kept")
  assert.equal(directHistory(`m${HISTORY_MACHINES}`, now, store).some((h: { ok: number }) => h.ok === 1), true)
})
