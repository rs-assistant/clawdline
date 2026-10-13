import { useSyncExternalStore } from "react"
import type { MachineNeedsUpdate } from "@clawdline/core"
import { catalogWordLanguage } from "../catalog.js"
import { nextWord } from "../next-strings.js"
import { UpdateBanner } from "./UpdateBanner.js"
import { currentUpdateRead, subscribeUpdate } from "./update.js"
import { INSTALL_COMMAND } from "./update-model.js"
import { needsUpdateWords, sameMachineVersion, type MachineVersion } from "./needs-update-model.js"

/*
 * The current machine's version and route level, from the one health read
 * the connection light already makes every fifteen seconds (App.tsx
 * `useConnectionLight` publishes it here). A second poll would be a second
 * request for the same answer.
 */
let current: MachineVersion = {}
const listeners = new Set<() => void>()

/** Called with each health answer's reading; a reading that did not change wakes nobody. */
export function publishMachineVersion(next: MachineVersion): void {
  if (sameMachineVersion(current, next)) return
  current = next
  for (const listener of listeners) listener()
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  return () => { listeners.delete(listener) }
}

/** `{ version?, apiLevel? }` for the machine this console reads; absent is unknown. */
export function useMachineVersion(): MachineVersion {
  return useSyncExternalStore(subscribe, () => current, () => current)
}

/**
 * 「這台機器的 Clawdline 版本較舊，沒有這個功能。更新後就能使用。」, the
 * machine's version when known, and the direct update action when an installable
 * release exists. Older machines that cannot answer the update route get the
 * machine-side install command instead of a link that cannot update them.
 */
export function NeedsUpdate({ update, className = "work-note" }: { update: MachineNeedsUpdate | null; className?: string }) {
  const known = useMachineVersion()
  const words = needsUpdateWords(update, known, nextWord)
  const read = useSyncExternalStore(subscribeUpdate, currentUpdateRead, currentUpdateRead)
  const legacy = read?.kind === "older" || (read?.kind === "status" && !read.status.install_kind)
  const source = read?.kind === "status" && read.status.install_kind === "source_checkout"
    ? nextWord("updateSourceCheckout")
    : read?.kind === "status" && read.status.install_kind === "source_deploy"
      ? nextWord("updateSourceDeploy")
      : null
  return (
    <>
      <p className={className} role="status" data-needs-update={update?.code ?? ""} style={{ overflowWrap: "anywhere" }}>
        <span lang={catalogWordLanguage("next", "machineNeedsUpdate")}>{words.sentence}</span>
        {words.version ? <> <span lang={catalogWordLanguage("next", "machineNeedsUpdateVersion")}>{words.version}</span></> : null}
      </p>
      <UpdateBanner embedded />
      {legacy ? <p className="say" role="status">{nextWord("updateApplyOlder")} <code className="update-command">{INSTALL_COMMAND}</code></p> : null}
      {source ? <p className="say" role="status">{source}</p> : null}
    </>
  )
}
