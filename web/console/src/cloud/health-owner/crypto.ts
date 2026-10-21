/*
 * Owner-sealed diagnostic attachments, the viewer's half (private contract §13.3).
 *
 * Cloud stores ciphertext a machine sealed with a subkey of the account master secret
 * and returns it only to a viewer device that signed a fresh proof. This module checks
 * every field Cloud returned against what was asked for and against the AAD it binds,
 * then opens it. Any mismatch is a typed `SealedError`; nothing is ever half-shown.
 */

export const SEALED_ALG = "A256GCM-HKDF-SHA256"
export const SEALED_AAD_SCHEMA = "clawdline.health-sealed-aad/1"
export const SEALED_READ_RESPONSE_SCHEMA = "clawdline.health-sealed-read-response/1"
export const SEALED_HKDF_INFO = "clawdline/health-sealed/v1"
/** Ciphertext with its 16-byte tag, per revision (`SEALED_LIMITS`). */
export const SEALED_CIPHERTEXT_MIN = 17
export const SEALED_CIPHERTEXT_MAX = 256 * 1024

export type SealedErrorCode =
  | "aad_mismatch" | "hash_mismatch" | "wrong_account" | "wrong_machine" | "wrong_attachment"
  | "wrong_epoch" | "tag_mismatch" | "bad_response" | "snapshot_invalid"
  | "key_handover_required" | "device_epoch_unknown" | "forbidden" | "not_signed_in"
  | "unknown_attachment" | "expired" | "deleted" | "conflict" | "busy" | "unavailable" | "stale"

/** A refusal the caller can branch on. `status` is the HTTP status when one was seen. */
export class SealedError extends Error {
  readonly code: SealedErrorCode
  readonly status: number | null
  readonly detail: string | null
  constructor(code: SealedErrorCode, message: string, status: number | null = null, detail: string | null = null) {
    super(message)
    this.name = "SealedError"
    this.code = code
    this.status = status
    this.detail = detail
  }
}

const encoder = new TextEncoder()

/** RFC 8785 canonical JSON, the pairing subset: sorted keys, safe integers, no whitespace. */
export function canonicalJSON(value: unknown): string {
  if (value === null) return "null"
  if (typeof value === "boolean") return value ? "true" : "false"
  if (typeof value === "string") return JSON.stringify(value)
  if (typeof value === "number") {
    if (!Number.isSafeInteger(value)) throw new SealedError("bad_response", "canonical JSON holds only safe integers")
    return String(value)
  }
  if (Array.isArray(value)) return "[" + value.map(canonicalJSON).join(",") + "]"
  if (typeof value === "object") {
    const record = value as Record<string, unknown>
    const keys = Object.keys(record).sort()
    return "{" + keys.map((key) => JSON.stringify(key) + ":" + canonicalJSON(record[key])).join(",") + "}"
  }
  throw new SealedError("bad_response", "canonical JSON cannot hold " + typeof value)
}

export function hex(bytes: Uint8Array): string {
  let out = ""
  for (const byte of bytes) out += byte.toString(16).padStart(2, "0")
  return out
}

export function base64Decode(text: unknown, field: string): Uint8Array {
  if (typeof text !== "string" || !/^[A-Za-z0-9+/]*={0,2}$/.test(text) || text.length % 4 !== 0) {
    throw new SealedError("bad_response", field + " is not base64")
  }
  const raw = atob(text)
  const out = new Uint8Array(raw.length)
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i)
  return out
}

export function base64Encode(bytes: Uint8Array): string {
  let raw = ""
  for (const byte of bytes) raw += String.fromCharCode(byte)
  return btoa(raw)
}

export async function sha256Hex(bytes: Uint8Array): Promise<string> {
  return hex(new Uint8Array(await crypto.subtle.digest("SHA-256", bytes as BufferSource)))
}

/** The 13 fields the machine bound, in the order the contract lists them. */
export const AAD_FIELDS = [
  "alg", "attachment_id", "boot_id", "content_key_epoch", "expires_at_ms", "identity_epoch", "machine_id",
  "previous_ciphertext_sha256", "revision", "schema", "subject_id", "subject_kind", "tenant",
] as const

