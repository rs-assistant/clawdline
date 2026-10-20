/* M01–M06 and the evidence view, drawn from the Cloud's stored reads.
 *
 * Each screen shows what its reads returned and nothing more: the overall
 * verdict stays "unknown" because the Cloud has no presence contract, a group
 * state is the Cloud's `cloud-derive/1` word, a read that failed says why in its
 * own panel while the reads that succeeded stay on screen, and what no producer
 * reports yet (relay, API, viewer, latency, queues, incidents, owner
 * diagnostics) is a labelled unsupported panel, never an empty table. */
import { useMemo, useState, type ReactNode } from "react"
import type { Coverage, Envelope, StoredEvent, StoredGroup, StoredSource } from "./stored"
import { isServiceSource, stageRole } from "./stored"
import { heldEvents, mayLoadOlder, partBody, type PartName, type StoredFailure, type StoredState } from "./cloud"
import { buildStoredEvidence, STORED_EVIDENCE_MAX_BYTES } from "./stored-export"
import type { HealthRoute, HealthScreen } from "./route"
import { healthDuration, type HealthSay, type HealthWord } from "./words"
import { HTable, linkClick, Panel, Unknown } from "./parts"

export type StoredNav = { href: (s: HealthScreen) => string; go: (s: HealthScreen) => void }
export type StoredProps = { state: StoredState; route: HealthRoute; say: HealthSay; nav: StoredNav; fixture: boolean; loadOlder: () => void }

/* ---- small pieces ---------------------------------------------------------- */

/** An exact instant as the server sent it, shown in UTC to the millisecond. */
export function exactUTC(iso: string): string {
  const d = new Date(iso)
  const p = (n: number, w = 2) => String(n).padStart(w, "0")
  return `${d.getUTCFullYear()}-${p(d.getUTCMonth() + 1)}-${p(d.getUTCDate())} ${p(d.getUTCHours())}:${p(d.getUTCMinutes())}:${p(d.getUTCSeconds())}.${p(d.getUTCMilliseconds(), 3)} UTC`
}

function At({ iso }: { iso: string | null }) {
  return iso === null ? <span className="adm-unknown">—</span> : <time dateTime={iso}>{exactUTC(iso)}</time>
}

function Link({ to, nav, children, className }: { to: HealthScreen; nav: StoredNav; children: ReactNode; className?: string }) {
  return <a className={className ?? "adh-link"} href={nav.href(to)} onClick={(e) => linkClick(e, () => nav.go(to))}>{children}</a>
}

function Id({ v }: { v: string | null }) {
  return v === null ? <span className="adm-unknown">—</span> : <code className="adh-id">{v}</code>
}

const num = (say: HealthSay, n: number | null) => (n === null ? <Unknown>{say("unknown")}</Unknown> : n.toLocaleString("en-US"))

const GROUP_GLYPH: Record<StoredGroup["group_outcome"], string> = {
  stall_detected: "■", slow: "▲", recovered: "✓", released_after_stall: "◇", loss: "✕", unsupported: "?",
}

function GroupBadge({ g, say }: { g: StoredGroup["group_outcome"]; say: HealthSay }) {
  return (
    <span className={`adh-badge adh-g-${g}`}>
      <span aria-hidden="true" className="adh-glyph">{GROUP_GLYPH[g]}</span>
      {say(`st_group_${g}` as HealthWord)}
    </span>
  )
}

function failureWord(say: HealthSay, code: StoredFailure): string {
  return say(`st_fail_${code}` as HealthWord)
}

/** One read's failure, in its own panel: never an empty list, never a zero. */
function PartFailed({ name, code, say }: { name: PartName; code: StoredFailure; say: HealthSay }) {
  return (
    <div className="adm-notice adh-part-failed" role="status">
      <p><strong>{say("st_partFailed", { part: say(`st_part_${name}` as HealthWord) })}</strong></p>
      <p>{failureWord(say, code)}</p>
      <p className="adm-code">{say("failedCode", { code })}</p>
    </div>
  )
}

