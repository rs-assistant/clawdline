/* The evidence record an operator can copy for an agent: only what this page
 * obtained under its own authorization, field by field. The Cloud's own export
 * route answers 501 (durable incidents and bundles are not built), so this is a
 * local copy of stored metadata, and it says so; it is not an incident bundle. */
import type { HealthRoute } from "./route.ts"
import type { StoredEvent, StoredGroup, StoredSource, Envelope } from "./stored.ts"
import type { PartName, StoredState } from "./cloud.ts"
// @ts-expect-error -- a `.ts` path, so node's strip-types tests can import this module.
import { heldEvents, partBody } from "./cloud.ts"

export const STORED_EVIDENCE_SCHEMA = "clawdline.connection-evidence.stored/1-draft"
/** The record's byte bound (console.health_ui_export_bytes). */
export const STORED_EVIDENCE_MAX_BYTES = 512 * 1024

export type StoredEvidence = { ok: true; json: string; bytes: number; events: number; groups: number } | { ok: false; reason: "nothing_held" | "too_large"; bytes: number | null }

const envelopeView = (e: Envelope) => ({
  generated_at: e.generated_at, outcome: e.outcome, outcome_reason: e.outcome_reason, rule_version: e.rule_version,
  evidence_quality: e.evidence_quality, partial_reason: e.partial_reason, clock: e.clock, owner_diagnostics: e.owner_diagnostics,
  coverage: {
    retention_floor: e.coverage.retention_floor, truncated: e.coverage.truncated, sequence: e.coverage.sequence,
    sequence_reason: e.coverage.sequence_reason, seq_ranges: e.coverage.seq_ranges, gaps: e.coverage.gaps,
    gap_edges: e.coverage.gap_edges, dropped: e.coverage.dropped,
  },
  source_coverage: e.source_coverage,
})

const groupView = (g: StoredGroup) => ({
  account: g.account, machine_id: g.machine_id, source_id: g.source_id, boot_id: g.boot_id, phase_id: g.phase_id, correlation: g.correlation,
  group_outcome: g.group_outcome, group_quality: g.group_quality, group_reason: g.group_reason, last_event: g.last_event, last_stage: g.last_stage,
  class: g.class, first_seq: g.first_seq, last_seq: g.last_seq, events: g.events, dropped: g.dropped, last_received_at: g.last_received_at,
  last_wall: g.last_wall, rule_version: g.rule_version,
})

const eventView = (e: StoredEvent) => ({
  id: e.id, account: e.account, machine_id: e.machine_id, source_id: e.source_id, producer_instance: e.producer_instance, schema: e.schema,
  producer_seq: e.producer_seq, event_id: e.event_id, boot_id: e.boot_id, batch_id: e.batch_id, build_commit: e.build_commit, event: e.event,
  wall: e.wall, mono_ns: e.mono_ns, source: e.source, stage: e.stage, class: e.class, kind: e.kind, phase_id: e.phase_id,
  correlation: e.correlation, count: e.count, rule: e.rule, received_at: e.received_at,
})

const sourceView = (s: StoredSource) => ({
  source_id: s.source_id, account: s.account, machine_id: s.machine_id, producer_instance: s.producer_instance, source: s.source,
  boot_id: s.boot_id, boot_basis: s.boot_basis, boot_order: s.boot_order, build_commit: s.build_commit, build_provenance: s.build_provenance,
  last_received_at: s.last_received_at, last_wall_at: s.last_wall_at, freshness: s.freshness, last_seq: s.last_seq,
  events_accepted: s.events_accepted, events_rejected: s.events_rejected, clock_offset_ms: s.clock_offset_ms, boots_evicted: s.boots_evicted,
  boots: s.boots.map((b) => ({ boot_id: b.boot_id, server_generation: b.server_generation, build_commit: b.build_commit, first_received_at: b.first_received_at, last_received_at: b.last_received_at, last_seq: b.last_seq })),
})

export function buildStoredEvidence(state: StoredState, route: HealthRoute, exportedAt: string, fixture: boolean): StoredEvidence {
  const outcomes = partBody(state, "outcomes")
  const sources = partBody(state, "sources")
  const events = heldEvents(state)
  const list = partBody(state, "trace") ?? partBody(state, "events")
  if (!outcomes && !sources && !list) return { ok: false, reason: "nothing_held", bytes: null }
  const groups = (outcomes?.groups ?? []).filter((g) => route.screen !== "evidence" || route.trace === null || g.correlation === route.trace)
  const failures: Record<string, string> = {}
  for (const [name, p] of Object.entries(state.parts) as [PartName, NonNullable<StoredState["parts"][PartName]>][]) {
    if (p.status === "failed") failures[name] = p.code
    else if (p.failure) failures[name] = `stale:${p.failure}`
  }
  const record = {
    schema: STORED_EVIDENCE_SCHEMA,
    // A task-owned fixture is never mistaken for Cloud evidence.
    fixture,
    exported_at: exportedAt,
    origin: "operator_stored_reads",
    not_an_incident_bundle: true,
    filter: { trace: route.screen === "evidence" ? route.trace : null, account: route.scope.account, window: state.window, pinned_until: route.scope.until },
    unsupported: { incidents: "unsupported", incident_export: "unsupported_capability", owner_diagnostics: "unsupported", overall_outcome: "no_presence_contract" },
    read_failures: failures,
    outcomes: outcomes ? { envelope: envelopeView(outcomes), groups: groups.map(groupView), ungrouped: outcomes.ungrouped } : null,
    events: list ? { envelope: envelopeView(list), pages: 1 + state.older.length, more: state.olderCursor !== null, rows: events.map(eventView) } : null,
    sources: sources ? { envelope: envelopeView(sources), rows: sources.sources.map(sourceView) } : null,
  }
  const json = JSON.stringify(record, null, 2)
  const bytes = new TextEncoder().encode(json).length
  return bytes > STORED_EVIDENCE_MAX_BYTES ? { ok: false, reason: "too_large", bytes } : { ok: true, json, bytes, events: events.length, groups: groups.length }
}
