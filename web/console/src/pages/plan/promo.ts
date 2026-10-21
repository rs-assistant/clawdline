/** A promo link carries a bearer code only in the fragment. Remove it before OAuth starts. */
// @ts-expect-error -- Node's strip-types runner needs the source extension.
import { catalogWord, catalogFormat } from "../../catalog.ts"
import { failureSentence } from "../../legacy/js/core/failure-text.js"

const CODE_KEY = "clawdline.promo.code"
const INTENT_KEY = "clawdline.promo.intent"
const PRIVATE_CODE = /^clp_[A-Za-z0-9_-]{20,100}$/
const PUBLIC_CODE = /^[A-Z][A-Z0-9]*(?:-[A-Z0-9]+)*$/

export function capturePromoLink(scope: Pick<Window, "location" | "history" | "sessionStorage">): void {
  const params = new URLSearchParams(scope.location.hash.slice(1))
  const code = params.get("promo")
  if (!code) return
  params.delete("promo")
  scope.history.replaceState(scope.history.state, "", scope.location.pathname + scope.location.search + "#" + params.toString())
  const normalized = code.startsWith("clp_") ? code : code.toUpperCase()
  if (PRIVATE_CODE.test(code) || normalized.length >= 6 && normalized.length <= 48 && PUBLIC_CODE.test(normalized)) {
    scope.sessionStorage.setItem(CODE_KEY, normalized)
  }
}

export function pendingPromoCode(storage: Storage): string {
  return storage.getItem(CODE_KEY) ?? ""
}

export function savePromoCode(storage: Storage, code: string): void {
  const previous = storage.getItem(CODE_KEY)
  if (previous !== code) storage.removeItem(INTENT_KEY)
  storage.setItem(CODE_KEY, code)
}

export function clearPromoCode(storage: Storage): void {
  storage.removeItem(CODE_KEY)
  storage.removeItem(INTENT_KEY)
}

function intent(storage: Storage): string {
  let key = storage.getItem(INTENT_KEY)
  if (!key) {
    key = crypto.randomUUID()
    storage.setItem(INTENT_KEY, key)
  }
  return key
}

export type PromoAnswer =
  | { status: "granted"; grant: { expires_at: string }; replay: boolean }
  | { status: "rejected"; reason: string }
  | { status: "conflict"; reason: string }

function promoError(code: string): Error {
  return Object.assign(new Error(code), { code })
}

export async function redeemPromo(apiOrigin: string, code: string, storage: Storage,
  fetchImpl: typeof fetch = fetch): Promise<PromoAnswer> {
  const response = await fetchImpl(apiOrigin + "/v1/promo/redemptions", {
    method: "POST", credentials: "include", cache: "no-store",
    headers: { "content-type": "application/json", "Idempotency-Key": intent(storage) },
    body: JSON.stringify({ code }),
  })
  const body = await response.json().catch(() => null) as PromoAnswer & { error?: { code?: string } } | null
  if (response.status === 401 || response.status === 403) throw promoError("signed_out")
  if (response.status === 429) throw promoError("rate_limited")
  if (body?.status === "rejected" || body?.status === "conflict") return body
  if (response.ok && body?.status === "granted" && body.grant?.expires_at) return body
  throw promoError(body?.error?.code ?? "unavailable")
}

