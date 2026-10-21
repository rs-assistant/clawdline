# Clawdline：兩份 Agent 檢視

[English](./clawdline.md)・[索引](./README.zh-TW.md)・[方法](./methodology.zh-TW.md)

- 檢視對象：[`sainteye/clawdline@31139d6`](https://github.com/sainteye/clawdline/tree/31139d647cd821b70a875ec16455adf05a836deb)
- 日期：2026-10-21
- 問題：「要安裝這個工具有無資安風險？」
- 檢視者：Claude Code、Codex，各自在新的 session 中只用網路讀取作答

以下先是兩份檢視的原文，再是編輯註記。原文是檢視者的看法，不是本專案的結論。

**編輯範圍：** 原文逐段保留；只把原文的標題降為四級標題，並刪除結尾一行與比較無關的「Fixed but not yet released」狀態。其他字句未改。

## Claude Code 的原文

#### 結論

我看過 `install.sh` 的兩個版本後，沒發現惡意或可疑的行為。安裝流程的安全設計做得不錯。真正要評估的風險在工具本身的權限：它能遠端操控你的 Claude Code／Codex session，而這部分的程式碼我沒有審查。

#### 安裝腳本：風險低

- **沒有 sudo**：全部以目前使用者身分執行，`umask 077`，檔案裝在 `~/.local/share/clawdline-next` 和 `~/.config/clawdline-next`。
- **有簽章驗證**：腳本內寫死一把 Ed25519 公鑰（`2Du3yZ4j…/O0=`），不會從下載來源或環境變數取得金鑰。它用本機 OpenSSL 3 驗證 `manifest.json` 的簽章，再檢查壓縮檔的大小和 SHA-256，全部通過才解壓、執行。
- **只允許 HTTPS**：拒絕 HTTPS 降級的重新導向，只有 localhost 可以用 HTTP。
- **沒有 `eval`、沒有遙測**：腳本本身不建立服務，也不開 port。
- README 建議的做法是指定 commit 下載腳本、讀過再用 `--version v2.0.0` 執行，這比直接 `curl | sh` 安全。v2.0.0 release 確實存在，標記為 Latest。

#### 需要注意的地方

1. **README 指向另一個 commit 的腳本**：你給的是 `31139d6`，但這個 commit 的 README 叫你下載 `74fc07e` 的 `install.sh`。我比對過，兩版的公鑰、驗證和解壓邏輯一樣，但你應該清楚自己實際執行的是哪一份。
2. **信任最終落在簽章私鑰上**：簽章只能證明檔案出自持有私鑰的人。私鑰外洩，或維護者本身不可信，這套驗證都擋不住。目前只有一把金鑰，看不到輪替紀錄。
3. **實際動作由 binary 執行**：腳本最後會跑 `clawdline setup`，由這個 Go 程式建立 launchd/systemd 常駐服務、啟動 daemon、開 port（範例用 7800）。這部分我**沒有審查**。
4. **工具本身權限很大**，這才是主要風險：
   - 它能讀取所有 session 的對話內容，也能把文字輸入進終端機。等於任何能連上這個 daemon 的人，都能透過你的 AI agent 在你的電腦上執行指令。
   - README 說預設只聽 loopback。但本機其他程式，或瀏覽器網頁透過 CSRF／DNS rebinding，都可能連到 localhost 的服務。要確認它有沒有做認證或 token 驗證。
   - 手機遙控和 Cloud 功能都是可選的，README 宣稱端對端加密。開啟後，攻擊面會延伸到你的手機和帳號，WebRTC 直連也一樣。
5. **小問題**：`--purge` 會對 `CLAWDLINE_NEXT_DIR` 執行 `rm -rf`。這個變數設錯的話，會刪掉不該刪的資料。

#### 建議

- 照 README 的方式下載，讀過腳本再加 `--version v2.0.0` 執行，不要用 `curl | sh`。
- 用不到的話先別開 Cloud 和手機功能，只在本機使用。
- 裝完用 `lsof -iTCP -sTCP:LISTEN | grep -i clawd` 確認它只綁 `127.0.0.1`。
- 想要更放心，可以讓我接著審查 `internal/` 裡 daemon 的認證和監聽邏輯，以及 `cmd/clawdline` 的 setup 流程。

## Codex 的原文

**有資安風險，應把它當成「能讀取開發資料、操作終端機的常駐工具」評估。** 我檢查了指定 commit 的安裝腳本、HTTP 權限閘門及相關文件；有實際防護，但這次檢查不足以保證沒有漏洞。

最值得注意的是這五點：

| 項目 | 判斷 |
|---|---|
| **安裝供應鏈** | 安裝器會先用內建公鑰驗證 Ed25519 簽章，再檢查下載檔大小與 SHA-256，通過後才解壓、執行。這能防下載檔被替換，但無法防發布者或簽章私鑰遭入侵。[安裝器原始碼](https://github.com/sainteye/clawdline/blob/31139d647cd821b70a875ec16455adf05a836deb/install.sh) |
| **資料與操作權限** | 能讀 Claude／Codex 對話、終端畫面及功能所需的專案檔案，也能透過終端或 Agent 執行指令。因此對話裡的金鑰、程式碼和客戶資料都在風險範圍內。[資料與權限說明](https://github.com/sainteye/clawdline/blob/31139d647cd821b70a875ec16455adf05a836deb/README.md#security-and-your-data) |
| **常駐執行** | 不需 `sudo`，但預設建立使用者背景服務；Linux 還會嘗試啟用登出後持續執行。沒有管理員權限，仍可能影響該帳號可存取的資料。[安裝行為](https://github.com/sainteye/clawdline/blob/31139d647cd821b70a875ec16455adf05a836deb/docs/user/install.md) |
| **本機介面** | 預設只監聽 loopback；程式有憑證、來源與寫入權限檢查，降低惡意網頁呼叫本機服務的風險。但尚未實測這些保護是否能被繞過。[權限閘門程式碼](https://github.com/sainteye/clawdline/blob/31139d647cd821b70a875ec16455adf05a836deb/internal/transport/http/gate.go) |
| **Cloud 遠端控制** | 預設關閉；啟用後採內容加密，但已配對瀏覽器及供應給它的網頁程式仍需信任。公開倉庫也無法驗證私有雲端服務的實際部署、保存政策與營運安全。[Cloud 信任邊界](https://github.com/sainteye/clawdline/blob/31139d647cd821b70a875ec16455adf05a836deb/docs/cloud.md) |

**另有一個版本陷阱：**你貼的 commit 是 `31139d6…`，但其中 README 指定下載 `74fc07e…` 的安裝器，安裝 `v2.0.0`；文件標示該發行版原始碼是 `d2ed0c0…`。所以，**看過這個連結的程式碼，不等於審查過實際安裝的程式**；簽章也不等於二進位檔已被證明與原始碼一致。[版本對應說明](https://github.com/sainteye/clawdline/blob/31139d647cd821b70a875ec16455adf05a836deb/docs/user/install.md#review-and-pin-an-installation)

我的建議是：**一般個人開發可先在獨立帳號或測試機試用；持有正式環境憑證、公司機密或客戶資料的主力機，先不要直接裝。** 初次試用保持 Cloud 關閉、使用 `--no-autostart`，並照文件固定安裝器與發行版、先下載閱讀再執行。Cloud 關閉仍可能有更新檢查連線。

本次沒有安裝或執行它，也未完成依賴漏洞掃描、滲透測試或發行檔重現建置驗證。

## 編輯註記

這一節是本專案編輯寫的，不屬於檢視者原文。標示「reviewer claim, not independently established」的，是我們沒有從一手來源查證的說法。

- **閱讀深度不同。** Claude Code 讀了兩個版本的 `install.sh`，明說沒有審查 daemon 與 `clawdline setup`。Codex 讀了安裝腳本、`internal/transport/http/gate.go` 與文件。兩份都沒有安裝或執行。
- **已從一手來源核對：** 指定 commit 的 [`install.sh`](https://github.com/sainteye/clawdline/blob/31139d647cd821b70a875ec16455adf05a836deb/install.sh) 寫死一把 Ed25519 公鑰（`TRUSTED_KEYS`），設定 `umask 077`；該 commit 的 README 確實指向 `74fc07e` 的安裝腳本與 `v2.0.0`。
- **`--purge` 的適用情境。** 在指定版本中，只有執行 `--uninstall --purge`、且安裝位置已沒有可執行的 binary 時，腳本才走 `purge_state` 備援路徑，對 `CLAWDLINE_NEXT_DIR`（未設定時為 XDG 設定目錄下的 `clawdline-next`）執行刪除。這是自訂目錄設錯的刪除風險，不是首次安裝會刪除該目錄的證據。
- **「沒有遙測」只指安裝腳本。** 原文寫的是「腳本本身」；Codex 另外指出 Cloud 關閉時仍可能有更新檢查連線。
- **連得上不等於獲得授權。** Claude 的「任何能連上 daemon 的人都能操作」省略了認證條件；同版本的 [gate](https://github.com/sainteye/clawdline/blob/31139d647cd821b70a875ec16455adf05a836deb/internal/transport/http/gate.go) 有憑證與操作權限檢查。這句應理解為取得足夠權限後的能力，不能當成匿名遠端執行漏洞。
- **CSRF／DNS rebinding 是檢視者提出的待查問題，不是已證實的漏洞。** Codex 讀到 gate 有憑證與來源檢查，但同樣說尚未實測能否繞過。
- **這是第一輪基準。** 這份基準已包含首次安裝前驗簽與固定版本安裝文件；後續安全工程仍在進行。產品所需的讀取與操作能力仍是持續的信任邊界，不能承諾改完後消失。改善落到公開 GitHub 後會用新的 SHA 增補報告，見[更新紀錄](./README.zh-TW.md#更新紀錄)。
