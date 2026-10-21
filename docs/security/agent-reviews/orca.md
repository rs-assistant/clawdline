# Orca: two agent reviews

[繁體中文](./orca.zh-TW.md) · [Index](./README.md) · [Method](./methodology.md)

- Reviewed: [`stablyai/orca@84af8ef`](https://github.com/stablyai/orca/tree/84af8efee3e4307ad13c800b45a5ab938ad4de7b)
- Date: 2026-10-21
- Question: "Is there a security risk in installing this tool?" (asked in Traditional Chinese)
- Reviewers: Claude Code and Codex, each in a new session with web access only

The two reviews come first, then the editors' notes. The reviews are the reviewers' views, not this project's conclusions.

**Translation.** Both reviewers answered in Traditional Chinese. What follows is a complete English translation made by this project's editors; the [original text](./orca.zh-TW.md) is authoritative. As in the original, headings are demoted to level four and a closing "Fixed but not yet released" status line unrelated to the comparison is removed.

## Claude Code (translated)

**Conclusion: I found no signs of malice and no published vulnerabilities. But this is a tool with very broad privileges; the main risk comes from what it "can do", not from a problem in the code itself.**

I looked only at the repo front page, the Security page and `package.json`; I did not review the source line by line, nor check the released binaries.

#### Positive factors
- **Licence and popularity**: MIT open source, about 100,000 stars and 15,500 commits, maintained by the YC-backed Stably AI; it looks like a legitimate project, not a repo of unknown origin.
- **Installation**: install with a .dmg, `brew install --cask stablyai/orca/orca` or the AUR, with no need for `curl | bash`. The Windows build is code-signed through SignPath.
- **Known issues**: there are no public security advisories on GitHub. The Orca CVEs found by searching online all belong to other software with the same name (the GNOME Orca screen reader, the Japanese medical system ORCA) and are unrelated to this project.

#### Risks to watch
1. **Very broad privileges**: its purpose is to run several AI coding agents at once (Claude Code, Codex and so on), and these agents can run shell commands and read and write your files. The "Computer Use" feature even lets an agent operate other desktop apps. The actual risk depends mainly on how the agents' permissions are set.
2. **Phone remote control**: the phone app pairs with the desktop through the relay in `cloud/`, which adds another channel for sending commands to agents remotely. If you do not need it, do not pair.
3. **Account credentials are concentrated**: it manages your Claude, OpenAI, GitHub, Linear and other accounts, plus SSH remote connections (`ssh2`). If this computer is compromised, the impact is larger.
4. **Telemetry**: it uses PostHog to collect anonymous usage data, which can be turned off.
5. **Automatic updates**: it uses `electron-updater`, which means you trust every version Stably pushes in future (that is, supply-chain risk).
6. **No SECURITY.md**: the project has no formal vulnerability-reporting policy. For a project of this size, its security process is relatively immature.
7. **Building from source**: if you build it yourself with `pnpm install`, it runs `postinstall` (rebuilding native modules) and husky's git hooks, and also installs a large number of npm dependencies. Ordinary users should just download the installer.

#### Recommendations
- Download from official sources (onorca.dev, GitHub Releases or Homebrew).
- After installing, turn off telemetry first, and if you do not need them, do not enable Computer Use or phone pairing.
- Do not let agents use an "auto-approve everything" mode or a mode that skips permission confirmation, especially in projects holding secrets or production credentials.
- If your security requirements are high, run it in a separate user account, a VM or a dedicated machine.

If you like, I can download this commit's source and look more deeply at what telemetry sends, the relay's verification mechanism, or the permission control of Computer Use.

Sources:
- [stablyai/orca @ 84af8ef](https://github.com/stablyai/orca/tree/84af8efee3e4307ad13c800b45a5ab938ad4de7b)
- [stablyai/orca Security](https://github.com/stablyai/orca/security)
- [package.json](https://raw.githubusercontent.com/stablyai/orca/84af8efee3e4307ad13c800b45a5ab938ad4de7b/package.json)
- [GNOME Orca CVE-2013-4245 (same name, unrelated)](https://cvemon.intruder.io/cves/CVE-2013-4245)
- [JVN#37376131 ORCA (same name, unrelated)](https://jvn.jp/en/jp/JVN37376131/)

## Codex (translated)

**There is security risk. I recommend trying it first in an isolated environment, not installing it directly on a main work account that holds production credentials.** This does not mean Orca is malware; rather, its features require a very wide scope of trust.

I spot-checked the commit you specified, `84af8ef` (version `1.4.214`); the main risks are:

| Risk | Basis found and its impact |
|---|---|
| **Local commands and file operations** | It supports terminals, AI agents, SSH and desktop control. If the program or an agent is exploited, it may affect the code and credentials that user can reach; a Git worktree only separates working directories and cannot be treated as a security sandbox. [Feature description](https://github.com/stablyai/orca/blob/84af8efee3e4307ad13c800b45a5ab938ad4de7b/README.md) |
| **It intervenes in existing agent environments** | The startup flow conditionally installs / updates agent hooks and status plugins, so the impact may extend to your existing CLI workflow. [Startup code](https://github.com/stablyai/orca/blob/84af8efee3e4307ad13c800b45a5ab938ad4de7b/src/main/startup/main-process-ready-runtime.ts) |
| **Remote control adds attack surface** | Phone control, SSH and browser automation all deserve further checks of authentication, pairing revocation and permission boundaries. This check did not fully verify these paths and cannot guarantee remote control is safe. [Feature description](https://github.com/stablyai/orca/blob/84af8efee3e4307ad13c800b45a5ab938ad4de7b/README.md) |
| **Installing from source also runs code** | `pnpm install`'s `postinstall` builds packages and rebuilds native dependencies; installation itself requires trusting the project and its dependency supply chain. [package.json](https://github.com/stablyai/orca/blob/84af8efee3e4307ad13c800b45a5ab938ad4de7b/package.json) |

I also saw protective measures: the main window sets Electron `sandbox: true`; telemetry has a consent check and an opt-out switch; the macOS release build is configured for signing, notarisation and hardened runtime. However, **a setting existing does not mean the downloaded installer has passed verification, nor that the whole tool is sandboxed**. [Window settings](https://github.com/stablyai/orca/blob/84af8efee3e4307ad13c800b45a5ab938ad4de7b/src/main/window/createMainWindow.ts), [Telemetry control](https://github.com/stablyai/orca/blob/84af8efee3e4307ad13c800b45a5ab938ad4de7b/src/main/telemetry/consent.ts), [Release configuration](https://github.com/stablyai/orca/blob/84af8efee3e4307ad13c800b45a5ab938ad4de7b/config/electron-builder.config.cjs)

For an actual trial, I recommend:

- Use a separate operating-system account or VM holding only test projects, and avoid sharing SSH keys, cloud credentials and a production `.env`.
- Do not enable phone / remote control at first, and do not grant Full Disk Access, Accessibility or Screen Recording until you actually need them.
- Keep the agents' command-approval mechanism.
- Verify the release file's source and signature before installing; you pasted a pinned source version, and the version installed from the download page or Homebrew is not necessarily the same.

**The current conclusion is "a development tool that requires a high degree of trust; it can be tried in isolation, but is not yet enough to place in a sensitive work environment with confidence."** This was a partial static source check; I did not run the installer, monitor connections or complete a dependency vulnerability scan, so it cannot serve as a guarantee of no malware or no vulnerabilities.

## Editors' notes

This section is written by this project's editors and is not part of either review. "Reviewer claim, not independently established" marks a statement we did not check against a primary source.

- **Reading depth differs.** Claude Code says it looked only at the repository front page, the Security page and `package.json`. Codex sampled the README and source for startup, the window, telemetry and release configuration.
- **Checked against primary sources:** the pinned [`consent.ts`](https://raw.githubusercontent.com/stablyai/orca/84af8efee3e4307ad13c800b45a5ab938ad4de7b/src/main/telemetry/consent.ts) resolves consent, has a first-launch banner, and disables telemetry under CI or an environment variable, as Codex describes.
- **"PostHog collects anonymous usage data": reviewer claim, not independently established.** We confirmed only the consent and opt-out mechanism; we did not verify that what is sent is anonymous, or what the default is.
- **Stars, commit counts and YC funding are not security endorsements.** About 100,000 stars and 15,500 commits: reviewer claim, not independently established. No public advisory does not mean no vulnerability; SignPath signing of the Windows build was likewise not verified by us.
- **No SECURITY.md:** we found none at the pinned commit's root or in `.github/`, which matches the review; this does not rule out a reporting channel elsewhere.
- **Installing through Homebrew avoids `curl | bash`,** but it changes the distribution channel; it is not inherently safer.
