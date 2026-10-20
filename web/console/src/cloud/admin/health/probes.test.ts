// Independent-probe evidence (`/su/v1/health/probes`, Cloud cb153af §17.4): the strict adapter,
// the one parameterless read the M06 subview sends, its address, and the reading that keeps
// stale, delayed and clock-ahead evidence from ever looking current. Also: the trend adapter
// accepts `external_probe` series and probe outcomes instead of refusing a whole readable page.
// `node --experimental-strip-types --test web/console/src/cloud/admin/health/probes.test.ts`.
// Bodies come from health/fixture.ts; the probe outcomes below are synthesized in the shape the
// contract names, not captured from a running probe (none is deployed).
import { test } from "node:test"
import assert from "node:assert/strict"
import type { HealthRoute } from "./route.ts"
import type { FixtureScenario } from "./fixture.ts"
// @ts-expect-error -- a `.ts` path, for node.
import { hostLoss, parseProbes, PROBE_BOUNDS, probePageState, targetEvidence } from "./probes.ts"
// @ts-expect-error -- a `.ts` path, for node.
import { parseAggregates, trendLanes } from "./aggregates.ts"
// @ts-expect-error -- a `.ts` path, for node.
import { StoredContractError } from "./stored.ts"
// @ts-expect-error -- a `.ts` path, for node.
import { HEALTH_UI_LIMITS, loadStored, planFor, readURL } from "./cloud.ts"
// @ts-expect-error -- a `.ts` path, for node.
import { healthHref, healthRoute, isProbeView, routeKey } from "./route.ts"
// @ts-expect-error -- a `.ts` path, for node.
import { fixtureAnswer } from "./fixture.ts"
// @ts-expect-error -- a `.ts` path, for node.
import { HEALTH_WORD_TABLES } from "./words.ts"

const NOW = Date.parse("2026-10-20T12:34:56.000Z")
const API = "https://api.example.invalid"
const route = (path: string, search = ""): HealthRoute => healthRoute(path, search, NOW)!
const clone = <T>(v: T): T => JSON.parse(JSON.stringify(v))

async function body(scenario: FixtureScenario) {
  return (await fixtureAnswer(scenario, `${API}/su/v1/health/probes`, NOW)).json()
}

function refusedAs(code: string, v: unknown) {
  assert.throws(() => parseProbes(v), (e: unknown) => e instanceof StoredContractError && e.code === code)
}

test("the probe subview's address round-trips and carries only the pause", () => {
  const r = route("/admin/health/monitoring", "view=probes&window=86400&account=acct_1&trend=7d&until=2026-10-20T12:00:00.000Z")
  assert.ok(isProbeView(r))
  // The Cloud's probe read honours no window, trend or account, so the address does not pretend to.
  assert.deepEqual(r.scope, { window_s: 3600, until: "2026-10-20T12:00:00.000Z", account: null })
  assert.equal(healthHref(r, r.scope), "/admin/health/monitoring?view=probes&until=2026-10-20T12%3A00%3A00.000Z")
  const live = route("/admin/health/monitoring", "view=probes")
  assert.equal(healthHref(live, live.scope), "/admin/health/monitoring?view=probes")
  assert.deepEqual(route("/admin/health/monitoring", healthHref(live, live.scope).split("?")[1]), live)
  assert.notEqual(routeKey(live), routeKey(route("/admin/health/monitoring")))
  // The view word on any other screen is ignored, not carried.
  assert.equal(isProbeView(route("/admin/health/services", "view=probes")), false)
})

