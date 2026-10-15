import type { SessionsSnapshot, SessionRow } from "@clawdline/contract"

/**
 * The snapshot a list holds after `next` arrives, or null when `next` is older
 * than `held` and changes nothing.
 *
 * Generation only orders readings from the same process, so epoch is checked
 * first: after a restart the counter starts over, and a client that compared
 * generations alone would refuse every reading from the new daemon.
 *
 * A reading proves the absence only of what it could have seen. A row it
 * leaves out is kept, marked unverified, when the reading is partial and the
 * row's own source did not answer — or when the row was observed after the
 * reading was taken. The second case is a Cloud page: its reader numbers its
 * own readings, so a held machine pass from before a Session started could
 * arrive with a newer generation than the pass that listed it, and, being
 * complete, take a Session the person was typing into off the list.
 */
export function acceptSnapshot(held: SessionsSnapshot | null, next: SessionsSnapshot): SessionsSnapshot | null {
  if (!held || held.scan.epoch !== next.scan.epoch) return next
  if (next.scan.generation < held.scan.generation) return null
  const present = new Set(next.sessions.map((row) => row.id))
  const completeSources = next.scan.complete ? null
    : new Set(next.scan.sources?.filter((source) => source.complete).map((source) => source.source) ?? [])
  const retained = held.sessions.filter((row) => !present.has(row.id) && (completeSources
    ? !completeSources.has(row.source?.provenance ?? "")
    : seenAfter(row, next)))
  if (!retained.length) return next
  return { ...next, sessions: [...next.sessions, ...retained.map((row) => unverified(row, held))] }
}

function seenAfter(row: SessionRow, reading: SessionsSnapshot): boolean {
  const observed = row.source?.observed_at
  return typeof observed === "number" && Number.isFinite(reading.at) && observed > reading.at
}

function unverified(row: SessionRow, held: SessionsSnapshot): SessionRow {
  const reasons = row.closeability.reasons.some((reason) => reason.code === "session_inventory_stale")
    ? row.closeability.reasons
    : [...row.closeability.reasons,
        { code: "session_inventory_stale", kind: "evidence", mover: { kind: "broker", person_needed: false, self: false } }]
  return { ...row,
    source: { freshness: "unverified" as const, observed_at: row.source?.observed_at ?? held.at,
      provenance: row.source?.provenance ?? held.scan.provenance },
    closeability: { ...row.closeability, state: "unknown" as const, attestation_id: null, reasons },
  } as SessionRow
}
