# 私人角色、技能與像素機器人

從 `clawdline squad --help` 或 `clawdline guide persona`（本指南別名）開始。
這些指令沿用已安裝 daemon 的版本化 Squad API，不呼叫 AI。不用瀏覽器、原始碼、Cloud 訂閱，也不必自行讀取 token。
主控台另外標示的 AI 草稿按鈕會使用模型供應商，與這些手寫操作分開。

**認證契約。** CLI 在程序內讀取本機 orchestrator 憑證，不接受或印出 token。此版本明確授權該憑證
呼叫 GET/POST `/v1/squad/catalog`、GET `/v1/squad/definitions/{id}`、GET/PUT `/v1/squad/settings`
及 GET `/v1/squad/scopes`。不授權 AI 草稿、套件操作或 Session 遙測。不要借用瀏覽器 cookie、已配對使用者憑證
或 local-person token。舊 daemon 回 `403 forbidden` 時應更新 daemon，不要換憑證繞過。
未帶憑證回 `401 unauthorized`，不代表離線。直接 API 用戶端在相同本機信任邊界內使用
`X-Clawdline-Orchestrator`；憑證只留在程序記憶體，不放 argv、輸出或文件。網路與已配對裝置維持原有權限。

**指令與格式。** `catalog`、`scopes`、`settings` 讀取 JSON；`definition --id ID` 讀取角色，
可加 `--version VERSION` 讀取不可變的歷史版本。目錄包含所有目前技能及內容。
`save --body-file FILE --key KEY` 向目錄 POST 一筆
`{"kind":"definition|skill|team","expected_version":N,"entity":{"definition":{...}}}`；
技能或團隊在 `entity` 內改用 `skill` 或 `team`。`bind --body-file FILE --key KEY` PUT 一筆設定。
`--body-file -` 讀取標準輸入。均支援 `--port N`、`--json`；JSON 模式把完整具型別錯誤保留在 stdout，
被拒絕時退出碼 1，用法錯誤為 2，成功為 0。不會自動重試。傳輸結果未知時退出 1：先讀回或以**同一份已存檔 JSON
及同一 key** 重送，再考慮新寫入。寫入回覆只證明已保存，不代表已啟動。

**最小完整範例。** 需要 Python 3 及已更新、正在運作的 Clawdline。第一次執行前選擇尚未使用的 ID 和 key。
只建立私人資料，不安裝內建角色。建立區塊執行一次；復原或重送使用它保存的 JSON 檔案。

```sh
mkdir -p squad-example
cd squad-example
python3 - <<'PY'
import json, subprocess
from pathlib import Path

def read(*args):
    return json.loads(subprocess.check_output(['clawdline', 'squad', *args, '--json']))

def save(filename, request, command, key):
    Path(filename).write_text(json.dumps(request), encoding='utf-8')
    subprocess.run(['clawdline', 'squad', command, '--body-file', filename,
                    '--key', key, '--json'], check=True)

colors = {'A': '#d97935', 'B': '#365e69', 'C': '#f4d59c'}
rows = ['..AAAA..', '.ABBBBA.', 'ABCBBCBA',
        'ABBBBBBA', 'ABCCCCBA', '.ABBBBA.', '..A..A..']
icon = {'accent': colors['A'], 'cells': [[colors.get(c) for c in row] for row in rows]}
Path('icon.json').write_text(json.dumps(icon), encoding='utf-8')
metadata = {'version': '1.0.0', 'source': 'Original private work', 'license': 'UNLICENSED', 'icon': icon}
skill = {**metadata, 'skill_id': 'personal.skill.source-check',
         'name': {'en': 'Source check', 'zh-Hant': '來源核對'},
         'purpose': {'en': 'Check claims against sources', 'zh-Hant': '核對主張與來源'},
         'content': 'For each factual claim, check its source and report unsupported claims.'}
save('skill.json', {'kind': 'skill', 'expected_version': read('catalog')['catalog_version'],
                   'entity': {'skill': skill}}, 'save', 'example-source-check-v1')
role = {**metadata, 'definition_id': 'personal.persona.source-editor',
        'name': {'en': 'Source editor', 'zh-Hant': '來源主編'},
        'summary': {'en': 'Edit with evidence', 'zh-Hant': '依證據編輯'},
        'body': 'Review the supplied draft. Use the source-check skill. Keep the author voice.',
        'teams': [{'id': 'clawdline.team.custom', 'version': '1'}], 'skills': []}
save('role.json', {'kind': 'definition', 'expected_version': read('catalog')['catalog_version'],
                  'entity': {'definition': role}}, 'save', 'example-source-editor-v1')
save('binding.json', {'definition_id': role['definition_id'], 'expected_version': 0,
                     'overrides': {'skills': {'present': True, 'value': [
                         {'id': skill['skill_id'], 'version': skill['version'], 'enabled': True}]}}},
     'bind', 'example-source-editor-skills-v1')
assert read('definition', '--id', role['definition_id'])['body'] == role['body']
effective = read('settings')
entry = next(p for p in effective['personas'] if p['definition_id'] == role['definition_id'])
assert entry['skills']['value'][0]['id'] == skill['skill_id']
assert entry['skills']['value'][0]['enabled'] is True
assert entry['auto_assign']['value'] is False
print('Private role and enabled skill verified; automatic assignment remains off.')
PY
# Safe replay of exactly the saved request and key:
clawdline squad bind --body-file binding.json --key example-source-editor-skills-v1 --json
clawdline squad definition --id personal.persona.source-editor --json
clawdline squad settings --json
```

