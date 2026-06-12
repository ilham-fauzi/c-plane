# Token-Saving Tooling Notes

Use this as a short local reference. It intentionally avoids copying full upstream docs.

## RTK

Source: https://github.com/rtk-ai/rtk

Purpose: compact shell command output before it enters the assistant context.

Best use in C-Plane:

```bash
rtk git status
rtk git diff
rtk read <file>
rtk grep "<pattern>" .
rtk test <test-command>
rtk tsc
rtk go test
rtk curl <url>
rtk gain
```

Notes:

- Upstream describes RTK as a CLI proxy for reducing LLM token consumption on common dev commands.
- Codex setup exists via `rtk init -g --codex`, but do not run global init unless the user asks.
- Prefer explicit `rtk ...` commands in this project when available.
- If missing, use normal commands with tight ranges and filters.

## code-review-graph

Source: https://github.com/tirth8205/code-review-graph

Purpose: local-first code intelligence graph for reviews and large-repo workflows. It builds a persistent structural map so the agent can read only relevant files.

Best use in C-Plane:

```bash
code-review-graph build
code-review-graph install --platform codex
```

Notes:

- Upstream quick start uses `pip install code-review-graph` or `pipx install code-review-graph`, then `code-review-graph install` and `code-review-graph build`.
- Do not install or configure globally unless the user asks.
- Most useful once this repo has real source files, tests, and dependency edges.
- For PRD-only work, targeted `rg`/`sed` is cheaper than graph setup.

## ECC

Source: https://github.com/affaan-m/ecc

Purpose: broad agent harness patterns: skills, memory, research-first development, security, token optimization, parallelization, and cross-harness workflows.

Best use in C-Plane:

- Keep project-local skill/reference files small.
- Use progressive disclosure: root `AGENTS.md` -> small skill -> targeted references.
- Prefer reusable scripts only when a task repeats or needs deterministic behavior.
- Avoid importing ECC wholesale into this project.

## caveman

Source: https://github.com/juliusbrussee/caveman

Purpose: compress assistant output style while keeping technical accuracy.

Best use in C-Plane:

- Use concise Indonesian by default.
- Cut filler and long apologies.
- Keep code, paths, commands, and exact technical terms intact.
- Do not force parody/caveman style unless the user asks; use the principle, not the persona.

## Decision Table

Use `rtk` when command output is noisy.

Use `code-review-graph` when reviewing code impact, dependencies, or a large change set.

Use ECC-inspired skill structure when adding durable project knowledge.

Use caveman-inspired compression when conversation output is getting too verbose.

## Project-Local Agent Files

Current C-Plane adapters:

- Codex/general: `AGENTS.md`
- Codex skill: `.codex/skills/cplane-token-economy/SKILL.md`
- Claude Code: `CLAUDE.md`
- Gemini CLI: `GEMINI.md`
- Google Antigravity: `.agents/rules/antigravity-token-economy.md`
- OpenCode: `.opencode/AGENTS.md`

These files are intentionally local notes. They do not install global hooks or rewrite commands automatically.

## Language Policy

- Conversation may follow the user's language.
- Repository artifacts must use English:
  - documentation
  - code comments
  - identifiers and constants
  - commit messages
  - changelog/release notes
  - API, CLI, UI, and error messages
- Keep translated product copy in explicit locale/content files when needed.
