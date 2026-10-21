# Private roles, skills and pixel robots

Use `clawdline squad --help`, or this guide's alias `clawdline guide persona`. These operations
use the installed daemon and its versioned Squad API. They never call an AI provider. Creating a
handwritten role does not require a browser, source checkout, Cloud subscription, or reading a token.
The separately labelled AI draft button in the console uses a provider and is not part of these commands.

**Authentication contract.** The CLI reads the installed machine's orchestrator credential inside
its process; it never accepts or prints a token. This build explicitly admits that credential to
GET/POST `/v1/squad/catalog`, GET `/v1/squad/definitions/{id}`, GET/PUT `/v1/squad/settings`, and
GET `/v1/squad/scopes`. It does not grant Agent access to AI drafts, package operations, or Session
telemetry. Do not borrow a browser cookie, paired-person credential, or local-person token. A daemon
that predates this contract returns `403 forbidden`; update it rather than changing credentials.
No credential returns `401 unauthorized`, which does not mean the daemon is offline. Direct API
clients use `X-Clawdline-Orchestrator` within the same local trust boundary; keep credentials in
process memory, never argv, output, or a document. Network/paired devices keep their existing permissions.

**Commands and wire format.** `catalog`, `scopes` and `settings` read JSON. `definition --id ID`
reads a role, optionally `--version VERSION` for its immutable historical definition. Catalog includes
all current skill definitions and their content. `save --body-file FILE --key KEY` posts one API request
`{"kind":"definition|skill|team","expected_version":N,"entity":{"definition":{...}}}` to catalog
(use `skill` or `team` inside `entity` for that kind). `bind --body-file FILE --key KEY` puts one settings
request. `--body-file -` reads stdin. All commands support `--port N` and `--json`; JSON mode retains the
whole typed refusal on stdout and exits 1 on refusal. Usage errors exit 2; successful requests exit 0.
There are no automatic retries. An unknown transport result exits 1: read back or replay the **same
saved file and key** before attempting a new write. A write response proves persistence, not launch.

**Minimal complete example.** Requires Python 3 and a running, updated Clawdline. Choose unused IDs
and keys before the first run; this creates only private records and never installs built-ins.
Run the creation block once; use its persisted JSON files for recovery/replay.

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

**Identity and attribution.** IDs are lowercase dot-separated namespaces such as
`personal.persona.source-editor` and `personal.skill.source-check`; each segment starts with a letter
and contains letters, digits or hyphens. `clawdline.*` and built-in short aliases are read-only.
Supply a nonempty immutable `version`, `source`, `license`, and English `name.en`. Names and summaries
use `en` and `zh-Hant`; role text may use the language needed for the work. Skills also require
`purpose.en` and `content`. Use honest upstream attribution and licences for adapted work;
`UNLICENSED` describes original private work, not permission to copy upstream material.
Roles carry `summary`, `body`, `teams` and `skills`; skills carry `purpose` and `content`.
For the Custom picker, include `{"id":"clawdline.team.custom","version":"1"}` in `teams`.
A role's default skills reference exact skill `id`, `version`, and `enabled` values.

**Icons.** The example's reusable `icon.json` is an 8-column by 7-row multicolor robot: every cell is
a color string or `null` for transparency; `accent` is the main color. Supply this existing format
with every catalog entity; there is no mandatory single-face default in the API. The console's
new-role form starts with a multicolor robot, offers another palette, and lets you paint/erase pixels.
A private role's detail panel offers **Edit robot icon** using the same grid.
To update only an existing role's icon, retain its ID and use a new immutable definition version:

```sh
clawdline squad catalog --json
# Copy the returned catalog_version to N. Keep icon.json and this command for retries.
clawdline squad icon --id personal.persona.source-editor --version 1.0.1 \
  --expected-version N --body-file icon.json --key source-editor-icon-v101 --json
clawdline squad definition --id personal.persona.source-editor --json
clawdline squad settings --json
```

`icon` reads the current definition, preserves its names, body, attribution and pinned team/skill
references, clears the old digest, then posts the new version with the supplied catalog revision.
It does not write handbooks or global/Project bindings. The response includes ID, version, digest,
and the new catalog revision. Read back and compare icon, body and bindings after success.
For an uncertain result, replay before making another edit; after a later definition update, do not
reuse an old icon command blindly: compare the current version and use a fresh key for a new change.
Existing Session snapshots remain immutable; the updated icon/definition applies to the current
picker and future launches. Never change a private role into a public built-in to update its icon.

**Versions, retries and safe updates.** Catalog `expected_version` is the **catalog_version**, not
the entity's immutable version string. A first write also uses the current catalog revision; it is
only zero on a fresh private catalog. The same entity ID with a new version updates the current
entry and retains historical versions. Reusing an immutable version with different bytes is
`definition_version_conflict`. A supplied digest must match; omit it on new content so the daemon
computes it. Do not post an entire catalog back. Settings `expected_version` is the selected role's
`settings_version` from `settings` in that scope (initially 0). A bind patches only named overrides;
missing fields stay unchanged. `skills.value` replaces that role's skill choice list: retain existing
choices when adding one. `present:false` restores inheritance; `present:true` stores the supplied value.
All writes require `Idempotency-Key` (1–200 characters); identical retries replay the stored reply.
A changed payload under that key returns `idempotency_conflict`; stale revisions return
`version_conflict` with `current_version`. Reload and reconcile before using a fresh key, never
blindly overwrite. Other typed refusals include `builtin_read_only`, `unknown_definition`,
`unknown_reference` (missing/repeated team or skill/version), `unknown_project`, `digest_mismatch`,
`bad_request`, `body_too_large`, `capacity_full`, `busy`, and `store_unreadable`/`write_failed`.
Unknown codes or unreadable state are not success. Request limit is 256 KiB, text limit 64 KiB;
use the target daemon's capacity diagnostics for runtime limits.

**Scope and launch.** No scope means global. `scopes` lists known Projects; for Project reads use
`settings --place-id PLACE_ID` or `--scope-id SCOPE_ID`. For binds add exactly one `place_id` or
`scope_id` in the JSON. A new Project scope is resolved using its known place ID; do not invent a
repository scope ID. Project settings override global defaults, and existing unrelated settings stay.
A private role can be manually selected from Custom immediately. `auto_assign` defaults to false
for private roles: manually choosing it is separate from letting a management Session automatically
choose it. To opt in, bind `"auto_assign":{"present":true,"value":true}` with that scope's current
settings_version. Machine automatic dispatch must also be enabled, and the management Session must
have an eligible bound actor; these commands do not turn those controls on. Enabled skill references
and the effective handbook are copied with the exact role definition into each new launch snapshot.
The console's Session role detail reads that immutable snapshot; paired readers may also GET
`/v1/squad/session-snapshots/{conversation}`. This is a paired read, not an Agent credential route.
