import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { arriveAtDetail, detailHash, detailInHash, enterDetail, leaveDetail, type DetailHistory } from "./page-detail.ts"

/** A history with entries and a cursor, as a browser's same-document steps behave. */
function fakeHistory(start: string): DetailHistory & { url(): string; length(): number; backs: number } {
  const entries: { state: unknown; url: string }[] = [{ state: null, url: start }]
  let at = 0
  const h = {
    backs: 0,
    get state() { return entries[at].state },
    pushState(state: unknown, _: string, url: string) {
      entries.splice(at + 1)
      entries.push({ state, url })
      at++
    },
    replaceState(state: unknown, _: string, url: string) { entries[at] = { state, url } },
    back() { if (at > 0) at--; h.backs++ },
    url: () => entries[at].url,
    length: () => entries.length,
  }
  return h
}

test("an address names one record of one page", () => {
  assert.equal(detailInHash("verify", detailHash("verify", "a b&c")), "a b&c")
  assert.equal(detailInHash("verify", "#page=verify"), "")
  assert.equal(detailInHash("verify", "#page=projects&id=x"), "")
})

test("opening a record adds one step, and leaving it goes back over that step", () => {
  const h = fakeHistory("#page=verify")
  enterDetail("verify", "r1", h)
  assert.equal(h.url(), "#page=verify&id=r1")
  assert.equal(h.length(), 2)
  assert.equal(leaveDetail("verify", h), true)
  assert.equal(h.url(), "#page=verify")
  assert.equal(h.backs, 1)
})

test("opening a second record from an open one replaces the step", () => {
  const h = fakeHistory("#page=verify")
  enterDetail("verify", "r1", h)
  enterDetail("verify", "r2", h)
  assert.equal(h.length(), 2)
  assert.equal(h.url(), "#page=verify&id=r2")
  leaveDetail("verify", h)
  assert.equal(h.url(), "#page=verify")
})

test("an address that arrived naming a record gets a list under it", () => {
  const h = fakeHistory("#page=verify&id=r1")
  arriveAtDetail("verify", "r1", h)
  assert.equal(h.length(), 2)
  assert.equal(h.url(), "#page=verify&id=r1")
  leaveDetail("verify", h)
  assert.equal(h.url(), "#page=verify")
})

test("a page never goes back over a step it did not add", () => {
  const h = fakeHistory("#page=verify&id=r1")
  assert.equal(leaveDetail("verify", h), false)
  assert.equal(h.backs, 0)
  assert.equal(h.url(), "#page=verify")
  enterDetail("projects", "p1", h)
  assert.equal(leaveDetail("verify", h), false)
  assert.equal(h.backs, 0)
})
