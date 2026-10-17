import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { carrierReading, machineLight } from "./carrier-state.ts"

test("an open channel is the direct road, whatever else is known", () => {
  assert.equal(carrierReading({ known: true, supported: true, open: true }).show, "direct")
  // The descriptor can arrive after the channel this page opened.
  assert.equal(carrierReading({ known: false, supported: false, open: true }).show, "direct")
})

test("a machine this browser has not read yet is unknown, not relay", () => {
  const reading = carrierReading({ known: false, supported: false, open: false })
  assert.equal(reading.show, "unknown")
  assert.equal(reading.word, "cloudCarrierUnknown")
})

test("relay says which of the two reasons it is", () => {
  assert.deepEqual(carrierReading({ known: true, supported: false, open: false }),
    { show: "relay", word: "cloudCarrierRelayMachineOff" })
  assert.deepEqual(carrierReading({ known: true, supported: true, open: false }),
    { show: "relay", word: "cloudCarrierRelayNotOpen" })
})

test("a machine that stopped reporting is offline, however its reads would travel", () => {
  // The descriptor it sent before it went quiet still says relay; the dot must not.
  const relay = carrierReading({ known: true, supported: true, open: false })
  assert.deepEqual(machineLight("stale", relay), { show: "offline", word: "cloudCarrierOffline" })
  assert.deepEqual(machineLight("stale", null), { show: "offline", word: "cloudCarrierOffline" })
})

test("a machine never heard from says so, not which road it would take", () => {
  const relay = carrierReading({ known: true, supported: true, open: false })
  assert.deepEqual(machineLight("unknown", relay), { show: "unknown", word: "cloudCarrierNoReport" })
})

test("only a reporting machine shows its road", () => {
  const direct = carrierReading({ known: true, supported: true, open: true })
  assert.equal(machineLight("current", direct), direct)
  assert.equal(machineLight("current", null), null)
})
