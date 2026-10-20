/* The charts the stored screens share: the problem summary and evidence quality
 * every screen opens with, the evidence timeline, sources on the window, the
 * sequence coverage, service stages, and one operation's observed stages.
 * Each chart has a table switch, so the same rows stay reachable by keyboard and
 * screen reader. Marks are glyph plus position, never colour alone. */
import { useState, type ReactNode } from "react"
import type { Envelope, StoredEvent } from "./stored"
import type { PartName, StoredState } from "./cloud"
import { partBody } from "./cloud"
import type { HealthScreen } from "./route"
import { healthClock, healthDuration, healthMs, type HealthSay, type HealthWord } from "./words"
import { linkClick, Panel } from "./parts"
import {
  bottleneck, problems, qualitySummary, stageMatrix, traceLanes, windowBands,
  type Axis, type Band, type Lane, type MarkKind, type PanelQuality, type Problem, type SeqLane, type SourceMark, type StageRow, type TraceLane,
} from "./timeline"

export type ChartNav = { href: (s: HealthScreen) => string; go: (s: HealthScreen) => void }

const pct = (f: number) => `${(f * 100).toFixed(3)}%`
const age = (say: HealthSay, ms: number) => healthDuration(say, Math.round(ms / 1000))

const MARK_GLYPH: Record<MarkKind | "unsupported", string> = { stall: "■", loss: "✕", slow: "▲", recovered: "✓", boot: "◆", error: "✕", other: "│", unsupported: "?" }

function ChartLink({ to, nav, children }: { to: HealthScreen; nav: ChartNav; children: ReactNode }) {
  return <a className="adh-link" href={nav.href(to)} onClick={(e) => linkClick(e, () => nav.go(to))}>{children}</a>
}

/** A panel with a chart and a table of the same rows; the chart shows first. */
export function ChartPanel({ title, say, chart, table }: { title: string; say: HealthSay; chart: ReactNode; table: ReactNode }) {
  const [mode, setMode] = useState<"chart" | "table">("chart")
  return (
    <Panel title={title} actions={
      <div className="adh-switch" role="group" aria-label={say("tl_viewLabel", { title })}>
        <button type="button" className="adh-switch-btn" aria-pressed={mode === "chart"} onClick={() => setMode("chart")}>{say("tl_chart")}</button>
        <button type="button" className="adh-switch-btn" aria-pressed={mode === "table"} onClick={() => setMode("table")}>{say("tl_table")}</button>
      </div>
    }>
      {mode === "chart" ? chart : table}
    </Panel>
  )
}

/* ---- quality and problems -------------------------------------------------- */

const RELEVANT: PartName[] = ["overview", "outcomes", "sources", "events", "trace"]

/** Per-read quality of the reads this screen relies on; incidents are labelled unsupported elsewhere. */
export function panelQualities(state: StoredState): PanelQuality[] {
  const out: PanelQuality[] = []
  for (const name of RELEVANT) {
    const p = state.parts[name]
    if (!p) continue
    if (p.status === "failed") out.push({ part: name, quality: "failed", stale: false, reason: p.code })
    else out.push({ part: name, quality: (p.body as Envelope).evidence_quality, stale: p.failure !== null, reason: (p.body as Envelope).partial_reason })
  }
  return out
}

