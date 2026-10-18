import type { Ref } from "react"
import { nextWord } from "../../next-strings.js"

/** A compact way back from a terminal to the terminal list; `compact` keeps only the arrow on screen. */
export function TerminalBackButton({ onClick, buttonRef, compact = false }: { onClick: () => void; buttonRef?: Ref<HTMLButtonElement>; compact?: boolean }) {
  return <button className="board-button terminal-back" type="button" ref={buttonRef} onClick={onClick}
    aria-label={nextWord("terminalBackSessions")}>
    <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">
      <path d="M19 12H5m6-6-6 6 6 6" />
    </svg>
    <span className={compact ? "terminal-sr" : undefined}>{nextWord("terminalBack")}</span>
  </button>
}
