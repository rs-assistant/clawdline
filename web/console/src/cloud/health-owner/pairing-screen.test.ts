import assert from "node:assert/strict"
import test from "node:test"

// @ts-expect-error -- Node's strip-types tests import this module directly.
import { HandoverController, forgetHandovers, handoverDoneWord, handoverSigner, heldMasterKeyFor, watchIdentityPairing } from "./handover.ts"
// @ts-expect-error -- Node's strip-types tests import this module directly.
import { IdentityPairingError } from "./identity-pair.ts"

// A browser that signed in but never took a legacy `#pair=` link sits on the pairing screen. It
// already has a registered device and its signing key, so a machine's `#pairid=` link runs there
// too. The handover leaves a diagnostics key only: nothing the relay reads, and the finished sheet
// says what reading Sessions still needs.


// An IndexedDB with one object store, kept in a Map outside every "page", so a second controller
// and a second open read what the first wrote: the reload seam. Values are kept by reference, as
// a structured clone of a CryptoKey keeps it non-extractable.
function memoryIDB(rows = new Map<string, unknown>()) {
  ;(globalThis as Record<string, unknown>).IDBKeyRange ??= { bound: (lower: string, upper: string) => ({ lower, upper }) }
  const later = (fn: () => void) => setTimeout(fn, 0)
  const factory = {
    rows,
    open() {
      const db = {
        objectStoreNames: { contains: () => true },
        createObjectStore() {},
        close() {},
        transaction() {
          const tx: Record<string, any> = {}
          const store = {
            put(value: unknown, key: string) { rows.set(key, value) },
            get(key: string) {
              const request: Record<string, any> = {}
              later(() => { request.result = rows.get(key); request.onsuccess?.() })
              return request
            },
            delete(range: { lower: string; upper: string }) {
              for (const key of [...rows.keys()]) if (key >= range.lower && key <= range.upper) rows.delete(key)
            },
          }
          tx.objectStore = () => store
          later(() => tx.oncomplete?.())
          return tx
        },
      }
      const request: Record<string, any> = { result: db }
      later(() => request.onsuccess?.())
      return request
    },
  }
  return factory as unknown as IDBFactory & { rows: Map<string, unknown> }
}

async function keys() {
  const raw = crypto.getRandomValues(new Uint8Array(32))
  return {
    hkdfKey: await crypto.subtle.importKey("raw", raw, "HKDF", false, ["deriveKey", "deriveBits"]),
    masterKey: await crypto.subtle.importKey("raw", raw, "AES-GCM", false, ["encrypt", "decrypt"]),
  }
}

function context(idb: IDBFactory, over: Record<string, unknown> = {}) {
  return {
    apiOrigin: "https://api.test", fetch: async () => new Response("{}"), accountID: "usr_a", deviceID: "wd_1",
    signingPublicKey: new Uint8Array(32), currentAccount: () => "usr_a", indexedDB: idb, ...over,
  }
}

/** A pairing run that asks for the code, then persists what a verified confirm would. */
function fakePair(epoch = 1, account = "usr_a") {
  return async (_fragment: string, deps: any) => {
    if (!await deps.confirmCode("123456", "MACH-FP")) throw new IdentityPairingError("not_confirmed")
    if (!deps.isCurrent()) throw new IdentityPairingError("stale")
    const result = { accountID: account, machineID: "mac_1", machineFingerprint: "MACH-FP", contentKeyEpoch: epoch, identityEpoch: 0, ...await keys() }
    await deps.persist(result)
    return result
  }
}

async function until(controller: HandoverController, phase: string) {
  for (let i = 0; i < 200 && controller.state().phase !== phase; i++) await new Promise((r) => setTimeout(r, 1))
  assert.equal(controller.state().phase, phase)
}


/** The address, history, session storage and hashchange of one tab. */
function tab(hash: string) {
  const store = new Map<string, string>()
  const listeners = new Set<() => void>()
  const scope = {
    location: { hash, pathname: "/", search: "" },
    history: { state: null, replaceState(_s: unknown, _u: string, url: string) { scope.location.hash = url.includes("#") ? url.slice(url.indexOf("#")) : "" } },
    sessionStorage: { getItem: (k: string) => store.get(k) ?? null, setItem: (k: string, v: string) => { store.set(k, v) }, removeItem: (k: string) => { store.delete(k) } },
    addEventListener: (_t: string, l: () => void) => { listeners.add(l) },
    removeEventListener: (_t: string, l: () => void) => { listeners.delete(l) },
    navigate(next: string) { scope.location.hash = next; for (const l of [...listeners]) l() },
    store, listeners,
  }
  return scope
}

/** The pairing screen's session: signed in, a device and its key, and no relay pairing at all. */
const fresh = (over: Record<string, unknown> = {}) => ({ account: "usr_a", deviceID: "wd_1", devicePublicKey: new Uint8Array(32).fill(3), ...over })

