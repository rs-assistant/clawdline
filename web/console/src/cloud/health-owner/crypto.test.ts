import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import test from "node:test"
// @ts-expect-error -- Node's type-stripping runner loads the source directly.
import { SealedError, attachmentID, canonicalJSON, deriveSealedKey, hex, openSealed, sha256Hex } from "./crypto.ts"
// @ts-expect-error -- Node's type-stripping runner loads the source directly.
import { decodeSnapshot, exportSnapshot, evidenceGaps } from "./snapshot.ts"
// @ts-expect-error -- Node's type-stripping runner loads the source directly.
import { sealedKeyFor } from "./source.ts"

// The committed synthetic vector (private contract §13.7), shared with the Go writer.
const vector = JSON.parse(readFileSync(new URL("../../../../../internal/adapters/healthsealed/testdata/vector.json", import.meta.url), "utf8"))
const NOW = 1787745600000

const fromHex = (text: string) => new Uint8Array(text.match(/../g)!.map((b) => parseInt(b, 16)))
const b64 = (bytes: Uint8Array) => Buffer.from(bytes).toString("base64")

async function vectorKey() {
  const base = await crypto.subtle.importKey("raw", fromHex(vector.master_secret_hex), "HKDF", false, ["deriveKey", "deriveBits"])
  return { contentKeyEpoch: 1, key: await deriveSealedKey(base) }
}

function response(over: Record<string, unknown> = {}, aadOver: Record<string, unknown> = {}) {
  return {
    schema: "clawdline.health-sealed-read-response/1", attachment_id: vector.attachment_id, revision: 1,
    aad: { ...vector.aad, ...aadOver }, aad_sha256: vector.aad_sha256, alg: "A256GCM-HKDF-SHA256",
    nonce: b64(fromHex(vector.nonce_hex)), ciphertext: vector.ciphertext_b64, ciphertext_sha256: vector.ciphertext_sha256,
    received_at_ms: NOW, audit: { recorded: true }, ...over,
  }
}

const expect = { accountID: "usr_vector", machineID: "mac_vector", attachmentID: vector.attachment_id, revision: 1, nowMs: NOW }

async function refused(promise: Promise<unknown>, code: string) {
  await assert.rejects(promise, (error: unknown) => error instanceof SealedError && error.code === code)
}

test("the committed vector's key, AAD, digest and attachment id are reproduced", async () => {
  const bits = await crypto.subtle.deriveBits(
    { name: "HKDF", hash: "SHA-256", salt: new Uint8Array(32), info: new TextEncoder().encode("clawdline/health-sealed/v1") },
    await crypto.subtle.importKey("raw", fromHex(vector.master_secret_hex), "HKDF", false, ["deriveBits"]), 256)
  assert.equal(hex(new Uint8Array(bits)), vector.key_hex)
  assert.equal(canonicalJSON(vector.aad), vector.aad_canonical)
  assert.equal(await sha256Hex(new TextEncoder().encode(vector.aad_canonical)), vector.aad_sha256)
  assert.equal(await attachmentID("usr_vector", "mac_vector", "boot-v1", "evidence", "ev-1"), vector.attachment_id)
})

test("the committed vector opens to its plaintext and decodes as a whitelisted snapshot", async () => {
  const opened = await openSealed(response(), expect, await vectorKey())
  assert.equal(new TextDecoder().decode(opened.plaintext), vector.plaintext)
  const snapshot = decodeSnapshot(opened.plaintext)
  assert.equal(snapshot.ageNs, "1")
  assert.equal(snapshot.location, "synthetic.go:1")
  assert.equal(snapshot.lost, null, "a lost count the machine did not give is unknown, not 0")
  assert.ok(evidenceGaps(snapshot).includes("lost_unknown"))
})