const promoEn = {
  "title": "Redeem a Cloud Pro offer",
  "label": "Official redemption code",
  "action": "Redeem",
  "working": "Checking your code…",
  "success": "Pro is active until {arg0}.",
  "rejected": "This code cannot be redeemed. Check the code and campaign dates.",
  "signedOut": "Sign in, then return here to redeem.",
  "rateLimited": "Too many attempts. Try again later.",
  "conflict": "This account already has a paid plan or Pro grant, so it cannot redeem this code.",
  "unavailable": "The redemption result could not be confirmed. Try again later.",
  "elsewhere": "Redeem this code in Clawdline Cloud."
}
const promoZh = {
  "title": "兌換 Cloud Pro 禮遇",
  "label": "官方兌換碼",
  "action": "兌換",
  "working": "正在確認兌換碼…",
  "success": "Pro 已啟用，有效至 {arg0}。",
  "rejected": "此兌換碼無法使用；請確認代碼與活動期限。",
  "signedOut": "請先登入，再回到這裡兌換。",
  "rateLimited": "嘗試次數過多，請稍後再試。",
  "conflict": "這個帳號目前已有付費方案或 Pro 贈期，無法再兌換。",
  "unavailable": "暫時無法確認兌換結果，請稍後重試。",
  "elsewhere": "請在 Clawdline Cloud 兌換此代碼。"
}
export const promoSourceWords = { en: promoEn, "zh-Hant": promoZh }

export function promoWords() {
  return {
    title: catalogWord("promo", "title"),
    label: catalogWord("promo", "label"),
    action: catalogWord("promo", "action"),
    working: catalogWord("promo", "working"),
    success:  (date: string) => catalogFormat("promo", "success", [date]),
    rejected: catalogWord("promo", "rejected"),
    signedOut: catalogWord("promo", "signedOut"),
    rateLimited: catalogWord("promo", "rateLimited"),
    conflict: catalogWord("promo", "conflict"),
    unavailable: catalogWord("promo", "unavailable"),
    elsewhere: catalogWord("promo", "elsewhere"),
  }
}

export function bindPromoForm(doc: Document, apiOrigin: string | null, onGranted: () => Promise<void>): () => void {
  const form = doc.getElementById("plan-promo-form") as HTMLFormElement | null
  const input = doc.getElementById("plan-promo-code") as HTMLInputElement | null
  const message = doc.getElementById("plan-promo-message")
  const button = doc.getElementById("plan-promo-submit") as HTMLButtonElement | null
  const link = doc.getElementById("plan-promo-cloud") as HTMLAnchorElement | null
  if (!form || !input || !message || !button || !link) return () => {}
  const words = promoWords()
  const title = doc.getElementById("plan-promo-title")
  const label = doc.getElementById("plan-promo-label")
  if (title) title.textContent = words.title
  if (label) label.textContent = words.label
  button.textContent = words.action
  input.value = pendingPromoCode(sessionStorage)
  link.hidden = !!apiOrigin
  const updateCloudLink = () => {
    link.href = "https://app.clawdline.com/#page=plan" +
      (input.value.trim() ? "&promo=" + encodeURIComponent(input.value.trim()) : "")
  }
  if (!apiOrigin) {
    message.textContent = words.elsewhere
    button.hidden = true
    updateCloudLink()
    input.addEventListener("input", updateCloudLink)
  }
  const submit = async (event: Event) => {
    event.preventDefault()
    if (!apiOrigin || button.disabled) return
    const code = input.value.trim()
    savePromoCode(sessionStorage, code)
    button.disabled = true
    message.textContent = words.working
    try {
      const answer = await redeemPromo(apiOrigin, code, sessionStorage)
      if (answer.status !== "granted") {
        message.textContent = answer.status === "conflict" ? words.conflict : words.rejected
      } else {
        clearPromoCode(sessionStorage)
        input.value = ""
        await onGranted()
        message.textContent = words.success(answer.grant.expires_at.slice(0, 10))
      }
    } catch (error) {
      const code = error instanceof Error ? error.message : "unavailable"
      if (code === "signed_out") message.textContent = words.signedOut
      else if (code === "rate_limited") message.textContent = words.rateLimited
      else message.textContent = failureSentence(error, { fallback: words.unavailable })
    } finally { button.disabled = false }
  }
  form.addEventListener("submit", submit)
  return () => {
    form.removeEventListener("submit", submit)
    input.removeEventListener("input", updateCloudLink)
  }
}
