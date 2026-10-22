import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see cloud/relay-reader.test.ts.
import { accessProblemAfterRead, machineAccessProblem, machineListAnswer, machineListRefusal, readProblemOffersPairing, retryReason } from "./state.ts"

test("the machine list keeps loading, authoritative empty, and refusal apart", () => {
  assert.deepEqual(machineListAnswer({ machines: [], syncing: true }), { phase: "loading" })
  assert.deepEqual(machineListAnswer({ machines: [], syncing: false }), { phase: "empty_authoritative" })
  assert.deepEqual(machineListRefusal({ code: "unauthorized" }), {
    phase: "refused",
    code: "unauthorized",
    next: "sign_in",
  })
  assert.deepEqual(machineListRefusal({ code: "unreadable_envelope" }), {
    phase: "refused",
    code: "unreadable_envelope",
    next: "pair",
  })
  assert.deepEqual(machineListRefusal(new TypeError("Failed to fetch")), {
    phase: "refused",
    code: "machine_list_unanswered",
    next: "retry",
  })
})

test("an untyped reconnect distinguishes this browser being offline from an unknown interruption", () => {
  assert.deepEqual(retryReason({ code: "rate_limited" }, true), { kind: "named", code: "rate_limited" })
  assert.deepEqual(retryReason(new TypeError("Failed to fetch"), false), { kind: "browser_offline" })
  assert.deepEqual(retryReason(new TypeError("Failed to fetch"), true), { kind: "unknown" })
})

test("an access problem follows its receipt back to the machine id", () => {
  const error = Object.assign(new Error("cannot open"), {
    code: "unknown_sender",
    viewerEvent: { n: 17 },
  })
  const viewerEvents = {
    snapshot: () => ({
      rows: [
        { n: 16, data: { routed_machine: "machine-before" } },
        { n: 17, data: { routed_machine: "machine-five" } },
      ],
    }),
  }
  assert.deepEqual(machineAccessProblem({ type: "error", error }, viewerEvents), {
    machine: "machine-five",
    code: "unknown_sender",
  })
})

test("an access problem with no machine is withheld instead of accusing one of five", () => {
  assert.equal(machineAccessProblem({ type: "error", error: { code: "unknown_sender" } }), null)
  assert.deepEqual(
    machineAccessProblem({ type: "error", error: { code: "unknown_sender", detail: { machine: "machine-two" } } }),
    { machine: "machine-two", code: "unknown_sender" },
  )
})


test("timeout and a named pairing refusal retain their machine without guessing a cause", () => {
  assert.deepEqual(machineAccessProblem({ type: "read", identity: {machine: "m"}, error: {code: "cloud_read_timeout"} }), {machine: "m", code: "cloud_read_timeout"})
  assert.deepEqual(machineAccessProblem({ type: "error", machine: "m", error: {code: "machine_pairing_required"} }), {machine: "m", code: "machine_pairing_required"})
  assert.equal(machineAccessProblem({type: "read", machine: "m"}), null)
  assert.equal(machineAccessProblem({type: "error", error: {code: "cloud_read_timeout"}}), null)
})


test("cached data and other machines do not clear a failed read", () => {
  const held = {machine: "m", code: "cloud_read_timeout"}
  for (const event of [
    {type:"sessions", machine:"m"}, {type:"read", machine:"other"}, {type:"read"},
    {type:"read", machine:"m", realign:true}, {type:"read", machine:"m", error:{code:"offline"}},
  ]) assert.equal(accessProblemAfterRead(held,event),held)
  assert.equal(accessProblemAfterRead(held,{type:"read", identity:{machine:"m"}}),null)
})

test("a machine that reports to the account but does not answer this device offers pairing again", () => {
  assert.equal(readProblemOffersPairing("cloud_read_timeout", "current"), true, "revoked here: dropped unanswered")
  assert.equal(readProblemOffersPairing("cloud_read_unavailable", "current"), true)
  assert.equal(readProblemOffersPairing("cloud_read_timeout", "stale"), false, "an offline machine is not fixed by pairing")
  assert.equal(readProblemOffersPairing("cloud_read_timeout", "unknown"), false)
  assert.equal(readProblemOffersPairing("unknown_sender", "stale"), true, "a named pairing failure offers it anyway")
  assert.equal(readProblemOffersPairing("socket_error", "current"), false, "this browser's own connection is not a pairing problem")
})
