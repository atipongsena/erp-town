# B: Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Codex: read `AGENTS.md` first; the same rules apply.

**Goal:** ERPNext v16 + HRMS running in Docker with a Frappe app `erp_town` that emits sales-flow events through an outbox to Redis Streams, Thai localization for the sales flow, a shared event schema package, and a monorepo both Claude Code and Codex can work in.

**Architecture:** Dev runs one `bench` container (frappe/bench image, bench in a named volume, `frappe-apps/erp_town` bind-mounted) plus MariaDB and three Redis instances. Document hooks write events into `Town Event Outbox` inside the document's transaction; an RQ job enqueued after commit (plus a 1-minute sweeper) `XADD`s them to `town:events` on `redis-town`. JSON Schema in `packages/event-schema` is the contract; it is copied into the app and validated in both Python and TS.

**Tech Stack:** Frappe/ERPNext/HRMS `version-16` (Python 3.14, Node 24), MariaDB 11, Redis 7, pnpm workspaces, TypeScript, ajv, Vitest, Biome, Playwright, ioredis, python-ulid, jsonschema.

**Spec:** `docs/superpowers/specs/2026-10-07-foundation-design.md` (program context: `docs/superpowers/specs/2026-10-07-program-overview.md`)

## Global Constraints

- Frappe/ERPNext/HRMS branch `version-16`; Python `>=3.14,<3.15`; Node `>=24`.
- Site names: `town.localhost` (dev, default site), `test.localhost` (tests, `allow_tests: true`).
- Desk at `http://localhost:8080`; `redis-town` exposed only at `127.0.0.1:6380` in dev.
- Stream name default `town:events`; stream retention 7 days; outbox retention 400 days; `max_attempts` 10; overdue job hour 8.
- Envelope `specversion` `"1.0"`, `schema_version` `1`; event `id` is a ULID.
- Error codes (exact): `EVENT_INVALID`, `OUTBOX_PUBLISH_FAILED`, `ON_BEHALF_OF_REJECTED`, `ON_BEHALF_OF_INVALID`.
- Slice-1 event `data` never contains money amounts or personal names; parties are referenced by code.
- A failure in the town layer must never block a document submit/cancel (tax-data validation is ERP, may block).
- Thai custom fields use prefix `th_`. Document numbers: `<prefix><BE yy><mm>-<####>`, prefixes SO, DN, IV, RC; month/year taken from the document date.
- `frappe-apps/erp_town` is GPLv3 and contains no code from other services.
- All files LF line endings (`.gitattributes` already enforces).
- Every commit ends with the trailer block from `AGENTS.md` (`Plan: foundation`, `Task: <n>`, `Agent: <claude|codex>`, `Status: <done|partial|blocked>`) followed by the `Co-Authored-By` line for the agent that wrote it.

## Review Focus

1. `redis-town` URL wrong or Redis unreachable at submit time → submit succeeds, row stays `Pending` with `last_error` (test in Task 8).
2. Amended documents (`IV6910-0001-1`) → new subject with `seq` starting at 1, `correlation_id` of the original chain (test in Task 7).
3. Thai text inside payloads → round-trips through `XADD`/`XRANGE` byte-exact, JSON stored with `ensure_ascii=False` (test in Task 8).
4. Back-dated documents (posting date in a previous month) → number uses the document's month/BE year, not today's (test in Task 10).
5. Container clock in UTC → envelope `time` still carries the system timezone offset `+07:00` (test in Task 6).

## Progress tracking (both agents)

- Tick checkboxes in this file as steps complete and commit the tick together with the work.
- Resume point: `git log --grep="Plan: foundation" --format="%h %s%n%b" -n 20` shows the last task, status and handoff notes. A `Status: partial` commit must include a `Next:` line in its body.

---

## File Structure

```
AGENTS.md, CLAUDE.md, .codex/config.toml, biome.json, pnpm-workspace.yaml, package.json, tsconfig.base.json
scripts/erp.mjs                         ← cross-platform wrapper for docker compose / bench commands
infra/docker/compose.yaml               ← services + profiles dev/test/pilot
infra/docker/dev/init-bench.sh          ← idempotent bench + site bootstrap (runs inside bench container)
infra/docker/.env.example
infra/docker/pilot/{Containerfile,apps.json,Caddyfile,backup.sh}
packages/event-schema/
  schema/envelope.json, schema/events/<type>.json
  fixtures/valid/*.json, fixtures/invalid/*.json
  scripts/gen.mjs                       ← TS types + copy schema/fixtures into the Frappe app
  src/index.ts, src/generated.ts (generated), test/contract.test.ts
frappe-apps/erp_town/
  pyproject.toml, LICENSE (GPLv3), README.md
  erp_town/__init__.py, hooks.py, modules.txt, patches.txt
  erp_town/erp_town/                    ← module "ERP Town": doctype/, report/, workspace/, number_card/
  erp_town/thai_localization/           ← module "Thai Localization": print_format/, report/
  erp_town/events/                      ← ulid.py envelope.py actor.py seq.py schema.py emit.py lineage.py
                                          system.py publisher.py redis_client.py workflow.py stock.py jobs.py api.py
                                          mappers/{__init__,sales_order,delivery_note,sales_invoice,payment_entry}.py
                                          schema/ (generated copy)
  erp_town/thai/                        ← bahttext.py taxid.py dates.py naming.py setup.py jinja.py
  erp_town/setup/                       ← install.py seed.py scenario.py charts/th_tfrs_npaes.json
  erp_town/public/css/town-desk.css, erp_town/public/fonts/Sarabun-*.ttf
  erp_town/tests/                       ← test_*.py
tests/integration/*.test.ts, tests/e2e/print-formats.spec.ts
```

---

### Task 1: Monorepo + AI tooling

**Files:**
- Create: `AGENTS.md`, `CLAUDE.md`, `.codex/config.toml`, `pnpm-workspace.yaml`, `biome.json`, `tsconfig.base.json`, `scripts/erp.mjs`
- Modify: `package.json` (convert to pnpm workspace root), `playwright.config.ts` (`testDir: './tests/e2e'`, `baseURL: 'http://localhost:8080'`)
- Delete: `package-lock.json`, `tests/example.spec.ts`

**Interfaces:**
- Produces: root scripts `lint`, `format`, `test` (= `pnpm -r test && pnpm erp:test`), `test:integration`, `test:e2e`, `schema:gen`, `erp:up`, `erp:down`, `erp:logs`, `erp:shell`, `erp:bench`, `erp:test`, `erp:seed`, `erp:scenario` (the `erp:*` ones call `node scripts/erp.mjs <cmd> [...args]`).
- `scripts/erp.mjs` commands: `up` (`docker compose -f infra/docker/compose.yaml --profile dev up -d` then wait for `http://localhost:8080/api/method/ping`), `down`, `logs [service]`, `shell`, `bench <args...>` (runs `bench <args>` in `/home/frappe/frappe-bench` of service `bench`), `test [--module m]` (`bench --site test.localhost run-tests --app erp_town [--module m]`), `seed` (`bench --site town.localhost execute erp_town.setup.seed.run`), `scenario [--days N] [--bulk N]` (`bench --site town.localhost execute erp_town.setup.scenario.run --kwargs '{"days":N,"bulk":N}'`). Exit code = child exit code.

