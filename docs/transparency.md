# What Clawdline changes

The public **What Clawdline changes** page lives at `https://clawdline.com/docs/transparency/`. It needs no Cloud account or paired machine. The Console sidebar and Settings → Workflow link there; old `#page=transparency` links forward their supported filters and interface language.

The page describes a reviewed product revision, not a person's configuration. English and Taiwan Traditional Chinese share the website's language control. Original excerpts stay in their source language. Source links point to the imported product commit; the website build refuses an outdated content snapshot.

## Content ownership

`web/console/src/pages/transparency/catalog.json` owns stable entry IDs, filters, runtime locations, original excerpts, source locations and review receipts. Human copy lives under `transparency.*` in both `public/catalogs/en.json` and `zh-Hant.json`. The website imports this inventory, both language copies and filter logic at an exact public product commit using its `scripts/sync-transparency.mjs`. Its build compares the imported content with current product main. No instruction excerpts are shipped in the Console bundle.

Separate loading from applicability. In normal Clawdline-launched Codex Sessions, Board language and the Note rule are startup defaults; language falls through the configured language, machine, bundled language and `zh-Hant`. Claude's language and existing Project memory are automatic startup decisions. Roles, Skills, Board assignments, todos, notices and handoffs have their own triggers. Reading a completion rule in a guide does not execute the completion action or reinject the guide every turn.

## Updating an instruction or entry

1. Change the implementation and update the matching entry, original excerpt, runtime path and source location. Add an entry for a new behavior; do not squeeze a new trigger into an unrelated existing card.
2. Review both English and Traditional Chinese, including loading behavior, agent, applicability and version scope. Existing user configuration and provider-native rules must not be described as Clawdline-installed defaults.
3. Run `node tools/refresh-transparency.mjs` only after this review. It refreshes fingerprints of the registered producers, guides and both language copies; it does not write or translate explanations.
4. Run `tools/check.sh`. Its unconditional `internal/config` repository guard refuses stale sources, missing source registrations, empty or missing card translations, and unreviewed copy changes. The Go AST scan also discovers new producers using known provider injection flags anywhere under `internal/` and `cmd/`.
5. Run the filter tests and review desktop/mobile layout for a user-facing change. Publish the reviewed product change, run the website sync command, and deploy clawdline.com. Cloud entry changes separately follow [hosted-console.md](hosted-console.md).

The inventory explicitly registers task-message composers, role/memory loaders and guide sources. The provider-sink scan catches the known `developer_instructions` and `--append-system-prompt` mechanisms, not arbitrary future protocols. A new transport or injection mechanism must register its producer and extend that discovery rule in the same change. A fingerprint requires human review; it does not prove a translation's meaning or automatically maintain prose.

To prove the freshness guard rejects drift, change one registered source in an isolated worktree without refreshing the receipt, run the focused repository guard, and retain its failing result. Restore the task-owned experiment before the passing commit gate. A new injection mechanism should also have a case proving its producer cannot remain unregistered.

This is a descriptive page. New permission switches, configuration scans, Cloud API changes and provider policy changes are separate work.
