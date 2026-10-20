import { classifyViewerError } from "../legacy/js/net/viewer-health-hook.js"

/**
 * The connection-health error word for a carrier that could not open or stopped carrying
 * (docs/admin-connection-health-contract.md §11.2). The link's own codes are named here; anything
 * else goes through the relay's table, so a word is never guessed.
 */
export function directCarrierHealthError(code: string): string {
  switch (code) {
    // ICE found no path between the two networks within the negotiation bound.
    case "terminal_direct_ice_failed": return "network_unreachable"
    case "terminal_direct_chunk_timeout": return "timeout"
    case "terminal_direct_closed": case "carrier_not_open": case "carrier_failed": return "carrier_closed"
    // The machine's sealed answer, or an envelope it delivered, would not open here.
    case "carrier_bad_answer": case "carrier_unusable": return "decrypt_failed"
    default: return classifyViewerError({ code }, "read")
  }
}

/**
 * Closes a terminal makes on purpose (`cloud/terminal-transport.ts`): replaced by its own next
 * negotiation, released when the terminal lets go, or closed by a negotiation that already
 * recorded its own failure. None of them is a carrier that stopped carrying.
 */
export const TERMINAL_DELIBERATE_CLOSE: ReadonlySet<string> = new Set([
  "terminal_direct_replaced", "terminal_direct_released", "terminal_direct_failed",
])
