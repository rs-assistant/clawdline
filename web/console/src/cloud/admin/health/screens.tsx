/* The six connection-health screens, M01–M06, and the agent evidence view.
 * Each renders one parsed snapshot it is handed; none fetches or invents data. */
import { useMemo, useRef, useState } from "react"
import type { HealthSnapshot, Incident, Lane, Machine, Stage } from "./types"
import { buildEvidence, EVIDENCE_MAX_BYTES, latencyView, logWidth, rankIncidents, rateView, sourceAge, stageOffsets, stageProgress } from "./model"
import { healthClock, healthDuration, healthMs, type HealthSay, type HealthWord } from "./words"
import {
  Age, ageSince, Crumb, HTable, latencyCells, LatencyText, NavLink, OutcomeBadge, Panel, QualityChip, RateText, Timeline,
  Unknown, type ScreenProps,
} from "./parts"

type SnapProps = ScreenProps & { snapshot: HealthSnapshot }

const segLabel = (say: HealthSay) => (lane: Lane) => say(`seg_${lane.segment}` as HealthWord)
const at = (iso: string | null, say: HealthSay) => (iso === null ? <Unknown>{say("unknown")}</Unknown> : `${healthClock(iso)} UTC`)
const stageWord = (say: HealthSay, s: Stage) => say(`stage_${s}` as HealthWord)

/** A forbidden item leaves a count, never a body. */
function HiddenCount({ n, say }: { n: number; say: HealthSay }) {
  return n > 0 ? <p className="adm-fine adh-hidden">{say("hiddenCount", { n })}</p> : null
}

/* ---- M01 Connection health ------------------------------------------------ */

