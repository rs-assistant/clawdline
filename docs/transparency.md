# What Clawdline changes

The Console's **What Clawdline changes** page (`#page=transparency`) explains the instructions, files and launch parameters Clawdline adds. The sidebar and Settings → Workflow link to it. It uses the existing English and Taiwan Traditional Chinese catalogs, with English fallback for other interface languages. Original instruction excerpts stay in their source language.

The page is a description, not an inspection of a person's configuration. It sends no commands and makes no additional health poll. It shows the console's compiled source commit and the selected machine version from the existing health subscription. Different machine versions and personal configuration may behave differently. Source links point to the compiled commit; source archives without Git show an unconfirmed description version instead of inventing one.

## Content ownership

`web/console/src/pages/transparency/catalog.json` owns stable entry IDs, filters, runtime locations, original excerpts, source locations and review receipts. Human copy lives under `transparency.*` in both `public/catalogs/en.json` and `zh-Hant.json`. The page is lazy-loaded so the instruction excerpts do not enter the initial Console bundle.

Separate loading from applicability. In normal Clawdline-launched Codex Sessions, Board language and the Note rule are startup defaults; language falls through the configured language, machine, bundled language and `zh-Hant`. Claude's language and existing Project memory are automatic startup decisions. Roles, Skills, Board assignments, todos, notices and handoffs have their own triggers. Reading a completion rule in a guide does not execute the completion action or reinject the guide every turn.

## Updating an instruction or entry

1. Change the implementation and update the matching entry, original excerpt, runtime path and source location. Add an entry for a new behavior; do not squeeze a new trigger into an unrelated existing card.
2. Review both English and Traditional Chinese, including loading behavior, agent, applicability and version scope. Existing user configuration and provider-native rules must not be described as Clawdline-installed defaults.
3. Run `node tools/refresh-transparency.mjs` only after this review. It refreshes fingerprints of the registered producers, guides and both language copies; it does not write or translate explanations.
4. Run `tools/check.sh`. Its unconditional `internal/config` repository guard refuses stale sources, missing source registrations, empty or missing card translations, and unreviewed copy changes. The Go AST scan also discovers new producers using known provider injection flags anywhere under `internal/` and `cmd/`.
5. Run the filter tests and review desktop/mobile layout for a user-facing change. Hosted deployment follows [hosted-console.md](hosted-console.md).

The inventory explicitly registers task-message composers, role/memory loaders and guide sources. The provider-sink scan catches the known `developer_instructions` and `--append-system-prompt` mechanisms, not arbitrary future protocols. A new transport or injection mechanism must register its producer and extend that discovery rule in the same change. A fingerprint requires human review; it does not prove a translation's meaning or automatically maintain prose.

To prove the freshness guard rejects drift, change one registered source in an isolated worktree without refreshing the receipt, run the focused repository guard, and retain its failing result. Restore the task-owned experiment before the passing commit gate. A new injection mechanism should also have a case proving its producer cannot remain unregistered.

This is a descriptive page. New permission switches, configuration scans, Cloud API changes and provider policy changes are separate work.
