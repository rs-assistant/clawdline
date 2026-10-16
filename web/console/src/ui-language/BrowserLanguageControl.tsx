import { useState } from "react"
import { browserPreference, CATALOG_TAGS, catalogWord, catalogWordLanguage, saveBrowserPreference } from "../catalog.js"
import "./browser-language.css"
import { ChoiceMenu } from "./ChoiceMenu.js"

const names: Record<(typeof CATALOG_TAGS)[number], string> = {
  en: "English",
  "zh-Hant": "繁體中文",
  ja: "日本語",
  "zh-Hans": "简体中文",
  ko: "한국어",
  es: "Español",
  "pt-BR": "Português (Brasil)",
  fr: "Français",
  de: "Deutsch",
}
const flags: Record<(typeof CATALOG_TAGS)[number], string> = {
  en: "🇺🇸", "zh-Hant": "🇹🇼", ja: "🇯🇵", "zh-Hans": "🇨🇳", ko: "🇰🇷",
  es: "🇪🇸", "pt-BR": "🇧🇷", fr: "🇫🇷", de: "🇩🇪",
}

/** Browser-local UI choice. The saved value is never the daemon's agent or voice setting. */
export function BrowserLanguageControl({ id, compact = false }: { id: string; compact?: boolean }) {
  const [preference, setPreference] = useState(browserPreference)
  const [state, setState] = useState<"ready" | "saving" | "failed">("ready")
  const unsupported = preference !== "auto" && !CATALOG_TAGS.includes(preference as (typeof CATALOG_TAGS)[number])
  const label = catalogWord("ui", "language")
  const choose = (value: string) => {
    setState("saving")
    if (!saveBrowserPreference(value)) {
      setState("failed")
      return
    }
    setPreference(value)
    location.reload()
  }
  return (
    <div className={`ui-language-control${compact ? " compact" : ""}`}>
      {!compact && <div className="ui-language-label" lang={catalogWordLanguage("ui", "language")}>{label}</div>}
      <ChoiceMenu id={id} label={label} value={preference} disabled={state === "saving"} compact={compact} onPick={choose}
        options={[
          { value: "auto", label: catalogWord("ui", "auto"), icon: "🌐", lang: catalogWordLanguage("ui", "auto") },
          ...CATALOG_TAGS.map((tag) => ({ value: tag, label: names[tag], icon: flags[tag], lang: tag })),
          ...(unsupported ? [{ value: preference, label: catalogWord("ui", "unsupportedValue").replace("{value}", preference), icon: "🌐", lang: catalogWordLanguage("ui", "unsupportedValue") }] : []),
        ]} />
      {!compact && <p className="ui-language-hint" lang={catalogWordLanguage("ui", "browserOnly")}>{catalogWord("ui", "browserOnly")}</p>}
      {unsupported && <p className="ui-language-warning" role="status" lang={catalogWordLanguage("ui", "unsupportedHint")}>{catalogWord("ui", "unsupportedHint")}</p>}
      <p className="ui-language-status" role="status" aria-live="polite" lang={catalogWordLanguage("ui", state === "failed" ? "saveFailed" : "applying")}>
        {state === "saving" ? catalogWord("ui", "applying") : state === "failed" ? catalogWord("ui", "saveFailed") : ""}
      </p>
    </div>
  )
}