test("the probe view is one parameterless read, inside the load's cap and budget", async () => {
  const r = route("/admin/health/monitoring", "view=probes&until=2026-10-20T12:00:00.000Z")
  const plan = planFor(r, NOW)
  assert.equal(plan.reads.length, 1)
  assert.ok(plan.reads.length <= HEALTH_UI_LIMITS.readsPerLoad)
  assert.equal(readURL(API, plan.reads[0]), `${API}/su/v1/health/probes`)
  const urls: string[] = []
  const get = (async (input: RequestInfo | URL) => { urls.push(String(input)); return fixtureAnswer("normal", String(input), NOW) }) as typeof fetch
  const out = await loadStored(get, API, r, NOW, new AbortController().signal)
  assert.equal(out.kind, "parts")
  assert.deepEqual(urls, [`${API}/su/v1/health/probes`])
  if (out.kind === "parts") assert.equal(out.parts.probes?.ok, true)
})

test("a store failure stays a typed failure, not an empty probe list", async () => {
  const r = route("/admin/health/monitoring", "view=probes")
  const get = (async (input: RequestInfo | URL) => fixtureAnswer("errors", String(input), NOW)) as typeof fetch
  const out = await loadStored(get, API, r, NOW, new AbortController().signal)
  assert.equal(out.kind, "parts")
  if (out.kind === "parts") assert.deepEqual(out.parts.probes, { ok: false, code: "unavailable" })
})

test("probing off, registered-but-silent and evidence are three different pages", async () => {
  assert.equal(probePageState(parseProbes(await body("empty"))), "off")
  const silent = parseProbes(await body("probes_silent"))
  assert.equal(probePageState(silent), "silent")
  assert.equal(silent.evidence_quality, "not_collected")
  assert.equal(silent.hosts[0].freshness, "missing")
  assert.equal(probePageState(parseProbes(await body("normal"))), "evidence")
})

test("current, delayed, clock-ahead and stale targets each read as themselves", async () => {
  const b = parseProbes(await body("normal"))
  const by = (host: string, target: string) => b.targets.find((t: { producer_instance: string; target_id: string }) => t.producer_instance === host && t.target_id === target)!
  const fresh = b.probe.fresh_within_ms
  assert.equal(targetEvidence(by("fixture_probe_a", "fixture_api"), fresh), "current")
  assert.equal(by("fixture_probe_a", "fixture_api").status, "answered_2xx")
  assert.equal(by("fixture_probe_a", "fixture_relay").status, "timeout")
  // A spool replay received a minute ago of an observation 40 minutes old is not current.
  const delayed = by("fixture_probe_b", "fixture_api")
  assert.equal(targetEvidence(delayed, fresh), "delayed")
  assert.equal(delayed.status, "unknown")
  assert.equal(delayed.independence, "same_fault_domain")
  const stale = by("fixture_probe_b", "fixture_relay")
  assert.equal(targetEvidence(stale, fresh), "stale")
  assert.equal(stale.status, "unknown")
  const capped = parseProbes(await body("partial"))
  assert.equal(targetEvidence(capped.targets.find((t: { target_id: string }) => t.target_id === "fixture_clock")!, fresh), "clock_ahead")
  assert.equal(capped.partial_reason, "scan_cap")
  assert.ok(capped.targets.every((t: { counts_are_floor: boolean }) => t.counts_are_floor))
})

test("host loss is reported, none, or unknown when the scan was capped — never zero by default", async () => {
  const b = parseProbes(await body("normal"))
  const host = (id: string) => b.hosts.find((h: { producer_instance: string }) => h.producer_instance === id)!
  assert.equal(hostLoss(host("fixture_probe_a")), "none")
  assert.equal(hostLoss(host("fixture_probe_b")), "reported")
  assert.equal(host("fixture_probe_c").freshness, "missing")
  assert.equal(b.partial_reason, "producer_reported_loss")
  const capped = parseProbes(await body("partial"))
  assert.ok(capped.hosts.every((h: Parameters<typeof hostLoss>[0]) => hostLoss(h) === "unknown"))
})