/** The strip each screen opens with: the overall outcome stays unknown, the quality is the weakest read's. */
export function KnownStrip({ state, say }: { state: StoredState; say: HealthSay }) {
  const outcomes = partBody(state, "outcomes")
  const q = qualitySummary(panelQualities(state), outcomes?.groups ?? null)
  return (
    <section className="adh-verdict adh-v-unknown" aria-label={say("st_verdictLabel")}>
      <div className="adh-verdict-pair">
        <div>
          <p className="adm-tile-label">{say("verdictOutcome")}</p>
          <p className="adh-verdict-value"><span className="adh-badge adh-o-unknown"><span aria-hidden="true" className="adh-glyph">?</span>{say("outcome_unknown")}</span></p>
          <p className="adm-fine">{say("st_noPresence")}</p>
        </div>
        <div>
          <p className="adm-tile-label">{say("tl_qualityOverall")}</p>
          <p className="adh-verdict-value"><span className={`adh-chip adh-q-${q.overall}`}>{say(`quality_${q.overall}` as HealthWord)}</span></p>
          <p className="adm-fine">
            {[q.failed > 0 ? say("tl_qualityFailed", { n: q.failed }) : null, q.stale > 0 ? say("tl_qualityStale", { n: q.stale }) : null, q.partialGroups > 0 ? say("tl_qualityPartialGroups", { n: q.partialGroups }) : null].filter(Boolean).join(" · ") || say("tl_qualityNotFull")}
          </p>
        </div>
      </div>
      <ul className="adh-quality-list" aria-label={say("tl_qualityPanels")}>
        {q.panels.map((p) => (
          <li key={p.part}>
            <span className="adh-quality-name">{say(`st_part_${p.part}` as HealthWord)}</span>{" "}
            <span className={`adh-chip adh-q-${p.quality === "failed" ? "stale" : p.quality}`}>{p.quality === "failed" ? say("tl_readFailed") : say(`quality_${p.quality}` as HealthWord)}{p.stale ? ` · ${say("tl_readStale")}` : ""}</span>
          </li>
        ))}
      </ul>
    </section>
  )
}

function ProblemLine({ p, say, nav }: { p: Problem; say: HealthSay; nav: ChartNav }) {
  const w = p.worst
  return (
    <>
      <span className="adh-problem-what">{say(`tl_problem_${p.kind}` as HealthWord, { n: p.count })}</span>
      {w ? (
        <span className="adm-fine">
          {" · "}
          <ChartLink to={{ screen: "M02", machine: w.machine_id }} nav={nav}><code className="adh-id">{w.machine_id}</code></ChartLink>
          {w.stage ? ` · ${w.stage}` : ""}
          {" · "}{say("tl_evidenceAge", { age: age(say, w.ageMs) })}
          {w.correlation ? <>{" · "}<ChartLink to={{ screen: "M04", trace: w.correlation }} nav={nav}>{say("tl_openTrace")}</ChartLink></> : null}
        </span>
      ) : null}
    </>
  )
}

const PROBLEM_GLYPH: Record<Problem["kind"], string> = { loss: "✕", stall_detected: "■", slow: "▲", released_after_stall: "◇", stale_sources: "◌", gaps: "⋯", failed_reads: "!" }

/** The first thing on a screen: the worst problem in what was loaded, and the rest as one line each. */
export function ProblemSummary({ state, say, nav, staleCount = null }: { state: StoredState; say: HealthSay; nav: ChartNav; staleCount?: number | null }) {
  const groups = partBody(state, "outcomes")?.groups ?? null
  const sources = partBody(state, "sources")?.sources ?? null
  const coverage = (partBody(state, "overview") ?? partBody(state, "outcomes"))?.coverage ?? null
  const failed = panelQualities(state).filter((p) => p.quality === "failed").length
  const list = problems(groups, sources, coverage, failed, staleCount)
  // Durable incidents come first: each is a record Cloud keeps, so the line links to its evidence.
  const incidents = partBody(state, "incidents")?.incidents.filter((r) => r.state !== null && r.state !== "closed") ?? []
  const RANK = { active: 0, recovering: 1, recovered_awaiting_review: 2, closed: 3 } as const
  const worst = [...incidents].sort((a, b) => RANK[a.state!] - RANK[b.state!])[0]
  return (
    <section className="adm-panel adh-problems" aria-label={say("tl_problemsTitle")}>
      <h2 className="adm-h3">{say("tl_problemsTitle")}</h2>
      {worst ? (
        <p className="adh-problem adh-problem-first">
          <span aria-hidden="true" className="adh-glyph">■</span>{" "}
          <span>{say("inc_problemLine", { n: incidents.length })}{" "}
            <a className="adh-link" href={nav.href({ screen: "M05", incident: worst.id })} onClick={(e) => linkClick(e, () => nav.go({ screen: "M05", incident: worst.id }))}>
              {say("inc_problemWorst", { state: say(`inc_state_${worst.state}` as HealthWord), machine: worst.machine_id })}
            </a>
          </span>
        </p>
      ) : null}
      {list.length === 0 ? (worst ? null : <p className="adm-fine">{say("tl_problemNone")}</p>) : (
        <ol className="adh-problem-list">
          {list.map((p, i) => (
            <li key={p.kind} className={i === 0 ? "adh-problem adh-problem-first" : "adh-problem"}>
              <span aria-hidden="true" className={`adh-glyph adh-pg-${p.kind}`}>{PROBLEM_GLYPH[p.kind]}</span>
              <span><ProblemLine p={p} say={say} nav={nav} /></span>
            </li>
          ))}
        </ol>
      )}
    </section>
  )
}

