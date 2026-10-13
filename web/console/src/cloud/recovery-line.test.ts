import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node's strip-types runner loads this source in its suite.
import { keepConnectedRecovering } from "./recovery-line.ts"
import type { CloudConnection, CloudSession, CloudUpdate } from "./copied.js"
// @ts-expect-error -- Node's strip-types runner loads these sources in its suite.
import { PendingSends } from "../session/pending.ts"
// @ts-expect-error -- Node's strip-types runner loads this source in its suite.
import { Sender } from "../session/sender.ts"
// @ts-expect-error -- Node's strip-types runner loads this source in its suite.
import { outcomeOf } from "../session/outcome.ts"

function harness() {
  let at = 1_000_000
  let nextTimer = 1
  const timers = new Map<number, { due: number; run: () => void }>()
  const lines: { options: { onState: (update: CloudUpdate) => void;
    maximumConnectFailures?: number; initialBackoffMs?: number; maximumBackoffMs?: number };
    stopped: boolean }[] = []
  const updates: CloudUpdate[] = []
  const connection = keepConnectedRecovering({} as CloudSession, { onState: (update) => updates.push(update) }, {
    connect: (_session, options) => {
      const line = { options, stopped: false }
      lines.push(line)
      let finish!: () => void
      const done = new Promise<void>((resolve) => { finish = resolve })
      return { stop() { line.stopped = true; finish() }, done } satisfies CloudConnection
    },
    now: () => at,
    setTimeout: ((run: () => void, ms: number) => {
      const id = nextTimer++
      timers.set(id, { due: at + ms, run })
      return id as unknown as ReturnType<typeof setTimeout>
    }) as typeof setTimeout,
    clearTimeout: ((id: ReturnType<typeof setTimeout>) => {
      timers.delete(id as unknown as number)
    }) as typeof clearTimeout,
  })
  return {
    connection, lines, updates,
    emit(index: number, update: CloudUpdate) { lines[index]!.options.onState(update) },
    async advance(ms: number) {
      const end = at + ms
      for (;;) {
        const next = [...timers].filter(([, timer]) => timer.due <= end)
          .sort((a, b) => a[1].due - b[1].due)[0]
        if (!next) break
        timers.delete(next[0])
        at = next[1].due
        next[1].run()
        await Promise.resolve()
        await Promise.resolve()
      }
      at = end
    },
  }
}

const exhausted = (code = "offline"): CloudUpdate => ({ state: "terminal_error",
  reason: "retries_exhausted", error: Object.assign(new Error(code), { code }) })

test("the hosted line keeps one background probe per ceiling after repeated failures", async () => {
  const h = harness()
  h.emit(0, exhausted())
  assert.deepEqual(h.updates.map((update) => update.state), ["retrying"])
  assert.equal(h.updates[0]?.state === "retrying" && h.updates[0].afterMs, 30_000)
  await h.advance(29_999)
  assert.equal(h.lines.length, 1)
  await h.advance(1)
  assert.equal(h.lines.length, 2)
  assert.equal(h.lines[0]?.stopped, true)
  assert.equal(h.lines[1]?.options.maximumConnectFailures, 1, "a probe cannot restart a burst")
  assert.equal(h.lines[1]?.options.initialBackoffMs, 30_000)
  h.emit(1, exhausted())
  await h.advance(29_999)
  assert.equal(h.lines.length, 2)
  await h.advance(1)
  assert.equal(h.lines.length, 3)
  h.emit(2, { state: "connected", client: {} as never })
  await h.advance(60_000)
  assert.equal(h.lines.length, 3, "a recovered line has no probe timer")
  h.connection.stop()
})

test("a message retry brings a parked attempt forward but keeps minimum spacing", async () => {
  const h = harness()
  h.emit(0, exhausted())
  h.connection.retry()
  await h.advance(1_999)
  assert.equal(h.lines.length, 1)
  await h.advance(1)
  assert.equal(h.lines.length, 2)
  await h.advance(30_000)
  assert.equal(h.lines.length, 2, "the old background timer was canceled")
  h.connection.stop()
})

test("a retained message waits for recovery, checks the transcript, and posts once", async () => {
  const h = harness()
  const cards = new PendingSends()
  const card = cards.add("session", "a saved message", [], 1_000_000)
  cards.failed(card.token, "cloud_reconnecting")
  let answerRead!: (turns: []) => void
  const read = new Promise<[]>(resolve => { answerRead = resolve })
  let posts = 0
  const sender = new Sender({
    cards, now: () => 1_002_000, outcomeOf,
    wakeConnection: () => h.connection.retry(),
    readBack: async () => read,
    post: async () => { posts++; return { ok: true } },
  })
  h.emit(0, exhausted())
  const sending = sender.resend(card.token)
  assert.equal(posts, 0)
  await h.advance(2_000)
  assert.equal(h.lines.length, 2, "the person's press starts a new Cloud attempt")
  h.emit(1, { state: "connected", client: {} as never })
  answerRead([])
  await sending
  assert.equal(posts, 1)
  assert.equal(cards.card(card.token)?.state, "accepted")
  h.connection.stop()
})

test("repeated forbidden refusals do not start background probes", async () => {
  const h = harness()
  h.emit(0, exhausted("forbidden"))
  assert.equal(h.updates[0]?.state, "terminal_error")
  await h.advance(120_000)
  assert.equal(h.lines.length, 1)
  h.connection.stop()
})
