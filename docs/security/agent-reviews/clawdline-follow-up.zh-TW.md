# Clawdline 後續公開檢視

[English](./clawdline-follow-up.md)・[四個專案的對照](./README.zh-TW.md)・[第一輪基準](./clawdline.zh-TW.md)

- 檢視版本：[`sainteye/clawdline@dacb02ac1`](https://github.com/sainteye/clawdline/tree/dacb02ac15b673c48734c46fa2a777f698872a8c)
- 日期：2026-10-21
- 題目：「要安裝這個工具有無資安風險？」
- Claude Code 與 Codex 各自從全新 CLI 工作階段執行，只開放公開網路工具，沒有本機原始碼、既有對話或開發者身分背景。

以下保留兩位的原始回覆，後面另列編輯註記。只調整 Claude 的標題層級，並移除 Codex 最後一行與檢視內容無關的交付狀態字串；英文頁為編輯翻譯。這是主線修正後的公開初評，不是本波工程或正式發版的完成驗收。

## Claude Code（原文）

#### 結論

這個安裝程式設計得相當謹慎，看起來不是惡意程式。不過它沒有做到「零風險」。主要風險不在安裝過程，而在工具本身：它是一個可以遠端操作 Claude Code／Codex Session 的服務，而那些 Session 能在你的機器上執行程式。

**我查了哪些東西：** `dacb02a` 版的 `install.sh`、README、`SECURITY.md`，以及 `docs/` 裡跟資安、遠端存取、專案檔案相關的文件。我沒有審查 Go 原始碼，也沒有驗證發佈的執行檔是不是由這份原始碼編譯出來的。

#### 安裝程式的優點

- **不需要 sudo**：檔案只寫進 `~/.local/share/clawdline-next` 和 `~/.config/clawdline-next`，而且設了 `umask 077`。
- **有簽章驗證**：程式裡寫死一把 Ed25519 公鑰，用本機的 OpenSSL 3 驗證 manifest 簽章。驗過之後才解析 manifest，再比對壓縮檔的大小和 SHA-256，全部通過才解壓、執行。
- **限制連線方式**：只接受 HTTPS，拒絕降級成 HTTP 的重新導向，也有大小上限和逾時。
- 沒有用 `eval`。內嵌的 Python 驗證程式用 `python3 -I` 執行，可以避免載入當前目錄裡被植入的模組。

#### 需要注意的地方

1. **README 指向的不是你看的這版。** README 要你下載 commit `74fc07e` 的 `install.sh`，不是 `dacb02a`。要審查，就要看你實際會執行的那一份。
2. **簽章只能證明來源，不能證明安全。** 驗證通過，只代表執行檔是持有私鑰的人簽的。最後執行的是預先編譯好的 `clawdline setup`。它會不會建立 launchd 或 systemd 服務、實際做了什麼，從安裝程式看不出來。
3. **新配對的裝置預設可以操作。** 文件寫明，新配對的裝置預設是「讀取與操作」權限，可以傳訊息、在終端機打字、開關 Session，等於能在你的機器上跑程式。如果只想從手機看，建議配對時改成唯讀。
4. **登入網址等同密碼。** `clawdline open` 產生的網址裡帶著金鑰，裝置登入後會一直有效，直到你撤銷為止。不要貼到聊天室，也不要留在共用的瀏覽器裡。
5. **遠端派工。** 配對好的另一台機器，可以在你的機器上開 Session，讓 Agent 去執行任務。文件沒有提到每次派工需要你確認，控管靠的是配對、本機的寫入開關和撤銷。
6. **Cloud 還在預覽階段。** 預設關閉，宣稱端對端加密。`SECURITY.md` 也坦白說，支援的版本「不代表每個部分都已經審查過」。
7. **還是有對外連線。** 就算不開 Cloud，更新檢查還是會連到 GitHub 或官網。
8. **Unify 功能會改專案檔。** 它會修改 `CLAUDE.md`、`AGENTS.md`，搬動 `.claude/skills`。沒有內建復原功能，請在用 git 管理的專案裡使用。
9. **環境變數。** `CLAWDLINE_INSTALL_BASE_URL` 和 `CLAWDLINE_INSTALL_OPENSSL` 可以改掉下載來源和驗證工具。安裝前確認這兩個變數沒有被設定。

#### 建議

- 照 README 的方式：先下載安裝程式、讀過，再加 `--version v2.0.0` 執行。不要用 `curl | sh`。
- Cloud 不需要就保持關閉。遠端存取用 SSH port forward，或 Tailscale Serve，絕對不要用 `tailscale funnel`。
- 只想用手機看的裝置，一律設成唯讀。
- 如果要放在有敏感程式碼或憑證的機器上，最好先審查 `internal/transport/http/auth.go` 和 `internal/domain/auth/`，或自己從原始碼編譯。

需要的話，我可以接著審查 `74fc07e` 版的安裝程式，或 Go 的認證程式碼。

## Codex（原文）

**有資安風險，但目前查閱的內容不足以認定它是惡意軟體，或有已證實可利用的漏洞。** 它能讀取 AI 對話、查看終端並操作工作階段，應以具備你帳號操作能力的工具看待。

針對你指定的 `dacb02ac…` 版本，重點如下：

| 面向 | 判斷 |
|---|---|
| 安裝流程 | 有實質防護：安裝腳本先以內建公鑰驗證 Ed25519 簽章，再核對壓縮檔大小及 SHA-256，通過後才解壓、執行。但仍須信任腳本、發布者與簽章私鑰。 |
| 本機資料 | 能讀取 Claude／Codex 對話紀錄、終端畫面及功能需要的專案檔案；其中若有密碼、金鑰或客戶資料，也可能出現在介面。 |
| 遠端操作 | 有操作權限的已配對裝置可以送出指令、操作終端。裝置或瀏覽器遭入侵，可能連帶危及你的電腦。 |
| Cloud | 預設關閉；啟用後文件描述採端對端加密，但服務仍能看見部分連線中繼資料，且公開原始碼無法驗證私有後端的實際部署。 |

依據：[安裝腳本](https://github.com/sainteye/clawdline/blob/dacb02ac15b673c48734c46fa2a777f698872a8c/install.sh)、[資料與權限說明](https://github.com/sainteye/clawdline/blob/dacb02ac15b673c48734c46fa2a777f698872a8c/README.md)、[遠端權限](https://github.com/sainteye/clawdline/blob/dacb02ac15b673c48734c46fa2a777f698872a8c/docs/user/remote-access.md)、[Cloud 信任邊界](https://github.com/sainteye/clawdline/blob/dacb02ac15b673c48734c46fa2a777f698872a8c/docs/cloud.md)。

**另一個要注意的地方：你給的 commit，不等於照 README 安裝到的程式。** README 指定另一個版本的安裝腳本及 `v2.0.0` 發行版，因此不能假設這份原始碼中的所有防護都已包含在下載的執行檔中。[版本區分說明](https://github.com/sainteye/clawdline/blob/dacb02ac15b673c48734c46fa2a777f698872a8c/docs/releasing.md)

我的建議是：

- 初次試用用獨立 OS 帳號或虛擬機，先放測試專案。
- 先保持 Cloud 關閉；需要遠端查看時，明確選擇唯讀權限。
- 固定安裝版本，下載並閱讀安裝腳本後才執行。
- 有正式環境憑證或敏感客戶資料的主力帳號，先不要直接裝。

這次是**安裝腳本與文件的靜態初評**，沒有執行安裝、完整審查程式碼、掃描依賴或核驗發行二進位，不能當作完整安全稽核。

## 編輯註記

本節由專案編輯撰寫，不是兩位檢視者的結論。

- **兩位都有讀到加密說明。** Claude 寫 Cloud「宣稱端對端加密」；Codex 明確描述內容加密與仍可見的中繼資料。他們的提醒涉及本機明文、授權端點與信任，並沒有認定 Clawdline 把對話明文上傳 Cloud；兩位也未獨立驗證正式後端。
- **這次仍是腳本與文件初評。** Claude 明說未審查 Go 原始碼；Codex 明說未完整審碼、掃描依賴或核驗二進位。兩位未報告新的可利用漏洞，不能解讀成完整程式沒有漏洞。
- **兩位都辨識到執行前驗簽。** 這項保護在第一輪基準已存在，不能把這次較正面的措辭當成新修正帶來的因果證據。
- **不同腳本 commit 不等於不同腳本內容。** 我們比較了 README 固定的 `74fc07e5bc8c8d092f216478f26d99b7ed1b77ee:install.sh` 與本次 `dacb02ac1:install.sh`，位元組相同。固定安裝腳本版本是刻意設計；它與被安裝程式的版本仍然是兩件事。
- **版本差異提醒成立。** README 固定安裝 `v2.0.0`；主線新增的裝置權限與換鑰工程不能據此宣稱已包含在那個發行版。尤其受檢 commit 的內容換鑰 runtime 與 viewer 啟動當時刻意關閉，等待跨版本與部署驗收；之後的原始碼與部署更新不改變這份歷史檢視。參見[版本與安裝說明](../../user/install.md#review-and-pin-an-installation)、[runtime 範圍](../content-key-runtime.md)與[viewer 範圍](../viewer-rekey.md)。
- **有權限的工具仍有信任邊界。** 讀取對話、更新檢查、授權後遠端操作、對發布者與瀏覽器的信任，不會因修正安裝或撤銷流程而消失。這些能力本身不能當成未授權存取或資料外洩的證據。
- **不是統計上的「agent 慣性」證明。** 四專案、八份基準加兩份後續回答，說明這種問題常會得到保守的能力與信任提醒；樣本、閱讀深度與版本不同，不能證明偏誤或安全排名，也不能忽略真正的缺陷。

第一輪與 Herdr、T3 Code、Orca 的原始檢視保留不動；本輪只重新檢視 Clawdline，不能拿不同輪次的措辭替四個專案排名。對資料範圍與 Cloud 能看見的內容，請看[安全性說明](https://clawdline.com/security/)。
