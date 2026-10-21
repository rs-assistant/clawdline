# Herdr：兩份 Agent 檢視

[English](./herdr.md)・[索引](./README.zh-TW.md)・[方法](./methodology.zh-TW.md)

- 檢視對象：[`herdrdev/herdr@143bf5f`](https://github.com/herdrdev/herdr/tree/143bf5fd4781d164260c63fe28e7dc08054ad32f)
- 日期：2026-10-21
- 問題：「要安裝這個工具有無資安風險？」
- 檢視者：Claude Code、Codex，各自在新的 session 中只用網路讀取作答

以下先是兩份檢視的原文，再是編輯註記。原文是檢視者的看法，不是本專案的結論。

**編輯範圍：** 原文逐段保留；只把原文的標題降為四級標題，並刪除結尾一行與比較無關的「Fixed but not yet released」狀態。其他字句未改。

## Claude Code 的原文

**結論：從我能查到的資料看，安裝本身風險不高，沒發現惡意或特別可疑的地方。** 不過這是我看文件和安裝腳本得出的判斷，沒有逐行審查原始碼。真正要留意的是它的功能本身帶來的權限，不是安裝這一步。

#### 安裝這一步（風險低）
- 我讀了安裝腳本 `curl -fsSL https://herdr.dev/install.sh | sh`：
  - 不用 sudo，binary 預設裝到 `~/.local/bin`
  - 不會改 `.zshrc` 等 shell 設定檔
  - 會用 SHA-256 驗證下載檔，但雜湊值和 binary 來自同一個 manifest（`herdr.dev/latest.json`），**沒有數位簽章**。所以它只能防下載損壞，防不了 herdr.dev 本身被入侵
- 想多一層保障，可以改用 `brew install herdr`。
- 專案用 Rust 寫成，Apache 2.0 授權，約 4.5 萬顆星。GitHub 上沒有已公開的安全公告，但**也沒有 SECURITY.md**（沒有正式的漏洞通報政策）。
- 注意：你給的是特定 commit `143bf5f`，但安裝腳本裝的是官方最新發行版的 binary，兩者不一定是同一份程式碼。要確定跑的就是那個 commit，得自己 `cargo build --release`。

#### 使用時要知道的風險
1. **本機 socket 權限很大**：`~/.config/herdr/herdr.sock` 可以讀取任何 pane 的輸出、送按鍵、執行指令、對 agent 下 prompt，文件裡沒提到任何身分驗證。也就是說，你這個帳號底下的任何程式（包括 AI agent 本身）都能透過它控制所有終端機。在單人電腦上問題不大，多人共用的機器就要確認 socket 的檔案權限。
2. **外掛（plugin）＝執行任意程式碼**：官方文件寫明外掛「不審查、不沙箱化」，會以你的帳號身分執行。marketplace 只是自動收錄 GitHub 上加了 `herdr-plugin` 標籤的 repo，沒有人工審核。第三方外掛要先看過原始碼再裝，可以用 `--ref` 鎖定版本，不要加 `--yes` 跳過預覽。
3. **Agent 整合會改設定檔**：執行 `herdr integration install claude` 之類的指令時，會在 Claude Code 的 `settings.json` 加入 hook，並寫入 `hooks/herdr-agent-state.sh`（Codex、Copilot 等也一樣）。只有你主動執行才會改，也可以解除安裝，但裝之前最好先看一下 hook 腳本的內容。
4. **SSH 遠端機器**：它能保存並連線到遠端機器，這部分的認證細節我沒有深入查。

#### 建議
- 一般個人開發機：用 Homebrew 裝或官方腳本裝都可以接受。
- 謹慎做法：自行從原始碼編譯、只用官方內建功能、不裝第三方外掛、裝 agent 整合前先檢查 hook 腳本。
- 公司環境：遠端控制 socket 加上 agent 自動化，可能牴觸公司的端點安全政策，建議先問過資安部門。

如果需要，我可以接著審查該 commit 裡的 socket 權限設定（例如 socket 建立時的 chmod）或 hook 腳本內容。

Sources:
- [herdr GitHub repo](https://github.com/herdrdev/herdr/tree/143bf5fd4781d164260c63fe28e7dc08054ad32f)
- [install.sh](https://herdr.dev/install.sh)
- [Security 頁面](https://github.com/herdrdev/herdr/security)
- [Socket API 文件](https://herdr.dev/docs/socket-api/)
- [Integrations 文件](https://herdr.dev/docs/integrations/)
- [Plugins 文件](https://herdr.dev/docs/plugins/)

## Codex 的原文

**有風險。我建議先在隔離環境試用，不要直接放進有正式環境憑證的主力帳號。** 目前讀到的部分沒有明顯惡意行為，但不足以認定整個工具安全。

我針對你指定的 `143bf5f…` 版本做了靜態抽查，主要發現：

| 項目 | 判斷 |
|---|---|
| **安裝來源** | 腳本會下載 `herdr.dev/latest.json` 指定的最新版，**不保證是你貼的 commit**。有 SHA-256 驗證，但下載位置與雜湊由同一份網站清單提供；網站若遭入侵，兩者可以一起被替換。[安裝腳本](https://raw.githubusercontent.com/herdrdev/herdr/143bf5fd4781d164260c63fe28e7dc08054ad32f/distribution/install.sh) |
| **安裝權限** | 檢查到的 Unix 腳本預設只寫入 `~/.local/bin`，沒有要求 `sudo`、新增開機服務或自動修改 shell 設定，這點相對保守。[安裝腳本](https://raw.githubusercontent.com/herdrdev/herdr/143bf5fd4781d164260c63fe28e7dc08054ad32f/distribution/install.sh) |
| **終端控制能力** | 它的 API 可以讀取其他 pane 的輸出、傳送按鍵及啟動 agent。因此，能存取控制介面的程式，可能讀到終端顯示的秘密或操作其他工作階段；不應把不同 pane 當成安全隔離。[官方 API 文件](https://herdr.dev/docs/socket-api/) |
| **本機存取保護** | 原始碼有將 client socket 設為 `0600` 的機制，限制其他帳號存取；但這不能隔離同一帳號下的不可信程式或 agent。[socket 權限程式碼](https://raw.githubusercontent.com/herdrdev/herdr/143bf5fd4781d164260c63fe28e7dc08054ad32f/src/server/socket_paths.rs) |
| **外掛** | **風險最高的擴充入口。** 官方明確說明外掛沒有 sandbox，會以你的帳號執行、繼承環境變數，並可呼叫完整 CLI；安裝外掛等同信任另一套程式。[官方安全說明](https://herdr.dev/docs/plugins/#trust-and-security) |
| **背景連線** | 設定範本顯示會檢查版本與 agent 偵測規則更新；更新模組說明背景版本檢查只提示，手動更新才替換 binary。這次沒有完成全程式的資料外傳追查。[設定範本](https://raw.githubusercontent.com/herdrdev/herdr/143bf5fd4781d164260c63fe28e7dc08054ad32f/src/main.rs)、[更新模組](https://raw.githubusercontent.com/herdrdev/herdr/143bf5fd4781d164260c63fe28e7dc08054ad32f/src/update.rs) |

我的安裝建議：

- 用獨立 OS 帳號或 VM 試用，不提供正式 SSH key、雲端憑證或 production `.env`。
- 固定版本；要評估這個 commit，就從這個 commit 建置並鎖定依賴。自行編譯仍有依賴供應鏈風險。
- 避免直接 `curl | sh`；先下載、審閱再執行，也不要加 `sudo`。
- 初期不裝第三方外掛，也不把可信與不可信的 agent 工作混在一起。

**結論：一般開發測試可在隔離後試用；含公司機密或正式憑證的環境，現有證據還不足以放心導入。** 這次未安裝或執行工具，也未完成依賴漏洞掃描、binary 與原始碼一致性驗證或完整安全稽核。

## 編輯註記

這一節是本專案編輯寫的，不屬於檢視者原文。標示「reviewer claim, not independently established」的，是我們沒有從一手來源查證的說法。

- **閱讀深度不同。** Claude Code 讀了 `herdr.dev/install.sh` 與官方文件，明說沒有逐行審查原始碼。Codex 讀了指定 commit 的 `distribution/install.sh` 與數個原始碼檔。兩者讀的安裝腳本來源不同，內容未必一致。
- **已從一手來源核對：** 指定 commit 的 [`distribution/install.sh`](https://raw.githubusercontent.com/herdrdev/herdr/143bf5fd4781d164260c63fe28e7dc08054ad32f/distribution/install.sh) 從 `https://herdr.dev/latest.json` 取清單並做 SHA-256 比對；[`socket_paths.rs`](https://raw.githubusercontent.com/herdrdev/herdr/143bf5fd4781d164260c63fe28e7dc08054ad32f/src/server/socket_paths.rs) 有 `0o600` 的 socket 權限設定。
- **Claude Code 說「文件裡沒提到任何身分驗證」，Codex 在原始碼找到 `0600`。** 兩者不矛盾：檔案權限擋其他帳號，擋不住同一帳號下的程式。
- **星數與「沒有已公開的安全公告」不是安全背書。** 約 4.5 萬顆星：reviewer claim, not independently established。沒有公告不等於沒有漏洞。
- **「改用 `brew install herdr` 多一層保障」：reviewer claim, not independently established。** Homebrew 換的是發布管道與信任對象，不代表天然更安全。
- **沒有 SECURITY.md：** 我們在指定 commit 的根目錄與 `.github/` 都查不到，與原文一致；但這不排除專案在其他地方提供通報管道。
