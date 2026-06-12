# C-Plane OpenCode Notes

Follow `../AGENTS.md` for the canonical project instructions.

## Token Economy

- Start narrow: `rg --files`, targeted `rg -n`, short `sed -n` ranges.
- Prefer explicit RTK commands when available:
  - `rtk git status`
  - `rtk git diff`
  - `rtk read <file>`
  - `rtk grep "<pattern>" .`
  - `rtk test <cmd>`
- Use `code-review-graph build` before large review or dependency-impact tasks once source files exist.
- Avoid global init/config changes unless the user asks.

## Language Policy

- Conversation may follow the user's language.
- Repository artifacts must use English: documentation, code comments, identifiers, commit messages, API/CLI/UI labels, and error messages.

## Product Truth

Use `../docs/C_PLANE_PRD.md` as the product source of truth.
