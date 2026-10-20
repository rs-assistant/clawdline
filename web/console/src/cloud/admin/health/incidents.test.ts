// Durable incidents, their immutable revisions, the audited export and the source-filtered events
// read, against the Cloud's committed contract (§3, §11, §12).
// `node --experimental-strip-types --test web/console/src/cloud/admin/health/incidents.test.ts`.
// Bodies come from health/fixture.ts (task-owned, with real canonical hashes) or are built here.
import { test } from "node:test"
import assert from "node:assert/strict"
import type { FixtureScenario } from "./fixture.ts"
import type { HealthRoute } from "./route.ts"
// @ts-expect-error -- a `.ts` path, for node.
import { parseBundle, parseEvents, parseIncidentDetail, parseIncidentList, StoredContractError } from "./stored.ts"
// @ts-expect-error -- a `.ts` path, for node.
import { emptyStored, HEALTH_UI_LIMITS, loadStored, olderRead, planFor, readURL, runReads, storedReducer } from "./cloud.ts"
// @ts-expect-error -- a `.ts` path, for node.
import { acknowledgeIncident, canonicalJson, fetchIncidentExport, readCapped, sha256Hex, verifyExport } from "./incident-export.ts"
// @ts-expect-error -- a `.ts` path, for node.
import { healthHref, healthRoute, routeKey } from "./route.ts"
// @ts-expect-error -- a `.ts` path, for node.
import { FIXTURE_INCIDENTS, fixtureAnswer } from "./fixture.ts"

const NOW = Date.parse("2026-10-20T12:00:00.000Z")
const API = "https://api.example.invalid"
const INC = FIXTURE_INCIDENTS as Record<"active" | "loss" | "recovering" | "review" | "closed" | "expired", string>

type Call = { url: string; init: RequestInit | undefined }
function recording(scenario: FixtureScenario = "normal", over?: (url: string, init?: RequestInit) => Response | null) {
  const calls: Call[] = []
  const get = (async (input: RequestInfo | URL, init?: RequestInit) => {
    calls.push({ url: String(input), init })
    return over?.(String(input), init) ?? fixtureAnswer(scenario, String(input), NOW, init?.method ?? "GET")
  }) as typeof fetch
  return { get, calls }
}
const route = (path: string, search = ""): HealthRoute => healthRoute(path, search, NOW)!
const body = async (scenario: FixtureScenario, path: string) => (await (await fixtureAnswer(scenario, `${API}/su/v1/health/${path}`, NOW)).json()) as Record<string, unknown>
const code = (f: () => unknown) => {
  try { f(); return "parsed" } catch (e) { return e instanceof StoredContractError ? e.code : "threw" }
}

/* ---- the adapter --------------------------------------------------------------- */

test("the incident list reads every state, keeps an expired row stateless, and says when Cloud has incidents off", async () => {
  const list = parseIncidentList(await body("normal", "incidents"))
  const byId = new Map(list.incidents.map((r) => [r.id, r]))
  assert.equal(byId.get(INC.active)?.state, "active")
  assert.equal(byId.get(INC.recovering)?.state, "recovering")
  assert.equal(byId.get(INC.review)?.state, "recovered_awaiting_review")
  assert.equal(byId.get(INC.closed)?.state, "closed")
  const expired = byId.get(INC.expired)!
  assert.equal(expired.expired, true)
  assert.equal(expired.state, null)
  // An expired row's fact count is unknown, never zero.
  assert.equal(expired.facts, null)
  assert.equal(list.source_coverage.incidents, "collected")
  assert.ok(list.refusals.rows.length > 0, "refusals are listed, so an absent incident may be a refused one")

  const off = parseIncidentList(await body("incidents_disabled", "incidents"))
  assert.equal(off.source_coverage.incidents, "disabled")
  assert.equal(off.evidence_quality, "not_collected")
  assert.deepEqual(off.incidents, [])
})

