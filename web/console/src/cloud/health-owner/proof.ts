/*
 * The viewer proof Cloud requires before it returns or deletes sealed bytes (§13.3):
 * signed by this browser's registered Ed25519 device key, never by the master secret.
 */
// @ts-expect-error -- Node's strip-types tests import this module directly.
import { base64Encode, canonicalJSON } from "./crypto.ts"

export const READ_PROOF_SCHEMA = "clawdline.health-sealed-read/1"
export const DELETE_PROOF_SCHEMA = "clawdline.health-sealed-delete/1"
export const PROOF_PREFIX = "clawdline-health-sealed-proof-v1\n"

export interface ProofIdentity {
  accountID: string
  deviceID: string
  deviceKeyEpoch: number
  identityEpoch: number
  signingKey: CryptoKey
}

export interface SealedProof {
  schema: string
  tenant: string
  device_id: string
  device_key_epoch: number
  identity_epoch: number
  attachment_id: string
  revision: number
  issued_at_ms: number
  request_nonce: string
  signature: string
}

export function proofSigningBytes(fields: Omit<SealedProof, "signature">): Uint8Array {
  return new TextEncoder().encode(PROOF_PREFIX + canonicalJSON(fields))
}

/** A read proof for one revision, or a delete proof (revision 0). A fresh nonce each call. */
export async function signProof(kind: "read" | "delete", who: ProofIdentity, attachmentID: string, revision: number,
  nowMs = Date.now(), nonce: Uint8Array = crypto.getRandomValues(new Uint8Array(16))): Promise<SealedProof> {
  const fields = {
    schema: kind === "read" ? READ_PROOF_SCHEMA : DELETE_PROOF_SCHEMA,
    tenant: who.accountID,
    device_id: who.deviceID,
    device_key_epoch: who.deviceKeyEpoch,
    identity_epoch: who.identityEpoch,
    attachment_id: attachmentID,
    revision: kind === "read" ? revision : 0,
    issued_at_ms: nowMs,
    request_nonce: base64Encode(nonce),
  }
  const signature = new Uint8Array(await crypto.subtle.sign({ name: "Ed25519" }, who.signingKey, proofSigningBytes(fields) as BufferSource))
  return { ...fields, signature: base64Encode(signature) }
}