export function M01Overview({ snapshot: s, say, now, nav }: SnapProps) {
  const ranked = useMemo(() => rankIncidents(s.incidents), [s.incidents])
  const lead = ranked.find((i) => i.state === "active" || i.state === "recovering") ?? null
  const lat = latencyView(s.latency)
  const rate = rateView(s.errors)
  return (
    <div className="adh-screen">
      <section className={`adh-verdict adh-v-${s.outcome}`} aria-labelledby="adh-verdict-h">
        <h2 id="adh-verdict-h" className="adm-visually-hidden">{say("screen_M01")}</h2>
        <div className="adh-verdict-pair">
          <div>
            <p className="adm-tile-label">{say("verdictOutcome")}</p>
            <p className="adh-verdict-value"><OutcomeBadge outcome={s.outcome} say={say} /></p>
          </div>
          <div>
            <p className="adm-tile-label">{say("verdictQuality")}</p>
            <p className="adh-verdict-value"><QualityChip quality={s.quality} say={say} /></p>
          </div>
        </div>
        {s.outcome === "unknown" ? <p className="adm-fine">{say("verdictUnknownNote")}</p> : null}
        {lead ? (
          <div className="adh-verdict-incident">
            <p className="adh-verdict-title">{lead.title} <span className="adh-id">{lead.id}</span></p>
            <p className="adm-fine">{say("verdictSince", { time: healthClock(lead.started_at), age: healthDuration(say, ageSince(lead.started_at, now)) })}</p>
            <NavLink to={{ screen: "M05", incident: lead.id }} nav={nav} className="adm-btn adm-btn-go adm-btn-small">{say("openIncident")}</NavLink>
          </div>
        ) : <p className="adm-fine">{say("verdictNoIncident")}</p>}
        <p className="adh-verdict-links">
          <NavLink to={{ screen: "M02", machine: null }} nav={nav}>{say("screen_M02")}</NavLink>
          <NavLink to={{ screen: "M03" }} nav={nav}>{say("screen_M03")}</NavLink>
          <NavLink to={{ screen: "M06" }} nav={nav}>{say("screen_M06")}</NavLink>
        </p>
      </section>

      <div className="adm-tiles adm-tiles-4">
        <div className="adm-tile adh-tile">
          <p className="adm-tile-label">{say("tileIncident")}</p>
          {lead ? (
            <>
              <p className="adm-tile-value adh-tile-small"><OutcomeBadge outcome={lead.outcome} say={say} /></p>
              <p className="adm-tile-note">{lead.id} · {say(`certainty_${lead.certainty}` as HealthWord)}</p>
            </>
          ) : <p className="adm-tile-value adh-tile-small">{say("none")}</p>}
        </div>
        <div className="adm-tile adh-tile">
          <p className="adm-tile-label">{say("tileQueue")}</p>
          {s.queue === null ? <p className="adm-tile-value adh-tile-small"><Unknown>{say("notVisible")}</Unknown></p> : (
            <>
              <p className="adm-tile-value">{s.queue.used === null || s.queue.limit === null ? <Unknown>{say("unknown")}</Unknown> : say("tileQueueValue", { used: s.queue.used, limit: s.queue.limit })}</p>
              {s.queue.used !== null && s.queue.limit ? (
                <span className="adm-meter" aria-hidden="true"><span style={{ width: `${Math.min(100, (s.queue.used / s.queue.limit) * 100)}%` }} /></span>
              ) : null}
              <p className="adm-tile-note">{say("tileQueueOldest", { age: healthDuration(say, s.queue.oldest_s) })}</p>
              {s.queue.as_of ? <p className="adm-tile-note">{say("tileQueueAsOf", { time: healthClock(s.queue.as_of) })}</p> : null}
            </>
          )}
        </div>
        <div className="adm-tile adh-tile">
          <p className="adm-tile-label">{say("tileErrors")}</p>
          <p className="adm-tile-value"><RateText ratio={s.errors} say={say} /></p>
          {rate.kind === "rate" ? <p className="adm-tile-note">{say("tileErrorsOf", { count: rate.count, denominator: rate.denominator })}</p> : null}
        </div>
        <div className="adm-tile adh-tile">
          <p className="adm-tile-label">{say("tileLatency")}</p>
          <p className="adm-tile-value"><LatencyText latency={s.latency} say={say} /></p>
          {lat.kind === "values" ? <p className="adm-tile-note">{say("tileLatencyMore", { p95: healthMs(say, lat.p95), p99: lat.tooFewForP99 ? say("tooFew", { n: lat.samples }) : healthMs(say, lat.p99), n: lat.samples })}</p> : null}
        </div>
      </div>
      <p className="adm-fine">{say("windowFigures", { start: healthClock(s.window.start), end: healthClock(s.window.end) })}</p>

      <Freshness snapshot={s} say={say} now={now} nav={nav} />

      <Timeline title={say("timelineTitle")} lanes={s.lanes} window={s.window} label={segLabel(say)} incidents={s.incidents} say={say} nav={nav} />

      <div className="adh-two">
        <IncidentList incidents={ranked} say={say} nav={nav} />
        <Panel title={say("segTitle")}>
          <HTable
            caption={say("segTitle")}
            columns={[{ label: say("colSegment") }, { label: say("colOutcome") }, { label: say("colQuality") }, { label: "p50", num: true }, { label: "p95", num: true }, { label: "p99", num: true }, { label: say("colSamples"), num: true }, { label: say("colErrors"), num: true }, { label: say("colAge"), num: true }]}
            rows={s.segments.map((r) => ({
              key: r.segment,
              head: segmentLink(r.segment, say, nav),
              cells: r.quality === "forbidden"
                ? [<OutcomeBadge key="o" outcome="unknown" say={say} />, <QualityChip key="q" quality="forbidden" say={say} />, "—", "—", "—", "—", "—", "—"]
                : [<OutcomeBadge key="o" outcome={r.outcome} say={say} />, <QualityChip key="q" quality={r.quality} say={say} />, ...latencyCells(r.latency, say), <RateText key="e" ratio={r.errors} say={say} />, <Age key="a" seconds={r.source_age_s} say={say} />],
            }))}
          />
        </Panel>
      </div>
    </div>
  )
}

function segmentLink(seg: Lane["segment"], say: HealthSay, nav: ScreenProps["nav"]) {
  const word = say(`seg_${seg}` as HealthWord)
  if (seg.startsWith("machine_")) return <NavLink to={{ screen: "M02", machine: null }} nav={nav}>{word}</NavLink>
  if (seg === "relay") return <NavLink to={{ screen: "M03" }} nav={nav}>{word}</NavLink>
  if (seg === "collector") return <NavLink to={{ screen: "M06" }} nav={nav}>{word}</NavLink>
  return word
}

