import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error Node's strip-types test runner needs the source extension.
import { workflowRunOf, workflowTodoText } from "./workflow-run.ts"

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
