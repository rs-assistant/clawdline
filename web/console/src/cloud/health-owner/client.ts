/*
 * The owner-diagnostics reader: Cloud's metadata list, one proof-gated read, and the opened
 * snapshots, held in memory only. Nothing here is written to localStorage, sessionStorage or
 * IndexedDB; a reload starts empty.
 *
 * Every clear (sign-out, another account, an identity-epoch change, a revoked device) moves the
 * store to a new generation. A fetch or a decryption that began under an older generation is
 * thrown away when it finishes, so a slow answer can never put bytes back after a clear.
 *
 * This path reads Cloud's own routes, not the business relay, so a machine that is offline
 * still has its retained ciphertext listed and readable by a browser that holds the key.
 */
// @ts-expect-error -- Node's strip-types tests import this module directly.
import { SealedError, openSealed, type SealedErrorCode } from "./crypto.ts"
// @ts-expect-error -- Node's strip-types tests import this module directly.
import { signProof } from "./proof.ts"
// @ts-expect-error -- Node's strip-types tests import this module directly.
import { decodeSnapshot, type OwnerSnapshot } from "./snapshot.ts"
// @ts-expect-error -- Node's strip-types tests import this module directly.
import { proofIdentityOf, sealedKeyFor, type OwnerDiagnosticsSource } from "./source.ts"

export const SEALED_LIST_PATH = "/v1/health/sealed"
export const SEALED_READ_PATH = "/v1/health/sealed/read"
/** One Cloud call; the list and the read are both bounded server-side (5 s deadline). */
export const SEALED_REQUEST_TIMEOUT_MS = 10_000
/** Cloud lists at most 100 attachments; the store keeps no more opened snapshots than that. */
export const SEALED_LIST_MAX = 100
export const SEALED_OPENED_MAX = 100

export interface SealedRevisionMeta {
  revision: number
  ciphertextBytes: number
  contentKeyEpoch: number
  identityEpoch: number
  receivedAtMs: number
  expiresAtMs: number
  readable: boolean
}

export interface SealedAttachmentMeta {
  attachmentID: string
  machineID: string
  bootID: string
  subjectKind: string
  subjectID: string
  openedAtMs: number
  state: "live" | "expired" | "deleted"
  latestRevision: number
  expiresAtMs: number
  revisions: SealedRevisionMeta[]
}

export interface SealedList {
  attachments: SealedAttachmentMeta[]
  truncated: boolean
  readAtMs: number
}

export interface OpenedDiagnostic {
  attachmentID: string
  machineID: string
  revision: number
  bootID: string
  subjectKind: string
  subjectID: string
  contentKeyEpoch: number
  expiresAtMs: number
  receivedAtMs: number
  snapshot: OwnerSnapshot
}

export type ClearReason = "sign_out" | "account_changed" | "epoch_changed" | "revoked" | "source_removed"

type Fetch = (input: string, init: RequestInit) => Promise<Response>

function num(value: unknown, field: string): number {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < 0) {
    throw new SealedError("bad_response", field + " is not a count")
  }
  return value
}

function str(value: unknown, field: string): string {
  if (typeof value !== "string" || value.length === 0 || value.length > 128) throw new SealedError("bad_response", field + " is not an id")
  return value
}

export function parseSealedList(body: unknown, nowMs: number): SealedList {
  const r = body as Record<string, unknown> | null
  if (!r || typeof r !== "object" || r.schema !== "clawdline.health-sealed-list/1" || !Array.isArray(r.attachments) ||
      r.attachments.length > SEALED_LIST_MAX || typeof r.truncated !== "boolean") {
    throw new SealedError("bad_response", "the sealed list is not clawdline.health-sealed-list/1")
  }
  const attachments = r.attachments.map((value: unknown): SealedAttachmentMeta => {
    const a = value as Record<string, unknown>
    if (!a || typeof a !== "object" || !Array.isArray(a.revisions) || a.revisions.length > 8) {
      throw new SealedError("bad_response", "a sealed attachment row is malformed")
    }
    if (a.state !== "live" && a.state !== "expired" && a.state !== "deleted") throw new SealedError("bad_response", "unknown attachment state")
    return {
      attachmentID: str(a.attachment_id, "attachment_id"), machineID: str(a.machine_id, "machine_id"),
      bootID: str(a.boot_id, "boot_id"), subjectKind: str(a.subject_kind, "subject_kind"), subjectID: str(a.subject_id, "subject_id"),
      openedAtMs: num(a.opened_at_ms, "opened_at_ms"), state: a.state, latestRevision: num(a.latest_revision, "latest_revision"),
      expiresAtMs: num(a.expires_at_ms, "expires_at_ms"),
      revisions: a.revisions.map((value: unknown): SealedRevisionMeta => {
        const v = value as Record<string, unknown>
        if (!v || typeof v !== "object" || typeof v.readable !== "boolean") throw new SealedError("bad_response", "a revision row is malformed")
        return {
          revision: num(v.revision, "revision"), ciphertextBytes: num(v.ciphertext_bytes, "ciphertext_bytes"),
          contentKeyEpoch: num(v.content_key_epoch, "content_key_epoch"), identityEpoch: num(v.identity_epoch, "identity_epoch"),
          receivedAtMs: num(v.received_at_ms, "received_at_ms"), expiresAtMs: num(v.expires_at_ms, "expires_at_ms"),
          readable: v.readable,
        }
      }),
    }
  })
  return { attachments, truncated: r.truncated, readAtMs: nowMs }
}

