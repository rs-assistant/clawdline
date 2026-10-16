import type { UpdateApplyState, UpdateState } from "@clawdline/contract"
import type { CloudMachine } from "./copied.js"
import type { CloudReadClient } from "./relay-reader.js"
// @ts-expect-error -- Node runs source tests with type stripping.
import { buildName, cachedUpdateAfterFreshFailure, classifyUpdateRead, type UpdateRead } from "../machine/update-model.ts"

/**
 * The fleet picker reads each paired, online machine by its own encrypted
 * "update" word. The chosen-machine fetch seam cannot answer for another row.
 * A refused or unanswered read stays distinct from an older daemon.
 */
export async function readFleetUpdate(client: CloudReadClient, machine: string, signal?: AbortSignal, fresh = false): Promise<UpdateRead> {
  const request = client._machineRequest
  if (typeof request !== "function") return { kind: "refused" }
  const read = async (word: "update" | "update-fresh"): Promise<UpdateRead> => {
    try {
      const parsed = await request.call(client, machine, word, {}, "read", undefined, signal ? { signal } : undefined)
      return classifyUpdateRead({ transport: "answered", status: 200, parsed })
    } catch (error) {
      const failure = error as { code?: unknown; status?: unknown }
      const code = typeof failure?.code === "string" ? failure.code : ""
      const status = typeof failure?.status === "number" && failure.status >= 400 && failure.status < 600
        ? failure.status : 502
      return classifyUpdateRead({ transport: "answered", status, parsed: { error: { code } } })
    }
  }
  const answer = await read(fresh ? "update-fresh" : "update")
  return fresh && (answer.kind === "unreachable" || answer.kind === "refused")
    ? cachedUpdateAfterFreshFailure(await read("update")) : answer
}

export interface FleetUpdateFacts {
  running: string | null
  latest: string | null
  compatibility: "supported" | "older" | "offline" | "unpaired" | "unknown"
  state: UpdateState | null
  result: UpdateApplyState | null
  from: string | null
  to: string | null
  errorCode: string | null
}

/** Missing facts stay unknown; a retained version is never evidence of an online machine. */
export function fleetUpdateFacts(
  machine: Pick<CloudMachine, "freshness" | "pairing">,
  appVersion: string | undefined,
  read: UpdateRead | null,
): FleetUpdateFacts {
  const status = read?.kind === "status" ? read.status : null
  const apply = status?.apply
  return {
    running: (status && buildName(status.running)) || appVersion || null,
    latest: status ? buildName(status.latest) || null : null,
    compatibility: machine.pairing !== "paired" ? "unpaired"
      : machine.freshness !== "current" ? "offline"
        : read?.kind === "status" ? "supported"
          : read?.kind === "older" ? "older" : "unknown",
    state: status?.state ?? null,
    result: apply?.state ?? null,
    from: apply?.from ?? null,
    to: apply?.to ?? null,
    errorCode: apply?.error?.code ?? null,
  }
}
