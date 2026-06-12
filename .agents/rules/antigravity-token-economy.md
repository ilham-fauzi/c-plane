# C-Plane Antigravity Token Economy

Use this rule when working in the C-Plane project.

## Default Behavior

- Follow `AGENTS.md` first.
- Prefer token-efficient file discovery:
  - `rg --files`
  - `rg -n "<term>" docs src test .`
  - `sed -n 'start,endp' <file>`
- Use `rtk` explicitly when available for compact command output:
  - `rtk git status`
  - `rtk git diff`
  - `rtk read <file>`
  - `rtk grep "<pattern>" .`
  - `rtk test <cmd>`
- Use `code-review-graph build` before broad review or blast-radius work once source files exist.
- Do not run global setup or init commands unless the user asks.

## Language Policy

- Conversation may follow the user's language.
- Repository artifacts must use English: documentation, code comments, identifiers, commit messages, API/CLI/UI labels, and error messages.

## Product Truth

- `docs/C_PLANE_PRD.md` is the source of truth.
- Preserve the architecture: Go API, SQLite MVP, Go agent, MQTT signal, HTTPS job detail, symlink releases, dashboard rollback from retained releases.
