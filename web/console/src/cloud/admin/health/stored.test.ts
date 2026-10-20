// Connection health over the Cloud's stored reads: the v1 adapter, the bounded loader, the page
// state, the addresses and the evidence record.
// `node --experimental-strip-types --test web/console/src/cloud/admin/health/stored.test.ts`.
// Bodies come from health/fixture.ts (task-owned examples) or are built here; none is Cloud's.
import { test } from "node:test"
import assert from "node:assert/strict"
import type { HealthRoute } from "./route.ts"
import type { LoadOutcome, Read, StoredState } from "./cloud.ts"
import type { FixtureScenario } from "./fixture.ts"
import type { HealthScope } from "./route.ts"
// @ts-expect-error -- a `.ts` path, for node.
import { parseEnvelope, parseEvents, parseOutcomes, parseOverview, parseSources, stageRole, StoredContractError, STORED_BOUNDS } from "./stored.ts"
// @ts-expect-error -- a `.ts` path, for node.
import { emptyStored, failureFor, heldEvents, HEALTH_UI_LIMITS, loadStored, mayLoadOlder, olderRead, planFor, readURL, runReads, storedReducer } from "./cloud.ts"
// @ts-expect-error -- a `.ts` path, for node.
import { healthHref, healthRoute, healthScope, routeKey, windowBounds } from "./route.ts"
// @ts-expect-error -- a `.ts` path, for node.
import { buildStoredEvidence, STORED_EVIDENCE_MAX_BYTES } from "./stored-export.ts"
// @ts-expect-error -- a `.ts` path, for node.
import { fixtureAnswer } from "./fixture.ts"
// @ts-expect-error -- a `.ts` path, for node.
import { HEALTH_WORD_TABLES } from "./words.ts"

const NOW = Date.parse("2026-10-20T12:00:00.000Z")
const API = "https://api.example.invalid"

async function fixtureBody(path: string, scenario: FixtureScenario = "normal"): Promise<unknown> {
  return (await (await fixtureAnswer(scenario, `${API}/su/v1/health/${path}?from=2026-10-20T11:00:00.000Z&to=2026-10-20T12:00:00.000Z`, NOW)).json())
}

const route = (path: string, search = ""): HealthRoute => healthRoute(path, search, NOW)!

/* ---- the adapter ------------------------------------------------------------ */

test("the v1 envelope keeps the overall verdict unknown and refuses one v1 cannot give", async () => {
  const body = await fixtureBody("overview") as Record<string, unknown>
  const env = parseEnvelope(body)
  assert.equal(env.outcome, "unknown")
  assert.equal(env.outcome_reason, "no_presence_contract")
  assert.equal(env.source_coverage.relay_worker, "service_scope")
  assert.equal(env.source_coverage.viewer, "unsupported")
  assert.throws(() => parseEnvelope({ ...body, outcome: "healthy" }), (e: unknown) => e instanceof StoredContractError && e.code === "contract_shape")
  assert.throws(() => parseEnvelope({ ...body, schema_version: 2 }), (e: unknown) => e instanceof StoredContractError && e.code === "contract_schema")
})

test("null stays unknown and zero stays zero; a floor stays a floor", async () => {
  const body = await fixtureBody("overview", "partial") as Record<string, unknown>
  const o = parseOverview(body)
  assert.equal(o.events.by_event, null)
  assert.equal(o.events.total_is_floor, true)
  assert.equal(o.evidence_quality, "partial")
  assert.equal(o.partial_reason, "count_cap")
  const empty = parseOverview(await fixtureBody("overview", "empty"))
  assert.equal(empty.events.total, null)
  assert.equal(empty.sources.fresh, null)
  assert.equal(empty.coverage.seq_ranges, null)
  assert.equal(empty.evidence_quality, "not_collected")
  const zero = parseOverview({ ...body, events: { total: 0, total_is_floor: false, by_event: { boot: 0 } } })
  assert.equal(zero.events.total, 0)
})