/** The verdict strip every screen starts with: the overall outcome and how much is known. */
function Verdict({ env, say }: { env: Envelope; say: HealthSay }) {
  return (
    <section className="adh-verdict adh-v-unknown" aria-label={say("st_verdictLabel")}>
      <div className="adh-verdict-pair">
        <div>
          <p className="adm-tile-label">{say("verdictOutcome")}</p>
          <p className="adh-verdict-value"><span className="adh-badge adh-o-unknown"><span aria-hidden="true" className="adh-glyph">?</span>{say("outcome_unknown")}</span></p>
          <p className="adm-fine">{say(env.outcome_reason === "no_presence_contract" ? "st_noPresence" : "st_outcomeReasonOther", { reason: env.outcome_reason ?? "—" })}</p>
        </div>
        <div>
          <p className="adm-tile-label">{say("verdictQuality")}</p>
          <p className="adh-verdict-value"><span className={`adh-chip adh-q-${env.evidence_quality}`}>{say(`quality_${env.evidence_quality}` as HealthWord)}</span></p>
          {env.partial_reason ? <p className="adm-fine">{say("st_partialReason", { reason: say(`st_partial_${env.partial_reason}` as HealthWord) })}</p> : null}
        </div>
      </div>
      <p className="adm-fine">{say("st_clockBasis")} <At iso={env.generated_at} /></p>
    </section>
  )
}

function Stale({ state, name, say }: { state: StoredState; name: PartName; say: HealthSay }) {
  const p = state.parts[name]
  if (!p || p.status !== "ok" || p.failure === null) return null
  return <p className="adm-stale" role="status">{say("st_partStale", { part: say(`st_part_${name}` as HealthWord), reason: failureWord(say, p.failure), time: exactUTC(new Date(p.at).toISOString()) })}</p>
}

/** Renders a part's body, its stale note, or its failure; `missing` covers a part the plan did not ask for. */
function WithPart<N extends PartName>({ state, name, say, children }: { state: StoredState; name: N; say: HealthSay; children: (body: NonNullable<ReturnType<typeof partBody<N>>>) => ReactNode }) {
  const p = state.parts[name]
  if (!p) return null
  if (p.status === "failed") return <PartFailed name={name} code={p.code} say={say} />
  return <><Stale state={state} name={name} say={say} />{children(p.body as NonNullable<ReturnType<typeof partBody<N>>>)}</>
}

function UnsupportedPanel({ title, reason, say }: { title: string; reason: string; say: HealthSay }) {
  return (
    <Panel title={title}>
      <p><span className="adh-chip adh-q-unsupported">{say("quality_unsupported")}</span></p>
      <p className="adm-fine">{reason}</p>
    </Panel>
  )
}

/* ---- shared tables --------------------------------------------------------- */

function CoveragePanel({ c, say }: { c: Coverage; say: HealthSay }) {
  return (
    <Panel title={say("st_coverageTitle")}>
      <dl className="adh-facts">
        <dt>{say("st_sequence")}</dt><dd>{say(`st_seq_${c.sequence}` as HealthWord)}{c.sequence_reason ? ` (${c.sequence_reason})` : ""}</dd>
        <dt>{say("st_retentionFloor")}</dt><dd><At iso={c.retention_floor} /></dd>
        <dt>{say("st_dropped")}</dt>
        <dd>{c.dropped === null ? <Unknown>{say("unknown")}</Unknown> : say("st_droppedValue", { lost: c.dropped.transitions_lost === null ? say("unknown") : c.dropped.transitions_lost, overflow: c.dropped.overflow === null ? say("unknown") : c.dropped.overflow })}</dd>
        <dt>{say("st_truncated")}</dt><dd>{c.truncated ? say("st_yes") : say("st_no")}</dd>
      </dl>
      {c.gaps === null ? <p className="adm-fine"><Unknown>{say("st_gapsUnknown")}</Unknown></p> : c.gaps.length === 0 ? (
        <p className="adm-fine">{say("st_noGapsListed")}</p>
      ) : (
        <HTable caption={say("st_gapsTitle")}
          columns={[{ label: say("st_colMachine") }, { label: say("st_colBoot") }, { label: say("st_colSeq") }, { label: say("st_colCount"), num: true }, { label: say("st_colCause") }]}
          rows={c.gaps.map((g, i) => ({ key: `${g.source_id}-${g.boot_id}-${g.from}-${i}`, head: <Id v={g.machine_id} />, cells: [<Id key="b" v={g.boot_id} />, `${g.from}–${g.to}`, num(say, g.count), say(`st_cause_${g.cause}` as HealthWord)] }))} />
      )}
      <p className="adm-fine">{say("st_gapEdges")}</p>
    </Panel>
  )
}

