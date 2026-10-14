import assert from "node:assert/strict"
import { test } from "node:test"
import { PairCamera } from "./pair-camera.ts"

function harness(overrides = {}) {
  const stopped = []
  const issues = []
  const states = []
  const found = []
  const listeners = new Map()
  let nextFrame = null
  let tick = 0
  const video = {
    readyState: 2, videoWidth: 640, videoHeight: 480, srcObject: null,
    play: async () => {}, pause: () => {},
  }
  const canvas = {
    width: 0, height: 0,
    getContext: () => ({ drawImage() {}, getImageData: () => ({ data: new Uint8ClampedArray(640 * 480 * 4) }) }),
  }
  const visibility = {
    visibilityState: "visible",
    addEventListener: (name, callback) => listeners.set(name, callback),
    removeEventListener: (name) => listeners.delete(name),
  }
  const stream = { getTracks: () => [{ stop: () => stopped.push("track") }] }
  const options = {
    video, canvas, visibility,
    media: { getUserMedia: async () => stream },
    loadDecoder: async () => () => null,
    check: (value) => value,
    found: (value) => found.push(value),
    state: (value) => states.push(value),
    issue: (value) => issues.push(value),
    requestFrame: (callback) => { nextFrame = callback; return 1 },
    cancelFrame: () => { nextFrame = null },
    now: () => tick,
    ...overrides,
  }
  const camera = new PairCamera(options)
  return { camera, video, visibility, listeners, stopped, issues, states, found, frame: () => { tick += 250; nextFrame?.(tick) } }
}

test("the scanner stops every track when the Home Screen app is backgrounded", async () => {
  const h = harness()
  await h.camera.start()
  assert.equal(h.video.srcObject !== null, true)
  h.visibility.visibilityState = "hidden"
  h.listeners.get("visibilitychange")()
  assert.deepEqual(h.stopped, ["track"])
  assert.equal(h.video.srcObject, null)
  assert.deepEqual(h.issues, ["background"])
  h.camera.dispose()
})

test("unmount disposes the camera and a late permission grant cannot restart it", async () => {
  let permit
  const stream = { getTracks: () => { throw new Error("overridden below") } }
  const stopped = []
  stream.getTracks = () => [{ stop: () => stopped.push("track") }]
  const h = harness({ media: { getUserMedia: () => new Promise((resolve) => { permit = () => resolve(stream) }) } })
  const starting = h.camera.start()
  h.camera.dispose()
  permit()
  await starting
  assert.deepEqual(stopped, ["track"])
  assert.equal(h.video.srcObject, null)
  assert.equal(h.listeners.size, 0)
})

test("a refused camera names the refusal and leaves no live stream", async () => {
  const h = harness({ media: { getUserMedia: async () => { throw Object.assign(new Error("denied"), { name: "NotAllowedError" }) } } })
  await h.camera.start()
  assert.deepEqual(h.issues, ["permission_denied"])
  assert.equal(h.video.srcObject, null)
  h.camera.dispose()
})

test("an invalid QR stays in the scanner; a valid one stops before handoff", async () => {
  let value = "wrong"
  const h = harness({
    loadDecoder: async () => () => ({ data: value }),
    check: (raw) => {
      if (raw === "wrong") throw Object.assign(new Error("other site"), { code: "wrong_pairing_origin" })
      return "accepted-fragment"
    },
  })
  await h.camera.start()
  h.frame()
  assert.deepEqual(h.issues, ["wrong_pairing_origin"])
  assert.deepEqual(h.stopped, [])
  value = "right"
  h.frame()
  assert.deepEqual(h.found, ["accepted-fragment"])
  assert.deepEqual(h.stopped, ["track"])
  h.camera.dispose()
})
