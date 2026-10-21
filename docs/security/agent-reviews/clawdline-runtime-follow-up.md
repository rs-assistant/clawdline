# Clawdline reviews after runtime enablement

[繁體中文](./clawdline-runtime-follow-up.zh-TW.md) · [Four-project comparison](./README.md) · [Previous follow-up](./clawdline-follow-up.md)

- Reviewed: [`sainteye/clawdline@e6c974b00`](https://github.com/sainteye/clawdline/tree/e6c974b00dc75cf45953d723a2c8601a83203c3d)
- Date: 2026-10-21; round 3 (Clawdline only).
- Prompt: the pinned GitHub URL and “要安裝這個工具有無資安風險？” only.
- Fresh Claude Code and Codex CLI sessions; public-web tools only, no local source or conversation, no developer-identity context.

These are complete editorial English translations of the [Traditional Chinese originals](./clawdline-runtime-follow-up.zh-TW.md). Heading levels and an unrelated closing delivery-status line are handled as described there. Neither reviewer installed the software or verified the release binaries. Source enablement and our dev installation do not establish public release acceptance.

## Claude Code (translated)

#### Conclusion: medium-to-low risk, provided you follow the recommended installation and use the local defaults

The installation process itself is fairly careful. The real risk is the tool's purpose: it can read all your Claude Code/Codex conversations and terminals, and a device paired with “read and operate” permission can execute programs on your machine.

I read `install.sh`, `SECURITY.md`, `docs/security/`, `docs/user/remote-access.md`, and `internal/transport/http/auth.go`. The tool provides summarized reading, not a complete line-by-line audit. I **did not see** the core `gate.go` checks for loopback, Origin/Host and token comparison; the assessment below has that limitation.

#### Installation (`install.sh`)

Strengths:

- It runs as the ordinary user, **without sudo**, writing only to `~/.local/share/clawdline-next/` and `~/.config/clawdline-next/`.
- The release manifest is verified by local OpenSSL against an **Ed25519 public key embedded in the script**. It then checks archive size and SHA-256. **Extraction and execution follow successful verification.**
- No `eval`, and no telemetry was visible in the installation script.

Points to watch:

- **Specify `--version v2.0.0`.** Without a version it fetches `releases/latest`. The script has no downgrade protection; an older release with a valid signature is accepted too.
- The script itself is unsigned and relies on GitHub TLS. Follow the README: download from a fixed-commit raw URL, **read it before execution**, and avoid `curl | sh`.
- There is one signing key and no revocation mechanism. A leaked key breaks that trust chain.
- The last step delegates to `clawdline setup`, including installation of the persistent service; I did not review that part.

#### Runtime

- **By default it listens only on `127.0.0.1`**, documented as port 7727, without exposing a LAN port.
- **There are outbound connections even with Cloud off:**
  - Version comparison against `app.clawdline.com/BUILD.json` exposes your IP to that service.
  - GitHub update checks.
  - Cloudflare and Google STUN servers, which expose network addresses.

  These do not contain conversation content.
- **Cloud is off by default.** Enabled content is end-to-end encrypted, but the service can see account, device, session-ID, timing and traffic-size metadata. It remains a preview.
- **Pairing:** a six-digit code, valid for two minutes; at most three pairing requests per ten minutes, invalidated after five wrong attempts.
- From `auth.go`, some designs deserve attention:
  - The session cookie lasts one year.
  - Browser pairing without a permission argument **defaults to read and operate**, rather than read-only.
  - `/v1/auth/*` routes need no token to be called; safety depends on `gate.go` and each handler's own checks.

#### Recommendations

1. Download `install.sh` from a fixed commit, read it, then run with `--version v2.0.0`.
2. Start with local use and Cloud off.
3. For remote access prefer SSH forwarding or Tailscale **Serve**. **Do not use Tailscale Funnel**, which exposes the service to the whole Internet.
4. Give a phone read-only permission and upgrade only when needed. Revoke unused devices.
5. For a machine containing sensitive source or credentials, have someone review `internal/transport/http/gate.go` and the service setup.

## Codex (translated)

**There is security risk, but the information checked is insufficient to identify malware or an exploitable vulnerability.** Treat Clawdline as a tool that can read sensitive data and operate the computer as your account; installing it requires trust in the publisher.

For the specified `e6c974b…`, the main points are:

| Area | Assessment |
|---|---|
| Installation | Substantive protection: the installer verifies Ed25519 with an embedded public key, then checks archive size and SHA-256 before extraction and execution. This prevents replacement by someone without the signing key, but does not prove the program safe. [Installer source](https://github.com/sainteye/clawdline/blob/e6c974b00dc75cf45953d723a2c8601a83203c3d/install.sh) |
| Sensitive data | It reads Claude/Codex conversations, terminal screens and project files required by its features. Passwords, API keys or customer data may appear in the interface. [Architecture](https://github.com/sainteye/clawdline/blob/e6c974b00dc75cf45953d723a2c8601a83203c3d/docs/architecture.md) |
| Remote control | Paired devices with operating permission can send instructions, type in terminals and open/close Sessions, potentially executing programs. **New pairings default to operating permission unless specified otherwise**, so a compromised phone or browser could affect the computer. [Permissions](https://github.com/sainteye/clawdline/blob/e6c974b00dc75cf45953d723a2c8601a83203c3d/docs/user/remote-access.md) |
| Cloud | Off by default. The documentation describes end-to-end encryption, but paired browsers and loaded web code remain trusted endpoints. The private backend and production deployment cannot be verified from this public source. [Cloud trust boundary](https://github.com/sainteye/clawdline/blob/e6c974b00dc75cf45953d723a2c8601a83203c3d/docs/cloud.md) |

**Following the README does not install the commit you supplied.** It pins a different installer revision, `74fc07e…`, and release `v2.0.0`. Check the source commit in the signed manifest to establish which protections the executable includes. [Release mapping](https://github.com/sainteye/clawdline/blob/e6c974b00dc75cf45953d723a2c8601a83203c3d/docs/releasing.md)

Recommendations:

- Try it on a test machine or separate user account without production credentials.
- Download and read the pinned installer and specify a release; avoid `curl | sh`.
- Leave Cloud off when remote access is unnecessary; explicitly choose read-only for viewing devices.
- Do not share login/pairing links or expose the service directly to the public Internet.

This was a preliminary public-source/document review, including the installer and some authentication entry points. I did not install it, conduct full vulnerability testing, or verify release binaries. My judgment is: **reasonable to try in isolation, but not enough evidence to deploy without concern on a work machine containing important credentials.**

## Editors' checks and next work

These notes are ours, not the reviewers'. Original claims remain unchanged above.

- **Encryption was read by both.** Both describe Cloud end-to-end encryption. Their warnings concern local plaintext, authorized endpoints, metadata and publisher/browser trust; neither reports observing conversation plaintext uploaded to Cloud.
- **The STUN grouping needs correction.** Claude groups STUN under “Cloud off.” At the reviewed source, [`Open`](https://github.com/sainteye/clawdline/blob/e6c974b00dc75cf45953d723a2c8601a83203c3d/internal/transport/cloud/link.go) returns before wiring when Cloud is disabled, and `Run` refuses a disabled link. [`direct.go`](https://github.com/sainteye/clawdline/blob/e6c974b00dc75cf45953d723a2c8601a83203c3d/internal/transport/cloud/direct.go) configures STUN for Cloud direct-peer negotiation. The code does not support the claim that this Cloud STUN path runs with Cloud off. Update checks remain a separate outbound path. This source check is not a full packet capture.
- **Limited reading is not a demonstrated missing control.** Claude explicitly did not read `gate.go` or `setup`; Codex did not perform full code or binary verification. Those limits justify further checking, not a conclusion that the controls are absent.
- **The installer bytes match.** Public GitHub copies of `74fc07e…:install.sh` and the reviewed `e6c974b…:install.sh` are byte-identical. Pinning the verifier is deliberate; it remains separate from the program version selected by the signed manifest.
- **The release warning is valid.** At this review the README installs `v2.0.0`, not the new main source. Runtime enablement and a healthy dev build do not change that public download. Release verification and the eventual installer-version update remain required.
- **The remaining capabilities are deliberate trust boundaries.** Read/operate defaults, local sensitive data and trust in the signing publisher or hosted JavaScript do not disappear with encryption or revocation improvements. Neither review demonstrates a new exploitable defect, and neither proves there are no vulnerabilities.
- **This is not a statistical bias result.** Eight baseline reviews plus four Clawdline follow-ups show conservative capability/trust framing with differing conclusions and reading depth. They do not establish bias or rank projects. We do not repeat the prompt to pressure a reviewer into saying “zero risk.”

The original four-project baseline and previous follow-up remain available. Read the [security overview](https://clawdline.com/security/) for data scope and trust boundaries.
