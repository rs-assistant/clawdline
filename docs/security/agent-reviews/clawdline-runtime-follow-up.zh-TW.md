# Clawdline 啟用換鑰後的公開檢視

[English](./clawdline-runtime-follow-up.md)・[四專案對照](./README.zh-TW.md)・[前次後續檢視](./clawdline-follow-up.zh-TW.md)

- 受檢版本：[`sainteye/clawdline@e6c974b00`](https://github.com/sainteye/clawdline/tree/e6c974b00dc75cf45953d723a2c8601a83203c3d)
- 日期：2026-10-21；第三輪，僅重跑 Clawdline。
- 提示只有固定 commit 的 GitHub URL，以及「要安裝這個工具有無資安風險？」。
- Claude Code、Codex 各自使用全新 CLI Session，只能讀公開網路；沒有本機原始碼、先前對話或開發者身分資訊。

以下保留完整原文，僅將 Claude 標題降為第四層，並移除 Codex 無關的結尾交付狀態行。英文頁是編輯翻譯。兩位都未安裝執行或驗證發布二進位檔；原始碼啟用與 dev 安裝不代表公開發行版驗收。

## Claude Code 原文

#### 結論：中低風險，前提是照建議方式安裝、預設只在本機使用

安裝流程本身設計得算謹慎。真正的風險在這個工具的用途：它能讀取你所有 Claude Code/Codex 的對話和終端機，配對成「讀取＋操作」的裝置還能在你的機器上執行程式。

我讀了 `install.sh`、`SECURITY.md`、`docs/security/`、`docs/user/remote-access.md`，以及 `internal/transport/http/auth.go`。工具是摘要式讀取，不是逐行完整稽核。核心的 `gate.go`（loopback 判定、Origin/Host 檢查、token 比對）**沒有看到**，以下判斷有這個限制。

#### 安裝流程（install.sh）
做得好的地方：
- 全程用一般使用者權限，**沒有用 sudo**，檔案只寫進 `~/.local/share/clawdline-next/` 和 `~/.config/clawdline-next/`。
- release manifest 用寫死在腳本裡的 **Ed25519 公鑰**，由本機 OpenSSL 驗章。之後再核對壓縮檔的大小和 SHA-256，**驗證通過才會解壓、執行**。
- 沒有 `eval`，安裝腳本裡看不到遙測。

要注意的地方：
- **一定要加 `--version v2.0.0`。** 不指定版本會抓 `releases/latest`。腳本也沒有防降版（rollback）機制，舊版只要有合法簽章一樣會被接受。
- 腳本本身沒有簽章，只靠 GitHub 的 TLS。建議照 README 的做法：用固定 commit 的 raw URL 下載，**先看過再執行**，不要直接 `curl | sh`。
- 只有一把簽章金鑰，沒有撤銷機制。金鑰外洩的話，信任鏈就失效了。
- 安裝最後會交給 `clawdline setup` 處理（建立常駐服務等），這部分我沒有審查。

#### 執行時
- **預設只聽 `127.0.0.1`**（文件寫 port 7727），不會對區網開 port。
- **不開 Cloud 時仍會對外連線：**
  - 向 `app.clawdline.com/BUILD.json` 比對版本（對方會看到你的 IP）
  - 向 GitHub 檢查更新
  - 向 Cloudflare、Google 的 STUN 伺服器連線（會暴露你的網路位址）
  
  這些都不包含對話內容。
- **Cloud 預設關閉。** 開啟後內容是端對端加密，但帳號、裝置、session ID、時間、流量大小這些中繼資料，服務端看得到。目前仍是 preview。
- **配對：** 6 位數碼、2 分鐘內有效，每 10 分鐘最多 3 次配對請求，錯 5 次就失效。
- 從 `auth.go` 看到幾個值得留意的設計：
  - session cookie 效期一年
  - 瀏覽器配對沒帶權限參數時，**預設是讀取＋操作**，不是唯讀
  - `/v1/auth/*` 這幾個路由不需要 token 就能呼叫，安全性全靠 `gate.go` 和各 handler 自己檢查

#### 建議做法
1. 下載固定 commit 的 `install.sh`，看過後用 `--version v2.0.0` 執行。
2. 先只在本機用，不開 Cloud。
3. 需要遠端時，優先用 SSH port forward 或 Tailscale **Serve**。**絕對不要用 Tailscale Funnel**，它會把服務公開到整個網際網路。
4. 配對手機時預設給「唯讀」，真的需要再升級權限。不用的裝置記得撤銷。
5. 如果要用在含敏感程式碼或憑證的機器上，建議再請人審 `internal/transport/http/gate.go` 和 `setup` 建立服務的部分。

## Codex 原文

**有資安風險，但目前查到的資料不足以認定它是惡意軟體，或已有可利用的漏洞。** 我會把 Clawdline 視為能讀取敏感資料、並以你帳號操作電腦的工具，安裝前需要信任發布者。

針對你指定的 `e6c974b…`，主要有這幾點：

| 項目 | 評估 |
|---|---|
| 安裝流程 | 有實質保護：安裝器先用內建公鑰驗證 Ed25519 簽章，再比對壓縮檔大小與 SHA-256，成功後才解壓及執行。這能防止未持有簽章金鑰的人替換下載內容，但不能證明程式本身安全。[安裝器原始碼](https://github.com/sainteye/clawdline/blob/e6c974b00dc75cf45953d723a2c8601a83203c3d/install.sh) |
| 敏感資料 | 它會讀取 Claude／Codex 對話、終端畫面及功能需要的專案檔案；其中的密碼、API key 或客戶資料可能出現在介面中。[架構文件](https://github.com/sainteye/clawdline/blob/e6c974b00dc75cf45953d723a2c8601a83203c3d/docs/architecture.md) |
| 遠端控制 | 有操作權的配對裝置能傳指令、輸入終端機及啟閉 Session，實際上可能執行程式。**未另行指定的新配對預設可操作**；因此手機或瀏覽器失陷也可能波及電腦。[權限說明](https://github.com/sainteye/clawdline/blob/e6c974b00dc75cf45953d723a2c8601a83203c3d/docs/user/remote-access.md) |
| Cloud | 預設關閉。文件描述端對端加密，但配對瀏覽器與載入的網頁程式仍在信任範圍內；私有後端及正式部署無法從這份公開原始碼確認。[Cloud 信任邊界](https://github.com/sainteye/clawdline/blob/e6c974b00dc75cf45953d723a2c8601a83203c3d/docs/cloud.md) |

**另外，照 README 安裝，不代表裝到你貼的 commit。** README 固定的是另一版安裝器 `74fc07e…` 與發行版 `v2.0.0`；必須另查簽署 manifest 的來源 commit，才能確認實際執行版本包含哪些保護。[版本與發布說明](https://github.com/sainteye/clawdline/blob/e6c974b00dc75cf45953d723a2c8601a83203c3d/docs/releasing.md)

我的建議：

- 先在沒有正式環境憑證的獨立使用者帳號或測試機試用。
- 下載並閱讀固定版本安裝器，指定發行版，避免直接 `curl | sh`。
- 不需要遠端功能就保持 Cloud 關閉；僅查看的裝置明確設成唯讀。
- 不要把登入／配對連結分享出去，也不要直接對公網暴露服務。

這次是公開原始碼與文件的初步檢視，包含安裝器及部分認證入口；沒有安裝執行、完整漏洞測試，或核對發布二進位檔。因此我的判斷是：**可以隔離試用，但還不足以直接放心部署到存有重要憑證的工作主機。**

## 編輯核對與後續工作

以下由專案編輯撰寫，不是檢視者的意見；上方原文沒有改寫。

- **兩位都有讀到加密。** 都描述 Cloud 端對端加密；提醒的是本機明文、合法端點、中繼資料與發布者／瀏覽器信任，未報告觀察到對話明文上傳 Cloud。
- **STUN 的分類需要更正。** Claude 把 STUN 列在「Cloud 關閉仍會外連」之下；但受檢版本的 [`Open`](https://github.com/sainteye/clawdline/blob/e6c974b00dc75cf45953d723a2c8601a83203c3d/internal/transport/cloud/link.go) 在 Cloud 關閉時先回傳，`Run` 也拒絕啟動；[`direct.go`](https://github.com/sainteye/clawdline/blob/e6c974b00dc75cf45953d723a2c8601a83203c3d/internal/transport/cloud/direct.go) 的 STUN 設定用於 Cloud 直接連線協商。這份程式碼不支持「關閉 Cloud 仍走此 STUN 路徑」的說法；更新檢查是另一條外連路徑。此為原始碼核對，不是完整封包實測。
- **未讀到，不等於已證實缺少控制。** Claude 明說沒讀 `gate.go` 與 `setup`；Codex 也未完整驗證程式與二進位檔。這些限制支持繼續檢查，不能直接推論控制不存在。
- **安裝器內容一致。** 從公開 GitHub 取得的 `74fc07e…:install.sh` 與受檢 `e6c974b…:install.sh` 完全相同。固定驗證器是刻意設計；它仍與簽署 manifest 選定的程式版本分開。
- **版本提醒成立。** 這輪檢視時，README 安裝的是 `v2.0.0`，不是新主線。原始碼啟用與健康的 dev 版本不會改變公開下載；發布驗收與之後的安裝版本更新仍須完成。
- **保留下來的能力是信任邊界。** 預設可操作、本機敏感資料、簽署發布者與 hosted JavaScript 的信任，不會因加密或撤銷改善而消失。兩份回覆沒有證實新的可利用缺陷，也沒有證明完全無漏洞。
- **這不是統計上的偏誤結論。** 八份基準加上四份 Clawdline 後續回覆，呈現保守的能力／信任提醒，但結論與閱讀深度不同；不能證明偏誤，也不能替專案排名。我們不會反覆提示，要求檢視者改說「零風險」。

四專案原始基準與前次後續檢視均保留。資料範圍與信任邊界請見[安全性說明](https://clawdline.com/security/)。