const COVERAGE_NAMES = ["daemon", "relay_worker", "relay_do", "api", "viewer", "external_probe", "incidents", "bundles", "exports"] as const

function SourceCoveragePanel({ env, say }: { env: Envelope; say: HealthSay }) {
  const entries = Object.entries(env.source_coverage)
  return (
    <Panel title={say("st_sourceCoverageTitle")}>
      {entries.length === 0 ? <p className="adm-fine"><Unknown>{say("unknown")}</Unknown></p> : (
        <ul className="adh-coverage-list">
          {entries.map(([k, v]) => (
            <li key={k}><span className="adh-coverage-name">{(COVERAGE_NAMES as readonly string[]).includes(k) ? say(`st_src_${k}` as HealthWord) : k}</span> <span className={`adh-chip adh-q-${v === "collected" ? "complete" : v === "service_scope" ? "partial" : v}`}>{say(`st_cov_${v}` as HealthWord)}</span></li>
          ))}
        </ul>
      )}
      <p className="adm-fine">{say("st_ownerDiagnostics", { state: env.owner_diagnostics })}</p>
    </Panel>
  )
}

function GroupsTable({ groups, say, nav, caption }: { groups: StoredGroup[]; say: HealthSay; nav: StoredNav; caption: string }) {
  return (
    <HTable caption={caption}
      columns={[{ label: say("st_colOutcome") }, { label: say("st_colMachine") }, { label: say("st_colBoot") }, { label: say("st_colTrace") }, { label: say("st_colStage") }, { label: say("st_colSeq") }, { label: say("st_colEvents"), num: true }, { label: say("st_colDropped"), num: true }, { label: say("st_colQuality") }, { label: say("st_colReceived") }, { label: say("st_colWall") }]}
      rows={groups.map((g, i) => ({
        key: `${g.source_id}-${g.boot_id}-${g.phase_id ?? ""}-${g.correlation ?? ""}-${i}`,
        head: <GroupBadge g={g.group_outcome} say={say} />,
        cells: [
          <Link key="m" to={{ screen: "M02", machine: g.machine_id }} nav={nav}><Id v={g.machine_id} /></Link>,
          <Id key="b" v={g.boot_id} />,
          g.correlation ? <Link key="t" to={{ screen: "M04", trace: g.correlation }} nav={nav}><Id v={g.correlation} /></Link> : <span key="t" className="adm-unknown">{g.phase_id === null ? "—" : say("st_phase", { id: g.phase_id })}</span>,
          `${g.last_stage ?? "—"}${g.class ? ` · ${g.class}` : ""}`,
          `${g.first_seq}–${g.last_seq}`,
          String(g.events),
          num(say, g.dropped),
          g.group_quality === "complete" ? say("quality_complete") : `${say("quality_partial")}: ${say(`st_greason_${g.group_reason}` as HealthWord)}`,
          <At key="r" iso={g.last_received_at} />,
          <span key="w" className="adm-fine"><At iso={g.last_wall} /></span>,
        ],
      }))} />
  )
}

function GroupsPanel({ state, say, nav, filter, title }: { state: StoredState; say: HealthSay; nav: StoredNav; filter?: (g: StoredGroup) => boolean; title: string }) {
  return (
    <Panel title={title}>
      <WithPart state={state} name="outcomes" say={say}>
        {(o) => {
          if (o.groups === null) return <p className="adm-fine"><Unknown>{say("st_groupsUnknown", { reason: o.partial_reason ?? "—" })}</Unknown></p>
          const groups = filter ? o.groups.filter(filter) : o.groups
          return (
            <>
              <p className="adm-fine">{say("st_groupsRule", { rule: o.rule_version ?? "—" })}</p>
              {groups.length === 0 ? <p className="adm-fine">{say("st_noGroups")}</p> : <GroupsTable groups={groups} say={say} nav={nav} caption={title} />}
              {o.partial_reason ? <p className="adm-fine">{say("st_partialReason", { reason: say(`st_partial_${o.partial_reason}` as HealthWord) })}</p> : null}
            </>
          )
        }}
      </WithPart>
    </Panel>
  )
}

