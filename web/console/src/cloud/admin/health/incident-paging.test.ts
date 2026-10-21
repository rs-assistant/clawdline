// The incident list past its first 50 rows: older pages by the Cloud's cursor, under one frozen
// window, up to the page's held bound, and a cursor from another window refused as such.
// `node --experimental-strip-types --test web/console/src/cloud/admin/health/incident-paging.test.ts`.
import { test } from "node:test"
import assert from "node:assert/strict"
import type { HealthRoute } from "./route.ts"
import type { StoredState } from "./cloud.ts"
// @ts-expect-error -- a `.ts` path, for node.
import { emptyStored, heldIncidents, HEALTH_UI_LIMITS, loadStored, mayLoadOlderIncidents, olderRead, planFor, runReads, storedReducer } from "./cloud.ts"
// @ts-expect-error -- a `.ts` path, for node.
import { healthRoute, routeKey } from "./route.ts"
// @ts-expect-error -- a `.ts` path, for node.
import { fixtureAnswer } from "./fixture.ts"

const NOW = Date.parse("2026-10-20T12:00:00.000Z")
const API = "https://api.example.invalid"
const get = (async (input: RequestInfo | URL) => fixtureAnswer("many_incidents", String(input), NOW)) as typeof fetch
const pinned: HealthRoute = healthRoute("/admin/health/incidents", "?until=2026-10-20T11:59:00.000Z", NOW)!

async function first(route: HealthRoute): Promise<StoredState> {
  const out = await loadStored(get, API, route, NOW, new AbortController().signal)
  return storedReducer(emptyStored(routeKey(route)), { type: "loaded", outcome: out, at: NOW })
}

async function older(state: StoredState, route: HealthRoute): Promise<StoredState> {
  const read = planFor(route, NOW).reads.find((r: { part: string }) => r.part === "incidents")!
  const r = await runReads(get, API, [olderRead(read, { from: read.params.from, to: read.params.to }, state.incCursor!)], new AbortController().signal)
  assert.equal(r.kind, "parts")
  return storedReducer(state, { type: "inc_older_loaded", result: r.parts.incidents!, at: NOW })
}

test("a paused incident list loads older pages by cursor until every incident in the window is held", async () => {
  let state = await first(pinned)
  assert.equal(heldIncidents(state).length, HEALTH_UI_LIMITS.incidentRows)
  assert.equal(mayLoadOlderIncidents(state, true), "yes")
  assert.equal(mayLoadOlderIncidents(state, false), "live", "a live window moves, so its cursor cannot page")
  state = await older(state, pinned)
  state = await older(state, pinned)
  const held = heldIncidents(state)
  assert.equal(new Set(held.map((r) => r.id)).size, held.length, "no repeats")
  assert.ok(held.length > 100 && held.length <= 3 * HEALTH_UI_LIMITS.incidentRows)
  assert.equal(state.incCursor, null, "the Cloud said there is no more")
  assert.equal(mayLoadOlderIncidents(state, true), "none")
  // Newest opened first across pages.
  const opened = held.map((r) => Date.parse(r.opened_at))
  assert.deepEqual(opened, [...opened].sort((a, b) => b - a))
})

test("the held pages stop at the page bound, and a refresh of the same window keeps them", async () => {
  let state = await first(pinned)
  state = { ...state, incOlder: Array.from({ length: HEALTH_UI_LIMITS.incidentPages - 1 }, () => []) }
  assert.equal(mayLoadOlderIncidents(state, true), "bound")
  const again = await loadStored(get, API, pinned, NOW, new AbortController().signal)
  const kept = storedReducer(state, { type: "loaded", outcome: again, at: NOW + 15_000 })
  assert.equal(kept.incOlder.length, HEALTH_UI_LIMITS.incidentPages - 1)
})

test("a moved window drops older pages, and a cursor from another window is refused as bad_cursor", async () => {
  let state = await first(pinned)
  state = await older(state, pinned)
  const moved: HealthRoute = healthRoute("/admin/health/incidents", "?until=2026-10-20T11:30:00.000Z", NOW)!
  const out = await loadStored(get, API, moved, NOW, new AbortController().signal)
  const next = storedReducer(state, { type: "loaded", outcome: out, at: NOW })
  assert.deepEqual(next.incOlder, [])
  const read = planFor(moved, NOW).reads.find((r: { part: string }) => r.part === "incidents")!
  const r = await runReads(get, API, [olderRead(read, { from: read.params.from, to: read.params.to }, state.incCursor!)], new AbortController().signal)
  assert.equal(r.kind, "parts")
  assert.deepEqual(r.parts.incidents, { ok: false, code: "bad_cursor" })
  const failed = storedReducer(state, { type: "inc_older_loaded", result: r.parts.incidents!, at: NOW })
  assert.equal(failed.incFailure, "bad_cursor")
  assert.equal(heldIncidents(failed).length, heldIncidents(state).length, "a failed page leaves the held ones")
})
