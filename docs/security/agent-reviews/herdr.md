# Herdr: two agent reviews

[繁體中文](./herdr.zh-TW.md) · [Index](./README.md) · [Method](./methodology.md)

- Reviewed: [`herdrdev/herdr@143bf5f`](https://github.com/herdrdev/herdr/tree/143bf5fd4781d164260c63fe28e7dc08054ad32f)
- Date: 2026-10-21
- Question: "Is there a security risk in installing this tool?" (asked in Traditional Chinese)
- Reviewers: Claude Code and Codex, each in a new session with web access only

The two reviews come first, then the editors' notes. The reviews are the reviewers' views, not this project's conclusions.

**Translation.** Both reviewers answered in Traditional Chinese. What follows is a complete English translation made by this project's editors; the [original text](./herdr.zh-TW.md) is authoritative. As in the original, headings are demoted to level four and a closing "Fixed but not yet released" status line unrelated to the comparison is removed.

## Claude Code (translated)

**Conclusion: from what I could find, the installation itself is not high risk, and I found nothing malicious or particularly suspicious.** This judgement comes from reading the documentation and the installation script, though; I did not review the source line by line. What really needs attention is the privileges that come with its features, not the installation step.

#### The installation step (low risk)
- I read the installation script `curl -fsSL https://herdr.dev/install.sh | sh`:
  - No sudo; the binary installs to `~/.local/bin` by default
  - It does not modify shell configuration files such as `.zshrc`
  - It verifies the download with SHA-256, but the hash and the binary come from the same manifest (`herdr.dev/latest.json`), with **no digital signature**. So it protects only against a corrupted download, not against herdr.dev itself being compromised
- For an extra layer of protection, you can use `brew install herdr` instead.
- The project is written in Rust, under the Apache 2.0 licence, with about 45,000 stars. There are no published security advisories on GitHub, but **there is also no SECURITY.md** (no formal vulnerability-reporting policy).
- Note: you gave a specific commit, `143bf5f`, but the installation script installs the binary of the latest official release, and the two are not necessarily the same code. To be sure you are running that commit, you have to `cargo build --release` it yourself.

#### Risks to know when using it
1. **The local socket is very powerful**: `~/.config/herdr/herdr.sock` can read any pane's output, send keystrokes, run commands and prompt agents, and the documentation mentions no authentication. That means any program under your account (including the AI agent itself) can control every terminal through it. On a single-user computer this is not much of a problem; on a shared machine, check the socket's file permissions.
2. **Plugins = running arbitrary code**: the official documentation states plugins are "not reviewed, not sandboxed" and run as your account. The marketplace merely auto-lists GitHub repos tagged `herdr-plugin`, with no human review. Read a third-party plugin's source before installing it; you can pin a version with `--ref`, and do not add `--yes` to skip the preview.
3. **Agent integrations modify configuration files**: running a command such as `herdr integration install claude` adds a hook to Claude Code's `settings.json` and writes `hooks/herdr-agent-state.sh` (the same applies to Codex, Copilot and others). These changes happen only when you run the command yourself, and can be uninstalled, but it is best to look at the hook script before installing.
4. **SSH remote machines**: it can store and connect to remote machines; I did not look into the authentication details of this part.

#### Recommendations
- An ordinary personal development machine: installing with Homebrew or the official script is acceptable.
- The cautious approach: build from source yourself, use only the official built-in features, install no third-party plugins, and inspect the hook scripts before installing agent integrations.
- A company environment: a remote-control socket plus agent automation may conflict with company endpoint-security policy; ask the security department first.

If you like, I can go on to review the socket permission setup in that commit (for example the chmod when the socket is created) or the contents of the hook scripts.

Sources:
- [herdr GitHub repo](https://github.com/herdrdev/herdr/tree/143bf5fd4781d164260c63fe28e7dc08054ad32f)
- [install.sh](https://herdr.dev/install.sh)
- [Security page](https://github.com/herdrdev/herdr/security)
- [Socket API documentation](https://herdr.dev/docs/socket-api/)
- [Integrations documentation](https://herdr.dev/docs/integrations/)
- [Plugins documentation](https://herdr.dev/docs/plugins/)

## Codex (translated)

**There is risk. I recommend trying it first in an isolated environment, not putting it straight into a main account that holds production credentials.** The parts I read show no obvious malicious behaviour, but that is not enough to conclude the whole tool is safe.

I did a static spot check of the `143bf5f…` version you specified; the main findings:

| Item | Assessment |
|---|---|
| **Installation source** | The script downloads the latest version named by `herdr.dev/latest.json`, which **is not guaranteed to be the commit you pasted**. There is SHA-256 verification, but the download location and the hash come from the same website manifest; if the website were compromised, both could be replaced together. [Installation script](https://raw.githubusercontent.com/herdrdev/herdr/143bf5fd4781d164260c63fe28e7dc08054ad32f/distribution/install.sh) |
| **Installation privileges** | The Unix script I checked writes only to `~/.local/bin` by default, and does not require `sudo`, add a startup service or automatically modify shell configuration; this is relatively conservative. [Installation script](https://raw.githubusercontent.com/herdrdev/herdr/143bf5fd4781d164260c63fe28e7dc08054ad32f/distribution/install.sh) |
| **Terminal control capability** | Its API can read other panes' output, send keystrokes and start agents. A program that can reach the control interface may therefore read secrets shown in a terminal or operate other sessions; do not treat separate panes as security isolation. [Official API documentation](https://herdr.dev/docs/socket-api/) |
| **Local access protection** | The source has a mechanism that sets the client socket to `0600`, restricting access by other accounts; but this cannot isolate untrusted programs or agents under the same account. [Socket permission code](https://raw.githubusercontent.com/herdrdev/herdr/143bf5fd4781d164260c63fe28e7dc08054ad32f/src/server/socket_paths.rs) |
| **Plugins** | **The riskiest extension point.** The official documentation states plainly that plugins have no sandbox, run as your account, inherit environment variables and can call the full CLI; installing a plugin amounts to trusting another program. [Official security notes](https://herdr.dev/docs/plugins/#trust-and-security) |
| **Background connections** | The configuration template shows it checks for version updates and agent-detection rule updates; the update module states the background version check only notifies, and only a manual update replaces the binary. I did not complete a whole-program trace for data exfiltration. [Configuration template](https://raw.githubusercontent.com/herdrdev/herdr/143bf5fd4781d164260c63fe28e7dc08054ad32f/src/main.rs), [Update module](https://raw.githubusercontent.com/herdrdev/herdr/143bf5fd4781d164260c63fe28e7dc08054ad32f/src/update.rs) |

My installation recommendations:

- Try it in a separate OS account or VM, without production SSH keys, cloud credentials or a production `.env`.
- Pin the version; to evaluate this commit, build from this commit and lock the dependencies. Building it yourself still carries dependency supply-chain risk.
- Avoid piping `curl | sh` directly; download, review, then run, and do not add `sudo`.
- At first install no third-party plugins, and do not mix trusted and untrusted agent work.

**Conclusion: for ordinary development and testing it can be tried once isolated; for an environment with company secrets or production credentials, the current evidence is not enough to adopt it with confidence.** This time I did not install or run the tool, and did not complete a dependency vulnerability scan, binary-to-source consistency verification, or a full security audit.

## Editors' notes

This section is written by this project's editors and is not part of either review. "Reviewer claim, not independently established" marks a statement we did not check against a primary source.

- **Reading depth differs.** Claude Code read `herdr.dev/install.sh` and the official documentation, and says it did not review the source line by line. Codex read `distribution/install.sh` at the pinned commit and several source files. The two read installers from different places, which need not be identical.
- **Checked against primary sources:** the pinned [`distribution/install.sh`](https://raw.githubusercontent.com/herdrdev/herdr/143bf5fd4781d164260c63fe28e7dc08054ad32f/distribution/install.sh) fetches its manifest from `https://herdr.dev/latest.json` and compares SHA-256; [`socket_paths.rs`](https://raw.githubusercontent.com/herdrdev/herdr/143bf5fd4781d164260c63fe28e7dc08054ad32f/src/server/socket_paths.rs) sets the socket mode to `0o600`.
- **Claude Code says the documentation mentions no authentication; Codex found `0600` in the source.** These do not conflict: file permissions stop other accounts, not programs running under the same account.
- **Star counts and "no published security advisories" are not security endorsements.** About 45,000 stars: reviewer claim, not independently established. No advisory does not mean no vulnerability.
- **"`brew install herdr` adds a layer of protection": reviewer claim, not independently established.** Homebrew changes the distribution channel and whom you trust; it is not inherently safer.
- **No SECURITY.md:** we found none at the pinned commit's root or in `.github/`, which matches the review; this does not rule out a reporting channel elsewhere.
