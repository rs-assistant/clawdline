/* M05: the incident's agent investigation, as one card beside the summary.
 *
 * The newest investigation leads, in words: its state, the concrete reason and what to do next,
 * then the agent's enumerated report. Earlier ones fold away. Task success is never recovery, and
 * human review stays its own panel. A start names one of the operator's own active schedule
 * webhooks; Cloud cannot say which one is bound to an investigation schedule, so the page says
 * that and asks the operator to confirm it, and a machine without the binding refuses it. */
import { useEffect, useRef, useState } from "react"
import type { Refusal } from "./cloud"
import type { Investigation, Investigations, StartGate } from "./investigations"
import { startGate } from "./investigations"
import { fetchHooks, newIdempotencyKey, startInvestigation, START_REFUSALS, type HookChoice, type StartRequest } from "./investigation-actions"
import type { HealthSay, HealthWord } from "./words"
import { At, Id } from "./stored-screens"
import { Panel } from "./parts"

export type InvestigationActions = {
  get: typeof fetch
  apiOrigin: string
  refresh: () => void
  refuse: (r: Refusal) => void
}

const GLYPH = { investigating: "◇", needs_you: "■", verified_recovered: "✓" } as const

export function InvestigationBadge({ inv, say }: { inv: Investigation; say: HealthSay }) {
  if (!inv.state.known) return <span className="adh-badge adh-iv-unknown"><span aria-hidden="true" className="adh-glyph">?</span>{say("iv_state_unknown", { raw: inv.state.raw ?? "—" })}</span>
  const s = inv.state.value
  return <span className={`adh-badge adh-iv-${s}`}><span aria-hidden="true" className="adh-glyph">{GLYPH[s]}</span>{say(`iv_state_${s}` as HealthWord)}</span>
}

/** The reason in words and its next step. A verified recovery has no reason; an unknown one says so. */
export function reasonWords(inv: Investigation, say: HealthSay): { why: string; next: string } {
  if (inv.reason === null) {
    if (inv.state.known && inv.state.value === "verified_recovered") return { why: say("iv_why_verified"), next: say("iv_next_verified") }
    return { why: say("iv_why_none"), next: say("iv_next_unknown") }
  }
  if (!inv.reason.known) return { why: say("iv_why_unknown", { raw: inv.reason.raw ?? "—" }), next: say("iv_next_unknown") }
  return { why: say(`iv_why_${inv.reason.value}` as HealthWord), next: say(`iv_next_${inv.reason.value}` as HealthWord) }
}

function Report({ inv, say }: { inv: Investigation; say: HealthSay }) {
  const r = inv.report
  if (r === null) return <p className="adm-fine">{say("iv_reportNone")}</p>
  return (
    <dl className="adh-facts">
      <dt>{say("iv_reportConclusion")}</dt>
      <dd>{r.conclusion.known ? say(`iv_conc_${r.conclusion.value}` as HealthWord) : say("iv_conc_unknown", { raw: r.conclusion.raw ?? "—" })}</dd>
      <dt>{say("iv_fixCommit")}</dt><dd><Id v={r.fix_commit} /></dd>
      <dt>{say("iv_evidenceRefs")}</dt>
      <dd>{r.evidence_refs.length === 0 ? <span className="adm-unknown">{say("iv_noRefs")}</span> : r.evidence_refs.map((x) => <Id key={x} v={x} />)}</dd>
    </dl>
  )
}

