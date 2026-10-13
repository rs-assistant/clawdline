import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error Node's strip-types runner imports this source without a build.
import { askedSession } from "./asked-session.ts"

test("a confirmed new Session opens while the last complete inventory is still old", () => {
  const lastInventory: { id: string }[] = [{ id: "older" }]
  const newSession = { id: "new" }
  assert.deepEqual(askedSession(["new"], [...lastInventory, newSession], true),
    { kind: "opened", id: "new" })
  assert.deepEqual(askedSession(["new"], lastInventory, false), { kind: "waiting" })
  assert.deepEqual(askedSession(["gone"], lastInventory, true), { kind: "gone" })
})
