/* Task-owned example answers for the connection-health fixture harness and the
 * module tests. Every id starts with `fixture`, nothing here came from Cloud or
 * describes a real machine, and it is reachable from the page only in a
 * development build with VITE_ADMIN_HEALTH_FIXTURE set (AdminGate.tsx). It
 * answers in memory: no request leaves the browser.
 *
 * Scenarios: `normal` (also `1`), `partial`, `errors`, `empty`, `slow`,
 * `signed_out`, `forbidden`, `service`. */

export const FIXTURE_SCENARIOS = ["normal", "partial", "errors", "empty", "slow", "signed_out", "forbidden", "service"] as const
export type FixtureScenario = (typeof FIXTURE_SCENARIOS)[number]

const MAC_A = "fixture_mac_a"
const MAC_B = "fixture_mac_b"
const SRC_A = "hs_fixture_a"
const SRC_B = "hs_fixture_b"
const SRC_RELAY = "hs_fixture_relay"
const ACCOUNT = "fixture_account"
const BOOT_A = "fixture-boot-a"
const BOOT_B = "fixture-boot-b"
const BUILD = "f1x7e0000000"

type Row = Record<string, unknown>

function envelope(now: number, quality: string, extra: Row, coverage: Row | null): Row {
  const iso = new Date(now).toISOString()
  return {
    schema_version: 1, generated_at: iso, as_of: iso, scope: { kind: "operator", account: null },
    outcome: "unknown", outcome_reason: "no_presence_contract", rule_version: null,
    evidence_quality: quality, partial_reason: null, next_cursor: null, clock_uncertainty_ms: null,
    clock: { basis: "server_received_at", order: "producer_seq", producer_wall: "display_only" },
    owner_diagnostics: "unsupported",
    coverage: { retention_floor: new Date(now - 86_400_000).toISOString(), truncated: false, ...(coverage ?? { sequence: "not_computed", sequence_reason: null, seq_ranges: null, gaps: null, dropped: null }) },
    source_coverage: { daemon: "collected", relay_worker: "service_scope", relay_do: "service_scope", api: "service_scope", viewer: "unsupported", external_probe: "unsupported" },
    ...extra,
  }
}

