---
name: cplane-token-economy
description: Use when working inside the C-Plane project and you need to reduce token use during repo exploration, command execution, code review, PRD updates, or implementation. Combines project-local guidance inspired by RTK, code-review-graph, ECC, and caveman without requiring global setup.
---

# C-Plane Token Economy

Goal: get enough context to act without repeatedly loading broad files, noisy command output, or full external docs.

## Default Workflow

1. Discover shape:
   - `rg --files`
   - `rg -n "<term>" docs src test .`
   - targeted `sed -n 'start,endp' <file>`
2. For code review or broad changes, use `code-review-graph` if available.
3. For noisy shell output, use `rtk` if available.
4. Keep user updates short; keep final answers focused on changed files and verification.
5. Load `references/tooling.md` only when deciding whether/how to use RTK, code-review-graph, ECC patterns, or caveman-style compression.

## Project Defaults

- Treat `docs/C_PLANE_PRD.md` as product truth until implementation files exist.
- Preserve C-Plane architecture: Go API, SQLite MVP, MQTT signal, HTTPS job detail, Go agent, symlink releases, dashboard rollback from retained releases.
- Do not add unrelated global config to the user's machine unless explicitly requested.
- If a tool appears globally installed, prefer explicit command use in this project over installing again.

## Compact Commands

Prefer these when available:

```bash
rtk git status
rtk git diff
rtk read docs/C_PLANE_PRD.md
rtk grep "rollback" docs
rtk test <test-command>
code-review-graph build
```

If the command is missing, fall back to normal `git`, `rg`, `sed`, and project test commands.

## Response Style

- Conversation may follow the user's language, including Indonesian.
- Repository artifacts must use English: documentation, code comments, identifiers, commit messages, changelog/release notes, API/CLI/UI labels, and error messages.
- If non-English product copy is requested, keep implementation identifiers and comments in English, and isolate translated user-facing copy in explicit locale/content files.
- Use concise, direct prose.
- For implementation work, report:
  - what changed
  - where
  - what was verified
  - anything not verified