test("gaps, partial groups and service records arrive as the server listed them", async () => {
  const outcomes = parseOutcomes(await fixtureBody("outcomes"))
  assert.equal(outcomes.coverage.gaps?.[0].cause, "unexplained")
  assert.equal(outcomes.coverage.gaps?.[0].count, 2)
  const relay = outcomes.groups?.find((g) => g.machine_id === "svc_relay")
  assert.equal(relay?.account, "svc:relay")
  assert.equal(relay?.group_quality, "partial")
  assert.equal(relay?.group_reason, "sequence_gap_after")
  const events = parseEvents(await fixtureBody("events")).events
  const pending = events.find((e) => e.outcome === "pending")
  assert.equal(pending?.elapsed_ms, 8_400)
  assert.equal(stageRole(pending?.stage ?? null), "downstream")
  assert.equal(stageRole("fanout_sent"), "handoff")
  assert.equal(stageRole("handler_started"), null)
  // A daemon record/1 row has no service result; it is not given one.
  assert.equal(events.find((e) => e.source === "daemon")?.outcome, null)
})

test("a list longer than the Cloud's own bound is refused, not cut", async () => {
  const body = await fixtureBody("events") as Record<string, unknown>
  const one = (body.events as unknown[])[0]
  const long = Array.from({ length: STORED_BOUNDS.pageRows + 1 }, () => one)
  assert.throws(() => parseEvents({ ...body, events: long }), (e: unknown) => e instanceof StoredContractError && e.code === "contract_bound")
})

test("only listed fields are kept: an owner-private or unknown field never reaches the page", async () => {
  const body = await fixtureBody("events") as Record<string, unknown>
  const leaked = (body.events as Record<string, unknown>[]).map((e) => ({ ...e, location: "src/secret.go:12", age_ns: 9, payload: "private" }))
  const parsed = parseEvents({ ...body, events: leaked })
  for (const e of parsed.events) {
    assert.equal("location" in e, false)
    assert.equal("age_ns" in e, false)
    assert.equal("payload" in e, false)
  }
  const sources = parseSources(await fixtureBody("sources"))
  assert.equal(sources.sources[0].build_provenance, "producer_declared")
  assert.equal(sources.sources[1].boot_order, "unverified")
})

/* ---- reads and the loader --------------------------------------------------- */

test("each screen asks for a fixed, bounded set of reads, never one per row", () => {
  for (const path of ["/admin/health", "/admin/health/machines", "/admin/health/machines/fixture_mac_a", "/admin/health/services", "/admin/health/trace/fixture-trace-1", "/admin/health/incidents", "/admin/health/incidents/hi_f1000000000000000000000000000a03", "/admin/health/monitoring", "/admin/health/evidence", "/admin/health/evidence/fixture-trace-1"]) {
    const plan = planFor(route(path), NOW)
    assert.ok(plan.reads.length >= 1 && plan.reads.length <= HEALTH_UI_LIMITS.readsPerLoad, path)
    for (const r of plan.reads) assert.ok(!readURL(API, r).includes("cursor="), path)
  }
  const filtered = planFor(route("/admin/health", "?account=acct_1"), NOW)
  assert.ok(filtered.reads.every((r: Read) => r.params.account === "acct_1"))
  const trace = planFor(route("/admin/health/trace/tr-a_1"), NOW)
  assert.equal(readURL(API, trace.reads[0]).startsWith(`${API}/su/v1/health/trace/tr-a_1?`), true)
})

test("the operator surface's 404 not_found is a refusal, and typed codes keep their meaning", () => {
  assert.equal(failureFor(401, "unauthorized"), "signed_out")
  assert.equal(failureFor(404, "not_found"), "forbidden")
  assert.equal(failureFor(404, "machine_not_found"), "machine_not_found")
  assert.equal(failureFor(501, "unsupported_capability"), "unsupported")
  assert.equal(failureFor(429, "health_busy"), "rate_limited")
  assert.equal(failureFor(503, "audit_unavailable"), "audit_unavailable")
  assert.equal(failureFor(400, "bad_cursor"), "bad_cursor")
  assert.equal(failureFor(400, "window_too_wide"), "invalid_request")
})