function Freshness({ snapshot: s, say, now }: SnapProps) {
  return (
    <section className="adh-fresh" aria-labelledby="adh-fresh-h">
      <h2 className="adm-h2" id="adh-fresh-h">{say("freshTitle")}</h2>
      <ul className="adh-fresh-list">
        {s.sources.map((src) => {
          const age = sourceAge(src, now)
          return (
            <li key={src.id} className={`adh-fresh-card adh-f-${src.state}`}>
              <p className="adh-fresh-name">{say(`kind_${src.kind}` as HealthWord)}</p>
              <p className="adh-fresh-state">{say(`fresh_${src.state}` as HealthWord)}</p>
              <p className="adm-fine">{age === null ? say("freshNever") : say("freshLast", { age: healthDuration(say, age) })}</p>
              {src.expected_every_s !== null ? <p className="adm-fine">{say("freshEvery", { every: healthDuration(say, src.expected_every_s) })}</p> : null}
              <p className="adm-fine">{src.retention_s === null ? say("freshKeptUnknown") : say("freshKept", { kept: healthDuration(say, src.retention_s) })}</p>
              {src.retention_floor_at ? <p className="adm-fine">{say("freshFloor", { time: healthClock(src.retention_floor_at) })}</p> : null}
              {src.gaps.length ? <p className="adm-fine">{say("freshGaps", { n: src.gaps.length })}</p> : null}
            </li>
          )
        })}
      </ul>
    </section>
  )
}

function IncidentList({ incidents, say, nav }: { incidents: Incident[]; say: HealthSay; nav: ScreenProps["nav"] }) {
  return (
    <Panel title={say("incidentsTitle")}>
      {incidents.length === 0 ? <p className="adm-fine">{say("incidentsNone")}</p> : (
        <ul className="adh-incidents">
          {incidents.map((inc) => (
            <li key={inc.id} className="adh-incident">
              <OutcomeBadge outcome={inc.outcome} say={say} />
              <div className="adh-incident-body">
                <p className="adh-incident-title">{inc.title} <span className="adh-id">{inc.id}</span></p>
                <p className="adm-fine">
                  {say(`incState_${inc.state}` as HealthWord)} · {healthClock(inc.started_at)}–{inc.ended_at ? healthClock(inc.ended_at) : say("ongoing")} UTC · {say(`certainty_${inc.certainty}` as HealthWord)}
                  {inc.affected ? ` · ${say(inc.affected.lower_bound ? "affectedLower" : "affected", { n: inc.affected.count })}` : ""}
                </p>
              </div>
              <NavLink to={{ screen: "M05", incident: inc.id }} nav={nav} className="adm-btn adm-btn-small">{say("open")}</NavLink>
            </li>
          ))}
        </ul>
      )}
    </Panel>
  )
}

function Correlated({ snapshot: s, say, nav, filter }: SnapProps & { filter: (i: Incident) => boolean }) {
  return <IncidentList incidents={rankIncidents(s.incidents.filter(filter))} say={say} nav={nav} />
}

/* ---- M02 Machine connections ---------------------------------------------- */

