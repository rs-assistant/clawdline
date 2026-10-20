// Connection-health trends over the Cloud's minute rollups (`/su/v1/health/aggregates`): the
// strict adapter, the query each range sends, the loader's read budget, and the chart model's
// refusal to turn unknown into zero, a restart into continuity, or stage times into latency.
// `node --experimental-strip-types --test web/console/src/cloud/admin/health/aggregates.test.ts`.
// Bodies come from health/fixture.ts, which answers in the shape Cloud 13e7387 serves (§15.2).
import { test } from "node:test"
import assert from "node:assert/strict"
import type { HealthRoute } from "./route.ts"
import type { AggregatesBody } from "./aggregates.ts"
import type { FixtureScenario } from "./fixture.ts"
// @ts-expect-error -- a `.ts` path, for node.
import { AGGREGATE_BOUNDS, parseAggregates, stageElapsed, TREND_RANGES, trendLanes, trendWindow, viewerErrors } from "./aggregates.ts"
// @ts-expect-error -- a `.ts` path, for node.
import { StoredContractError } from "./stored.ts"
// @ts-expect-error -- a `.ts` path, for node.
import { HEALTH_UI_LIMITS, loadStored, planFor, readURL } from "./cloud.ts"
// @ts-expect-error -- a `.ts` path, for node.
import { healthHref, healthRoute, routeKey } from "./route.ts"
// @ts-expect-error -- a `.ts` path, for node.
import { fixtureAnswer } from "./fixture.ts"

const NOW = Date.parse("2026-10-20T12:34:56.000Z")
const API = "https://api.example.invalid"
const route = (path: string, search = ""): HealthRoute => healthRoute(path, search, NOW)!

/** A fetch over the fixture that records every URL asked for. */
function recording(scenario: FixtureScenario) {
  const urls: string[] = []
  const get = (async (input: RequestInfo | URL) => {
    urls.push(String(input))
    return fixtureAnswer(scenario, String(input), NOW)
  }) as typeof fetch
  return { urls, get }
}

async function trendBody(search: string, scenario: FixtureScenario = "normal", path = "/admin/health"): Promise<AggregatesBody> {
  const plan = planFor(route(path, search), NOW)
  const read = plan.reads.find((r: { part: string }) => r.part === "aggregates")!
  return parseAggregates(await (await fixtureAnswer(scenario, readURL(API, read), NOW)).json())
}

const clone = (v: unknown) => JSON.parse(JSON.stringify(v)) as Record<string, any>
async function rawBody(search = "?trend=24h"): Promise<Record<string, any>> {
  const read = planFor(route("/admin/health/monitoring", search), NOW).reads.find((r: { part: string }) => r.part === "aggregates")!
  return clone(await (await fixtureAnswer("normal", readURL(API, read), NOW)).json())
}
function refusedAs(code: string, body: unknown) {
  assert.throws(() => parseAggregates(body), (e: unknown) => e instanceof StoredContractError && e.code === code)
}

/* ---- the address and the query ---------------------------------------------- */

test("each trend range asks for its own bucket over exactly the Cloud's widest window, and the address keeps it", () => {
  for (const [range, spec] of Object.entries(TREND_RANGES) as [string, { bucket: string; bucketMs: number; spanMs: number }][]) {
    const r = route("/admin/health", `?trend=${range}`)
    const agg = planFor(r, NOW).reads.find((x: { part: string }) => x.part === "aggregates")!
    assert.equal(agg.params.bucket, spec.bucket)
    const from = Date.parse(agg.params.from)
    const to = Date.parse(agg.params.to)
    assert.equal(to - from, spec.spanMs, range)
    assert.equal(to % spec.bucketMs, 0, "aligned, so the Cloud's floor/ceil cannot widen it")
    assert.ok(to >= NOW && to - NOW < spec.bucketMs)
    assert.equal(healthHref(r, r.scope), `/admin/health?trend=${range}`)
  }
  // 30 days is 30 daily buckets: never the raw 24 h events read, never a client-side scan.
  const reads = planFor(route("/admin/health", "?trend=30d"), NOW).reads
  assert.ok(reads.every((x: { part: string }) => x.part !== "events" && x.part !== "outcomes"))
  // A malformed range is the raw view, not a guess.
  assert.equal((route("/admin/health", "?trend=90d") as { trend?: string }).trend, undefined)
})