**ID 與來源。** 使用小寫、點分隔的命名空間，例如 `personal.persona.source-editor`、
`personal.skill.source-check`；每段以字母開頭，只含字母、數字及連字號。`clawdline.*` 與內建簡稱唯讀。
提供非空的不可變 `version`、`source`、`license`、英文 `name.en`；名稱與摘要使用 `en` 及 `zh-Hant`。
角色本文可使用實際工作所需語言。技能另需 `purpose.en` 及 `content`。改作須正確標示來源及授權；
`UNLICENSED` 適用原創私人內容，不表示可以複製他人內容。角色含 `summary`、`body`、`teams`、`skills`；
技能含 `purpose`、`content`。要出現在「自訂」角色選擇器，`teams` 加入
`{"id":"clawdline.team.custom","version":"1"}`。預設技能引用固定的 `id`、`version`、`enabled`。

**圖示。** 範例保存的 `icon.json` 是 8 欄 × 7 列多色機器人，每格為顏色字串或透明的 `null`，
`accent` 是主色。每個目錄實體都提供此既有格式；API 沒有強制使用同一張臉。
主控台新增角色表單預設多色機器人，可換配色、逐格繪製及清除。私人角色詳情另有「編輯機器人圖示」，使用同一格線。
只更新既有角色圖示時保留 ID，使用新的不可變定義版本：

```sh
clawdline squad catalog --json
# 將回覆的 catalog_version 填入 N；保留 icon.json 與此指令供重試。
clawdline squad icon --id personal.persona.source-editor --version 1.0.1 \
  --expected-version N --body-file icon.json --key source-editor-icon-v101 --json
clawdline squad definition --id personal.persona.source-editor --json
clawdline squad settings --json
```

`icon` 讀取整份目前定義，保留名稱、本文、來源、團隊與技能引用，清除舊 digest，帶著指定的目錄修訂號寫入新版本。
它不寫入手冊或全域／Project 綁定。回覆包含 ID、版本、digest、新目錄修訂號。成功後讀回比對圖示、本文及綁定。
結果未知時應先重送，再做其他編輯；若之後已有新定義，勿盲目重送舊圖示指令，先核對版本，新變更使用新 key。
既有 Session 啟動快照維持不可變；新圖示與定義用於目前選擇器及未來啟動。不要把私人角色改成公開內建角色。

**版本、重試與安全更新。** 目錄的 `expected_version` 是 `catalog_version`，不是實體版本字串。
首次寫入也使用目前目錄修訂號，只有全新私人目錄才是 0。同一 ID 搭配新版本更新目前項目並保存歷史版本。
同一不可變版本放不同內容回 `definition_version_conflict`；若提供 digest 必須正確，新內容省略 digest 由 daemon 計算。
不要把整份目錄寫回。設定的 `expected_version` 是該範圍角色的 `settings_version`（初始 0）。
綁定只修改具名欄位，未提供的欄位保留。`skills.value` 取代該角色的技能選擇清單，新增時須保留既有項目。
`present:false` 恢復繼承，`present:true` 保存指定值。
每筆寫入必須帶 1–200 字元 `Idempotency-Key`。同一內容重送重播收據；同 key 換內容回 `idempotency_conflict`。
過期修訂號回 `version_conflict` 與 `current_version`，重新讀取並整合後才用新 key，不盲目覆寫。
其他具型別錯誤包括 `builtin_read_only`、`unknown_definition`、`unknown_reference`（團隊／技能／版本不存在或重複）、
`unknown_project`、`digest_mismatch`、`bad_request`、`body_too_large`、`capacity_full`、`busy`、
`store_unreadable`／`write_failed`。未知錯誤或讀不到狀態都不算成功。請求上限 256 KiB、本文上限 64 KiB；
實際上限以目標 daemon 的容量診斷為準。

**範圍與啟動。** 不帶範圍代表全域。`scopes` 列出已知 Project；讀取用 `settings --place-id PLACE_ID`
或 `--scope-id SCOPE_ID`，綁定則在 JSON 加入其中一個 `place_id` 或 `scope_id`。
新 Project 範圍以已知 place ID 解析，不自行編造 repository scope ID。Project 設定覆蓋全域預設，不影響其他設定。
私人角色建立後可立即從「自訂」手動選取；其 `auto_assign` 預設 false，與允許管理 Session 自動挑選是兩回事。
若要加入自動指派，帶目前範圍 settings_version 綁定 `"auto_assign":{"present":true,"value":true}`。
機器自動派工也必須開啟，管理 Session 必須具備合資格的綁定 actor；本組指令不替你開啟這些控制。
每次新啟動會把精確角色定義、有效手冊及啟用技能複製進不可變快照；主控台 Session 角色詳情讀取該快照。
已配對讀者也可 GET `/v1/squad/session-snapshots/{conversation}`，這是配對讀取，不是 Agent 憑證路由。