export interface SealedAAD {
  alg: string
  attachment_id: string
  boot_id: string
  content_key_epoch: number
  expires_at_ms: number
  identity_epoch: number
  machine_id: string
  previous_ciphertext_sha256: string
  revision: number
  schema: string
  subject_id: string
  subject_kind: string
  tenant: string
}

const ID = /^[A-Za-z0-9._:-]{1,128}$/
const DIGEST = /^[0-9a-f]{64}$/

function exactKeys(value: unknown, keys: readonly string[], what: string): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new SealedError("bad_response", what + " is not an object")
  const record = value as Record<string, unknown>
  const have = Object.keys(record)
  if (have.length !== keys.length || keys.some((key) => !(key in record))) {
    throw new SealedError("bad_response", what + " does not carry exactly its fields")
  }
  return record
}

function int(value: unknown, field: string, min: number, max = Number.MAX_SAFE_INTEGER): number {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < min || value > max) {
    throw new SealedError("bad_response", field + " is not an integer in range")
  }
  return value
}

function id(value: unknown, field: string): string {
  if (typeof value !== "string" || !ID.test(value)) throw new SealedError("bad_response", field + " is not an id")
  return value
}

export function parseAAD(value: unknown): SealedAAD {
  const r = exactKeys(value, AAD_FIELDS, "aad")
  if (r.schema !== SEALED_AAD_SCHEMA) throw new SealedError("aad_mismatch", "aad schema is not " + SEALED_AAD_SCHEMA)
  if (r.alg !== SEALED_ALG) throw new SealedError("aad_mismatch", "aad alg is not " + SEALED_ALG)
  if (r.subject_kind !== "incident" && r.subject_kind !== "evidence") throw new SealedError("bad_response", "aad subject_kind is unknown")
  const previous = r.previous_ciphertext_sha256
  const revision = int(r.revision, "revision", 1, 8)
  if (typeof previous !== "string" || (revision === 1 ? previous !== "" : !DIGEST.test(previous))) {
    throw new SealedError("bad_response", "aad previous_ciphertext_sha256 does not fit its revision")
  }
  if (typeof r.tenant !== "string" || !r.tenant || typeof r.machine_id !== "string" || !r.machine_id ||
      typeof r.attachment_id !== "string" || !/^hs_[0-9a-f]{32}$/.test(r.attachment_id)) {
    throw new SealedError("bad_response", "aad names no tenant, machine or attachment")
  }
  return {
    alg: SEALED_ALG, schema: SEALED_AAD_SCHEMA,
    attachment_id: r.attachment_id, boot_id: id(r.boot_id, "boot_id"),
    content_key_epoch: int(r.content_key_epoch, "content_key_epoch", 0),
    expires_at_ms: int(r.expires_at_ms, "expires_at_ms", 0),
    identity_epoch: int(r.identity_epoch, "identity_epoch", 0),
    machine_id: r.machine_id, previous_ciphertext_sha256: previous, revision,
    subject_id: id(r.subject_id, "subject_id"), subject_kind: r.subject_kind, tenant: r.tenant,
  }
}

/** `hs_` + the first 32 hex digits of SHA-256 over the attachment's identity (§13.2). */
export async function attachmentID(tenant: string, machine: string, boot: string, kind: string, subject: string): Promise<string> {
  const text = SEALED_AAD_SCHEMA + "\n" + tenant + "\n" + machine + "\n" + boot + "\n" + kind + "\n" + subject
  return "hs_" + (await sha256Hex(encoder.encode(text))).slice(0, 32)
}

/**
 * K = HKDF-SHA256(master secret, salt = 32 zero bytes, info = "clawdline/health-sealed/v1", 32 bytes),
 * returned as a non-extractable AES-GCM key that can only decrypt. The base key must be the
 * master secret imported for HKDF; an AES-GCM import cannot derive anything.
 */
export async function deriveSealedKey(hkdfBaseKey: CryptoKey): Promise<CryptoKey> {
  if (!hkdfBaseKey || hkdfBaseKey.algorithm?.name !== "HKDF") {
    throw new SealedError("key_handover_required", "the held master secret cannot derive the diagnostic key")
  }
  return crypto.subtle.deriveKey(
    { name: "HKDF", hash: "SHA-256", salt: new Uint8Array(32), info: encoder.encode(SEALED_HKDF_INFO) },
    hkdfBaseKey, { name: "AES-GCM", length: 256 }, false, ["decrypt"])
}