test("the trend window never exceeds the bucket's maximum, whatever instant the page ends at", () => {
  for (let i = 0; i < 500; i++) {
    const end = NOW - Math.floor(Math.random() * 40 * 86_400_000)
    for (const range of Object.keys(TREND_RANGES) as (keyof typeof TREND_RANGES)[]) {
      const w = trendWindow(range, end)
      const ms = TREND_RANGES[range].bucketMs
      // The Cloud floors `from` and ceils `to`; both are already aligned, so the span is unchanged.
      const span = Math.ceil(Date.parse(w.to) / ms) * ms - Math.floor(Date.parse(w.from) / ms) * ms
      assert.ok(span <= TREND_RANGES[range].spanMs, `${range} at ${end}`)
    }
  }
})

test("a trend view replaces the screen's reads: at most three per load, and the scope filters ride on the aggregates read", () => {
  const m03 = route("/admin/health/services", "?trend=7d&source=relay&source_id=hs_f100000000000000000000000000000c&machine_id=svc_relay&account=acct_1")
  const plan = planFor(m03, NOW)
  assert.ok(plan.reads.length <= HEALTH_UI_LIMITS.readsPerLoad)
  const agg = plan.reads.find((x: { part: string }) => x.part === "aggregates")!
  assert.deepEqual([agg.params.source, agg.params.source_id, agg.params.machine_id, agg.params.account], ["relay", "hs_f100000000000000000000000000000c", "svc_relay", "acct_1"])
  assert.ok(plan.reads.every((x: { params: Record<string, string> }) => x.params.account === "acct_1"))
  // Range and machine are part of the key: a change drops the held answer and its figures.
  assert.notEqual(routeKey(m03), routeKey(route("/admin/health/services", "?trend=30d&source=relay&source_id=hs_f100000000000000000000000000000c&machine_id=svc_relay&account=acct_1")))
  assert.notEqual(routeKey(m03), routeKey(route("/admin/health/services", "?trend=7d&source=relay&source_id=hs_f100000000000000000000000000000c&account=acct_1")))
  assert.notEqual(routeKey(route("/admin/health")), routeKey(route("/admin/health", "?trend=30d")))
  // The incident list stays inside its 7-day window and never asks about the future.
  const inc = planFor(route("/admin/health", "?trend=30d"), NOW).reads.find((x: { part: string }) => x.part === "incidents")!
  assert.ok(Date.parse(inc.params.to) <= NOW && Date.parse(inc.params.to) - Date.parse(inc.params.from) <= HEALTH_UI_LIMITS.incidentWindowMs)
})

/* ---- the loader -------------------------------------------------------------- */

test("a 30-day trend loads through the same bounded loader, filtered by the Cloud", async () => {
  const { urls, get } = recording("normal")
  const out = await loadStored(get, API, route("/admin/health/services", "?trend=30d&source=relay"), NOW, new AbortController().signal)
  assert.equal(out.kind, "parts")
  assert.ok(urls.length <= HEALTH_UI_LIMITS.readsPerLoad)
  const agg = out.parts.aggregates
  assert.ok(agg?.ok)
  const body = (agg as { body: AggregatesBody }).body
  assert.equal(body.window.bucket, "1d")
  assert.deepEqual(body.series.map((s) => s.source), ["relay"])
  assert.ok(urls.some((u) => u.includes("/su/v1/health/aggregates?") && u.includes("bucket=1d") && u.includes("source=relay")))
})