/* ---- the evidence timeline ------------------------------------------------ */

function AxisLabels({ axis, say }: { axis: Axis; say: HealthSay }) {
  const mid = new Date((axis.from + axis.to) / 2).toISOString()
  return (
    <div className="adh-tl-row" aria-hidden="true">
      <span />
      <div className="adh-tl-axis">
        <span>{healthClock(new Date(axis.from).toISOString())}</span>
        <span>{healthClock(mid)}</span>
        <span>{healthClock(new Date(axis.to).toISOString())} UTC</span>
      </div>
    </div>
  )
}

function bandWord(say: HealthSay, b: Band): string {
  if (b.kind === "gap") return b.count === null ? say("tl_band_gapUnknown") : say("tl_band_gap", { n: b.count })
  return say(`tl_band_${b.kind}` as HealthWord)
}

function BandView({ b, say }: { b: Band; say: HealthSay }) {
  return <span className={`adh-tl-band adh-tl-b-${b.kind}`} style={{ left: pct(b.from), width: pct(Math.max(0.004, b.to - b.from)) }} title={bandWord(say, b)} />
}

function laneName(say: HealthSay, l: Lane): string {
  return say("tl_lane", { machine: l.machine_id, producer: l.producer_instance || say("tl_groups"), boot: l.boot_id, generation: l.generation === null ? say("unknown") : l.generation })
}

function LaneFlags({ l, say }: { l: Lane; say: HealthSay }) {
  const flags = [
    l.before > 0 ? say("tl_before", { n: l.before }) : null,
    l.after > 0 ? say("tl_after", { n: l.after }) : null,
    l.late > 0 ? say("tl_late", { n: l.late }) : null,
    l.monoBackwards > 0 ? say("tl_monoBack", { n: l.monoBackwards }) : null,
    l.wallBackwards > 0 ? say("tl_wallBack", { n: l.wallBackwards }) : null,
    l.gapsUnplaced > 0 ? say("tl_gapsUnplaced", { n: l.gapsUnplaced }) : null,
  ].filter((x): x is string => x !== null)
  return flags.length ? <p className="adh-tl-flags">{flags.join(" · ")}</p> : null
}

function markWord(say: HealthSay, kind: MarkKind | "unsupported"): string {
  return say(`tl_mark_${kind}` as HealthWord)
}