function Technical({ inv, say }: { inv: Investigation; say: HealthSay }) {
  return (
    <details className="adh-disclose">
      <summary>{say("iv_tech")}</summary>
      <dl className="adh-facts">
        <dt>{say("iv_f_reason")}</dt><dd><Id v={inv.reason === null ? null : inv.reason.known ? inv.reason.value : inv.reason.raw} /></dd>
        <dt>{say("iv_f_deliveryState")}</dt><dd><Id v={inv.delivery_state} /></dd>
        <dt>{say("iv_f_task")}</dt><dd><Id v={inv.task_terminal_state === null ? null : inv.task_terminal_state.known ? inv.task_terminal_state.value : inv.task_terminal_state.raw} /></dd>
        <dt>{say("iv_deadline")}</dt><dd><At iso={inv.deadline_at} /></dd>
        <dt>{say("iv_f_revision")}</dt><dd>{inv.incident_revision ?? <span className="adm-unknown">—</span>}</dd>
        <dt>{say("iv_f_hash")}</dt><dd><code className="adh-id adh-hash">{inv.incident_content_hash ?? "—"}</code></dd>
        <dt>{say("iv_f_hook")}</dt><dd><Id v={inv.hook_id} /></dd>
        <dt>{say("iv_f_machine")}</dt><dd><Id v={inv.machine_id} /></dd>
        <dt>{say("iv_f_delivery")}</dt><dd><Id v={inv.delivery_id} /></dd>
        <dt>{say("iv_f_investigation")}</dt><dd><Id v={inv.investigation_id} /></dd>
        {inv.recovery ? <><dt>{say("iv_f_recovery")}</dt><dd><Id v={inv.recovery.event_key} /> {inv.recovery.producer_seq !== null ? `· ${say("inc_card_seq", { seq: inv.recovery.producer_seq })}` : ""}</dd></> : null}
      </dl>
    </details>
  )
}

function Latest({ inv, say }: { inv: Investigation; say: HealthSay }) {
  const { why, next } = reasonWords(inv, say)
  return (
    <div className="adh-iv-latest">
      <p className="adh-badges"><InvestigationBadge inv={inv} say={say} /> <span className="adm-fine">{say("iv_started")} <At iso={inv.started_at} />{inv.closed_at ? <> · {say("iv_closed")} <At iso={inv.closed_at} /></> : null}</span></p>
      <p className="adh-inc-lead">{why}</p>
      <p><strong>{say("iv_nextLabel")}</strong> {next}</p>
      {inv.recovery ? (
        <p className="adm-fine">{say("iv_recoveryProof", { basis: inv.recovery.basis.known ? say(`iv_basis_${inv.recovery.basis.value}` as HealthWord) : say("iv_basis_unknown") })} <At iso={inv.recovery.received_at} /></p>
      ) : null}
      <h3 className="adh-sum-h">{say("iv_reportTitle")}</h3>
      <Report inv={inv} say={say} />
      <Technical inv={inv} say={say} />
    </div>
  )
}

function Earlier({ items, say }: { items: Investigation[]; say: HealthSay }) {
  if (items.length === 0) return null
  return (
    <details className="adh-disclose">
      <summary>{say("iv_earlier", { n: items.length })}</summary>
      <ol className="adh-iv-earlier">
        {items.map((inv) => (
          <li key={inv.investigation_id}>
            <p className="adh-badges"><InvestigationBadge inv={inv} say={say} /> <span className="adm-fine"><At iso={inv.started_at} /></span></p>
            <p className="adm-fine">{reasonWords(inv, say).why}</p>
            <Technical inv={inv} say={say} />
          </li>
        ))}
      </ol>
    </details>
  )
}

type HooksState = { kind: "loading" } | { kind: "failed"; code: string } | { kind: "ready"; hooks: HookChoice[]; more: boolean }
/** `pending` holds the key and body of a request whose outcome is not known yet. */
type StartState =
  | { kind: "closed" }
  | { kind: "picking" }
  | { kind: "busy"; pending: { key: string; req: StartRequest } }
  | { kind: "ambiguous"; pending: { key: string; req: StartRequest }; code: string }
  | { kind: "refused"; code: string }
  | { kind: "done"; replayed: boolean }

function refusalWord(say: HealthSay, code: string): string {
  return (START_REFUSALS as readonly string[]).includes(code) ? say(`iv_ref_${code}` as HealthWord) : say(`st_fail_${code}` as HealthWord)
}

