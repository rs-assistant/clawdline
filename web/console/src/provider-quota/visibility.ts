// @ts-expect-error -- Node's source tests import the TypeScript module directly.
import { QUOTA_VISIBLE_KEY, readQuotaVisible, saveQuotaVisible } from "./model.ts"
let current = readQuotaVisible()
const listeners = new Set<() => void>()
export const quotaVisible = () => current
export function subscribeQuotaVisible(listener: () => void): () => void {
  listeners.add(listener)
  const storage = (event: StorageEvent) => {
    if (event.key === QUOTA_VISIBLE_KEY || event.key === null) {
      current = readQuotaVisible()
      for (const fn of listeners) fn()
    }
  }
  window.addEventListener("storage", storage)
  return () => { listeners.delete(listener); window.removeEventListener("storage", storage) }
}
export function setQuotaVisible(on: boolean): boolean {
  current = on
  const saved = saveQuotaVisible(on)
  for (const fn of listeners) fn()
  return saved
}
