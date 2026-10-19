// Local transport benchmark, deliberately outside the ordinary *.test.ts suite.
// Run through tools/heavy.sh; see the task artifact rerun.sh. No browser or relay.
import assert from "node:assert/strict"
import { spawn } from "node:child_process"
import { readFile, writeFile, mkdir, rm } from "node:fs/promises"
import { join } from "node:path"
import { setTimeout as delay } from "node:timers/promises"
// @ts-expect-error -- Node loads TypeScript source directly.
import { RelayReader, type CloudIdentity, type CloudReadClient, type CloudRow } from "./relay-reader.ts"
// @ts-expect-error -- Node loads TypeScript source directly.
import { RelayWriter, writeRoute } from "./relay-writer.ts"
import { failureFromMac } from "../legacy/js/net/cloud-failure.js"

type Fixture = { version: number; generation: string; session: string }
type Ask = { client: string; seq: number; word: string; fields: Record<string, unknown>; ms: number;
  requestBytes: number; httpBodyBytes: number; answerPayloadBytes: number; status: number; code: string }
type Page = { id?: string; signature?: string; entries?: { text?: string }[]; nextBefore?: number; error?: unknown }
type Observation = { round: number; arm: string; clients: number; callers: number; phase: string;
  callerFetch: number; inventoryReplayReads: number; clientAsk: number; machineRoute: number;
  requestBytes: number; answerPayloadBytes: number; httpBodyBytes: number; callerBodyBytes: number;
  elapsedMs: number; callerMs: number[]; nodeCPUus: number; routes: unknown[]; asks: Ask[];
  status: number[]; signatures: (string | undefined)[]; entryCounts: number[]; correct: boolean }

const out = process.env.CLAWDLINE_READ_BENCH_OUT
const binary = process.env.CLAWDLINE_READ_BENCH_BINARY
const work = process.env.CLAWDLINE_READ_BENCH_WORK
if (!out || !binary || !work) throw new Error("Set CLAWDLINE_READ_BENCH_OUT, _BINARY and _WORK to task-owned paths")
await mkdir(out, { recursive: true })
await mkdir(work, { recursive: true })
const ready = join(work, "ready")
await rm(ready, { force: true })
const state = join(work, "empty-state")
await mkdir(state, { recursive: true })
const child = spawn(binary, ["-test.run=^TestServeCloudReadEfficiency$", "-test.v", "-test.timeout=6m"], {
  env: { ...process.env, CLAWDLINE_READ_BENCH_READY: ready, CLAWDLINE_NEXT_DIR: state, TMPDIR: work },
  stdio: ["ignore", "pipe", "pipe"],
})
let serverOutput = ""
child.stdout?.on("data", (b) => { serverOutput += b.toString() })
child.stderr?.on("data", (b) => { serverOutput += b.toString() })
const exited = new Promise<number | null>((resolve) => child.on("exit", resolve))
let base = ""
const asks: Ask[] = []
let replayReads = 0
let abortOnAsk: AbortController | null = null
let now = 1_000_000
let fixture: Fixture
const target = () => ({ machineID: "mac_a", sessionID: fixture.session, executionGeneration: fixture.generation })

