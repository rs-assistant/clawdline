// Connection health: contract parsing, view decisions, the bounded source and the copy.
// `node --experimental-strip-types --test web/console/src/cloud/admin/health/health.test.ts`.
// Every snapshot below is a synthetic test body; none is reachable from the page.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node.
import { HEALTH_BOUNDS, HealthContractError, parseHealthSnapshot } from "./parse.ts"
// @ts-expect-error -- a `.ts` path, for node.
import { buildEvidence, healthPath, healthTarget, keepSelection, latencyView, placeLane, rankIncidents, rateView, stageProgress, worstOutcome } from "./model.ts"
// @ts-expect-error -- a `.ts` path, for node.
import { failureForStatus, healthReducer, HealthSourceError, httpHealthSource, initialHealthState, loadHealth } from "./source.ts"
// @ts-expect-error -- a `.ts` path, for node.
import { HEALTH_WORD_TABLES, healthDuration, healthWords } from "./words.ts"

const W = { start: "2026-10-20T09:00:00Z", end: "2026-10-20T10:00:00Z" }

function incident(over: Record<string, unknown> = {}) {
  return {
    id: "INC-1", title: "Handler does not return", state: "active", outcome: "blocked", segment: "machine_process",
    started_at: "2026-10-20T09:40:00Z", ended_at: null, certainty: "suspected", recovery: "unverified",
    affected: { count: 3, lower_bound: true }, build: "abc123", trace_id: "TR-1", rule_version: "r1",
    facts: [
      { id: "f1", source: "relay", statement: "Forwarded, no reply", observed_at: "2026-10-20T09:41:00Z", forbidden: false },
      { id: "f2", source: "machine_snapshot", statement: null, observed_at: null, forbidden: true },
    ],
    hypotheses: [{ id: "h1", statement: "Handler holds its lease", basis: "ordering", sources: ["relay"], forbidden: false }],
    missing: [{ id: "m1", what: "Handler completion receipt", next_step: "Read the local journal" }],
    ...over,
  }
}

function body(over: Record<string, unknown> = {}) {
  return {
    schema: "clawdline.connection-health.v0-draft",
    generated_at: "2026-10-20T10:00:00Z",
    scope: "cloud_operator",
    window: W,
    example: false,
    outcome: "blocked",
    quality: "partial",
    errors: { count: 0, denominator: 0 },
    latency: { p50_ms: 120, p95_ms: 900, p99_ms: 4000, samples: 12 },
    queue: { used: 64, limit: 64, oldest_s: 1080, as_of: null },
    sources: [{
      id: "relay", kind: "relay", state: "fresh", last_receipt_at: "2026-10-20T09:59:48Z", expected_every_s: 30,
      retention_s: 86400, retention_floor_at: null, gaps: [{ start: "2026-10-20T09:10:00Z", end: "2026-10-20T09:16:00Z", reason: "not_collected" }],
      dropped: 0, schema: "v1",
    }],
    lanes: [{ segment: "relay", spans: [{ start: "2026-10-20T09:20:00Z", end: "2026-10-20T09:40:00Z", state: "healthy" }], ticks: [] }],
    segments: [{ segment: "relay", outcome: "slow", quality: "complete", latency: null, errors: null, source_age_s: 12 }],
    incidents: [incident()],
    traces: [{
      id: "TR-1", incident_id: "INC-1", operation: "read", attempts: 1, build: "abc123",
      stages: [
        { stage: "accepted", state: "received", at: "2026-10-20T09:40:00Z", recorder: "api", late: false },
        { stage: "queued", state: "received", at: "2026-10-20T09:40:00.040Z", recorder: "machine", late: false },
        { stage: "executed", state: "no_receipt", at: null, recorder: null, late: false },
        { stage: "published", state: "forbidden", at: null, recorder: null, late: false },
      ],
      open_spans: [{ stage: "executed", holder: "handler", started_at: "2026-10-20T09:40:00.050Z", last_progress_at: null, deadline_ms: 15000 }],
      locations: [{ code: "machine.handler.start", path: null, line: null, symbol: null, note: null }],
    }],
    machines: [],
    services: null,
    probe: null,
    ...over,
  }
}

test("a snapshot is read field for field, and unknown values stay unknown rather than zero", () => {
  const s = parseHealthSnapshot(body())
  assert.equal(s.queue?.used, 64)
  assert.equal(s.segments[0].latency, null)
  assert.equal(s.segments[0].errors, null)
  assert.equal(s.incidents[0].facts[1].statement, null)
  assert.equal(s.example, false)
})

