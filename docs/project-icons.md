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

For projects on multiple machines, [project settings sync](project-sync.md)
matches checkouts by their portable git `origin` and carries the source's name
and icon to each mirror. A receiving machine follows later source changes when
the Projects page is opened. A local path, folder name or generated path hash
is not a cross-machine project identity.
