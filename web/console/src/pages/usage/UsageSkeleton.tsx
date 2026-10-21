import { usageWord as w } from "./words.js"
import "./usage.css"

/** The lazy page and its first data read share the same content footprint. */
export function UsageSkeleton() {
  return <div className="usage-skeleton" role="status">
    <span className="usage-loading-label">{w("usageLoading")}</span>
    <div aria-hidden="true">
      <div className="usage-totals">{[0, 1].map(key => <div className="usage-total" key={key}>
        <i className="usage-placeholder usage-placeholder-label" />
        <i className="usage-placeholder usage-placeholder-number" />
        <i className="usage-placeholder usage-placeholder-prior" />
      </div>)}</div>
      <div className="usage-chart-head"><i className="usage-placeholder usage-placeholder-control" /><i className="usage-placeholder usage-placeholder-label" /></div>
      <div className="usage-placeholder usage-placeholder-chart" />
      <i className="usage-placeholder usage-placeholder-caption" />
      <div className="usage-columns">{[0, 1].map(key => <section key={key}>
        <i className="usage-placeholder usage-placeholder-label" />
        <div className="usage-placeholder-distribution">{[0, 1, 2, 3].map(row => <i key={row} className="usage-placeholder" />)}</div>
      </section>)}</div>
      <div className="usage-duration"><i className="usage-placeholder usage-placeholder-label" /><div className="usage-placeholder usage-placeholder-chart" /></div>
    </div>
  </div>
}

export function UsagePageSkeleton({ shown }: { shown: boolean }) {
  return <section className="page board-page usage-page" hidden={!shown} aria-label={w("usageOverview")} aria-busy="true">
    <div className="usage-wrap">
      <header className="usage-header" aria-hidden="true">
        <div className="usage-heading"><div className="usage-title-block"><h1>{w("usageOverview")}</h1>
          <div className="usage-metadata"><i className="usage-placeholder usage-placeholder-caption" /><i className="usage-placeholder usage-placeholder-label" /></div>
        </div><i className="usage-placeholder usage-placeholder-refresh" /></div>
        <div className="usage-filters"><div className="usage-project-filter"><span>{w("usageProject")}</span><i className="usage-placeholder usage-placeholder-control" /></div>
          <div className="usage-switch"><i className="usage-placeholder usage-placeholder-period" /><i className="usage-placeholder usage-placeholder-period" /></div>
        </div>
      </header>
      <div className="usage-body"><UsageSkeleton /></div>
    </div>
  </section>
}
