/* The trend view of M01, M03 and M06: the Cloud's stored minute rollups over 6 h to 30 days
 * (aggregates.ts), drawn as one lane per source with every bucket either a figure or a labelled
 * unknown. It never fills a missing bucket, never divides by an unknown denominator, never adds
 * stage times into a latency, and never draws a line across a restart. */
import type { ReactNode } from "react"
import { partBody, type StoredState } from "./cloud"
import { stageElapsed, trendLanes, viewerErrors, type AggregatesBody, type Slot, type TrendLane } from "./aggregates"
import { healthHref, STORED_WINDOWS_S, type HealthRoute, type HealthScreen, type StoredWindow } from "./route"
import { healthMs, type HealthSay, type HealthWord } from "./words"
import { HTable, linkClick, Panel, Unknown } from "./parts"
import { ChartPanel, KnownStrip } from "./timeline-view"
import { exactUTC, Id, PartFailed, Stale } from "./stored-screens"
import { IncidentCoverage } from "./incident-screens"

/** Lanes drawn as charts; the rest stay in the table. Registered as console.health_ui_trend_lanes. */
export const TREND_LANES = 12
/** Investigation rows listed under the chart, newest first. Registered as console.health_ui_trend_signals. */
export const TREND_SIGNALS = 20
/** Raw records the Cloud keeps (contract §3): older buckets can only link to incidents. */
const RAW_RETENTION_MS = 86_400_000

const fmt = (n: number) => n.toLocaleString("en-US")
const SIGNAL_GLYPH: Record<Slot["signals"][number], string> = { stall: "■", slow: "▲", failed: "✕", loss: "⋯" }

function slotLabel(say: HealthSay, s: Slot): string {
  if (s.gap !== null) return `${exactUTC(new Date(s.start).toISOString())} · ${say(`tr_gap_${s.gap}` as HealthWord)}`
  const parts = [exactUTC(new Date(s.start).toISOString()), say("tr_records", { n: fmt(s.records ?? 0) })]
  if (s.operations) parts.push(say(s.operations.floor ? "tr_opsFloor" : "tr_ops", { n: fmt(s.operations.n) }))
  for (const sig of s.signals) parts.push(say(`tr_sig_${sig}` as HealthWord))
  if (s.restart) parts.push(say("tr_restart"))
  if (s.incomplete) parts.push(say("tr_incomplete"))
  return parts.join(" · ")
}

/** One lane: a bar per held bucket, a hatched block per unknown one, a break mark per restart. */
function LaneChart({ lane, say }: { lane: TrendLane; say: HealthSay }) {
  const n = lane.slots.length
  const h = 40
  return (
    <svg className="adh-tr-svg" viewBox={`0 0 ${n} ${h}`} preserveAspectRatio="none" role="img"
      aria-label={say("tr_laneAria", { records: fmt(lane.totals.records), held: lane.totals.heldBuckets, unknown: lane.totals.unknownBuckets })}>
      {lane.slots.map((s, i) => {
        if (s.gap !== null) return <rect key={i} x={i} y={0} width={1} height={h} className={`adh-tr-gap adh-tr-gap-${s.gap}`}><title>{slotLabel(say, s)}</title></rect>
        const bar = Math.max(s.records! > 0 ? 2 : 0.6, ((s.records ?? 0) / lane.max) * (h - 8))
        const bad = s.signals.length > 0
        return (
          <g key={i}>
            <rect x={i + 0.1} y={h - bar} width={0.8} height={bar} className={bad ? "adh-tr-bar adh-tr-bar-bad" : s.incomplete ? "adh-tr-bar adh-tr-bar-floor" : "adh-tr-bar"}><title>{slotLabel(say, s)}</title></rect>
            {s.restart ? <line x1={i} x2={i} y1={0} y2={h} className="adh-tr-restart" /> : null}
            {bad ? <rect x={i + 0.1} y={0} width={0.8} height={4} className="adh-tr-signal" /> : null}
          </g>
        )
      })}
    </svg>
  )
}

/** The raw-evidence window that best holds one bucket: the smallest the page offers that covers it. */
function rawWindow(bucketMs: number): StoredWindow {
  return STORED_WINDOWS_S.find((w) => w * 1000 >= bucketMs) ?? STORED_WINDOWS_S[STORED_WINDOWS_S.length - 1]
}