/** A fetch that answers from the fixture and records how many reads overlap. */
function instrumented(scenario: FixtureScenario, delayMs = 10, override?: (url: string) => Response | null) {
  let inFlight = 0
  let peak = 0
  const urls: string[] = []
  const credentials: (string | undefined)[] = []
  const get = ((input: RequestInfo | URL, init?: RequestInit) => new Promise<Response>((resolve, reject) => {
    const url = String(input)
    urls.push(url)
    credentials.push(init?.credentials)
    inFlight++
    peak = Math.max(peak, inFlight)
    const timer = setTimeout(() => { inFlight--; resolve(override?.(url) ?? fixtureAnswer(scenario, url, NOW)) }, delayMs)
    init?.signal?.addEventListener("abort", () => { clearTimeout(timer); inFlight--; reject(new Error("aborted")) }, { once: true })
  })) as typeof fetch
  return { get, peak: () => peak, urls, credentials }
}

test("a load runs at most two reads at once, with the page's own cookie only", async () => {
  const f = instrumented("normal")
  const out = await loadStored(f.get, API, route("/admin/health/monitoring"), NOW, new AbortController().signal)
  assert.equal(out.kind, "parts")
  assert.equal(f.urls.length, 3)
  assert.ok(f.peak() <= HEALTH_UI_LIMITS.parallel)
  assert.ok(f.credentials.every((c) => c === "include"))
})

test("a read that fails keeps its reason and does not discard the reads that answered", async () => {
  const f = instrumented("errors")
  const out = await loadStored(f.get, API, route("/admin/health"), NOW, new AbortController().signal) as Extract<LoadOutcome, { kind: "parts" }>
  assert.equal(out.parts.overview?.ok, true)
  assert.deepEqual(out.parts.outcomes, { ok: false, code: "unavailable" })
  assert.equal(out.parts.incidents?.ok, true)
})

test("a refused session ends the load at once and clears what the page held", async () => {
  const f = instrumented("signed_out", 5)
  const out = await loadStored(f.get, API, route("/admin/health"), NOW, new AbortController().signal)
  assert.deepEqual(out, { kind: "refused", refusal: "signed_out" })
  const ready = storedReducer(emptyStored("k"), { type: "loaded", outcome: await loadStored(instrumented("normal").get, API, route("/admin/health"), NOW, new AbortController().signal), at: 1 })
  assert.equal(ready.status, "ready")
  const refused = storedReducer(ready, { type: "loaded", outcome: { kind: "refused", refusal: "forbidden" }, at: 2 })
  assert.equal(refused.status, "refused")
  assert.deepEqual(refused.parts, {})
  assert.equal(refused.window, null)
})

test("a slow read times out on its own deadline, and the budget covers reads never started", async () => {
  const slow = instrumented("normal", 200)
  const out = await runReads(slow.get, API, planFor(route("/admin/health"), NOW).reads, new AbortController().signal, { requestMs: 30, budgetMs: 1_000 })
  assert.equal(out.kind, "parts")
  if (out.kind === "parts") assert.ok(Object.values(out.parts).every((p) => p && !p.ok && p.code === "timeout"))
  const budget = await runReads(slow.get, API, planFor(route("/admin/health"), NOW).reads, new AbortController().signal, { requestMs: 1_000, budgetMs: 30 })
  if (budget.kind === "parts") assert.deepEqual(Object.values(budget.parts).map((p) => p && !p.ok && p.code), ["budget", "budget", "budget"])
  const ctl = new AbortController()
  const pending = runReads(slow.get, API, planFor(route("/admin/health"), NOW).reads, ctl.signal)
  ctl.abort()
  assert.deepEqual(await pending, { kind: "aborted" })
})

