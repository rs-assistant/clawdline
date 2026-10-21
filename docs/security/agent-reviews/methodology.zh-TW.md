# Agent 資安檢視的做法

[English](./methodology.md)・[索引](./README.zh-TW.md)

這一頁讓你可以重做這些檢視，或判斷它們該占多少分量。

## 問題

每份檢視收到的都是同一個最小提示：專案的一個固定 GitHub 網址（指向特定 commit），加上這一句：

> 要安裝這個工具有無資安風險？

提示裡沒有其他內容：沒有告訴檢視者這是比較實驗的一部分、誰開發了 Clawdline，也沒有要求它一定要判定安全或不安全。

## 設定

- 日期：2026-10-21。輪次：第 1 輪。
- 檢視者：Claude Code 與 Codex，每份檢視都在新的、獨立的 CLI session 執行。
- 工具：只能讀網路（WebFetch、WebSearch 或檢視者自己的即時網頁工具）。不能讀本機檔案，也沒有 clone、安裝或執行任何專案。

| 專案 | Repository | Commit |
|---|---|---|
| Clawdline | `sainteye/clawdline` | `31139d6` |
| Herdr | `herdrdev/herdr` | `143bf5f` |
| T3 Code | `pingdotgg/t3code` | `7ffc3bb` |
| Orca | `stablyai/orca` | `84af8ef` |

完整的 commit hash 在 [`report-metadata.json`](./report-metadata.json)。

## 這不是什麼

每份檢視的問題相同，但檢視者讀得多深沒有控制：有些只讀 README 與文件，有些讀了部分原始碼。這不是對照實驗、安裝實測、完整資安稽核、依賴掃描，也沒有驗證任何二進位檔與原始碼一致。

## 文字怎麼編輯

- 繁體中文原文逐段保留。標題降為四級，讓它排在每頁自己的標題之下；結尾一行與比較無關的「Fixed but not yet released」狀態已刪除。
- 英文頁是本專案編輯的完整翻譯，並標明是翻譯。檢視者沒有用英文作答。
- 檢視者附的來源連結原樣保留。T3 報告的資安通報信箱依本程式庫隱私守衛規則，改為連到該專案公開的 SECURITY.md；兩語報告均揭露這項替換。
- 編輯註記與檢視原文分開。檢視有過度推論時（例如把知名度當安全背書，或把「沒有公告」讀成「沒有漏洞」），註記會寫「reviewer claim, not independently established」或給出修正，但不改原文。註記說「已核對」的，是用網路讀取、對照被檢視專案在固定 commit 的公開檔案。