function events(now: number, scenario: FixtureScenario): Row[] {
  const rows: Row[] = []
  // 240 daemon rows on machine A with a hole at 41–42, so paging and gaps both show.
  for (let seq = 1; seq <= 242; seq++) {
    if (seq === 41 || seq === 42) continue
    const at = now - (243 - seq) * 9_000
    const stall = seq === 230 || seq === 231
    rows.push({
      id: `he_fixture_a_${seq}`, account: ACCOUNT, machine_id: MAC_A, source_id: SRC_A, producer_instance: "daemon",
      schema: "clawdline.connection-health.record/1", producer_seq: seq, event_id: `${BOOT_A}-${seq}`, boot_id: BOOT_A,
      batch_id: `${BOOT_A}-b${Math.ceil(seq / 20)}`, build_commit: BUILD,
      event: seq === 1 ? "boot" : seq === 230 ? "slow_detected" : seq === 231 ? "stall_detected" : seq === 238 ? "recovered" : "scheduling_gap",
      wall: new Date(at - 1_500).toISOString(), mono_ns: seq * 9_000_000_000, source: "daemon",
      stage: stall || seq === 238 ? "handler_started" : null, class: stall || seq === 238 ? "read" : null, kind: stall ? "finite" : null,
      phase_id: stall || seq === 238 ? 7 : null, correlation: stall || seq === 238 ? "fixture-trace-1" : null,
      count: 1, rule: stall ? "watchdog/1" : null, received_at: new Date(at).toISOString(), retain_until: new Date(at + 86_400_000).toISOString(),
    })
  }
  // Machine B: an earlier boot, then a new one whose seq 7 arrives after seq 8 and whose clocks step back at seq 10.
  if (scenario === "normal" || scenario === "partial") {
    for (const [boot, seq, ago, mono, wallAgo, event] of [
      ["fixture-boot-b0", 1, 3_300_000, 1, 3_300_500, "boot"], ["fixture-boot-b0", 2, 3_240_000, 60, 3_240_500, "scheduling_gap"],
      [BOOT_B, 1, 2_400_000, 1, 2_400_500, "boot"], [BOOT_B, 6, 2_100_000, 300, 2_100_500, "slow_detected"],
      [BOOT_B, 8, 1_980_000, 420, 1_980_500, "scheduling_gap"], [BOOT_B, 7, 1_950_000, 400, 1_950_500, "scheduling_gap"],
      [BOOT_B, 10, 1_860_000, 380, 1_990_000, "scheduling_gap"], [BOOT_B, 12, 1_800_000, 600, 1_800_500, "scheduling_gap"],
    ] as const) {
      const at = now - ago
      rows.push({
        id: `he_fixture_b_${boot}_${seq}`, account: ACCOUNT, machine_id: MAC_B, source_id: SRC_B, producer_instance: "daemon",
        schema: "clawdline.connection-health.record/1", producer_seq: seq, event_id: `${boot}-${seq}`, boot_id: boot, batch_id: null, build_commit: null,
        event, wall: new Date(now - wallAgo).toISOString(), mono_ns: mono * 1_000_000_000, source: "daemon", stage: null, class: null, kind: null,
        phase_id: null, correlation: null, count: 1, rule: null, received_at: new Date(at).toISOString(), retain_until: new Date(at + 86_400_000).toISOString(),
      })
    }
  }
  if (scenario === "service" || scenario === "normal") {
    const stages = [
      ["relay", "relay-worker", SRC_RELAY, 1, "worker_admitted", "ok", 2, 12, null],
      ["relay", "relay-worker", SRC_RELAY, 2, "account_dispatch_started", "pending", 8_400, 1, "fixture-relay-op"],
      ["relay", "relay-worker", SRC_RELAY, 3, "account_dispatch_returned", "ok", 120, 11, null],
      ["relay", "relay-worker", SRC_RELAY, 4, "outer_settled", "ok", 35, 12, null],
      ["relay", "relay-do", "hs_fixture_relay_do", 1, "do_entered", "ok", 1, 11, null],
      ["relay", "relay-do", "hs_fixture_relay_do", 2, "storage_started", "ok", 260, 11, null],
      ["relay", "relay-do", "hs_fixture_relay_do", 3, "fanout_sent", "zero_target", 0, 11, null],
      ["cloud_api", "api", "hs_fixture_api", 1, "api_admitted", "ok", 1, 40, null],
      ["cloud_api", "api", "hs_fixture_api", 2, "api_dependency_started", "ok", 640, 40, null],
      ["cloud_api", "api", "hs_fixture_api", 3, "api_response_completed", "error", 700, 2, "fixture-api-op"],
    ] as const
    stages.forEach(([source, instance, sourceId, i, stage, outcome, elapsed, count, correlation], n) => {
      const at = now - (60 - n * 2) * 1_000
      const boot = `fixture-${instance}-boot`
      rows.push({
        id: `he_fixture_${instance}_${i}`, account: source === "relay" ? "svc:relay" : "svc:api", machine_id: source === "relay" ? "svc_relay" : "svc_api", source_id: sourceId, producer_instance: instance,
        schema: "clawdline.connection-health.record/2", producer_seq: i, event_id: `${boot}-${i}`, boot_id: boot,
        batch_id: `${boot}-b1`, build_commit: null, event: outcome === "pending" ? "stall_detected" : "stage_completed",
        wall: new Date(at).toISOString(), mono_ns: i * 1_000_000, source, stage, class: null, kind: null, phase_id: null,
        correlation, count, rule: null, outcome, elapsed_ms: elapsed, received_at: new Date(at).toISOString(), retain_until: new Date(at + 86_400_000).toISOString(),
      })
    })
  }
  return rows.sort((a, b) => String(b.received_at).localeCompare(String(a.received_at)))
}

const coverageOf = () => ({
  sequence: "exact", sequence_reason: null,
  seq_ranges: [
    { source_id: SRC_A, machine_id: MAC_A, boot_id: BOOT_A, from: 1, to: 40 },
    { source_id: SRC_A, machine_id: MAC_A, boot_id: BOOT_A, from: 43, to: 242 },
  ],
  gaps: [{ source_id: SRC_A, machine_id: MAC_A, boot_id: BOOT_A, from: 41, to: 42, count: 2, cause: "unexplained" }],
  gap_edges: "not_inferred", dropped: { transitions_lost: 0, overflow: 0 },
})