export function M02Machines({ snapshot: s, say, now, nav, machine }: SnapProps & { machine: string | null }) {
  const shown: Machine[] = machine === null ? s.machines : s.machines.filter((m) => m.id === machine)
  return (
    <div className="adh-screen">
      <Crumb say={say} nav={nav} />
      {shown.length === 0 ? <p className="adm-fine">{say("machinesNone")}</p> : shown.map((m) => (
        <div key={m.id} className="adh-screen">
          <Panel title={m.id}>
            <dl className="adh-facts">
              <div><dt>{say("verdictOutcome")}</dt><dd>{presenceText(m, say, now)}</dd></div>
              <div><dt>{say("factBoot")}</dt><dd>{m.boot_id ?? <Unknown>{say("unknown")}</Unknown>}{m.boot_at ? ` · ${healthClock(m.boot_at)} UTC` : ""}</dd></div>
              <div><dt>{say("factBuild")}</dt><dd>{m.build ?? <Unknown>{say("unknown")}</Unknown>}</dd></div>
              <div><dt>{say("factPlatform")}</dt><dd>{m.platform ?? <Unknown>{say("unknown")}</Unknown>}</dd></div>
              <div><dt>{say("factSnapshot")}</dt><dd><Age seconds={ageSince(m.snapshot_at, now)} say={say} /></dd></div>
            </dl>
            {m.presence === "expired" ? <p className="adm-fine">{say("offlineNote")}</p> : null}
          </Panel>
          <Timeline title={say("machineLanes")} lanes={m.lanes} window={s.window} label={segLabel(say)} incidents={[]} say={say} nav={nav} />
          <Panel title={say("carriersTitle")}>
            {m.carriers === null ? <p className="adm-fine">{say("notVisible")}</p> : (
              <HTable caption={say("carriersTitle")}
                columns={[{ label: say("colViewer") }, { label: say("colRead") }, { label: say("colCommand") }, { label: say("colState") }, { label: say("colGeneration"), num: true }, { label: say("colLastSeen") }, { label: say("colReplaced"), num: true }, { label: say("colRefused"), num: true }]}
                rows={m.carriers.map((c) => ({
                  key: c.viewer, head: c.viewer,
                  cells: [c.read_carrier, c.command_carrier, <OutcomeBadge key="s" outcome={c.state} say={say} />, num(c.generation, say), at(c.last_seen_at, say), num(c.replaced, say), num(c.refused, say)],
                }))} />
            )}
          </Panel>
          <Panel title={say("replacementsTitle")}>
            {m.replacements === null ? <p className="adm-fine">{say("notReported")}</p> : m.replacements.length === 0 ? <p className="adm-fine">{say("replacementsNone")}</p> : (
              <HTable caption={say("replacementsTitle")}
                columns={[{ label: say("tableFrom") }, { label: say("colViewer") }, { label: say("colGeneration") }, { label: say("colState") }]}
                rows={m.replacements.slice(-6).reverse().map((r, i) => ({
                  key: `${r.at}-${i}`, head: `${healthClock(r.at)} UTC`,
                  cells: [r.viewer, `${r.from_generation ?? "?"} → ${r.to_generation ?? "?"}`, r.reason ?? <Unknown>{say("notReported")}</Unknown>],
                }))} />
            )}
          </Panel>
          <Panel title={say("holdersTitle")}>
            {m.holders === null ? <p className="adm-fine">{say("notVisible")}</p> : (
              <HTable caption={say("holdersTitle")}
                columns={[{ label: say("colStage") }, { label: say("colHolder") }, { label: say("colAge"), num: true }, { label: say("colDeadline"), num: true }, { label: say("colProgress") }]}
                rows={m.holders.map((h) => ({
                  key: h.stage, head: h.stage,
                  cells: [h.holder ?? <Unknown>{say("none")}</Unknown>, <Age key="a" seconds={h.age_s} say={say} />, <Age key="d" seconds={h.deadline_s} say={say} />,
                    <span key="p">{at(h.last_progress_at, say)}{h.as_of ? ` · ${say("asOf", { time: healthClock(h.as_of) })}` : ""}</span>],
                }))} />
            )}
          </Panel>
        </div>
      ))}
      <Correlated snapshot={s} say={say} now={now} nav={nav} filter={(i) => i.segment.startsWith("machine_")} />
    </div>
  )
}

function presenceText(m: Machine, say: HealthSay, now: number) {
  const age = healthDuration(say, ageSince(m.presence_at, now))
  if (m.presence === "renewed") return <OutcomeLine outcome="healthy" text={say("presence_renewed", { age })} />
  if (m.presence === "expired") return <OutcomeLine outcome="offline" text={say("presence_expired", { age })} />
  return <OutcomeLine outcome="unknown" text={say("presence_unknown")} />
}

function OutcomeLine({ outcome, text }: { outcome: "healthy" | "offline" | "unknown"; text: string }) {
  const glyph = outcome === "healthy" ? "✓" : outcome === "offline" ? "○" : "?"
  return <span className={`adh-badge adh-o-${outcome}`}><span aria-hidden="true" className="adh-glyph">{glyph}</span>{text}</span>
}

const num = (v: number | null, say: HealthSay) => (v === null ? <Unknown>{say("unknown")}</Unknown> : String(v))

/* ---- M03 Cloud services --------------------------------------------------- */

