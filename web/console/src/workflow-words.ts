/** The Workflow surface's English source and Taiwan Traditional Chinese copy. */
const words = {
  en: {
    reload: "Reload", intro: "Save a complete launch message for each Project. Skill import fills an editable draft.",
    projectUnavailable: "Could not read Projects. Try again.", workflowUnavailable: "Could not read this Project's Workflows or Skills. Try again.",
    loading: "Loading…", new: "Create Workflow", fromSkill: "Create from Skill", chooseSkill: "Choose a Skill",
    empty: "This Project has no Workflows yet.", edit: "Edit Workflow", name: "Name", message: "Launch message",
    save: "Save", cancel: "Cancel", delete: "Delete", needsText: "Enter a name and launch message.",
    saveUnknown: "The save result is unknown. Reload the list before trying again.",
    deleteUnknown: "The delete result is unknown. Reload the list.",
    tooLong: "The name or launch message is too long. Keep the name under 200 bytes and the message under 4,000 bytes.",
    limitReached: "This Project has reached the Workflow storage limit.",
    saveRefused: "This Workflow could not be saved. Check the name and launch message.",
    deleteRefused: "This Workflow could not be deleted. Reload the list.",
    confirmDelete: "Delete Workflow ‘{name}’?",
    skillPrompt: "Use the {name} Skill, complete its workflow, and report the result.",
    noSessionWorkflows: "This Project has no Workflows yet. Create one from the sidebar.",
    projectResolveUnavailable: "This machine could not identify the Session's Project. Update it before starting a Workflow here.",
    start: "Start Workflow", starting: "Starting…", startUnknown: "The start result is unknown. Check Session to-dos before sending again.",
    running: "Workflow · In progress", done: "Workflow · Done", sent: "Sent", queued: "Ready to send",
    complete: "Mark done", reopen: "Reopen", send: "Send",
  },
  "zh-Hant": {
    reload: "重新讀取", intro: "為每個 Project 保存完整的啟動訊息。從 Skill 帶入後仍可編輯。",
    projectUnavailable: "無法讀取 Project，請重試。", workflowUnavailable: "無法讀取此 Project 的 Workflow 或 Skill，請重試。",
    loading: "讀取中…", new: "建立 Workflow", fromSkill: "從 Skill 建立", chooseSkill: "選擇 Skill",
    empty: "這個 Project 還沒有 Workflow。", edit: "編輯 Workflow", name: "名稱", message: "啟動訊息",
    save: "儲存", cancel: "取消", delete: "刪除", needsText: "請填寫名稱與啟動訊息。",
    saveUnknown: "儲存結果無法確認；請重新讀取清單後再試。",
    deleteUnknown: "刪除結果無法確認；請重新讀取清單。",
    tooLong: "名稱或啟動訊息太長。名稱最多 200 位元組，訊息最多 4,000 位元組。",
    limitReached: "這個 Project 的 Workflow 儲存數量已達上限。",
    saveRefused: "無法儲存這個 Workflow，請檢查名稱與啟動訊息。",
    deleteRefused: "無法刪除這個 Workflow，請重新讀取清單。",
    confirmDelete: "確定刪除 Workflow「{name}」？",
    skillPrompt: "請執行「{name}」Skill，依照其中流程完成工作並回報結果。",
    noSessionWorkflows: "這個 Project 還沒有 Workflow。請從側邊欄建立。",
    projectResolveUnavailable: "這台機器無法辨識此 Session 所屬的 Project。更新機器後再從這裡啟動 Workflow。",
    start: "開始 Workflow", starting: "啟動中…", startUnknown: "啟動結果無法確認。請到 Session 待辦查看紀錄，再決定是否重送。",
    running: "Workflow · 執行中", done: "Workflow · 完成", sent: "已送出", queued: "待送出",
    complete: "標記完成", reopen: "重新開啟", send: "送出",
  },
} as const

export type WorkflowWord = keyof typeof words.en
export function workflowWord(key: WorkflowWord, holes: Record<string, string> = {}): string {
  const lang = typeof document !== "undefined" && document.documentElement.lang.toLowerCase().startsWith("zh-hant") ? "zh-Hant" : "en"
  const source = words[lang][key]
  return source.replace(/\{(\w+)\}/g, (all, name) => holes[name] ?? all)
}
