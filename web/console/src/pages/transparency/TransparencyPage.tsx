import { useEffect, useState } from "react"
import { catalogWord } from "../../catalog.js"
import { useMachineVersion } from "../../machine/NeedsUpdate.js"
import data from "./catalog.json"
import { facets, filterHash, filtersFromHash, matches, toggleFilter, type Facet, type Filters } from "./model.js"
import "./page.css"

declare const __CLAWDLINE_BUILD_COMMIT__: string
const build = __CLAWDLINE_BUILD_COMMIT__
const word = (key: string) => catalogWord("transparency", key)

export default function TransparencyPage({ shown }: { shown: boolean }) {
  const [filters, setFilters] = useState(() => filtersFromHash(location.hash))
  const machine = useMachineVersion()
  useEffect(() => {
    const changed = () => setFilters(filtersFromHash(location.hash))
    window.addEventListener("hashchange", changed)
    return () => window.removeEventListener("hashchange", changed)
  }, [])
  const change = (next: Filters) => {
    setFilters(next)
    history.replaceState(history.state, "", filterHash(next))
  }
  const row = (facet: Facet) => <fieldset className="transparency-filter" key={facet}>
    <legend>{word(facet)}</legend>
    <div className="transparency-choices">{facets[facet].map(value =>
      <button key={value} type="button" className="chip" aria-pressed={filters[facet].includes(value)}
        onClick={() => change(toggleFilter(filters, facet, value))}>{word(value)}</button>)}</div>
  </fieldset>
  const visible = data.cards.filter(entry => matches(entry, filters))
  return <section className="page transparency-page" id="transparency" data-page-view="transparency" hidden={!shown}>
    <header className="transparency-heading"><h1>{word("title")}</h1><p>{word("intro")}</p></header>
    <div className="transparency-version">
      <span>{word("version")} {build ? <a href={`https://github.com/sainteye/clawdline/tree/${build}`} target="_blank" rel="noreferrer">{build.slice(0, 9)}</a> : word("unknown")}</span>
      <span>{word("machineVersion")} {machine.version || word("unknown")}</span>
      <p>{word("versionHelp")}</p>
    </div>
    <div className="block transparency-filters">
      {row("scope")}{row("agents")}{row("stages")}
      <details><summary>{word("scenes")}</summary>{row("scenes")}</details>
      <div className="transparency-filter-footer"><p>{word("filterHelp")}</p>
        <button type="button" className="chip" onClick={() => change(filtersFromHash(""))}>{word("reset")}</button></div>
    </div>
    <p className="transparency-summary">{word("summary")}</p>
    <div className="transparency-results" aria-live="polite" aria-atomic="false">
      {!visible.length && <p role="status">{word("empty")}</p>}
      {visible.map(entry => {
        const copy = (key: string) => word(`card.${entry.id}.${key}`)
        return <article className="block transparency-entry" key={entry.id} data-entry={entry.id}>
          <h2>{copy("title")}</h2>
          <div className="transparency-tags"><span>{word(entry.scope)}</span>
            {entry.agents.map(value => <span key={value}>{word(value)}</span>)}
            {entry.stages.map(value => <span key={value}>{word(value)}</span>)}
          </div>
          <p className="transparency-condition">{copy("condition")}</p><p>{copy("summary")}</p>
          <dl><dt>{word("how")}</dt><dd>{copy("how")}</dd><dt>{word("effect")}</dt><dd>{copy("effect")}</dd></dl>
          <details><summary>{word("original")}</summary>
            {entry.locations.length > 0 && <div><strong>{word("locations")}</strong><ul className="transparency-sources">{entry.locations.map(path => <li key={path}><code>{path}</code></li>)}</ul></div>}
            <ul className="transparency-sources">{entry.sources.map(source => <li key={`${source.path}:${source.line}`}>
              <a href={`https://github.com/sainteye/clawdline/blob/${build || "main"}/${source.path}#L${source.line}`} target="_blank" rel="noreferrer">{source.path}:{source.line}</a>
            </li>)}</ul>
            {entry.original.length > 0 && <pre>{entry.original.join("")}</pre>}
          </details>
        </article>
      })}
    </div>
    <footer className="transparency-footer"><p>{word("boundary")}</p><p>{word("state")}</p><p>{word("fresh")}</p></footer>
  </section>
}
