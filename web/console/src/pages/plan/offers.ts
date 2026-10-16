// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { catalogFormat, catalogWord } from "../../catalog.ts"
import { T, fill } from "../../legacy/js/core/i18n.js"

/**
 * The two plans on sale, drawn as cards.
 *
 * Only Free and Pro are offered; Max and Team stay in the Cloud tier table and are
 * not advertised. Viewer devices are not a line on either card: both plans allow
 * the same number, an abuse bound rather than something Pro sells, and the fine
 * print says it once. These are the same limits the pricing page prints and the Cloud
 * API enforces at pairing. The current tier's own numbers are taken from
 * `/v1/entitlements` when the account has answered, so the card the person is on
 * never disagrees with the server; the other card is the listing.
 */
export type OfferTier = "free" | "pro"

export interface Offer {
  machines: number
  webhooks: boolean
}

export const OFFERS: Record<OfferTier, Offer> = {
  free: { machines: 1, webhooks: false },
  pro: { machines: 5, webhooks: true },
}

export interface Entitlements {
  max_machines?: number | null
  schedule_webhooks?: boolean
}

/** The account's answer laid over the listing, for the tier it is on. */
export function offerFor(tier: OfferTier, current: OfferTier | null, entitlements?: Entitlements | null): Offer {
  const listed = OFFERS[tier]
  if (tier !== current || !entitlements) return listed
  return {
    machines: Number.isSafeInteger(entitlements.max_machines) ? entitlements.max_machines! : listed.machines,
    webhooks: typeof entitlements.schedule_webhooks === "boolean" ? entitlements.schedule_webhooks : listed.webhooks,
  }
}

/** The lines of one card, in the pricing page's order. */
export function offerFeatures(offer: Offer): string[] {
  const lines = [
    offer.machines === 1
      ? catalogFormat("plan", "machinesOne", [offer.machines])
      : catalogFormat("plan", "machinesMany", [offer.machines]),
    catalogWord("plan", "control"),
    catalogWord("plan", "push"),
  ]
  if (offer.webhooks) lines.push(catalogWord("plan", "webhooks"))
  return lines
}

/**
 * Which card is the account's. Null while nothing has been read, when signed out,
 * on the machine-served copy, and when the plan could not be read: no card claims
 * to be current unless the server said so.
 */
export function currentOffer(state: string, tier: string | undefined): OfferTier | null {
  if (["idle", "loading", "signed_out", "not_here", "unavailable", "waiting"].includes(state)) return null
  if (!tier) return null
  return tier === "free" ? "free" : "pro"
}

/** The module's own spelling of a tier, so its sentence can be recognized. */
function tierLabel(tier: string | undefined): string {
  const raw = String(tier || "free")
  return raw.charAt(0).toUpperCase() + raw.slice(1)
}

function text(doc: Document, id: string, words: string): void {
  const node = doc.getElementById(id)
  if (node && node.textContent !== words) node.textContent = words
}

function show(doc: Document, id: string, on: boolean): void {
  const node = doc.getElementById(id)
  if (node && node.hidden === on) node.hidden = !on
}

function list(doc: Document, id: string, lines: string[]): void {
  const node = doc.getElementById(id)
  if (!node) return
  const now = [...node.children].map((child) => child.textContent).join("\n")
  if (now === lines.join("\n")) return
  node.replaceChildren(...lines.map((line) => {
    const item = doc.createElement("li")
    item.textContent = line
    return item
  }))
}

/** Paint both cards for the module's current state. Idempotent: an unchanged card is not touched. */
export function paintOffers(doc: Document, state: string, plan: { tier?: string; entitlements?: Entitlements } | null): void {
  const current = currentOffer(state, plan?.tier)
  text(doc, "plan-title", catalogWord("plan", "heading"))
  text(doc, "plan-lede", catalogWord("plan", "lede"))
  text(doc, "plan-offer-pro-tag", catalogWord("plan", current === "pro" ? "current" : "recommended"))
  text(doc, "plan-offer-pro-active", catalogWord("plan", "active"))
  text(doc, "plan-offer-pro-per", catalogWord("plan", "perMonth"))
  text(doc, "plan-offer-pro-pitch", catalogWord("plan", current === "pro" ? "proBilling" : "proPitch"))
  text(doc, "plan-offer-free-current", catalogWord("plan", "current"))
  text(doc, "plan-offer-free-pitch", catalogWord("plan", "freePitch"))
  text(doc, "plan-offer-fine", catalogWord("plan", "fine"))
  show(doc, "plan-offer-pro-active", current === "pro")
  const say = doc.getElementById("plan-say")
  if (say) {
    const repeats = (current === "pro" && say.textContent === fill(T.webPlanActive, { tier: tierLabel(plan?.tier) }))
      || (current === "free" && say.textContent === T.webPlanOnFree)
    if (say.dataset.repeatsPill !== String(repeats)) say.dataset.repeatsPill = String(repeats)
  }
  show(doc, "plan-offer-free-current", current === "free")
  for (const tier of ["free", "pro"] as const) {
    const card = doc.getElementById(`plan-offer-${tier}`)
    if (card) card.dataset.current = String(current === tier)
    list(doc, `plan-offer-${tier}-features`, offerFeatures(offerFor(tier, current, plan?.entitlements)))
  }
}