function watch(scope: ReturnType<typeof tab>, idb: IDBFactory, session: () => Record<string, unknown> | null, controller: HandoverController, pair = fakePair()) {
  return watchIdentityPairing(scope, { apiOrigin: "https://api.test", account: "usr_a", session, fetch: async () => new Response("{}"),
    controller, indexedDB: idb, pair })
}

test("a fresh device on the pairing screen takes the link, and keeps only a diagnostics key", async () => {
  const idb = memoryIDB()
  const scope = tab("#pairid=QR")
  const controller = new HandoverController()
  const stop = watch(scope, idb, () => fresh(), controller)
  assert.equal(scope.location.hash, "", "the link stayed in the address")
  await until(controller, "comparing")
  controller.confirm(true)
  await until(controller, "done")
  const held = await heldMasterKeyFor("usr_a", "mac_1", idb)
  assert.equal(held.contentKeyEpoch, 1)
  assert.equal(held.key?.extractable, false)
  // Nothing the relay reads: no account master key, no machine pairing, no sender key.
  assert.deepEqual([...idb.rows.keys()], ["clawdline.health-handover:usr_a:mac_1"])
  assert.equal(handoverDoneWord("pairing_required"), "handoverDoneRelayNeeded")
  assert.equal(handoverDoneWord("connected"), "handoverDone")
  stop()
})

test("a session with no device key, or another account, runs nothing and keeps the link for later", async () => {
  const idb = memoryIDB()
  const scope = tab("#pairid=QR")
  const controller = new HandoverController()
  let session: Record<string, unknown> = fresh({ devicePublicKey: null })
  const stop = watch(scope, idb, () => session, controller)
  assert.equal(controller.state().phase, "idle")
  assert.equal(scope.location.hash, "#pairid=QR", "a link nobody can run yet was taken out of the address")
  assert.equal(handoverSigner(fresh({ account: "usr_b" }), "usr_a"), null)
  assert.equal(handoverSigner(fresh({ deviceID: "" }), "usr_a"), null)
  assert.equal(handoverSigner(null, "usr_a"), null)
  // The device key arrives (sign-in finished); the next hashchange runs the same link.
  session = fresh()
  scope.navigate("#pairid=QR")
  await until(controller, "comparing")
  stop()
  assert.equal(controller.state().phase, "idle")
  assert.equal(idb.rows.size, 0)
})

test("fault injection: the person saying the codes differ keeps nothing", async () => {
  const idb = memoryIDB()
  const controller = new HandoverController()
  const stop = watch(tab("#pairid=QR"), idb, () => fresh(), controller)
  await until(controller, "comparing")
  controller.confirm(false)
  await until(controller, "failed")
  assert.equal((controller.state() as { code: string }).code, "not_confirmed")
  assert.equal(idb.rows.size, 0)
  stop()
})

test("fault injection: signing out during the comparison stops the run and keeps nothing", async () => {
  const idb = memoryIDB()
  const controller = new HandoverController()
  const stop = watch(tab("#pairid=QR"), idb, () => fresh(), controller)
  await until(controller, "comparing")
  stop()
  await new Promise((r) => setTimeout(r, 5))
  assert.equal(controller.state().phase, "idle")
  assert.equal(idb.rows.size, 0)
})

test("fault injection: the account changing while the keys are written leaves none behind", async () => {
  const idb = memoryIDB()
  const controller = new HandoverController()
  let session = fresh()
  const stop = watch(tab("#pairid=QR"), idb, () => session, controller)
  await until(controller, "comparing")
  session = fresh({ account: "usr_b" })
  controller.confirm(true)
  await new Promise((r) => setTimeout(r, 20))
  assert.equal(idb.rows.size, 0)
  assert.notEqual(controller.state().phase, "done")
  stop()
})

test("a reload reads the key back; an epoch change replaces it; revocation deletes it", async () => {
  const rows = new Map<string, unknown>()
  const first = new HandoverController()
  const stop1 = watch(tab("#pairid=QR"), memoryIDB(rows), () => fresh(), first)
  await until(first, "comparing")
  first.confirm(true)
  await until(first, "done")
  stop1()
  // Reload: a new page, the same store.
  assert.equal((await heldMasterKeyFor("usr_a", "mac_1", memoryIDB(rows))).contentKeyEpoch, 1)
  // The identity epoch rotated and the machine handed over again under content-key epoch 2.
  const second = new HandoverController()
  const stop2 = watch(tab("#pairid=QR2"), memoryIDB(rows), () => fresh(), second, fakePair(2))
  await until(second, "comparing")
  second.confirm(true)
  await until(second, "done")
  stop2()
  assert.equal((await heldMasterKeyFor("usr_a", "mac_1", memoryIDB(rows))).contentKeyEpoch, 2)
  await forgetHandovers("usr_a", memoryIDB(rows))
  assert.deepEqual(await heldMasterKeyFor("usr_a", "mac_1", memoryIDB(rows)), { key: null, contentKeyEpoch: null })
})