/** Lanes over one window: marks at receive times, explicit bands, nothing drawn as healthy. */
export function Timeline({ lanes, axis, shared, say, env }: { lanes: Lane[]; axis: Axis; shared: Band[]; say: HealthSay; env: Envelope | null }) {
  if (lanes.length === 0) {
    return (
      <div className="adh-tl">
        <p className="adm-fine">{say("tl_noLanes")}</p>
        <TimelineLegend say={say} shared={shared} />
      </div>
    )
  }
  const kinds = new Set(lanes.flatMap((l) => l.marks.map((m) => m.kind)))
  return (
    <div className="adh-tl">
      <p className="adm-fine adh-axis-note">{say("tl_axisNote")} {env ? (env.clock_uncertainty_ms === null ? say("tl_clockUnknown") : say("tl_clock", { ms: env.clock_uncertainty_ms })) : null}</p>
      <AxisLabels axis={axis} say={say} />
      <ul className="adh-tl-lanes">
        {lanes.map((l) => {
          const counts = new Map<string, number>()
          for (const m of l.marks) counts.set(m.kind, (counts.get(m.kind) ?? 0) + 1)
          return (
            <li key={l.key} className="adh-tl-row">
              <span className="adh-lane-name"><code className="adh-id">{l.machine_id}</code> {laneName(say, l).replace(l.machine_id, "").trim()}</span>
              <div>
                <div className="adh-tl-track" aria-hidden="true">
                  {shared.map((b, i) => <BandView key={`s${i}`} b={b} say={say} />)}
                  {l.gaps.map((b, i) => <BandView key={`g${i}`} b={b} say={say} />)}
                  {l.before > 0 ? <span className="adh-tl-edge adh-tl-edge-before">‹</span> : null}
                  {l.after > 0 ? <span className="adh-tl-edge adh-tl-edge-after">›</span> : null}
                  {l.marks.map((m) => (
                    <span key={m.id} className={`adh-tl-mark adh-mk-${m.kind}${m.late ? " adh-mk-late" : ""}`} style={{ left: pct(m.at) }}
                      title={`${markWord(say, m.kind)} · ${m.event}${m.stage ? ` · ${m.stage}` : ""} · #${m.seq} · ${m.received_at}${m.late ? ` · ${say("tl_traceLate")}` : ""}`}>{MARK_GLYPH[m.kind]}</span>
                  ))}
                </div>
                <p className="adm-visually-hidden">{say("tl_laneSummary", { n: l.marks.length, kinds: [...counts.entries()].map(([k, n]) => `${markWord(say, k as MarkKind)} ${n}`).join(", ") || "—" })}</p>
                <LaneFlags l={l} say={say} />
              </div>
            </li>
          )
        })}
      </ul>
      <TimelineLegend say={say} shared={shared} kinds={kinds} gaps={lanes.some((l) => l.gaps.length > 0)} />
    </div>
  )
}

function TimelineLegend({ say, shared, kinds = new Set(), gaps = false }: { say: HealthSay; shared: Band[]; kinds?: Set<string>; gaps?: boolean }) {
  const bandKinds = new Set(shared.map((b) => b.kind))
  return (
    <ul className="adh-legend">
      <li><span className="adh-key adh-tl-key-none" aria-hidden="true" />{say("tl_notObserved")}</li>
      {(Object.keys(MARK_GLYPH) as (MarkKind | "unsupported")[]).filter((k) => kinds.has(k)).map((k) => (
        <li key={k}><span aria-hidden="true" className={`adh-tl-keyglyph adh-mk-${k}`}>{MARK_GLYPH[k]}</span>{markWord(say, k)}</li>
      ))}
      {(["before_retention", "not_loaded", "after_as_of"] as const).filter((k) => bandKinds.has(k)).map((k) => (
        <li key={k}><span className={`adh-key adh-tl-band adh-tl-b-${k}`} aria-hidden="true" />{say(`tl_band_${k}` as HealthWord)}</li>
      ))}
      {gaps ? <li><span className="adh-key adh-tl-band adh-tl-b-gap" aria-hidden="true" />{say("tl_band_gapKey")}</li> : null}
    </ul>
  )
}

/** Bands for a list read: retention, `as_of`, and rows not loaded when more pages exist. */
export function listBands(axis: Axis, events: StoredEvent[], more: boolean): Band[] {
  let oldest: string | null = null
  for (const e of events) if (oldest === null || e.received_at < oldest) oldest = e.received_at
  return windowBands(axis, oldest, more)
}

/* ---- sources on the window ------------------------------------------------- */

function Pos({ p, cls, title, glyph }: { p: SourceMark["last"]; cls: string; title: string; glyph: string }) {
  if ("edge" in p) return <span className={`adh-tl-edge adh-tl-edge-${p.edge} ${cls}`} title={title}>{p.edge === "before" ? "‹" : "›"}</span>
  return <span className={`adh-tl-mark ${cls}`} style={{ left: pct(p.at) }} title={title}>{glyph}</span>
}

