# C-Plane Gemini Notes

Follow the project-local agent guidance in `AGENTS.md`.

## Token Economy

- Start with `rg --files`, targeted `rg -n`, and short `sed -n` ranges.
- NEVER read a full file if it exceeds 100 lines. Always search first, then view files using precise line ranges.
- Limit command output sizes: pipe long outputs through `head`, `tail`, or `grep` to avoid context flooding.
- Do not list directories repeatedly.
- Prefer compact explicit commands when available:
  - `rtk git status`
  - `rtk git diff`
  - `rtk read <file>`
  - `rtk grep "<pattern>" .`
  - `rtk test <cmd>`
- Use `code-review-graph build` for large code review, dependency impact, or blast-radius analysis after source files exist.
- Do not run global setup or init commands unless the user asks.

## Language Policy

- Conversation may follow the user's language.
- Repository artifacts must use English: documentation, code comments, identifiers, commit messages, API/CLI/UI labels, and error messages.

## Product Truth

- Treat `docs/C_PLANE_PRD.md` as the current product source of truth.
- Preserve the C-Plane direction: Go API, SQLite MVP, Go agent, MQTT signal, HTTPS job detail, symlink releases, dashboard rollback from retained releases.

For deeper local tooling notes, read `.codex/skills/cplane-token-economy/references/tooling.md` only when needed.