function Start({ incidentId, revision, account, gate, say, actions }: { incidentId: string; revision: number; account: string | null; gate: StartGate; say: HealthSay; actions: InvestigationActions }) {
  const [st, setSt] = useState<StartState>({ kind: "closed" })
  const [hooks, setHooks] = useState<HooksState>({ kind: "loading" })
  // The chosen hook is kept for this page only; nothing is stored or created.
  const [hookId, setHookId] = useState("")
  const [bound, setBound] = useState(false)
  const ctl = useRef<AbortController | null>(null)
  useEffect(() => () => { ctl.current?.abort() }, [])

  const loadHooks = () => {
    ctl.current?.abort()
    const mine = new AbortController()
    ctl.current = mine
    setHooks({ kind: "loading" })
    void fetchHooks(actions.get, actions.apiOrigin, mine.signal).then((r) => {
      if (mine.signal.aborted) return
      if ("refused" in r) { actions.refuse(r.refused); return }
      if (!r.ok) { setHooks({ kind: "failed", code: r.code }); return }
      setHooks({ kind: "ready", hooks: r.hooks, more: r.more })
      if (r.hooks.length === 1) setHookId(r.hooks[0].hook_id)
    })
  }

  const send = (pending: { key: string; req: StartRequest }) => {
    const mine = new AbortController()
    ctl.current = mine
    setSt({ kind: "busy", pending })
    void startInvestigation(actions.get, actions.apiOrigin, incidentId, account, pending.req, pending.key, mine.signal).then((r) => {
      if (mine.signal.aborted) return
      if ("refused" in r) { actions.refuse(r.refused); return }
      if (r.ok) { setSt({ kind: "done", replayed: r.replayed }); actions.refresh(); return }
      if (!r.definite) { setSt({ kind: "ambiguous", pending, code: r.code }); return }
      setSt({ kind: "refused", code: r.code })
      // Another live investigation, or a moved revision: read the incident again to show what Cloud holds.
      if (r.code === "investigation_open" || r.code === "incident_revision_moved" || r.code === "investigation_limit") actions.refresh()
    })
  }

  if (gate !== "ok" && st.kind !== "busy" && st.kind !== "ambiguous") {
    return <p className="adm-fine" role="note">{say(`iv_gate_${gate}` as HealthWord)}</p>
  }
  if (st.kind === "closed" || st.kind === "done" || st.kind === "refused") {
    return (
      <div className="adm-notice-actions">
        <button type="button" className="adm-btn adm-btn-small" onClick={() => { setSt({ kind: "picking" }); setBound(false); loadHooks() }}>{say("iv_start")}</button>
        <span role="status" className={st.kind === "refused" ? "adm-stale" : "adm-fine"}>
          {st.kind === "done" ? say(st.replayed ? "iv_replayed" : "iv_startedOk") : st.kind === "refused" ? <>{say("iv_refused", { reason: refusalWord(say, st.code) })} <span className="adm-code">({st.code})</span></> : ""}
        </span>
      </div>
    )
  }
  const locked = st.kind === "busy" || st.kind === "ambiguous"
  const chosen = locked ? st.pending.req.hook_id : hookId
  const ready = hooks.kind === "ready" && hooks.hooks.some((h) => h.hook_id === hookId) && bound
  return (
    <div className="adm-notice adh-iv-start" role="group" aria-label={say("iv_pickTitle")}>
      <p><strong>{say("iv_pickTitle")}</strong></p>
      <p className="adm-fine">{say("iv_bindNote")}</p>
      {hooks.kind === "loading" ? <p className="adm-fine" role="status">{say("iv_hooksLoading")}</p>
        : hooks.kind === "failed" ? (
          <p className="adm-stale" role="status">{say("iv_hooksFailed", { reason: say(`st_fail_${hooks.code}` as HealthWord) })} <button type="button" className="adm-btn adm-btn-small adm-btn-quiet" onClick={loadHooks}>{say("tryAgain")}</button></p>
        ) : hooks.hooks.length === 0 ? <p className="adm-stale" role="status">{say("iv_hooksNone")}</p> : (
          <fieldset className="adh-iv-hooks" disabled={locked}>
            <legend className="adm-fine">{say("iv_hookLegend")}</legend>
            {hooks.hooks.map((h) => (
              <label key={h.hook_id} className="adh-iv-hook">
                <input type="radio" name={`iv-hook-${incidentId}`} value={h.hook_id} checked={chosen === h.hook_id} onChange={() => setHookId(h.hook_id)} />
                <span><code className="adh-id">{h.hook_id}</code> <span className="adm-fine">{say("iv_hookMachine")} <code className="adh-id">{h.machine_id}</code></span></span>
              </label>
            ))}
            {hooks.more ? <p className="adm-fine">{say("iv_hooksMore", { n: hooks.hooks.length })}</p> : null}
            <label className="adh-iv-hook">
              <input type="checkbox" checked={bound || locked} onChange={(e) => setBound(e.target.checked)} />
              <span>{say("iv_bindConfirm")}</span>
            </label>
          </fieldset>
        )}
      <p className="adm-fine">{say("iv_frozenNote", { n: revision })}</p>
      <div className="adm-notice-actions">
        {st.kind === "ambiguous" ? (
          <button type="button" className="adm-btn adm-btn-small" onClick={() => send(st.pending)}>{say("iv_retry")}</button>
        ) : (
          <button type="button" className="adm-btn adm-btn-small" disabled={!ready || st.kind === "busy"} aria-busy={st.kind === "busy"}
            onClick={() => send({ key: newIdempotencyKey(), req: { hook_id: hookId, expected_incident_revision: revision } })}>
            {st.kind === "busy" ? say("iv_sending") : say("iv_send")}
          </button>
        )}
        {!locked ? <button type="button" className="adm-btn adm-btn-small adm-btn-quiet" onClick={() => { ctl.current?.abort(); setSt({ kind: "closed" }) }}>{say("inc_cancel")}</button> : null}
      </div>
      <div role="status" aria-live="polite">
        {st.kind === "ambiguous" ? <p className="adm-stale">{say("iv_ambiguous")} <span className="adm-code">({st.code})</span></p> : null}
      </div>
    </div>
  )
}