/** Each source's newest receipt and its boots' first and last receipts on the window. */
export function SourceChart({ marks, axis, say, nav }: { marks: SourceMark[]; axis: Axis; say: HealthSay; nav: ChartNav }) {
  if (marks.length === 0) return <p className="adm-fine">{say("st_notCollected")}</p>
  return (
    <div className="adh-tl">
      <p className="adm-fine adh-axis-note">{say("tl_sourcesNote")}</p>
      <AxisLabels axis={axis} say={say} />
      <ul className="adh-tl-lanes">
        {marks.map((s) => (
          <li key={s.source_id} className="adh-tl-row">
            <span className="adh-lane-name">
              <ChartLink to={{ screen: "M02", machine: s.machine_id }} nav={nav}><code className="adh-id">{s.machine_id}</code></ChartLink>{" "}
              {s.source ?? "—"} · {s.producer_instance}
            </span>
            <div>
              <div className="adh-tl-track" aria-hidden="true">
                {s.boots.map((b) => {
                  const a = "at" in b.first ? b.first.at : "edge" in b.first && b.first.edge === "before" ? 0 : null
                  const z = "at" in b.last ? b.last.at : "edge" in b.last && b.last.edge === "after" ? 1 : null
                  return a !== null && z !== null && z >= a ? <span key={b.boot_id} className="adh-tl-span" style={{ left: pct(a), width: pct(Math.max(0.004, z - a)) }} title={say("tl_bootSpan", { boot: b.boot_id, n: b.generation })} /> : null
                })}
                <Pos p={s.last} cls={`adh-mk-src adh-mk-${s.freshness}`} glyph={s.freshness === "stale" ? "◌" : "●"} title={say("tl_lastReceipt")} />
              </div>
              <p className="adh-tl-flags">
                <span className={`adh-chip adh-q-${s.freshness === "fresh" ? "complete" : "stale"}`}>{say(`st_fresh_${s.freshness}` as HealthWord)}</span>{" "}
                {say("tl_lastAge", { age: age(say, s.ageMs) })}
                {" · "}{s.expectedEveryMs === null ? say("tl_expectedUnknown") : say("tl_expected", { d: age(say, s.expectedEveryMs) })}
                {" · "}{say("tl_bootCount", { n: s.boots.length })}{s.bootOrder === "unverified" ? ` · ${say("st_boot_unverified")}` : ""}{s.bootsEvicted > 0 ? ` · ${say("st_bootsEvicted", { n: s.bootsEvicted })}` : ""}
                {"edge" in s.last ? ` · ${say(s.last.edge === "before" ? "tl_lastBefore" : "tl_lastAfter")}` : ""}
              </p>
            </div>
          </li>
        ))}
      </ul>
      <ul className="adh-legend">
        <li><span aria-hidden="true" className="adh-tl-keyglyph adh-mk-src adh-mk-fresh">●</span>{say("tl_keyFresh")}</li>
        <li><span aria-hidden="true" className="adh-tl-keyglyph adh-mk-src adh-mk-stale">◌</span>{say("tl_keyStale")}</li>
        <li><span className="adh-key adh-tl-span" aria-hidden="true" />{say("tl_keyBoot")}</li>
      </ul>
    </div>
  )
}

/* ---- sequence coverage ------------------------------------------------------ */

/** Received ranges and listed gaps in producer-sequence space, per source and boot. */
export function SeqChart({ lanes, say }: { lanes: SeqLane[] | null; say: HealthSay }) {
  if (lanes === null) return <p className="adm-fine"><span className="adm-unknown">{say("tl_seqUnknown")}</span></p>
  if (lanes.length === 0) return <p className="adm-fine">{say("st_noGapsListed")}</p>
  return (
    <div className="adh-tl">
      <p className="adm-fine">{say("tl_seqNote")}</p>
      <ul className="adh-tl-lanes">
        {lanes.map((l) => {
          const span = Math.max(1, l.to - l.from + 1)
          const x = (n: number) => (n - l.from) / span
          return (
            <li key={l.key} className="adh-tl-row">
              <span className="adh-lane-name"><code className="adh-id">{l.machine_id}</code> {say("tl_bootShort", { boot: l.boot_id })}</span>
              <div>
                <div className="adh-tl-track" aria-hidden="true">
                  {l.ranges.map((r, i) => <span key={`r${i}`} className="adh-seq-range" style={{ left: pct(x(r.from)), width: pct(Math.max(0.004, (r.to - r.from + 1) / span)) }} />)}
                  {l.gaps.map((g, i) => <span key={`g${i}`} className="adh-tl-band adh-tl-b-gap" style={{ left: pct(x(g.from)), width: pct(Math.max(0.006, (g.to - g.from + 1) / span)) }} />)}
                </div>
                <p className="adh-tl-flags">
                  {say("tl_seqSpan", { from: l.from, to: l.to })}
                  {l.gaps.length ? ` · ${l.gaps.map((g) => say("tl_seqGap", { from: g.from, to: g.to, n: g.count === null ? say("unknown") : g.count })).join(" · ")}` : ""}
                </p>
              </div>
            </li>
          )
        })}
      </ul>
      <ul className="adh-legend">
        <li><span className="adh-key adh-seq-range" aria-hidden="true" />{say("tl_keyReceived")}</li>
        <li><span className="adh-key adh-tl-band adh-tl-b-gap" aria-hidden="true" />{say("tl_band_gapKey")}</li>
      </ul>
    </div>
  )
}

