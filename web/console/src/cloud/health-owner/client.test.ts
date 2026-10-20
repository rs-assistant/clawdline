import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import test from "node:test"
// @ts-expect-error -- Node's type-stripping runner loads the source directly.
import { OwnerDiagnosticsStore, SEALED_LIST_PATH, SEALED_READ_PATH } from "./client.ts"
// @ts-expect-error -- Node's type-stripping runner loads the source directly.
import { SealedError, canonicalJSON } from "./crypto.ts"
// @ts-expect-error -- Node's type-stripping runner loads the source directly.
import { PROOF_PREFIX } from "./proof.ts"

/*
 * A Cloud stand-in shaped like the private contract's routes (§13.3, §13.4): it lists metadata,
 * verifies the Ed25519 viewer proof against the registered public key, refuses a replayed nonce,
 * and answers a read with the committed vector. The client code under test is the real one.
 */
const vector = JSON.parse(readFileSync(new URL("../../../../../internal/adapters/healthsealed/testdata/vector.json", import.meta.url), "utf8"))
const NOW = 1787745600000
const fromHex = (text: string) => new Uint8Array(text.match(/../g)!.map((b) => parseInt(b, 16)))

async function fixture(options: { masterKey?: "hkdf" | "aes", refuse?: { status: number, code: string } } = {}) {
  const device = await crypto.subtle.generateKey({ name: "Ed25519" }, false, ["sign", "verify"]) as CryptoKeyPair
  const raw = fromHex(vector.master_secret_hex)
  const master = options.masterKey === "aes"
    ? await crypto.subtle.importKey("raw", raw, { name: "AES-GCM" }, false, ["encrypt", "decrypt"])
    : await crypto.subtle.importKey("raw", raw, "HKDF", false, ["deriveKey"])
  const calls: { path: string, body: unknown }[] = []
  const nonces = new Set<string>()
  const hooks: { afterRead?: () => void } = {}
  const fetchImpl = async (url: string, init: RequestInit): Promise<Response> => {
    const path = url.replace("https://api.test", "")
    const body = init.body ? JSON.parse(String(init.body)) : null
    calls.push({ path, body })
    assert.equal(init.credentials, "include")
    assert.ok(!url.includes("?"), "no query string carries anything")
    const json = (status: number, value: unknown) => new Response(JSON.stringify(value), { status, headers: { "content-type": "application/json" } })
    if (options.refuse) return json(options.refuse.status, { error: { code: options.refuse.code, message: "refused" } })
    if (path === SEALED_LIST_PATH) {
      return json(200, { schema: "clawdline.health-sealed-list/1", read_authority: "viewer_device_proof", truncated: false, attachments: [{
        attachment_id: vector.attachment_id, machine_id: "mac_vector", boot_id: "boot-v1", subject_kind: "evidence", subject_id: "ev-1",
        opened_at_ms: NOW - 1000, state: "live", latest_revision: 1, expires_at_ms: vector.aad.expires_at_ms,
        revisions: [{ revision: 1, ciphertext_bytes: 106, content_key_epoch: 1, identity_epoch: 0, received_at_ms: NOW - 1000, expires_at_ms: vector.aad.expires_at_ms, readable: true }],
      }] })
    }
    if (path === SEALED_READ_PATH) {
      const { signature, ...fields } = body
      const ok = await crypto.subtle.verify({ name: "Ed25519" }, device.publicKey, Buffer.from(signature, "base64"),
        new TextEncoder().encode(PROOF_PREFIX + canonicalJSON(fields)))
      if (!ok || fields.tenant !== "usr_vector" || fields.device_id !== "wd_1") return json(403, { error: { code: "bad_proof" } })
      if (nonces.has(fields.request_nonce)) return json(409, { error: { code: "proof_replayed" } })
      nonces.add(fields.request_nonce)
      if (fields.attachment_id !== vector.attachment_id) return json(404, { error: { code: "unknown_attachment" } })
      hooks.afterRead?.()
      return json(200, {
        schema: "clawdline.health-sealed-read-response/1", attachment_id: vector.attachment_id, revision: 1,
        aad: vector.aad, aad_sha256: vector.aad_sha256, alg: "A256GCM-HKDF-SHA256",
        nonce: Buffer.from(fromHex(vector.nonce_hex)).toString("base64"), ciphertext: vector.ciphertext_b64,
        ciphertext_sha256: vector.ciphertext_sha256, received_at_ms: NOW, audit: { recorded: true },
      })
    }
    return json(404, { error: { code: "not_found" } })
  }
  const source = {
    apiOrigin: "https://api.test",
    accountID: () => "usr_vector",
    deviceID: () => "wd_1",
    proofIdentity: () => ({ signingKey: device.privateKey, deviceKeyEpoch: 1, identityEpoch: 0 }),
    heldMasterKey: async () => ({ key: master, contentKeyEpoch: 1 }),
  }
  const store = new OwnerDiagnosticsStore(fetchImpl as never, () => NOW)
  return { store, source, calls, hooks }
}

