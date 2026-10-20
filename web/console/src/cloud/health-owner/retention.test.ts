import assert from "node:assert/strict"
import test from "node:test"

// @ts-expect-error -- Node's strip-types tests import this module directly.
import { createRetentionPoller, OWNER_RETENTION_LIMITS, retentionLines, senderReading, viewLines } from "./retention.ts"

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

// The block's reads (createRetentionPoller), driven by a hand clock so a hung daemon, an
// overlapping tick and an unmount mid-read can each be placed exactly.

function clock() {
  let now = 0
  let seq = 0
  const due = new Map<number, { at: number; fn: () => void; every?: number }>()
  return {
    timers: {
      setTimeout: (fn: () => void, ms: number) => { due.set(++seq, { at: now + ms, fn }); return seq },
      clearTimeout: (h: unknown) => { due.delete(h as number) },
      setInterval: (fn: () => void, ms: number) => { due.set(++seq, { at: now + ms, fn, every: ms }); return seq },
      clearInterval: (h: unknown) => { due.delete(h as number) },
    },
    async advance(ms: number) {
      const end = now + ms
      for (;;) {
        await flush()
        let next: [number, { at: number; fn: () => void; every?: number }] | undefined
        for (const entry of due) if (entry[1].at <= end && (!next || entry[1].at < next[1].at)) next = entry
        if (!next) break
        now = next[1].at
        if (next[1].every) next[1].at += next[1].every
        else due.delete(next[0])
        next[1].fn()
      }
      now = end
      await flush()
    },
  }
}
const flush = async () => { for (let i = 0; i < 20; i++) await Promise.resolve() }

interface Pending<T> { signal: AbortSignal; resolve(v: T): void; reject(e: unknown): void }
function deferredReaders(options: { honourAbort?: boolean } = {}) {
  const settings: Pending<{ cloud_health_owner_retention?: boolean }>[] = []
  const sender: Pending<ReturnType<typeof senderReading>>[] = []
  const make = <T,>(list: Pending<T>[]) => (signal: AbortSignal) => new Promise<T>((resolve, reject) => {
    list.push({ signal, resolve, reject })
    if (options.honourAbort !== false) signal.addEventListener("abort", () => reject(new DOMException("aborted", "AbortError")))
  })
  return { settings, sender, readers: { settings: make(settings), sender: make(sender) } }
}
const active = senderReading(status(owner({ coverage: "active", stored: 2 })))

test("the block's two bounds are five seconds each", () => {
  assert.deepEqual({ ...OWNER_RETENTION_LIMITS }, { pollMs: 5_000, requestMs: 5_000 })
})

test("fault injection: a hung settings read is unknown at the deadline and is not stacked by later ticks", async () => {
  const c = clock()
  const r = deferredReaders({ honourAbort: false })
  const views: unknown[] = []
  const p = createRetentionPoller(r.readers, (v) => views.push(v), { timers: c.timers })
  p.start()
  await flush()
  r.sender[0].resolve(active)
  await c.advance(5_000)
  assert.deepEqual(views.at(-1), { setting: { kind: "unknown", why: "timeout" }, sender: active })
  assert.equal(r.settings[0].signal.aborted, true)
  // The settings read ignores its abort; while it is still out no tick starts a second one.
  await c.advance(60_000)
  assert.equal(r.settings.length, 1)
  assert.equal(r.sender.length, 1)
  // When it finally answers it is obsolete (aborted) and the next tick reads afresh.
  r.settings[0].resolve({ cloud_health_owner_retention: true })
  await c.advance(5_000)
  assert.equal(r.settings.length, 2)
  assert.deepEqual(views.at(-1), { setting: { kind: "unknown", why: "timeout" }, sender: active })
  r.settings[1].resolve({ cloud_health_owner_retention: true })
  r.sender[1].resolve(active)
  await flush()
  assert.deepEqual(views.at(-1), { setting: { kind: "known", on: true }, sender: active })
  p.stop()
})