- [x] **Step 1: Write `AGENTS.md`** containing: project one-liner + links to both specs and this plan; the commands above; rules (Global Constraints of this plan, TDD, no secrets in git, Desk/test site names); the git progress convention:

```
<type>(<scope>): <summary>

<what changed / why>
Next: <only when Status is partial>

Plan: foundation
Task: 7
Agent: codex
Status: done
```

and the resume command `git log --grep="Plan: foundation" ...`. `CLAUDE.md` is one line `@AGENTS.md`.

- [x] **Step 2: Write `.codex/config.toml`** mirroring `.mcp.json`:

```toml
[mcp_servers.blender]
command = "cmd"
args = ["/c", "uvx", "mcp-for-blender"]
env = { DISABLE_TELEMETRY = "true", PYTHONIOENCODING = "utf-8" }
```

- [x] **Step 3: Convert root to pnpm workspace** (`packages: ["packages/*"]`), devDependencies `@biomejs/biome`, `typescript`, `vitest`, `@playwright/test`, `@types/node`; `"type": "module"`; `"engines": {"node": ">=24"}`. Delete `package-lock.json` and `tests/example.spec.ts`.
- [x] **Step 4: Verify**

Run: `pnpm install && pnpm lint && node scripts/erp.mjs --help`
Expected: install succeeds, Biome reports no errors, help lists all commands.

- [x] **Step 5: Commit** (`chore: set up pnpm monorepo and shared agent tooling`, trailers `Task: 1`)

---

### Task 2: Event schema package

**Files:**
- Create: `packages/event-schema/{package.json,tsconfig.json}`, `schema/envelope.json`, `schema/events/*.json` (one per type in spec §3.3 plus `erp.<doctype>.cancelled` for SO/DN/SI/PE and `system.error`), `fixtures/valid/*.json`, `fixtures/invalid/*.json`, `scripts/gen.mjs`, `src/index.ts`, `test/contract.test.ts`

**Interfaces:**
- Produces (TS, `@erp-town/event-schema`): `type TownEvent` (generated union), `validateEvent(e: unknown): { valid: true } | { valid: false; errors: string[] }`, `EVENT_TYPES: readonly string[]`.
- Produces (files): `pnpm schema:gen` writes `src/generated.ts` and copies `schema/` and `fixtures/` to `frappe-apps/erp_town/erp_town/events/schema/` with a `GENERATED — edit packages/event-schema` README.

Schema rules: envelope required fields exactly as spec §3.1; `additionalProperties: false` on envelope; `actor.kind` enum `user|agent|system|dev`; `actor.on_behalf_of` must be `null` unless `kind == "agent"` (use `if/then`); `seq` integer ≥ 1; `visibility.level` enum `public|roles|private`, `roles` non-empty when level is `roles`; per-type `data` schemas from spec §3.3 with `additionalProperties: false`; a shared `money` definition that is only allowed when `visibility.level != "public"` (not used by any slice-1 type, but present and tested); `fill_ratio` number ≥ 0 with 2 decimals (`multipleOf: 0.01`).

- [x] **Step 1: Write fixtures + failing contract test**

```ts
// test/contract.test.ts
for (const f of readdir('fixtures/valid')) it(`valid: ${f}`, () => expect(validateEvent(load(f)).valid).toBe(true))
for (const f of readdir('fixtures/invalid')) it(`invalid: ${f}`, () => expect(validateEvent(load(f)).valid).toBe(false))
it('covers every event type with a valid fixture', () => expect(new Set(validTypes)).toEqual(new Set(EVENT_TYPES)))
```

Invalid fixtures at minimum: missing `id`; `actor.kind: "user"` with `on_behalf_of` set; `seq: 0`; `visibility.level: "roles"` with empty roles; unknown `data` field on `erp.sales_order.submitted`; a `grand_total` in a public event; `fill_ratio: 0.333`.

- [x] **Step 2: Run** `pnpm --filter @erp-town/event-schema test` → FAIL (`validateEvent` not defined)
- [x] **Step 3: Implement** `src/index.ts` with ajv `Ajv2020` + `ajv-formats` (`date-time`), schemas loaded via `import ... with { type: 'json' }`; `gen.mjs` uses `json-schema-to-typescript`.
- [x] **Step 4: Run** `pnpm schema:gen && pnpm --filter @erp-town/event-schema test` → PASS; `git status` shows generated files under `frappe-apps/erp_town/erp_town/events/schema/`
- [x] **Step 5: Commit** (`feat(event-schema): add town event contract with fixtures`, `Task: 2`)

---

### Task 3: Dev infrastructure + app skeleton

**Files:**
- Create: `infra/docker/compose.yaml`, `infra/docker/dev/init-bench.sh`, `infra/docker/.env.example`
- Create: `frappe-apps/erp_town/{pyproject.toml,LICENSE,README.md}`, `erp_town/{__init__.py,hooks.py,modules.txt,patches.txt}`, `erp_town/erp_town/__init__.py`, `erp_town/thai_localization/__init__.py`, `erp_town/tests/__init__.py`, `erp_town/tests/test_smoke.py`
- Modify: `.gitignore` (add `infra/docker/.env`)

**Interfaces:**
- Produces: compose service names `bench`, `mariadb`, `redis-cache`, `redis-queue`, `redis-town`; bench path `/home/frappe/frappe-bench`; repo mounted at `/workspace`; `frappe-apps/erp_town` mounted at `/home/frappe/frappe-bench/apps/erp_town`; site config key `town_redis_url = "redis://redis-town:6379/0"` on both sites.
- `pyproject.toml` dependencies: `python-ulid>=3,<4`, `jsonschema>=4.23,<5`.
- `modules.txt`: `ERP Town`, `Thai Localization`.

Compose details: `bench` uses `frappe/bench:latest`, command `bash /workspace/infra/docker/dev/init-bench.sh && cd /home/frappe/frappe-bench && bench start`, ports `127.0.0.1:8080:8000` and `127.0.0.1:9000:9000`, env `WATCHFILES_FORCE_POLLING=true`, `CHOKIDAR_USEPOLLING=true`; named volume `bench-data` at `/home/frappe/frappe-bench`; `mariadb:11.4` with utf8mb4 config (`--character-set-server=utf8mb4 --collation-server=utf8mb4_unicode_ci --skip-character-set-client-handshake`) and healthcheck; `redis-town` `redis:7-alpine --appendonly yes --appendfsync everysec`, port `127.0.0.1:6380:6379`, volume `redis-town-data`.

`init-bench.sh` (idempotent; each block guarded by an existence check):
1. If `/home/frappe/frappe-bench/apps/frappe` missing: `bench init --skip-redis-config-generation --frappe-branch version-16 --python <python3.14 path>` into a temp dir and move contents in (the volume mountpoint already exists). Check `pyenv versions`; if 3.14 is absent install it with `uv python install 3.14`.
2. `bench set-config -g db_host mariadb`, `redis_cache redis://redis-cache:6379`, `redis_queue redis://redis-queue:6379`, `redis_socketio redis://redis-queue:6379`, `bench set-config -g serve_default_site true`.
3. `bench get-app --branch version-16 erpnext` and `hrms` if missing.
4. `./env/bin/pip install -e apps/erp_town` and append `erp_town` to `sites/apps.txt` if missing.
5. For each of `town.localhost` and `test.localhost`: `bench new-site` with `--db-root-password "$DB_ROOT_PASSWORD" --admin-password "$ADMIN_PASSWORD" --mariadb-user-host-login-scope=%` if missing, then `install-app erpnext hrms erp_town` (skip already-installed), `set-config town_redis_url redis://redis-town:6379/0`; on `test.localhost` also `set-config allow_tests true`. `bench use town.localhost`. `bench --site all set-config developer_mode 1` on town only.
6. Run `bench --site all migrate` on every start (idempotent).

