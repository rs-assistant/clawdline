import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- Node's type-stripping runner resolves the source .ts file.
import { BRAND_MARK, projectedSessionIdentity } from "./brand-mark.ts"

test("Cloud Session cards keep the machine identity through registration", () => {
  const provider = { accent: "#55aa77", cells: [["#55aa77"]] }
  const pending = projectedSessionIdentity("Codex · machine-workspace", provider, true, false)
  assert.equal(pending.title, "Clawdfather")
  assert.equal(pending.icon, BRAND_MARK)
  assert.equal(pending.crown, true)
  assert.equal(pending.pending, true)
  const bound = projectedSessionIdentity("A renamed conversation", provider, true, true)
  assert.equal(bound.title, "Clawdfather")
  assert.equal(bound.icon, BRAND_MARK)
  assert.equal(bound.crown, true)
  assert.equal(bound.pending, false)
  const ordinary = projectedSessionIdentity("Work", provider, false, false)
  assert.equal(ordinary.title, "Work")
  assert.equal(ordinary.icon, provider)
  assert.equal(ordinary.crown, false)
})
