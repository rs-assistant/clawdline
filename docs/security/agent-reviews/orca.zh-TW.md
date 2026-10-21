# Orca：兩份 Agent 檢視

[English](./orca.md)・[索引](./README.zh-TW.md)・[方法](./methodology.zh-TW.md)

- 檢視對象：[`stablyai/orca@84af8ef`](https://github.com/stablyai/orca/tree/84af8efee3e4307ad13c800b45a5ab938ad4de7b)
- 日期：2026-10-21
- 問題：「要安裝這個工具有無資安風險？」
- 檢視者：Claude Code、Codex，各自在新的 session 中只用網路讀取作答

以下先是兩份檢視的原文，再是編輯註記。原文是檢視者的看法，不是本專案的結論。

**編輯範圍：** 原文逐段保留；只把原文的標題降為四級標題，並刪除結尾一行與比較無關的「Fixed but not yet released」狀態。其他字句未改。

## Claude Code 的原文

**結論：沒有發現惡意跡象，也沒有已公開的漏洞。不過這是一個權限很大的工具，主要的風險來自它「能做的事」，而不是程式碼本身有問題。**

我只看了 repo 首頁、Security 頁和 `package.json`，沒有逐行審查原始碼，也沒有檢查發行的二進位檔。

#### 正面因素
- **授權與熱度**：MIT 開源，約 10 萬 stars、1.55 萬次 commit，由 YC 投資的 Stably AI 維護，看起來是正規專案，不是來路不明的 repo。
- **安裝方式**：用 .dmg 安裝檔、`brew install --cask stablyai/orca/orca` 或 AUR 安裝，不需要 `curl | bash`。Windows 版有經 SignPath 程式碼簽章。
- **已知問題**：GitHub 上沒有公開的安全公告。網路上搜尋到的 Orca CVE 都屬於同名的其他軟體（GNOME Orca 螢幕閱讀器、日本醫療系統 ORCA），跟這個專案無關。

#### 需要注意的風險
1. **權限非常大**：它的用途就是同時跑多個 AI coding agent（Claude Code、Codex 等），這些 agent 能執行 shell 指令、讀寫你的檔案。「Computer Use」功能更能讓 agent 操作桌面上的其他 App。實際風險主要取決於 agent 的權限怎麼設定。
2. **手機遠端控制**：手機 App 透過 `cloud/` 中的 relay 和桌機配對，等於多了一個可以遠端下指令給 agent 的管道。如果不需要，建議不要配對。
3. **帳號憑證集中**：它會管理你的 Claude、OpenAI、GitHub、Linear 等帳號，還有 SSH 遠端連線（`ssh2`）。這台電腦一旦被入侵，影響範圍會比較大。
4. **遙測**：使用 PostHog 收集匿名使用數據，可以關閉。
5. **自動更新**：使用 `electron-updater`，代表你信任 Stably 未來推送的每一個版本（也就是供應鏈風險）。
6. **沒有 SECURITY.md**：專案沒有正式的漏洞通報政策。以這個規模的專案來說，安全流程算是比較不成熟。
7. **從原始碼建置的情況**：如果你用 `pnpm install` 自己建置，會執行 `postinstall`（重新編譯 native 模組）和 husky 的 git hook，也會一併安裝大量 npm 相依套件。一般使用者直接下載安裝檔就好。

#### 建議
- 從官方來源（onorca.dev、GitHub Releases 或 Homebrew）下載。
- 安裝後先關閉遙測，用不到的話也不要開 Computer Use 和手機配對。
- 不要讓 agent 使用「全部自動核准」或略過權限確認的模式，尤其是在放有機密資料或正式環境憑證的專案裡。
- 對安全要求高的話，可以在獨立的使用者帳號、VM 或專用機器上執行。

如果需要，我可以下載這個 commit 的原始碼，深入檢查遙測送出的內容、relay 的驗證機制，或 Computer Use 的權限控管。

Sources:
- [stablyai/orca @ 84af8ef](https://github.com/stablyai/orca/tree/84af8efee3e4307ad13c800b45a5ab938ad4de7b)
- [stablyai/orca Security](https://github.com/stablyai/orca/security)
- [package.json](https://raw.githubusercontent.com/stablyai/orca/84af8efee3e4307ad13c800b45a5ab938ad4de7b/package.json)
- [GNOME Orca CVE-2013-4245（同名但無關）](https://cvemon.intruder.io/cves/CVE-2013-4245)
- [JVN#37376131 ORCA（同名但無關）](https://jvn.jp/en/jp/JVN37376131/)

## Codex 的原文

**有資安風險。我建議先用隔離環境試用，不要直接裝在存有正式環境憑證的主要工作帳號。** 這不代表 Orca 是惡意軟體，而是它的功能需要很大的信任範圍。

我抽查了你指定的 commit `84af8ef`（版本 `1.4.214`），主要風險如下：

| 風險 | 查到的依據與影響 |
|---|---|
| **本機指令與檔案操作** | 支援終端、AI agent、SSH 與桌面操作。若程式或 agent 遭利用，可能影響該使用者能存取的程式碼與憑證；Git worktree 只是工作目錄隔離，不能當成安全沙箱。[功能說明](https://github.com/stablyai/orca/blob/84af8efee3e4307ad13c800b45a5ab938ad4de7b/README.md) |
| **會介入既有 agent 環境** | 啟動流程會依條件安裝／更新 agent hooks 與狀態外掛。因此影響可能延伸到原本的 CLI 使用流程。[啟動程式碼](https://github.com/stablyai/orca/blob/84af8efee3e4307ad13c800b45a5ab938ad4de7b/src/main/startup/main-process-ready-runtime.ts) |
| **遠端控制增加攻擊面** | 手機控制、SSH、瀏覽器操作都值得進一步檢查身分驗證、配對撤銷及權限邊界。本次尚未完整驗證這些路徑，不能保證遠端控制安全。[功能說明](https://github.com/stablyai/orca/blob/84af8efee3e4307ad13c800b45a5ab938ad4de7b/README.md) |
| **從原始碼安裝也會執行程式** | `pnpm install` 的 `postinstall` 會建置套件並重建原生依賴；安裝本身就需要信任專案與依賴供應鏈。[package.json](https://github.com/stablyai/orca/blob/84af8efee3e4307ad13c800b45a5ab938ad4de7b/package.json) |

也有看到保護措施：主視窗設定了 Electron `sandbox: true`；遙測有同意判斷與停用開關；macOS 發行建置配置了簽章、公證及 hardened runtime。不過，**設定存在不等於下載的安裝檔已通過驗證，也不代表整個工具受到沙箱限制**。[視窗設定](https://github.com/stablyai/orca/blob/84af8efee3e4307ad13c800b45a5ab938ad4de7b/src/main/window/createMainWindow.ts)、[遙測控制](https://github.com/stablyai/orca/blob/84af8efee3e4307ad13c800b45a5ab938ad4de7b/src/main/telemetry/consent.ts)、[發行設定](https://github.com/stablyai/orca/blob/84af8efee3e4307ad13c800b45a5ab938ad4de7b/config/electron-builder.config.cjs)

實際試用時，我建議：

- 用獨立作業系統帳號或 VM，只放測試專案，避免共用 SSH keys、雲端憑證和正式 `.env`。
- 先不啟用手機／遠端控制，不授予完整磁碟、輔助使用或螢幕錄製權限，直到確實需要。
- 保留 agent 的指令審批機制。
- 安裝前驗證發行檔來源與簽章；你貼的是固定原始碼版本，下載頁或 Homebrew 安裝的版本未必相同。

**目前結論是「需要高度信任的開發工具，可以隔離試用；尚不足以放心放入敏感工作環境」。** 本次是部分原始碼靜態檢查，沒有執行安裝檔、監測連線或完成依賴漏洞掃描，因此不能作為無惡意程式或無漏洞的保證。

## 編輯註記

這一節是本專案編輯寫的，不屬於檢視者原文。標示「reviewer claim, not independently established」的，是我們沒有從一手來源查證的說法。

- **閱讀深度不同。** Claude Code 明說只看 repo 首頁、Security 頁與 `package.json`。Codex 抽查了 README、啟動、視窗、遙測與發行設定等原始碼。
- **已從一手來源核對：** 指定 commit 的 [`consent.ts`](https://raw.githubusercontent.com/stablyai/orca/84af8efee3e4307ad13c800b45a5ab938ad4de7b/src/main/telemetry/consent.ts) 有同意判斷、首次啟動提示，並在 CI 或環境變數下停用，與 Codex 的描述一致。
- **「PostHog 收集匿名使用數據」：reviewer claim, not independently established。** 我們只確認有同意與停用機制，沒有查證送出的內容是否匿名，也沒有查證預設狀態。
- **星數、commit 數、YC 投資不是安全背書。** 約 10 萬 stars、1.55 萬次 commit：reviewer claim, not independently established。沒有公開安全公告不等於沒有漏洞；Windows 版經 SignPath 簽章同樣未經我們查證。
- **沒有 SECURITY.md：** 我們在指定 commit 的根目錄與 `.github/` 都查不到，與原文一致；但這不排除專案在其他地方提供通報管道。
- **Homebrew 安裝不需要 `curl | bash`，** 但這只是換發布管道，不代表天然更安全。
