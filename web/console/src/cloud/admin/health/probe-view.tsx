/* M06's independent-probe subview, drawn from `GET /su/v1/health/probes` (probes.ts).
 *
 * It reads the latest retained probe evidence and says so: no window, pin or account applies,
 * and a pause only stops refreshing. Probing off, registered-but-silent, a host that never
 * reported, a stale host, a delayed upload, an observation dated after the read, a
 * same-fault-domain host and producer-reported loss each have their own word and glyph, so none
 * of them reads as current or as offline. A 2xx is shown with what it cannot prove. */
import type { ReactNode } from "react"
import { partBody, type StoredState } from "./cloud"
import { hostLoss, probePageState, targetEvidence, type ProbeEvidence, type ProbeHost, type ProbesBody, type ProbeTarget } from "./probes"
import type { HealthRoute } from "./route"
import { HEALTH_WORD_TABLES, healthDuration, type HealthSay, type HealthWord } from "./words"
import { HTable, Panel, Unknown } from "./parts"
import { At, exactUTC, Id, PartFailed, Stale } from "./stored-screens"

const EVIDENCE_GLYPH: Record<ProbeEvidence, string> = { current: "●", delayed: "↺", clock_ahead: "⇢", stale: "?" }
const HOST_GLYPH: Record<ProbeHost["freshness"], string> = { fresh: "●", stale: "?", missing: "∅" }

const secs = (ms: number) => Math.abs(ms) / 1000
// A word this page does not know yet is shown as the Cloud's own id, never dropped.
const known = (key: string): key is HealthWord => Object.hasOwn(HEALTH_WORD_TABLES.en, key)
const text = (say: HealthSay, key: string, raw: string): string => (known(key) ? say(key) : raw)
const word = (say: HealthSay, key: string, raw: string): ReactNode => (known(key) ? say(key) : <code className="adh-id">{raw}</code>)

function Chip({ cls, glyph, children }: { cls: string; glyph: string; children: ReactNode }) {
  return <span className={`adh-chip adh-pr ${cls}`}><span aria-hidden="true" className="adh-glyph">{glyph}</span>{children}</span>
}

function HostState({ h, say }: { h: ProbeHost; say: HealthSay }) {
  return <Chip cls={`adh-pr-host-${h.freshness}`} glyph={HOST_GLYPH[h.freshness]}>{say(`pr_host_${h.freshness}` as HealthWord)}</Chip>
}

function LossCell({ h, say }: { h: ProbeHost; say: HealthSay }) {
  const l = hostLoss(h)
  if (l === "unknown") return <Unknown>{say("pr_loss_unknown")}</Unknown>
  if (l === "none") return <>{say("pr_loss_none")}</>
  const c = h.producer_reported_loss!
  return <Chip cls="adh-pr-loss" glyph="✕">{say("pr_loss_reported", { dropped: c.spool_dropped, expired: c.spool_expired, rejected: c.upload_rejected })}</Chip>
}

function Evidence({ t, freshMs, say }: { t: ProbeTarget; freshMs: number; say: HealthSay }) {
  const e = targetEvidence(t, freshMs)
  const values = { obs: healthDuration(say, secs(t.observation_age_ms)), rec: healthDuration(say, secs(t.evidence_age_ms)) }
  return <Chip cls={`adh-pr-ev-${e}`} glyph={EVIDENCE_GLYPH[e]}>{say(`pr_ev_${e}` as HealthWord, values)}</Chip>
}

function Status({ t, say }: { t: ProbeTarget; say: HealthSay }) {
  const failed = t.status !== "answered_2xx" && t.status !== "unknown"
  const label = say(`pr_status_${t.status}` as HealthWord)
  const http = t.latest.http_class !== null && t.status === "http_error" ? ` (${t.latest.http_class})` : ""
  // A stale target keeps its last result visible, labelled as the past, never as now.
  const last = t.status === "unknown" ? say("pr_lastSeen", { result: t.latest.event === "probe_completed" ? say("pr_status_answered_2xx") : say(`pr_status_${t.latest.outcome ?? "unknown"}` as HealthWord) }) : null
  return (
    <>
      <span className={`adh-badge ${failed ? "adh-pr-failed" : t.status === "unknown" ? "adh-pr-unknown" : ""}`}>
        <span aria-hidden="true" className="adh-glyph">{t.status === "answered_2xx" ? "✓" : failed ? "✕" : "?"}</span>{label}{http}
      </span>
      {last ? <span className="adm-fine adh-pr-sub">{last}</span> : null}
    </>
  )
}

function Independence({ t, say }: { t: ProbeTarget; say: HealthSay }) {
  if (t.independence === "same_fault_domain") return <Chip cls="adh-pr-shared" glyph="⚠">{say("pr_ind_same_fault_domain")}</Chip>
  return <span className="adm-fine">{say(t.independence === null ? "pr_ind_none" : "pr_ind_declared_separate")}</span>
}

