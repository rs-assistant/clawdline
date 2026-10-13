import type { ReactNode } from "react"
import "./page-bar.css"

/**
 * The bar at the top of a sidebar page: the way back on the left, Read again
 * on the right, and it stays on screen while the page scrolls (page-bar.css).
 * A page scrolled to its last note still has its way back one press away.
 */
export function PageBar({ back, refresh, id }: { back?: ReactNode; refresh?: ReactNode; id?: string }) {
  return (
    <header className="page-bar" id={id}>
      <div className="page-bar-start">{back}</div>
      <div className="page-bar-end">{refresh}</div>
    </header>
  )
}

/** The circular arrow, drawn on the same 24-unit stroke canvas as the Work icons. */
export function RefreshIcon() {
  return (
    <svg className="page-bar-icon" viewBox="0 0 24 24" aria-hidden="true" focusable="false">
      <path d="M19 12a7 7 0 1 1-2.05-4.95" />
      <path d="M19 4.5V9h-4.5" />
    </svg>
  )
}

/**
 * Read again, as an icon with its word as the accessible name and tooltip.
 * While a read is under way the arrow turns and the button is not pressable.
 */
export function RefreshButton({ label, busy, onClick, id }: { label: string; busy: boolean; onClick: () => void; id?: string }) {
  return (
    <button className="page-bar-refresh" id={id} type="button" aria-label={label} title={label}
      disabled={busy} aria-busy={busy || undefined} data-busy={busy || undefined} onClick={onClick}>
      <RefreshIcon />
    </button>
  )
}

/** The way back, an arrow and the name of where it goes. */
export function BackButton({ label, onClick, className }: { label: string; onClick: () => void; className?: string }) {
  return (
    <button className={"page-bar-back" + (className ? " " + className : "")} type="button" onClick={onClick}>
      <svg className="page-bar-icon" viewBox="0 0 24 24" aria-hidden="true" focusable="false">
        <path d="M19 12H5m6-6-6 6 6 6" />
      </svg>
      <span>{label}</span>
    </button>
  )
}