function EventsTable({ events, say, nav, caption }: { events: StoredEvent[]; say: HealthSay; nav: StoredNav; caption: string }) {
  return (
    <HTable caption={caption}
      columns={[{ label: say("st_colReceived") }, { label: say("st_colEvent") }, { label: say("st_colStage") }, { label: say("st_colSeq"), num: true }, { label: say("st_colMachine") }, { label: say("st_colBoot") }, { label: say("st_colBuild") }, { label: say("st_colTrace") }, { label: say("st_colCount"), num: true }, { label: say("st_colWall") }]}
      rows={events.map((e) => ({
        key: e.id,
        head: <At iso={e.received_at} />,
        cells: [
          `${e.event}${e.rule ? ` (${e.rule})` : ""}`,
          `${e.stage ?? "—"}${e.class ? ` · ${e.class}` : ""}${e.kind ? ` · ${e.kind}` : ""}`,
          String(e.producer_seq),
          <Link key="m" to={{ screen: "M02", machine: e.machine_id }} nav={nav}><Id v={e.machine_id} /></Link>,
          <Id key="b" v={e.boot_id} />,
          <span key="c"><Id v={e.build_commit} /> {e.build_commit ? <span className="adm-fine">{say("st_producerDeclared")}</span> : null}</span>,
          e.correlation ? <Link key="t" to={{ screen: "M04", trace: e.correlation }} nav={nav}><Id v={e.correlation} /></Link> : <span key="t" className="adm-unknown">—</span>,
          num(say, e.count),
          <span key="w" className="adm-fine"><At iso={e.wall} /></span>,
        ],
      }))} />
  )
}

function EventsPanel({ state, say, nav, title, pinned, loadOlder }: { state: StoredState; say: HealthSay; nav: StoredNav; title: string; pinned: boolean; loadOlder: () => void }) {
  const name: PartName = state.parts.trace ? "trace" : "events"
  const events = heldEvents(state)
  const older = mayLoadOlder(state, pinned)
  return (
    <Panel title={title}>
      <WithPart state={state} name={name} say={say}>
        {(body) => (
          <>
            {events.length === 0 ? <p className="adm-fine">{body.evidence_quality === "not_collected" ? say("st_notCollected") : say("st_noEvents")}</p> : <EventsTable events={events} say={say} nav={nav} caption={title} />}
            <p className="adm-fine">{say("st_eventsHeld", { n: events.length, pages: 1 + state.older.length })}</p>
            {older === "yes" ? (
              <button type="button" className="adm-btn adm-btn-small" onClick={loadOlder} disabled={state.olderBusy} aria-busy={state.olderBusy}>
                {state.olderBusy ? say("st_loadingOlder") : say("st_loadOlder")}
              </button>
            ) : older === "live" ? <p className="adm-fine">{say("st_pauseToPage")}</p> : older === "bound" ? <p className="adm-fine">{say("st_pageBound")}</p> : null}
            {state.olderFailure ? <p className="adm-stale" role="status">{say("st_olderFailed", { reason: failureWord(say, state.olderFailure) })}</p> : null}
            <p className="adm-fine">{say("st_wallDisplayOnly")}</p>
          </>
        )}
      </WithPart>
    </Panel>
  )
}

/** Service records on the newest page: each stage's own result, its elapsed time as the service
 *  measured it, and whether that stage is waiting on something downstream. */