test("fault injection: an unreachable network is unknown, and a failed refresh does not keep the last value", async () => {
  const c = clock()
  const r = deferredReaders()
  const views: { setting: unknown; sender: unknown }[] = []
  const p = createRetentionPoller(r.readers, (v) => views.push(v), { timers: c.timers })
  p.start()
  await flush()
  r.settings[0].resolve({ cloud_health_owner_retention: true })
  r.sender[0].resolve(active)
  await flush()
  assert.deepEqual(views.at(-1)?.setting, { kind: "known", on: true })
  await c.advance(5_000)
  r.settings[1].reject(new TypeError("Failed to fetch"))
  r.sender[1].reject(new TypeError("Failed to fetch"))
  await flush()
  assert.deepEqual(views.at(-1), { setting: { kind: "unknown", why: "unreachable" }, sender: { kind: "unknown", why: "unreachable" } })
  // The lines say the switch is unknown; they do not describe an "on" that may no longer hold.
  assert.deepEqual(words(viewLines(views.at(-1) as never)), ["ownerRetentionSettingUnknown", "ownerRetentionUnknown"])
  p.stop()
})

test("overlapping ticks: a slow read is never doubled, and an explicit refresh waits for the slot", async () => {
  const c = clock()
  const r = deferredReaders()
  const p = createRetentionPoller(r.readers, () => {}, { timers: c.timers, requestMs: 60_000 })
  p.start()
  await c.advance(20_000)
  assert.equal(r.settings.length, 1)
  p.refresh()
  p.refresh()
  await flush()
  assert.equal(r.settings.length, 1)
  r.settings[0].resolve({})
  r.sender[0].resolve(active)
  await flush()
  // The two refresh requests coalesced into one read, started once the slot was free.
  assert.equal(r.settings.length, 2)
  p.stop()
})

test("unmount: the read in flight is aborted and its late answer is never shown", async () => {
  const c = clock()
  const r = deferredReaders({ honourAbort: false })
  let shown = 0
  const p = createRetentionPoller(r.readers, () => { shown++ }, { timers: c.timers })
  p.start()
  await flush()
  p.stop()
  assert.equal(r.settings[0].signal.aborted, true)
  r.settings[0].resolve({ cloud_health_owner_retention: true })
  r.sender[0].resolve(active)
  await c.advance(60_000)
  assert.equal(shown, 0)
  assert.equal(r.settings.length, 1)
})

test("switch pending: a read from before the write cannot overwrite the written value", async () => {
  const c = clock()
  const r = deferredReaders({ honourAbort: false })
  const views: { setting: unknown }[] = []
  const p = createRetentionPoller(r.readers, (v) => views.push(v), { timers: c.timers })
  p.start()
  await flush()
  r.settings[0].resolve({ cloud_health_owner_retention: false })
  r.sender[0].resolve(active)
  await c.advance(5_000)
  // A tick's read is out when the person flips the switch.
  assert.equal(r.settings.length, 2)
  p.beginWrite()
  assert.equal(r.settings[1].signal.aborted, true)
  await c.advance(15_000)
  assert.equal(r.settings.length, 2, "no tick reads while the write is out")
  p.endWrite(true)
  assert.deepEqual(views.at(-1)?.setting, { kind: "known", on: true })
  // The old read now answers with the pre-write value: obsolete, not shown.
  r.settings[1].resolve({ cloud_health_owner_retention: false })
  r.sender[1].resolve(active)
  await flush()
  assert.deepEqual(views.at(-1)?.setting, { kind: "known", on: true })
  // Then the re-read the write asked for runs.
  assert.equal(r.settings.length, 3)
  r.settings[2].resolve({ cloud_health_owner_retention: true })
  r.sender[2].resolve(active)
  await flush()
  assert.deepEqual(views.at(-1)?.setting, { kind: "known", on: true })
  p.stop()
})

test("a failed write leaves the switch unknown until a read answers", async () => {
  const c = clock()
  const r = deferredReaders()
  const views: { setting: unknown }[] = []
  const p = createRetentionPoller(r.readers, (v) => views.push(v), { timers: c.timers })
  p.start()
  await flush()
  r.settings[0].resolve({ cloud_health_owner_retention: false })
  r.sender[0].resolve(active)
  await flush()
  p.beginWrite()
  p.endWrite(null, "write_unconfirmed")
  assert.deepEqual(views.at(-1)?.setting, { kind: "unknown", why: "write_unconfirmed" })
  await flush()
  r.settings[1].resolve({})
  r.sender[1].resolve(active)
  await flush()
  // Absent in the file is off: consent stays default-off.
  assert.deepEqual(views.at(-1)?.setting, { kind: "known", on: false })
  p.stop()
})
