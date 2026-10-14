import type { Screen, ScanSource } from "@clawdline/contract"
import type { NextWord } from "../next-strings.js"
import type { ReadState } from "../read-state.js"

/** The next action for the screen's three materially different failure paths. */
export function screenFailureWord(
  reading: Extract<ReadState<Screen | null>, { phase: "refused" | "unanswered" }>,
): NextWord | null {
  if (reading.phase === "unanswered") return "screenUnanswered"
  const failure = reading.error as { code?: unknown } | null | undefined
  const code = typeof failure?.code === "string" ? failure.code : ""
  if (code === "session_not_found" || code === "not_found") return "screenSessionGone"
  if (code === "forbidden") return "screenForbidden"
  return null
}

/** A disabled source explains this screen only when it is actually unreadable. */
export function screenITermScanOff(
  screen: Pick<Screen, "backend" | "readable"> | null,
  sources: readonly ScanSource[] | undefined,
): boolean {
  return screen?.backend === "iterm" && screen.readable === false &&
    !!sources?.some((source) => source.source === "iterm" && source.disabled === "setting")
}