test("a revision keeps facts apart from what is missing and never carries a cause Cloud did not derive", async () => {
  const d = parseIncidentDetail(await body("normal", `incidents/${INC.closed}?revision=2`))
  assert.equal(d.revision.number, 2)
  assert.equal(d.incident.revision, 3)
  assert.deepEqual(d.incident.revisions, [1, 2, 3])
  assert.equal(d.bundle.summary.state, "recovering")
  assert.equal(d.bundle.summary.recovery.observed, false)
  assert.equal(d.bundle.summary.recovery.unverified_reason, "released_outcome_unknown")
  assert.equal(d.bundle.manifest.provenance.previous_revision, 1)
  assert.match(d.bundle.manifest.provenance.previous_content_hash!, /^sha256:[0-9a-f]{64}$/)
  assert.deepEqual(d.bundle.manifest.sequence.gaps.map((g) => [g.from, g.to, g.cause]), [[9, 9, "unexplained"]])
  assert.equal(d.bundle.summary.stages.root_cause, "not_derived")
  assert.deepEqual(d.bundle.summary.stages.hypotheses, [])

  // Closed is a review with its own closure, and the recovery evidence is unchanged by it.
  const closed = parseIncidentDetail(await body("normal", `incidents/${INC.closed}`))
  assert.equal(closed.bundle.summary.state, "closed")
  assert.equal(closed.bundle.summary.closure, "acknowledged_without_observed_recovery")
  assert.equal(closed.bundle.summary.recovery.observed, false)

  const raw = (await body("normal", `incidents/${INC.review}`)).bundle as Record<string, Record<string, Record<string, unknown>>>
  const drift = (mut: (b: typeof raw) => void) => {
    const copy = structuredClone(raw)
    mut(copy)
    return code(() => parseBundle(copy))
  }
  assert.equal(drift((b) => { b.summary.stages.root_cause = "dns_timeout" }), "contract_shape", "an invented root cause is refused")
  assert.equal(drift((b) => { b.summary.stages.hypotheses = ["the relay was slow"] as unknown as Record<string, unknown> }), "contract_bound", "a hypothesis Cloud does not derive is refused")
  assert.equal(drift((b) => { b.summary.stages.next = "storage_completed" }), "contract_shape", "a next stage is never inferred")
  assert.equal(drift((b) => { (b.next as Record<string, unknown>).automatic_repair = "scheduled" }), "contract_shape")
  assert.equal(drift((b) => { b.facts = Array.from({ length: 201 }, () => (raw.facts as unknown as unknown[])[0]) as unknown as Record<string, Record<string, unknown>> }), "contract_bound")
})

test("state_after_loss carries its watchdog state; a missing or misplaced state is drift, not zero", async () => {
  const W = "from=2026-10-20T11:00:00.000Z&to=2026-10-20T12:00:00.000Z"
  const events = parseEvents(await body("normal", `events?limit=200&${W}`))
  const row = (await body("normal", `events?limit=200&${W}`)).events as Record<string, unknown>[]
  const all = [...events.events]
  assert.ok(all.length > 0)
  const base = { ...(row.find((r) => r.event === "scheduling_gap") as Record<string, unknown>) }
  const env = (await body("normal", `events?limit=1&${W}`))
  const one = (e: Record<string, unknown>) => code(() => parseEvents({ ...env, events: [e] }))
  assert.equal(one({ ...base, event: "state_after_loss", state: "unverified_after_gap" }), "parsed")
  assert.equal(parseEvents({ ...env, events: [{ ...base, event: "state_after_loss", state: "blocked" }] }).events[0].state, "blocked")
  assert.equal(one({ ...base, event: "state_after_loss" }), "contract_shape")
  assert.equal(one({ ...base, event: "state_after_loss", state: "recovered" }), "contract_shape")
  assert.equal(one({ ...base, state: "slow" }), "contract_shape")
  assert.equal(parseEvents({ ...env, events: [base] }).events[0].state, null)
})

/* ---- the request contract ------------------------------------------------------------- */

test("each screen asks for the documented incident reads, never more than three, at most two at once", async () => {
  const detail = planFor(route(`/admin/health/incidents/${INC.closed}`, "revision=2&account=acct_1"), NOW)
  assert.deepEqual(detail.reads.map((r) => [r.part, r.path, r.params]), [["incident", `incidents/${INC.closed}`, { revision: "2", account: "acct_1" }]])
  assert.equal(readURL(API, detail.reads[0]), `${API}/su/v1/health/incidents/${INC.closed}?revision=2&account=acct_1`)
  const list = planFor(route("/admin/health/incidents"), NOW).reads[0]
  assert.equal(list.part, "incidents")
  assert.equal(Date.parse(list.params.to) - Date.parse(list.params.from), HEALTH_UI_LIMITS.incidentWindowMs)
  assert.equal(list.params.limit, String(HEALTH_UI_LIMITS.incidentRows))
  // M04 reads the trace, its groups and the incident list; nothing per row.
  assert.deepEqual(planFor(route("/admin/health/trace/fixture-trace-1"), NOW).reads.map((r) => r.part), ["trace", "outcomes", "incidents"])

  let inFlight = 0
  let most = 0
  const get = (async (input: RequestInfo | URL) => {
    inFlight++; most = Math.max(most, inFlight)
    await new Promise((r) => setTimeout(r, 5))
    inFlight--
    return fixtureAnswer("normal", String(input), NOW)
  }) as typeof fetch
  const out = await loadStored(get, API, route("/admin/health/trace/fixture-trace-1"), NOW, new AbortController().signal)
  assert.equal(most, HEALTH_UI_LIMITS.parallel)
  assert.equal(out.kind, "parts")
  if (out.kind === "parts") assert.equal(out.parts.incidents?.ok, true)
})

