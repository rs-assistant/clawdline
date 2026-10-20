/* Loads health for one query and keeps it fresh while live: one request at a
 * time, each bounded by its deadline, cancelled when the query changes or the
 * page goes away. A changed scope starts empty, so one scope's figures never
 * flash under another's. */
import { useCallback, useEffect, useReducer, useRef } from "react"
import type { HealthQuery } from "./types"
import { HEALTH_REFRESH_MS, healthReducer, initialHealthState, loadHealth, type HealthSource, type HealthState } from "./source"

export function useHealth(source: HealthSource, query: HealthQuery, live: boolean, everyMs = HEALTH_REFRESH_MS): { state: HealthState; refresh: () => void } {
  const [state, dispatch] = useReducer(healthReducer, initialHealthState)
  const inFlight = useRef<AbortController | null>(null)
  const key = `${query.scope}|${query.window_s}|${query.until ?? ""}`
  const queryRef = useRef(query)
  queryRef.current = query

  const run = useCallback(() => {
    if (inFlight.current) return
    const ctl = new AbortController()
    inFlight.current = ctl
    dispatch({ type: "start" })
    void loadHealth(source, queryRef.current, ctl.signal).then((result) => {
      if (inFlight.current === ctl) inFlight.current = null
      if (!ctl.signal.aborted) dispatch({ type: "result", result, at: Date.now() })
    })
  }, [source])

  useEffect(() => {
    dispatch({ type: "reset" })
    run()
    return () => {
      inFlight.current?.abort()
      inFlight.current = null
    }
  }, [key, run])

  useEffect(() => {
    if (!live) return
    const id = setInterval(run, everyMs)
    return () => clearInterval(id)
  }, [live, run, everyMs])

  return { state, refresh: run }
}
