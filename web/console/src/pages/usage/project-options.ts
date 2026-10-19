import { sha256Hex } from "../../session/fingerprint.js"
import type { ProjectPickerOption } from "../../ui/ProjectPicker.js"

/** Usage IDs are SHA-256 of the exact ledger path, not the basename or place ID. */
export function usageProjectOptions(options: { id: string; name: string }[], places: { path: string; icon?: unknown }[], unattributed: string): ProjectPickerOption[] {
  const icons = new Map(places.map(place => [sha256Hex(new TextEncoder().encode(place.path)), place.icon]))
  return options.map(option => ({ id: option.id, label: option.id === "unattributed" ? unattributed : option.name, icon: icons.get(option.id) }))
}
