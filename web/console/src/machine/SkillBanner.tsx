import { useEffect, useState } from "react"
import { client, followsRelay } from "../client.js"
import { nextWord } from "../next-strings.js"
import { SKILL_BANNER_DISMISSED_KEY, skillOffer, skillResult, type SkillOffer, type SkillResult } from "./skill-model.js"
import "./update.css"

function dismissedSignature(): string | null {
  try {
    return localStorage.getItem(SKILL_BANNER_DISMISSED_KEY)
  } catch {
    return null
  }
}

async function call(method: string, path: string): Promise<{ status: number; parsed: unknown } | null> {
  try {
    // A POST is spelled as `update.ts` spells its press: a JSON body, so the
    // gate reads it as the same kind of write.
    const init: RequestInit = method === "POST"
      ? { method, credentials: "same-origin", headers: { "Content-Type": "application/json" }, body: "{}" }
      : { method, credentials: "same-origin" }
    const res = await fetch(client.url(path), init)
    const text = await res.text()
    try {
      return { status: res.status, parsed: text ? JSON.parse(text) : null }
    } catch {
      return { status: res.status, parsed: null }
    }
  } catch {
    return null
  }
}

/**
 * One line under the update strip: this machine's assistants do not have the
 * clawdline skill, or have an older one, with the press that installs it
 * (`GET /v1/skill`, `POST /v1/skill/install`).
 *
 * Without the skill an assistant has never heard of a Note, a dispatch or a
 * delivery receipt, and nothing told the person so. The files are theirs
 * (DG-10), so the strip offers and the press decides. It is not drawn through
 * Cloud: the daemon answers only this machine's own token, and a phone is the
 * wrong screen to change a computer's assistant configuration from.
 */
export function SkillBanner() {
  const [offer, setOffer] = useState<SkillOffer>({ kind: "none" })
  const [sending, setSending] = useState(false)
  const [result, setResult] = useState<SkillResult | null>(null)
  const [closed, setClosed] = useState(false)

  useEffect(() => {
    if (followsRelay()) return
    let live = true
    void call("GET", "/v1/skill").then((answer) => {
      if (live && answer?.status === 200) setOffer(skillOffer(answer.parsed, dismissedSignature()))
    })
    return () => { live = false }
  }, [])

  if (closed || (offer.kind === "none" && !result)) return null

  const press = async () => {
    if (sending) return
    setSending(true)
    const answer = await call("POST", "/v1/skill/install")
    setSending(false)
    setResult(answer ? skillResult(answer.status, answer.parsed) : "failed")
  }
  const dismiss = () => {
    if (offer.kind !== "none" && !result) {
      try { localStorage.setItem(SKILL_BANNER_DISMISSED_KEY, offer.signature) } catch { /* current page only */ }
    }
    setClosed(true)
  }
  const message = sending ? nextWord("skillInstalling")
    : result === "installed" ? nextWord("skillInstalled")
    : result === "partial" ? nextWord("skillInstallPartial")
    : result === "failed" ? nextWord("skillInstallFailed")
    : null

  return (
    <div className={"update-strip" + (message ? " is-saying" : "")} id="skill-banner" role="status" aria-busy={sending}>
      <span className="update-strip-say" aria-live={message ? "polite" : undefined}>
        {message ? <>
          {sending ? <span className="update-banner-spinner" aria-hidden="true" /> : null}
          {message}
        </> : nextWord(offer.kind === "update" ? "skillBannerStale" : "skillBannerMissing")}
      </span>
      {!message && offer.kind !== "none" ? (
        <button className="update-strip-link is-go" type="button" onClick={() => void press()}>
          {nextWord(offer.kind === "update" ? "skillUpdate" : "skillInstall")}
        </button>
      ) : null}
      {!sending ? (
        <button className="update-strip-x" type="button" aria-label={nextWord("updateBannerDismiss")}
          title={nextWord("updateBannerDismiss")} onClick={dismiss}>×</button>
      ) : null}
    </div>
  )
}
