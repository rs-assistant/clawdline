import assert from "node:assert/strict"
import test from "node:test"

// @ts-expect-error -- Node's strip-types tests import this module directly.
import { IdentityPairingError, pairThisViewer } from "./identity-pair.ts"

const b64url = (text: string) => Buffer.from(text).toString("base64url")
const b64 = (n: number, fill: number) => Buffer.alloc(n, fill).toString("base64")

async function fragment(): Promise<string> {
  const machine = await crypto.subtle.generateKey({ name: "X25519" }, true, ["deriveBits"]) as CryptoKeyPair
  const signing = await crypto.subtle.generateKey({ name: "Ed25519" }, true, ["sign", "verify"]) as CryptoKeyPair
  const raw = new Uint8Array(await crypto.subtle.exportKey("raw", signing.publicKey))
  const { ed25519Fingerprint, canonicalJSON } = await import("../../legacy/js/net/cloud-pairing.js")
  return b64url(canonicalJSON({
    v: 1, type: "pairing_qr", pairing_id: "pai_test", claim_nonce: b64(32, 1), expires_at: Date.now() + 60_000,
    account_id: "usr_test", machine_id: "mac_test", machine_signing_key: Buffer.from(raw).toString("base64"),
    machine_fingerprint: await ed25519Fingerprint(raw),
    machine_ephemeral_key: Buffer.from(new Uint8Array(await crypto.subtle.exportKey("raw", machine.publicKey))).toString("base64"),
    pairing_nonce: b64(32, 2),
  }))
}

function deps(over: Record<string, unknown>) {
  return {
    apiOrigin: "https://api.test", accountID: "usr_test", deviceID: "wd_test", signingPublicKey: new Uint8Array(32).fill(7),
    confirmCode: async () => true, persist: async () => { throw new Error("must not persist") },
    isCurrent: () => true, now: () => Date.now(), sleep: async () => {},
    fetch: async () => new Response("{}", { status: 200 }),
    ...over,
  }
}

test("a sign-out while the offer is in flight persists nothing", async () => {
  let current = true
  let persisted = 0
  const run = pairThisViewer(await fragment(), deps({
    isCurrent: () => current,
    fetch: async () => { current = false; return new Response("{}", { status: 200 }) },
    persist: async () => { persisted++ },
  }))
  await assert.rejects(run, (e: unknown) => e instanceof IdentityPairingError && e.code === "stale")
  assert.equal(persisted, 0)
})

test("an unconfirmed code sends nothing", async () => {
  let calls = 0
  await assert.rejects(pairThisViewer(await fragment(), deps({
    confirmCode: async () => false, fetch: async () => { calls++; return new Response("{}") },
  })), (e: unknown) => e instanceof IdentityPairingError && e.code === "not_confirmed")
  assert.equal(calls, 0)
})

test("a QR for another account is refused before any request", async () => {
  let calls = 0
  await assert.rejects(pairThisViewer(await fragment(), deps({
    accountID: "usr_other", fetch: async () => { calls++; return new Response("{}") },
  })), (e: unknown) => e instanceof IdentityPairingError && e.code === "wrong_account")
  assert.equal(calls, 0)
})

test("a Cloud refusal keeps its code", async () => {
  await assert.rejects(pairThisViewer(await fragment(), deps({
    fetch: async () => new Response(JSON.stringify({ error: { code: "identity_epoch_changed" } }), { status: 409 }),
  })), (e: unknown) => e instanceof IdentityPairingError && e.code === "identity_epoch_changed")
})
