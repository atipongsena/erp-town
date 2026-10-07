# ERP Town — agent instructions

ERP Town is an ERP (ERPNext v16 + HRMS) presented as an office-simulation game. This repo is a pnpm
monorepo worked on alternately by Claude Code and Codex; this file is the single source of
instructions for both (`CLAUDE.md` imports it).

- Program overview: `docs/superpowers/specs/2026-10-07-program-overview.md`
- Foundation (sub-project B) spec: `docs/superpowers/specs/2026-10-07-foundation-design.md`
- Foundation plan (current work, tick checkboxes as you go): `docs/superpowers/plans/2026-10-07-foundation.md`

## Layout

- `packages/*` — TS workspace packages (e.g. `@erp-town/event-schema`)
- `frappe-apps/erp_town` — Frappe app (Python, GPLv3)
- `infra/docker` — dev/pilot compose stack; `scripts/erp.mjs` — wrapper for it
- `tests/integration`, `tests/e2e` — Vitest integration and Playwright e2e against the running stack

## Commands

Requires Node >= 24, pnpm 10, Docker Desktop. Run from the repo root.

| Command | What it does |
| --- | --- |
| `pnpm install` | install workspace dependencies |
| `pnpm lint` / `pnpm format` | Biome check / Biome check with fixes |
| `pnpm test` | `pnpm -r test && pnpm erp:test` (package unit tests + Python tests) |
| `pnpm test:integration` | Vitest integration tests against the dev stack |
| `pnpm test:e2e` | Playwright e2e (`tests/e2e`, base URL `http://localhost:8080`) |
| `pnpm schema:gen` | regenerate TS types and copy event schema into the Frappe app |
| `pnpm erp:up` | `docker compose -f infra/docker/compose.yaml --profile dev up -d`, then wait for `/api/method/ping` (first boot 15–30 min) |
| `pnpm erp:down` | stop the dev stack |
| `pnpm erp:logs [service]` | follow logs |
| `pnpm erp:shell` | bash in the `bench` container at `/home/frappe/frappe-bench` |
| `pnpm erp:bench <args...>` | `bench <args>` inside the `bench` container |
| `pnpm erp:test [--module m]` | `bench --site test.localhost run-tests --app erp_town [--module m]` |
| `pnpm erp:seed` | `bench --site town.localhost execute erp_town.setup.seed.run` |
| `pnpm erp:scenario [--days N] [--bulk N]` | `bench --site town.localhost execute erp_town.setup.scenario.run --kwargs '{"days":N,"bulk":N}'` |

Pass options after `--` with pnpm (e.g. `pnpm erp:test -- --module erp_town.tests.test_smoke`), or call
`node scripts/erp.mjs <command> [args...]` directly; `node scripts/erp.mjs --help` lists everything.
`erp.mjs` exits with the child's exit code.

## Rules

- Follow the plan's **Global Constraints** section (versions, site names, stream/outbox settings,
  error codes, event rules, Thai field prefix `th_`, document numbering, licensing, LF line endings).
- TDD: write the failing test first, see it fail, implement, see it pass.
- Never commit secrets: `.env` files stay local (`infra/docker/.env.example` documents keys);
  API keys/secrets printed by seed go only into your local `.env`.
- Sites: `town.localhost` = dev (default site, Desk at `http://localhost:8080`);
  `test.localhost` = tests only (`allow_tests: true`). Never run tests against `town.localhost`.
- Do not hand-edit generated files (`packages/event-schema/src/generated.ts`,
  `frappe-apps/erp_town/erp_town/events/schema/`); run `pnpm schema:gen`.
- `pnpm lint` must pass before committing.

## Progress convention (git is the cross-agent record)

`git log` is the shared record between agents. Every commit message uses this format and ends with
the trailer block, followed by the `Co-Authored-By` line of the agent that wrote it:

```
<type>(<scope>): <summary>

<what changed / why>
Next: <only when Status is partial>

Plan: foundation
Task: 7
Agent: codex
Status: done
```

- `Agent`: `claude` or `codex`. `Status`: `done`, `partial` (must include a `Next:` line) or `blocked`.
- Claude commits end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>` after the trailer block.
- Resume point: `git log --grep="Plan: foundation" --format="%h %s%n%b" -n 20`
- Subagent-driven-development working notes (briefs, reports) live in `.superpowers/sdd/`, which is
  git-ignored and local only; anything other agents need must be in a commit.

## MCP

Blender MCP (`mcp-for-blender`, telemetry disabled) is configured for Claude in `.mcp.json` and for
Codex in `.codex/config.toml`; keep the two in sync.