function ServicePanel({ state, say, nav }: { state: StoredState; say: HealthSay; nav: StoredNav }) {
  const title = say("st_serviceTitle")
  return (
    <Panel title={title}>
      <WithPart state={state} name="events" say={say}>
        {(body) => {
          const rows = body.events.filter((e) => isServiceSource(e.source))
          return (
            <>
              {rows.length === 0 ? <p className="adm-fine">{say("st_serviceNone")}</p> : (
                <HTable caption={title}
                  columns={[{ label: say("st_colReceived") }, { label: say("st_colSource") }, { label: say("st_colStage") }, { label: say("st_colRole") }, { label: say("st_colEvent") }, { label: say("st_colResult") }, { label: say("st_colElapsed"), num: true }, { label: say("st_colCount"), num: true }, { label: say("st_colBuild") }, { label: say("st_colTrace") }]}
                  rows={rows.map((e) => {
                    const role = stageRole(e.stage)
                    return {
                      key: e.id,
                      head: <At iso={e.received_at} />,
                      cells: [
                        `${e.source} · ${e.producer_instance}`,
                        e.stage ?? "—",
                        role === null ? <Unknown key="r">{say("unknown")}</Unknown> : say(`st_role_${role}` as HealthWord),
                        e.event,
                        e.outcome === null ? <Unknown key="o">{say("unknown")}</Unknown> : say(`st_out_${e.outcome}` as HealthWord),
                        e.elapsed_ms === null ? <Unknown key="e">{say("unknown")}</Unknown> : say("ms", { n: e.elapsed_ms }),
                        num(say, e.count),
                        <span key="c"><Id v={e.build_commit} /> {e.build_commit ? <span className="adm-fine">{say("st_producerDeclared")}</span> : null}</span>,
                        e.correlation ? <Link key="t" to={{ screen: "M04", trace: e.correlation }} nav={nav}><Id v={e.correlation} /></Link> : <span key="t" className="adm-unknown">—</span>,
                      ],
                    }
                  })} />
              )}
              <p className="adm-fine">{say("st_serviceNote")}</p>
            </>
          )
        }}
      </WithPart>
    </Panel>
  )
}

function SourcesTable({ sources, say, nav }: { sources: StoredSource[]; say: HealthSay; nav: StoredNav }) {
  return (
    <HTable caption={say("st_sourcesTitle")}
      columns={[{ label: say("st_colMachine") }, { label: say("st_colSource") }, { label: say("st_colBoot") }, { label: say("st_colBuild") }, { label: say("st_colFreshness") }, { label: say("st_colReceived") }, { label: say("st_colAccepted"), num: true }, { label: say("st_colRejected"), num: true }, { label: say("st_colOffset"), num: true }]}
      rows={sources.map((s) => ({
        key: s.source_id,
        head: <Link to={{ screen: "M02", machine: s.machine_id }} nav={nav}><Id v={s.machine_id} /></Link>,
        cells: [
          `${s.source ?? "—"} · ${s.producer_instance}`,
          <span key="b"><Id v={s.boot_id} /> <span className="adm-fine">{say(`st_boot_${s.boot_order}` as HealthWord)}{s.boots_evicted > 0 ? ` · ${say("st_bootsEvicted", { n: s.boots_evicted })}` : ""}</span></span>,
          <span key="c"><Id v={s.build_commit} /> <span className="adm-fine">{say("st_producerDeclared")}</span></span>,
          <span key="f" className={`adh-chip adh-q-${s.freshness === "fresh" ? "complete" : "stale"}`}>{say(`st_fresh_${s.freshness}` as HealthWord)}</span>,
          <span key="r"><At iso={s.last_received_at} /> <span className="adm-fine">({healthDuration(say, Math.round(s.last_receipt_age_ms / 1000))})</span></span>,
          String(s.events_accepted),
          String(s.events_rejected),
          s.clock_offset_ms === null ? <Unknown key="o">{say("unknown")}</Unknown> : say("ms", { n: s.clock_offset_ms }),
        ],
      }))} />
  )
}

function SourcesPanel({ state, say, nav, title }: { state: StoredState; say: HealthSay; nav: StoredNav; title: string }) {
  return (
    <Panel title={title}>
      <WithPart state={state} name="sources" say={say}>
        {(b) => (
          <>
            {b.sources.length === 0 ? <p className="adm-fine">{say("st_notCollected")}</p> : <SourcesTable sources={b.sources} say={say} nav={nav} />}
            {b.partial_reason ? <p className="adm-fine">{say("st_partialReason", { reason: say(`st_partial_${b.partial_reason}` as HealthWord) })}</p> : null}
            <p className="adm-fine">{say("st_freshNotPresence")}</p>
          </>
        )}
      </WithPart>
    </Panel>
  )
}

function firstEnvelope(state: StoredState): Envelope | null {
  for (const name of ["overview", "outcomes", "sources", "trace", "events", "incidents", "incident"] as PartName[]) {
    const b = partBody(state, name)
    if (b) return b
  }
  return null
}

