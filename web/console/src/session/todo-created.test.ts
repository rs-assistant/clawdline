import assert from "node:assert/strict"
import test from "node:test"
import type { DirectTodoV2, SessionWorkV2 } from "../pages/work/api.ts"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { retainCreatedTodos, showCreatedTodo } from "./todo-created.ts"

const todo = (id: string, text = id): DirectTodoV2 => ({
  id, text, created_at: 1, sent_at: null, read_at: null, completed_at: null, version: 1,
})

const page = (direct_todos: DirectTodoV2[]): SessionWorkV2 => ({
  ok: true, assigned_items: [], recent_items: [], direct_todos, open_decisions: [], truncated: false,
})

test("a creation receipt is visible before the first full Session work read", () => {
  const retained = new Map<string, DirectTodoV2>()
  const created = todo("new", "Written on Linux")
  retained.set(created.id, created)
  const visible = showCreatedTodo(null, created)
  assert.deepEqual(visible.direct_todos, [created])
  assert.deepEqual([...retained], [[created.id, created]])
})

test("a newer create receipt replaces the provisional row without duplication", () => {
  const created = todo("new")
  const visible = showCreatedTodo(page([created]), { ...created, version: 2 })
  assert.deepEqual(visible.direct_todos.map(({ id, version }) => [id, version]), [["new", 2]])
})

test("an older read cannot erase a created to-do before a read observes it", () => {
  const created = todo("new")
  const retained = new Map([[created.id, created]])
  assert.deepEqual(retainCreatedTodos(page([todo("old")]), retained).direct_todos.map(({ id }) => id), ["old", "new"])
  assert.equal(retained.has(created.id), true)

  const observed = retainCreatedTodos(page([todo("old"), { ...created, version: 2 }]), retained)
  assert.deepEqual(observed.direct_todos.map(({ id, version }) => [id, version]), [["old", 1], ["new", 2]])
  assert.equal(retained.has(created.id), false)
})