export function M03Services({ snapshot: s, say, now, nav }: SnapProps) {
  const sv = s.services
  return (
    <div className="adh-screen">
      <Crumb say={say} nav={nav} />
      {sv === null ? <p className="adm-fine">{say("servicesNone")}</p> : (
        <>
          <Panel title={say("componentsTitle")}>
            <HTable caption={say("componentsTitle")}
              columns={[{ label: say("colComponent") }, { label: say("colOutcome") }, { label: say("colQuality") }, { label: say("factBuild") }, { label: say("colInstance") }, { label: say("colAge"), num: true }, { label: say("colImpact") }]}
              rows={sv.components.map((c) => ({
                key: c.id, head: c.id,
                cells: [<OutcomeBadge key="o" outcome={c.outcome} say={say} />, <QualityChip key="q" quality={c.quality} say={say} />, c.build ?? <Unknown>{say("unknown")}</Unknown>, c.instance ?? <Unknown>{say("unknown")}</Unknown>, <Age key="a" seconds={c.source_age_s} say={say} />, c.impact ?? "—"],
              }))} />
          </Panel>
          <Timeline title={say("serviceLanes")} lanes={sv.lanes} window={s.window} label={segLabel(say)} incidents={[]} say={say} nav={nav} />
          <Panel title={say("relayStagesTitle")}>
            <p className="adh-notice">{say("socketNote")}</p>
            {sv.stages === null ? <p className="adm-fine">{say("notVisible")}</p> : (
              <HTable caption={say("relayStagesTitle")}
                columns={[{ label: say("colStage") }, { label: "p50", num: true }, { label: "p95", num: true }, { label: say("colSamples"), num: true }, { label: say("colPending"), num: true }, { label: say("colOldestPending"), num: true }]}
                rows={sv.stages.map((r) => ({
                  key: r.id, head: r.id,
                  cells: r.samples === 0
                    ? [<Unknown key="a">{say("unknown")}</Unknown>, <Unknown key="b">{say("unknown")}</Unknown>, "0", num(r.pending, say), <Age key="o" seconds={r.oldest_pending_s} say={say} />]
                    : [healthMs(say, r.p50_ms), healthMs(say, r.p95_ms), String(r.samples), num(r.pending, say), <Age key="o" seconds={r.oldest_pending_s} say={say} />],
                }))} />
            )}
          </Panel>
          <div className="adh-two">
            <Panel title={say("waitsTitle")}>
              {sv.waits === null ? <p className="adm-fine">{say("notVisible")}</p> : (
                <HTable caption={say("waitsTitle")}
                  columns={[{ label: say("colWait") }, { label: "p95", num: true }, { label: say("colSamples"), num: true }, { label: say("colErrors"), num: true }]}
                  rows={sv.waits.map((w) => ({ key: w.id, head: w.id, cells: [w.samples === 0 ? <Unknown key="p">{say("unknown")}</Unknown> : healthMs(say, w.p95_ms), String(w.samples), <RateText key="e" ratio={w.errors} say={say} />] }))} />
              )}
            </Panel>
            <Panel title={say("saturationTitle")}>
              {sv.saturation === null ? <p className="adm-fine">{say("notVisible")}</p> : (
                <HTable caption={say("saturationTitle")}
                  columns={[{ label: say("colComponent") }, { label: say("colUsed"), num: true }, { label: say("colLimit"), num: true }, { label: say("colOldestPending"), num: true }]}
                  rows={sv.saturation.map((r) => ({ key: r.id, head: r.id, cells: [num(r.used, say), num(r.limit, say), <Age key="o" seconds={r.oldest_pending_s} say={say} />] }))} />
              )}
            </Panel>
          </div>
        </>
      )}
      <Correlated snapshot={s} say={say} now={now} nav={nav} filter={(i) => i.segment === "relay" || i.segment === "collector"} />
      <p><NavLink to={{ screen: "M06" }} nav={nav}>{say("screen_M06")}</NavLink></p>
    </div>
  )
}

/* ---- M04 Request trace ---------------------------------------------------- */

