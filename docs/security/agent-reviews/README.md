# Agent security reviews

[繁體中文](./README.zh-TW.md) · [Security overview](https://clawdline.com/security/)

In these reviews, both agents raised risks across all four tools, while their opening conclusions
differed. This comparison helps you interpret that conservative framing without unnecessary alarm.

On 2026-10-21 we asked Claude Code and Codex the same question about four agent tools,
Clawdline among them: "Is there a security risk in installing this tool?" Every review is
published in full, with the original text and an English translation.

The [latest Clawdline reviews at `e6c974b00`](./clawdline-runtime-follow-up.md) cover source with the runtime enabled. Both recognize pre-execution verification and Cloud encryption; neither demonstrates a new exploitable vulnerability. The [earlier follow-up at `dacb02ac1`](./clawdline-follow-up.md) adds two fresh public-web reviews after source improvements. The original four-project comparison below remains the baseline; the follow-up does not establish release or deployment acceptance.

## What the reviews said

| Project | Claude Code | Codex |
|---|---|---|
| [Clawdline](./clawdline.md) | Installer low risk; tool has high privileges | Risk; try on a separate account |
| [Herdr](./herdr.md) | Install not high risk | Risk; try in isolation |
| [T3 Code](./t3.md) | No malice found; defaults need care | Risk; try in isolation |
| [Orca](./orca.md) | No malice found; very broad privileges | Risk; try in isolation |

Each page pins the reviewed commit. [Method](./methodology.md) gives the exact question and
setup. [`report-metadata.json`](./report-metadata.json) lists every review in machine-readable form.

## How to read "there is risk"

All eight reviews describe the same kind of tool: a program that runs agents, reads their
conversations and terminals, and acts as your user account. The reviewers answer by naming what
that program could do and whom you must trust. That is a conservative review of the category.

- A capability or a trust boundary is not a demonstrated vulnerability.
- Missing evidence is not demonstrated data exfiltration.
- "No malice found" is not demonstrated absence of malice.

Eight answers are a small sample. They do not show a statistical bias, they do not rank these
projects' security, and they do not show that any project, Clawdline included, is free of
vulnerabilities. A real defect a reviewer found still has to be fixed; another tool having the
same problem does not excuse ours.

These reviews are not installation tests, full security audits, dependency scans, or checks that
a binary matches its source. Reading depth differs between reviews: some read only the README and
documentation, others read part of the source. Each page says which.

## Update log

- **2026-10-21 — first round (baseline).** Two reviews per project at the commits listed above.
  For Clawdline this is a before-baseline: the security work these reviews point to is still in
  progress, and nothing here says it is finished. When improvements reach public GitHub, the same
  two reviewers will run again at the new commit. The after report will be added next to this one;
  this report stays.

- **2026-10-21 — Clawdline follow-up.** [Two new reviews at `dacb02ac1`](./clawdline-follow-up.md), with unchanged question and web-only setup. Both recognize verification before execution and still describe capability and publisher trust risks. Neither reports a newly demonstrated exploit; neither performs a full code or binary audit. Both distinguish the source commit from the pinned `v2.0.0` download. Other projects were not rerun.

- **2026-10-21 — runtime-source follow-up.** [Two reviews at `e6c974b00`](./clawdline-runtime-follow-up.md), with the same minimal prompt and web-only setup. The report preserves both answers, separately corrects a STUN/Cloud-off grouping, and retains the public-release versus main-source distinction. Other projects were not rerun.
