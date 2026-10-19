import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error Node's strip-types test runner needs the source extension.
import { workflowRunOf, workflowTodoText } from "./workflow-run.ts"
// @ts-expect-error Node's strip-types test runner needs the source extension.
import { localizedWorkflow } from "../workflow-words.ts"
// @ts-expect-error Node's strip-types test runner needs the source extension.
import { activateCatalog, resetCatalogForTest } from "../catalog.ts"
import english from "../../public/catalogs/en.json" with { type: "json" }
import traditionalChinese from "../../public/catalogs/zh-Hant.json" with { type: "json" }
import type { Snippet } from "../legacy/snippets-bridge.js"

const ID = "00000000-0000-4000-8000-000000000041"

test("a run keeps the launch-time title and full message after the definition changes", () => {
  const message = "Check the release.\nThen ship it."
  const sent = workflowTodoText(ID, "今天出貨", message)
  assert.deepEqual(workflowRunOf(sent), { id: ID, title: "今天出貨", message })
})

test("ordinary to-dos and malformed markers never become Workflow runs", () => {
  assert.equal(workflowRunOf("Check the release."), null)
  assert.equal(workflowRunOf("Check the release.\n\n[Clawdline-Workflow missing Title]"), null)
  assert.equal(workflowRunOf("Check the release.\n\n[Clawdline-Workflow " + ID + " %ZZ]"), null)
})

test("the shipped global Workflow follows the UI language until edited", () => {
  const row: Snippet = { id: "00000000-0000-4000-8000-000000000101", title: "Zero-Based Review",
    body: "Run a zero-based review.", scope: "workflow", builtin: "zero_review",
    position: 100, created_at: 1, updated_at: 1 }
  const prior = globalThis.document
  try {
    globalThis.document = { documentElement: { lang: "zh-Hant-TW" } } as Document
    activateCatalog(traditionalChinese, "zh-Hant")
    const zh = localizedWorkflow(row)
    assert.equal(zh.title, "歸零審查")
    assert.match(zh.body, /每項結論都要附上證據/)
    assert.doesNotMatch(zh.body, /唯讀/)
    globalThis.document = { documentElement: { lang: "en" } } as Document
    activateCatalog(english, "en")
    const en = localizedWorkflow(row)
    assert.equal(en.title, "Zero-Based Review")
    assert.match(en.body, /Require evidence for every finding/)
    assert.deepEqual(localizedWorkflow({ ...row, builtin: undefined, title: "My review" }),
      { ...row, builtin: undefined, title: "My review" })
  } finally { resetCatalogForTest(); globalThis.document = prior }
})