export function M04Trace({ snapshot: s, say, now, nav, trace: id }: SnapProps & { trace: string }) {
  const t = s.traces.find((x) => x.id === id)
  if (!t) return <div className="adh-screen"><Crumb say={say} nav={nav} /><p className="adm-fine">{say("traceNotFound")}</p></div>
  const progress = stageProgress(t)
  const offsets = stageOffsets(t)
  const max = Math.max(1, ...[...offsets.values()].map((v) => v ?? 0))
  let previous: number | null = null
  const rows = t.stages.map((st) => {
    const off = offsets.get(st.stage) ?? null
    const delta = off !== null && previous !== null ? off - previous : null
    if (off !== null) previous = off
    return { st, off, delta }
  })
  const openFor = (stage: Stage) => t.open_spans.find((sp) => sp.stage === stage) ?? null
  return (
    <div className="adh-screen">
      <Crumb say={say} nav={nav} />
      <Panel title={t.operation}>
        <dl className="adh-facts">
          <div><dt>{say("factTrace")}</dt><dd className="adh-id">{t.id}</dd></div>
          <div><dt>{say("traceAttempts")}</dt><dd>{t.attempts}</dd></div>
          <div><dt>{say("factBuild")}</dt><dd>{t.build ?? <Unknown>{say("unknown")}</Unknown>}</dd></div>
          <div><dt>{say("lastProven")}</dt><dd>{progress.last_proven_stage ? stageWord(say, progress.last_proven_stage) : say("none")}</dd></div>
          <div><dt>{say("activeStage")}</dt><dd>{progress.active_stage ? stageWord(say, progress.active_stage) : say("none")}</dd></div>
          <div><dt>{say("nextUnobserved")}</dt><dd>{progress.next_unobserved_stage ? stageWord(say, progress.next_unobserved_stage) : say("none")}</dd></div>
          {progress.visibility_limit_at ? <div><dt>{say("visibilityLimit")}</dt><dd>{stageWord(say, progress.visibility_limit_at)}</dd></div> : null}
        </dl>
        {t.incident_id ? <p><NavLink to={{ screen: "M05", incident: t.incident_id }} nav={nav}>{say("screen_M05")} {t.incident_id}</NavLink></p> : null}
      </Panel>
      <Panel title={say("waterfallTitle")}>
        <ol className="adh-waterfall">
          {rows.map(({ st, off }) => {
            const span = st.state === "active" || st.state === "no_receipt" ? openFor(st.stage) : null
            let text: string
            if (st.state === "received") text = healthMs(say, off)
            else if (span) text = say("stageState_active", { age: healthDuration(say, ageSince(span.started_at, now)) })
            else if (st.state === "active") text = say("stageActiveUnknown")
            else text = say(`stageState_${st.state}` as HealthWord)
            return (
              <li key={st.stage} className={`adh-wf adh-wf-${span ? "active" : st.state}${st.late ? " adh-wf-late" : ""}`}>
                <span className="adh-wf-name">{stageWord(say, st.stage)}</span>
                <span className="adh-wf-time">{st.late ? <><span aria-hidden="true">▲ </span>{say("late")} · </> : null}{text}</span>
                <span className="adh-wf-track" aria-hidden="true">
                  {st.state === "received" ? <span className="adh-wf-bar" style={{ width: `${logWidth(off ?? 0, max)}%` }} /> : <span className="adh-wf-empty" />}
                </span>
              </li>
            )
          })}
        </ol>
        <HTable caption={say("waterfallTitle")}
          columns={[{ label: say("colStage") }, { label: say("colOffset"), num: true }, { label: say("colDelta"), num: true }, { label: say("colRecorder") }, { label: say("colState") }]}
          rows={rows.map(({ st, off, delta }) => ({
            key: st.stage, head: stageWord(say, st.stage),
            cells: [off === null ? "—" : healthMs(say, off), delta === null ? "—" : healthMs(say, delta), st.recorder ?? "—", st.state === "received" ? (st.late ? say("late") : "✓") : st.state === "active" ? say("activeStage") : say(`stageState_${st.state}` as HealthWord)],
          }))} />
      </Panel>
      {t.open_spans.length ? (
        <Panel title={say("openSpansTitle")}>
          <HTable caption={say("openSpansTitle")}
            columns={[{ label: say("colStage") }, { label: say("colHolder") }, { label: say("colStarted") }, { label: say("colProgress") }, { label: say("colDeadline"), num: true }]}
            rows={t.open_spans.map((sp, i) => ({ key: `${sp.stage}-${i}`, head: stageWord(say, sp.stage), cells: [sp.holder, at(sp.started_at, say), at(sp.last_progress_at, say), healthMs(say, sp.deadline_ms)] }))} />
        </Panel>
      ) : null}
      <Locations locations={t.locations} say={say} />
    </div>
  )
}

function Locations({ locations, say }: { locations: HealthSnapshot["traces"][number]["locations"]; say: HealthSay }) {
  return (
    <Panel title={say("locationsTitle")}>
      <p className="adm-fine">{say("locationsNote")}</p>
      {locations.length === 0 ? <p className="adm-fine">{say("none")}</p> : (
        <HTable caption={say("locationsTitle")}
          columns={[{ label: say("colCode") }, { label: say("colWhere") }, { label: say("colImpact") }]}
          rows={locations.map((l) => ({
            key: l.code, head: <code>{l.code}</code>,
            cells: [l.path ? <code key="p">{l.path}{l.line !== null ? `:${l.line}` : ""}{l.symbol ? ` ${l.symbol}` : ""}</code> : "—", l.note ?? "—"],
          }))} />
      )}
    </Panel>
  )
}

