import assert from "node:assert/strict"
import test from "node:test"
import type { SessionRow } from "@clawdline/contract"
import type { SessionWorkV2 } from "./api.js"
// @ts-expect-error -- a `.ts` path is required by Node's native type stripping.
import { assignmentCandidates, assignmentChoiceRows, assignmentChoices, assignmentChoicesWords, assistantName, sessionListRead, awaitsAcceptance, rememberAssistant, rememberedAssistant, sessionActivityName, sessionWorkCounts, sessionWorkLabel, sessionWorkStateName } from "./session-assignment.ts"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { withCatalog } from "../../catalog-testing.ts"

withCatalog("zh-Hant")

test("assignment choices distinguish live activity from unreadable state", () => {
  assert.equal(sessionActivityName("working"), "Working")
  assert.equal(sessionActivityName("idle"), "Idle")
  assert.equal(sessionActivityName("unknown"), "狀況不明")
})

test("assignment choices explain what an otherwise idle Session needs", () => {
  assert.equal(sessionWorkStateName("ready" as SessionRow["work_state"]), "可接工作")
  assert.equal(sessionWorkStateName("waiting_you" as SessionRow["work_state"]), "等你回應")
  assert.equal(sessionWorkStateName("milestone_complete" as SessionRow["work_state"]), "待驗收")
})

test("unfinished counts include open Board items, their pending steps, and direct to-dos", () => {
  const page = {
    assigned_items: [{ id: "board-a", steps: [{ done: false }, { done: true }] }, { id: "board-b" }],
    direct_todos: [{ id: "todo-a" }],
    recent_items: [{ id: "done-a" }, { id: "done-b" }],
  } as SessionWorkV2
  assert.deepEqual(sessionWorkCounts(page), { board: 2, todos: 2, unfinished: 4 })
})

test("assignment choices name which company's assistant each Session runs", () => {
  assert.equal(assistantName("claude"), "Claude Code")
  assert.equal(assistantName("codex"), "Codex")
  assert.equal(assistantName(undefined), "助理不明")
})

test("a new Session keeps the assistant last chosen, and Codex before any choice", () => {
  const kept = new Map<string, string>()
  const storage = { getItem: (k: string) => kept.get(k) ?? null, setItem: (k: string, v: string) => void kept.set(k, v) }
  assert.equal(rememberedAssistant(storage), "codex")
  rememberAssistant("claude", storage)
  assert.equal(rememberedAssistant(storage), "claude")
  kept.set("clawdline.work.new-session-assistant", "gemini")
  assert.equal(rememberedAssistant(storage), "codex")
  const blocked = { getItem: () => { throw new Error("blocked") }, setItem: () => { throw new Error("blocked") } }
  assert.equal(rememberedAssistant(blocked), "codex")
  assert.doesNotThrow(() => rememberAssistant("claude", blocked))
})

test("an owned item can be handed to any other Session in its Project, never back to its owner", () => {
  const row = (id: string, sessionId: string, cwd = "/p") => ({ id, sessionId, cwd }) as SessionRow
  const sessions = [row("%1", "owner"), row("%2", "other"), row("%3", ""), row("%4", "elsewhere", "/q")]
  const item = { project: { path: "/p" }, owner_session: "owner" }
  assert.deepEqual(assignmentCandidates(sessions, item).map((s) => s.id), ["%2"])
  assert.deepEqual(assignmentCandidates(sessions, { ...item, owner_session: null }).map((s) => s.id), ["%1", "%2"])
})

const emptySentence = "這個 Project 目前沒有可用的 Session。"
const candidate = (id: string, sessionId: string | undefined, cwd: string | undefined = "/p") => ({ id, sessionId, cwd }) as SessionRow
const project = { project: { path: "/p" }, owner_session: null }
const said = (reading: Parameters<typeof assignmentChoices>[0], sessions: SessionRow[]) =>
  assignmentChoicesWords(assignmentChoices(reading, sessions, project))

test("a Session list still being read never says the Project has no Session", () => {
  assert.equal(said({ phase: "loading" }, []), "正在讀取 Session 清單…")
  assert.notEqual(said({ phase: "loading" }, []), emptySentence)
  // Rows kept from an earlier reading do not make a pending one an answer.
  assert.equal(said({ phase: "loading" }, [candidate("%1", "s1", "/q")]), "正在讀取 Session 清單…")
})

test("a failed Session read says so and offers to read again", () => {
  const choices = assignmentChoices({ phase: "failed", error: "逾時" }, [], project)
  assert.equal(choices.phase, "failed")
  assert.equal(assignmentChoicesWords(choices), "讀不到 Session 清單：逾時")
  // Rows from an earlier reading stay choosable beside the failure.
  const kept = assignmentChoices({ phase: "failed", error: "逾時" }, [candidate("%1", "s1")], project)
  assert.deepEqual(assignmentChoiceRows(kept).map((s) => s.id), ["%1"])
})

