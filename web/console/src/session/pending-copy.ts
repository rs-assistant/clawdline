import * as L from "../legacy/bridge.js"
import { nextWord } from "../next-strings.js"
import { writeIsOff } from "./outcome.js"

/** A failed send's catalogued sentence and code tag. */
export function pendingFailureSentence(code: string): string {
  if (code === "session_unknown") {
    return L.failureSentence({ code }, { sentence: nextWord("cloudActionUnknown"), fallback: L.strings.sendFailed })
  }
  if (code === "execution_generation_changed") {
    return L.failureSentence({ code }, { sentence: nextWord("cloudActionChanged"), fallback: L.strings.sendFailed })
  }
  return L.failureSentence({ code }, L.strings.sendFailed)
}

/** Whether the composer would still accept the same write. */
export function pendingFailureCanRetry(code: string): boolean {
  return code !== "execution_generation_changed" && !writeIsOff(code)
}
