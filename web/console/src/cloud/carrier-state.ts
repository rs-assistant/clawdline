/*
 * Which road this page's reads of one machine are taking, said as one dot.
 *
 * The direct carrier is **one data channel per page and machine**
 * (`direct-carrier.ts`), shared by that machine's terminal and by every Session
 * read of it. So it is a fact about a machine, never about a Session: two
 * Sessions on the same machine cannot take different roads, and two machines
 * routinely do. That is the whole reason this is drawn on the machine's own row
 * and nowhere near a Session's.
 *
 * What a filled dot does not promise: sends, dispatches and the content-free
 * status rows stay on the relay on purpose (`docs/cloud-terminal-wire.md`,
 * Session reads on the carrier). The words name reads, and only reads.
 */

/**
 * What a machine's dot says. `unknown` is drawn as neither, because it is neither;
 * `offline` is a machine that has stopped reporting, whichever road it used to take.
 */
export type CarrierShow = "direct" | "relay" | "unknown" | "offline"

export interface CarrierFacts {
  /** Whether this browser has read that machine's own descriptor at all. */
  known: boolean
  /** Whether that descriptor says the machine opens carriers (`session_carrier_v1`). */
  supported: boolean
  /** Whether a carrier this page already holds to it is open now. */
  open: boolean
}

export interface CarrierReading {
  show: CarrierShow
  /** The word key the dot's tip uses, so the reason is said and not guessed. */
  word: "cloudCarrierDirect" | "cloudCarrierRelayMachineOff" | "cloudCarrierRelayNotOpen" | "cloudCarrierUnknown"
    | "cloudCarrierOffline" | "cloudCarrierNoReport"
}

/**
 * An open carrier is the only thing that makes reads direct, so it is read
 * first: a machine whose descriptor has not arrived yet can still have the
 * channel this page opened before the descriptor did.
 */
export function carrierReading(facts: CarrierFacts): CarrierReading {
  if (facts.open) return { show: "direct", word: "cloudCarrierDirect" }
  if (!facts.known) return { show: "unknown", word: "cloudCarrierUnknown" }
  if (!facts.supported) return { show: "relay", word: "cloudCarrierRelayMachineOff" }
  return { show: "relay", word: "cloudCarrierRelayNotOpen" }
}

/**
 * The dot a machine's row draws: whether it reports in first, its road second.
 *
 * The road alone made a closed MacBook look like a running Linux host: the
 * descriptor it sent before it went quiet still says "relay", and relay was
 * drawn green. `presence` is the roster's own freshness (`_machineRows`, the
 * newest signed envelope this browser opened from that machine), the same
 * fact the machine list's "Online" and every send's admission read, so the
 * dot cannot say a machine is there while a send to it is refused as stale.
 */
export function machineLight(
  presence: "current" | "stale" | "unknown",
  carrier: CarrierReading | null,
): CarrierReading | null {
  if (presence === "stale") return { show: "offline", word: "cloudCarrierOffline" }
  if (presence === "unknown") return { show: "unknown", word: "cloudCarrierNoReport" }
  return carrier
}