Spec §6.3's `test` profile is realised as the separate site `test.localhost` on the dev stack (same containers, isolated database); no extra compose profile is needed.

- [x] **Step 1: Write failing smoke test** `erp_town/tests/test_smoke.py`:

```python
from frappe.tests import IntegrationTestCase
import frappe
class TestSmoke(IntegrationTestCase):
    def test_apps_installed(self):
        self.assertTrue({"frappe", "erpnext", "hrms", "erp_town"} <= set(frappe.get_installed_apps()))
    def test_town_redis_url_configured(self):
        self.assertEqual(frappe.conf.town_redis_url, "redis://redis-town:6379/0")
```

- [x] **Step 2: Run** `pnpm erp:up` (first run takes 15–30 min) then `pnpm erp:test -- --module erp_town.tests.test_smoke`
Expected before skeleton exists: install-app fails / module not found.
- [x] **Step 3: Implement** compose, init script, app skeleton (`hooks.py` with `app_name`, `app_title = "ERP Town"`, `app_publisher`, `app_license = "GPL-3.0-only"`, `required_apps = ["erpnext", "hrms"]`).
- [x] **Step 4: Verify**

Run: `pnpm erp:up && curl -s http://localhost:8080/api/method/ping && pnpm erp:test -- --module erp_town.tests.test_smoke && pnpm erp:down && pnpm erp:up`
Expected: `{"message":"pong"}`; 2 tests pass; second `up` completes in < 2 min (idempotent init).

- [x] **Step 5: Commit** (`feat(infra): dev compose stack and erp_town app skeleton`, `Task: 3`)

---

### Task 4: Thai utilities (bahttext, tax ID, BE dates)

**Files:**
- Create: `erp_town/thai/{__init__,bahttext,taxid,dates,jinja}.py`, `erp_town/tests/test_thai_utils.py`
- Modify: `hooks.py` (`jinja = {"filters": ["erp_town.thai.jinja.be_date", "erp_town.thai.jinja.bahttext"]}`)

**Interfaces:**
- Produces: `bahttext(amount: Decimal | float | int | str) -> str`; `is_valid_tax_id(value: str) -> bool`; `be_date(value: date | datetime | str, fmt: Literal["long", "short"] = "long") -> str`.

Rules: amounts rounded to 2 decimals with `ROUND_HALF_UP` via `Decimal(str(x))`; negative prefix `ลบ`; tax ID checksum: `s = Σ_{i=0..11} d[i]·(13−i)`, check digit `(11 − s mod 11) mod 10` equals `d[12]`; only 13 ASCII digits are valid (strip nothing). `long` → `7 ตุลาคม 2569`, `short` → `07/10/2569`.

- [x] **Step 1: Write failing tests**

```python
CASES = [(0,"ศูนย์บาทถ้วน"),(1,"หนึ่งบาทถ้วน"),(10,"สิบบาทถ้วน"),(11,"สิบเอ็ดบาทถ้วน"),
 (20,"ยี่สิบบาทถ้วน"),(21,"ยี่สิบเอ็ดบาทถ้วน"),(100,"หนึ่งร้อยบาทถ้วน"),(101,"หนึ่งร้อยเอ็ดบาทถ้วน"),
 (120,"หนึ่งร้อยยี่สิบบาทถ้วน"),("1250.50","หนึ่งพันสองร้อยห้าสิบบาทห้าสิบสตางค์"),
 (1_000_000,"หนึ่งล้านบาทถ้วน"),(2_000_000,"สองล้านบาทถ้วน"),
 ("21000021.21","ยี่สิบเอ็ดล้านยี่สิบเอ็ดบาทยี่สิบเอ็ดสตางค์"),("0.25","ยี่สิบห้าสตางค์"),
 ("0.01","หนึ่งสตางค์"),(1.005,"หนึ่งบาทหนึ่งสตางค์"),(10_000_000,"สิบล้านบาทถ้วน"),
 (1_000_000_000_000,"หนึ่งล้านล้านบาทถ้วน"),(-5,"ลบห้าบาทถ้วน"),("100000.10","หนึ่งแสนบาทสิบสตางค์")]
def test_bahttext(self):
    for amount, words in CASES: self.assertEqual(bahttext(amount), words, amount)
def test_tax_id(self):
    self.assertTrue(is_valid_tax_id("0105536112014"))   # compute a valid fixture with the formula, keep it in the test
    self.assertFalse(is_valid_tax_id("0105536112015")); self.assertFalse(is_valid_tax_id("010553611201"))
    self.assertFalse(is_valid_tax_id("01055361120a4"))
def test_be_date(self):
    self.assertEqual(be_date(date(2026,10,7)), "7 ตุลาคม 2569")
    self.assertEqual(be_date("2026-01-05", "short"), "05/01/2569")
```

(If `0105536112014` is not valid under the formula, replace it with a number the formula validates and its last-digit-changed twin — the assertion structure stays.)

- [x] **Step 2: Run** `pnpm erp:test -- --module erp_town.tests.test_thai_utils` → FAIL (import error)
- [x] **Step 3: Implement** the three modules and Jinja wrappers.
- [x] **Step 4: Run** same command → PASS
- [x] **Step 5: Commit** (`feat(thai): bahttext, tax id checksum, BE date filters`, `Task: 4`)

---

### Task 5: Town DocTypes

**Files:**
- Create: `erp_town/erp_town/doctype/{town_event_outbox,town_subject_seq,town_agent_identity,town_settings,town_naming_rule}/` (JSON + `.py` controller + `__init__.py`), `erp_town/tests/test_doctypes.py`

**Interfaces:**
- Produces DocTypes with fields exactly as spec §4.1. Additional decisions:
  - `Town Event Outbox`: autoname `field:event_id`; `payload` type `JSON`; `status` Select default `Pending`; `attempts` Int default 0; `next_attempt_at` Datetime; DB indexes via `search_index` on `status`, `next_attempt_at`, `correlation_id`; composite unique index (`subject_doctype`, `subject_name`, `seq`) added in controller `on_doctype_update()` with `frappe.db.add_unique`. Permissions: System Manager read only; no create/write from Desk (`in_create: 1`, read-only fields).
  - `Town Subject Seq`: autoname `hash`; unique composite (`subject_doctype`, `subject_name`) via `on_doctype_update`.
  - `Town Agent Identity`: autoname `field:agent_key`; `user` Link User unique.
  - `Town Settings` (Single): fields from spec + `approval_states` (Small Text, default `รออนุมัติ`) + table `naming_rules` → child `Town Naming Rule` (`document_type` Link DocType, `prefix` Data). `get_town_settings() -> frappe._dict` in `erp_town/erp_town/doctype/town_settings/town_settings.py`, cached per request; `redis_town_url` falls back to `frappe.conf.town_redis_url`.