test("a refused session ends a trend load and a store failure stays in its own panel", async () => {
  for (const [scenario, refusal] of [["signed_out", "signed_out"], ["forbidden", "forbidden"]] as const) {
    const out = await loadStored(recording(scenario).get, API, route("/admin/health", "?trend=30d"), NOW, new AbortController().signal)
    assert.deepEqual(out, { kind: "refused", refusal })
  }
  const out = await loadStored(recording("errors").get, API, route("/admin/health/monitoring", "?trend=24h"), NOW, new AbortController().signal)
  assert.equal(out.kind, "parts")
  assert.deepEqual(out.parts.aggregates, { ok: false, code: "unavailable" })
  assert.ok(out.parts.sources?.ok, "the reads that answered stay")
})

test("a window the Cloud calls too wide is a typed refusal, not an empty chart", async () => {
  const res = await fixtureAnswer("normal", `${API}/su/v1/health/aggregates?bucket=1d&from=2026-09-01T00:00:00.000Z&to=2026-10-20T00:00:00.000Z`, NOW)
  assert.equal(res.status, 400)
  assert.equal(((await res.json()) as { error: { code: string } }).error.code, "window_too_wide")
})

/* ---- the adapter ------------------------------------------------------------- */

test("the adapter reads the served shape and refuses drifted meanings and broken invariants", async () => {
  const good = await rawBody()
  const parsed = parseAggregates(good)
  assert.equal(parsed.outcome, "unknown")
  assert.equal(parsed.outcome_reason, "no_presence_contract")
  assert.equal(parsed.capability.minute, "collected")

  const sem = clone(good); sem.semantics.elapsed = "end_to_end"
  refusedAs("contract_schema", sem)
  const absent = clone(good); absent.coverage.absent_bucket = "zero"
  refusedAs("contract_schema", absent)
  const rule = clone(good); rule.rule_version = "minute-rollup/2"
  refusedAs("contract_schema", rule)
  const verdict = clone(good); verdict.outcome = "healthy"
  refusedAs("contract_shape", verdict)
  const floor = clone(good); floor.series[0].points[0].cells[0].count_is_floor = !floor.series[0].points[0].cells[0].count_is_floor
  refusedAs("contract_shape", floor)
  const misaligned = clone(good); misaligned.series[0].points[0].start = new Date(Date.parse(misaligned.series[0].points[0].start) + 1000).toISOString()
  refusedAs("contract_shape", misaligned)
  const boots = clone(good); boots.series[0].points[0].boots = 9
  refusedAs("contract_shape", boots)
  const unknownError = clone(good); unknownError.series[2].points[0].cells[0].error = "free text"
  refusedAs("contract_shape", unknownError)
})

test("past any of the Cloud's read bounds the answer is refused whole, never cut", async () => {
  const good = await rawBody()
  const many = clone(good); many.series = Array.from({ length: AGGREGATE_BOUNDS.series + 1 }, () => many.series[0]); many.series_total = many.series.length
  refusedAs("contract_bound", many)
  const cells = clone(good); cells.series[0].points[0].cells = Array.from({ length: AGGREGATE_BOUNDS.cells + 1 }, () => cells.series[0].points[0].cells[0])
  refusedAs("contract_bound", cells)
  const ids = clone(good); ids.series[0].points[0].boot_ids = Array.from({ length: AGGREGATE_BOUNDS.bootIds + 1 }, (_, i) => `b${i}`)
  refusedAs("contract_bound", ids)
  const total = clone(good); total.series_total = 0
  refusedAs("contract_shape", total)
})

/* ---- the chart model ----------------------------------------------------------- */

test("an absent bucket is unknown, never zero, and the daemon gets no ratio", async () => {
  const body = await trendBody("?trend=24h")
  const lanes = trendLanes(body)
  const daemon = lanes.find((l) => l.series.source === "daemon")!
  assert.equal(daemon.slots.length, 24 * 12)
  const holes = daemon.slots.filter((s) => s.gap === "absent")
  assert.ok(holes.length > 0)
  assert.ok(holes.every((s) => s.records === null && s.operations === null), "unknown is not zero")
  assert.equal(daemon.totals.operations, null, "unknown denominator: no operations total, no ratio")
  assert.equal(daemon.totals.heldBuckets + daemon.totals.unknownBuckets, daemon.slots.length)
  const relay = lanes.find((l) => l.series.source === "relay")!
  assert.ok(relay.totals.operations !== null && relay.totals.operations.n > 0)
})