test("a response is refused with a typed code instead of being partly shown", () => {
  const code = (b: unknown) => {
    try {
      parseHealthSnapshot(b)
      return "accepted"
    } catch (err) {
      assert.ok(err instanceof HealthContractError)
      return (err as HealthContractError).code
    }
  }
  assert.equal(code(body({ schema: "clawdline.connection-health.v9" })), "contract_schema")
  assert.equal(code(body({ latency: { p50_ms: 1, p95_ms: 2, p99_ms: 3 } })), "contract_shape", "a missing sample count is not zero samples")
  assert.equal(code(body({ errors: { count: 3, denominator: 1 } })), "contract_shape")
  assert.equal(code(body({ incidents: [incident({ facts: [{ id: "x", source: "machine", statement: "leaked", observed_at: null, forbidden: true }] })] })), "contract_shape",
    "a forbidden item that still carries text was not filtered by its source")
  assert.equal(code(body({ incidents: Array.from({ length: HEALTH_BOUNDS.incidents + 1 }, (_, i) => incident({ id: `I${i}` })) })), "contract_bound")
  const t = body().traces[0]
  assert.equal(code(body({ traces: [{ ...t, stages: [{ stage: "executed", state: "no_receipt", at: "2026-10-20T09:40:00Z", recorder: null, late: false }] }] })), "contract_shape",
    "a stage without a receipt never gets a time")
})

test("empty latency is unknown and a thin sample hides p99; no traffic is not a success rate", () => {
  assert.deepEqual(latencyView(null), { kind: "unknown" })
  assert.deepEqual(latencyView({ p50_ms: null, p95_ms: null, p99_ms: null, samples: 0 }), { kind: "unknown" })
  const v = latencyView({ p50_ms: 10, p95_ms: 20, p99_ms: 30, samples: 19 })
  assert.equal(v.kind === "values" && v.p99, null)
  assert.equal(v.kind === "values" && v.tooFewForP99, true)
  assert.deepEqual(rateView({ count: 0, denominator: 0 }), { kind: "no_requests" })
  assert.deepEqual(rateView(null), { kind: "unknown" })
  assert.equal(worstOutcome([]), "unknown")
  assert.equal(worstOutcome(["healthy", "no_requests", "slow"]), "slow")
  assert.equal(worstOutcome(["healthy", "unknown"]), "unknown")
})

test("time a lane did not observe is drawn as unknown, never healthy", () => {
  const s = parseHealthSnapshot(body())
  const placed = placeLane(s.lanes[0], W)
  assert.deepEqual(placed.map((p: { state: string; covered: boolean }) => [p.state, p.covered]), [["unknown", false], ["healthy", true], ["unknown", false]])
  const total = placed.reduce((sum: number, p: { width: number }) => sum + p.width, 0)
  assert.ok(Math.abs(total - 100) < 1e-9)
  assert.deepEqual(placeLane({ segment: "relay", spans: [], ticks: [] }, W).map((p: { state: string }) => p.state), ["unknown"])
})

test("the last proven stage ends at the first missing receipt; only an open span names an active stage", () => {
  const s = parseHealthSnapshot(body())
  assert.deepEqual(stageProgress(s.traces[0]), { last_proven_stage: "queued", active_stage: "executed", next_unobserved_stage: "executed", visibility_limit_at: null })
  const hidden = { ...s.traces[0], stages: s.traces[0].stages.filter((x: { stage: string }) => x.stage !== "executed"), open_spans: [] }
  assert.deepEqual(stageProgress(hidden), { last_proven_stage: "queued", active_stage: null, next_unobserved_stage: "published", visibility_limit_at: "published" })
})

test("a live refresh keeps a chosen incident, and says when the new data no longer holds it", () => {
  const a = parseHealthSnapshot(body()).incidents
  const kept = keepSelection({ id: "INC-OLD", chosen: true, absent: false }, a)
  assert.deepEqual(kept, { id: "INC-OLD", chosen: true, absent: true })
  assert.deepEqual(keepSelection({ id: null, chosen: false, absent: false }, a), { id: "INC-1", chosen: false, absent: false })
  const newer = parseHealthSnapshot(body({ incidents: [incident({ id: "A", outcome: "slow", started_at: "2026-10-20T09:50:00Z" }), incident({ id: "B" }), incident({ id: "C", state: "closed", outcome: "blocked" })] })).incidents
  assert.deepEqual(rankIncidents(newer).map((i: { id: string }) => i.id), ["B", "A", "C"])
})

test("every screen has an address that reads back to itself, and stray addresses name none", () => {
  for (const t of [{ screen: "M01" }, { screen: "M02", machine: null }, { screen: "M02", machine: "m/1 x" }, { screen: "M03" }, { screen: "M04", trace: "TR 1" }, { screen: "M05", incident: "INC-1" }, { screen: "M06" }, { screen: "evidence", incident: "INC-1" }]) {
    assert.deepEqual(healthTarget(healthPath(t)), t)
  }
  assert.equal(healthTarget("/admin"), null)
  assert.equal(healthTarget("/admin/health/incident"), null)
  assert.equal(healthTarget("/admin/health/incident/a/b"), null)
  assert.equal(healthTarget("/admin/health/nowhere"), null)
  assert.equal(healthTarget("/admin/health/trace/%E0%A4%A"), null)
})