- [x] **Step 1: Write failing tests**: inserting two outbox rows with the same (`subject_doctype`, `subject_name`, `seq`) raises `frappe.UniqueValidationError`; `get_town_settings()` returns defaults (`stream_name == "town:events"`, `max_attempts == 10`, `stream_retention_days == 7`, `outbox_retention_days == 400`, `overdue_job_hour == 8`); `Town Agent Identity` with duplicate `user` raises.
- [x] **Step 2: Run** `pnpm erp:test -- --module erp_town.tests.test_doctypes` → FAIL
- [x] **Step 3: Implement** DocTypes; run `pnpm erp:bench -- --site all migrate`.
- [x] **Step 4: Run** tests → PASS
- [x] **Step 5: Commit** (`feat(erp_town): outbox, seq, agent identity and settings doctypes`, `Task: 5`)

---

### Task 6: Event core (`emit_raw`)

**Files:**
- Create: `erp_town/events/{__init__,ulid,envelope,actor,seq,schema,emit,system}.py`, `erp_town/tests/test_event_core.py`

**Interfaces:**
- Consumes: Task 2 schema copy at `erp_town/events/schema/`; Task 5 DocTypes and `get_town_settings()`.
- Produces:
  - `new_ulid() -> str`
  - `resolve_actor(default_kind: str = "user") -> dict` — `{"kind","id","on_behalf_of"}`; user ids are `user.<User.name>`; scheduler/system context (`frappe.session.user == "Administrator"` inside a scheduled job, signalled by `frappe.flags.town_system_actor = True`) → `{"kind":"system","id":"system.scheduler","on_behalf_of":None}`; agent per spec §4.2 step 2 (header `X-Town-On-Behalf-Of`, value is a User name → `user.<name>`).
  - `next_seq(subject_doctype: str, subject_name: str) -> int` — `SELECT … FOR UPDATE`, insert row if missing.
  - `validate_event(event: dict) -> None` raises `EventSchemaError(message: str)` (jsonschema `Draft202012Validator`, schemas loaded once).
  - `emit_raw(event_type: str, *, source: str, subject_doctype: str, subject_name: str, company: str | None, data: dict, actor: dict | None = None, correlation_id: str | None = None, causation_id: str | None = None, visibility: dict | None = None) -> str` — builds envelope (`time` = `now_datetime()` in system timezone formatted ISO with offset), allocates seq, validates, inserts outbox row (`Pending`, or `Invalid` + `system.error` code `EVENT_INVALID`), registers the publish job via `frappe.enqueue("erp_town.events.publisher.publish", queue="short", event_ids=[id], enqueue_after_commit=True)`, returns event id. Defaults: `visibility = {"level":"public","roles":[]}`, `correlation_id = subject_name`. Never raises for town-layer errors: wraps everything after the outbox insert attempt in try/except → `frappe.log_error`.
  - `emit_system_error(code: str, message: str, *, subject_doctype: str, subject_name: str, company: str | None = None) -> str | None` — calls `emit_raw("system.error", source="erp/town", ...)`; must not recurse if the error event itself is invalid (log only).
  - Payload stored with `frappe.as_json(event, ensure_ascii=False)` (or `json.dumps(..., ensure_ascii=False)`).

- [x] **Step 1: Write failing tests**

```python
def test_emit_raw_writes_pending_valid_row(self):
    eid = emit_raw("erp.stock.below_reorder", source="erp/stock", subject_doctype="Item", subject_name="ITEM-T1",
                   company=None, data={"warehouse": "Stores - T", "fill_ratio": 0.5})
    row = frappe.get_doc("Town Event Outbox", eid)
    self.assertEqual(row.status, "Pending"); self.assertEqual(row.seq, 1)
    ev = json.loads(row.payload); self.assertEqual(ev["specversion"], "1.0"); self.assertEqual(len(ev["id"]), 26)
def test_seq_increments_per_subject(self): ...  # three emits on same subject → 1,2,3; other subject → 1
def test_invalid_event_is_stored_invalid_and_raises_nothing(self): ...  # data={"bogus": 1} → status Invalid, last_error set, one system.error row with data.code == "EVENT_INVALID"
def test_time_has_system_tz_offset(self): ...  # with System Settings time_zone = "Asia/Bangkok": ev["time"].endswith("+07:00")
def test_actor_user_and_rejected_header(self): ...  # normal user + header → actor.on_behalf_of None, system.error ON_BEHALF_OF_REJECTED
def test_actor_agent_with_on_behalf_of(self): ...  # user mapped in Town Agent Identity + header "test@example.com" (exists) → kind agent, on_behalf_of "user.test@example.com"
def test_actor_agent_with_unknown_on_behalf_of(self): ...  # header "nobody@x" → on_behalf_of None, system.error ON_BEHALF_OF_INVALID
def test_rollback_leaves_no_row_and_no_seq(self): ...  # savepoint → emit_raw → rollback(save_point) → no outbox row, Town Subject Seq absent
def test_contract_fixtures(self): ...  # every file in events/schema/fixtures/valid passes validate_event, every file in invalid raises EventSchemaError (spec §9.2)
```

Use `frappe.local.request` mock (`MockedRequestTestCase` or patch `frappe.get_request_header`) for headers.

- [x] **Step 2: Run** `pnpm erp:test -- --module erp_town.tests.test_event_core` → FAIL
- [x] **Step 3: Implement** modules.
- [x] **Step 4: Run** → PASS
- [x] **Step 5: Commit** (`feat(events): envelope, actor, seq and outbox emit`, `Task: 6`)

---

### Task 7: Sales-flow mappers + document hooks

**Files:**
- Create: `erp_town/events/mappers/{__init__,sales_order,delivery_note,sales_invoice,payment_entry}.py`, `erp_town/events/lineage.py`, `erp_town/events/doc_hooks.py`, `erp_town/tests/test_sales_flow_events.py`, `erp_town/tests/factories.py`
- Modify: `hooks.py` (`doc_events` for the 4 doctypes: `on_submit`, `on_cancel` → `erp_town.events.doc_hooks.on_doc_event`). `on_update_after_submit` is not wired in slice 1 because spec §3.3 defines no event type for it; add it together with a schema type when a later slice needs it.

