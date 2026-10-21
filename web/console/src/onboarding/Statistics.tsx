import { useEffect, useState } from "react"
import { REASONS, SOURCES, TOOLS } from "./model.js"
import { surveyWord as w, type SurveyWord } from "./words.js"
import "./statistics.css"
interface Row { key: string; count: number }
interface Statistics {
  generated_at: string; shown: number | null; submitted: number; skipped: number | null; paired: number | null;
  reasons: Row[]; tools: Row[]; sources: Row[];
  reason_tools: { reason: string; tool: string; count: number }[];
  source_reasons: { source: string; reason: string; count: number }[];
}
export function parseStatistics(value: unknown): Statistics {
  const s = value as Statistics & { schema?: unknown }
  const count = (n: unknown) => typeof n === "number" && Number.isSafeInteger(n) && n >= 0
  if (!s || s.schema !== "clawdline.onboarding.statistics.v1" || !count(s.submitted) ||
    ![s.shown, s.skipped, s.paired].every(n => n === null || count(n)) ||
    !Number.isFinite(Date.parse(s.generated_at))) throw new Error("invalid_statistics")
  for (const [rows, keys] of [[s.reasons, REASONS], [s.tools, TOOLS], [s.sources, SOURCES]] as const) {
    if (!Array.isArray(rows) || rows.some(r => !keys.includes(r.key as never) || !count(r.count) || r.count > s.submitted) || new Set(rows.map(r => r.key)).size !== rows.length) throw new Error("invalid_statistics")
  }
  if (!Array.isArray(s.reason_tools) || s.reason_tools.some(r => !REASONS.includes(r.reason as never) || !TOOLS.includes(r.tool as never) || !count(r.count) || r.count > s.submitted) ||
    !Array.isArray(s.source_reasons) || s.source_reasons.some(r => !SOURCES.includes(r.source as never) || !REASONS.includes(r.reason as never) || !count(r.count) || r.count > s.submitted)) throw new Error("invalid_statistics")
  return s
}
function Distribution({ title, rows, keys, prefix, total }: { title: string; rows: Row[]; keys: readonly string[]; prefix: string; total: number }) {
  return <section className="survey-chart"><h2>{title}</h2><table><thead><tr><th>{w("option")}</th><th>{w("count")}</th><th>{w("share")}</th></tr></thead><tbody>
    {keys.map(key => {
      const count = rows.find(r => r.key === key)?.count ?? 0
      const percent = total ? count / total * 100 : 0
      return <tr key={key}><th scope="row"><span>{w(`${prefix}_${key}` as SurveyWord)}</span><span className="survey-bar" aria-hidden="true"><span style={{ width: percent + "%" }} /></span></th><td>{count}</td><td>{total ? percent.toFixed(1) + "%" : "—"}</td></tr>
    })}
  </tbody></table></section>
}
export function SurveyStatistics({ apiOrigin, demo = false, refreshToken }: { apiOrigin: string; demo?: boolean; refreshToken?: string }) {
  const [filters, setFilters] = useState(() => {
    const until = new Date(); until.setUTCDate(until.getUTCDate() + 1)
    const from = new Date(until); from.setUTCMonth(from.getUTCMonth() - 1)
    return { from: from.toISOString().slice(0, 10), until: until.toISOString().slice(0, 10), scope: "cloud", locale: "", product_version: "", entry: "" }
  })
  const [query, setQuery] = useState(() => new URLSearchParams(Object.entries(filters).filter(([,v]) => v)).toString())
  const [reading, setReading] = useState<{ status: "loading" | "failed" | "ready"; stats?: Statistics }>({ status: "loading" })
  const [refresh, setRefresh] = useState(0)
  useEffect(() => {
    if (demo) return
    const abort = new AbortController(); setReading({ status: "loading" })
    void fetch(apiOrigin + "/su/v1/onboarding/statistics?" + query, { credentials: "include", cache: "no-store", signal: abort.signal })
      .then(async res => { if (!res.ok) throw new Error("unavailable"); return parseStatistics(await res.json()) })
      .then(stats => { if (!abort.signal.aborted) setReading({ status: "ready", stats }) })
      .catch(() => { if (!abort.signal.aborted) setReading({ status: "failed" }) })
    return () => abort.abort()
  }, [apiOrigin, query, refresh, demo, refreshToken])
  if (demo) return <p>{w("demo")}</p>
  const s = reading.stats
  return <div className="survey-stats"><p>{w("statsIntro")}</p>
    <form className="survey-filters" onSubmit={e => { e.preventDefault(); setQuery(new URLSearchParams(Object.entries(filters).filter(([,v]) => v)).toString()); setRefresh(r => r + 1) }}>
      <label>{w("from")}<input type="date" required value={filters.from} onChange={e => setFilters({ ...filters, from: e.target.value })} /></label>
      <label>{w("until")}<input type="date" required value={filters.until} onChange={e => setFilters({ ...filters, until: e.target.value })} /></label>
      <label>{w("respondents")}<select value={filters.scope} onChange={e => setFilters({ ...filters, scope: e.target.value })}><option value="cloud">{w("cloud")}</option><option value="local_browser">{w("local")}</option></select></label>
      <label>{w("language")}<input value={filters.locale} placeholder="zh-Hant" onChange={e => setFilters({ ...filters, locale: e.target.value })} /></label>
      <label>{w("productVersion")}<input value={filters.product_version} placeholder={w("any")} onChange={e => setFilters({ ...filters, product_version: e.target.value })} /></label>
      <label>{w("entry")}<select value={filters.entry} onChange={e => setFilters({ ...filters, entry: e.target.value })}><option value="">{w("any")}</option><option value="onboarding">{w("onboarding")}</option><option value="settings">{w("settingsEntry")}</option></select></label>
      <button type="submit" className="adm-btn">{w("apply")}</button>
    </form>
    {reading.status === "loading" ? <p role="status">{w("loading")}</p> : reading.status === "failed" ? <p role="alert">{w("statsFailed")} <button className="adm-btn" onClick={() => setRefresh(r => r + 1)}>{w("retry")}</button></p> : s ? <>
      <p className="survey-stats-basis">{w(query.includes("scope=local_browser") ? "localBasis" : "cloudBasis")}</p>
      <div className="survey-metrics">{[
        ["shown", s.shown], ["submitted", s.submitted], ["skipped", s.skipped],
        ["completion", s.shown === null || s.shown === 0 ? null : (s.submitted / s.shown * 100).toFixed(1) + "%"], ["paired", s.paired],
      ].map(([label, value]) => <div key={label}><span>{w(label as SurveyWord)}</span><strong>{value === null ? w("unknown") : value}</strong></div>)}</div>
      <p>{w("denominator", { n: s.submitted })}</p>
      {!s.submitted ? <p>{w("empty")}</p> : null}
      <Distribution title={w("reason")} rows={s.reasons} keys={REASONS} prefix="reason" total={s.submitted} />
      <Distribution title={w("tools")} rows={s.tools} keys={TOOLS} prefix="tool" total={s.submitted} />
      <Distribution title={w("source")} rows={s.sources} keys={SOURCES} prefix="source" total={s.submitted} />
      <section className="survey-chart"><h2>{w("crossTools")}</h2><table><thead><tr><th>{w("reason")}</th><th>{w("tools")}</th><th>{w("count")}</th></tr></thead><tbody>{s.reason_tools.map(r => <tr key={r.reason + r.tool}><th scope="row">{w(`reason_${r.reason}` as SurveyWord)}</th><td>{w(`tool_${r.tool}` as SurveyWord)}</td><td>{r.count}</td></tr>)}</tbody></table></section>
      <section className="survey-chart"><h2>{w("crossSource")}</h2><table><thead><tr><th>{w("source")}</th><th>{w("reason")}</th><th>{w("count")}</th></tr></thead><tbody>{s.source_reasons.map(r => <tr key={r.source + r.reason}><th scope="row">{w(`source_${r.source}` as SurveyWord)}</th><td>{w(`reason_${r.reason}` as SurveyWord)}</td><td>{r.count}</td></tr>)}</tbody></table></section>
    </> : null}
  </div>
}