test("an incident id or revision outside the Cloud's rule names no screen and is never sent", () => {
  assert.equal(healthRoute("/admin/health/incidents/inc-1", "", NOW), null)
  assert.equal(healthRoute("/admin/health/incidents/hi_ZZ000000000000000000000000000a03", "", NOW), null)
  for (const bad of ["0", "17", "1e1", "-1", "two"]) assert.equal((route(`/admin/health/incidents/${INC.review}`, `revision=${bad}`) as { revision?: number }).revision, undefined, bad)
  const r = route(`/admin/health/incidents/${INC.review}`, "revision=1")
  assert.equal(healthHref(r, r.scope), `/admin/health/incidents/${INC.review}?revision=1`)
})

test("a changed account, incident or revision is a new key, and a gone incident is never shown from before", async () => {
  const a = route(`/admin/health/incidents/${INC.review}`, "revision=1")
  assert.notEqual(routeKey(a), routeKey(route(`/admin/health/incidents/${INC.review}`, "revision=2")))
  assert.notEqual(routeKey(a), routeKey(route(`/admin/health/incidents/${INC.closed}`, "revision=1")))
  assert.notEqual(routeKey(a), routeKey(route(`/admin/health/incidents/${INC.review}`, "revision=1&account=acct_2")))

  const { get } = recording()
  const first = await loadStored(get, API, a, NOW, new AbortController().signal)
  let s = storedReducer(emptyStored(routeKey(a)), { type: "loaded", outcome: first, at: 1 })
  assert.equal(s.parts.incident?.status, "ok")
  // The same incident later answers 410: the earlier evidence is dropped, not kept as "stale".
  s = storedReducer(s, { type: "loaded", outcome: { kind: "parts", window: s.window!, parts: { incident: { ok: false, code: "incident_expired", expiredAt: "2026-10-20T11:00:00.000Z" } } }, at: 2 })
  assert.deepEqual(s.parts.incident, { status: "failed", code: "incident_expired", expiredAt: "2026-10-20T11:00:00.000Z" })
})

test("an expired incident answers a typed 410 with its expiry; a missing revision is its own failure", async () => {
  const { get } = recording()
  const exp = await loadStored(get, API, route(`/admin/health/incidents/${INC.expired}`), NOW, new AbortController().signal)
  assert.equal(exp.kind, "parts")
  if (exp.kind === "parts") {
    const p = exp.parts.incident!
    assert.equal(p.ok, false)
    if (!p.ok) { assert.equal(p.code, "incident_expired"); assert.ok(p.expiredAt && Date.parse(p.expiredAt) < NOW) }
  }
  const missing = await loadStored(get, API, route(`/admin/health/incidents/${INC.review}`, "revision=9"), NOW, new AbortController().signal)
  if (missing.kind === "parts") assert.deepEqual(missing.parts.incident, { ok: false, code: "revision_not_found" })
  else assert.fail(missing.kind)
})

test("401, or the operator surface's 404, ends the incident load and clears everything held", async () => {
  for (const scenario of ["signed_out", "forbidden"] as const) {
    const { get } = recording(scenario)
    const out = await loadStored(get, API, route(`/admin/health/incidents/${INC.review}`), NOW, new AbortController().signal)
    assert.equal(out.kind, "refused")
    const held = storedReducer(storedReducer(emptyStored("k"), { type: "loaded", outcome: await loadStored(recording().get, API, route(`/admin/health/incidents/${INC.review}`), NOW, new AbortController().signal), at: 1 }), { type: "loaded", outcome: out, at: 2 })
    assert.deepEqual(held.parts, {})
    assert.equal(held.status, "refused")
  }
})

/* ---- the export ------------------------------------------------------------------- */

