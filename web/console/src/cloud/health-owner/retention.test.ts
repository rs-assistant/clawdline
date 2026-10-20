import assert from "node:assert/strict"
import test from "node:test"

// @ts-expect-error -- Node's strip-types tests import this module directly.
import { retentionLines, senderReading } from "./retention.ts"

// What the Settings window may say about `cloud_health_owner_retention`, from the daemon's own
// status body. Unknown is never "off" or "0 sent", and off never reads as "deleted".

const status = (owner: Record<string, unknown> | undefined) => ({
  upload: { state: "idle", status: owner === undefined ? {} : { owner_diagnostics: owner } },
})
const owner = (over: Record<string, unknown> = {}) => ({
  coverage: "waiting", stored: 0, refused: 0, dropped: 0, queued: 0, attachments: 0, store_bytes: 0, ...over,
})
const words = (lines: { word: string }[]) => lines.map((l) => l.word)

test("a daemon without the sender, a body that is not a status, and a sender are told apart", () => {
  assert.deepEqual(senderReading(status(undefined)), { kind: "no_sender" })
  assert.deepEqual(senderReading(null), { kind: "unknown", why: "bad_status" })
  assert.deepEqual(senderReading({ upload: {} }), { kind: "unknown", why: "bad_status" })
  // A count that is not a count is not read as 0.
  assert.deepEqual(senderReading(status(owner({ stored: "3" }))), { kind: "unknown", why: "bad_status" })
  const read = senderReading(status(owner({ attachments: null, store_bytes: null })))
  assert.equal(read.kind, "sender")
  assert.equal(read.status.attachments, null)
})

test("off says nothing is sent; off after anything was kept says Cloud's copy is not deleted", () => {
  assert.deepEqual(words(retentionLines(false, senderReading(status(owner({ coverage: "off" }))))), ["ownerRetentionOff"])
  const kept = retentionLines(false, senderReading(status(owner({ coverage: "off", stored: 4, attachments: 2, store_bytes: 2048 }))))
  assert.deepEqual(words(kept), ["ownerRetentionOffKept", "ownerRetentionLocal"])
  assert.deepEqual(kept[1].vars, { count: 2, bytes: "2.0 KiB" })
})

test("on states: waiting, active, partial, key handover needed, failed with Cloud's code", () => {
  const on = (o: Record<string, unknown>) => words(retentionLines(true, senderReading(status(owner(o)))))
  assert.deepEqual(on({ coverage: "waiting" }), ["ownerRetentionWaiting"])
  // The file says on; the sender has not re-read it yet.
  assert.deepEqual(on({ coverage: "off" }), ["ownerRetentionWaiting"])
  assert.deepEqual(on({ coverage: "active", stored: 3, attachments: 1, store_bytes: 10 }), ["ownerRetentionActive", "ownerRetentionLocal"])
  assert.deepEqual(on({ coverage: "partial", stored: 3, dropped: 1 }), ["ownerRetentionPartial"])
  assert.deepEqual(on({ coverage: "failed", code: "key_handover_required" }), ["ownerRetentionKeyNeeded"])
  const failed = retentionLines(true, senderReading(status(owner({ coverage: "failed", code: "upstream_unavailable", cloud_code: "health_ingest_disabled" }))))
  assert.deepEqual(failed[0].vars, { code: "upstream_unavailable: health_ingest_disabled" })
})

test("records not counted yet are unknown, not none", () => {
  assert.deepEqual(words(retentionLines(true, senderReading(status(owner({ attachments: null, store_bytes: null }))))),
    ["ownerRetentionWaiting", "ownerRetentionLocalUnknown"])
})

test("fault injection: a status that could not be read is unknown whatever the switch says", () => {
  assert.deepEqual(words(retentionLines(true, { kind: "unknown", why: "HTTP 501" })), ["ownerRetentionUnknown"])
  assert.deepEqual(words(retentionLines(false, { kind: "unknown", why: "unreachable" })), ["ownerRetentionOff", "ownerRetentionUnknown"])
  assert.deepEqual(words(retentionLines(true, { kind: "no_sender" })), ["ownerRetentionNoSender"])
})