function Go({ href, go, children }: { href: string; go: (href: string) => void; children: ReactNode }) {
  return <a className="adh-link" href={href} onClick={(e) => linkClick(e, () => go(href))}>{children}</a>
}

/** Where a stalled, slow, failed or lossy bucket can be investigated, in the same account scope. */
function SignalRows({ body, lanes, route, say, go }: { body: AggregatesBody; lanes: TrendLane[]; route: HealthRoute; say: HealthSay; go: (href: string) => void }) {
  const asOf = Date.parse(body.as_of)
  const rows = lanes.flatMap((lane) => lane.slots.filter((s) => s.gap === null && s.signals.length > 0).map((s) => ({ lane, s })))
    .sort((a, b) => b.s.start - a.s.start)
  if (rows.length === 0) return <p className="adm-fine">{say("tr_noSignals")}</p>
  const shown = rows.slice(0, TREND_SIGNALS)
  return (
    <>
      <ul className="adh-tr-signals">
        {shown.map(({ lane, s }) => {
          const until = new Date(Math.min(s.end, asOf)).toISOString()
          const scope = { ...route.scope, until, window_s: rawWindow(body.window.bucket_ms) }
          const raw: HealthScreen = { screen: "M03", source: lane.series.source, sourceId: lane.series.source_id }
          const rawKept = s.end > asOf - RAW_RETENTION_MS
          return (
            <li key={`${lane.key}|${s.start}`}>
              <span aria-hidden="true" className="adh-glyph">{s.signals.map((g) => SIGNAL_GLYPH[g]).join("")}</span>{" "}
              <strong>{s.signals.map((g) => say(`tr_sig_${g}` as HealthWord)).join(" · ")}</strong>{" · "}
              <Id v={lane.series.machine_id} /> · {say(`tr_source_${lane.series.source}` as HealthWord)} · <time dateTime={new Date(s.start).toISOString()}>{exactUTC(new Date(s.start).toISOString())}</time>
              <span className="adm-fine">{" · "}
                {rawKept ? <Go href={healthHref(raw, scope)} go={go}>{say("tr_openRaw")}</Go> : say("tr_rawExpired")}
                {" · "}<Go href={healthHref({ screen: "M05", incident: null }, scope)} go={go}>{say("tr_openIncidents")}</Go>
              </span>
            </li>
          )
        })}
      </ul>
      {rows.length > shown.length ? <p className="adm-fine">{say("tr_moreSignals", { n: rows.length - shown.length })}</p> : null}
    </>
  )
}

function CoverageNotes({ body, say }: { body: AggregatesBody; say: HealthSay }) {
  const r = body.rollup
  const notes: ReactNode[] = []
  if (body.partial_reason) notes.push(say("tr_partial", { reason: say(`tr_reason_${body.partial_reason}` as HealthWord) }))
  if (r.retention_clipped) notes.push(say("tr_retentionClipped", { floor: exactUTC(r.retention_floor) }))
  if (Date.parse(r.covered_from) > Date.parse(body.window.from) && body.partial_reason === "scan_cap") notes.push(say("tr_coveredFrom", { at: exactUTC(r.covered_from) }))
  if (body.series_total > body.series.length) notes.push(say("tr_seriesMore", { shown: body.series.length, total: body.series_total }))
  if (r.cells_dropped > 0) notes.push(say("tr_cellsDropped", { n: fmt(r.cells_dropped) }))
  for (const x of r.refusals) notes.push(say("tr_refusal", { day: exactUTC(x.day).slice(0, 10), reason: x.reason, n: fmt(x.batches) }) + (x.account ? ` · ${x.account}` : ""))
  if (r.refusals_truncated) notes.push(say("tr_refusalsMore"))
  return (
    <Panel title={say("tr_coverageTitle")}>
      <ul className="adh-tr-notes">
        <li>{say("tr_absentUnknown")}</li>
        <li>{say("tr_clockBasis")}</li>
        <li>{say("tr_countsBasis")}</li>
        {r.cell_filter === "applied" ? <li>{say("tr_cellFilter")}</li> : null}
        {notes.map((n, i) => <li key={i} className="adh-tr-note-warn">{n}</li>)}
      </ul>
    </Panel>
  )
}

function lossText(say: HealthSay, l: TrendLane["series"]["producer_reported_loss"]): string {
  const total = l.storage_lost_records + l.store_gaps + l.excluded
  return total === 0 && l.counters === "monotonic" ? say("tr_lossNone") : say(l.counters === "unverified" ? "tr_lossUnverified" : "tr_loss", { lost: fmt(l.storage_lost_records), gaps: fmt(l.store_gaps), excluded: fmt(l.excluded) })
}