class ReadBenchClient implements CloudReadClient {
  ready = true
  allowWrites = true
  macCapabilities = new Set<string>()
  sessionInventoryByMachine = new Map<string, unknown>()
  seq = 0
  returnGate: Promise<void> | null = null
  payloadReady: (() => void) | null = null
  readonly id: string
  constructor(id: string) { this.id = id }
  events() { return () => {} }
  machineDescriptor() { return { machine: { commands: ["transcript", "send", "focus", "session-receipt"] } } }
  async sessions() {
    replayReads++
    const row: CloudRow = { id: fixture.session, machine: "mac_a", session: fixture.session,
      identity: { machine: "mac_a", session: fixture.session }, execution_generation: fixture.generation,
      state: "idle", activity: { at: fixture.version, known: true } }
    return { sessions: [row], at: 100, scan: { emptyAuthoritative: true, recovering: [], failures: [] } }
  }
  async request(identity: CloudIdentity, word: string, fields: Record<string, unknown>, signal?: AbortSignal) {
    const seq = ++this.seq
    const read = word === "transcript"
    const body = { type: word, session: identity.session, ...fields,
      ...(read ? { machine_id: identity.machine, expected_generation: fixture.generation } : {}) }
    const request = JSON.stringify({ sender: "web_" + this.id, seq, rail: read ? "r" : "ctl", body })
    const started = performance.now()
    const pending = fetch(base + "/command", { method: "POST", body: request, signal })
    abortOnAsk?.abort()
    const response = await pending
    const text = await response.text()
    const a = JSON.parse(text)
    asks.push({ client: this.id, seq, word, fields, ms: performance.now() - started,
      requestBytes: Buffer.byteLength(request), httpBodyBytes: Buffer.byteLength(text),
      answerPayloadBytes: a.payloadBytes, status: a.status, code: a.code })
    if (read) {
      this.payloadReady?.()
      if (this.returnGate) await this.returnGate
    }
    if (a.payload?.error) throw failureFromMac(a.payload.error, a.status, { sender: "web_" + this.id, seq })
    if (a.status >= 400) throw failureFromMac({ code: a.code, layer: "mac_preflight", message: a.code }, a.status,
      { sender: "web_" + this.id, seq })
    assert.equal(a.status, 200, text)
    assert.equal(a.payload?.machine_id ?? "mac_a", "mac_a")
    if (read) {
      assert.equal(a.payload?.session_id, identity.session)
      assert.equal(a.payload?.seq, seq)
      assert.equal(a.payload?.expected_generation, fixture.generation)
    }
    return a.payload?.body ?? {}
  }
  transcript(identity: CloudIdentity) { return this.request(identity, "transcript", { limit: 200, priority: "foreground" }) }
  transcriptForGeneration(destination: ReturnType<typeof target>, signal?: AbortSignal) {
    assert.deepEqual(destination, target())
    return this.request({ machine: destination.machineID, session: destination.sessionID }, "transcript", { limit: 200 }, signal)
  }
  transcriptPageForGeneration(destination: ReturnType<typeof target>, before: number, signal?: AbortSignal) {
    return this.request({ machine: destination.machineID, session: destination.sessionID }, "transcript", { limit: 200, before }, signal)
  }
  _read(identity: CloudIdentity, word: string, fields: Record<string, unknown>) { return this.request(identity, word, fields) }
  focus(identity: CloudIdentity) { return this.request(identity, "focus", {
    request: `focus-${this.id}-${this.seq + 1}`, execution_generation: fixture.generation }) }
}
type Viewer = { reader: RelayReader; client: ReadBenchClient }
let nextClient = 0
function viewer(): Viewer {
  const client = new ReadBenchClient("bench" + String(++nextClient).padStart(8, "0"))
  const reader = new RelayReader("mac_a", { now: () => now, classicStatus: true,
    resolveSession: async (machine, session) => machine === "mac_a" && session === fixture.session ? target() : null })
  const writer = new RelayWriter(reader.writeHost, { now: () => now })
  reader.carryWrites({ route: writeRoute, answer: (route, method, url, init) => writer.answer(route, method, url, init) })
  reader.attach(client)
  return { client, reader }
}
async function routes(): Promise<unknown[]> { return (await fetch(base + "/routes")).json() }
const observations: Observation[] = []
async function phase(round: number, arm: string, viewers: Viewer[], callers: number, name: string,
  options: { query?: (index: number) => string; init?: RequestInit; path?: string; expected?: number; inflight?: boolean;
    abortAfterAsk?: AbortController } = {}) {
  const beforeRoutes = (await routes()).length
  const beforeAsks = asks.length
  const beforeReplay = replayReads
  const cpu = process.cpuUsage()
  const started = performance.now()
  abortOnAsk = options.abortAfterAsk ?? null
  const times: number[] = []
  const codes: number[] = []
  let bytes = 0
  const run = async (i: number) => {
    const t = performance.now()
    const path = options.path ?? "/v1/transcript?session=" + encodeURIComponent(fixture.session) + (options.query?.(i) ?? "&limit=200")
    const init = options.init?.headers && options.path?.endsWith("/send")
      ? { ...options.init, headers: { "Idempotency-Key": `${new Headers(options.init.headers).get("Idempotency-Key")}-${i}` } }
      : options.init
    const response = await viewers[i % viewers.length].reader.fetch(path, init)
    const text = await response.text()
    bytes += Buffer.byteLength(text)
    times.push(performance.now() - t)
    codes.push(response.status)
    assert.equal(response.status, options.expected ?? 200, text)
    const page: Page = JSON.parse(text)
    if (response.status === 200 && !options.path) {
      assert.equal(page.id, fixture.session)
      assert.ok(Array.isArray(page.entries))
      if (!path.includes("before=")) {
        assert.equal(page.entries?.length, 200, "the carried latest page uses the fixed console window")
        assert.ok(page.entries?.at(-1)?.text?.includes(String(fixture.version).padStart(4, "0")))
      }
      assert.ok(page.entries?.every((e) => e.text?.startsWith("synthetic turn")))
    }
    return page
  }
  let pages: Page[]
  if (options.inflight) {
    let release!: () => void
    let arrived!: () => void
    const atPayload = new Promise<void>((resolve) => { arrived = resolve })
    viewers[0].client.returnGate = new Promise<void>((resolve) => { release = resolve })
    viewers[0].client.payloadReady = arrived
    const first = run(0)
    // The first real response is held at the adapter boundary, so siblings
    // demonstrably join a still-pending reader, independent of HTTP speed.
    await atPayload
    const siblings = Array.from({ length: callers - 1 }, (_, i) => run(i + 1))
    await Promise.resolve()
    release()
    pages = await Promise.all([first, ...siblings])
    viewers[0].client.returnGate = null
    viewers[0].client.payloadReady = null
  } else pages = await Promise.all(Array.from({ length: callers }, (_, i) => run(i)))
  const usage = process.cpuUsage(cpu)
  abortOnAsk = null
  const allRoutes = (await routes()).slice(beforeRoutes)
  const part = asks.slice(beforeAsks)
  const sum = (key: "requestBytes" | "answerPayloadBytes" | "httpBodyBytes") => part.reduce((n, a) => n + a[key], 0)
  const row: Observation = { round, arm, clients: viewers.length, callers, phase: name,
    callerFetch: callers, inventoryReplayReads: replayReads - beforeReplay, clientAsk: part.length, machineRoute: allRoutes.length,
    requestBytes: sum("requestBytes"), answerPayloadBytes: sum("answerPayloadBytes"), httpBodyBytes: sum("httpBodyBytes"),
    callerBodyBytes: bytes, elapsedMs: performance.now() - started, callerMs: times, nodeCPUus: usage.user + usage.system,
    routes: allRoutes, asks: part, status: codes, signatures: pages.map((p) => p.signature),
    entryCounts: pages.map((p) => p.entries?.length ?? 0), correct: true }
  observations.push(row)
  return pages
}