function sources(now: number): Row[] {
  const boot = (id: string, gen: number, last: number) => ({
    boot_id: id, server_generation: gen, build_commit: BUILD, first_received_at: new Date(now - 3_600_000).toISOString(),
    last_received_at: new Date(last).toISOString(), last_wall_at: new Date(last - 1_500).toISOString(), last_seq: 242, events_accepted: 240, events_rejected: 0,
  })
  return [
    {
      source_id: SRC_A, account: ACCOUNT, machine_id: MAC_A, producer_instance: "daemon", source: "daemon", boot_id: BOOT_A,
      boot_basis: "latest_receipt", boot_order: "single_boot", build_commit: BUILD, build_provenance: "producer_declared",
      first_seen_at: new Date(now - 3_600_000).toISOString(), last_received_at: new Date(now - 9_000).toISOString(),
      last_wall_at: new Date(now - 10_500).toISOString(), last_receipt_age_ms: 9_000, freshness: "fresh", expected_every_ms: null,
      last_seq: 242, events_accepted: 240, events_rejected: 0, clock_offset_ms: 1_500, boots: [boot(BOOT_A, 1, now - 9_000)], boots_evicted: 0,
      retain_until: new Date(now + 86_400_000).toISOString(),
    },
    {
      source_id: SRC_B, account: ACCOUNT, machine_id: MAC_B, producer_instance: "daemon", source: "daemon", boot_id: BOOT_B,
      boot_basis: "latest_receipt", boot_order: "unverified", build_commit: null, build_provenance: "producer_declared",
      first_seen_at: new Date(now - 7_200_000).toISOString(), last_received_at: new Date(now - 1_800_000).toISOString(),
      last_wall_at: null, last_receipt_age_ms: 1_800_000, freshness: "stale", expected_every_ms: null,
      last_seq: 12, events_accepted: 12, events_rejected: 1, clock_offset_ms: null,
      boots: [boot(BOOT_B, 2, now - 1_800_000), boot("fixture-boot-b0", 1, now - 5_000_000)], boots_evicted: 0,
      retain_until: new Date(now + 84_600_000).toISOString(),
    },
  ]
}

function groups(now: number): Row[] {
  return [{
    account: ACCOUNT, machine_id: MAC_A, source_id: SRC_A, boot_id: BOOT_A, phase_id: 7, correlation: "fixture-trace-1",
    group_outcome: "recovered", group_quality: "complete", group_reason: null, last_event: "recovered", last_stage: "handler_started",
    class: "read", first_seq: 230, last_seq: 238, events: 3, dropped: null, last_received_at: new Date(now - 45_000).toISOString(),
    evidence_age_ms: 45_000, last_wall: new Date(now - 46_500).toISOString(), basis: { time: "server_received_at", order: "producer_seq", rule_version: "cloud-derive/1" },
  }, {
    account: "svc:relay", machine_id: "svc_relay", source_id: SRC_RELAY, boot_id: "fixture-relay-boot", phase_id: null, correlation: "fixture-relay-op",
    group_outcome: "stall_detected", group_quality: "partial", group_reason: "sequence_gap_after", last_event: "stall_detected", last_stage: "account_dispatch_started",
    class: null, first_seq: 1, last_seq: 1, events: 1, dropped: null, last_received_at: new Date(now - 39_000).toISOString(),
    evidence_age_ms: 39_000, last_wall: new Date(now - 39_000).toISOString(), basis: { time: "server_received_at", order: "producer_seq", rule_version: "cloud-derive/1" },
  }]
}

const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } })
const refusal = (status: number, code: string) => json(status, { error: { code, message: code } })

