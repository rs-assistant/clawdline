/** Resolve an address against the rows the page can actually open. */
export function askedSession(asked: readonly string[] | null, rows: readonly { id: string }[],
  complete: boolean): { kind: "none" | "waiting" | "opened" | "gone"; id?: string } {
  if (!asked) return { kind: "none" }
  const id = asked.find((candidate) => rows.some((row) => row.id === candidate))
  if (id) return { kind: "opened", id }
  return { kind: complete ? "gone" : "waiting" }
}
