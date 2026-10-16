import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { currentOffer, OFFERS, offerFeatures, offerFor } from "./offers.ts"

test("only Free and Pro are offered, with the limits the pricing page prints", () => {
  assert.deepEqual(Object.keys(OFFERS), ["free", "pro"])
  assert.deepEqual(offerFeatures(OFFERS.free), [
    "1 paired machine", "2 paired phones or browsers",
    "View and control Sessions from your phone", "Browser push notifications",
  ])
  assert.deepEqual(offerFeatures(OFFERS.pro), [
    "Up to 5 paired machines", "5 paired phones or browsers",
    "View and control Sessions from your phone", "Browser push notifications",
    "Start saved schedules by webhook",
  ])
})

test("the card the account is on takes its numbers from the server's answer", () => {
  const answer = { max_machines: 3, max_viewer_devices: 4, schedule_webhooks: true }
  assert.deepEqual(offerFor("free", "free", answer), { machines: 3, devices: 4, webhooks: true })
  assert.deepEqual(offerFor("pro", "free", answer), OFFERS.pro)
  assert.deepEqual(offerFor("pro", "pro", { max_machines: null }), OFFERS.pro)
})

test("no card claims to be current until the server has named a tier", () => {
  for (const state of ["idle", "loading", "signed_out", "not_here", "unavailable", "waiting"]) {
    assert.equal(currentOffer(state, "pro"), null, state)
  }
  assert.equal(currentOffer("free", "free"), "free")
  assert.equal(currentOffer("paid", "pro"), "pro")
  assert.equal(currentOffer("already_paid", "max"), "pro")
  assert.equal(currentOffer("paid", undefined), null)
})
