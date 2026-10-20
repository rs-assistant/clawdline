/* The connection-health page: controls, whole-page states and the screen the
 * route names. It renders inside the existing admin shell, which the root wires;
 * it owns no route table and fetches only through the injected source. */
import { useEffect, useMemo, useRef, useState } from "react"
import "./health.css"
import type { HealthQuery, Scope } from "./types"
import { WINDOWS_S } from "./types"
import { keepSelection, type HealthTarget, type Selection } from "./model"
import { useHealth } from "./useHealth"
import type { HealthSource } from "./source"
import { healthClock, healthWords, type HealthLocale, type HealthWord } from "./words"
import type { HealthNav } from "./parts"
import { EvidenceView, M01Overview, M02Machines, M03Services, M04Trace, M05Incident, M06Monitoring } from "./screens"

export type ConnectionHealthProps = {
  /** Where data comes from. Production passes a source for real routes; never an example source. */
  source: HealthSource
  target: HealthTarget
  nav: HealthNav
  locale: HealthLocale
  /** The scopes this person holds, each authorized separately. One is never derived from another. */
  scopes: readonly Scope[]
}

export function ConnectionHealth(props: ConnectionHealthProps) {
  // With no authorized scope there is nothing to ask for; no scope is assumed on the person's behalf.
  if (props.scopes.length === 0) {
    const say = healthWords(props.locale)
    return (
      <div className="adh">
        <div className="adm-notice" role="alert"><h2>{say("failedTitle")}</h2><p>{say("failure_forbidden")}</p></div>
      </div>
    )
  }
  return <Page {...props} />
}

function Page({ source, target, nav, locale, scopes }: ConnectionHealthProps) {
  const say = useMemo(() => healthWords(locale), [locale])
  const [scope, setScope] = useState<Scope>(scopes[0])
  const [windowS, setWindowS] = useState<number>(3600)
  const [pausedAt, setPausedAt] = useState<string | null>(null)
  const query: HealthQuery = useMemo(() => ({ scope, window_s: windowS, until: pausedAt }), [scope, windowS, pausedAt])
  const { state, refresh } = useHealth(source, query, pausedAt === null)
  const [now, setNow] = useState(() => Date.now())
  const [selection, setSelection] = useState<Selection>({ id: null, chosen: false, absent: false })
  const heading = useRef<HTMLHeadingElement>(null)

  // Ages keep counting while live; paused freezes every age at the pause instant.
  useEffect(() => {
    if (pausedAt !== null) {
      setNow(Date.parse(pausedAt))
      return
    }
    setNow(Date.now())
    const id = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(id)
  }, [pausedAt])

  // The route names the investigation; a refresh never replaces it.
  const routed = target.screen === "M05" || target.screen === "evidence" ? target.incident : null
  useEffect(() => {
    if (routed !== null) setSelection((prev) => (prev.id === routed && prev.chosen ? prev : { id: routed, chosen: true, absent: false }))
  }, [routed])
  const snapshot = state.snapshot
  useEffect(() => {
    if (snapshot) setSelection((prev) => keepSelection(prev, snapshot.incidents))
  }, [snapshot])

  // A route change moves focus to the page heading; a live refresh does not.
  useEffect(() => {
    heading.current?.focus()
  }, [target.screen])

  const title = say(`screen_${target.screen}` as HealthWord)
  const lead = say(`lead_${target.screen}` as HealthWord)
  const props = snapshot ? { snapshot, say, now, nav } : null

  return (
    <div className="adh">
      {snapshot?.example ? <p className="adh-example" role="note">{say("exampleBanner")}</p> : null}
      <header className="adh-head">
        <h1 className="adh-title" tabIndex={-1} ref={heading}>{title}</h1>
        <p className="adm-sub">{lead}</p>
      </header>

      <div className="adh-controls">
        <label className="adh-control">
          <span>{say("scopeLabel")}</span>
          <select value={scope} onChange={(e) => setScope(e.target.value as Scope)} disabled={scopes.length < 2}>
            {scopes.map((sc) => <option key={sc} value={sc}>{say(`scope_${sc}` as HealthWord)}</option>)}
          </select>
        </label>
        <fieldset className="adh-control adh-windows">
          <legend>{say("windowLabel")}</legend>
          {WINDOWS_S.map((w) => (
            <label key={w} className={windowS === w ? "adh-window adh-window-on" : "adh-window"}>
              <input type="radio" name="adh-window" value={w} checked={windowS === w} onChange={() => setWindowS(w)} />
              {say(`window_${w}` as HealthWord)}
            </label>
          ))}
        </fieldset>
        <div className="adh-control adh-live">
          <button type="button" className="adm-btn adm-btn-small" aria-pressed={pausedAt !== null}
            onClick={() => setPausedAt(pausedAt === null ? new Date().toISOString() : null)}>
            {pausedAt === null ? say("pause") : say("resume")}
          </button>
          <button type="button" className="adm-btn adm-btn-small adm-btn-quiet" onClick={refresh} disabled={state.refreshing}>
            {state.refreshing ? say("refreshing") : say("refresh")}
          </button>
        </div>
      </div>
      <p className="adm-fine">{say(`scopeNote_${scope}` as HealthWord)}</p>
      {pausedAt ? <p className="adm-fine">{say("pausedNote", { time: healthClock(pausedAt) })}</p> : null}
      {snapshot ? <p className="adm-stamp adh-stamp">{say("generated", { time: healthClock(snapshot.generated_at) })}</p> : null}

      {state.status === "ready" && state.failure ? (
        <p className="adm-stale" role="status">{say("staleNote", { code: say(`failure_${state.failure}` as HealthWord), time: healthClock(state.snapshot.generated_at) })}</p>
      ) : null}
      {selection.chosen && selection.absent && selection.id ? <p className="adm-stale" role="status">{say("selectionAbsent", { id: selection.id })}</p> : null}

      {state.status === "loading" ? (
        <div className="adm-skeleton" aria-busy="true">
          <p className="adm-skeleton-say" role="status">{say("loading")}</p>
          <div className="adm-tile adm-shimmer" />
          <div className="adm-panel adm-shimmer adm-shimmer-tall" />
        </div>
      ) : state.status === "failed" ? (
        <div className="adm-notice adm-notice-wide" role="alert">
          <h2>{say("failedTitle")}</h2>
          <p>{say("failedBody")}</p>
          <p>{say(`failure_${state.failure}` as HealthWord)}</p>
          <p className="adm-code">{say("failedCode", { code: state.failure })}</p>
          <div className="adm-notice-actions">
            <button type="button" className="adm-btn" onClick={refresh} disabled={state.refreshing}>{say("tryAgain")}</button>
          </div>
        </div>
      ) : props ? (
        target.screen === "M01" ? <M01Overview {...props} />
          : target.screen === "M02" ? <M02Machines {...props} machine={target.machine} />
          : target.screen === "M03" ? <M03Services {...props} />
          : target.screen === "M04" ? <M04Trace {...props} trace={target.trace} />
          : target.screen === "M05" ? <M05Incident {...props} incident={target.incident} />
          : target.screen === "M06" ? <M06Monitoring {...props} />
          : <EvidenceView {...props} incident={target.incident} />
      ) : null}
    </div>
  )
}
