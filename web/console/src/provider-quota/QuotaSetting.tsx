import { useState, useSyncExternalStore } from "react"
import { nextWord } from "../next-strings.js"
import { Switch } from "../pages/settings/Switch.js"
import { quotaVisible, setQuotaVisible, subscribeQuotaVisible } from "./visibility.js"
export function QuotaSetting() {
  const on = useSyncExternalStore(subscribeQuotaVisible, quotaVisible)
  const [saved, setSaved] = useState(true)
  return <div className="block" id="settings-provider-quota">
    <div className="settings-control-heading">
      <b id="settings-provider-quota-title">{nextWord("quotaSettingTitle")}</b>
      <Switch id="settings-provider-quota-switch" labelledBy="settings-provider-quota-title"
        describedBy="settings-provider-quota-hint" on={on} disabled={false}
        stateText={nextWord(on ? "quotaOn" : "quotaOff")}
        onToggle={() => setSaved(setQuotaVisible(!on))} />
    </div>
    <p className="say" id="settings-provider-quota-hint">{nextWord("quotaSettingHint")}</p>
    {!saved && <p className="said" role="status">{nextWord("quotaSaveFailed")}</p>}
  </div>
}