/* ---- M05 Incident investigation ------------------------------------------- */

export function M05Incident({ snapshot: s, say, nav, incident: id }: SnapProps & { incident: string }) {
  const inc = s.incidents.find((i) => i.id === id)
  if (!inc) return <div className="adh-screen"><Crumb say={say} nav={nav} /><p className="adm-fine">{say("incidentNotFound")}</p></div>
  const facts = inc.facts.filter((f) => !f.forbidden)
  const hyps = inc.hypotheses.filter((h) => !h.forbidden)
  const trace = inc.trace_id ? s.traces.find((t) => t.id === inc.trace_id) ?? null : null
  return (
    <div className="adh-screen">
      <Crumb say={say} nav={nav} />
      <Panel title={`${inc.title} · ${inc.id}`}>
        <p className="adh-badges">
          <OutcomeBadge outcome={inc.outcome} say={say} />
          <span className={`adh-chip adh-c-${inc.certainty}`}>{say(`certainty_${inc.certainty}` as HealthWord)}</span>
          <span className="adh-chip">{say(`incState_${inc.state}` as HealthWord)}</span>
          <span className="adh-chip">{say(`recovery_${inc.recovery}` as HealthWord)}</span>
        </p>
        <dl className="adh-facts">
          <div><dt>{say("factStart")}</dt><dd>{healthClock(inc.started_at)} UTC</dd></div>
          <div><dt>{say("factEnd")}</dt><dd>{inc.ended_at ? `${healthClock(inc.ended_at)} UTC` : say("ongoing")}</dd></div>
          <div><dt>{say("factSegment")}</dt><dd>{segmentLink(inc.segment, say, nav)}</dd></div>
          <div><dt>{say("factBuild")}</dt><dd>{inc.build ?? <Unknown>{say("unknown")}</Unknown>}</dd></div>
          {inc.affected ? <div><dt>{say("colImpact")}</dt><dd>{say(inc.affected.lower_bound ? "affectedLower" : "affected", { n: inc.affected.count })}</dd></div> : null}
          {inc.rule_version ? <div><dt>{say("colSchema")}</dt><dd>{say("ruleVersion", { rule: inc.rule_version })}</dd></div> : null}
        </dl>
        <p className="adh-verdict-links">
          {trace ? <NavLink to={{ screen: "M04", trace: trace.id }} nav={nav} className="adm-btn adm-btn-small">{say("openTrace")}</NavLink> : null}
          <NavLink to={{ screen: "evidence", incident: inc.id }} nav={nav} className="adm-btn adm-btn-small">{say("openEvidence")}</NavLink>
        </p>
      </Panel>
      <div className="adh-three">
        <Panel title={say("factsTitle")}>
          {facts.length === 0 ? <p className="adm-fine">{say("factsNone")}</p> : (
            <ul className="adh-evidence">{facts.map((f) => <li key={f.id}><p>{f.statement}</p><p className="adm-fine">{f.source}{f.observed_at ? ` · ${healthClock(f.observed_at)} UTC` : ""}</p></li>)}</ul>
          )}
          <HiddenCount n={inc.facts.length - facts.length} say={say} />
        </Panel>
        <Panel title={say("hypothesesTitle")}>
          {hyps.length === 0 ? <p className="adm-fine">{say("hypothesesNone")}</p> : (
            <ul className="adh-evidence">{hyps.map((h) => <li key={h.id}><p>{h.statement}</p><p className="adm-fine">{say(`basis_${h.basis}` as HealthWord)}{h.sources.length ? ` · ${h.sources.join(", ")}` : ""}</p></li>)}</ul>
          )}
          <HiddenCount n={inc.hypotheses.length - hyps.length} say={say} />
        </Panel>
        <Panel title={say("missingTitle")}>
          {inc.missing.length === 0 ? <p className="adm-fine">{say("missingNone")}</p> : (
            <ul className="adh-evidence">{inc.missing.map((m) => <li key={m.id}><p>{m.what}</p>{m.next_step ? <p className="adm-fine">{say("nextStep", { step: m.next_step })}</p> : null}</li>)}</ul>
          )}
        </Panel>
      </div>
      {trace ? <Locations locations={trace.locations} say={say} /> : null}
    </div>
  )
}

/* ---- M06 Monitoring status ------------------------------------------------ */

