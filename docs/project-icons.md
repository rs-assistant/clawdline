# Project icons

A project icon is a colour grid (`accent`, `cells`). The daemon resolves it for
the console's project, Session and Board views. Application and assistant marks
are separate assets.

`internal/domain/icon` resolves a local mark in this order:

1. A saved mark in `CLAWDLINE_NEXT_DIR/project-icon-overrides.json`, using the
   longest containing path. Existing overrides remain readable so an upgrade
   does not make a chosen mark disappear. The console no longer creates them.
2. The longest containing entry in `~/.claude/project-icons.json`. This
   compatibility input remains read-only. A home-directory entry names only
   the home directory, not every project below it.
3. A deterministic creature generated from the working directory's FNV-1a
   hash.

The daemon rereads the override file when resolving a Project or Session icon.
Creation, edits, atomic replacement and removal take effect on the next read;
the legacy registry is also reread and compared by content. A malformed or
unreadable setting keeps the last valid mark in memory, while Project and
Session list requests report `icon_store_unavailable` (HTTP 503) instead of
claiming a successful refresh. Saving an icon also refuses the write until an
externally changed override file can be read again.

Run `clawdline project refresh [directory]` to ask the running daemon to read
one listed Project's current display state. The directory defaults to the
current directory. It reads the Project list, including its icon and source,
repository identity, deployment activity, server declarations, sync readiness
and unify status, then checks the Session list presentation. It only reads:
it does not start or restart a server, run a deployment, or change settings.

For example, after changing an icon file:

```sh
clawdline project refresh /path/to/project
```

Success prints the path, current accent and icon source, repository identity,
deployment and server status, sync readiness, and unify status, then a refreshed
status and exits 0. Human-facing labels use the CLI language. A directory outside the current Project list
reports that it is not listed and exits 3 (unknown); the list is limited to
the 40 most recent places. An unreadable or invalid file, daemon refusal,
network error or invalid answer reports an error and exits 1. The command
never reports success on those errors.

For projects on multiple machines, [project settings sync](project-sync.md)
matches checkouts by their portable git `origin` and carries the source's name
and icon to each mirror. A receiving machine follows later source changes when
the Projects page is opened. A local path, folder name or generated path hash
is not a cross-machine project identity.
