<!-- clawdline-doc: kind=guide audience=both -->

# Project Workflows

A Workflow is a Project-scoped name and complete launch message. It may start
with a Skill invocation, but saving does not bind it to that Skill: the message
can be edited, and a Workflow made from an empty draft is equally valid.

The Workflow page in the sidebar lists one Project's saved messages. It reads
registered places and that Project's Skill files; choosing a Skill fills a
new editable draft. The editor saves only `title` and `body`. The Project path
is assigned from the selected place, not typed into the form. There is no
description, scheduling rule, or implicit trigger.

The Session mark opens one sheet with Snippet and Workflow tabs. A Snippet
continues to insert text without sending. Selecting a Workflow shows its
entire saved message and Start sends it to this Session. A Workflow belongs to
the same Project key as the Session. The list is machine-specific, including
when the Console is reached through Cloud.

## Storage and execution

Workflows use the reusable-text store's `workflow` scope, with the same title,
body, Project key, ordering, write receipts and capacity limits as snippets.
The Snippet tab filters that scope out. This shares the existing encrypted
Cloud read and command transport without giving Cloud the text in cleartext.

Starting creates a direct Session to-do containing the saved launch message
and a trailing Workflow identity marker, then sends that to-do. The to-do
route's own receipt prevents a repeated send for a single row. The marker
preserves the Workflow name at launch time even when its definition is later
edited or deleted. The Session to-do fold groups these rows as Workflow
progress: unsent, sent, read/in progress, and complete. The assistant's
`clawdline todo done` receipt or the person's checkbox completes the run.
Delivery and reading are not reported as completion. If creation succeeds but
send fails, the unsent row stays in the fold with a Send action; an uncertain
result directs the person to that fold before retrying.

The first version does not schedule Workflows, chain Sessions, run commands
directly, or define a second step language. The launch message may tell the
assistant to use a Skill, but Clawdline does not interpret that instruction.

## Acceptance

- A Project Workflow can be created blank or from a Skill, edited after import,
  read after a reload, and deleted without changing the source Skill.
- A Session in another Project sees no Workflow from this Project.
- Snippets still insert text and never send; a Workflow start leaves a durable
  to-do and one send receipt.
- The fold distinguishes sent/read from complete, and an unsent run remains
  recoverable after a failed send.
- The phone E2E path imports a Skill, saves the Workflow, starts it from the
  Session mark, and sees its progress in the fold.
