import { TerminalBackButton } from "./TerminalBackButton.js"

/**
 * The hosted terminal before it has a screen, drawn with the terminal page's own classes so the
 * header, the screen box and the key row stand where the live page will put them: nothing moves
 * when the first frame arrives. The words are only in the status line; the rest is decoration.
 */
const LINE_WIDTHS = [62, 38, 0, 84, 71, 55, 0, 90, 47, 76, 33, 0, 68, 58, 81, 24]

export function TerminalSkeletonLines() {
  return <div className="cloud-skel-lines" aria-hidden="true">
    {LINE_WIDTHS.map((width, index) => width === 0
      ? <span key={index} className="cloud-skel-gap" />
      : <span key={index} className="cloud-skel cloud-skel-line" style={{ width: `${width}%` }} />)}
  </div>
}

export function TerminalSkeletonKeys() {
  return <div className="cloud-keyrow" aria-hidden="true">
    <span className="cloud-skel cloud-skel-round" />
    <div className="cloud-quick">{[0, 1, 2, 3, 4, 5].map((index) => <span key={index} className="cloud-skel cloud-skel-chip" />)}</div>
    <span className="cloud-skel cloud-skel-round" />
  </div>
}

/** The whole page while the Project, the machine's line or the terminal itself is still on its way. */
export function CloudTerminalSkeleton({ label, status, onBack }: { label: string; status: string; onBack: () => void }) {
  return <div className="terminal-view cloud-terminal-view" aria-busy="true">
    <header className="terminal-head">
      <div className="terminal-head-row"><TerminalBackButton onClick={onBack} compact />
        <div className="cloud-title">
          {label ? <strong className="terminal-context" title={label}>{label}</strong> : <span className="cloud-skel cloud-skel-title" aria-hidden="true" />}
          <span className="cloud-phase" data-tone="idle" role="status" aria-live="polite">{status}</span>
        </div>
        <span className="cloud-skel cloud-skel-more" aria-hidden="true" />
      </div>
    </header>
    <div className="terminal-scroll"><TerminalSkeletonLines /></div>
    <div className="cloud-dock"><TerminalSkeletonKeys /></div>
  </div>
}