function Targets({ b, say }: { b: ProbesBody; say: HealthSay }) {
  if (b.targets.length === 0) return <p className="adm-fine">{say("pr_noTargets")}</p>
  return (
    <HTable caption={say("pr_targetsTitle")}
      columns={[{ label: say("pr_colTarget") }, { label: say("pr_colStatus") }, { label: say("pr_colEvidence") }, { label: say("pr_colIndependence") }, { label: say("pr_colCounts") }]}
      rows={b.targets.map((t) => ({
        key: `${t.producer_instance}|${t.target_id}`,
        head: <><Id v={t.target_id} /> <span className="adm-fine adh-pr-sub">{say(`pr_stage_${t.stage}` as HealthWord)} · <Id v={t.producer_instance} /></span></>,
        cells: [
          <Status key="s" t={t} say={say} />,
          <Evidence key="e" t={t} freshMs={b.probe.fresh_within_ms} say={say} />,
          <Independence key="i" t={t} say={say} />,
          say(t.counts_are_floor ? "pr_countsFloor" : "pr_counts", { results: t.results_in_window.toLocaleString("en-US"), failures: t.failures_in_window.toLocaleString("en-US"), skipped: t.overlaps_skipped_in_window.toLocaleString("en-US") }),
        ],
      }))} />
  )
}

function Hosts({ b, say }: { b: ProbesBody; say: HealthSay }) {
  return (
    <HTable caption={say("pr_hostsTitle")}
      columns={[{ label: say("pr_colHost") }, { label: say("pr_colHostState") }, { label: say("pr_colViewpoint") }, { label: say("pr_colReceipt") }, { label: say("pr_colLoss") }]}
      rows={b.hosts.map((h) => ({
        key: h.producer_instance,
        head: <Id v={h.producer_instance} />,
        cells: [
          <HostState key="s" h={h} say={say} />,
          h.registered ? <Id key="v" v={h.registered_viewpoint} /> : <span key="v" className="adm-fine">{say("pr_unregistered")}</span>,
          <At key="r" iso={h.last_received_at} />,
          <LossCell key="l" h={h} say={say} />,
        ],
      }))} />
  )
}

function Claims({ b, say }: { b: ProbesBody; say: HealthSay }) {
  const proves = [...new Set(b.targets.map((t) => t.proves).filter((p): p is NonNullable<typeof p> => p !== null))]
  const notProven = [...new Set(b.targets.flatMap((t) => t.does_not_prove))]
  return (
    <Panel title={say("pr_provesTitle")}>
      {proves.length > 0 ? <ul className="adh-pr-list">{proves.map((p) => <li key={p}>{say(`pr_proves_${p}` as HealthWord)}</li>)}</ul> : null}
      {notProven.length > 0 ? (
        <p>{say("pr_notProven", { list: notProven.map((k) => text(say, `pr_np_${k}`, k)).join(", ") })}</p>
      ) : null}
      <p className="adm-fine">{say("pr_clockNote", { window: healthDuration(say, b.probe.fresh_within_ms / 1000) })}</p>
    </Panel>
  )
}

export function ProbeScreen({ state, route, say }: { state: StoredState; route: HealthRoute; say: HealthSay }) {
  const part = state.parts.probes
  const b = partBody(state, "probes")
  const paused = route.scope.until !== null
  return (
    <div className="adh-screen">
      <p className="adm-fine adh-pr-scope" role="note">{say("pr_scopeNote")}{paused ? <><br />{say("pr_paused")}</> : null}</p>
      {!part ? null : part.status === "failed" ? <PartFailed name="probes" code={part.code} say={say} /> : <Stale state={state} name="probes" say={say} />}
      {b ? <ProbeBody b={b} say={say} /> : null}
    </div>
  )
}

function ProbeBody({ b, say }: { b: ProbesBody; say: HealthSay }) {
  const page = probePageState(b)
  return (
    <>
      <p className="adm-fine">{say("pr_readAt", { time: exactUTC(b.as_of) })}</p>
      {page === "off" ? (
        <div className="adm-notice adh-pr-off" role="status">
          <p><strong>{say("pr_off_title")}</strong></p>
          <p>{say("pr_off_body")}</p>
        </div>
      ) : page === "silent" ? (
        <div className="adm-notice adh-pr-silent" role="status">
          <p><strong>{say("pr_silent_title")}</strong></p>
          <p>{say("pr_silent_body", { n: b.probe.registered_hosts })}</p>
        </div>
      ) : null}
      {page !== "off" ? (
        <>
          <p className="adh-badges">
            <span className="adm-fine">{say("pr_quality")}</span>
            <span className={`adh-chip adh-q-${b.evidence_quality}`}>{say(`quality_${b.evidence_quality}` as HealthWord)}</span>
            {b.partial_reason !== null ? <span className="adm-fine">{word(say, `pr_partial_${b.partial_reason}`, b.partial_reason)}</span> : null}
          </p>
          <Panel title={say("pr_hostsTitle")}><Hosts b={b} say={say} /></Panel>
          <Panel title={say("pr_targetsTitle")}><Targets b={b} say={say} /></Panel>
          {b.targets.length > 0 ? <Claims b={b} say={say} /> : null}
        </>
      ) : null}
      <Panel title={say("pr_unsupportedTitle")}>
        <ul className="adh-pr-list">{b.probe.unsupported.map((k) => <li key={k}>{word(say, `pr_uns_${k}`, k)}</li>)}</ul>
      </Panel>
    </>
  )
}