/* ---- service stages ----------------------------------------------------------- */

function outcomeLine(say: HealthSay, c: StageRow["cells"][number]): string {
  return Object.entries(c.outcomes).map(([o, n]) => `${o === "unknown" ? say("unknown") : say(`st_out_${o}` as HealthWord)} ${n}`).join(" · ")
}

/** Source × stage on the loaded page, with each stage's own elapsed time as a bar. */
export function StageChart({ events, say, nav, asOf, more }: { events: StoredEvent[]; say: HealthSay; nav: ChartNav; asOf: string; more: boolean }) {
  // The source filter is the Cloud's own (M03's control); this draws what that read returned.
  const filtered = events.filter((e) => e.source === "relay" || e.source === "cloud_api")
  const of = filtered.length
  const rows = stageMatrix(filtered)
  const max = Math.max(1, ...rows.flatMap((r) => r.cells.map((c) => c.maxElapsedMs ?? 0)))
  const b = bottleneck(rows, asOf)
  return (
    <div className="adh-stages">
      <p className="adm-fine">{say(more ? "tl_stagesLocalMore" : "tl_stagesLocal", { n: filtered.length, of })}</p>
      {b ? (
        <p className="adh-bneck" role="status">
          <span aria-hidden="true" className={`adh-glyph adh-mk-${b.why === "stall" ? "stall" : b.why === "failure" ? "error" : "slow"}`}>{b.why === "stall" ? "■" : b.why === "failure" ? "✕" : "▲"}</span>{" "}
          <strong>{say(`tl_bneck_${b.why}` as HealthWord, { stage: b.cell.stage, source: b.row.source, ms: b.cell.maxElapsedMs === null ? say("unknown") : healthMs(say, b.cell.maxElapsedMs) })}</strong>
          {" · "}{say("tl_evidenceAge", { age: age(say, b.ageMs) })}
          {b.cell.role === "downstream" ? ` · ${say("st_role_downstream")}` : ""}
        </p>
      ) : <p className="adm-fine">{say("tl_bneckNone")}</p>}
      {rows.length === 0 ? <p className="adm-fine">{say("st_serviceNone")}</p> : rows.map((r) => (
        <section key={r.key} className="adh-stage-row" aria-label={`${r.source} · ${r.producer_instance}`}>
          <h3 className="adh-stage-src">{r.source} · {r.producer_instance}</h3>
          <ol className="adh-stage-list">
            {r.cells.map((c) => {
              const hot = b !== null && b.row === r && b.cell === c
              return (
                <li key={c.stage} className={`adh-stage${hot ? " adh-stage-hot" : ""} adh-role-${c.role ?? "none"}`}>
                  <span className="adh-stage-name">{c.stage}<span className="adm-fine"> · {c.role === null ? say("unknown") : say(`st_role_${c.role}` as HealthWord)}</span></span>
                  <span className="adh-stage-track" aria-hidden="true">
                    {c.maxElapsedMs === null ? <span className="adh-stage-none" /> : <span className="adh-stage-bar" style={{ width: pct(Math.max(0.01, c.maxElapsedMs / max)) }} />}
                  </span>
                  <span className="adh-stage-val">{c.maxElapsedMs === null ? say("tl_elapsedUnknown") : healthMs(say, c.maxElapsedMs)}</span>
                  <span className="adh-stage-out adm-fine">{say("tl_records", { n: c.records })} · {outcomeLine(say, c)}</span>
                </li>
              )
            })}
          </ol>
        </section>
      ))}
      <p className="adm-fine">{say("tl_elapsedNote")}</p>
      <p className="adm-fine">{say("socketNote")}</p>
      {filtered.some((e) => e.correlation) ? (
        <p className="adm-fine">{say("tl_serviceTraces")} {[...new Set(filtered.map((e) => e.correlation).filter((c): c is string => c !== null))].map((c) => (
          <span key={c}><ChartLink to={{ screen: "M04", trace: c }} nav={nav}><code className="adh-id">{c}</code></ChartLink> </span>
        ))}</p>
      ) : null}
    </div>
  )
}

