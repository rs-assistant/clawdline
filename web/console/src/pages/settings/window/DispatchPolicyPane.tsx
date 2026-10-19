import { catalogWord } from "../../../catalog.js"
import { useEffect, useState } from "react"
import { client } from "../../../client.js"

const dispatchPolicyEn = {
  "navTitle": "Dispatch rules",
  "changed": "The rules changed elsewhere. Copy your draft before reloading.",
  "failed": "Could not read or save the dispatch rules.",
  "saved": "Saved. The next dispatch uses these rules.",
  "title": "My dispatch rules",
  "intro": "These additions guide work handed to another Session. They are read at each dispatch.",
  "legacy": "Showing rules carried over from the retired app. Saving moves them into this version.",
  "save": "Save rules",
  "reload": "Reload",
  "baseTitle": "Built-in rules",
  "baseIntro": "Clawdline supplies these rules. Your additions above are kept separately.",
  "readBase": "Read built-in rules"
} as const

const dispatchPolicyZh: Record<keyof typeof dispatchPolicyEn, string> = {
  "navTitle": "派工規則",
  "changed": "規則已在別處修改。重新讀取前，請先複製目前草稿。",
  "failed": "無法讀取或儲存派工規則。",
  "saved": "已儲存；下一次派工會使用這些規則。",
  "title": "我的派工規則",
  "intro": "這些補充規則會交給接手工作的 Session；每次派工都重新讀取。",
  "legacy": "目前讀取的是舊版 App 留下的規則；儲存後會搬到新版使用。",
  "save": "儲存規則",
  "reload": "重新讀取",
  "baseTitle": "內建規則",
  "baseIntro": "Clawdline 提供這份基礎規則；上方的補充規則會另外保存。",
  "readBase": "查看內建規則"
}

export const dispatchPolicyWords = { en: dispatchPolicyEn, "zh-Hant": dispatchPolicyZh }

function word(key: keyof typeof dispatchPolicyEn): string {
  return catalogWord("dispatchPolicy", key)
}

type Policy = { base: string; local: string; source: "next" | "legacy" | "none"; version: string }

async function request(method: "GET" | "PUT", change?: { local: string; version: string }): Promise<Policy> {
  const response = await fetch(client.url("/v1/settings/dispatch-policy"), {
    method,
    headers: change ? { "Content-Type": "application/json" } : undefined,
    body: change ? JSON.stringify(change) : undefined,
    cache: "no-store",
  })
  const answer = await response.json()
  if (!response.ok) {
    if (answer?.error === "policy_changed") throw new Error(word("changed"))
    throw new Error(word("failed"))
  }
  return answer as Policy
}

/** The shipped dispatch rules are read-only; only this person's local additions are saved. */
export function DispatchPolicyPane() {
  const [policy, setPolicy] = useState<Policy | null>(null)
  const [draft, setDraft] = useState("")
  const [busy, setBusy] = useState(false)
  const [said, setSaid] = useState("")
  const load = () => {
    setBusy(true)
    void request("GET").then((answer) => {
      setPolicy(answer)
      setDraft(answer.local)
      setSaid("")
    }, (error: unknown) => setSaid(error instanceof Error ? error.message : String(error))).finally(() => setBusy(false))
  }
  useEffect(load, [])
  const save = () => {
    if (!policy || busy || (draft === policy.local && policy.source !== "legacy")) return
    setBusy(true)
    setSaid("")
    void request("PUT", { local: draft, version: policy.version }).then((answer) => {
      setPolicy(answer)
      setDraft(answer.local)
      setSaid(word("saved"))
    }, (error: unknown) => setSaid(error instanceof Error ? error.message : String(error))).finally(() => setBusy(false))
  }
  return <>
    <div className="sw-column sw-policy">
      <h3>{word("title")}</h3>
      <p>{word("intro")}</p>
      {policy?.source === "legacy" && <p role="status">{word("legacy")}</p>}
      <textarea aria-label={word("title")} value={draft} onChange={(event) => setDraft(event.currentTarget.value)} disabled={!policy || busy} rows={14} />
      <div className="sw-policy-actions">
        <button className="sw-chip" type="button" onClick={save} disabled={!policy || busy || (draft === policy.local && policy.source !== "legacy")}>{word("save")}</button>
        <button className="sw-chip" type="button" onClick={load} disabled={busy}>{word("reload")}</button>
      </div>
      <p role="status" aria-live="polite">{said}</p>
    </div>
    <div className="sw-column sw-policy">
      <h3>{word("baseTitle")}</h3>
      <p>{word("baseIntro")}</p>
      <details><summary>{word("readBase")}</summary><pre>{policy?.base ?? ""}</pre></details>
    </div>
  </>
}
