/*
 * The identity handover as the hosted page runs it: a machine's `#pairid=` link opens this page,
 * the person compares six digits with the machine's own screen, and only a confirm the machine
 * signed leaves keys behind (`identity-pair.ts`). This file decides where those keys live and
 * when a run must stop.
 *
 * - Kept: one IndexedDB record per account and machine, written in one put after the confirm
 *   verified: the master secret as non-extractable HKDF and AES-GCM keys, and its content-key
 *   epoch. Nothing is extracted, nothing is guessed: a machine with no record answers
 *   `{ key: null, contentKeyEpoch: null }`, and the reader says the browser must be paired again.
 * - A browser paired before this handover existed holds the master secret only as an AES-GCM
 *   key with no epoch. That key is never read here; such a browser needs this handover once.
 * - Stopped: a sign-out, an account switch or a revocation calls `stop()`. A run in flight then
 *   fails `stale` at its next await, an unanswered code comparison is answered "no", and a record
 *   written after the stop is deleted again.
 */
// @ts-expect-error -- Node's strip-types tests import this module directly.
import { IdentityPairingError, pairThisViewer, type IdentityViewerResult } from "./identity-pair.ts"
import { deleteHealthHandovers, loadHealthHandover, storeHealthHandover } from "../../legacy/js/net/cloud-crypto.js"
import type { HeldMasterKey } from "./source.js"

export const IDENTITY_FRAGMENT = "#pairid="
const PENDING_KEY = "clawdline.identity-pairing"
const RECORD_PREFIX = "clawdline.health-handover:"

export function handoverPrefix(account: string): string {
  return RECORD_PREFIX + account + ":"
}

export function handoverName(account: string, machine: string): string {
  return handoverPrefix(account) + encodeURIComponent(machine)
}

/** What one machine's handover left in this browser. */
export interface HandoverRecord {
  v: 1
  accountID: string
  machineID: string
  machineFingerprint: string
  contentKeyEpoch: number
  identityEpoch: number
  pairedAtMs: number
  hkdfKey: CryptoKey
  masterKey: CryptoKey
}

function validRecord(value: unknown, account: string, machine: string): value is HandoverRecord {
  const r = value as HandoverRecord | null
  return !!r && r.v === 1 && r.accountID === account && r.machineID === machine &&
    Number.isSafeInteger(r.contentKeyEpoch) && r.contentKeyEpoch >= 1 &&
    typeof CryptoKey !== "undefined" && r.hkdfKey instanceof CryptoKey && r.hkdfKey.algorithm?.name === "HKDF" &&
    r.hkdfKey.extractable === false && r.masterKey instanceof CryptoKey && r.masterKey.extractable === false
}

export async function saveHandover(result: IdentityViewerResult, nowMs: number, idb?: IDBFactory): Promise<HandoverRecord> {
  const record: HandoverRecord = {
    v: 1, accountID: result.accountID, machineID: result.machineID, machineFingerprint: result.machineFingerprint,
    contentKeyEpoch: result.contentKeyEpoch, identityEpoch: result.identityEpoch, pairedAtMs: nowMs,
    hkdfKey: result.hkdfKey, masterKey: result.masterKey,
  }
  if (!validRecord(record, result.accountID, result.machineID)) throw new IdentityPairingError("bad_result", "the handover result is not storable")
  await storeHealthHandover(handoverName(result.accountID, result.machineID), record, idb)
  return record
}

/** The record for one machine, or null. A record that does not validate is unknown, not a key. */
export async function loadHandover(account: string, machine: string, idb?: IDBFactory): Promise<HandoverRecord | null> {
  const value = await loadHealthHandover(handoverName(account, machine), idb)
  return validRecord(value, account, machine) ? value : null
}

export function forgetHandovers(account: string, idb?: IDBFactory): Promise<void> {
  return deleteHealthHandovers(handoverPrefix(account), idb)
}

/** `heldMasterKey` for the owner-diagnostics source. A read failure throws; it is not "no key". */
export async function heldMasterKeyFor(account: string | null, machine: string, idb?: IDBFactory): Promise<HeldMasterKey> {
  if (!account) return { key: null, contentKeyEpoch: null }
  const record = await loadHandover(account, machine, idb)
  return record ? { key: record.hkdfKey, contentKeyEpoch: record.contentKeyEpoch } : { key: null, contentKeyEpoch: null }
}

// MARK: the link

interface FragmentScope {
  location: { hash: string; pathname: string; search: string }
  history: { state: unknown; replaceState(state: unknown, unused: string, url: string): void }
  sessionStorage: { getItem(key: string): string | null; setItem(key: string, value: string): void; removeItem(key: string): void }
}

