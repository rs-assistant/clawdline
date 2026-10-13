import { useEffect, useState, useSyncExternalStore } from "react"
import type { SettingsRequest, SettingsSnapshot } from "@clawdline/contract"
import { readNotificationSettings, writeNotificationSettings } from "./settings/api.js"
import { settingsFailureSentence } from "./settings/window/failure.js"
import { pushShape, sendPushTest, startPush, subscribePush, togglePush } from "../push/push.js"
import { legacyState } from "../legacy/overlay-bridge.js"
import type { PageModule } from "./types.js"
import "./notifications/notifications.css"

type Preference = keyof Pick<SettingsRequest,
  "push_on_waiting" | "orchestrator_agent_notify" | "push_on_board_decision" |
  "push_on_board_done" | "push_on_schedule_failure" | "push_on_capacity" |
  "push_on_dead_letter" | "push_on_app_release_success" | "push_on_cloud_release_success" | "push_on_release_failure">

type Row = { key: Preference; en: string; zh: string; destination: "Session" | "list" | "optional-session"; hintEn: string; hintZh: string }

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

function current(snapshot: SettingsSnapshot, key: Preference): boolean {
  const value = snapshot[key]
  if (typeof value === "boolean") return value
  if (key === "push_on_waiting") return snapshot.orchestrator_agent_notify ?? true
  if (key === "push_on_app_release_success" || key === "push_on_cloud_release_success" || key === "push_on_release_failure") return snapshot.push_on_deploy ?? false
  return true
}

function NotificationsPage({ shown }: { shown: boolean }) {
  const push = useSyncExternalStore(subscribePush, pushShape)
  const [settings, setSettings] = useState<SettingsSnapshot | null>(null)
  const [pending, setPending] = useState<Preference | null>(null)
  const [error, setError] = useState("")

  useEffect(() => {
    if (!shown) return
    startPush()
    let live = true
    void readNotificationSettings().then((answer) => {
      if (live) { setSettings(answer); setError("") }
    }, (cause: unknown) => { if (live) setError(settingsFailureSentence(cause)) })
    return () => { live = false }
  }, [shown])

  const change = async (key: Preference) => {
    if (!settings || pending) return
    setPending(key)
    setError("")
    try {
      const answer = await writeNotificationSettings({ [key]: !current(settings, key) } as Partial<SettingsRequest>)
      setSettings(answer)
    } catch (cause) {
      setError(settingsFailureSentence(cause))
    } finally {
      setPending(null)
    }
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
        <input type="checkbox" checked={current(settings, row.key)} disabled={pending !== null} onChange={() => void change(row.key)} />
      </label>)}</div>}
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