/** Cloud's `{error:{code}}` refusal as the typed error the panel branches on. */
export async function refusalOf(response: Response): Promise<SealedError> {
  let code = ""
  try {
    const body = await response.json() as { error?: { code?: unknown } }
    if (typeof body?.error?.code === "string") code = body.error.code
  } catch { /* a refusal without a body is still the status */ }
  const s = response.status
  const as = (kind: SealedErrorCode, message: string) => new SealedError(kind, message, s, code || null)
  if (s === 401) return as("not_signed_in", "Cloud does not know this browser as a viewer")
  if (s === 403) return code === "key_handover_required" ? as("key_handover_required", "this device has no key grant for that epoch")
    : as("forbidden", "Cloud refused this device's proof")
  if (s === 404) return as("unknown_attachment", "Cloud has no such attachment for this account")
  if (s === 409) return code === "identity_epoch_changed" ? as("stale", "the account identity epoch changed")
    : code === "key_handover_required" ? as("key_handover_required", "this device has no key grant for that epoch")
    : as("conflict", "Cloud refused the proof as replayed or out of date")
  if (s === 410) return code === "sealed_deleted" ? as("deleted", "the attachment was deleted") : as("expired", "the attachment has expired")
  if (s === 429) return as("busy", "Cloud is busy or the quota is used")
  return as("unavailable", "Cloud could not answer")
}

export class OwnerDiagnosticsStore {
  private generation = 0
  private accountID: string | null = null
  private identityEpoch: number | null = null
  private list: SealedList | null = null
  private opened = new Map<string, OpenedDiagnostic>()
  private listeners = new Set<() => void>()
  private readonly fetchImpl: Fetch
  private readonly now: () => number

  constructor(fetchImpl: Fetch = (input, init) => fetch(input, init), now: () => number = Date.now) {
    this.fetchImpl = fetchImpl
    this.now = now
  }

  watch(listener: () => void): () => void {
    this.listeners.add(listener)
    return () => { this.listeners.delete(listener) }
  }

  private changed(): void {
    for (const listener of [...this.listeners]) listener()
  }

  /** Drops every list row and every opened snapshot, and orphans anything in flight. */
  clear(_reason: ClearReason): void {
    this.generation++
    this.list = null
    this.opened.clear()
    this.changed()
  }

  /** Called with the current account and identity epoch; a change of either clears. */
  bind(accountID: string | null, identityEpoch: number | null): void {
    if (accountID !== this.accountID) {
      if (this.accountID !== null || this.list || this.opened.size) this.clear(accountID ? "account_changed" : "sign_out")
      this.accountID = accountID
    }
    if (identityEpoch !== this.identityEpoch) {
      if (this.identityEpoch !== null) this.clear("epoch_changed")
      this.identityEpoch = identityEpoch
    }
  }

  snapshotList(): SealedList | null { return this.list }
  openedFor(attachmentID: string, revision: number): OpenedDiagnostic | null {
    return this.opened.get(attachmentID + "#" + revision) ?? null
  }

  private guard(generation: number): void {
    if (generation !== this.generation) throw new SealedError("stale", "the store was cleared while this request ran")
  }

  private async call(source: OwnerDiagnosticsSource, path: string, init: RequestInit): Promise<unknown> {
    let response: Response
    try {
      response = await this.fetchImpl(source.apiOrigin + path, {
        ...init, credentials: "include", cache: "no-store", signal: AbortSignal.timeout(SEALED_REQUEST_TIMEOUT_MS),
      })
    } catch {
      throw new SealedError("unavailable", "Cloud could not be reached")
    }
    if (!response.ok) throw await refusalOf(response)
    try { return await response.json() } catch { throw new SealedError("bad_response", "Cloud's answer is not JSON") }
  }

  async refresh(source: OwnerDiagnosticsSource): Promise<SealedList> {
    const accountID = source.accountID()
    if (!accountID) throw new SealedError("not_signed_in", "this browser is not signed in to an account")
    this.bind(accountID, this.identityEpoch)
    const generation = this.generation
    const body = await this.call(source, SEALED_LIST_PATH, { method: "GET" })
    this.guard(generation)
    const list = parseSealedList(body, this.now())
    this.list = list
    this.changed()
    return list
  }

  /**
   * Opens one revision. The key is resolved before anything is asked of Cloud, so a browser
   * that cannot decrypt never triggers an audited read of bytes it would throw away.
   */
  async open(source: OwnerDiagnosticsSource, machineID: string, attachmentID: string, revision: number, contentKeyEpoch: number): Promise<OpenedDiagnostic> {
    const generation = this.generation
    const key = await sealedKeyFor(source, machineID, contentKeyEpoch)
    const who = proofIdentityOf(source)
    this.guard(generation)
    const proof = await signProof("read", who, attachmentID, revision, this.now())
    const body = await this.call(source, SEALED_READ_PATH, {
      method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(proof),
    })
    this.guard(generation)
    const opened = await openSealed(body, { accountID: who.accountID, machineID, attachmentID, revision, nowMs: this.now() }, key)
    this.guard(generation)
    const snapshot = decodeSnapshot(opened.plaintext)
    const result: OpenedDiagnostic = {
      attachmentID, machineID, revision, bootID: opened.aad.boot_id, subjectKind: opened.aad.subject_kind,
      subjectID: opened.aad.subject_id, contentKeyEpoch: opened.aad.content_key_epoch,
      expiresAtMs: opened.aad.expires_at_ms, receivedAtMs: opened.receivedAtMs, snapshot,
    }
    if (this.opened.size >= SEALED_OPENED_MAX) this.opened.delete(this.opened.keys().next().value as string)
    this.opened.set(attachmentID + "#" + revision, result)
    this.changed()
    return result
  }
}

/** The one store the console uses; the gate clears it whenever its authority changes. */
export const ownerDiagnostics = new OwnerDiagnosticsStore()