test("every AAD field substituted, with a recomputed digest, fails the tag or the binding", async () => {
  const key = await vectorKey()
  const changes: Record<string, unknown> = {
    boot_id: "boot-v2", subject_id: "ev-2", expires_at_ms: 1787832000001, identity_epoch: 1, subject_kind: "incident",
  }
  for (const [field, value] of Object.entries(changes)) {
    const aad = { ...vector.aad, [field]: value }
    const digest = await sha256Hex(new TextEncoder().encode(canonicalJSON(aad)))
    await assert.rejects(openSealed(response({ aad_sha256: digest }, { [field]: value }), expect, key),
      (error: unknown) => error instanceof SealedError && ["tag_mismatch", "aad_mismatch"].includes(error.code), field)
  }
  await refused(openSealed(response({}, { tenant: "usr_other" }), expect, key), "aad_mismatch")
  await refused(openSealed(response(), { ...expect, accountID: "usr_other" }, key), "wrong_account")
  await refused(openSealed(response(), { ...expect, machineID: "mac_other" }, key), "wrong_machine")
  await refused(openSealed(response(), { ...expect, revision: 2 }, key), "wrong_attachment")
  await refused(openSealed(response(), { ...expect, attachmentID: "hs_00000000000000000000000000000000" }, key), "wrong_attachment")
})

test("a tampered ciphertext, digest or nonce never yields plaintext", async () => {
  const key = await vectorKey()
  const bytes = Buffer.from(vector.ciphertext_b64, "base64")
  bytes[3] ^= 1
  await refused(openSealed(response({ ciphertext: bytes.toString("base64") }), expect, key), "hash_mismatch")
  const digest = await sha256Hex(new Uint8Array(bytes))
  await refused(openSealed(response({ ciphertext: bytes.toString("base64"), ciphertext_sha256: digest }), expect, key), "tag_mismatch")
  await refused(openSealed(response({ nonce: b64(new Uint8Array(12)) }), expect, key), "tag_mismatch")
  await refused(openSealed(response({ aad_sha256: "0".repeat(64) }), expect, key), "aad_mismatch")
  await refused(openSealed(response({ audit: { recorded: false } }), expect, key), "bad_response")
  await refused(openSealed(response(), { ...expect, nowMs: vector.aad.expires_at_ms }, key), "expired")
})

test("a key for another epoch is never used", async () => {
  const key = await vectorKey()
  await refused(openSealed(response(), expect, { ...key, contentKeyEpoch: 2 }), "wrong_epoch")
  const base = await crypto.subtle.importKey("raw", fromHex(vector.master_secret_hex), "HKDF", false, ["deriveKey"])
  const source = { heldMasterKey: async () => ({ key: base, contentKeyEpoch: 2 }) }
  await refused(sealedKeyFor(source as never, "mac_vector", 1), "wrong_epoch")
})

test("today's pairing key, an AES-GCM import, is a key handover requirement and not a fallback", async () => {
  const aes = await crypto.subtle.importKey("raw", fromHex(vector.master_secret_hex), { name: "AES-GCM" }, false, ["encrypt", "decrypt"])
  await refused(sealedKeyFor({ heldMasterKey: async () => ({ key: aes, contentKeyEpoch: 1 }) } as never, "mac_vector", 1), "key_handover_required")
  const base = await crypto.subtle.importKey("raw", fromHex(vector.master_secret_hex), "HKDF", false, ["deriveKey"])
  await refused(sealedKeyFor({ heldMasterKey: async () => ({ key: base, contentKeyEpoch: null }) } as never, "mac_vector", 1), "key_handover_required")
  await refused(sealedKeyFor({ heldMasterKey: async () => ({ key: null, contentKeyEpoch: null }) } as never, "mac_vector", 1), "key_handover_required")
})

test("the snapshot whitelist refuses unknown fields and keeps int64 exact", () => {
  const enc = (value: unknown) => new TextEncoder().encode(JSON.stringify(value))
  const schema = "clawdline.health-owner-diagnostics/1"
  assert.throws(() => decodeSnapshot(enc({ schema, payload: "x" })), (e: unknown) => e instanceof SealedError && e.code === "snapshot_invalid")
  assert.throws(() => decodeSnapshot(enc({ schema, age_ns: 2 ** 60 })), (e: unknown) => e instanceof SealedError && e.code === "snapshot_invalid")
  assert.throws(() => decodeSnapshot(enc({ schema, age_ns: "9223372036854775808" })), (e: unknown) => e instanceof SealedError)
  const s = decodeSnapshot(enc({ schema, age_ns: "9223372036854775807", lost: "0", coverage: [{ source: "journal", from_ms: "1", to_ms: "2", lost: "0", truncated: false }] }))
  assert.equal(s.ageNs, "9223372036854775807")
  assert.deepEqual(evidenceGaps(s), [])
  const exported = JSON.parse(exportSnapshot(s, { attachmentID: "hs_x", machineID: "m", revision: 1, bootID: "b", subjectKind: "evidence", subjectID: "e", contentKeyEpoch: 1, expiresAtMs: 2, receivedAtMs: 1 }))
  assert.equal(exported.snapshot.age_ns, "9223372036854775807")
  assert.deepEqual(Object.keys(exported).sort(), ["evidence_gaps", "schema", "snapshot", "source"])
  assert.ok(!JSON.stringify(exported).includes("ciphertext") && !JSON.stringify(exported).includes("nonce"))
})

