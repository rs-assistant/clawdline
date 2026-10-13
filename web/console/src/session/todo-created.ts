import type { DirectTodoV2, SessionWorkV2 } from "../pages/work/api.js"

/**
 * Keep a successful create receipt visible until a full read has observed it.
 *
 * The create reply is durable evidence: hiding it because the next Cloud read
 * is delayed or briefly answers an older projection makes a successful write
 * look lost. A full read that contains the row releases the receipt and owns
 * every later change to it.
 */
export function retainCreatedTodos(page: SessionWorkV2,
  retained: Map<string, DirectTodoV2>): SessionWorkV2 {
  const observed = new Set(page.direct_todos.map(({ id }) => id))
  for (const id of observed) retained.delete(id)
  const missing = [...retained.values()].filter(({ id }) => !observed.has(id))
  return missing.length ? { ...page, direct_todos: [...page.direct_todos, ...missing] } : page
}

/** Draw one create receipt even before detail loaded, replacing its older receipt. */
export function showCreatedTodo(page: SessionWorkV2 | null, todo: DirectTodoV2): SessionWorkV2 {
  const visible = page ?? {
    ok: true, assigned_items: [], recent_items: [], direct_todos: [], open_decisions: [], truncated: false,
  }
  const index = visible.direct_todos.findIndex(({ id }) => id === todo.id)
  if (index < 0) return { ...visible, direct_todos: [...visible.direct_todos, todo] }
  const direct_todos = [...visible.direct_todos]
  direct_todos[index] = todo
  return { ...visible, direct_todos }
}
