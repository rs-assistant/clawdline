import { useEffect, useState, useSyncExternalStore } from "react"
import type { SettingsRequest, SettingsSnapshot } from "@clawdline/contract"
import { readNotificationSettings, writeNotificationSettings } from "./settings/api.js"
import { settingsFailureSentence } from "./settings/window/failure.js"
import { pushShape, sendPushTest, startPush, subscribePush, togglePush } from "../push/push.js"
import { legacyState } from "../legacy/overlay-bridge.js"
import { client } from "../client.js"
import type { PageModule } from "./types.js"
import "./notifications/notifications.css"

type Preference = keyof Pick<SettingsRequest,
  "push_on_waiting" | "orchestrator_agent_notify" | "push_on_board_decision" |
  "push_on_board_done" | "push_on_schedule_failure" | "push_on_capacity" |
  "push_on_dead_letter" | "push_on_app_release_success" | "push_on_cloud_release_success" | "push_on_release_failure">

type Row = { key: Preference; en: string; zh: string; destination: "Session" | "list" | "optional-session"; hintEn: string; hintZh: string }

type CustomRule = { id: string; enabled: boolean; after_minutes: number; label_contains: string }
type NotificationSettings = { rules: CustomRule[]; preferences: Partial<SettingsSnapshot>; preview?: CustomRule }

type RuleChange = { action: "create" | "update"; rule: CustomRule } | { action: "delete"; id: string } | { action: "preview"; text: string }

async function notificationRules(change?: RuleChange): Promise<NotificationSettings> {
  const response = await fetch(client.url("/v1/notification-rules"), change ? {
    method: "POST", headers: { "Content-Type": "application/json", "Idempotency-Key": crypto.randomUUID() }, body: JSON.stringify(change),
  } : undefined)
  const answer = await response.json() as NotificationSettings & { message?: string; error?: { message?: string; detail?: string } }
  if (!response.ok) throw new Error(response.status === 422 ? word("This situation is not supported yet. Try one of the Session examples below.", "目前還無法依這個情境推播。請試試下方的 Session 範例。") : answer.error?.message || answer.error?.detail || answer.message || word("Could not save notification settings.", "無法儲存通知設定。"))
  return { rules: answer.rules ?? [], preferences: answer.preferences ?? {}, preview: answer.preview }
}

function ruleSentence(rule: CustomRule): string {
  const subject = rule.label_contains ? word(`a Session named "${rule.label_contains}"`, `名稱包含「${rule.label_contains}」的 Session`) : word("a Session", "Session")
  return rule.after_minutes === 0
    ? word(`Notify me when ${subject} needs my answer.`, `當${subject}等我回答時通知我。`)
    : word(`Notify me when ${subject} needs my answer for ${rule.after_minutes} minutes.`, `當${subject}等我回答超過 ${rule.after_minutes} 分鐘時通知我。`)
}

const rows: Row[] = [
  { key: "push_on_waiting", en: "A Session needs my answer", zh: "Session 等我回答或授權", destination: "Session", hintEn: "Once per unanswered stop, after ten minutes.", hintZh: "等待十分鐘後，每次停住最多通知一次。" },
  { key: "orchestrator_agent_notify", en: "An agent sends me a notification", zh: "Agent 主動通知我", destination: "optional-session", hintEn: "Includes unsolicited notes; subject to the agent notification limit.", hintZh: "包含主動建立的待辦 Note，並受 Agent 通知次數限制。" },
  { key: "push_on_board_decision", en: "A Board decision needs an answer", zh: "Board 決策等我回答", destination: "list", hintEn: "Once per pending decision.", hintZh: "每個待回答決策最多通知一次。" },
  { key: "push_on_board_done", en: "A Board item is done", zh: "Board 項目完成", destination: "Session", hintEn: "Opens its assigned Session while that Session exists.", hintZh: "若指派的 Session 還在，就開啟它。" },
  { key: "push_on_schedule_failure", en: "A schedule fails or cannot run", zh: "排程失敗或無法執行", destination: "list", hintEn: "Missed or refused runs also require that schedule's failure switch; unreadable files notify once per change.", hintZh: "錯過或拒絕執行還需開啟該排程的「失敗時通知」；檔案無法讀取時每次變更最多通知一次。" },
  { key: "push_on_capacity", en: "A capacity limit is reached", zh: "系統容量達到警示值", destination: "list", hintEn: "The limit and its current reading remain in diagnostics.", hintZh: "即使不推播，診斷頁仍保留警示與讀數。" },
  { key: "push_on_dead_letter", en: "A task completion cannot reach its Session", zh: "任務完成訊息無法送達 Session", destination: "Session", hintEn: "Opens its root Session when it still exists.", hintZh: "若原本的 Root Session 還在，就開啟它。" },
  { key: "push_on_app_release_success", en: "An App release succeeds", zh: "App 新版本發布成功", destination: "list", hintEn: "After the running App passes its version check.", hintZh: "運行中的 App 通過版本驗收後通知。" },
  { key: "push_on_cloud_release_success", en: "A Cloud release succeeds", zh: "Cloud 新版本發布成功", destination: "list", hintEn: "After the production site passes its checks.", hintZh: "正式網站通過發布驗收後通知。" },
  { key: "push_on_release_failure", en: "An App or Cloud release fails", zh: "App 或 Cloud 發布失敗", destination: "list", hintEn: "When app installation or Cloud production verification fails.", hintZh: "App 安裝或 Cloud 正式環境驗收失敗時通知。" },
]