// The /2 plaintext the Go writer commits (internal/adapters/healthsealed/testdata/snapshot-v2.json).
const v2Text = readFileSync(new URL("../../../../../internal/adapters/healthsealed/testdata/snapshot-v2.json", import.meta.url), "utf8").trim()

test("a /2 snapshot carries the capture join, frames and recovery, and nothing outside its whitelist", () => {
  const enc = (value: unknown) => new TextEncoder().encode(JSON.stringify(value))
  const invalid = (e: unknown) => e instanceof SealedError && e.code === "snapshot_invalid"
  const s = decodeSnapshot(new TextEncoder().encode(v2Text))
  assert.equal(s.schema, "clawdline.health-owner-diagnostics/2")
  assert.equal(s.capture?.function, "github.com/sainteye/clawdline/internal/adapters/cloud.(*Conn).writeFrame")
  assert.deepEqual(s.capture?.sources, ["internal/adapters/cloud/websocket.go:211"])
  assert.equal(s.capture?.frames[1].source, "internal/adapters/cloud/websocket.go:211")
  assert.equal(s.capture?.frames[2].function, "<redacted>")
  assert.deepEqual(s.recovery, { state: "recovered", recoveryNs: "3500000000" })
  assert.deepEqual(evidenceGaps(s), ["capture_source_unavailable", "capture_functions_redacted"])
  const exported = JSON.parse(exportSnapshot(s, { attachmentID: "hs_x", machineID: "m", revision: 3, bootID: "b", subjectKind: "incident", subjectID: "phase-7", contentKeyEpoch: 1, expiresAtMs: 2, receivedAtMs: 1 }))
  assert.equal(exported.snapshot.capture.frames[1].source, "internal/adapters/cloud/websocket.go:211")
  assert.equal(exported.snapshot.recovery.recovery_ns, "3500000000")

  const v2 = JSON.parse(v2Text)
  // A /1 cannot carry a capture; an unknown version and an empty /2 are refused.
  assert.throws(() => decodeSnapshot(enc({ ...v2, schema: "clawdline.health-owner-diagnostics/1" })), invalid)
  assert.throws(() => decodeSnapshot(enc({ ...v2, schema: "clawdline.health-owner-diagnostics/3" })), invalid)
  assert.throws(() => decodeSnapshot(enc({ schema: "clawdline.health-owner-diagnostics/2" })), invalid)
  // A /1 written before /2 still opens exactly as before.
  const old = decodeSnapshot(enc({ schema: "clawdline.health-owner-diagnostics/1", lost: "0", coverage: [{ source: "journal", from_ms: "1", to_ms: "2", lost: "0", truncated: false }] }))
  assert.equal(old.capture, null)
  assert.equal(old.recovery, null)
  const frame = (over: Record<string, unknown>) => ({ ...v2, capture: { ...v2.capture, frames: [{ ...v2.capture.frames[0], ...over }] } })
  assert.throws(() => decodeSnapshot(enc(frame({ source: "/Users/a/go/src/sync/mutex.go:81" }))), invalid)
  assert.throws(() => decodeSnapshot(enc(frame({ source: "../x.go:1" }))), invalid)
  assert.throws(() => decodeSnapshot(enc(frame({ function: 'sync.(*Mutex).Lock(0xc000, "tok")' }))), invalid)
  assert.throws(() => decodeSnapshot(enc(frame({ source_kind: "home" }))), invalid)
  assert.throws(() => decodeSnapshot(enc(frame({ raw: "x" }))), invalid)
  assert.throws(() => decodeSnapshot(enc({ ...v2, capture: { ...v2.capture, raw_stack: "goroutine 1" } })), invalid)
  assert.throws(() => decodeSnapshot(enc({ ...v2, recovery: { state: "fine" } })), invalid)
  assert.throws(() => decodeSnapshot(enc({ ...v2, capture: { ...v2.capture, frames: new Array(25).fill(v2.capture.frames[0]) } })), invalid)
})
