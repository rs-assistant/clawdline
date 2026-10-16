// Explanations for the capacity register. The four commonly full rows have
// specific copy; every other registered row still gets its scope and policy.
import { catalogFormat, catalogWord } from "../../catalog.js"
import type { CapacityEntry } from "@clawdline/contract"

const specific: Record<string, string> = {
  "timeline.entries": "timeline",
  "artifacts.images": "images",
  "iterm.stall_diagnoses": "stall",
  "log.daemon": "log",
}

export function capacityExplanation(row: CapacityEntry): string[] {
  const detail = specific[row.name]
  const subject = detail
    ? catalogWord("capacityHelp", detail)
    : catalogFormat("capacityHelp", "general", [row.name.replaceAll("_", " ").replaceAll(".", " / ")])
  const policy = row.evicted_by === "person"
    ? catalogWord("capacityHelp", "person")
    : catalogWord("capacityHelp", `action_${row.at_limit}`)
  return [subject, catalogFormat("capacityHelp", "limit", [row.limit, row.unit]), policy]
}
