import test from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { acceptSnapshot } from "../../core/src/fleet-accept.ts"

/** What a list holds after each reading arrives, in order (`FleetStore.accept`). */
function held(...readings: any[]) {
  return readings.reduce((list, next) => acceptSnapshot(list, next) ?? list, null)
}

function row(id: string, observedAt: number) {
  return { id, label: id, work_state: "working", source: { freshness: "current", observed_at: observedAt, provenance: "cloud" },
    closeability: { state: "safe", attestation_id: null, reasons: [] } }
}

function reading(generation: number, at: number, rows: unknown[]) {
  return { at, scan: { complete: true, completed: { complete: true, sequence: generation }, emptyAuthoritative: true,
    epoch: 7, generation, provenance: "cloud" }, sessions: rows }
}

test("a complete reading taken before a Session was seen cannot take that Session off the list", () => {
  const list = held(
    // The newest pass names the Session started at 200.
    reading(1, 200, [row("%1", 200), row("%2", 200)]),
    // A held, older pass (taken at 150) comes back with a later local generation.
    reading(2, 150, [row("%1", 150)]),
  )
  assert.deepEqual(list.sessions.map((r: { id: string }) => r.id).sort(), ["%1", "%2"])
  assert.equal(list.sessions.find((r: { id: string }) => r.id === "%2").source.freshness, "unverified")
})

test("a complete reading taken after a Session was last seen still takes it off the list", () => {
  const list = held(
    reading(1, 200, [row("%1", 200), row("%2", 200)]),
    reading(2, 260, [row("%1", 260)]),
  )
  assert.deepEqual(list.sessions.map((r: { id: string }) => r.id), ["%1"])
})