**Interfaces:**
- Consumes: `emit_raw` (Task 6).
- Produces:
  - `class Mapper(Protocol)`: `doctype: str`, `type_prefix: str` (e.g. `"erp.sales_order"`), `source: str` (`erp/selling`, `erp/stock`, `erp/accounts`), `data(doc, action: str) -> dict`, `upstream(doc) -> list[tuple[str, str]]`.
  - `REGISTRY: dict[str, Mapper]` keyed by doctype.
  - `Lineage(correlation_id: str, causation_id: str | None, against: list[str])`; `find_lineage(doc, mapper) -> Lineage` (spec §4.2 step 3; upstream order = item/reference row order, de-duplicated; amended docs: correlation of `amended_from`'s latest event); `latest_event(doctype: str, name: str) -> dict | None` (`{"event_id","correlation_id"}` of max seq row).
  - `on_doc_event(doc, method: str) -> None` maps `on_submit→submitted`, `on_cancel→cancelled` and calls `emit_raw` with `data` from mapper, `against` placed into `data["against"]` for DN/SI/PE.
  - `erp_town/tests/factories.py`: `outbox_events_in_creation_order(correlation_id: str | None = None) -> list[dict]` (parsed payloads), `make_customer()`, `make_item(reorder_level=None)`, `make_so(customer, items)`, `make_dn_from_so(so)`, `make_si_from_dn(dn)`, `make_pe_for_si(si)` using ERPNext's `make_delivery_note`, `make_sales_invoice`, `get_payment_entry`, all for a test company created once in `setUpClass`.

Data fields per spec §3.3 (`status`, `party` = customer code, `item_count` = number of item rows, `delivery_date`, `warehouse` = first item's warehouse, `due_date`, `mode_of_payment`).

- [x] **Step 1: Write failing tests**

```python
def test_full_flow_chain(self):
    so = make_so(...); dn = make_dn_from_so(so); si = make_si_from_dn(dn); pe = make_pe_for_si(si)
    evs = outbox_events_in_creation_order()
    self.assertEqual([e["type"] for e in evs][:4], ["erp.sales_order.submitted","erp.delivery_note.submitted",
                                                     "erp.sales_invoice.submitted","erp.payment_entry.submitted"])
    self.assertTrue(all(e["correlation_id"] == so.name for e in evs[:4]))
    self.assertEqual(evs[1]["causation_id"], evs[0]["id"]); self.assertEqual(evs[2]["causation_id"], evs[1]["id"])
    self.assertEqual(evs[3]["data"]["against"], [si.name])
    for e in evs: self.assertNotIn("grand_total", e["data"]); validate_event(e)
def test_dn_against_two_sos_uses_first_and_lists_all(self): ...
def test_si_without_upstream_is_its_own_root(self): ...      # correlation_id == si.name, causation_id is None
def test_cancel_emits_cancelled_with_next_seq(self): ...     # SO submit then cancel → seq 1, 2
def test_amended_doc_new_subject_same_correlation(self): ... # Review Focus 2: amend cancelled SI → new name, seq 1, correlation == original chain root
def test_mapper_exception_does_not_block_submit(self): ...   # patch mapper.data to raise → SO submitted (docstatus 1), outbox Invalid
```

- [x] **Step 2: Run** `pnpm erp:test -- --module erp_town.tests.test_sales_flow_events` → FAIL
- [x] **Step 3: Implement** mappers, lineage, hooks (wrap whole hook body in try/except → `emit_system_error`, never re-raise).
- [x] **Step 4: Run** → PASS
- [x] **Step 5: Commit** (`feat(events): sales flow mappers with causation lineage`, `Task: 7`)

---

### Task 8: Publisher, sweeper, purge

**Files:**
- Create: `erp_town/events/{redis_client,publisher}.py`, `erp_town/tests/test_publisher.py`
- Modify: `hooks.py` (`scheduler_events = {"cron": {"* * * * *": ["erp_town.events.publisher.sweep"]}, "daily": ["erp_town.events.publisher.purge"]}`)

**Interfaces:**
- Consumes: outbox rows (Task 5/6), `get_town_settings()`.
- Produces:
  - `get_town_redis() -> redis.Redis` (module-level cache keyed by URL; `decode_responses=False`; socket timeouts 2 s).
  - `publish(event_ids: list[str]) -> int` — spec §4.3; selects with `FOR UPDATE SKIP LOCKED`; `xadd(stream, {"event": payload_bytes}, minid=f"{cutoff_ms}-0", approximate=True)`; returns number published; on failure updates attempts/backoff `min(2**attempts, 300)` s; at `attempts >= max_attempts` → `Failed` + `OUTBOX_PUBLISH_FAILED`. Commits its own transaction (it runs as a background job).
  - `sweep() -> int` — rows `Pending`, `creation < now-30s`, `next_attempt_at is null or <= now`, ordered by `creation`, limit 500.
  - `purge() -> int` — delete `Published` older than `outbox_retention_days`.

- [x] **Step 1: Write failing tests** (these talk to the real `redis-town`; each test uses a unique stream name via `Town Settings.stream_name = f"town:test:{frappe.generate_hash(8)}"` and deletes it in `tearDown`):

```python
def test_publish_marks_published_and_roundtrips_thai(self):   # Review Focus 3
    eid = emit_raw(..., data={"status": "รอส่งของ", ...}); self.assertEqual(publish([eid]), 1)
    row = frappe.get_doc("Town Event Outbox", eid); self.assertEqual(row.status, "Published"); self.assertTrue(row.stream_id)
    [(sid, fields)] = get_town_redis().xrange(stream); self.assertEqual(json.loads(fields[b"event"].decode())["data"]["status"], "รอส่งของ")
def test_publish_twice_is_noop(self): ...                       # second publish([eid]) returns 0, stream length 1
def test_redis_down_sets_backoff(self): ...                     # Review Focus 1: settings.redis_town_url = "redis://127.0.0.1:1/0" → publish returns 0, status Pending, attempts 1, last_error set, next_attempt_at ≈ now+2s
def test_max_attempts_marks_failed_and_emits_error(self): ...   # attempts preset to 9 → Failed + system.error OUTBOX_PUBLISH_FAILED
def test_sweep_picks_only_due_rows(self): ...                   # backdate creation 60s; one row with future next_attempt_at is skipped
def test_purge_deletes_only_old_published(self): ...
```

- [x] **Step 2: Run** `pnpm erp:test -- --module erp_town.tests.test_publisher` → FAIL
- [x] **Step 3: Implement**
- [x] **Step 4: Run** → PASS
- [x] **Step 5: Commit** (`feat(events): redis stream publisher with sweeper and retention`, `Task: 8`)

---

### Task 9: Workflow, reorder and overdue events

**Files:**
- Create: `erp_town/events/{workflow,stock,jobs}.py`, `erp_town/tests/test_derived_events.py`
- Modify: `hooks.py` (`doc_events["Sales Order"]["on_change"] = "erp_town.events.workflow.on_change"`; `doc_events["Delivery Note"]["on_submit"]` becomes a list that also calls `erp_town.events.stock.on_dn_submit`; `scheduler_events["cron"]["0 * * * *"] = ["erp_town.events.jobs.emit_overdue_hourly"]`)

**Interfaces:**
- Consumes: `emit_raw`, `latest_event`, `get_town_settings()`.
- Produces:
  - `on_change(doc, method)` — compare `doc.get_doc_before_save().workflow_state` with `doc.workflow_state`; entering a state in `approval_states` → `workflow.approval.requested` (`approver_role` = role on the transition out of that state from the doc's active Workflow); leaving such a state → `workflow.approval.decided` with `decision` = `approved` if new state's `doc_status` is 1 else `rejected`. Causation = latest event of the doc.
  - `on_dn_submit(doc, method)` — per spec §4.2: for each (item, warehouse) in the DN with an `Item Reorder` row for that warehouse: `after = Bin.actual_qty`, `before = after + qty in this DN`; emit `erp.stock.below_reorder` (subject Item, `fill_ratio = round(after / reorder_level, 2)` floored at 0) only when `before >= level > after`; causation = the DN's submitted event id, correlation = DN's correlation.
  - `emit_overdue_hourly()` — runs `emit_overdue()` only when the current hour equals `overdue_job_hour`.
  - `emit_overdue(today: date | None = None) -> int` — spec §4.3; dedupe = no `erp.sales_invoice.overdue` outbox row for that SI with `creation` date == today; actor system (`frappe.flags.town_system_actor = True` during the run); `days_overdue = (today - due_date).days`.

- [x] **Step 1: Write failing tests**: SO with workflow fixture (created in `setUpClass`: states `Draft`(0) → `รออนุมัติ`(0) → `อนุมัติแล้ว`(1) / `ปฏิเสธ`(0), approver role `Sales Manager`) → requested then decided `approved`, and a second SO → `rejected`; DN that crosses reorder level emits exactly one below_reorder with `fill_ratio` and correct causation, a DN that stays above emits none, a DN already below emits none; overdue: SI due yesterday unpaid → 1 event, second call same day → 0, paid SI → 0, `days_overdue` correct.
- [x] **Step 2: Run** `pnpm erp:test -- --module erp_town.tests.test_derived_events` → FAIL
- [x] **Step 3: Implement**
- [x] **Step 4: Run** → PASS
- [x] **Step 5: Commit** (`feat(events): approval, reorder and overdue events`, `Task: 9`)

---

### Task 10: Thai localization setup (fields, taxes, chart, BE numbering)

**Files:**
- Create: `erp_town/thai/{naming,setup}.py`, `erp_town/setup/install.py`, `erp_town/setup/charts/th_tfrs_npaes.json`, `erp_town/tests/test_thai_setup.py`
- Modify: `hooks.py` (`after_install = "erp_town.setup.install.after_install"`, `after_migrate = ["erp_town.thai.setup.ensure_custom_fields", "erp_town.thai.setup.ensure_naming"]`, `naming_series_variables = {"BEYY": "erp_town.thai.naming.be_yy", "BEMM": "erp_town.thai.naming.be_mm"}`, `doc_events` `validate` on Company and Customer → `erp_town.thai.setup.validate_tax_id`)
- Modify: `town_settings.py` `on_update` → `ensure_naming()`

**Interfaces:**
- Consumes: `is_valid_tax_id` (Task 4).
- Produces:
  - `be_yy(doc, part: str) -> str` / `be_mm(doc, part: str) -> str` — date from `doc.posting_date or doc.transaction_date or today`; BE year = Gregorian + 543, last 2 digits.
  - `ensure_naming() -> None` — for each `Town Naming Rule` (defaults SO/DN/IV/RC for Sales Order/Delivery Note/Sales Invoice/Payment Entry) sets Property Setters so `naming_series` options/default = `<prefix>.BEYY.BEMM.-.####`.
  - `ensure_custom_fields() -> None` — `th_branch_code` (Data, length 5, default `00000`, label `รหัสสาขา`) on Company, Customer, Address via `create_custom_fields(..., update=True)`.
  - `validate_tax_id(doc, method)` — if `doc.tax_id` set and invalid → `frappe.throw(_("เลขประจำตัวผู้เสียภาษีไม่ถูกต้อง: {0}").format(doc.tax_id))`.
  - `create_thai_company(company_name: str, abbr: str, tax_id: str) -> str` (in `erp_town/thai/setup.py`) — creates Company with `frappe.local.flags.ignore_chart_of_accounts = True`, then `erpnext.accounts.doctype.account.chart_of_accounts.chart_of_accounts.create_charts(company, custom_chart=<json>)`, sets company default accounts, creates `ภาษีขาย 7%` and `ภาษีขาย 7% (ราคารวม VAT)` Sales Taxes and Charges Templates (account `ภาษีขาย`, rate 7, the second with `included_in_print_rate = 1`), Item Tax Templates `ยกเว้น VAT` and `VAT 0%`, and sets `valuation_method` default `Moving Average` in Stock Settings if unset. Returns company name.

Chart `th_tfrs_npaes.json` (ERPNext chart tree format, `is_group` nodes 1 สินทรัพย์, 2 หนี้สิน, 3 ส่วนของเจ้าของ, 4 รายได้, 5 ค่าใช้จ่าย) must contain at least these leaves with `account_type`: เงินสด (Cash), เงินฝากธนาคาร (Bank), ลูกหนี้การค้า (Receivable), สินค้าคงเหลือ (Stock), ภาษีซื้อ (Tax), เจ้าหนี้การค้า (Payable), ภาษีขาย (Tax), สินค้ารับแล้วยังไม่ได้รับใบแจ้งหนี้ (Stock Received But Not Billed), ทุนจดทะเบียน (Equity), กำไรสะสม (Equity), รายได้จากการขาย (Income Account), ต้นทุนขาย (Cost of Goods Sold), ผลต่างจากการปรับปรุงสต็อก (Stock Adjustment), ปัดเศษ (Round Off), ค่าเสื่อมราคา (Depreciation), ส่วนลดจ่าย (Expense Account). Mark the file header comment/README: "draft — accountant review required".

- [x] **Step 1: Write failing tests**: `create_thai_company("ทดสอบ จำกัด","TT","<valid id>")` creates all listed accounts and both tax templates; SI with `posting_date = 2026-09-30` created on any day gets name starting `IV6909-` (Review Focus 4), next SI in Oct 2026 starts `IV6910-0001` in a fresh series; Customer with invalid tax id → `frappe.ValidationError` whose message contains `เลขประจำตัวผู้เสียภาษีไม่ถูกต้อง`; empty tax id saves; `th_branch_code` exists on the 3 doctypes with default `00000`; changing a Town Naming Rule prefix to `INV` makes the next SI start with `INV69`.
- [x] **Step 2: Run** `pnpm erp:test -- --module erp_town.tests.test_thai_setup` → FAIL
- [x] **Step 3: Implement**
- [x] **Step 4: Run** → PASS, then run the whole suite `pnpm erp:test` → all PASS (BE numbering changes names used in earlier tests)
- [x] **Step 5: Commit** (`feat(thai): company setup, VAT templates, branch codes and BE numbering`, `Task: 10`)

---

### Task 11: Thai print formats + Desk theme

**Files:**
- Create: `erp_town/thai_localization/print_format/{th_sales_order,th_delivery_note,th_tax_invoice,th_receipt}/` (JSON + `.html`), `erp_town/public/css/town-desk.css`, `erp_town/public/css/print-th.css`, `erp_town/public/fonts/Sarabun-{Regular,Bold}.ttf` + `OFL.txt`, `erp_town/translations/th.csv` (Thai labels for every user-facing string the app adds: DocType/field labels, report and workspace names, error messages), `erp_town/tests/test_print_formats.py`
- Modify: `hooks.py` (`app_include_css = "/assets/erp_town/css/town-desk.css"`)

**Interfaces:**
- Consumes: Jinja filters `be_date`, `bahttext` (Task 4); `th_branch_code` (Task 10); factories (Task 7).
- Produces print format names (exact): `ใบยืนยันคำสั่งซื้อ` (Sales Order), `ใบส่งของ` (Delivery Note), `ใบกำกับภาษี/ใบแจ้งหนี้` (Sales Invoice, set as default print format), `ใบเสร็จรับเงิน` (Payment Entry). All `standard: "Yes"`, `print_format_type: "Jinja"`, module `Thai Localization`, font via `@font-face` Sarabun from `/assets/erp_town/fonts/`.
- `town-desk.css` defines only `:root` custom properties (`--town-primary`, `--town-surface`, `--town-radius`, `--town-font-ui`) mapped onto Frappe's variables; neutral values, art bible fills them later.

- [x] **Step 1: Write failing tests** using `frappe.get_print(doctype, name, print_format=...)` HTML:

```python
def test_tax_invoice_has_section_86_4_elements(self):
    html = frappe.get_print("Sales Invoice", si.name, print_format="ใบกำกับภาษี/ใบแจ้งหนี้")
    for needle in ["ใบกำกับภาษี", company_tax_id, customer_tax_id, "สำนักงานใหญ่", si.name,
                   be_date(si.posting_date), bahttext(si.grand_total), "ภาษีมูลค่าเพิ่ม"]:
        self.assertIn(needle, html)
def test_branch_label(self): ...      # th_branch_code "00001" → "สาขาที่ 00001"; "00000" → "สำนักงานใหญ่"
def test_receipt_references_invoice(self): ...  # Payment Entry print contains si.name and bahttext(paid_amount)
def test_dn_and_so_render(self): ...  # contain doc name and be_date
```

- [x] **Step 2: Run** `pnpm erp:test -- --module erp_town.tests.test_print_formats` → FAIL
- [x] **Step 3: Implement** templates + CSS, `pnpm erp:bench -- build --app erp_town`
- [x] **Step 4: Run** → PASS; open `http://localhost:8080/printview?doctype=Sales%20Invoice&name=<seeded SI>&format=ใบกำกับภาษี/ใบแจ้งหนี้` and confirm Thai vowels/tone marks render (manual check, note result in commit body) — automated proxy done (printview page rendered for a test SI, Sarabun font URLs 200, Thai text present); visual check of vowels/tone marks deferred to the controller
- [x] **Step 5: Commit** (`feat(thai): Thai print formats with Sarabun and Desk theme tokens`, `Task: 11`)

---

### Task 12: Sales VAT report

**Files:**
- Create: `erp_town/thai_localization/report/th_output_vat_report/` (JSON `report_type: "Script Report"`, `.py`, `.js` filters), `erp_town/tests/test_output_vat_report.py`

**Interfaces:**
- Produces: report name `รายงานภาษีขาย`; filters `company` (Link, required), `month` (Select 1–12), `year` (Int, Gregorian; label shows BE in `.js`); `execute(filters) -> (columns, data)` with columns (fieldnames exact): `idx`, `posting_date_be`, `invoice_no`, `customer_name`, `customer_tax_id`, `customer_branch`, `net_amount`, `vat_amount`; a final total row `{"invoice_no": "รวม", "net_amount": Σ, "vat_amount": Σ}`. Cancelled SIs in the period appear with `customer_name` suffixed ` (ยกเลิก)` and zero amounts. Source: submitted/cancelled SIs of the company in the month, VAT from tax rows whose account is the company's `ภาษีขาย` account.

- [x] **Step 1: Write failing tests**: three SIs (one VAT-exclusive 1,000 → net 1,000 / vat 70; one VAT-inclusive 1,070 → net 1,000 / vat 70; one cancelled) → 3 rows + total row with net 2,000.00 and vat 140.00; SI in another month excluded; `posting_date_be` = `be_date(..., "short")`.
- [x] **Step 2: Run** `pnpm erp:test -- --module erp_town.tests.test_output_vat_report` → FAIL
- [x] **Step 3: Implement**
- [x] **Step 4: Run** → PASS
- [x] **Step 5: Commit** (`feat(thai): output VAT report`, `Task: 12`)

---

### Task 13: Town Status workspace + requeue

**Files:**
- Create: `erp_town/events/api.py`, `erp_town/erp_town/report/town_outbox_status/` (Script Report + `.js` with "ส่งใหม่" button), `erp_town/erp_town/number_card/{town_pending,town_failed,town_invalid,town_oldest_pending_age}/`, `erp_town/erp_town/workspace/erp_town/erp_town.json`, `erp_town/tests/test_town_status.py`

**Interfaces:**
- Produces: `@frappe.whitelist() requeue_failed(event_ids: list[str] | str) -> int` (System Manager only; sets `Failed → Pending`, `attempts = 0`, `next_attempt_at = None`, `last_error = None`, enqueues `publish`); `@frappe.whitelist() oldest_pending_age_seconds() -> int` (used by the number card, 0 when none).

- [x] **Step 1: Write failing tests**: requeue by System Manager resets fields and returns count; non-System-Manager → `frappe.PermissionError`; `oldest_pending_age_seconds()` returns ≥ 60 for a row backdated 60 s; report returns rows grouped by status.
- [x] **Step 2: Run** `pnpm erp:test -- --module erp_town.tests.test_town_status` → FAIL
- [x] **Step 3: Implement**
- [x] **Step 4: Run** → PASS; workspace "ERP Town" visible in Desk sidebar for Administrator
- [x] **Step 5: Commit** (`feat(erp_town): town status workspace and failed-event requeue`, `Task: 13`)

---

### Task 14: Seed + scenario

**Files:**
- Create: `erp_town/setup/{seed,scenario}.py`, `erp_town/tests/test_seed.py`

**Interfaces:**
- Consumes: `create_thai_company` (Task 10), factories pattern (Task 7), workflow definition shape (Task 9).
- Produces:
  - `seed.run() -> dict` — idempotent; returns counts. Sets System Settings `language = "th"`, `time_zone = "Asia/Bangkok"`, `country = "Thailand"`, `currency = "THB"`. Creates exactly: company `บริษัท ตัวอย่างเทรดดิ้ง จำกัด` abbr `ACME-TH`; fiscal year covering today; warehouse `คลังหลัก`; 20 items `ITEM-001..020` (stock items, UOM `ชิ้น`, `Item Reorder` for คลังหลัก) with opening stock via Stock Reconciliation (once); 10 customers `CUST-001..010` with tax ids valid by checksum (generated deterministically, document the generator) — CUST-009 and CUST-010 have `credit_limit` 5,000; users `sales@acme-th.test` (Sales User), `stock@acme-th.test` (Stock User), `accounts@acme-th.test` (Accounts User), `approver@acme-th.test` (Sales Manager) with password from env `SEED_USER_PASSWORD` (default `town-dev-123`, dev only); custom Check field `th_requires_approval` on Sales Order computed in `validate` (`grand_total + customer outstanding > credit_limit` when a limit exists); Workflow `อนุมัติ SO เกินวงเงิน` (states as Task 9; transitions: Draft→รออนุมัติ when `doc.th_requires_approval`, Draft→อนุมัติแล้ว when not, รออนุมัติ→อนุมัติแล้ว/ปฏิเสธ by Sales Manager); agent users `agent.sales_assistant@acme-th.test` and `agent.ar_collector@acme-th.test` with roles Sales User / Accounts User and `Town Agent Identity` keys `agent.sales_assistant`, `agent.ar_collector`; API key/secret generated only when absent and printed once.
  - `scenario.run(days: int = 7, bulk: int = 0, rng_seed: int = 42) -> dict` — for each simulated day: 3–8 SOs (random customers/items), ~80% get DN, ~70% of DNs get SI, ~60% of SIs paid; some SI due dates in the past so overdue fires; occasional SO to CUST-009/010 to trigger approval; `bulk > 0` additionally creates `bulk` SIs in one transaction batch (commit every 50). Returns counts per doctype.

- [x] **Step 1: Write failing tests** on `test.localhost`: `run()` twice → identical counts and no duplicates; every seeded customer tax id passes `is_valid_tax_id`; workflow exists and an SO for CUST-009 above limit enters `รออนุมัติ`; `scenario.run(days=1, rng_seed=1)` twice with the same seed produces the same number of SOs.
- [x] **Step 2: Run** `pnpm erp:test -- --module erp_town.tests.test_seed` → FAIL
- [x] **Step 3: Implement**
- [x] **Step 4: Run** → PASS; then `pnpm erp:seed && pnpm erp:seed && pnpm erp:scenario -- --days 3` on `town.localhost` succeed
- [x] **Step 5: Commit** (`feat(setup): idempotent seed and scenario generator`, `Task: 14`)

---

### Task 15: Integration, load and print-format e2e tests

**Files:**
- Create: `tests/integration/{vitest.config.ts,setup.ts,helpers.ts,sales-flow.test.ts,redis-outage.test.ts,bulk-load.test.ts}`, `tests/e2e/print-formats.spec.ts`, `tests/e2e/global-setup.ts`
- Modify: root `package.json` (`test:integration` = `vitest run -c tests/integration/vitest.config.ts`; `test:e2e` = `playwright test`; `test` stays as defined in Task 1 so it remains fast)

**Interfaces:**
- Consumes: seed users and agent API keys (Task 14) on `test.localhost`; ioredis to `127.0.0.1:6380`; `@erp-town/event-schema` `validateEvent`.
- `helpers.ts`: `erp(path, init)` — fetch `http://127.0.0.1:8080` with header `X-Frappe-Site-Name: test.localhost` and `Authorization: token <key>:<secret>` from env `TEST_API_KEY`/`TEST_API_SECRET` (created in `setup.ts` via `node scripts/erp.mjs bench --site test.localhost execute erp_town.setup.seed.run` + a helper method that returns Administrator keys for the test site); `readStream(fromId)`; `compose(...args)` running `docker compose -f infra/docker/compose.yaml --profile dev ...`.

- [x] **Step 1: Write the tests**
  - `sales-flow.test.ts`: create SO → submit → make DN/SI/PE via `erpnext.selling.doctype.sales_order.sales_order.make_delivery_note` etc. over REST → within 5 s the stream contains the 4 events for that `correlation_id`, ordered, valid per `validateEvent`, causation chain correct; measure submit→stream latency for 20 SO submits and assert p95 < 1000 ms.
  - `redis-outage.test.ts`: `compose('stop','redis-town')` → submit 3 SOs (all succeed) → outbox rows `Pending` → `compose('start','redis-town')` → within 150 s (2 sweeps + backoff) exactly 3 new events for those subjects, no duplicate ids.
  - `bulk-load.test.ts` (timeout 180 s): `scenario.run(days=0, bulk=400)` → all 400 SI events `Published` within 60 s of the call returning.
  - `print-formats.spec.ts`: log in as Administrator to `http://test.localhost:8080`, open print view for a seeded SO, DN, SI, PE with the Thai formats, `expect(page).toHaveScreenshot()` per format (baseline committed).
- [x] **Step 2: Run** `pnpm test:integration && pnpm test:e2e -- --update-snapshots` (first run creates baselines) then `pnpm test:e2e`
Expected: all PASS; record measured p95 and bulk time in the commit body.
- [x] **Step 3: Fix** any product defect found by these tests in its owning module (with a unit test there first), not in the test.
- [x] **Step 4: Run** `pnpm test && pnpm test:integration && pnpm test:e2e` → PASS
- [x] **Step 5: Commit** (`test: integration, outage, load and print-format e2e`, `Task: 15`)

---

### Task 16: Pilot image + compose profile

**Files:**
- Create: `infra/docker/pilot/{Containerfile,apps.json,Caddyfile,backup.sh,RESTORE.md}`
- Modify: `infra/docker/compose.yaml` (profile `pilot`: services `backend`, `websocket`, `frontend`, `queue-short`, `queue-long`, `scheduler`, `create-site`, `caddy`, `backup` using image `erp-town/erpnext:v16-<sha>`; shared `mariadb`, `redis-*`), `infra/docker/.env.example` (`SITE_NAME`, `LETSENCRYPT_EMAIL`, `BACKUP_S3_ENDPOINT`, `BACKUP_S3_BUCKET`, `BACKUP_S3_ACCESS_KEY`, `BACKUP_S3_SECRET_KEY`), `scripts/erp.mjs` (`pilot:build`, `pilot:up`, `pilot:backup`), root `package.json` (`erp:pilot:build`, `erp:pilot:up`, `erp:pilot:backup`)

**Interfaces:**
- `Containerfile`: `FROM frappe/build:version-16` builder running `bench init --apps_path=/opt/frappe/apps.json --frappe-branch=version-16 --no-procfile --no-backups --skip-redis-config-generation` (apps.json = erpnext + hrms `version-16`), then `COPY frappe-apps/erp_town apps/erp_town`, `./env/bin/pip install apps/erp_town`, append to `sites/apps.txt`, `bench build --production`; final stage mirrors frappe_docker `images/layered/Containerfile` backend stage (assets moved, entrypoint/start scripts from frappe_docker `resources/core`, vendored with their license header). Build context = repo root.
- `backup.sh`: `bench --site $SITE_NAME backup --with-files` then upload newest files to S3-compatible storage (`aws s3 cp --endpoint-url`), keep 14 days remotely.
- `RESTORE.md`: exact restore drill steps (`bench --site ... restore <sql> --with-public-files ... --with-private-files ...`) and a checklist to tick before pilot start.

- [x] **Step 1: Verify build** `pnpm erp:pilot:build` → image tagged `erp-town/erpnext:v16-<git short sha>`
- [x] **Step 2: Smoke** `SITE_NAME=pilot.localhost pnpm erp:pilot:up` (Caddy on `https://pilot.localhost` with internal TLS) → `curl -k https://pilot.localhost/api/method/ping` returns pong; `bench --site pilot.localhost list-apps` includes erp_town
- [x] **Step 3: Backup drill** run `pnpm erp:pilot:backup` against a local MinIO started for the drill (document the one-off `docker run minio/minio` command in RESTORE.md); restore into a fresh site following RESTORE.md; confirm seeded SI count matches
- [x] **Step 4: Commit** (`feat(infra): pilot image, TLS, backup and restore drill`, `Task: 16`)

---

## Acceptance check (after Task 16)

Walk spec §10 items 1–9; each maps to: 1 → Task 3, 2 → Task 14, 3 → Task 15 sales-flow, 4 → Task 15 redis-outage, 5 → Tasks 10–11 + e2e, 6 → Task 12, 7 → Task 15 bulk-load, 8 → Task 2 + Python contract test in Task 6 (`validate_event` over `events/schema/fixtures`), 9 → Task 1. Record results in the final commit body (`Status: done`).