/* ---- one operation ---------------------------------------------------------------- */

/** One operation's observed stages per source and boot, with sequence holes; never one merged waterfall. */
export function TraceChart({ events, say }: { events: StoredEvent[]; say: HealthSay }) {
  const lanes: TraceLane[] = traceLanes(events)
  if (lanes.length === 0) return <p className="adm-fine">{say("tl_traceNone")}</p>
  return (
    <div className="adh-trace">
      <p className="adm-fine">{say("tl_traceNote")}</p>
      {lanes.map((l) => (
        <section key={l.key} className="adh-stage-row" aria-label={`${l.machine_id} · ${l.producer_instance}`}>
          <h3 className="adh-stage-src"><code className="adh-id">{l.machine_id}</code> · {l.source ?? "—"} · {l.producer_instance} · {say("tl_bootShort", { boot: l.boot_id })}</h3>
          <ol className="adh-steps">
            {l.steps.map((s, i) => s.kind === "gap" ? (
              <li key={`gap${i}`} className="adh-step adh-step-gap">
                <span aria-hidden="true" className="adh-step-dot">⋯</span>
                <span>{say("tl_traceGap", { n: s.to - s.from + 1, from: s.from, to: s.to })}</span>
              </li>
            ) : (
              <li key={s.id} className={`adh-step adh-mk-${s.outcome === "pending" ? "stall" : s.event === "stall_detected" ? "stall" : "other"}`}>
                <span aria-hidden="true" className="adh-step-dot">{s.outcome === "pending" || s.event === "stall_detected" ? "■" : "●"}</span>
                <span className="adh-step-body">
                  <span className="adh-step-name">#{s.seq} {s.stage ?? s.event}</span>
                  <span className="adm-fine">
                    {s.stage ? `${s.event} · ` : ""}{s.role ? `${say(`st_role_${s.role}` as HealthWord)} · ` : ""}
                    {s.outcome ? `${say(`st_out_${s.outcome}` as HealthWord)} · ` : ""}
                    {s.elapsedMs !== null ? `${say("tl_ownTime", { ms: healthMs(say, s.elapsedMs) })} · ` : ""}
                    {say("tl_receivedAt", { time: s.received_at })}{s.late ? ` · ${say("tl_traceLate")}` : ""}
                  </span>
                </span>
              </li>
            ))}
          </ol>
        </section>
      ))}
    </div>
  )
}

/* ---- what is not collected ---------------------------------------------------------- */

/** One compact panel for every requested part with no producer yet; each keeps its exact limitation. */
export function NotCollected({ items, say }: { items: { title: string; reason: string }[]; say: HealthSay }) {
  return (
    <Panel title={say("tl_notCollectedTitle")}>
      <p className="adm-fine">{say("tl_notCollectedNote")}</p>
      <dl className="adh-nc">
        {items.map((i) => (
          <div key={i.title} className="adh-nc-item">
            <dt><span className="adh-chip adh-q-unsupported">{say("quality_unsupported")}</span> {i.title}</dt>
            <dd className="adm-fine">{i.reason}</dd>
          </div>
        ))}
      </dl>
    </Panel>
  )
}
