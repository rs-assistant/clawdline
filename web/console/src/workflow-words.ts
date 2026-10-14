/** The Workflow surface's English source and Taiwan Traditional Chinese copy. */
import type { Snippet } from "./legacy/snippets-bridge.js"
const words = {
  en: {
    reload: "Reload", intro: "Save a launch message for this Project or every Project on this machine. Import a Skill or command into an editable draft.",
    projectUnavailable: "Could not read Projects. Try again.", workflowUnavailable: "Could not read this Project's Workflows or Skills. Try again.",
    loading: "Loading…", new: "Create Workflow", fromSkill: "Create from Skill", chooseSkill: "Choose a Skill",
    empty: "This Project has no Workflows yet.", edit: "Edit Workflow", name: "Name", message: "Launch message",
    emptyTitle: "Give this Project a repeatable start", emptyDetail: "Save a launch message once, then start it from any Session in this Project.",
    importSkill: "Import a Skill or command", editAction: "Edit", workflowCount: "Saved Workflows", projectLabel: "Project",
    skillListTitle: "Choose a Skill or command", skillPreviewTitle: "Skill preview", commandPreviewTitle: "Command preview", claudeCommand: "Claude command", skillNoDescription: "No description provided",
    skillReadFailed: "Could not read this Skill. Try again.", backToSkills: "Back to Skills", useSkill: "Use this Skill",
    skillImportHint: "This creates an editable Workflow launch message using the Skill name.",
    commandImportHint: "The editable launch message asks the assistant to read this Project command. The command file is still required.",
    global: "Global", projectOnly: "This Project", availability: "Available in", globalHint: "Available in every Project on this machine.",
    chooseWorkflow: "Choose a Workflow to start", reviewLaunch: "Ready to start", noSkills: "No available Skills or commands to import for this Project",
    save: "Save", cancel: "Cancel", delete: "Delete", needsText: "Enter a name and launch message.",
    saveUnknown: "The save result is unknown. Reload the list before trying again.",
    deleteUnknown: "The delete result is unknown. Reload the list.",
    tooLong: "The name or launch message is too long. Keep the name under 200 bytes and the message under 4,000 bytes.",
    limitReached: "This Project has reached the Workflow storage limit.",
    saveRefused: "This Workflow could not be saved. Check the name and launch message.",
    deleteRefused: "This Workflow could not be deleted. Reload the list.",
    confirmDelete: "Delete Workflow ‘{name}’?",
    skillPrompt: "Use the {name} Skill, complete its workflow, and report the result.",
    commandPrompt: "Read the Project's Claude command /{name}, follow its instructions to complete the workflow, and report the result.",
    noSessionWorkflows: "This Project has no Workflows yet. Create one from the sidebar.",
    projectResolveUnavailable: "This machine could not identify the Session's Project. Project Workflows and the built-in review need it; other Global Workflows are available.",
    start: "Start Workflow", starting: "Starting…", startUnknown: "The start result is unknown. Check Session to-dos before sending again.",
    running: "Workflow · In progress", done: "Workflow · Done", sent: "Sent", queued: "Ready to send",
    complete: "Mark done", reopen: "Reopen", send: "Send",
    zeroReviewTitle: "Zero-Based Review",
    zeroReviewBody: `Run a zero-based review of this Project. Create an Epic to track it, then assign independent reviews from the relevant roles: product, UX, architecture, code quality, efficiency, documentation, and evidence.
Require evidence for every finding. Compare the current design with mature expert practice, and distinguish deliberate trade-offs from accumulated complexity.
Find confusing behavior, redundant steps or reads and writes, and missing capabilities.
Synthesize a report and a target workflow. Turn improvements into role-owned child items, and list decisions that need my input separately.
Collect shared facts once and include them in each review brief so reviewers do not repeat the same reading.`,
  },
  "zh-Hant": {
    reload: "重新讀取", intro: "為這個 Project 或這台機器上的所有 Project 儲存啟動訊息。從 Skill 或指令帶入後仍可編輯。",
    projectUnavailable: "無法讀取 Project，請重試。", workflowUnavailable: "無法讀取此 Project 的 Workflow 或 Skill，請重試。",
    loading: "讀取中…", new: "建立 Workflow", fromSkill: "從 Skill 建立", chooseSkill: "選擇 Skill",
    empty: "這個 Project 還沒有 Workflow。", edit: "編輯 Workflow", name: "名稱", message: "啟動訊息",
    emptyTitle: "為這個 Project 建立常用流程", emptyDetail: "儲存完整的啟動訊息，之後就能從這個 Project 的 Session 啟動。",
    importSkill: "從 Skill／指令帶入", editAction: "編輯", workflowCount: "已儲存的 Workflow", projectLabel: "Project",
    skillListTitle: "選擇 Skill 或指令", skillPreviewTitle: "預覽 Skill", commandPreviewTitle: "預覽指令", claudeCommand: "Claude 指令", skillNoDescription: "未提供說明",
    skillReadFailed: "無法讀取這個 Skill，請重試。", backToSkills: "返回 Skill 清單", useSkill: "帶入這個 Skill",
    skillImportHint: "帶入後會建立使用 Skill 名稱的啟動訊息，仍可編輯。",
    commandImportHint: "帶入後會建立請助理讀取此 Project 指令的啟動訊息，仍可編輯；執行時仍需原始指令檔案。",
    global: "全域", projectOnly: "這個 Project", availability: "使用範圍", globalHint: "這台機器上的所有 Project 都能使用。",
    chooseWorkflow: "選擇要啟動的 Workflow", reviewLaunch: "確認啟動", noSkills: "這個 Project 沒有可帶入的 Skill 或指令",
    save: "儲存", cancel: "取消", delete: "刪除", needsText: "請填寫名稱與啟動訊息。",
    saveUnknown: "儲存結果無法確認；請重新讀取清單後再試。",
    deleteUnknown: "刪除結果無法確認；請重新讀取清單。",
    tooLong: "名稱或啟動訊息太長。名稱最多 200 位元組，訊息最多 4,000 位元組。",
    limitReached: "這個 Project 的 Workflow 儲存數量已達上限。",
    saveRefused: "無法儲存這個 Workflow，請檢查名稱與啟動訊息。",
    deleteRefused: "無法刪除這個 Workflow，請重新讀取清單。",
    confirmDelete: "確定刪除 Workflow「{name}」？",
    skillPrompt: "請執行「{name}」Skill，依照其中流程完成工作並回報結果。",
    commandPrompt: "請讀取這個 Project 的 Claude 指令「/{name}」，依照其中內容完成流程並回報結果。",
    noSessionWorkflows: "這個 Project 還沒有 Workflow。請從側邊欄建立。",
    projectResolveUnavailable: "這台機器無法辨識此 Session 所屬的 Project。專屬 Workflow 與內建審查需要這項資訊；其他全域 Workflow 仍可使用。",
    start: "開始 Workflow", starting: "啟動中…", startUnknown: "啟動結果無法確認。請到 Session 待辦查看紀錄，再決定是否重送。",
    running: "Workflow · 執行中", done: "Workflow · 完成", sent: "已送出", queued: "待送出",
    complete: "標記完成", reopen: "重新開啟", send: "送出",
    zeroReviewTitle: "歸零審查",
    zeroReviewBody: `對這個 Project 做一次歸零審查。建立 Epic 追蹤工作，並依需要分派獨立的角色審查：產品、UX、架構、程式碼品質、效率、文件與證據。
每項結論都要附上證據。對照成熟的人類專家做法，分清刻意取捨與歷史累積的複雜度。
找出容易誤導人的行為、多餘步驟或讀寫，以及缺少的能力。
彙整審查報告與目標流程，把改善項目拆成各角色負責的子項目；需要我決定的取捨另外列出。
共用事實只整理一次，放進每份審查 brief，避免各角色重複讀取。`,
  },
} as const

export type WorkflowWord = keyof typeof words.en
export function workflowWord(key: WorkflowWord, holes: Record<string, string> = {}): string {
  const lang = typeof document !== "undefined" && document.documentElement.lang.toLowerCase().startsWith("zh-hant") ? "zh-Hant" : "en"
  const source = words[lang][key]
  return source.replace(/\{(\w+)\}/g, (all, name) => holes[name] ?? all)
}

/** An unedited built-in uses the reader's language; edited rows keep their text. */
export function localizedWorkflow(row: Snippet): Snippet {
  if (row.builtin !== "zero_review") return row
  return { ...row, title: workflowWord("zeroReviewTitle"), body: workflowWord("zeroReviewBody") }
}
