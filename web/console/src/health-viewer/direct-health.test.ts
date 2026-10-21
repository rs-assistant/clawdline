import assert from "node:assert/strict"
import test from "node:test"
import type { DirectChannelLike, DirectPeerLike } from "../cloud/terminal-direct.js"
// @ts-expect-error -- a `.ts` path, so Node's strip-types runner can load it.
import { DirectCarrier, directCarrierHealthError } from "../cloud/direct-carrier.ts"
import { bytesBase64 } from "../legacy/js/net/cloud-crypto.js"
import { installViewerHealthObserver } from "../legacy/js/net/viewer-health-hook.js"

// The direct carrier's own connection-health records (docs/admin-connection-health-contract.md
// §11.2): a negotiation is a carrier replacement with its own random correlation, and an open
// carrier that stops carrying is a failure even though the relay socket never noticed.

class FakeChannel implements DirectChannelLike {
  readyState = "connecting"
  onopen: ((event?: unknown) => void) | null = null
  onclose: ((event?: unknown) => void) | null = null
  onerror: ((event?: unknown) => void) | null = null
  onmessage: ((event: { data: unknown }) => void) | null = null
  send(): void {}
  close(): void {
    if (this.readyState === "closed") return
    this.readyState = "closed"
    this.onclose?.()
  }
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

type Observation = { event: string; stage: string; class?: string; correlation?: string; error?: string; outcome?: string }

function observe(): { seen: Observation[]; remove: () => void } {
  const seen: Observation[] = []
  let n = 0
  const remove = installViewerHealthObserver({
    record: (o: Observation) => { seen.push(o) },
    correlation: () => `vt-${(++n).toString(16).padStart(12, "0")}`,
    monotonic: () => 0,
  })
  return { seen, remove }
}

let n = 0
function carrierFor(answer: (offer: { connection: string; key: Uint8Array }) => Promise<string>) {
  const machine = `machine_health_${++n}`
  const offers: { connection: string; request: string; sdp: string }[] = []
  const client = {
    deviceID: "viewer", ready: true, retired: false,
    publishCarrierOffer: async (_m: string, offer: { connection: string; key: Uint8Array; request: string; sdp: string }) => {
      offers.push(offer)
      return { sdpSealed: await answer(offer), directReceipts: false }
    },
    receiveCarrierEnvelope: () => {},
    carrierSupported: () => true,
  }
  return { offers, carrier: DirectCarrier.for(client, machine, () => new FakePeer()) }
}

const fail = (code: string) => async () => { throw Object.assign(new Error(code), { code }) }
const short = (o: Observation) => [o.event, o.stage, o.class, o.error ?? o.outcome]

test("an opened carrier is one carrier replacement, started and completed under one random correlation", async () => {
  const { seen, remove } = observe()
  try {
    const { carrier, offers } = carrierFor((offer) => sealAnswer(offer, "v=0 answer"))
    assert.equal((await carrier.tryNow()).kind, "open")
    assert.deepEqual(seen.map(short), [
      ["viewer_stage_started", "viewer_carrier_replace", "transport", undefined],
      ["viewer_stage_completed", "viewer_carrier_replace", "transport", "ok"],
    ])
    assert.equal(seen[0]!.correlation, seen[1]!.correlation)
    // The correlation is the observer's random id, never the connection, request or SDP.
    const leaked = JSON.stringify(seen)
    for (const offer of offers) {
      assert.ok(!leaked.includes(offer.connection) && !leaked.includes(offer.request) && !leaked.includes("v=0"))
    }
  } finally { remove() }
})

test("fault injection: each way a negotiation fails is a failed carrier replacement with a fixed word", async () => {
  const { seen, remove } = observe()
  try {
    await carrierFor(fail("request_timeout")).carrier.tryNow()
    await carrierFor(async () => bytesBase64(crypto.getRandomValues(new Uint8Array(48)))).carrier.tryNow()
    await carrierFor(fail("carrier_bad_answer")).carrier.tryNow()
    await carrierFor(fail("terminal_busy")).carrier.tryNow()
    assert.deepEqual(seen.filter((o) => o.event !== "viewer_stage_started").map(short), [
      ["viewer_stage_failed", "viewer_carrier_replace", "transport", "timeout"],
      // The machine's sealed answer would not open here.
      ["viewer_stage_failed", "viewer_carrier_replace", "transport", "decrypt_failed"],
      ["viewer_stage_failed", "viewer_carrier_replace", "transport", "decrypt_failed"],
      // The machine saying "not now" is not this network failing.
      ["viewer_stage_completed", "viewer_carrier_replace", "transport", "aborted"],
    ])
  } finally { remove() }
})

test("fault injection: an open carrier that stops carrying is a failure while the relay socket is untouched", async () => {
  const { seen, remove } = observe()
  try {
    const { carrier } = carrierFor((offer) => sealAnswer(offer, "v=0 answer"))
    assert.equal((await carrier.tryNow()).kind, "open")
    seen.length = 0
    carrier.failed(carrier.channelGeneration, "carrier_unusable")
    assert.equal(carrier.open, false)
    assert.deepEqual(seen.map(short), [["viewer_stage_failed", "viewer_carrier_replace", "transport", "decrypt_failed"]])
  } finally { remove() }
})

test("a carrier this page lets go on purpose records nothing", async () => {
  const { seen, remove } = observe()
  try {
    const { carrier } = carrierFor((offer) => sealAnswer(offer, "v=0 answer"))
    await carrier.tryNow()
    seen.length = 0
    carrier.close("carrier_client_retired")
    assert.equal(carrier.open, false)
    assert.deepEqual(seen, [])
  } finally { remove() }
})

test("with no observer installed a carrier records nothing and still opens", async () => {
  const { carrier } = carrierFor((offer) => sealAnswer(offer, "v=0 answer"))
  assert.equal((await carrier.tryNow()).kind, "open")
})

test("the link's own codes map to the Cloud's words; anything else is the relay table's, never a guess", () => {
  assert.equal(directCarrierHealthError("terminal_direct_ice_failed"), "network_unreachable")
  assert.equal(directCarrierHealthError("terminal_direct_chunk_timeout"), "timeout")
  assert.equal(directCarrierHealthError("terminal_direct_closed"), "carrier_closed")
  assert.equal(directCarrierHealthError("carrier_unusable"), "decrypt_failed")
  assert.equal(directCarrierHealthError("machine_offline"), "unclassified")
  assert.equal(directCarrierHealthError("terminal_direct_violation"), "unclassified")
})
