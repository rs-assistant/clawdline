import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for Node's strip-types test runner.
import { skillOffer, skillResult } from "./skill-model.ts"

const reading = (claude: string, codex: string, outcomes: [string?, string?] = []) => ({
  targets: [
    { assistant: "claude", path: "/h/.claude/skills/clawdline/SKILL.md", state: claude, outcome: outcomes[0] },
    { assistant: "codex", path: "/h/.agents/skills/clawdline/SKILL.md", state: codex, outcome: outcomes[1] },
  ],
})

test("a fresh machine is offered the install", () => {
  assert.deepEqual(skillOffer(reading("missing", "missing"), null), { kind: "install", signature: "claude:missing,codex:missing" })
})

test("an older stub of ours is offered as an update", () => {
  assert.deepEqual(skillOffer(reading("stale", "current"), null), { kind: "update", signature: "claude:stale" })
})

test("somebody's own file, an edit, or an unreadable state is never offered", () => {
  for (const state of ["current", "changed", "other", "unreadable"]) {
    assert.deepEqual(skillOffer(reading(state, state), null), { kind: "none" })
  }
})

test("a dismissal holds until there is something new to say", () => {
  assert.deepEqual(skillOffer(reading("missing", "missing"), "claude:missing,codex:missing"), { kind: "none" })
  assert.equal(skillOffer(reading("stale", "stale"), "claude:missing,codex:missing").kind, "update")
})

test("an older daemon without the route offers nothing", () => {
  assert.deepEqual(skillOffer({ error: "not_implemented" }, null), { kind: "none" })
  assert.deepEqual(skillOffer(null, null), { kind: "none" })
})

test("a press says installed only when every file is now current", () => {
  assert.equal(skillResult(200, reading("current", "current", ["installed", "installed"])), "installed")
  assert.equal(skillResult(200, reading("current", "other", ["installed", "skipped"])), "partial")
  assert.equal(skillResult(200, reading("current", "missing", ["installed", "failed"])), "failed")
  assert.equal(skillResult(403, { error: "forbidden" }), "failed")
})
