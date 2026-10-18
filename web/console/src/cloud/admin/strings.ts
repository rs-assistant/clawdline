/*
 * The operations page's own words, in the two languages every product change
 * carries (docs/localization.md). The page stands outside the console, before
 * any catalog is loaded, so it holds a small table of its own; every other
 * language reads the English one.
 *
 * Nothing is imported at run time, so `node --test` loads it as it is.
 */

export type AdminLocale = "en" | "zh-TW"

/** Taiwan Traditional Chinese for any Traditional Chinese preference; English for the rest. */
export function adminLocale(languages: readonly string[]): AdminLocale {
  for (const raw of languages) {
    const tag = raw.toLowerCase()
    if (tag === "zh-tw" || tag === "zh-hk" || tag === "zh-mo" || tag.startsWith("zh-hant")) return "zh-TW"
    if (tag === "en" || tag.startsWith("en-")) return "en"
  }
  return "en"
}

const en = {
  docTitle: "Operations · Clawdline Cloud",
  title: "Operations",
  subtitle: "Anonymous totals for Clawdline Cloud",
  back: "Back to console",
  refresh: "Refresh",
  refreshing: "Refreshing…",
  generated: "Updated {time} UTC",
  checking: "Checking your session…",
  loading: "Loading the overview…",
  signInTitle: "Sign in to continue",
  signInBody: "This page is for Clawdline operators. Sign in with GitHub to check your access.",
  signIn: "Sign in with GitHub",
  forbiddenTitle: "Nothing to show here",
  forbiddenBody: "This account does not have access to operations data. If you expected it to, sign in with the GitHub account that was granted access.",
  failedTitle: "The overview could not be loaded",
  failedBody: "No figures are shown, because none could be confirmed. Try again in a moment.",
  failedCode: "Reason: {code}",
  sessionFailedTitle: "Your session could not be checked",
  misdeclared: "This build does not declare a Cloud service, so there is no operations page here.",
  tryAgain: "Try again",
  deviceLimitSignIn: "Sign in again",
  staleNote: "The last refresh failed ({code}). The figures below are from {time} UTC.",

  sectionAccounts: "Accounts",
  sectionPromotions: "Promotion codes",
  sectionUsage: "Monthly usage",
  accountsTotal: "Total accounts",
  accountsNew7: "New, last 7 days",
  accountsNew30: "New, last 30 days",
  redemptionsTotal: "Redemptions",
  redeemersTotal: "Accounts that redeemed",
  redeemed7: "Redeemed, last 7 days",
  redeemed30: "Redeemed, last 30 days",
  promoInventory: "{campaigns} campaigns · {codes} codes issued. One code can serve many accounts, so codes are not a redemption-rate base.",
  windowNote: "7- and 30-day figures count complete UTC days, ending yesterday, against the same number of days before them.",

  trendUp: "▲ {delta}",
  trendDown: "▼ {delta}",
  trendFlat: "No change",
  trendPercent: "{percent}%",
  trendAgainst: "previous {previous}",
  trendLabelUp: "Up {delta} from {previous} in the previous period",
  trendLabelUpPercent: "Up {delta} ({percent}%) from {previous} in the previous period",
  trendLabelDown: "Down {delta} from {previous} in the previous period",
  trendLabelDownPercent: "Down {delta} ({percent}%) from {previous} in the previous period",
  trendLabelFlat: "Same as the previous period, {previous}",

  chartAccounts: "New accounts per day",
  chartRedemptions: "Redemptions per day",
  chartRange: "{from} – {to}, UTC",
  chartSummary: "{total} in {days} days; busiest day {peakDay} with {peak}.",
  chartEmpty: "No days reported yet.",
  chartTable: "Show as table",
  chartDay: "Day",
  chartCount: "Count",

  campaignsTitle: "Campaigns",
  campaignsNone: "No campaigns yet.",
  campaignsCapped: "Showing the latest {shown} of {total} campaigns.",
  campaignName: "Campaign",
  campaignUnnamed: "Unnamed campaign",
  campaignStatus: "Status",
  campaignWindow: "Window",
  campaignRedeemed: "Redeemed",
  campaignCodes: "Codes available",
  campaignOf: "{n} of {limit}",
  campaignNoLimit: "{n} · no limit",
  campaignCodesValue: "{available} of {issued}",
  windowFrom: "from {date}",
  windowUntil: "until {date}",
  windowRange: "{from} – {to}",
  windowAlways: "Always",
  status_redeemable: "Redeemable",
  status_not_started: "Not started",
  status_expired: "Expired",
  status_revoked: "Revoked",
  status_campaign_exhausted: "Limit reached",
  status_no_available_code: "No codes left",
  status_unknown: "Unknown",

  usageCurrent: "This month",
  usagePrevious: "Last month",
  usagePartial: "Partial data",
  usagePartialNote: "Not every source reported for this month; figures may be low.",
  usageActive: "Active accounts",
  usageDispatches: "Dispatches",
  usageMinutes: "Connected minutes",
  usageIn: "Data in",
  usageOut: "Data out",
  usageUnknown: "Unknown",
  usageUnknownLabel: "Unknown: the data could not be confirmed as complete",
  usageNoGrowth: "Months are shown side by side; a month still in progress is not compared as growth.",
}