export function M06Monitoring({ snapshot: s, say, now, nav }: SnapProps) {
  const p = s.probe
  return (
    <div className="adh-screen">
      <Crumb say={say} nav={nav} />
      <Panel title={say("freshTitle")}>
        <HTable caption={say("freshTitle")}
          columns={[{ label: say("colSource") }, { label: say("colFreshness") }, { label: say("colAge"), num: true }, { label: say("colGaps"), num: true }, { label: say("colDropped"), num: true }, { label: say("colSchema") }, { label: say("colRetention") }]}
          rows={s.sources.map((src) => ({
            key: src.id, head: say(`kind_${src.kind}` as HealthWord),
            cells: [
              say(`fresh_${src.state}` as HealthWord),
              <Age key="a" seconds={sourceAge(src, now)} say={say} />,
              String(src.gaps.length),
              num(src.dropped, say),
              src.schema ?? <Unknown>{say("notReported")}</Unknown>,
              <span key="r">{src.retention_s === null ? say("freshKeptUnknown") : healthDuration(say, src.retention_s)}{src.retention_floor_at ? ` · ${say("freshFloor", { time: healthClock(src.retention_floor_at) })}` : ""}</span>,
            ],
          }))} />
      </Panel>
      <Panel title={say("probeTitle")}>
        {p === null ? <p className="adm-fine">{say("probeNone")}</p> : (
          <>
            <p className="adm-fine">{say("probeLead", { every: healthDuration(say, p.every_s) })}</p>
            <p className="adm-fine">{p.last_at ? say("probeLast", { age: healthDuration(say, ageSince(p.last_at, now)) }) : say("probeNone")}</p>
            <HTable caption={say("probeTitle")}
              columns={[{ label: say("colEdge") }, { label: say("colResult") }]}
              rows={p.edges.map((e) => ({ key: e.edge, head: e.edge, cells: [<span key="r" className={`adh-probe adh-p-${e.result}`}>{say(`probe_${e.result}` as HealthWord)}</span>] }))} />
          </>
        )}
      </Panel>
    </div>
  )
}

/* ---- Agent evidence -------------------------------------------------------- */

export function EvidenceView({ snapshot: s, say, nav, incident: id }: SnapProps & { incident: string }) {
  const [exportedAt] = useState(() => new Date().toISOString())
  const built = useMemo(() => buildEvidence(s, id, exportedAt), [s, id, exportedAt])
  const [status, setStatus] = useState<string | null>(null)
  const pre = useRef<HTMLPreElement>(null)
  if (!built.ok) {
    return (
      <div className="adh-screen">
        <Crumb say={say} nav={nav} />
        <p className="adm-fine">{built.reason === "no_incident" ? say("incidentNotFound") : say("evidenceTooLarge", { kb: EVIDENCE_MAX_BYTES / 1024 })}</p>
      </div>
    )
  }
  const name = `connection-evidence-${id.replace(/[^A-Za-z0-9_-]/g, "_")}.json`
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(built.json)
      setStatus(say("copied"))
    } catch {
      const range = document.createRange()
      if (pre.current) {
        range.selectNodeContents(pre.current)
        const sel = window.getSelection()
        sel?.removeAllRanges()
        sel?.addRange(range)
      }
      setStatus(say("copyFailed"))
    }
  }
  const download = () => {
    const url = URL.createObjectURL(new Blob([built.json], { type: "application/json" }))
    const a = document.createElement("a")
    a.href = url
    a.download = name
    a.click()
    setTimeout(() => URL.revokeObjectURL(url), 0)
    setStatus(say("downloaded", { name }))
  }
  return (
    <div className="adh-screen">
      <Crumb say={say} nav={nav} />
      <Panel title={`${say("screen_evidence")} · ${id}`} actions={
        <>
          <button type="button" className="adm-btn adm-btn-small" onClick={() => void copy()}>{say("copy")}</button>
          <button type="button" className="adm-btn adm-btn-small" onClick={download}>{say("download")}</button>
        </>
      }>
        <p className="adm-fine">{say("evidenceLead")}</p>
        {s.example ? <p className="adh-notice">{say("evidenceExampleNote")}</p> : null}
        <p className="adm-fine" role="status" aria-live="polite">{status ?? say("evidenceSize", { kb: Math.ceil(built.bytes / 1024) })}</p>
        <pre ref={pre} className="adh-json" tabIndex={0}>{built.json}</pre>
      </Panel>
    </div>
  )
}
