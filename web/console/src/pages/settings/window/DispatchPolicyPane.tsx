import { useEffect, useState } from "react"
import { client } from "../../../client.js"

type Policy = { base: string; local: string; source: "next" | "legacy" | "none"; version: string }

function word(en: string, zh: string): string {
  return /^zh(?:-|$)/i.test(document.documentElement.lang) ? zh : en
}

async function request(method: "GET" | "PUT", change?: { local: string; version: string }): Promise<Policy> {
  const response = await fetch(client.url("/v1/settings/dispatch-policy"), {
    method,
    headers: change ? { "Content-Type": "application/json" } : undefined,
    body: change ? JSON.stringify(change) : undefined,
    cache: "no-store",
  })
  const answer = await response.json()
  if (!response.ok) {
    if (answer?.error === "policy_changed") throw new Error(word("The rules changed elsewhere. Copy your draft before reloading.", "規則已在別處修改。重新讀取前，請先複製目前草稿。"))
    throw new Error(word("Could not read or save the dispatch rules.", "無法讀取或儲存派工規則。"))
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
      setSaid(word("Saved. The next dispatch uses these rules.", "已儲存；下一次派工會使用這些規則。"))
    }, (error: unknown) => setSaid(error instanceof Error ? error.message : String(error))).finally(() => setBusy(false))
  }
  return <>
    <div className="sw-column sw-policy">
      <h3>{word("My dispatch rules", "我的派工規則")}</h3>
      <p>{word("These additions guide work handed to another Session. They are read at each dispatch.", "這些補充規則會交給接手工作的 Session；每次派工都重新讀取。")}</p>
      {policy?.source === "legacy" && <p role="status">{word("Showing rules carried over from the retired app. Saving moves them into this version.", "目前讀取的是舊版 App 留下的規則；儲存後會搬到新版使用。")}</p>}
      <textarea aria-label={word("My dispatch rules", "我的派工規則")} value={draft} onChange={(event) => setDraft(event.currentTarget.value)} disabled={!policy || busy} rows={14} />
      <div className="sw-policy-actions">
        <button className="sw-chip" type="button" onClick={save} disabled={!policy || busy || (draft === policy.local && policy.source !== "legacy")}>{word("Save rules", "儲存規則")}</button>
        <button className="sw-chip" type="button" onClick={load} disabled={busy}>{word("Reload", "重新讀取")}</button>
      </div>
      <p role="status" aria-live="polite">{said}</p>
    </div>
    <div className="sw-column sw-policy">
      <h3>{word("Built-in rules", "內建規則")}</h3>
      <p>{word("Clawdline supplies these rules. Your additions above are kept separately.", "Clawdline 提供這份基礎規則；上方的補充規則會另外保存。")}</p>
      <details><summary>{word("Read built-in rules", "查看內建規則")}</summary><pre>{policy?.base ?? ""}</pre></details>
    </div>
  </>
}