try {
  for (let i = 0; i < 400; i++) {
    if (child.exitCode !== null) throw new Error(serverOutput)
    try { base = (await readFile(ready, "utf8")).trim(); break } catch { await delay(25) }
  }
  if (!base.startsWith("http://127.0.0.1:") || base.endsWith(":7727")) throw new Error("No isolated server: " + serverOutput)
  fixture = await (await fetch(base + "/fixture")).json() as Fixture
  // Warm the Go parser and Node HTTP machinery once, outside measured arms.
  await viewer().client.transcript({ machine: "mac_a", session: fixture.session })
  for (let round = 1; round <= 5; round++) {
    for (const count of [1, 2, 4]) {
      const vs = Array.from({ length: count }, viewer)
      const cold = await phase(round, "independent", vs, count, "cold-open")
      now += 4_000
      const warm = await phase(round, "independent", vs, count, "warm-unchanged")
      assert.deepEqual(warm.map((p) => p.signature), cold.map((p) => p.signature))
      fixture = await (await fetch(base + "/fixture", { method: "POST" })).json() as Fixture
      const changed = await phase(round, "independent", vs, count, "data-changed")
      assert.notEqual(changed[0].signature, cold[0].signature)
      // Client replacement is what attach actually does; a new reader is a separate cold-cache arm.
      for (const v of vs) { v.client = new ReadBenchClient("bench" + String(++nextClient).padStart(8, "0")); v.reader.attach(v.client) }
      await phase(round, "independent", vs, count, "reconnect-attach")
      await phase(round, "independent", Array.from({ length: count }, viewer), count, "reconnect-new-reader")
      now += 31_000
      await phase(round, "independent", vs, count, "unchanged-after-ceiling")
      const current = await phase(round, "independent", vs, count, "fresh-no-store", { init: { cache: "no-store" } })
      const cursor = current[0].nextBefore
      assert.ok(cursor && cursor > 0, "fixture must exercise a real older page")
      await phase(round, "independent", vs, count, "different-cursors", { query: (i) => "&before=" + (i % 2 ? 1 : cursor) })
      await phase(round, "independent", vs, count, "different-limits", { query: (i) => "&limit=" + (i % 2 ? 100 : 200) })
      await phase(round, "independent", vs, count, "invalid-cursor", { query: () => "&before=-1", expected: 400 })
      await phase(round, "independent", vs, count, "unsupported-after", { query: () => "&after=1", expected: 501 })
      await phase(round, "independent", vs, count, "focus-command", { path: "/v1/sessions/" + encodeURIComponent(fixture.session) + "/focus", init: { method: "POST", body: "{}" } })
      await phase(round, "independent", vs, count, "after-focus")
      const ac = new AbortController(); ac.abort()
      // Record observed cancellation semantics rather than inventing a cancellable relay.
      await phase(round, "independent", vs, count, "already-aborted-warm", { init: { signal: ac.signal } })
      const during = new AbortController()
      await phase(round, "independent", vs, count, "abort-during-fresh-read", {
        init: { signal: during.signal, cache: "no-store" }, abortAfterAsk: during })
      await phase(round, "independent", vs, count, "send-command", {
        path: "/v1/sessions/" + encodeURIComponent(fixture.session) + "/send",
        init: { method: "POST", body: JSON.stringify({ text: "synthetic command" }), headers: { "Idempotency-Key": `bench-send-${round}-${count}` } } })
      await phase(round, "independent", vs, count, "after-send-fresh")
      await phase(round, "independent", vs, count, "after-send-immediate-poll")
      now += 4_000
      await phase(round, "independent", vs, count, "after-send-4s-poll")
    }
    for (const count of [2, 4]) {
      await phase(round, "shared-reader", [viewer()], count, "same-tick-cold")
      await phase(round, "shared-reader", [viewer()], count, "established-inflight", { inflight: true })
    }
  }
  const freshRaces: unknown[] = []
  for (let round = 1; round <= 3; round++) {
    const v = viewer()
    let release!: () => void
    let arrived!: () => void
    const atPayload = new Promise<void>((resolve) => { arrived = resolve })
    v.client.returnGate = new Promise<void>((resolve) => { release = resolve })
    v.client.payloadReady = arrived
    const before = asks.length
    const routeBefore = (await routes()).length
    const started = performance.now()
    const path = "/v1/transcript?session=" + encodeURIComponent(fixture.session)
    const old = v.reader.fetch(path)
    await atPayload
    const sent = await v.reader.fetch("/v1/sessions/" + encodeURIComponent(fixture.session) + "/send", {
      method: "POST", body: JSON.stringify({ text: "synthetic fresh race" }),
      headers: { "Idempotency-Key": `fresh-race-${round}` } })
    assert.equal(sent.status, 200)
    fixture = await (await fetch(base + "/fixture", { method: "POST" })).json() as Fixture
    const fresh = v.reader.fetch(path, { cache: "no-store" })
    release()
    const oldPage = await (await old).json() as Page
    const newPage = await (await fresh).json() as Page
    assert.notEqual(newPage.signature, oldPage.signature)
    assert.ok(newPage.entries?.at(-1)?.text?.includes(String(fixture.version).padStart(4, "0")))
    freshRaces.push({ round, oldSignature: oldPage.signature, newSignature: newPage.signature,
      asks: asks.slice(before), routes: (await routes()).slice(routeBefore), ms: performance.now() - started, correct: true })
  }
  const security = viewer()
  await phase(0, "authority", [security], 1, "prime-before-revoke")
  await fetch(base + "/revoke?on=1", { method: "POST" })
  await phase(0, "authority", [security], 1, "revoked-warm-cache")
  await phase(0, "authority", [security], 1, "revoked-fresh", { init: { cache: "no-store" }, expected: 403 })
  await fetch(base + "/revoke?on=0", { method: "POST" })
  const generation = fixture.generation
  fixture = { ...fixture, generation: "00000000000000000000000000000000" }
  await phase(0, "authority", [viewer()], 1, "wrong-generation-fresh", { init: { cache: "no-store" }, expected: 409 })
  fixture = { ...fixture, generation }
  assert.notEqual(fixture.session, "%99", "the missing-session control must differ from the fixture")
  const identityChecks: unknown[] = []
  for (const [name, identity, expected] of [
    ["wrong-machine", { machine: "other_machine", session: fixture.session }, "wrong_machine"],
    ["missing-session", { machine: "mac_a", session: "%99" }, "execution_target_missing"],
  ] as const) {
    const before = (await routes()).length
    let code = "unexpected_success"
    try { await viewer().client.transcript(identity) } catch (error) { code = String((error as { code?: unknown }).code) }
    assert.equal(code, expected)
    identityChecks.push({ name, code, machineRoutes: (await routes()).length - before })
  }
  const percentile = (values: number[], p: number) => [...values].sort((a, b) => a - b)[Math.ceil(values.length * p) - 1]
  const groups = new Map<string, Observation[]>()
  for (const row of observations) {
    const key = `${row.arm}/${row.clients}/${row.callers}/${row.phase}`
    groups.set(key, [...groups.get(key) ?? [], row])
  }
  const summary = [...groups].map(([key, rows]) => ({ key, runs: rows.length,
    callerMsMedian: percentile(rows.flatMap((r) => r.callerMs), .5), callerMsP95: percentile(rows.flatMap((r) => r.callerMs), .95),
    clientAsk: rows.map((r) => r.clientAsk), machineRoute: rows.map((r) => r.machineRoute),
    answerPayloadBytes: rows.map((r) => r.answerPayloadBytes), nodeCPUus: rows.map((r) => r.nodeCPUus) }))
  await writeFile(join(out, "raw.json"), JSON.stringify({ schema: 1, measuredAt: new Date().toISOString(), node: process.version,
    platform: process.platform, arch: process.arch, rounds: 5, observations, summary, identityChecks, freshRaces,
    boundaries: ["Node transport clients, not browser contexts", "real cloudops and transcriptRoute/parser/store",
      "synthetic terminal inventory and authority", "plaintext HTTP loopback; no relay, sealing, TLS, external API or production data",
      "Node CPU only; Go CPU and encrypted frame bytes unavailable", "simulated freshness clock, real request durations"] }, null, 2) + "\n")
  console.log(JSON.stringify({ observations: observations.length, summary }, null, 2))
} finally {
  await writeFile(join(out, "partial-observations.json"), JSON.stringify(observations, null, 2) + "\n")
  if (base) await fetch(base + "/stop", { method: "POST" }).catch(() => {})
  const code = await Promise.race([exited, delay(5_000).then(() => { child.kill("SIGTERM"); return -1 })])
  await writeFile(join(out, "go-server.log"), serverOutput)
  if (code !== 0) throw new Error("Isolated Go test failed: " + code + "\n" + serverOutput)
}
