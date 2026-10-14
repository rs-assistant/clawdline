import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import test from "node:test"
// @ts-expect-error -- node's type-stripping runner resolves the source .ts file.
import { screenAfterConnected } from "./state.ts"

test("a connected renewal leaves an open machine-management confirmation in place", () => {
  let screen = { at: "console" } as { at: string }
  screen = screenAfterConnected(screen, true)
  assert.equal(screen.at, "console")
  // The Devices page opens the account-only machine's confirmation here.
  screen = { at: "machines" }
  screen = screenAfterConnected(screen, true)
  assert.equal(screen.at, "machines")
  // Losing and renewing the client does not answer the destructive question.
  screen = screenAfterConnected(screen, true)
  assert.equal(screen.at, "machines")
})

test("initial connection and a reader reconnect still reach their intended screens", () => {
  assert.deepEqual(screenAfterConnected({ at: "checking" }, false), { at: "machines" })
  assert.deepEqual(screenAfterConnected({ at: "retrying" }, true), { at: "console" })
  assert.deepEqual(screenAfterConnected({ at: "machines" }, false), { at: "machines" })
})

test("the Devices confirmation owns the picker until cancel, so auto-opening cannot hide it", () => {
  const gate = readFileSync(new URL("./CloudGate.tsx", import.meta.url), "utf8")
  const onDevicesForget = gate.slice(gate.indexOf("setMachineForgetting((id) => {"), gate.indexOf("setMachineForgetting(null)"))
  assert.match(onDevicesForget, /setManagingMachines\(true\)/)
  assert.match(onDevicesForget, /setAsking\(named\)/)
  assert.match(onDevicesForget, /setScreen\(\{ at: "machines" \}\)/)
  assert.match(gate, /setScreen\(\(current\) => screenAfterConnected\(current, true\)\)/)
  assert.match(gate, /setScreen\(\(current\) => screenAfterConnected\(current, false\)\)/)
})