function LaneTable({ lanes, say }: { lanes: TrendLane[]; say: HealthSay }) {
  return (
    <HTable caption={say("tr_lanesTitle")}
      columns={[{ label: say("tr_colSource") }, { label: say("tr_colRecords"), num: true }, { label: say("tr_colOps"), num: true }, { label: say("tr_colFailed"), num: true }, { label: say("tr_colBuckets") }, { label: say("tr_colLoss") }]}
      rows={lanes.map((l) => ({
        key: l.key,
        head: <><Id v={l.series.machine_id} /> · {say(`tr_source_${l.series.source}` as HealthWord)} <span className="adm-fine">{l.series.producer_instance}</span></>,
        cells: [
          fmt(l.totals.records),
          l.totals.operations === null ? <Unknown>{say("tr_denomUnknown")}</Unknown> : say(l.totals.operations.floor ? "tr_opsFloor" : "tr_ops", { n: fmt(l.totals.operations.n) }),
          fmt(l.totals.failed),
          say("tr_bucketsHeld", { held: l.totals.heldBuckets, unknown: l.totals.unknownBuckets }),
          lossText(say, l.series.producer_reported_loss),
        ],
      }))} />
  )
}

function Lanes({ body, route, say, go }: { body: AggregatesBody; route: HealthRoute; say: HealthSay; go: (href: string) => void }) {
  const lanes = trendLanes(body)
  if (lanes.length === 0) {
    return (
      <Panel title={say("tr_lanesTitle")}>
        <p><span className={`adh-chip adh-q-${body.evidence_quality}`}>{say(`quality_${body.evidence_quality}` as HealthWord)}</span></p>
        <p className="adm-fine">{say("tr_noSeries")}</p>
      </Panel>
    )
  }
  const drawn = lanes.slice(0, TREND_LANES)
  const machineHref = (machine: string) => healthHref({ ...(route as HealthScreen), trendMachine: machine } as HealthScreen, route.scope)
  const chart = (
    <div className="adh-tr">
      <p className="adh-tr-axis"><span>{exactUTC(body.window.from)}</span><span>{exactUTC(body.window.to)}</span></p>
      <ul className="adh-tr-lanes">
        {drawn.map((l) => (
          <li key={l.key} className="adh-tr-row">
            <div className="adh-tr-name">
              <Id v={l.series.machine_id} /> · {say(`tr_source_${l.series.source}` as HealthWord)}
              <span className="adm-fine"> · {say(`tr_denom_${l.series.denominator}` as HealthWord)}</span>
              {route.screen !== "M03" && !("trendMachine" in route && route.trendMachine) ? <> · <Go href={machineHref(l.series.machine_id)} go={go}>{say("tr_onlyMachine")}</Go></> : null}
            </div>
            <LaneChart lane={l} say={say} />
            <p className="adh-tl-flags">
              {say("tr_laneLine", { records: fmt(l.totals.records), held: l.totals.heldBuckets, unknown: l.totals.unknownBuckets })}
              {l.totals.operations ? ` · ${say(l.totals.operations.floor ? "tr_opsFloor" : "tr_ops", { n: fmt(l.totals.operations.n) })}` : ` · ${say("tr_denomUnknown")}`}
              {l.slots.some((s) => s.restart) ? ` · ${say("tr_restarts", { n: l.slots.filter((s) => s.restart).length })}` : ""}
              {l.slots.some((s) => s.wallSkew > 0) ? ` · ${say("tr_wallSkew", { n: fmt(l.slots.reduce((a, s) => a + s.wallSkew, 0)) })}` : ""}
            </p>
          </li>
        ))}
      </ul>
      {lanes.length > drawn.length ? <p className="adm-fine">{say("tr_lanesMore", { shown: drawn.length, total: lanes.length })}</p> : null}
      <p className="adh-tr-key adm-fine">
        <span className="adh-tr-k adh-tr-k-bar" /> {say("tr_keyBar")} <span className="adh-tr-k adh-tr-k-bad" /> {say("tr_keyBad")}{" "}
        <span className="adh-tr-k adh-tr-k-gap" /> {say("tr_keyGap")} <span className="adh-tr-k adh-tr-k-old" /> {say("tr_keyOld")} <span className="adh-tr-k adh-tr-k-restart" /> {say("tr_keyRestart")}
      </p>
    </div>
  )
  return (
    <>
      <ChartPanel title={say("tr_lanesTitle")} say={say} chart={chart} table={<LaneTable lanes={lanes} say={say} />} />
      <Panel title={say("tr_signalsTitle")}><SignalRows body={body} lanes={lanes} route={route} say={say} go={go} /></Panel>
    </>
  )
}

