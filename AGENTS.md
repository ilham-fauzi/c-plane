# C-Plane Agent Notes

Use token-efficient project exploration by default.

## Token Economy

- Start with `rg --files`, targeted `rg -n`, and short `sed -n` ranges before reading whole files.
- Prefer compact command wrappers when available:
  - `rtk git status`, `rtk git diff`, `rtk read`, `rtk grep`, `rtk test <cmd>`
  - `code-review-graph build` and graph/blast-radius queries for review or large change analysis
- Do not run global setup commands unless the user asks. Prefer project-local notes and explicit command use.
- Keep summaries short and preserve exact file/line references.

## Language Policy

- Conversation may follow the user's language, including Indonesian.
- Repository artifacts must use English:
  - documentation
  - code comments
  - identifiers, names, labels, and constants
  - commit messages and changelog/release notes
  - API, CLI, UI, and error messages
- If the user requests non-English product copy, keep implementation identifiers and comments in English, and isolate translated user-facing copy in explicit locale/content files.

## C-Plane Product Context

C-Plane is a lightweight CI/CD control plane. Preserve the PRD direction:

- Go control plane API with SQLite MVP.
- Go host agent over MQTT signal + HTTPS job detail.
- Dashboard for hosts, repositories, apps, deployment jobs, logs, release history, rollback, and audit.
- Rollback uses retained host-side release directories and should not require GitHub/repo access when the release still exists.

For reusable token-saving workflow details, read `.codex/skills/cplane-token-economy/SKILL.md`.