test("an answer larger than the page accepts is refused unread", async () => {
  const big = instrumented("normal", 1, () => new Response("{}", { status: 200, headers: { "content-length": String(HEALTH_UI_LIMITS.bodyBytes + 1) } }))
  const out = await runReads(big.get, API, [planFor(route("/admin/health"), NOW).reads[0]], new AbortController().signal)
  assert.deepEqual(out.kind === "parts" ? out.parts.overview : null, { ok: false, code: "body_too_large" })
})

/* ---- page state ------------------------------------------------------------- */

test("a failed refresh keeps the last answer for that read, marked stale", async () => {
  const r = route("/admin/health", "?until=2026-10-20T11:30:00.000Z")
  const first = storedReducer(emptyStored(routeKey(r)), { type: "loaded", outcome: await loadStored(instrumented("normal").get, API, r, NOW, new AbortController().signal), at: 1 })
  const second = storedReducer(first, { type: "loaded", outcome: await loadStored(instrumented("errors").get, API, r, NOW, new AbortController().signal), at: 2 })
  const outcomes = second.parts.outcomes
  assert.equal(outcomes?.status, "ok")
  assert.equal(outcomes?.status === "ok" && outcomes.failure, "unavailable")
  assert.equal(outcomes?.status === "ok" && outcomes.at, 1)
})

test("older pages use the first page's frozen window and cursor, and stop at the page bound", async () => {
  const r = route("/admin/health/machines/fixture_mac_a", "?until=2026-10-20T11:59:00.000Z")
  let s: StoredState = storedReducer(emptyStored(routeKey(r)), { type: "loaded", outcome: await loadStored(instrumented("normal").get, API, r, NOW, new AbortController().signal), at: 1 })
  assert.equal(heldEvents(s).length, 200)
  assert.equal(mayLoadOlder(s, true), "yes")
  assert.equal(mayLoadOlder(s, false), "live")
  const first = planFor(r, NOW).reads.find((x: Read) => x.part === "events")!
  const next = olderRead(first, s.window!, s.olderCursor!)
  assert.equal(next.params.from, s.window!.from)
  assert.equal(next.params.machine_id, "fixture_mac_a")
  const page = await runReads(instrumented("normal").get, API, [next], new AbortController().signal)
  s = storedReducer(s, { type: "older_loaded", result: page.kind === "parts" ? page.parts.events! : { ok: false, code: "network" }, at: 2 })
  assert.equal(heldEvents(s).length, 240)
  assert.equal(new Set(heldEvents(s).map((e) => e.id)).size, 240)
  assert.equal(mayLoadOlder(s, true), "none")
  const full = { ...s, olderCursor: "x", older: [[], [], [], []] }
  assert.equal(mayLoadOlder(full, true), "bound")
})

/* ---- addresses ------------------------------------------------------------- */

test("an address round-trips its screen and scope, and drops what it cannot vouch for", () => {
  const scope: HealthScope = { window_s: 21600, until: "2026-10-20T11:00:00.000Z", account: "acct_1" }
  for (const screen of [{ screen: "M01" }, { screen: "M02", machine: null }, { screen: "M02", machine: "mac_x" }, { screen: "M03" }, { screen: "M04", trace: "tr-1" }, { screen: "M05", incident: null }, { screen: "M05", incident: "hi_f1000000000000000000000000000a03" }, { screen: "M05", incident: "hi_f1000000000000000000000000000a03", revision: 1 }, { screen: "M03", source: "relay", sourceId: "hs_f100000000000000000000000000000c" }, { screen: "M06" }, { screen: "evidence", trace: null }, { screen: "evidence", trace: "tr-1" }] as const) {
    const href = healthHref(screen, scope)
    const u = new URL(href, "https://x.invalid")
    assert.deepEqual(healthRoute(u.pathname, u.search, NOW), { ...screen, scope }, href)
  }
  assert.equal(healthRoute("/admin/health/trace/" + "a".repeat(65), "", NOW), null)
  assert.equal(healthRoute("/admin/health/trace/a%2Fb", "", NOW), null)
  assert.equal(healthRoute("/admin/health/nope", "", NOW), null)
  assert.deepEqual(healthScope("?window=7&until=2099-01-01T00:00:00Z&account=a:b", NOW), { window_s: 3600, until: null, account: null })
  assert.equal(healthHref({ screen: "M01" }, { window_s: 3600, until: null, account: null }), "/admin/health")
  assert.deepEqual(windowBounds({ window_s: 900, until: "2026-10-20T11:00:00.000Z", account: null }, NOW), { from: "2026-10-20T10:45:00.000Z", to: "2026-10-20T11:00:00.000Z" })
})

