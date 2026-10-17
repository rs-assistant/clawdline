import { nextWord } from "../next-strings.js"

/** A callback's broker phase is separate from whether the assistant is idle. */
export function callbackWord(state: string | undefined): string {
  switch (state) {
    case "queued": return nextWord("bgQueued")
    case "running": return nextWord("bgRunning")
    case "success": return nextWord("bgSuccess")
    case "failure": return nextWord("bgFailure")
    case "timeout": return nextWord("bgTimeout")
    case "cancelled": return nextWord("bgCancelled")
    default: return nextWord("bgUnavailable")
  }
}