function Links({ nav, say }: { nav: StoredNav; say: HealthSay }) {
  return (
    <nav className="adh-verdict-links" aria-label={say("navGroup")}>
      <Link to={{ screen: "M02", machine: null }} nav={nav}>{say("screen_M02")}</Link>
      <Link to={{ screen: "M03" }} nav={nav}>{say("screen_M03")}</Link>
      <Link to={{ screen: "M05", incident: null }} nav={nav}>{say("screen_M05")}</Link>
      <Link to={{ screen: "M06" }} nav={nav}>{say("screen_M06")}</Link>
      <Link to={{ screen: "evidence", trace: null }} nav={nav}>{say("screen_evidence")}</Link>
    </nav>
  )
}

/* ---- the screens ----------------------------------------------------------- */

function M01({ state, say, nav }: StoredProps) {
  const env = firstEnvelope(state)
  const outcomes = partBody(state, "outcomes")
  const tally = useMemo(() => {
    if (!outcomes?.groups) return null
    const t: Partial<Record<StoredGroup["group_outcome"], number>> = {}
    for (const g of outcomes.groups) t[g.group_outcome] = (t[g.group_outcome] ?? 0) + 1
    return t
  }, [outcomes])
  return (
    <div className="adh-screen">
      {env ? <Verdict env={env} say={say} /> : null}
      <Links nav={nav} say={say} />
      <div className="adm-tiles adm-tiles-4">
        <div className="adm-tile adh-tile">
          <p className="adm-tile-label">{say("st_tileEvents")}</p>
          <WithPart state={state} name="overview" say={say}>
            {(o) => (
              <>
                <p className="adm-tile-value">{o.events.total === null ? <Unknown>{say("unknown")}</Unknown> : o.events.total_is_floor ? say("st_atLeast", { n: o.events.total }) : o.events.total.toLocaleString("en-US")}</p>
                <p className="adm-tile-note">{o.events.by_event === null ? say("st_byEventUnknown") : Object.entries(o.events.by_event).filter(([, n]) => n > 0).map(([k, n]) => `${k} ${n}`).join(" · ") || say("st_noEventsWindow")}</p>
              </>
            )}
          </WithPart>
        </div>
        <div className="adm-tile adh-tile">
          <p className="adm-tile-label">{say("st_tileSources")}</p>
          <WithPart state={state} name="overview" say={say}>
            {(o) => (
              <>
                <p className="adm-tile-value">{String(o.sources.listed)}{o.sources.more ? "+" : ""}</p>
                <p className="adm-tile-note">{say("st_freshStale", { fresh: o.sources.fresh === null ? say("unknown") : o.sources.fresh, stale: o.sources.stale === null ? say("unknown") : o.sources.stale })}</p>
              </>
            )}
          </WithPart>
        </div>
        <div className="adm-tile adh-tile">
          <p className="adm-tile-label">{say("st_tileGroups")}</p>
          <WithPart state={state} name="outcomes" say={say}>
            {() => tally === null ? <p className="adm-tile-value adh-tile-small"><Unknown>{say("unknown")}</Unknown></p> : (
              <>
                <p className="adm-tile-value">{String(Object.values(tally).reduce((a, b) => a + (b ?? 0), 0))}</p>
                <p className="adm-tile-note">{Object.entries(tally).map(([k, n]) => `${say(`st_group_${k}` as HealthWord)} ${n}`).join(" · ") || say("st_noGroupsShort")}</p>
              </>
            )}
          </WithPart>
        </div>
        <div className="adm-tile adh-tile">
          <p className="adm-tile-label">{say("tileIncident")}</p>
          <p className="adm-tile-value adh-tile-small"><span className="adh-chip adh-q-unsupported">{say("quality_unsupported")}</span></p>
          <p className="adm-tile-note">{say("st_incidentsUnsupported")}</p>
        </div>
      </div>
      <p className="adm-fine">{say("st_noGroupNotSuccess")}</p>
      <GroupsPanel state={state} say={say} nav={nav} title={say("st_groupsTitle")} />
      {partBody(state, "overview") ? <CoveragePanel c={partBody(state, "overview")!.coverage} say={say} /> : null}
      <UnsupportedPanel title={say("st_latencyTitle")} reason={say("st_latencyUnsupported")} say={say} />
    </div>
  )
}

