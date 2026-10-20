// @ts-expect-error -- Node runs source tests with type stripping.
import { sha256Hex } from "../../session/fingerprint.ts"
import type { ProjectPickerOption } from "../../ui/ProjectPicker.js"

/** Only configured projects with readable usage are offered; ledger history is not a project registry. */
export function usageProjectOptions(options: { id: string; name: string }[], places: { path: string; label: string; icon?: unknown }[]): ProjectPickerOption[] {
  const available = new Set(options.map(option => option.id))
  return places.flatMap(place => {
    const id = sha256Hex(new TextEncoder().encode(place.path))
    return available.has(id) ? [{ id, label: place.label, icon: place.icon }] : []
  })
}