function word(en: string, zh: string): string {
  return /^zh(?:-|$)/i.test(document.documentElement.lang) ? zh : en
}

function current(snapshot: Partial<SettingsSnapshot>, key: Preference): boolean {
  const value = snapshot[key]
  if (typeof value === "boolean") return value
  if (key === "push_on_waiting") return snapshot.orchestrator_agent_notify ?? true
  if (key === "push_on_app_release_success" || key === "push_on_cloud_release_success" || key === "push_on_release_failure") return snapshot.push_on_deploy ?? false
  return true
}

function NotificationsPage({ shown }: { shown: boolean }) {
  const push = useSyncExternalStore(subscribePush, pushShape)
  const [settings, setSettings] = useState<Partial<SettingsSnapshot> | null>(null)
  const [pending, setPending] = useState<Preference | null>(null)
  const [error, setError] = useState("")
  const [customRules, setCustomRules] = useState<CustomRule[]>([])
  const [ruleBusy, setRuleBusy] = useState(false)
  const [ruleError, setRuleError] = useState("")
  const [ruleText, setRuleText] = useState("")
  const [preview, setPreview] = useState<CustomRule | null>(null)
  const [previewText, setPreviewText] = useState("")

  useEffect(() => {
    if (!shown) return
    startPush()
    let live = true
    void notificationRules().then((answer) => {
      if (live) { setCustomRules(answer.rules); setError("") }
    }, (cause: unknown) => { if (live) setError(cause instanceof Error ? cause.message : String(cause)) })
    void readNotificationSettings().then((answer) => {
      if (live) { setSettings(answer); setError("") }
    }, (cause: unknown) => { if (live) setError(settingsFailureSentence(cause)) })
    return () => { live = false }
  }, [shown])

  const change = async (key: Preference) => {
    if (!settings || pending || ruleBusy) return
    setPending(key)
    setError("")
    try {
      const answer = await writeNotificationSettings({ [key]: !current(settings, key) } as Partial<SettingsRequest>)
      setSettings(answer)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
    } finally {
      setPending(null)
    }
  }

  const saveRule = async (change: RuleChange): Promise<boolean> => {
    if (ruleBusy || pending) return false
    setRuleBusy(true)
    setRuleError("")
    try { const answer = await notificationRules(change); setCustomRules(answer.rules); return true }
    catch (cause) { setRuleError(cause instanceof Error ? cause.message : String(cause)); return false }
    finally { setRuleBusy(false) }
  }

  const interpret = async () => {
    const text = ruleText.trim()
    if (!text || ruleBusy || pending !== null) return
    setRuleBusy(true)
    setPreview(null)
    setRuleError("")
    try {
      const answer = await notificationRules({ action: "preview", text })
      if (!answer.preview) throw new Error(word("The situation could not be understood.", "無法理解這個情境。"))
      setPreview({ ...answer.preview, id: crypto.randomUUID().replaceAll("-", "") })
      setPreviewText(text)
    } catch (cause) {
      setRuleError(cause instanceof Error ? cause.message : String(cause))
    } finally {
      setRuleBusy(false)
    }
  }

  const addRule = async () => {
    if (!preview || previewText !== ruleText.trim()) return
    if (await saveRule({ action: "create", rule: preview })) { setRuleText(""); setPreview(null); setPreviewText("") }
  }

  return <section id="notifications" className="page notification-page" data-page-view="notifications" hidden={!shown}>
    <div className="notification-wrap">
      <h1>{word("Notifications", "通知")}</h1>
      <p>{word("Choose which events may notify your subscribed devices. These settings apply to this machine; browser permission is set separately on each device.", "逐項選擇哪些事件可以推播到已訂閱的裝置。這些設定套用到這台機器；每台裝置的瀏覽器權限需分別開啟。")}</p>
      <div className="notification-device">
        <h2>{word("This device", "這台裝置")}</h2>
        <p>{push.state === "on" ? word("Notifications are on.", "通知已開啟。") : push.state === "blocked" ? word("The browser blocks notifications for this site.", "瀏覽器已封鎖這個網站的通知。") : push.state === "homescreen" ? word("On iPhone or iPad, add this site to the Home Screen first.", "在 iPhone 或 iPad 上，請先將網站加入主畫面。") : push.state === "unsupported" ? word("This browser or address cannot receive notifications.", "這個瀏覽器或網址無法接收通知。") : word("Notifications are off on this device.", "這台裝置的通知尚未開啟。")}</p>
        {(push.state === "on" || push.state === "off") && <button type="button" disabled={push.busy} onClick={togglePush}>{push.state === "on" ? word("Turn off on this device", "關閉這台裝置的通知") : word("Turn on on this device", "開啟這台裝置的通知")}</button>}
        {push.state === "on" && <button type="button" disabled={push.testing} onClick={() => sendPushTest(legacyState.openId || legacyState.selectedId || null)}>{word("Send a test", "送一則測試")}</button>}
        <p role="status">{push.said}</p>
      </div>
      <h2>{word("When to notify me", "何時通知我")}</h2>
      {error && <p className="notification-error" role="alert">{error}</p>}
      {!settings && !error && <p>{word("Loading preferences…", "正在讀取通知設定…")}</p>}
      {settings && <div className="notification-rows">{rows.map((row) => <label className="notification-row" key={row.key}>
        <span><strong>{word(row.en, row.zh)}</strong><small>{word(row.hintEn, row.hintZh)} {row.destination === "Session" ? word("Opens the Session while it exists.", "若 Session 還在，點擊後開啟它。") : row.destination === "optional-session" ? word("Opens the named Session, or the list if none was named.", "有指定 Session 時開啟它，否則開啟清單。") : word("Opens the list.", "點擊後開啟清單。")}</small></span>
        <input type="checkbox" checked={current(settings, row.key)} disabled={pending !== null || ruleBusy} onChange={() => void change(row.key)} />
      </label>)}</div>}
      <div className="notification-custom">
        <h2>{word("Describe when to notify me", "用一句話設定通知")}</h2>
        <p>{word("Write the situation in your own words. We will show exactly what can trigger before saving. Currently, custom rules can watch a Session waiting for your answer or permission; other events use the switches above.", "寫下你想收到通知的情境；儲存前會先顯示系統理解的條件。目前自訂規則能觀察 Session 等你回答或授權；其他事件請使用上方開關。")}</p>
        {ruleError && <p className="notification-error" role="alert">{ruleError}</p>}
        {customRules.map((rule) => <div className="notification-custom-row" key={rule.id}>
          <label><input type="checkbox" checked={rule.enabled} disabled={ruleBusy || pending !== null} onChange={() => void saveRule({ action: "update", rule: { ...rule, enabled: !rule.enabled } })} />
            <span>{ruleSentence(rule)}</span>
          </label>
          <button type="button" disabled={ruleBusy || pending !== null} onClick={() => void saveRule({ action: "delete", id: rule.id })}>{word("Delete", "刪除")}</button>
        </div>)}
        <div className="notification-custom-form">
          <label>{word("When should we notify you?", "你想在什麼情況收到通知？")}<textarea rows={3} value={ruleText} placeholder={word("Notify me when a Session needs my answer.", "當 Session 等我回答時通知我")} onChange={(event) => { setRuleText(event.target.value); setPreview(null); setRuleError("") }} /></label>
          <button type="button" disabled={ruleBusy || pending !== null || customRules.length >= 16 || !ruleText.trim()} onClick={() => void interpret()}>{word("Check this situation", "理解這句話")}</button>
        </div>
        <div className="notification-custom-examples">
          <span>{word("Examples:", "例如：")}</span>
          {[word("Notify me when a Session needs my answer.", "當 Session 等我回答時通知我"), word("Notify me when a Session needs my answer for 5 minutes.", "當 Session 等我回答超過 5 分鐘時通知我"), word('Notify me when a Session named "deploy" needs my answer.', "當名稱包含「部署」的 Session 等我回答時通知我")].map((example) => <button type="button" key={example} onClick={() => { setRuleText(example); setPreview(null); setRuleError("") }}>{example}</button>)}
        </div>
        {preview && previewText === ruleText.trim() && <div className="notification-custom-preview" role="status"><strong>{word("This will notify you when:", "系統理解為：")}</strong><p>{ruleSentence(preview)}</p><small>{word("Immediately when first observed if no wait was specified. The notification opens that Session.", "若未指定等待時間，首次觀察到符合情境就通知；點擊通知會開啟該 Session。")}</small><button type="button" disabled={ruleBusy || pending !== null || customRules.length >= 16} onClick={() => void addRule()}>{word("Save this rule", "儲存這條規則")}</button></div>}
        <small>{word("Up to 16 rules per machine. When several match, the shortest wait wins. Disabled rules leave the standard setting in place.", "每台機器最多 16 條；多條符合時採用最短等待時間。停用規則後沿用原本的通知設定。")}</small>
      </div>
      <div className="notification-unavailable">
        <h2>{word("Not available yet", "尚未提供自動推播")}</h2>
        <p>{word("These older settings are stored, but have no active sender. Each will get a switch when it can actually send.", "下列舊設定仍會保存，但目前沒有實際發送者。接上發送流程後才會提供開關。")}</p>
        <ul>
          <li>{word("A Session reports delivery", "Session 回報交件")}</li>
          <li>{word("A whole batch of dispatched tasks ends", "整批派工結束")}</li>
          <li>{word("Smart wording for completion notices", "完成通知的智慧文案")}</li>
        </ul>
      </div>
    </div>
  </section>
}

export const page: PageModule = { id: "notifications", Component: NotificationsPage }
