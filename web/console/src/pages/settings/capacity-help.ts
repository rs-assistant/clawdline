// Explanations for the capacity register. Every registered row has its own two
// sentences in the catalog: what it counts and why it has a ceiling, and what a
// person notices when it is full. A sentence true of every row says nothing, so
// there is no generic subject; the register guard in internal/domain/capacity
// fails a row registered without its copy.
import { catalogWord } from "../../catalog.js"
import type { CapacityEntry } from "@clawdline/contract"

export interface CapacityExplanation {
  what: string
  full: string
}

/**
 * The row's own copy, or null when this console predates the row (a newer
 * daemon read through an older bundle). Then the block shows the grey policy
 * line it already has, rather than a sentence invented from the name.
 */
export function capacityExplanation(row: CapacityEntry): CapacityExplanation | null {
  const what = own(`${row.name}.what`)
  const full = own(`${row.name}.full`)
  return what && full ? { what, full } : null
}

function own(key: string): string | null {
  const id = `capacityRow.${key}`
  const text = catalogWord("capacityRow", key)
  return text === id ? null : text
}
