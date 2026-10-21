# Clawdline: two agent reviews

[繁體中文](./clawdline.zh-TW.md) · [Index](./README.md) · [Method](./methodology.md)

- Reviewed: [`sainteye/clawdline@31139d6`](https://github.com/sainteye/clawdline/tree/31139d647cd821b70a875ec16455adf05a836deb)
- Date: 2026-10-21
- Question: "Is there a security risk in installing this tool?" (asked in Traditional Chinese)
- Reviewers: Claude Code and Codex, each in a new session with web access only

The two reviews come first, then the editors' notes. The reviews are the reviewers' views, not this project's conclusions.

**Translation.** Both reviewers answered in Traditional Chinese. What follows is a complete English translation made by this project's editors; the [original text](./clawdline.zh-TW.md) is authoritative. As in the original, headings are demoted to level four and a closing "Fixed but not yet released" status line unrelated to the comparison is removed.

## Claude Code (translated)

#### Conclusion

Having read two versions of `install.sh`, I found no malicious or suspicious behaviour. The installation flow's security design is good. The risk that really needs weighing is in the tool's own privileges: it can remotely control your Claude Code / Codex sessions, and I did not review that part of the code.

#### Installation script: low risk

- **No sudo**: everything runs as the current user, with `umask 077`; files go into `~/.local/share/clawdline-next` and `~/.config/clawdline-next`.
- **Signature verification**: the script hard-codes one Ed25519 public key (`2Du3yZ4j…/O0=`) and never takes a key from the download source or an environment variable. It verifies the signature of `manifest.json` with the local OpenSSL 3, then checks the archive's size and SHA-256, and extracts and runs it only when all of these pass.
- **HTTPS only**: it refuses redirects that downgrade from HTTPS; only localhost may use HTTP.
- **No `eval`, no telemetry**: the script itself creates no service and opens no port.
- The README's recommended approach is to download the script at a pinned commit, read it, and then run it with `--version v2.0.0`, which is safer than piping `curl | sh` directly. The v2.0.0 release does exist and is marked Latest.

#### Things to watch

1. **The README points to a script from a different commit**: you gave `31139d6`, but that commit's README tells you to download `install.sh` from `74fc07e`. I compared them: the public key, verification and extraction logic are the same in both, but you should know which one you are actually running.
2. **Trust ultimately rests on the signing private key**: a signature proves only that a file came from whoever holds the private key. If the private key leaks, or the maintainer is not trustworthy, this verification stops neither. There is currently one key and no visible rotation history.
3. **The real actions are performed by the binary**: the script ends by running `clawdline setup`; that Go program creates the launchd/systemd resident service, starts the daemon and opens a port (the example uses 7800). I **did not review** this part.
4. **The tool itself has broad privileges**, and that is the main risk:
   - It can read the conversation content of every session and type text into terminals. In effect, anyone who can reach this daemon can run commands on your computer through your AI agent.
   - The README says it listens only on loopback by default. But other programs on the machine, or web pages in a browser via CSRF / DNS rebinding, may be able to reach a localhost service. Check whether it performs authentication or token verification.
   - Phone remote control and the Cloud features are optional, and the README claims end-to-end encryption. Once enabled, the attack surface extends to your phone and account, and the same goes for WebRTC direct connections.
5. **A minor issue**: `--purge` runs `rm -rf` on `CLAWDLINE_NEXT_DIR`. If that variable is set wrongly, it will delete data it should not.

#### Recommendations

- Download as the README describes, read the script, then run it with `--version v2.0.0`; do not use `curl | sh`.
- If you do not need them, leave Cloud and the phone features off for now and use it locally only.
- After installing, use `lsof -iTCP -sTCP:LISTEN | grep -i clawd` to confirm it binds only `127.0.0.1`.
- For more confidence, I can go on to review the daemon's authentication and listening logic in `internal/`, and the setup flow in `cmd/clawdline`.

## Codex (translated)

**There is security risk; evaluate it as "a resident tool that can read development data and operate terminals."** I examined the pinned commit's installation script, HTTP permission gate and related documentation; there are real protections, but this check is not enough to guarantee there are no vulnerabilities.

These five points matter most:

| Item | Assessment |
|---|---|
| **Installation supply chain** | The installer first verifies an Ed25519 signature with a built-in public key, then checks the download's size and SHA-256, and extracts and runs it only after these pass. This protects against a replaced download, but not against a compromised publisher or signing private key. [Installer source](https://github.com/sainteye/clawdline/blob/31139d647cd821b70a875ec16455adf05a836deb/install.sh) |
| **Data and operating privileges** | It can read Claude / Codex conversations, terminal screens and the project files its features need, and can run commands through a terminal or an Agent. Keys, code and customer data in conversations are therefore within the risk scope. [Data and permissions](https://github.com/sainteye/clawdline/blob/31139d647cd821b70a875ec16455adf05a836deb/README.md#security-and-your-data) |
| **Resident execution** | It needs no `sudo`, but by default creates a per-user background service; on Linux it also tries to enable running after logout. Without administrator rights it can still affect the data that account can reach. [Installation behaviour](https://github.com/sainteye/clawdline/blob/31139d647cd821b70a875ec16455adf05a836deb/docs/user/install.md) |
| **Local interface** | By default it listens only on loopback; the program checks credentials, origin and write permission, which lowers the risk of a malicious web page calling the local service. But whether these protections can be bypassed has not been tested. [Permission gate code](https://github.com/sainteye/clawdline/blob/31139d647cd821b70a875ec16455adf05a836deb/internal/transport/http/gate.go) |
| **Cloud remote control** | Off by default; when enabled it encrypts content, but a paired browser and the web code served to it still have to be trusted. A public repository also cannot verify the private cloud service's actual deployment, retention policy and operational security. [Cloud trust boundary](https://github.com/sainteye/clawdline/blob/31139d647cd821b70a875ec16455adf05a836deb/docs/cloud.md) |

**There is also a version trap:** the commit you pasted is `31139d6…`, but its README specifies downloading the installer from `74fc07e…` and installing `v2.0.0`; the documentation states that release's source is `d2ed0c0…`. So **having read the code at this link is not the same as having reviewed the program actually installed**; and a signature does not prove the binary matches the source. [Version mapping](https://github.com/sainteye/clawdline/blob/31139d647cd821b70a875ec16455adf05a836deb/docs/user/install.md#review-and-pin-an-installation)

My recommendation: **for ordinary personal development, try it first on a separate account or test machine; do not install it directly on a main machine that holds production credentials, company secrets or customer data.** For a first trial, keep Cloud off, use `--no-autostart`, pin the installer and release as the documentation describes, and download and read before running. With Cloud off there may still be update-check connections.

This time I did not install or run it, and did not complete a dependency vulnerability scan, penetration test, or reproducible-build verification of the release files.

## Editors' notes

This section is written by this project's editors and is not part of either review. "Reviewer claim, not independently established" marks a statement we did not check against a primary source.

- **Reading depth differs.** Claude Code read two versions of `install.sh` and says it did not review the daemon or `clawdline setup`. Codex read the installer, `internal/transport/http/gate.go` and the documentation. Neither installed or ran it.
- **Checked against primary sources:** the pinned [`install.sh`](https://github.com/sainteye/clawdline/blob/31139d647cd821b70a875ec16455adf05a836deb/install.sh) hard-codes one Ed25519 public key (`TRUSTED_KEYS`) and sets `umask 077`; that commit's README does point to the installer at `74fc07e` and to `v2.0.0`.
- **The `--purge` observation holds.** The script's `purge_state` runs `rm -rf` on `CLAWDLINE_NEXT_DIR` (by default `clawdline-next` under the XDG config directory). It is a real usage risk the reviewer found, and another tool's shortcomings do not excuse it.
- **"No telemetry" refers to the installer only.** The original says "the script itself"; Codex adds that an update check may still connect while Cloud is off.
- **CSRF and DNS rebinding are open questions the reviewer raised, not demonstrated vulnerabilities.** Codex read the gate's credential and origin checks, and also says it did not test whether they can be bypassed.
- **This is the first-round baseline.** The high privileges, resident service, version mapping and trust boundaries both reviews name are the scope of security work now in progress; after improvements reach public GitHub, a new report at a new SHA will be added. See the [update log](./README.md#update-log).
