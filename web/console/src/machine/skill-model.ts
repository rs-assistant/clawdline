/**
 * What the skill strip says, from `GET /v1/skill` and `POST /v1/skill/install`
 * (skill.schema.json). No fetching here: `SkillBanner.tsx` asks, this decides.
 *
 * Only `missing` and `stale` are offered. They are the two states the press
 * may change — nothing there, or an older file of ours — and anything else is
 * somebody's own file, which a button is the wrong place to decide about.
 */

export const SKILL_BANNER_DISMISSED_KEY = "clawdline.skill-banner.dismissed"

export interface SkillTargetRead {
  assistant: string
  state: string
  outcome?: string
}

export type SkillOffer = { kind: "none" } | { kind: "install" | "update"; signature: string }

const OFFERED = new Set(["missing", "stale"])

/**
 * The offer for one reading, or none. `signature` is what a dismissal keeps,
 * so a strip put away for "missing" comes back once an install is stale — a
 * newer build is a new thing to say — and not before.
 */
export function skillOffer(parsed: unknown, dismissed: string | null): SkillOffer {
  const targets = skillTargets(parsed)
  const offered = targets.filter((target) => OFFERED.has(target.state))
  if (offered.length === 0) return { kind: "none" }
  const signature = offered.map((target) => `${target.assistant}:${target.state}`).join(",")
  if (signature === dismissed) return { kind: "none" }
  return { kind: offered.some((target) => target.state === "missing") ? "install" : "update", signature }
}

export type SkillResult = "installed" | "partial" | "failed"

/** What one press did, across every target. */
export function skillResult(status: number, parsed: unknown): SkillResult {
  const targets = skillTargets(parsed)
  if (status !== 200 || targets.length === 0) return "failed"
  if (targets.some((target) => target.outcome === "failed")) return "failed"
  // A file that was never ours, or was edited since, is still not current.
  return targets.every((target) => target.state === "current") ? "installed" : "partial"
}

function skillTargets(parsed: unknown): SkillTargetRead[] {
  if (!parsed || typeof parsed !== "object") return []
  const list = (parsed as { targets?: unknown }).targets
  if (!Array.isArray(list)) return []
  return list.filter((item): item is SkillTargetRead =>
    Boolean(item) && typeof item === "object" &&
    typeof (item as SkillTargetRead).assistant === "string" &&
    typeof (item as SkillTargetRead).state === "string")
}