function M02({ state, route, say, nav, loadOlder }: StoredProps) {
  const machine = route.screen === "M02" ? route.machine : null
  if (machine === null) {
    return (
      <div className="adh-screen">
        <SourcesPanel state={state} say={say} nav={nav} title={say("st_sourcesTitle")} />
        <GroupsPanel state={state} say={say} nav={nav} title={say("st_groupsTitle")} />
        <UnsupportedPanel title={say("st_presenceTitle")} reason={say("st_presenceUnsupported")} say={say} />
      </div>
    )
  }
  const sources = partBody(state, "sources")?.sources ?? []
  return (
    <div className="adh-screen">
      <p className="adm-fine"><Link to={{ screen: "M02", machine: null }} nav={nav}>{say("st_allMachines")}</Link> · <Id v={machine} /></p>
      <SourcesPanel state={state} say={say} nav={nav} title={say("st_machineSources")} />
      {sources.map((s) => (
        <Panel key={s.source_id} title={say("st_bootsTitle", { source: s.producer_instance })}>
          <HTable caption={say("st_bootsTitle", { source: s.producer_instance })}
            columns={[{ label: say("st_colBoot") }, { label: say("st_colGeneration"), num: true }, { label: say("st_colBuild") }, { label: say("st_colFirst") }, { label: say("st_colReceived") }, { label: say("st_colSeq"), num: true }]}
            rows={s.boots.map((b) => ({ key: b.boot_id, head: <Id v={b.boot_id} />, cells: [String(b.server_generation), <Id key="c" v={b.build_commit} />, <At key="f" iso={b.first_received_at} />, <At key="l" iso={b.last_received_at} />, String(b.last_seq)] }))} />
          <p className="adm-fine">{say("st_bootOrderNote")}</p>
        </Panel>
      ))}
      <GroupsPanel state={state} say={say} nav={nav} title={say("st_groupsTitle")} />
      <EventsPanel state={state} say={say} nav={nav} title={say("st_eventsTitle")} pinned={route.scope.until !== null} loadOlder={loadOlder} />
      <UnsupportedPanel title={say("st_presenceTitle")} reason={say("st_presenceUnsupported")} say={say} />
    </div>
  )
}

function M03({ state, say, nav }: StoredProps) {
  const env = partBody(state, "overview") ?? partBody(state, "sources")
  return (
    <div className="adh-screen">
      {env ? <SourceCoveragePanel env={env} say={say} /> : <WithPart state={state} name="overview" say={say}>{() => null}</WithPart>}
      <Panel title={say("st_stagesTitle")}>
        <p>{say("socketNote")}</p>
        <p className="adm-fine">{say("st_stagesNote")}</p>
      </Panel>
      <ServicePanel state={state} say={say} nav={nav} />
      <SourcesPanel state={state} say={say} nav={nav} title={say("st_buildsTitle")} />
      <UnsupportedPanel title={say("st_relayTitle")} reason={say("st_relayUnsupported")} say={say} />
      <UnsupportedPanel title={say("st_latencyTitle")} reason={say("st_latencyUnsupported")} say={say} />
    </div>
  )
}

function M04({ state, route, say, nav, loadOlder }: StoredProps) {
  const trace = route.screen === "M04" ? route.trace : ""
  return (
    <div className="adh-screen">
      <p className="adm-fine">{say("st_traceId")} <Id v={trace} /></p>
      <GroupsPanel state={state} say={say} nav={nav} title={say("st_traceGroups")} filter={(g) => g.correlation === trace} />
      <EventsPanel state={state} say={say} nav={nav} title={say("st_traceEvents")} pinned={route.scope.until !== null} loadOlder={loadOlder} />
      <UnsupportedPanel title={say("st_spansTitle")} reason={say("st_spansUnsupported")} say={say} />
      <p><Link to={{ screen: "evidence", trace }} nav={nav} className="adm-btn adm-btn-small">{say("st_exportTrace")}</Link></p>
    </div>
  )
}