/* ---- the evidence record ---------------------------------------------------- */

test("the evidence record holds only what was read, says what is unsupported, and is bounded", async () => {
  const r = route("/admin/health/evidence/fixture-trace-1")
  const s = storedReducer(emptyStored(routeKey(r)), { type: "loaded", outcome: await loadStored(instrumented("normal").get, API, r, NOW, new AbortController().signal), at: 1 })
  const rec = buildStoredEvidence(s, r, "2026-10-20T12:00:01.000Z", true)
  assert.equal(rec.ok, true)
  if (!rec.ok) return
  const j = JSON.parse(rec.json)
  assert.equal(j.fixture, true)
  assert.equal(j.not_an_incident_bundle, true)
  assert.equal(j.incident_bundles, "not_included_use_incident_export")
  assert.ok(j.outcomes.groups.every((g: { correlation: string }) => g.correlation === "fixture-trace-1"))
  for (const word of ["location", "age_ns", "deadline_ns", "payload"]) assert.equal(rec.json.includes(`"${word}"`), false, word)
  assert.ok(rec.bytes <= STORED_EVIDENCE_MAX_BYTES)
  assert.deepEqual(buildStoredEvidence(emptyStored("k"), r, "x", false), { ok: false, reason: "nothing_held", bytes: null })
})

/* ---- copy ------------------------------------------------------------------- */

test("every composed stored-screen word exists in both languages with the same placeholders", () => {
  const en = HEALTH_WORD_TABLES.en as Record<string, string>
  const zh = HEALTH_WORD_TABLES["zh-TW"] as Record<string, string>
  const marks = (v: string) => [...v.matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort().join(",")
  const composed = [
    ...["timeout", "budget", "network", "body_too_large", "contract_schema", "contract_shape", "contract_bound", "unsupported", "rate_limited", "unavailable", "server_deadline", "audit_unavailable", "machine_not_found", "bad_cursor", "invalid_request", "http_error"].map((k) => `st_fail_${k}`),
    ...["overview", "outcomes", "sources", "events", "trace", "incidents", "incident"].map((k) => `st_part_${k}`),
    ...["stall_detected", "slow", "recovered", "released_after_stall", "loss", "unsupported"].map((k) => `st_group_${k}`),
    ...["collected", "not_collected", "unsupported", "service_scope"].map((k) => `st_cov_${k}`),
    ...["ok", "error", "timeout", "refused", "zero_target", "threw", "pending"].map((k) => `st_out_${k}`),
    ...["own", "downstream", "handoff", "final"].map((k) => `st_role_${k}`),
    ...["M01", "M02", "M03", "M04", "M05", "M06", "evidence"].flatMap((k) => [`screen_${k}`, `lead_${k}`]),
    "window_86400",
  ]
  for (const k of composed) {
    assert.ok(en[k], k)
    assert.ok(zh[k], k)
    assert.equal(marks(zh[k]), marks(en[k]), k)
  }
  // The stage note says what a socket write proves, and no more.
  assert.match(en.socketNote, /local socket implementation accepted/)
  assert.doesNotMatch(en.socketNote, /proves only that bytes left Cloud/)
})
