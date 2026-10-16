// The Plan page's way into its copied module.
//
// `js/view/plan.js` is the Swift app's, byte for byte, and so is the one module
// it imports, `js/net/billing.js` (for the simulation panel's words). It is
// bound here as that app's `main.js` binds it for the two environments:
//
// - A hosted, signed-in browser uses its Cloud account's API origin for plan
//   reads and the existing checkout/portal controls.
// - A page served by a machine has no Cloud account and keeps the copied
//   module's `not_here` state.
// - `consoleOrigin` is the hosted console's, as the original's is when it has no
//   Cloud configuration.
// - `navigate` is left to the module's default for checkout and portal URLs.
// - The two plan cards (`pages/plan/offers.ts`) are painted from the module's state.
import { bindPlanPage as bindPlanPageOriginal } from "./js/view/plan.js"
import { createBillingClient } from "./js/net/billing.js"
// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { paintOffers, type Entitlements } from "../pages/plan/offers.ts"

/** What `bindPlanPage` hands back. */
export interface PlanPage {
  enter(options?: { returning?: boolean }): Promise<void>
  leave(): void
  state(): string
  plan(): unknown
}

/** `main.js`'s element table for `bindPlanPage`, in its order. */
export const PLAN_ELEMENT_IDS = [
  "plan",
  "plan-title",
  "plan-lede",
  "plan-close",
  "plan-includes",
  "plan-tier",
  "plan-tier-note",
  "plan-say",
  "plan-alert",
  "plan-limits",
  "plan-fine",
  "plan-upgrade",
  "plan-portal",
  "plan-signin",
  "plan-retry",
  "plan-recheck",
  "plan-elsewhere",
  "plan-console",
  "plan-simulation",
  "plan-simulation-title",
  "plan-simulation-values",
  "plan-simulation-actual",
  "plan-simulation-free",
  "plan-simulation-pro",
] as const

/** `main.js`'s default when there is no Cloud configuration. */
export const PLAN_CONSOLE_ORIGIN = "https://app.clawdline.com"

/** Bind the page once its markup is in the document, with the account if there is one. */
export function bindPlan(doc: Document, apiOrigin: string | null = null): PlanPage {
  const elements: Record<string, HTMLElement | null> = {}
  for (const id of PLAN_ELEMENT_IDS) elements[id] = doc.getElementById(id)
  const billing = apiOrigin ? createBillingClient({ apiOrigin }) : null
  const original = (bindPlanPageOriginal as (elements: Record<string, HTMLElement | null>, seams: Record<string, unknown>) => PlanPage)(
    elements,
    {
      billing,
      signInURL: () => apiOrigin
        ? `${apiOrigin}/v1/auth/oauth/start?return_to=${encodeURIComponent(location.origin + "/#page=plan")}`
        : "",
      consoleOrigin: PLAN_CONSOLE_ORIGIN,
    },
  )
  // The module has no change event; every state it enters rewrites the calm line, the
  // alert or the tier, so a change to any of them is when the cards are repainted.
  const watched = ["plan-say", "plan-alert", "plan-tier"].map((id) => elements[id]).filter(Boolean) as HTMLElement[]
  const paint = () => paintOffers(doc, original.state(),
    original.plan() as { tier?: string; entitlements?: Entitlements } | null)
  const observer = watched.length && typeof MutationObserver !== "undefined"
    ? new MutationObserver(paint) : null
  return {
    ...original,
    async enter(options) {
      for (const node of watched) {
        observer?.observe(node, { childList: true, characterData: true, subtree: true, attributes: true, attributeFilter: ["hidden"] })
      }
      const arrived = original.enter(options)
      paint()
      await arrived
      paint()
    },
    leave() {
      observer?.disconnect()
      original.leave()
    },
  }
}
