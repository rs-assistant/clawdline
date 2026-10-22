/*
 * Where the owner-diagnostics reader gets its authority, installed by the gate while it holds
 * a signed-in client (as `machine-manager.ts` is) and removed when it lets go.
 *
 * Three different things are needed and none is invented here:
 * - the account id and this browser's device id (from the signed-in client);
 * - this device's registered Ed25519 key with its key epoch and the account identity epoch,
 *   for the read proof (`proof.ts`);
 * - the account master secret for the attachment's content-key epoch, held as an HKDF key.
 *
 * Today's pairing handover keeps the master secret only as a non-extractable AES-GCM key
 * (`legacy/js/net/cloud-crypto.js` `importMasterSecret`) and records no content-key epoch.
 * Such a key cannot be HKDF input, so `sealedKeyFor` answers `key_handover_required` rather
 * than falling back to anything: there is no second secret path and no automatic re-pairing.
 */
// @ts-expect-error -- Node's strip-types tests import this module directly.
import { SealedError, deriveSealedKey, type SealedOpeningKey } from "./crypto.ts"
import type { ProofIdentity } from "./proof.js"

/** The master secret as the pairing handover left it, for one machine. */
export interface HeldMasterKey {
  key: CryptoKey | null
  /** The content-key epoch it belongs to; null when the handover did not record one. */
  contentKeyEpoch: number | null
}

export interface OwnerDiagnosticsSource {
  apiOrigin: string
  accountID(): string | null
  deviceID(): string | null
  /** This device's proof identity; null fields mean the client cannot say, never a guess. */
  proofIdentity(): {
    signingKey: CryptoKey | null
    deviceKeyEpoch: number | null
    identityEpoch: number | null
  }
  heldMasterKey(machineID: string): Promise<HeldMasterKey>
}

/** Turns what the source holds into one opening key, or a typed refusal. */
export async function sealedKeyFor(source: OwnerDiagnosticsSource, machineID: string, contentKeyEpoch: number): Promise<SealedOpeningKey> {
  const held = await source.heldMasterKey(machineID)
  if (!held.key || held.key.algorithm?.name !== "HKDF" || held.contentKeyEpoch === null) {
    throw new SealedError("key_handover_required",
      "this browser does not hold the master secret in a form that can open diagnostics; pair it again once the handover carries it")
  }
  if (held.contentKeyEpoch !== contentKeyEpoch) {
    throw new SealedError("wrong_epoch", "this browser holds another content-key epoch")
  }
  return { contentKeyEpoch, key: await deriveSealedKey(held.key) }
}

export function proofIdentityOf(source: OwnerDiagnosticsSource): ProofIdentity {
  const accountID = source.accountID()
  const deviceID = source.deviceID()
  if (!accountID || !deviceID) throw new SealedError("not_signed_in", "this browser is not signed in to an account")
  const p = source.proofIdentity()
  if (!p.signingKey) throw new SealedError("device_epoch_unknown", "this browser's device key is not available")
  if (p.deviceKeyEpoch === null || p.identityEpoch === null) {
    throw new SealedError("device_epoch_unknown", "this browser cannot say its device key epoch or the account identity epoch")
  }
  return { accountID, deviceID, signingKey: p.signingKey, deviceKeyEpoch: p.deviceKeyEpoch, identityEpoch: p.identityEpoch }
}

let installed: OwnerDiagnosticsSource | null = null
const listeners = new Set<() => void>()

export function setOwnerDiagnosticsSource(source: OwnerDiagnosticsSource | null): void {
  installed = source
  for (const listener of [...listeners]) listener()
}

export function ownerDiagnosticsSource(): OwnerDiagnosticsSource | null {
  return installed
}

export function watchOwnerDiagnosticsSource(listener: () => void): () => void {
  listeners.add(listener)
  return () => { listeners.delete(listener) }
}
