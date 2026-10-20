// Previous and next in the picture preview: `node --test web/console/src/session/lightbox-steps.test.ts`.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see `session/order.test.ts`.
import { saveImageFromClick, stepFrom, swipeStep } from "./lightbox-steps.ts"

test("a step goes to the neighbour and stops at either end", () => {
  const list = ["a", "b", "c"]
  assert.equal(stepFrom(list, "b", 1), "c")
  assert.equal(stepFrom(list, "b", -1), "a")
  assert.equal(stepFrom(list, "a", -1), null)
  assert.equal(stepFrom(list, "c", 1), null)
})

// A tile that expired while the preview was open is no longer in the list of
// live tiles; stepping from it must not jump to the first picture.
test("a picture no longer in the list has no neighbours", () => {
  assert.equal(stepFrom(["a", "c"], "b", 1), null)
})

test("a finger moving left is the next picture, right the previous", () => {
  assert.equal(swipeStep(-80, 5), 1)
  assert.equal(swipeStep(80, -5), -1)
})

// Reading down a long screenshot is a mostly vertical drag; it must never
// change pictures, and neither must a short horizontal wobble.
test("a scroll or a wobble is not a swipe", () => {
  assert.equal(swipeStep(-60, 200), 0)
  assert.equal(swipeStep(-30, 0), 0)
})

test("a phone save press gives the native sheet the original image file immediately", () => {
  const file = new File([new Uint8Array([137, 80, 78, 71])], "clawdline-image.png", { type: "image/png" })
  const calls: unknown[] = []
  const platform = {
    userAgent: "iPhone", maxTouchPoints: 5,
    canShare(data: ShareData) { calls.push(["canShare", data.files]); return true },
    share(data: ShareData) { calls.push(["share", data.files]); return Promise.resolve() },
  }
  const fallback = () => calls.push(["download"])
  assert.equal(saveImageFromClick(file, platform, fallback), "native")
  assert.deepEqual(calls, [["canShare", [file]], ["share", [file]]])
  assert.equal(file.type, "image/png")
  assert.equal(file.size, 4)
})

test("a browser without file sharing keeps its download action", () => {
  const file = new File(["png"], "clawdline-image.png", { type: "image/png" })
  let downloads = 0
  const platform = { userAgent: "Desktop", maxTouchPoints: 0, canShare: () => false, share: () => Promise.resolve() }
  assert.equal(saveImageFromClick(file, platform, () => { downloads++ }), "download")
  assert.equal(downloads, 1)
})


test("a desktop with file sharing saves directly instead of opening the share sheet", () => {
  const file = new File(["png"], "picture.png", { type: "image/png" })
  let downloads = 0
  const platform = {
    userAgent: "Macintosh", maxTouchPoints: 0,
    canShare: () => true,
    share: () => { assert.fail("desktop download must not share"); return Promise.resolve() },
  }
  assert.equal(saveImageFromClick(file, platform, () => { downloads++ }), "download")
  assert.equal(downloads, 1)
})

test("a desktop save picker receives the filename and writes the original bytes", async () => {
  const file = new File(["original bytes"], "picture.png", { type: "image/png" })
  const calls: unknown[] = []
  const platform = {
    userAgent: "Windows", maxTouchPoints: 10,
    share: () => { assert.fail("desktop download must not share"); return Promise.resolve() },
    canShare: () => true,
    showSaveFilePicker: async (options: { suggestedName: string }) => {
      calls.push(options)
      return { createWritable: async () => ({
        write: async (bytes: Blob) => { calls.push(bytes) },
        close: async () => { calls.push("closed") },
      }) }
    },
  }
  assert.equal(saveImageFromClick(file, platform, () => assert.fail("unexpected download")), "picker")
  await new Promise(resolve => setImmediate(resolve))
  assert.deepEqual(calls, [{ suggestedName: "picture.png" }, file, "closed"])
})

test("cancelling the save picker does not download; an unavailable picker falls back", async () => {
  const file = new File(["png"], "picture.png")
  for (const name of ["AbortError", "SecurityError"]) {
    let downloads = 0
    const platform = {
      userAgent: "Desktop", maxTouchPoints: 0,
      share: undefined, canShare: undefined,
      showSaveFilePicker: () => Promise.reject(new DOMException("unavailable", name)),
    }
    saveImageFromClick(file, platform, () => { downloads++ })
    await new Promise(resolve => setImmediate(resolve))
    assert.equal(downloads, name === "AbortError" ? 0 : 1)
  }
})