function ViewerErrors({ body, say }: { body: AggregatesBody; say: HealthSay }) {
  const viewers = body.series.filter((s) => s.source === "viewer")
  const rows = viewerErrors(body)
  return (
    <Panel title={say("tr_viewerTitle")}>
      {viewers.length === 0 ? <p className="adm-fine">{say("tr_viewerNone")}</p> : rows.length === 0 ? <p className="adm-fine">{say("tr_viewerNoErrors")}</p> : (
        <HTable caption={say("tr_viewerTitle")} columns={[{ label: say("tr_colError") }, { label: say("tr_colRecords"), num: true }, { label: say("tr_colReported"), num: true }]}
          rows={rows.map((r) => ({ key: r.error, head: <code className="adh-id">{r.error}</code>, cells: [fmt(r.records), say(r.floor ? "tr_atLeast" : "tr_exactly", { n: fmt(r.reported) })] }))} />
      )}
      <p className="adm-fine">{say("tr_viewerNote")}</p>
    </Panel>
  )
}

function StageElapsed({ body, say }: { body: AggregatesBody; say: HealthSay }) {
  const rows = stageElapsed(body)
  const top = Math.max(1, ...rows.map((r) => r.max_ms ?? 0))
  return (
    <Panel title={say("tr_stageTitle")}>
      {rows.length === 0 ? <p className="adm-fine">{say("tr_stageNone")}</p> : (
        <ul className="adh-tr-stages">
          {rows.map((r) => (
            <li key={`${r.source}|${r.stage}`} className="adh-tr-stage">
              <span className="adh-tr-stage-name">{say(`tr_source_${r.source}` as HealthWord)} · <code className="adh-id">{r.stage}</code></span>
              <span className="adh-tr-stage-bar" aria-hidden="true"><span style={{ width: `${((r.max_ms ?? 0) / top) * 100}%` }} /></span>
              <span className="adm-fine">{say("tr_stageLine", { max: healthMs(say, r.max_ms), n: fmt(r.records) })}{" · "}
                {r.buckets.map((n, i) => n > 0 ? `${i < r.bounds_ms.length ? `≤${healthMs(say, r.bounds_ms[i])}` : `>${healthMs(say, r.bounds_ms[r.bounds_ms.length - 1])}`}: ${fmt(n)}` : null).filter(Boolean).join(" · ")}
              </span>
            </li>
          ))}
        </ul>
      )}
      <p className="adm-fine">{say("tr_stageNote")}</p>
    </Panel>
  )
}

export function TrendScreen({ state, route, say, go }: { state: StoredState; route: HealthRoute; say: HealthSay; go: (href: string) => void }) {
  const p = state.parts.aggregates
  const body = partBody(state, "aggregates")
  const incidents = partBody(state, "incidents")
  return (
    <div className="adh-screen">
      <KnownStrip state={state} say={say} />
      {!p ? null : p.status === "failed" ? <PartFailed name="aggregates" code={p.code} say={say} /> : body === null ? null : body.capability.minute === "unsupported" ? (
        <Panel title={say("tr_lanesTitle")}>
          <p><span className="adh-chip adh-q-unsupported">{say("quality_unsupported")}</span></p>
          <p className="adm-fine">{say("tr_unsupported")}</p>
        </Panel>
      ) : (
        <>
          <Stale state={state} name="aggregates" say={say} />
          <Lanes body={body} route={route} say={say} go={go} />
          {route.screen !== "M01" ? <StageElapsed body={body} say={say} /> : null}
          <ViewerErrors body={body} say={say} />
          <CoverageNotes body={body} say={say} />
        </>
      )}
      {incidents ? <IncidentCoverage body={incidents} say={say} /> : null}
      {(["overview", "sources", "incidents"] as const).map((n) => {
        const q = state.parts[n]
        return q && q.status === "failed" ? <PartFailed key={n} name={n} code={q.code} say={say} /> : null
      })}
    </div>
  )
}