/** The investigation card. `key` it by incident and account so a pending key never crosses scopes. */
export function InvestigationPanel({ incidentId, revision, latest, expired, investigations, account, say, actions }: {
  incidentId: string; revision: number; latest: boolean; expired: boolean; investigations: Investigations; account: string | null; say: HealthSay; actions: InvestigationActions
}) {
  const gate = startGate(investigations, latest, expired)
  return (
    <Panel title={say("iv_title")}>
      {investigations.status === "unsupported" ? <p className="adm-fine" role="note">{say("iv_unsupported")}</p>
        : investigations.status === "unavailable" ? <p className="adm-stale" role="status">{say("iv_unavailable")}</p>
          : investigations.items.length === 0 ? <p className="adm-fine">{investigations.unreadable > 0 ? say("iv_onlyUnreadable", { n: investigations.unreadable }) : say("iv_none")}</p> : (
            <>
              <Latest inv={investigations.items[0]} say={say} />
              {investigations.unreadable > 0 ? <p className="adm-stale">{say("iv_unreadable", { n: investigations.unreadable })}</p> : null}
              <Earlier items={investigations.items.slice(1)} say={say} />
            </>
          )}
      <p className="adm-fine">{say("iv_separateNote")}</p>
      <Start incidentId={incidentId} revision={revision} account={account} gate={gate} say={say} actions={actions} />
    </Panel>
  )
}
