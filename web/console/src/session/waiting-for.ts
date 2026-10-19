import type { SessionRow } from "@clawdline/contract"
import type { nextWord } from "../next-strings.js"

/**
 * What a waiting Session waits for, in words, from the row alone.
 *
 * Every list draws this from the daemon's `waiting_for`, which the daemon
 * collects in the same pass that decides the row waits. The local list, the
 * selected-machine Cloud list and the all-machines Cloud list therefore say
 * the same thing about the same Session: none of them reads a task collection
 * of its own, which is what once made one scope name the work and another say
 * only that it waited.
 *
 * `word` is `nextWord`, passed in so node's test runner can load this file.
 *
 * `covered` means the row waits only on the heavy command its 🏗️ marker
 * already names, so the hourglass would repeat it.
 */
export function waitingForWords(row: Partial<SessionRow>, word: typeof nextWord): { text: string; title: string; covered: boolean } {
  const targets = (row.waiting_for ?? []).filter((target) => target.task_id !== row.heavy_work?.task_id)
  const omitted = row.waiting_for_omitted ?? 0
  if (!targets.length && !omitted) return { text: "", title: "", covered: (row.waiting_for ?? []).length > 0 }
  const names = targets.map((target) => {
    if (target.kind !== "heavy" || !target.phase) return target.label
    return word(target.phase === "running" ? "sessionWaitHeavyRunning" : "sessionWaitHeavyQueued", { command: target.label })
  })
  const list = names.join(" · ") + (omitted ? "  +" + String(omitted) : "")
  const text = targets.every((target) => target.kind === "session")
    ? word("sessionWaitingOnNamedSession", { session: list })
    : word("sessionWaitingOnWork", { work: list })
  return { text, title: names.join("\n"), covered: false }
}

/** Queued callbacks have not started their command and are not running shells. */
export function runningShellCount(row: Partial<SessionRow>): number {
  return (row.shells ?? []).filter((shell) => shell.state !== "queued").length
}