test("claims beyond the wire are refused, not shown", async () => {
  const good = await body("normal")
  parseProbes(good)
  const verified = clone(good); verified.targets[0].independence_basis = "verified"
  refusedAs("contract_shape", verified)
  const clock = clone(good); clock.targets[0].observation_clock = "server"
  refusedAs("contract_shape", clock)
  // Stale evidence is unknown; a stale target claiming a result now is drift.
  const staleOk = clone(good); staleOk.targets[3].status = "http_error"
  refusedAs("contract_shape", staleOk)
  const futureFresh = clone(good); futureFresh.targets[0].observation_age_ms = -1
  refusedAs("contract_shape", futureFresh)
  const reporting = clone(good); reporting.hosts[2].status = "reporting"
  refusedAs("contract_shape", reporting)
  const dns = clone(good); dns.targets[0].latest_result.dns_phase = "12ms"
  refusedAs("contract_shape", dns)
  const owner = clone(good); owner.scope = { kind: "owner" }
  refusedAs("contract_shape", owner)
  const verdict = clone(good); verdict.outcome = "healthy"
  refusedAs("contract_shape", verdict)
  const addr = clone(good); addr.targets[0].target_id = "https://api.example.invalid/ready"
  refusedAs("contract_shape", addr)
})

test("past the Cloud's target bound the answer is refused whole, never cut", async () => {
  const good = await body("normal")
  const many = clone(good)
  many.targets = Array.from({ length: PROBE_BOUNDS.targets + 1 }, (_, i) => ({ ...good.targets[0], target_id: `fixture_t${i}` }))
  refusedAs("contract_bound", many)
})

test("a trend page holding external-probe series and probe outcomes is read, denominator unknown", async () => {
  const good = await (await fixtureAnswer("normal", `${API}/su/v1/health/aggregates?from=2026-09-20T00:00:00.000Z&to=2026-10-20T00:00:00.000Z&bucket=1d`, NOW)).json()
  const probe = clone(good.series[0])
  probe.source = "external_probe"
  probe.machine_id = "svc_probe"
  probe.producer_instance = "fixture_probe_a"
  probe.denominator = "unknown"
  const outcomes = ["ok", "http_error", "network_error", "skipped", "timeout"]
  for (const p of probe.points) p.cells = p.cells.slice(0, 1).flatMap((c: Record<string, unknown>) => outcomes.map((o) => ({ ...c, event: o === "ok" ? "probe_completed" : o === "skipped" ? "probe_overlap_skipped" : "probe_failed", outcome: o, error: null })))
  const page = clone(good)
  page.series = [...page.series, probe]
  page.series_total = page.series.length
  const parsed = parseAggregates(page)
  const s = parsed.series.find((x: { source: string }) => x.source === "external_probe")!
  assert.equal(s.denominator, "unknown")
  // http_error and network_error are failures; a skipped check never ran and is not one.
  const lane = trendLanes(parsed).find((l: { series: { source: string } }) => l.series.source === "external_probe")!
  const failing = new Set(["http_error", "network_error", "timeout"])
  const expected = probe.points.flatMap((p: { cells: { outcome: string; records: number }[] }) => p.cells).filter((c: { outcome: string }) => failing.has(c.outcome)).reduce((n: number, c: { records: number }) => n + c.records, 0)
  assert.ok(expected > 0)
  assert.equal(lane.totals.failed, expected)
  assert.equal(lane.totals.operations, null)
  const unknown = clone(page); unknown.series[unknown.series.length - 1].points[0].cells[0].outcome = "free text"
  assert.throws(() => parseAggregates(unknown), (e: unknown) => e instanceof StoredContractError)
})

test("every probe word ships in both languages with the same placeholders", () => {
  const en = HEALTH_WORD_TABLES.en as Record<string, string>
  const zh = HEALTH_WORD_TABLES["zh-TW"] as Record<string, string>
  const holes = (s: string) => [...s.matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort().join(",")
  for (const k of Object.keys(en).filter((k) => k.startsWith("pr_"))) {
    assert.ok(zh[k], k)
    assert.equal(holes(zh[k]), holes(en[k]), k)
  }
})