test("the agent record holds only what the scope could read, and names example data", () => {
  const raw = body({ example: true })
  ;(raw.incidents[0] as Record<string, unknown>).session_token = "secret-value"
  const s = parseHealthSnapshot(raw)
  const built = buildEvidence(s, "INC-1", "2026-10-20T10:01:00Z")
  assert.equal(built.ok, true)
  const json = built.ok ? built.json : ""
  assert.doesNotMatch(json, /secret-value/, "a field the source adds is never exported by accident")
  const record = JSON.parse(json)
  assert.equal(record.example, true)
  assert.deepEqual(record.facts[1], { source: "machine_snapshot", forbidden: true })
  assert.equal(record.trace.stage_progress.last_proven_stage, "queued")
  assert.deepEqual(buildEvidence(s, "missing", "2026-10-20T10:01:00Z"), { ok: false, reason: "no_incident" })
})

test("a load is bounded by its deadline, and a superseded load is dropped, not shown", async () => {
  const hang = { load: (_q: unknown, signal: AbortSignal) => new Promise((_, reject) => signal.addEventListener("abort", () => reject(new Error("x")))) }
  const q = { scope: "cloud_operator", window_s: 3600, until: null }
  assert.deepEqual(await loadHealth(hang, q, new AbortController().signal, 20), { ok: false, code: "timeout" })
  const outer = new AbortController()
  const pending = loadHealth(hang, q, outer.signal, 1000)
  outer.abort()
  assert.deepEqual(await pending, { ok: false, code: "aborted" })
  const refusing = { load: async () => { throw new HealthSourceError("forbidden") } }
  assert.deepEqual(await loadHealth(refusing, q, new AbortController().signal), { ok: false, code: "forbidden" })
  const malformed = { load: async () => ({ schema: "clawdline.connection-health.v0-draft" }) }
  assert.deepEqual(await loadHealth(malformed, q, new AbortController().signal), { ok: false, code: "contract_shape" })
  const good = { load: async () => body() }
  const r = await loadHealth(good, q, new AbortController().signal)
  assert.equal(r.ok, true)
})

test("the fetch source asks only the address it is given and types each refusal", async () => {
  const asked: string[] = []
  const fake = (async (url: string) => {
    asked.push(url)
    return new Response("{}", { status: 403 })
  }) as unknown as typeof fetch
  const src = httpHealthSource((q: { scope: string }) => `/injected/${q.scope}`, fake)
  const r = await loadHealth(src, { scope: "paired_owner", window_s: 900, until: null }, new AbortController().signal)
  assert.deepEqual(r, { ok: false, code: "forbidden" })
  assert.deepEqual(asked, ["/injected/paired_owner"])
  assert.equal(failureForStatus(404), "unsupported")
  assert.equal(failureForStatus(429), "rate_limited")
  assert.equal(failureForStatus(500), "http_error")
})

test("a failed refresh keeps dated figures marked stale; a failed first load shows none", () => {
  const snap = parseHealthSnapshot(body())
  const ready = healthReducer(initialHealthState, { type: "result", result: { ok: true, snapshot: snap }, at: 1 })
  const stale = healthReducer(healthReducer(ready, { type: "start" }), { type: "result", result: { ok: false, code: "timeout" }, at: 2 })
  assert.equal(stale.status, "ready")
  assert.equal(stale.snapshot, snap)
  assert.equal(stale.failure, "timeout")
  const failed = healthReducer(initialHealthState, { type: "result", result: { ok: false, code: "network" }, at: 1 })
  assert.deepEqual(failed, { status: "failed", snapshot: null, failure: "network", refreshing: false })
  assert.equal(healthReducer(ready, { type: "result", result: { ok: false, code: "aborted" }, at: 3 }).failure, null)
})

test("both languages carry every key with the same placeholders", () => {
  const en = HEALTH_WORD_TABLES.en as Record<string, string>
  const zh = HEALTH_WORD_TABLES["zh-TW"] as Record<string, string>
  const holes = (s: string) => (s.match(/\{\w+\}/g) ?? []).sort().join(",")
  assert.deepEqual(Object.keys(zh).sort(), Object.keys(en).sort())
  for (const key of Object.keys(en)) {
    assert.equal(holes(zh[key]), holes(en[key]), key)
    assert.ok(zh[key].trim().length > 0, key)
  }
  const say = healthWords("zh-TW")
  assert.equal(healthDuration(say, 125), "2 分鐘")
  assert.equal(healthDuration(say, null), "不明")
  assert.equal(healthWords("en")("tileErrorsOf", { count: 1, denominator: 4 }), "1 of 4 operations")
})
