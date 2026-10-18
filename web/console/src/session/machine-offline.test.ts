import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { offlinePlacement } from "./machine-offline.ts"

test("a machine that is reporting changes nothing on its page", () => {
  assert.equal(offlinePlacement(null, 0), null)
  assert.equal(offlinePlacement(null, 3), null)
})

test("an offline machine with nothing drawn says so instead of loading", () => {
  // The page used to say "connected to the machine, fetching the list" over a closed laptop.
  assert.equal(offlinePlacement({ machine: "Machine A", seenAt: 1 }, 0), "empty")
  assert.equal(offlinePlacement({ machine: "Machine B", seenAt: null }, 0), "empty")
})

test("an offline machine's last reported Sessions stay, under a line that says they are old", () => {
  assert.equal(offlinePlacement({ machine: "Machine A", seenAt: 1 }, 2), "line")
})
