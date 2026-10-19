import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node runs this source test directly.
import { parseState, readLocal, surveyRequest, toggleTool, validAnswers } from "./model.ts"

test("tools are exclusive only for not-started, and empty or unknown answers cannot be submitted", () => {
  assert.deepEqual(toggleTool(["claude"], "codex"), ["claude", "codex"])
  assert.deepEqual(toggleTool(["claude", "codex"], "not_started"), ["not_started"])
  assert.deepEqual(toggleTool(["not_started"], "codex"), ["codex"])
  assert.deepEqual(toggleTool(["codex"], "codex"), [])
  const good = { reason: "phone", tools: ["claude", "codex"], source: "github" }
  assert.ok(validAnswers(good))
  for (const bad of [{ ...good, tools: [] }, { ...good, tools: ["not_started", "codex"] }, { ...good, source: "" }, { ...good, reason: "unknown" }, { ...good, tools: ["claude", "claude"] }]) assert.equal(validAnswers(bad), false)
})
test("corrupt or disabled browser storage never blocks the form; a saved respondent keeps its identity", () => {
  const fresh = readLocal({ getItem: () => { throw new Error("disabled") } })
  assert.equal(fresh.state.status, "new")
  assert.deepEqual(readLocal({ getItem: () => JSON.stringify(fresh) }), fresh)
  assert.equal(readLocal({ getItem: () => '{"state":' }).state.status, "new")
  assert.throws(() => parseState({ version: 1, revision: 1, status: "submitted", answers: null }))
})
test("submissions omit cookies for local respondents and retain explicit consent; malformed success and HTTP failures stay failures", async () => {
  const original = globalThis.fetch
  const saved = { version: 1, revision: 1, status: "submitted", answers: { reason: "phone", tools: ["codex"], source: "github" } }
  try {
    globalThis.fetch = (async (_url, init) => {
      assert.equal(init?.credentials, "omit")
      assert.equal(JSON.parse(String(init?.body)).consent, true)
      assert.equal(new Headers(init?.headers).get("idempotency-key"), "same-key")
      return new Response(JSON.stringify(saved))
    }) as typeof fetch
    assert.deepEqual(await surveyRequest("https://example.test", "/survey", { local: true, key: "same-key", body: { consent: true } }), saved)
    globalThis.fetch = (async () => new Response(JSON.stringify({ error: { code: "survey_revision_conflict", details: { current: saved } } }), { status: 409 })) as typeof fetch
    await assert.rejects(surveyRequest("https://example.test", "/survey"), (err: Error & { current?: unknown }) => err.message === "survey_revision_conflict" && !!err.current)
    globalThis.fetch = (async () => new Response('{}')) as typeof fetch
    await assert.rejects(surveyRequest("https://example.test", "/survey"), /survey_invalid_response/)
  } finally { globalThis.fetch = original }
})