test("a boot change is a restart, a stall or loss bucket is a signal, and producer clocks are only counted", async () => {
  const body = await trendBody("?trend=24h")
  const daemon = trendLanes(body).find((l) => l.series.source === "daemon")!
  const restarts = daemon.slots.filter((s) => s.restart)
  assert.equal(restarts.length, 1, "BOOT_A to BOOT_B, never drawn as continuous")
  const signals = daemon.slots.filter((s) => s.signals.length > 0)
  assert.ok(signals.some((s) => s.signals.includes("stall") && s.signals.includes("slow")))
  assert.ok(signals.some((s) => s.signals.includes("loss") && s.loss === 6))
  assert.ok(daemon.slots.some((s) => s.wallSkew === 2))
  // Stall records carry no count: the reported total for them is a floor, the record count exact.
  const stallCell = body.series[0].points.flatMap((p) => p.cells).find((c) => c.event === "stall_detected")!
  assert.equal(stallCell.count_is_floor, true)
  assert.equal(stallCell.records, 1)
})

test("a scan-capped answer marks what it did not read and lists the refused day", async () => {
  const body = await trendBody("?trend=7d", "partial", "/admin/health/monitoring")
  assert.equal(body.partial_reason, "scan_cap")
  assert.equal(body.rollup.refusals[0].reason, "account_bytes")
  assert.equal(body.rollup.cells_dropped, 4)
  const daemon = trendLanes(body).find((l) => l.series.source === "daemon")!
  const covered = Date.parse(body.rollup.covered_from)
  assert.ok(daemon.slots.filter((s) => s.end <= covered && s.gap !== null).every((s) => s.gap === "before_covered"))
  assert.ok(daemon.slots.filter((s) => s.start < covered && s.gap === null).every((s) => s.incomplete))
})

test("a pinned thirty days reaching past retention are unknown before the floor", async () => {
  // A live 30-day window starts after the floor; one pinned two days back reaches before it.
  const body = await trendBody("?trend=30d&until=2026-10-18T12:00:00.000Z")
  assert.equal(body.rollup.retention_clipped, true)
  assert.equal(body.partial_reason, "retention_floor")
  const first = trendLanes(body)[0].slots[0]
  assert.equal(first.gap, "before_retention")
  assert.equal(first.records, null)
})

test("viewer errors stay in their own enum and stage times are per stage, never summed", async () => {
  const body = await trendBody("?trend=6h", "normal", "/admin/health/monitoring")
  const errors = viewerErrors(body)
  assert.deepEqual(errors.map((e) => e.error).sort(), ["auth_refused", "sse_gap"])
  assert.equal(errors.find((e) => e.error === "auth_refused")!.floor, true)
  const stages = stageElapsed(body)
  const dispatch = stages.find((s) => s.stage === "account_dispatch_started")!
  assert.equal(dispatch.max_ms, 390)
  assert.equal(dispatch.buckets.reduce((a, n) => a + n, 0), dispatch.records, "a per-record histogram")
  assert.ok(stages.every((s) => s.stage !== "end_to_end"))
  assert.equal(new Set(stages.map((s) => `${s.source}|${s.stage}`)).size, stages.length)
})

test("a Cloud with rollups off says unsupported, and an empty store says not collected", async () => {
  const off = await trendBody("?trend=30d", "rollups_off")
  assert.equal(off.capability.minute, "unsupported")
  assert.equal(off.series.length, 0)
  const empty = await trendBody("?trend=24h", "empty")
  assert.equal(empty.evidence_quality, "not_collected")
  assert.deepEqual(trendLanes(empty), [])
})