export type AdminWord = keyof typeof en

const zhTW: Record<AdminWord, string> = {
  docTitle: "營運 · Clawdline Cloud",
  title: "營運",
  subtitle: "Clawdline Cloud 的匿名匯總數據",
  back: "返回主控台",
  refresh: "重新整理",
  refreshing: "重新整理中…",
  generated: "更新於 UTC {time}",
  checking: "正在確認登入狀態…",
  loading: "正在載入營運數據…",
  signInTitle: "請先登入",
  signInBody: "這個頁面只開放給 Clawdline 營運者。請用 GitHub 登入以確認權限。",
  signIn: "使用 GitHub 登入",
  forbiddenTitle: "這裡沒有可顯示的內容",
  forbiddenBody: "這個帳號沒有營運數據的存取權限。如果你應該要有，請改用獲得授權的 GitHub 帳號登入。",
  failedTitle: "無法載入營運數據",
  failedBody: "目前沒有任何數字能確認，所以不顯示數據。請稍後再試一次。",
  failedCode: "原因：{code}",
  sessionFailedTitle: "無法確認登入狀態",
  misdeclared: "這個版本沒有宣告 Cloud 服務，因此沒有營運頁面。",
  tryAgain: "再試一次",
  deviceLimitSignIn: "重新登入",
  staleNote: "最近一次重新整理失敗（{code}）。下方數字是 UTC {time} 的資料。",

  sectionAccounts: "帳號",
  sectionPromotions: "推廣碼",
  sectionUsage: "每月用量",
  accountsTotal: "帳號總數",
  accountsNew7: "近 7 日新增",
  accountsNew30: "近 30 日新增",
  redemptionsTotal: "兌換次數",
  redeemersTotal: "兌換過的帳號",
  redeemed7: "近 7 日兌換",
  redeemed30: "近 30 日兌換",
  promoInventory: "{campaigns} 個活動・已發 {codes} 組碼。一組碼可供多個帳號使用，所以碼數不能當兌換率的分母。",
  windowNote: "近 7／30 日以完整的 UTC 日計算，算到昨天為止，並與前一段同樣天數比較。",

  trendUp: "▲ {delta}",
  trendDown: "▼ {delta}",
  trendFlat: "持平",
  trendPercent: "{percent}%",
  trendAgainst: "前期 {previous}",
  trendLabelUp: "比前期的 {previous} 增加 {delta}",
  trendLabelUpPercent: "比前期的 {previous} 增加 {delta}（{percent}%）",
  trendLabelDown: "比前期的 {previous} 減少 {delta}",
  trendLabelDownPercent: "比前期的 {previous} 減少 {delta}（{percent}%）",
  trendLabelFlat: "與前期相同，為 {previous}",

  chartAccounts: "每日新增帳號",
  chartRedemptions: "每日兌換次數",
  chartRange: "{from}－{to}（UTC）",
  chartSummary: "{days} 天共 {total}；最多的一天是 {peakDay}，共 {peak}。",
  chartEmpty: "目前還沒有每日資料。",
  chartTable: "以表格檢視",
  chartDay: "日期",
  chartCount: "數量",

  campaignsTitle: "活動",
  campaignsNone: "目前還沒有活動。",
  campaignsCapped: "共 {total} 個活動，只顯示最新的 {shown} 個。",
  campaignName: "活動",
  campaignUnnamed: "未命名活動",
  campaignStatus: "狀態",
  campaignWindow: "期間",
  campaignRedeemed: "已兌換",
  campaignCodes: "可用碼",
  campaignOf: "{n}／{limit}",
  campaignNoLimit: "{n}・不限次數",
  campaignCodesValue: "{available}／{issued}",
  windowFrom: "{date} 起",
  windowUntil: "至 {date}",
  windowRange: "{from}－{to}",
  windowAlways: "不限期間",
  status_redeemable: "可兌換",
  status_not_started: "尚未開始",
  status_expired: "已過期",
  status_revoked: "已撤銷",
  status_campaign_exhausted: "已達上限",
  status_no_available_code: "碼已用完",
  status_unknown: "未知",

  usageCurrent: "本月",
  usagePrevious: "上月",
  usagePartial: "資料不完整",
  usagePartialNote: "本月並非所有來源都已回報，數字可能偏低。",
  usageActive: "活躍帳號",
  usageDispatches: "派工次數",
  usageMinutes: "連線分鐘數",
  usageIn: "傳入資料量",
  usageOut: "傳出資料量",
  usageUnknown: "無法確認",
  usageUnknownLabel: "無法確認：資料無法證明完整",
  usageNoGrowth: "兩個月並列顯示；尚未結束的月份不計算成長率。",
}

const TABLES: Record<AdminLocale, Record<AdminWord, string>> = { en, "zh-TW": zhTW }

export type Say = (key: AdminWord, values?: Record<string, string | number>) => string

/** The sentence for `key` in `locale`, with `{name}` placeholders filled. */
export function adminWords(locale: AdminLocale): Say {
  const table = TABLES[locale]
  return (key, values) =>
    table[key].replace(/\{(\w+)\}/g, (whole, name: string) => (values && name in values ? String(values[name]) : whole))
}
