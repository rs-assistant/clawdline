import { loadingLayouts } from "./loading-skeleton.js"
import "./loading-skeleton.css"

/** The text remains accessible; the placeholder never imitates readable data or controls. */
export function LoadingSkeleton({ label, id, kind = "rows" }: {
  label: string; id: string; kind?: keyof typeof loadingLayouts
}) {
  return <div className={`loading-skeleton loading-skeleton-${kind} page-skeleton`} role="status" data-loading-id={id}>
    <span className="loading-skeleton-label">{label}</span>
    <div aria-hidden="true">{loadingLayouts[kind].map((width, index) => <div className="loading-skeleton-row" key={index}>
      <i className="loading-skeleton-mark page-skeleton-line" />
      <div className="loading-skeleton-copy"><i className="page-skeleton-line" style={{ width: `${width}%` }} /><i className="page-skeleton-line" /></div>
    </div>)}</div>
  </div>
}