test("only a complete answer with no candidate says the Project has no Session", () => {
  assert.equal(said({ phase: "ready", complete: true }, []), emptySentence)
  assert.equal(said({ phase: "ready", complete: true }, [candidate("%1", "s1", "/q")]), emptySentence)
  const found = assignmentChoices({ phase: "ready", complete: true }, [candidate("%1", "s1")], project)
  assert.equal(found.phase, "candidates")
  assert.equal(assignmentChoicesWords(found), null)
  assert.deepEqual(assignmentChoiceRows(found).map((s) => s.id), ["%1"])
})

test("a partial list, or a row without its cwd or conversation yet, is incomplete rather than empty", () => {
  const incomplete = "Session 清單還沒讀完整，這個 Project 的 Session 可能還不在裡面。"
  assert.equal(said({ phase: "ready", complete: false }, []), incomplete)
  assert.equal(said({ phase: "ready", complete: true }, [candidate("%1", undefined)]), incomplete)
  assert.equal(said({ phase: "ready", complete: true }, [candidate("%1", "s1", "")]), incomplete)
  // The item's own owner cannot be offered, so lacking a cwd it hides nothing.
  assert.equal(assignmentChoicesWords(assignmentChoices({ phase: "ready", complete: true },
    [candidate("%1", "owner", "")], { ...project, owner_session: "owner" })), emptySentence)
  // Candidates already found are still offered while the list settles.
  const partial = assignmentChoices({ phase: "ready", complete: false }, [candidate("%1", "s1")], project)
  assert.deepEqual([partial.phase, assignmentChoiceRows(partial).map((s) => s.id)], ["incomplete", ["%1"]])
})

test("a scan is complete unless a source is still settling; a source turned off does not hold it open", () => {
  assert.deepEqual(sessionListRead({ complete: true }), { phase: "ready", complete: true })
  // Cloud's relay snapshot names no sources and says complete only once every machine's list arrived.
  assert.deepEqual(sessionListRead({ complete: false }), { phase: "ready", complete: false })
  const source = (complete: boolean, disabled?: boolean) => ({ complete, disabled }) as NonNullable<Parameters<typeof sessionListRead>[0]["sources"]>[number]
  assert.deepEqual(sessionListRead({ complete: false, sources: [source(true), source(false, true)] }), { phase: "ready", complete: true })
  assert.deepEqual(sessionListRead({ complete: false, sources: [source(true), source(false)] }), { phase: "ready", complete: false })
})

test("a reported turn reads 本輪已回報 until an owned item awaits acceptance, and says when the Board was unread", () => {
  const reported = (acceptance?: SessionRow["acceptance"], work_state = "milestone_complete") => ({
    work_state: work_state as SessionRow["work_state"],
    disposition: { scope: "session", evidence: "authenticated_session_delivery", title: "summary" },
    acceptance,
  })
  assert.equal(sessionWorkLabel(reported({ state: "none" })), "本輪已回報")
  // A daemon that sends no answer has not said there is none.
  assert.equal(sessionWorkLabel(reported(undefined)), "本輪已回報 · 讀不到是否有待驗收項目")
  assert.equal(sessionWorkLabel(reported({ state: "pending", work_id: "w", title: "Ship it", phase: "deploying", count: 1 })), "待驗收 · Ship it")
  assert.equal(sessionWorkLabel(reported({ state: "pending", work_id: "w", title: "Ship it", phase: "done", count: 3 })), "待驗收 · Ship it · 共 3 項")
  assert.equal(sessionWorkLabel(reported({ state: "unknown" })), "本輪已回報 · 讀不到是否有待驗收項目")
  // A pending answer that names nothing is not trusted as one.
  assert.equal(sessionWorkLabel(reported({ state: "pending" })), "本輪已回報 · 讀不到是否有待驗收項目")
  assert.equal(sessionWorkLabel(reported({ state: "none" }, "work_complete")), "工作已完成")
  assert.equal(sessionWorkLabel(reported({ state: "pending", title: "Ship it" }, "work_complete")), "待驗收 · Ship it")
  // A task's delivery and an unfinished Session keep their own words.
  assert.equal(sessionWorkLabel({ work_state: "milestone_complete", disposition: { scope: "task", evidence: "authenticated_task_delivery", title: "t" },
    acceptance: { state: "pending", title: "Ship it" } }), "待驗收")
  assert.equal(sessionWorkLabel({ work_state: "working", acceptance: { state: "pending", title: "Ship it" } }), "執行中")
})

test("only deploying and done items wait for the person to look", () => {
  assert.equal(awaitsAcceptance("deploying"), true)
  assert.equal(awaitsAcceptance("done"), true)
  assert.equal(awaitsAcceptance("verifying"), false)
  assert.equal(awaitsAcceptance("cancelled"), false)
})
