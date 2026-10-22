/**
 * The answer of a reading this caller may not have: the Remote tab's route
 * (`/v1/cloud/status`) serves only this machine's own token
 * (`requireLocal`, 403 `forbidden`), and this is a browser that `clawdline open`
 * or a pairing signed in. That is not a failure to read — the daemon answered —
 * so the card says where Cloud is managed instead of "Could not read this
 * machine's status", which on a fresh install read as a broken machine.
 */
export const MACHINE_ONLY = "machine_only" as const
export type MachineOnly = typeof MACHINE_ONLY

/** What a status response says: its body, MACHINE_ONLY, or null for anything else. */
export async function remoteReading<T>(res: Response): Promise<T | MachineOnly | null> {
  if (res.status === 403) return MACHINE_ONLY
  if (!res.ok) return null
  return (await res.json()) as T
}