/** What the viewer asked for; every one of these must come back unchanged. */
export interface SealedExpectation {
  accountID: string
  machineID: string
  attachmentID: string
  revision: number
  nowMs?: number
}

/** The key that may open it: one derived key for exactly one content-key epoch. */
export interface SealedOpeningKey {
  contentKeyEpoch: number
  key: CryptoKey
}

export interface OpenedSealed {
  aad: SealedAAD
  receivedAtMs: number
  plaintext: Uint8Array
}

const READ_RESPONSE_FIELDS = [
  "schema", "attachment_id", "revision", "aad", "aad_sha256", "alg", "nonce", "ciphertext",
  "ciphertext_sha256", "received_at_ms", "audit",
] as const

/** Checks a read response end to end and opens it, or throws; it never returns part of it. */
export async function openSealed(response: unknown, expect: SealedExpectation, opening: SealedOpeningKey): Promise<OpenedSealed> {
  const r = exactKeys(response, READ_RESPONSE_FIELDS, "read response")
  if (r.schema !== SEALED_READ_RESPONSE_SCHEMA) throw new SealedError("bad_response", "read response schema is unknown")
  if (r.alg !== SEALED_ALG) throw new SealedError("aad_mismatch", "read response alg is not " + SEALED_ALG)
  const audit = r.audit as { recorded?: unknown } | null
  if (!audit || typeof audit !== "object" || audit.recorded !== true) {
    throw new SealedError("bad_response", "Cloud did not record the read before returning bytes")
  }
  const aad = parseAAD(r.aad)
  const aadBytes = encoder.encode(canonicalJSON(aad))
  if (typeof r.aad_sha256 !== "string" || await sha256Hex(aadBytes) !== r.aad_sha256) {
    throw new SealedError("aad_mismatch", "the AAD digest does not match the AAD")
  }
  if (aad.tenant !== expect.accountID) throw new SealedError("wrong_account", "the attachment names another account")
  if (aad.machine_id !== expect.machineID) throw new SealedError("wrong_machine", "the attachment names another machine")
  if (r.attachment_id !== expect.attachmentID || aad.attachment_id !== expect.attachmentID ||
      r.revision !== expect.revision || aad.revision !== expect.revision) {
    throw new SealedError("wrong_attachment", "the answer is not the attachment and revision asked for")
  }
  if (await attachmentID(aad.tenant, aad.machine_id, aad.boot_id, aad.subject_kind, aad.subject_id) !== aad.attachment_id) {
    throw new SealedError("aad_mismatch", "the attachment id does not derive from its AAD")
  }
  if (aad.content_key_epoch !== opening.contentKeyEpoch) {
    throw new SealedError("wrong_epoch", "the attachment was sealed under another content-key epoch")
  }
  const now = expect.nowMs ?? Date.now()
  if (aad.expires_at_ms <= now) throw new SealedError("expired", "the attachment has expired")
  const nonce = base64Decode(r.nonce, "nonce")
  if (nonce.length !== 12) throw new SealedError("bad_response", "nonce is not 12 bytes")
  const sealed = base64Decode(r.ciphertext, "ciphertext")
  if (sealed.length < SEALED_CIPHERTEXT_MIN || sealed.length > SEALED_CIPHERTEXT_MAX) {
    throw new SealedError("bad_response", "ciphertext size is outside its bound")
  }
  if (typeof r.ciphertext_sha256 !== "string" || await sha256Hex(sealed) !== r.ciphertext_sha256) {
    throw new SealedError("hash_mismatch", "the ciphertext digest does not match the ciphertext")
  }
  const receivedAtMs = int(r.received_at_ms, "received_at_ms", 0)
  let clear: ArrayBuffer
  try {
    clear = await crypto.subtle.decrypt({ name: "AES-GCM", iv: nonce as BufferSource, additionalData: aadBytes as BufferSource, tagLength: 128 },
      opening.key, sealed as BufferSource)
  } catch {
    throw new SealedError("tag_mismatch", "the attachment did not authenticate")
  }
  return { aad, receivedAtMs, plaintext: new Uint8Array(clear) }
}