function M05({ state, route, say, nav }: StoredProps) {
  const incident = route.screen === "M05" ? route.incident : null
  const part = incident === null ? "incidents" : "incident"
  const p = state.parts[part]
  return (
    <div className="adh-screen">
      <Panel title={say("st_incidentsTitle")}>
        <p><span className="adh-chip adh-q-unsupported">{say("quality_unsupported")}</span></p>
        <p>{say("st_incidentsUnsupported")}</p>
        {incident !== null ? <p className="adm-fine">{say("st_incidentAsked")} <Id v={incident} /></p> : null}
        {p && p.status === "failed" && p.code !== "unsupported" ? <PartFailed name={part} code={p.code} say={say} /> : null}
        <p className="adm-code">{say("failedCode", { code: p?.status === "failed" ? p.code : "unsupported" })}</p>
        <p className="adm-fine">{say("st_noRootCause")}</p>
      </Panel>
      <GroupsPanel state={state} say={say} nav={nav} title={say("st_observedGroups")} />
    </div>
  )
}

function M06({ state, say, nav }: StoredProps) {
  const overview = partBody(state, "overview")
  return (
    <div className="adh-screen">
      {overview ? <SourceCoveragePanel env={overview} say={say} /> : <WithPart state={state} name="overview" say={say}>{() => null}</WithPart>}
      {overview ? <CoveragePanel c={overview.coverage} say={say} /> : null}
      <SourcesPanel state={state} say={say} nav={nav} title={say("st_sourcesTitle")} />
      <Panel title={say("st_boundsTitle")}>
        <p className="adm-fine">{say("st_boundsNote")}</p>
      </Panel>
      <UnsupportedPanel title={say("st_probesTitle")} reason={say("st_probesUnsupported")} say={say} />
    </div>
  )
}

function EvidenceScreen({ state, route, say, fixture }: StoredProps) {
  const [copied, setCopied] = useState<"idle" | "done" | "failed">("idle")
  const record = useMemo(() => buildStoredEvidence(state, route, new Date().toISOString(), fixture), [state, route, fixture])
  return (
    <div className="adh-screen">
      <Panel title={say("st_evidenceTitle")}>
        <p className="adm-fine">{say("st_evidenceNote")}</p>
        {record.ok ? (
          <>
            <p className="adm-fine">{say("st_evidenceSize", { kib: Math.ceil(record.bytes / 1024), max: STORED_EVIDENCE_MAX_BYTES / 1024, events: record.events, groups: record.groups })}</p>
            <div className="adm-notice-actions">
              <button type="button" className="adm-btn adm-btn-small" onClick={() => {
                if (!navigator.clipboard) { setCopied("failed"); return }
                void navigator.clipboard.writeText(record.json).then(() => setCopied("done"), () => setCopied("failed"))
              }}>{say("st_copy")}</button>
              <span role="status" className="adm-fine">{copied === "done" ? say("st_copied") : copied === "failed" ? say("st_copyFailed") : ""}</span>
            </div>
            <label className="adh-control adh-evidence-box">
              <span>{say("st_evidenceJSON")}</span>
              <textarea readOnly value={record.json} rows={14} spellCheck={false} />
            </label>
          </>
        ) : (
          <p className="adm-stale" role="status">{record.reason === "too_large" ? say("st_evidenceTooLarge", { kib: Math.ceil((record.bytes ?? 0) / 1024), max: STORED_EVIDENCE_MAX_BYTES / 1024 }) : say("st_evidenceNothing")}</p>
        )}
      </Panel>
      {(["outcomes", "events", "trace", "sources"] as PartName[]).map((n) => {
        const p = state.parts[n]
        return p && p.status === "failed" ? <PartFailed key={n} name={n} code={p.code} say={say} /> : null
      })}
      <UnsupportedPanel title={say("st_bundleTitle")} reason={say("st_bundleUnsupported")} say={say} />
    </div>
  )
}

export function StoredScreen(props: StoredProps) {
  switch (props.route.screen) {
    case "M01": return <M01 {...props} />
    case "M02": return <M02 {...props} />
    case "M03": return <M03 {...props} />
    case "M04": return <M04 {...props} />
    case "M05": return <M05 {...props} />
    case "M06": return <M06 {...props} />
    case "evidence": return <EvidenceScreen {...props} />
  }
}
