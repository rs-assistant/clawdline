/*
 * "This machine's Clawdline is older than this feature", as words and as a
 * decision, without React or a DOM (`needs-update-model.test.ts`).
 *
 * The console at app.clawdline.com is deployed on every commit and reads
 * machines that update on their own schedule (docs/updates.md). A feature
 * whose route the machine lacks used to say 「讀取失敗」, which is a lie about
 * the wrong subject: nothing failed, the machine is simply older. Which
 * failures mean that is `machineNeedsUpdate` (core/src/refusal.ts); this file
 * is what a screen says about it. Whether an entry is offered at all is
 * `pageNeedsUpdate` (pages/registry.ts).
 *
 * Nothing here imports at run time, so node's test runner can load it alone.
 */

/** What the console knows about the current machine's Clawdline. Absent is unknown. */
export interface MachineVersion {
  version?: string
  apiLevel?: number
  /**
   * The version is not known yet, as opposed to not given: a Cloud console has
   * not received the machine's descriptor (Health `version_pending`).
   */
  pending?: boolean
}

/** The part of a `MachineNeedsUpdate` (core/src/refusal.ts) these words read. */
export interface NeedsUpdateFact {
  version?: string
}

type Words = (
  key: "machineNeedsUpdate" | "machineNeedsUpdateVersion",
  holes?: Record<string, string | number>,
) => string

/**
 * The Settings page's optional deep link to its update panel. Incompatibility
 * prompts use the direct update action and do not send a person here.
 */
export const SETTINGS_UPDATE_HREF = "#page=settings&focus=update"

/**
 * The version and route level a `/v1/health` answer names, each only when it
 * is the right type. A daemon from before these fields answers neither, and
 * that is unknown — not version "", not level 0.
 */
export function machineVersionFromHealth(health: unknown): MachineVersion {
  if (typeof health !== "object" || health === null) return {}
  const { version, api_level, version_pending } = health as { version?: unknown; api_level?: unknown; version_pending?: unknown }
  const known = typeof version === "string" && version.trim()
  return {
    ...(known ? { version: version.trim() } : {}),
    ...(typeof api_level === "number" && Number.isInteger(api_level) && api_level >= 0 ? { apiLevel: api_level } : {}),
    ...(!known && version_pending === true ? { pending: true } : {}),
  }
}

/** Two readings are the same when both fields are; lets a store skip a no-op publish. */
export function sameMachineVersion(a: MachineVersion, b: MachineVersion): boolean {
  return a.version === b.version && a.apiLevel === b.apiLevel && !a.pending === !b.pending
}

export interface NeedsUpdateWords {
  sentence: string
  /** 「目前版本：…」, or null when no version is known. */
  version: string | null
}

/**
 * The needs-update line. The refusal's own version wins (the Cloud relay
 * attaches the machine's descriptor version to it); otherwise the one health
 * reported; otherwise none is named rather than guessed.
 */
export function needsUpdateWords(fact: NeedsUpdateFact | null | undefined, known: MachineVersion, word: Words): NeedsUpdateWords {
  const version = fact?.version || known.version
  return {
    sentence: word("machineNeedsUpdate"),
    version: version ? word("machineNeedsUpdateVersion", { version }) : null,
  }
}