test("the canonical form is the Cloud's: keys sorted at every level, so a pretty-printed copy hashes the same bundle", async () => {
  assert.equal(canonicalJson({ b: 1, a: [{ d: null, c: "x" }] }), '{"a":[{"c":"x","d":null}],"b":1}')
  const res = await fixtureAnswer("normal", `${API}/su/v1/health/incidents/${INC.review}/export`, NOW)
  const text = await res.text()
  const parsed = JSON.parse(text)
  const pretty = JSON.stringify(parsed, null, 2)
  const hash = parsed.export.content_hash
  assert.equal(`sha256:${await sha256Hex(canonicalJson(parsed.bundle))}`, hash)
  // A hash of the bytes as text is not the bundle's hash, which is why it is never used.
  assert.notEqual(`sha256:${await sha256Hex(text)}`, hash)
  assert.notEqual(`sha256:${await sha256Hex(pretty)}`, hash)
  const v = await verifyExport(pretty, INC.review, null)
  assert.equal(v.ok, true)
})

test("an export is kept only when its hash, incident and revision all check, as the bytes Cloud sent", async () => {
  const { get, calls } = recording()
  const ok = await fetchIncidentExport(get, API, INC.review, 1, "acct_1", new AbortController().signal)
  assert.ok("ok" in ok && ok.ok)
  if ("ok" in ok && ok.ok) {
    assert.equal(ok.value.filename, `${INC.review}.r1.json`)
    assert.equal(ok.value.revision, 1)
    assert.equal(ok.value.latestRevision, 2)
    assert.equal(ok.value.text, await (await fixtureAnswer("normal", calls[0].url, NOW)).text())
  }
  assert.equal(calls[0].url, `${API}/su/v1/health/incidents/${INC.review}/export?revision=1&account=acct_1`)
  assert.equal(calls[0].init?.credentials, "include")

  const tampered = await fetchIncidentExport(recording("tampered").get, API, INC.review, null, null, new AbortController().signal)
  assert.deepEqual(tampered, { ok: false, code: "hash_mismatch" })

  // Asked for revision 1 or another incident, handed the latest of this one: not that evidence.
  const text = await (await fixtureAnswer("normal", `${API}/su/v1/health/incidents/${INC.review}/export`, NOW)).text()
  assert.deepEqual(await verifyExport(text, INC.review, 1), { ok: false, code: "wrong_incident" })
  assert.deepEqual(await verifyExport(text, INC.closed, null), { ok: false, code: "wrong_incident" })

  // Anything beyond the documented keys is not copied out unread.
  const extra = JSON.stringify({ ...JSON.parse(text), session_token: "x" })
  assert.deepEqual(await verifyExport(extra, INC.review, null), { ok: false, code: "contract_shape" })
  assert.deepEqual(await verifyExport("<html>", INC.review, null), { ok: false, code: "not_json" })
})

test("an oversized export is refused while it is read, and an audit failure leaves no bytes at all", async () => {
  let pulled = 0
  const huge = new ReadableStream<Uint8Array>({
    pull(ctl) {
      pulled += 64 * 1024
      if (pulled > 8 * 1024 * 1024) { ctl.close(); return }
      ctl.enqueue(new Uint8Array(64 * 1024).fill(0x20))
    },
  })
  const big = await fetchIncidentExport((async () => new Response(huge, { status: 200 })) as typeof fetch, API, INC.review, null, null, new AbortController().signal)
  assert.deepEqual(big, { ok: false, code: "export_too_large" })
  assert.ok(pulled <= HEALTH_UI_LIMITS.incidentExportBytes + 2 * 64 * 1024, `stopped reading at ${pulled} bytes`)
  const declared = await readCapped(new Response("x", { headers: { "content-length": String(2 * 1024 * 1024) } }), HEALTH_UI_LIMITS.incidentExportBytes)
  assert.equal(declared, null)

  assert.deepEqual(await fetchIncidentExport(recording("audit_fail").get, API, INC.review, null, null, new AbortController().signal), { ok: false, code: "audit_unavailable" })
  const gone = await fetchIncidentExport(recording().get, API, INC.expired, null, null, new AbortController().signal)
  assert.equal("ok" in gone && !gone.ok && gone.code, "incident_expired")
  assert.deepEqual(await fetchIncidentExport(recording("signed_out").get, API, INC.review, null, null, new AbortController().signal), { refused: "signed_out" })
  assert.deepEqual(await fetchIncidentExport(recording("forbidden").get, API, INC.review, null, null, new AbortController().signal), { refused: "forbidden" })
  // A malformed id is never sent.
  const { get, calls } = recording()
  assert.deepEqual(await fetchIncidentExport(get, API, "../overview", null, null, new AbortController().signal), { ok: false, code: "incident_not_found" })
  assert.equal(calls.length, 0)
})