export function identityPairingInHash(hash: string): string | null {
  if (typeof hash !== "string" || hash.indexOf(IDENTITY_FRAGMENT) !== 0) return null
  const raw = hash.slice(IDENTITY_FRAGMENT.length)
  return raw && raw.length <= 4096 ? raw : null
}

/**
 * The `#pairid=` this page was opened with, taken out of the address at once and kept only in
 * this tab's session storage until a run takes it, as `takeInvitation` keeps `#pair=`.
 */
export function takeIdentityPairing(scope: FragmentScope): string | null {
  const raw = identityPairingInHash(scope.location.hash)
  if (raw) {
    try { scope.sessionStorage.setItem(PENDING_KEY, raw) } catch { /* answered now regardless */ }
    try { scope.history.replaceState(scope.history.state, "", scope.location.pathname + scope.location.search) } catch { /* kept in the address */ }
    return raw
  }
  try { return scope.sessionStorage.getItem(PENDING_KEY) || null } catch { return null }
}

export function dropPendingIdentityPairing(scope: Pick<FragmentScope, "sessionStorage">): void {
  try { scope.sessionStorage.removeItem(PENDING_KEY) } catch { /* nothing kept */ }
}

// MARK: the run

export type HandoverState =
  | { phase: "idle" }
  | { phase: "comparing"; code: string; machineFingerprint: string }
  | { phase: "working" }
  | { phase: "done"; machineID: string; contentKeyEpoch: number }
  | { phase: "failed"; code: string }

export interface HandoverContext {
  apiOrigin: string
  fetch: (input: string, init: RequestInit) => Promise<Response>
  accountID: string
  deviceID: string
  signingPublicKey: Uint8Array
  /** The account the page is signed in to now; a different answer stops the run. */
  currentAccount(): string | null
  indexedDB?: IDBFactory
  now?: () => number
  sleep?: (ms: number) => Promise<void>
}

export class HandoverController {
  private current: HandoverState = { phase: "idle" }
  private generation = 0
  private answer: ((ok: boolean) => void) | null = null
  private readonly listeners = new Set<() => void>()

  state(): HandoverState { return this.current }

  watch(listener: () => void): () => void {
    this.listeners.add(listener)
    return () => { this.listeners.delete(listener) }
  }

  /** The person's answer to the code comparison. */
  confirm(ok: boolean): void {
    const answer = this.answer
    this.answer = null
    if (ok && answer) this.set({ phase: "working" })
    answer?.(ok)
  }

  /** Sign-out, account switch, revocation, or the person closing the dialog. */
  stop(): void {
    this.generation++
    const answer = this.answer
    this.answer = null
    answer?.(false)
    if (this.current.phase !== "idle") this.set({ phase: "idle" })
  }

  /** Run one handover from a `#pairid=` fragment. Resolves to the final state; never throws. */
  async run(fragment: string, ctx: HandoverContext): Promise<HandoverState> {
    this.stop()
    const generation = ++this.generation
    const isCurrent = () => generation === this.generation && ctx.currentAccount() === ctx.accountID
    const now = ctx.now ?? (() => Date.now())
    this.set({ phase: "working" })
    try {
      await pairThisViewer(fragment, {
        apiOrigin: ctx.apiOrigin, fetch: ctx.fetch, accountID: ctx.accountID, deviceID: ctx.deviceID,
        signingPublicKey: ctx.signingPublicKey, isCurrent, now,
        sleep: ctx.sleep ?? ((ms: number) => new Promise((resolve) => setTimeout(resolve, ms))),
        confirmCode: (code: string, machineFingerprint: string) => new Promise<boolean>((resolve) => {
          if (!isCurrent()) return resolve(false)
          this.answer = resolve
          this.set({ phase: "comparing", code, machineFingerprint })
        }),
        persist: async (result: IdentityViewerResult) => {
          await saveHandover(result, now(), ctx.indexedDB)
          if (!isCurrent()) {
            // Stopped while the write ran: the keys must not outlive the stop.
            await forgetHandovers(result.accountID, ctx.indexedDB).catch(() => undefined)
            throw new IdentityPairingError("stale", "the account changed while the keys were stored")
          }
          if (generation === this.generation) this.set({ phase: "done", machineID: result.machineID, contentKeyEpoch: result.contentKeyEpoch })
        },
      })
    } catch (error) {
      const code = error instanceof IdentityPairingError ? error.code : "failed"
      if (generation === this.generation) this.set(code === "stale" ? { phase: "idle" } : { phase: "failed", code })
    }
    return this.current
  }

  private set(state: HandoverState): void {
    this.current = state
    for (const listener of [...this.listeners]) listener()
  }
}

export const handover = new HandoverController()