/** Answers one URL from the fixture. Exported for the module tests. */
export function fixtureAnswer(scenario: FixtureScenario, url: string, now: number): Response {
  if (scenario === "signed_out") return refusal(401, "unauthorized")
  if (scenario === "forbidden") return refusal(404, "not_found")
  const u = new URL(url, "https://fixture.invalid")
  const path = u.pathname.replace(/^.*\/su\/v1\/health\//, "")
  const q = u.searchParams
  const empty = scenario === "empty"
  if (path === "overview") {
    const all = empty ? [] : events(now, scenario)
    const by: Record<string, number> = {}
    for (const e of all) by[String(e.event)] = (by[String(e.event)] ?? 0) + 1
    return json(200, envelope(now, empty ? "not_collected" : scenario === "partial" ? "partial" : "complete", {
      window: { from: q.get("from"), to: q.get("to") }, partial_reason: scenario === "partial" ? "count_cap" : null,
      events: { total: empty ? null : all.length, total_is_floor: scenario === "partial", by_event: empty || scenario === "partial" ? null : by },
      sources: { listed: empty ? 0 : 2, more: false, fresh: empty ? null : 1, stale: empty ? null : 1 },
      incidents: { coverage: "unsupported" },
    }, empty ? { sequence: "not_collected", sequence_reason: null, seq_ranges: null, gaps: null, dropped: null } : coverageOf()))
  }
  if (path === "outcomes") {
    if (scenario === "errors") return refusal(503, "health_store_unavailable")
    return json(200, { ...envelope(now, empty ? "not_collected" : "partial", {
      window: { from: q.get("from"), to: q.get("to") }, partial_reason: empty ? null : "sequence_coverage",
      groups: empty ? null : groups(now).filter((g) => !q.get("machine_id") || g.machine_id === q.get("machine_id")), ungrouped: empty ? null : { boot: 1, scheduling_gap: 233 },
    }, empty ? null : coverageOf()), rule_version: "cloud-derive/1" })
  }
  if (path === "sources") {
    const rows = empty ? [] : sources(now).filter((s) => !q.get("machine_id") || s.machine_id === q.get("machine_id"))
    return json(200, envelope(now, rows.length ? "complete" : "not_collected", { machines: null, sources: rows }, null))
  }
  if (path === "events" || path.startsWith("trace/")) {
    if (scenario === "errors" && path === "events") return refusal(429, "health_busy")
    const correlation = path.startsWith("trace/") ? decodeURIComponent(path.slice(6)) : null
    let rows = empty ? [] : events(now, scenario)
    if (correlation) rows = rows.filter((e) => e.correlation === correlation)
    if (q.get("machine_id")) rows = rows.filter((e) => e.machine_id === q.get("machine_id"))
    const limit = Math.min(200, Number(q.get("limit") ?? 100))
    const offset = q.get("cursor") ? Number(atob(q.get("cursor")!.replace(/-/g, "+").replace(/_/g, "/"))) : 0
    const page = rows.slice(offset, offset + limit)
    const more = offset + limit < rows.length
    const cursor = more ? btoa(String(offset + limit)).replace(/=+$/, "") : null
    return json(200, envelope(now, empty ? "not_collected" : more ? "partial" : "complete", {
      window: { from: q.get("from"), to: q.get("to") }, partial_reason: more ? "row_limit" : null, next_cursor: cursor, events: page,
    }, empty ? null : coverageOf()))
  }
  if (path === "incidents") return json(200, envelope(now, "unsupported", { source_coverage: { incidents: "unsupported", bundles: "unsupported", exports: "unsupported" }, incidents: null }, null))
  if (path.startsWith("incidents/")) return refusal(501, "unsupported_capability")
  return refusal(404, "not_found")
}

/** A fetch that answers from the fixture, honouring the caller's abort signal. */
export function fixtureFetch(name: string): typeof fetch {
  const scenario: FixtureScenario = (FIXTURE_SCENARIOS as readonly string[]).includes(name) ? (name as FixtureScenario) : "normal"
  return ((input: RequestInfo | URL, init?: RequestInit) => new Promise<Response>((resolve, reject) => {
    const signal = init?.signal
    const delay = scenario === "slow" ? 8_000 : 120
    const timer = setTimeout(() => resolve(fixtureAnswer(scenario, String(input), Date.now())), delay)
    signal?.addEventListener("abort", () => { clearTimeout(timer); reject(new DOMException("aborted", "AbortError")) }, { once: true })
  })) as typeof fetch
}