const code = (c: string) => (error: unknown) => error instanceof SealedError && error.code === c

test("list, signed proof read and decrypt round-trip through the real client", async () => {
  const { store, source, calls } = await fixture()
  const list = await store.refresh(source as never)
  assert.equal(list.attachments[0].revisions[0].readable, true)
  const opened = await store.open(source as never, "mac_vector", vector.attachment_id, 1, 1)
  assert.equal(opened.snapshot.location, "synthetic.go:1")
  assert.equal(store.openedFor(vector.attachment_id, 1)?.snapshot.ageNs, "1")
  const read = calls.find((c) => c.path === SEALED_READ_PATH)!.body as Record<string, unknown>
  assert.deepEqual(Object.keys(read).sort(), ["attachment_id", "device_id", "device_key_epoch", "identity_epoch", "issued_at_ms", "request_nonce", "revision", "schema", "signature", "tenant"])
  assert.equal(Buffer.from(String(read.request_nonce), "base64").length, 16)
  await store.open(source as never, "mac_vector", vector.attachment_id, 1, 1)
  const nonces = calls.filter((c) => c.path === SEALED_READ_PATH).map((c) => (c.body as { request_nonce: string }).request_nonce)
  assert.notEqual(nonces[0], nonces[1], "every read carries a fresh nonce")
})

test("a browser holding only the AES pairing key never asks Cloud for bytes", async () => {
  const { store, source, calls } = await fixture({ masterKey: "aes" })
  await assert.rejects(store.open(source as never, "mac_vector", vector.attachment_id, 1, 1), code("key_handover_required"))
  assert.equal(calls.length, 0)
})

test("Cloud's refusals arrive typed: forbidden, key handover, expired, deleted, stale epoch, unavailable", async () => {
  const cases: [number, string, string][] = [
    [403, "missing_capability", "forbidden"], [403, "key_handover_required", "key_handover_required"],
    [410, "sealed_expired", "expired"], [410, "sealed_deleted", "deleted"], [409, "identity_epoch_changed", "stale"],
    [409, "proof_replayed", "conflict"], [404, "unknown_attachment", "unknown_attachment"], [429, "health_sealed_busy", "busy"],
    [503, "audit_unavailable", "unavailable"], [401, "no_viewer", "not_signed_in"],
  ]
  for (const [status, wire, want] of cases) {
    const { store, source } = await fixture({ refuse: { status, code: wire } })
    await assert.rejects(store.open(source as never, "mac_vector", vector.attachment_id, 1, 1),
      (e: unknown) => e instanceof SealedError && e.code === want && e.status === status, wire)
    assert.equal(store.openedFor(vector.attachment_id, 1), null)
  }
})

test("signing out while the list is fetched leaves nothing behind", async () => {
  const { store, source } = await fixture()
  const pending = store.refresh(source as never)
  store.clear("sign_out")
  await assert.rejects(pending, code("stale"))
  assert.equal(store.snapshotList(), null)
})

test("signing out while a read is answered or decrypted cannot repopulate the store", async () => {
  const { store, source, hooks } = await fixture()
  hooks.afterRead = () => store.clear("sign_out")
  await assert.rejects(store.open(source as never, "mac_vector", vector.attachment_id, 1, 1), code("stale"))
  assert.equal(store.openedFor(vector.attachment_id, 1), null)

  hooks.afterRead = () => { setImmediate(() => store.clear("revoked")) }
  await assert.rejects(store.open(source as never, "mac_vector", vector.attachment_id, 1, 1), code("stale"))
  assert.equal(store.openedFor(vector.attachment_id, 1), null)
})

test("another account or a new identity epoch clears what was opened", async () => {
  const { store, source } = await fixture()
  store.bind("usr_vector", 0)
  await store.open(source as never, "mac_vector", vector.attachment_id, 1, 1)
  store.bind("usr_vector", 1)
  assert.equal(store.openedFor(vector.attachment_id, 1), null)
  await store.open(source as never, "mac_vector", vector.attachment_id, 1, 1)
  store.bind("usr_other", 1)
  assert.equal(store.openedFor(vector.attachment_id, 1), null)
})

test("a device whose key epoch is unknown is refused before any request", async () => {
  const { store, source, calls } = await fixture()
  const unknown = { ...source, proofIdentity: () => ({ signingKey: null, deviceKeyEpoch: null, identityEpoch: null }) }
  await assert.rejects(store.open(unknown as never, "mac_vector", vector.attachment_id, 1, 1), code("device_epoch_unknown"))
  assert.equal(calls.length, 0)
})
