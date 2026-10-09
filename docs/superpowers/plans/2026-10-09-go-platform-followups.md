# Go Platform — rulings and follow-ups

Recorded at the end of plan execution (branch feat/go-platform, 87ed4a4). Source: the subagent-driven-development ledger, which is local-only; this file keeps what other agents need. Plan: [2026-10-09-go-platform.md](2026-10-09-go-platform.md); spec: [../specs/2026-10-09-go-platform-design.md](../specs/2026-10-09-go-platform-design.md).

## Rulings made during execution

- Ruling 1: work on feat/go-platform in the main checkout, no worktree — Docker bind mounts, .mcp.json and the desktop session point at D:\ERP_Town (same as Foundation) — cost if wrong: main checkout busy during execution.
- Ruling 2: Task 8's test-site.ts also exports `TOWN_TEST_REDIS_URL=redis://127.0.0.1:6380/0` (the redis-town DB test.localhost publishes to); Task 11's consumer reads it — plan lists no redis URL for integration — cost if wrong: one extra env var.
- Ruling 3: Task 12's `pilot:up` in scripts/erp.mjs creates database `town_pilot` idempotently (`psql -U town -tc "SELECT 1 FROM pg_database WHERE datname='town_pilot'"` → `CREATE DATABASE`) before starting `town`; init.sql stays for fresh volumes — PG init scripts never run on the existing dev volume — cost if wrong: small duplication with init.sql.
- Ruling 4: models — sdd-implementer on sonnet for T1, T2, T3, T6, T7, T9, T12; sdd-implementer-high (opus) for T4, T5, T8, T10, T11 (concurrency, Frappe internals, integration); sdd-reviewer on sonnet for small diffs, opus for T4, T5, T8, T10 — cost if wrong: more fix rounds on a sonnet task.
- Task 4 Ruling: review Important 2 (plan-mandated Handler signature) — change Handler to `func(ctx context.Context, tx pgx.Tx, e Envelope) error` (handler runs in the dedupe/seq transaction; Postgres side effects become exactly-once per group) and fix the doc comment — spec §5.2 fixes the processing order but not the signature; agents (E) will write to Postgres from handlers — cost if wrong: handlers without DB work ignore a parameter.
- Task 4 Ruling: consumer statement failure while Postgres answers ping counts as a failed delivery (like a handler error) — otherwise one poison entry stalls the group forever — cost if wrong: a persistent per-entry SQL error drops that entry after 5 deliveries.
- Task 5 Ruling: review Minor 5 (plan-mandated type-only guard) — Go-emitted events must use Go-owned subjects (e.g. an agent run), never an ERPNext document subject, because Postgres and Frappe keep separate seq counters; enforcement (subject allowlist) is decided in the E spec when agent.* types are added — cost if wrong: a Go event on an ERP subject would be dropped by consumers' seq gate.
- Task 6 Ruling: Critical 1 (rounded totals) — add `RoundedTotal erp.Decimal` (payable total; equals GrandTotal when rounding is off) to SalesOrder and SalesInvoice; porttest asserts GrandTotal == NetTotal + TaxTotal, |RoundedTotal − GrandTotal| < 1, SI.Outstanding == SI.RoundedTotal before payment, PE.Paid == pre-payment Outstanding, Outstanding == 0 after; fake rounds to whole baht half-up — the seed rounds (Foundation Ruling R: payable = rounded_total) — cost if wrong: one more domain field.
- Task 6 Ruling: Important 2 — add `ItemRate erp.Decimal` to porttest.Env (DefaultEnv 12, = seed price of ITEM-002) and assert NetTotal == ItemRate × 3 in CreateAndGet — the brief's literal 36 belongs in the shared contract; Env keeps the suite in domain terms — cost if wrong: Env grows one field.
- Task 6 Ruling: promote Minor 4 (zero Date / null JSON round-trip) into the fix round — Task 10's gateway encodes domain types to JSON and a zero DueDate would emit an undecodable "0000-00-00" — cost if wrong: none.
- Task 7 Ruling: the 10 s ERPNext timeout applies per HTTP attempt (a GET with 2 retries can take ~31 s) — spec §4.3 "timeout ต่อ call 10 s" read as per request; documented on ClientConfig — cost if wrong: wrap Get's loop in one deadline.
- Task 8 Ruling: accept the adapter's extra ERPNext calls (get_party_details for price list/party defaults, get_default_taxes_and_charges for VAT rows, getdoctype for workflow states with 5-min cache, get_bank_cash_account for the payment account) and whoami as GET — they mirror what Desk does and honour customer-specific price lists; no seed change — cost if wrong: a few extra round trips per create.
- Task 8 Ruling: hand-written in-flight company load instead of golang.org/x/sync/singleflight — promoting x/sync to a direct dependency needs user approval; ~20 lines, reviewed race-free — cost if wrong: swap for singleflight later.
- Task 9 Ruling: promote into fix round 1 — Verify errors wrap erp.ErrUnauthenticated (keep jwt cause via multiple %w) so Task 10 maps to 401 with errors.Is; Whoami returning empty User → ErrUnauthenticated (not cached); add alg-pinning test (HS256/none) — cheap and Task 10 relies on them — cost if wrong: a few lines.
- Task 10 Ruling: promote Task 3 minor (goose advisory lock) into Task 10 — outbox role is Migrate's first production caller; goose.WithSessionLocker(lock.NewPostgresSessionLocker()) + concurrent-Migrate test — cost if wrong: one option and one test.
- Task 10 Ruling: CSRF header check runs before authentication; unexpected errors → 500 INTERNAL "internal error" with X-Request-Id (ULID) logged; gateway adds no retries — spec §7 + Review Focus 2 — cost if wrong: middleware reorder.
- Task 10 Ruling: Huma-raised 413/415/408 map to 422 VALIDATION (error table is closed; reviewer judged acceptable) — note it in the API description; promote a JSON catch-all for unmatched /town/api/ paths/methods → 404 {"code":"NOT_FOUND"} (error table has no 405) into fix round 1 so every /town/api response follows the error contract — cost if wrong: a few lines.
- Task 11 Ruling: if the real envelope's company/actor differ from the brief's literals ("ACME-TH", "user.sales@acme-th.test"), assert the value the event contract (packages/event-schema + frappe-apps/erp_town outbox, our own code) defines and record the difference in the report — the contract is the spec authority, the brief's literal is its argument — cost if wrong: one assertion.
- Task 12 Ruling: the backup drill may upload only to a local S3-compatible store (the Foundation drill's SeaweedFS container, e.g. http://erp-town-seaweedfs:8333); if the local .env's BACKUP_S3_ENDPOINT is a remote provider, the implementer skips upload/restore drill and reports it (stop condition: write outside this machine) — cost if wrong: the drill is re-run later by the user.
- Task 11 Ruling: untracked `graft/` (not ours, appeared mid-session) makes `pnpm lint` (Biome) fail; leave it untouched, lint tracked paths, tell the user at the end — cost if wrong: user adds an ignore or removes the dir.
- Task 12 Ruling: promote into fix round 1 — verify the PGDG key fingerprint (B97B0AFCAA1A47F044F244A07FCC7D46ACCC4CF8) and purge curl/gnupg after install (supply chain); add TOWN_PG_PASSWORD to the pre-pilot checklist with the fresh-volume note — cheap security items — cost if wrong: a few lines.
- Final Ruling: C1 fix = Client sets req.Host = Site when Site is set (works through nginx, gunicorn and the dev server alike) + unit test; verify on pilot with an authenticated curl (login → /town/api/me → 200) and add that check to RESTORE.md's checklist — cost if wrong: switch TOWN_ERP_URL to backend:8000.
- Final Ruling: one fix wave = C1, I1, M1 (CheckRedirect), M3 (whoami PermissionError → [] + Python test), M4 (TOWN_PG_PASSWORD in PILOT_REQUIRED), M5 (IdleTimeout + gateway per-request deadline → 503), M6 (client-cancel not 500/ERROR), M7 (AGENTS schema:gen row), M8 (.env.example TOWN_DATABASE_URL), M9 (ParseRoles non-blank → no roles errors), M11 (consumer doc); M2/M10 stay deferred to C/E per triage — all cheap, M2 needs design with E's emitters — cost if wrong: one more small fix later.

## Follow-ups with a deadline (final review triage)

Before the pilot goes live:
- Set a strong `TOWN_PG_PASSWORD` in `infra/docker/.env` (`pilot:up` now refuses empty or `town`); on the existing postgres volume also run `ALTER ROLE town PASSWORD …` (RESTORE.md) and set `TOWN_DATABASE_URL` for `pnpm dev:town` (`.env.example`).
- Prune local `town_pilot` `.dump`/`.part` files on the sites volume together with the bench set; pin `gcr.io/distroless/static-debian12:nonroot` by digest like the golang builder.
- Remote retention is skipped on runs where `pg_dump` fails (the run exits non-zero); a long pg_dump outage stops pruning of remote ERP sets.

Before C (World State) adds the first production consumer handler:
- Recover handler panics in the consumer (count as a failed delivery).
- Log and count stale-seq skips; add an access log and propagate `X-Request-Id` to ERPNext calls (final review M10).
- Emit→consume is ≈1.1–1.7 s on test.localhost (envelope `time` has 1 s precision + after-commit RQ publish); publish→consume 6 ms. C's 250 ms target must be measured from the stream entry, or the Frappe publish path must change.

Before E (agents) emits events through the Go outbox:
- Head-of-line publishing per subject: a row in backoff can be overtaken by a later seq of the same subject, which consumers then drop (final review M2; same behaviour as the Frappe outbox).
- Purge Published `town_outbox` rows (Frappe has a daily purge); set per-XADD timeout and go-redis `MaxRetries` so retries don't hold row locks.
- Typed data with nullable keys: generated pointer + `omitempty` drops explicit null on re-encode.
- Test whoami with an API-key credential against real Frappe; decide the Go-owned subject allowlist (Task 5 ruling).

Before D (client) relies on retries:
- No client idempotency keys: a create that times out (45 s gateway deadline → 503) or a client disconnect (499) can still have committed in ERPNext.

## Deferred minor review findings

- Task 1: .air.toml bin hardcodes tmp/town.exe
- Task 1: main.go run() untested (--roles override, extra args, -h exits 1)
- Task 1: TestServeShutsDownOnCancel free-port TOCTOU
- Task 2: go-jsonschema pkg/types (SerializableDate) is a runtime dependency of gen — consider mapping format:date to string/own type
- Task 2: nullable data fields are named pointer types with omitempty — typed re-encode drops explicit null
- Task 2: FuzzDecode ignores non-SchemaError errors; seq 1.0 validates but Decode (int64) rejects it
- Task 2: DecodeData can fail on schema-valid but calendar-invalid dates (doc comment)
- Task 2: no drift guard for generated Go data structs
- Task 3: store/fs.go trivial fs.Sub wrapper file not in plan
- Task 3: storetest goleak ignores pgxpool health-check goroutines — hides unclosed-pool leaks (carry to Task 4 dispatch)
- Task 3: storetest labels every container start error "docker unavailable"
- Task 3: store.Open untested; isolation test doesn't assert schema dropped
- Task 4: shutdown mid-batch increments delivery counts of read-but-unprocessed entries
- Task 4: stale-seq skips not logged (no trace when N is skipped after N+1 applied)
- Task 4: goleak ignores net/http persistConn loops in events package (broader than testcontainers)
- Task 4: consumer logs to slog.Default — WARN/ERROR noise in test output
- Task 4: XPENDING round trip per claimed entry + PEL scan each step (pilot-scale OK)
- Task 4: no test for fail-once-then-succeed on reclaim
- Task 4: handler panic not recovered
- Task 4: outage hitting mid-handler costs one delivery (handlerError path skips ping)
- Task 4: Begin has no per-call timeout against a blackholed DB (shutdown latency)
- Task 5: no test that one broken Redis connection costs one dial per batch
- Task 5: go-redis default retries can hold batch row locks ~20 s — document client options / per-XADD timeout
- Task 5: EVENT_INVALID report drops original company (Frappe passes it)
- Task 5: most tests not table-driven
- Task 5: per-row backoff on a server-side XADD error can still publish seq N+1 before N (same as Frappe)
- Task 5: no purge of Published town_outbox rows (Frappe has a daily purge)
- Task 5: sqlc InsertOrder nullable type; old (status,next_attempt_at) index likely unused
- Task 6: fake keeps caller Config maps without copying; Whoami reads them unlocked
- Task 6: unsupported doctype returns ErrForbidden before ValidationError
- Task 6: fake Draft transitions allow Sales Manager alone (seed: Sales User only)
- Task 6: no test of RoundTo(0) exact .5 boundary; null amount now silently zero (adapters must check presence)
- Task 7: LimitReader truncates 32 MiB silently; malformed header values classified ErrUnavailable and retried
- Task 7: Call prefers data over message; redundant MandatoryError check + empty case; tag stripper joins words; dangling colon on empty detail
- Task 7: test gaps (cancel during retry sleep, Get message unwrap, exception fallback, entity unescape)
- Task 8: onWorkflow labels every field-less ValidationError from apply_workflow as "workflow"
- Task 8: mode_of_payment notFoundAsValidation dead (ERPNext returns 417); docStatus error swallowed in Submit message
- Task 8: back-dated invoice with unparsable dates empties schedule but doesn't shift due_date
- Task 8: pickTransition takes first of several submitting transitions
- Task 8: whoami persists CSRF token on GET (comment)
- Task 8: integration CashIsReceivedIntoTheCashAccount asserts on all cash PEs (order dependent)
- Task 8: test-site.ts ignores SIGTERM for its lifetime; Client.CallCred unused
- Task 8: whoami with API-key credential untested against real Frappe
- Task 8: non-context company-load error shared with waiters of other principals (e.g. A's ErrUnauthenticated hits B, transient)
- Task 8: company flight cleared without defer (panic would wedge cc.flight)
- Task 8: Cancel GET-then-cancel race yields ValidationError not ErrConflict under concurrent cancel
- Task 8: erpfake Cancel conflicts on draft linked DN; ERPNext only on submitted
- Task 9: no 33-byte/empty seed test; Verify doesn't require non-empty sub; Issue accepts empty ID
- Task 9: concurrent cold-cache requests per sid each call Whoami (no singleflight); no empty-sid-cookie/30 s-boundary/fractional-second tests; redundant WithTimeFunc(time.Now)
- Task 10: wireOutbox runs Migrate before HTTP starts (healthz down up to 5 min lock wait)
- Task 10: fields keys mix Huma locations (body.lines[0].qty) and ERP field names (customer)
- Task 10: Huma also serves openapi.yaml / openapi-3.0.{json,yaml} publicly
- Task 10: wrong method on a real /town/api path → 404 (no 405 in the error table)
- Task 11: consumer start-up error surfaces only as 15 s timeout; bangkokToday host-clock midnight edge; latency probe scans last 1000 entries; login helper duplicated from erpnext integration test
- Task 12: local .dump/.part files accumulate on the sites volume; ensurePilotDatabase hides psql stderr; distroless tag not digest-pinned; compose.yaml:96-97 long comment line