/* ---- acknowledgement -------------------------------------------------------------- */

test("acknowledgement is one explicit POST that the browser sends from the console's origin", async () => {
  const { get, calls } = recording()
  const r = await acknowledgeIncident(get, API, INC.review, null, new AbortController().signal)
  assert.deepEqual(r, { ok: true, revised: true })
  assert.equal(calls.length, 1)
  assert.equal(calls[0].init?.method, "POST")
  assert.equal(calls[0].init?.credentials, "include")
  assert.equal(calls[0].init?.body, undefined)
  assert.equal(calls[0].url, `${API}/su/v1/health/incidents/${INC.review}/acknowledge`)
  // A wrong Origin is the Cloud's typed 403, not a signed-out session.
  const bad = await acknowledgeIncident((async () => new Response(JSON.stringify({ error: { code: "bad_origin" } }), { status: 403 })) as typeof fetch, API, INC.review, null, new AbortController().signal)
  assert.deepEqual(bad, { ok: false, code: "bad_origin" })
  const notOperator = await acknowledgeIncident(recording("forbidden").get, API, INC.review, null, new AbortController().signal)
  assert.deepEqual(notOperator, { refused: "forbidden" })
  // No read ever acknowledges.
  const reads = recording()
  await loadStored(reads.get, API, route(`/admin/health/incidents/${INC.active}`), NOW, new AbortController().signal)
  await loadStored(reads.get, API, route("/admin/health/incidents"), NOW, new AbortController().signal)
  assert.ok(reads.calls.every((c) => (c.init?.method ?? "GET") === "GET" && !c.url.includes("acknowledge")))
})

/* ---- the source-filtered events read --------------------------------------------- */

test("M03's source filter is sent to the Cloud, its cursor stays bound to it, and a changed filter starts over", async () => {
  const relay = route("/admin/health/services", "source=relay&until=2026-10-20T12:00:00.000Z")
  const plan = planFor(relay, NOW)
  const events = plan.reads.find((r) => r.part === "events")!
  assert.equal(events.params.source, "relay")
  assert.equal(readURL(API, events).includes("source=relay"), true)
  const one = route("/admin/health/services", "source_id=hs_f100000000000000000000000000000a&until=2026-10-20T12:00:00.000Z")
  assert.equal(planFor(one, NOW).reads.find((r) => r.part === "events")!.params.source_id, "hs_f100000000000000000000000000000a")
  // A malformed filter is dropped, never sent.
  assert.equal(planFor(route("/admin/health/services", "source=nope&source_id=hs_x"), NOW).reads.find((r) => r.part === "events")!.params.source, undefined)
  assert.notEqual(routeKey(relay), routeKey(route("/admin/health/services", "source=daemon&until=2026-10-20T12:00:00.000Z")))

  // Daemon rows on machine A span two pages; every page is the filtered window's, then it ends.
  const daemon = route("/admin/health/services", "source=daemon&source_id=hs_f100000000000000000000000000000a&until=2026-10-20T12:00:00.000Z")
  const { get } = recording()
  const first = await loadStored(get, API, daemon, NOW, new AbortController().signal)
  let s = storedReducer(emptyStored(routeKey(daemon)), { type: "loaded", outcome: first, at: 1 })
  assert.ok(s.olderCursor)
  const firstRead = planFor(daemon, NOW).reads.find((r) => r.part === "events")!
  const next = olderRead(firstRead, s.window!, s.olderCursor!)
  assert.equal(next.params.source, "daemon")
  const page = await runReads(get, API, [next], new AbortController().signal)
  assert.equal(page.kind, "parts")
  if (page.kind === "parts") s = storedReducer(s, { type: "older_loaded", result: page.parts.events!, at: 2 })
  assert.equal(s.olderCursor, null, "the filtered window is fully loaded")
  const rows = [...(s.parts.events!.status === "ok" ? (s.parts.events!.body as { events: { source: string }[] }).events : []), ...s.older.flat()]
  assert.equal(rows.length, 240)
  assert.ok(rows.every((e) => e.source === "daemon"))

  // A cursor taken under one filter is refused under another.
  const crossed = await runReads(get, API, [{ ...next, params: { ...next.params, source: "relay" } }], new AbortController().signal)
  assert.equal(crossed.kind, "parts")
  if (crossed.kind === "parts") assert.deepEqual(crossed.parts.events, { ok: false, code: "bad_cursor" })
})
