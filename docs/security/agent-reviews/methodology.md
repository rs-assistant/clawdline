# How the agent reviews were run

[繁體中文](./methodology.zh-TW.md) · [Index](./README.md)

This page lets you repeat the reviews, or judge how much weight to give them.

## The question

Every review received the same minimal prompt: one fixed GitHub URL for the project, pinned to a
commit, and this question in Traditional Chinese:

> 要安裝這個工具有無資安風險？

English translation for readers: "Is there a security risk in installing this tool?"
The translation was not included in the prompt.

The prompt contained nothing else. It did not tell the reviewer that the review was part of a
comparison, who develops Clawdline, or that it had to judge the tool safe or unsafe.

## The setup

- Date: 2026-10-21. Round: 1.
- Reviewers: Claude Code and Codex. Each review ran in a new, independent CLI session.
- Tools: web access only (WebFetch, WebSearch or the reviewer's live web tool). The reviewer could
  not read local files, and did not clone, install or run any project.

| Project | Repository | Commit |
|---|---|---|
| Clawdline | `sainteye/clawdline` | `31139d6` |
| Herdr | `herdrdev/herdr` | `143bf5f` |
| T3 Code | `pingdotgg/t3code` | `7ffc3bb` |
| Orca | `stablyai/orca` | `84af8ef` |

Full commit hashes are in [`report-metadata.json`](./report-metadata.json).

## What this is not

The question was the same for every review, but how deeply each reviewer read was not controlled.
Some reviews read only the README and documentation; others read part of the source. This is not
a controlled experiment, an installation test, a full security audit, a dependency scan, or a
check that any binary matches its source.

## How the text was edited

- The original Traditional Chinese text is kept paragraph by paragraph. Headings are demoted to
  level four so they sit under each page's own headings, and a closing "Fixed but not yet
  released" status line, unrelated to the comparison, is removed.
- The English pages are complete translations by this project's editors and are labelled as
  translations. The reviewers did not answer in English.
- Each reviewer's source links are kept as they were. The T3 review's security reporting email
  is replaced by a link to its published SECURITY.md to satisfy this repository's privacy guard;
  both language versions disclose that substitution.
- The editors' notes are kept apart from the reviews. Where a review overreaches (for example
  treating popularity as a security endorsement, or reading "no advisory" as "no vulnerability"),
  the notes say "reviewer claim, not independently established" or give a correction, without
  changing the review's text. Claims the notes call checked were checked against the reviewed
  project's own public files at the pinned commit, by web reading only.
