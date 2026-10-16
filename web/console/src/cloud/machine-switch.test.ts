import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- Node's type-stripping runner loads the source directly.
import { onMachineChanged, readingThrough } from "./machine-switch.ts"

// Switching the machine this page reads tells whatever keeps something about
// "the machine" — the update status above all, which showed the Mac's
// finished update on a Linux machine's page and offered no update.
test("reading another machine through the relay is announced; the same one again is not", () => {
  let changes = 0
  const stop = onMachineChanged(() => { changes++ })
  const mac = {}, linux = {}
  readingThrough(mac)
  assert.equal(changes, 0, "the first machine is not a change")
  readingThrough(linux)
  assert.equal(changes, 1)
  readingThrough(linux)
  assert.equal(changes, 1, "the same machine again is not a change")
  readingThrough(mac)
  assert.equal(changes, 2)
  stop()
})
