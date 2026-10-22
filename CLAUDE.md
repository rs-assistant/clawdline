@AGENTS.md

# Fork: rs-assistant/clawdline

This fork runs Clawdline **only from source we have reviewed**. It is not a release install and
does not take upstream releases or self-updates.

## Remotes and branches

- `origin` = `sainteye/clawdline` (upstream), `fork` = `rs-assistant/clawdline`.
- `local-source-build` branches from upstream `418bac7`, the commit that was security-reviewed
  on 2026-10-19. Pulling upstream means new unreviewed code: review the diff before merging.
- Session handoff and review summary: `handoff.yaml`.

## Build and run (source build only)

```sh
(cd web && npm ci --ignore-scripts && npm run build)   # lockfile integrity, no install scripts
go build -o bin/clawdline ./cmd/clawdline              # go.mod pins toolchain go1.27.3
CLAWDLINE_NEXT_UPDATE_CHECK=off \
CLAWDLINE_NEXT_WEB="$PWD/web/console/dist" \
./bin/clawdline serve
```

- Do not use `install.sh` or `clawdline update`. A source build never starts the GitHub release
  updater (`internal/transport/http/update.go`, `kind != install.KindRelease`).
- `CLAWDLINE_NEXT_UPDATE_CHECK=off` is required: without it the daemon GETs
  `app.clawdline.com/BUILD.json` every 30 min (`internal/adapters/updatecheck`).
- After a dependency change, run `go run golang.org/x/vuln/cmd/govulncheck@latest ./...`.
  Third-party Go/npm dependencies are hash-verified, not code-reviewed.

## Operating policy

- Cloud stays off (`cloud_enabled` unset/false). Do not run `clawdline cloud login`. If Cloud was
  ever turned on, `clawdline cloud off` does not drop a live relay line — restart the daemon.
- Do not enable `remote_tunnel`.
- Dispatch with `--permission-mode ask`; the default `full` maps to `bypassPermissions` /
  `--ask-for-approval never`. Never add `clawdline` to an agent's Bash allowlist.
- Do not open untrusted services on other `127.0.0.1` ports in the browser signed in to the
  console: the session cookie is not port-scoped.
