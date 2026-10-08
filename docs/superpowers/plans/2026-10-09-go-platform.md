# Go Platform Skeleton Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Go module `go/` (binary `town`) that reaches ERPNext only through `erp.Port`, consumes and produces town events, keeps its own data in PostgreSQL 18, and serves an authenticated gateway — proven end to end (gateway → ERPNext → stream → Go consumer) on `test.localhost`, and packaged for the pilot.

**Architecture:** Modular monolith: one module, one binary, roles switched on with `--roles` (this plan builds `gateway` and `outbox`). Domain code depends on `erp` interfaces only; `erp/erpnext` (Frappe REST, principal-scoped credentials) is chosen in `cmd/town`. `erp/porttest` is one conformance suite run against `erp/erpfake` (unit) and `erp/erpnext` on `test.localhost` (integration). Events reuse the Foundation envelope: Go validates against the same JSON Schema and golden fixtures, consumes `town:events` with consumer groups, and publishes its own events through a Postgres outbox.

**Tech Stack:** Go 1.27, go-redis v9, pgx v5, goose v3, sqlc (Docker image), santhosh-tekuri/jsonschema v6, atombender/go-jsonschema (go tool), oklog/ulid v2, cockroachdb/apd v3, golang-jwt v5, OpenTelemetry metrics + Prometheus exporter, golangci-lint v2 (go tool), PostgreSQL 18.

**Spec:** `docs/superpowers/specs/2026-10-09-go-platform-design.md` (program context: `docs/superpowers/specs/2026-10-07-program-overview.md`, event contract: `docs/superpowers/specs/2026-10-07-foundation-design.md` §3)

## Global Constraints

- Go `1.27` (`go 1.27` in `go/go.mod`); module path `github.com/atipongsena/erp-town/go`; all builds `CGO_ENABLED=0`; tools pinned with the go.mod `tool` directive and run as `go tool <name>`; commands run from the repo root with `go -C go ...`.
- `go/` is proprietary: `go/LICENSE` reads `Copyright (c) 2026 atipongsena. All rights reserved.`; no GPL headers in `go/`; no code from Frappe/ERPNext/HRMS is copied or translated into `go/` (clean-room, spec §12).
- PostgreSQL 18 (`postgres:18-alpine`), user `town`, dev host port `127.0.0.1:5433`; databases `town` (dev), `town_test` (tests), `town_pilot` (pilot). Go never connects to MariaDB.
- `town` HTTP default `127.0.0.1:8090`; API routes under `/town/api/`; metrics at `/town/metrics`.
- redis-town `127.0.0.1:6380`; Go unit tests use DB index 15 with a unique stream per test; stream default `town:events`, integration `town:test:events`; stream entry field `event` = UTF-8 JSON payload (no HTML escaping); stream retention 7 days (`MINID` approximate).
- Envelope: `specversion` `"1.0"`, `schema_version` `1`, `id` ULID, `time` ISO 8601 with the `Asia/Bangkok` offset (`+07:00`) regardless of the host clock zone; `company` = Company abbr code.
- Go outbox: statuses `Pending`/`Published`/`Failed`/`Invalid`; `max_attempts` 10; retry after `min(2^attempts, 300)` s; error codes exactly `EVENT_INVALID`, `OUTBOX_PUBLISH_FAILED`; a `system.error` row that exhausts its attempts becomes `Failed` without a new error event; Go never emits `erp.*` or `workflow.*` events (ERPNext owns those subjects' `seq`).
- Consumer: dedupe window 8 days; reclaim pending entries idle 60 s (`XAUTOCLAIM`); an entry delivered 5 times without success is acked and dropped; schema-invalid entries are acked and counted.
- Metrics names exactly: `town_events_invalid_total`, `town_events_dropped_total`, `town_outbox_publish_failures_total`.
- `erp`: amounts and quantities are `erp.Decimal` (apd), JSON-encoded as strings; ERPNext JSON numbers are decoded with `json.Number`, never `float64`; every Port call uses the caller's `Principal` credential — never an admin key on someone's behalf; `OnBehalfOf` is sent as header `X-Town-On-Behalf-Of`.
- ERPNext adapter: per-call timeout 10 s; GET retried at most 2 times on `ErrUnavailable` (sleep 200 ms, then 800 ms); writes never retried.
- Auth: whoami cache 30 s per session id; JWT EdDSA (Ed25519), lifetime 15 min, claims `sub`, `roles`, `companies`, `exp`, `iat`; key from env `TOWN_JWT_PRIVATE_KEY` = standard base64 of the 32-byte seed.
- Gateway error body `{"code","message","fields"?}` with exactly: `UNAUTHENTICATED` 401, `FORBIDDEN` 403, `NOT_FOUND` 404, `CONFLICT` 409, `VALIDATION` 422, `UNAVAILABLE` 503, `INTERNAL` 500, `CSRF_HEADER_MISSING` 403; state-changing requests (POST/PUT/PATCH/DELETE) require header `X-Town-Request: 1`.
- Generated, never hand-edited: `go/internal/events/gen/`, `go/internal/events/schema/`, `go/internal/store/sqlc/` (plus the existing `packages/event-schema/src/generated.ts`, `frappe-apps/erp_town/erp_town/events/schema/`).
- Tests never touch `town.localhost`; Go integration tests run only through `pnpm go:test:integration` (test.localhost seeded and restored).
- `pnpm lint` (Biome + golangci-lint) must pass before committing; LF line endings.
- Commit trailer: `Plan: go-platform`, `Task: <n>`, `Agent: <claude|codex>`, `Status: <done|partial|blocked>`, then the agent's `Co-Authored-By` line (AGENTS.md).

## Plan rulings (where this plan refines the spec)

- R1: spec §11 names `scripts/test-site.mjs`; instead `tests/integration/test-site.ts` (run by Node 24 type stripping) reuses `prepareTestSite`/`restoreTestSite` from `helpers.ts` (no duplicate logic) and wraps one whole `go test -p 1 -tags integration ./...` run — a Go `TestMain` runs once per package and would re-seed the site per package.
- R2: the envelope struct is hand-written (`events/envelope.go`, stable and small); per-type `data` structs and the type registry are generated. A round-trip contract test proves the struct matches every fixture.
- R3: domain errors gain `ErrUnauthenticated` (gateway 401) next to the five in spec §4.2.
- R4: `erp.Identities` (whoami) joins the Port; Frappe side is a new whitelisted `erp_town.session_api.whoami` returning user, roles, company codes and the session's CSRF token, so the browser never sends the Frappe CSRF token to Go; the gateway's own CSRF defence is the `X-Town-Request` header.
- R5: Port interfaces in this plan cover the sales slice (`Documents`, `Sales`, `Delivery`, `Billing`, `Workflow`, `Identities`); `Stock`, `Parties`, `Projection` arrive with the C/E plans (spec §4.1: methods added as needed).
- R6: sqlc runs from the `sqlc/sqlc` Docker image (sqlc needs cgo, unavailable on the Windows dev host).
- R7: `world_snapshots` and agent tables belong to the C/E plans; this plan's migration creates only event tables.
- R8: same-origin routing of `/town/*` is done for the pilot (Caddy, Task 12); the dev-side proxy arrives with the client (D) — until then dev callers send the `sid` cookie to `127.0.0.1:8090` themselves.

## Review Focus

1. Thai text and `&`/`<` in event payloads → consumed and published byte-exact (Go's default JSON HTML-escaping must not alter payloads) — tests in Task 4 and Task 5.
2. ERPNext down or slow during a gateway write → `503 UNAVAILABLE`, the write is not retried (no duplicate Sales Order) — tests in Task 7 and Task 10.
3. Expired or missing Frappe session cookie → `401 UNAUTHENTICATED`, never 500 — tests in Task 9 and Task 10.
4. Redelivery of an already-applied event (crash between commit and `XACK`, or the same id published twice by the Frappe outbox) → handler runs once — test in Task 4.
5. ERPNext amounts such as `1234567.89`, `0.1`, `135.0` → exact decimals, never float artefacts — test in Task 8.

---

## File Structure

```
go/go.mod, go/go.sum, go/LICENSE, go/.golangci.yml
go/cmd/town/main.go                         ← CLI `town serve`, wiring of roles and adapters
go/internal/platform/config.go              ← Config, LoadConfig, Roles
go/internal/platform/server.go              ← HTTP server, /town/api/healthz, graceful shutdown
go/internal/platform/metrics.go             ← OTel meter provider + Prometheus handler
go/internal/events/envelope.go              ← Envelope, Actor, Subject, Visibility (hand-written)
go/internal/events/validate.go              ← Validator over embedded schema, Decode, DecodeData
go/internal/events/schema/                  ← GENERATED copy of packages/event-schema schema + fixtures
go/internal/events/gen/data.go, types.go    ← GENERATED data structs, EventTypes, NewData registry
go/internal/events/consumer.go              ← stream consumer group: validate, dedupe, seq gate, ack
go/internal/events/outbox.go                ← Emit into town_outbox
go/internal/events/publisher.go             ← PublishDue / Run: XADD, backoff, Failed
go/internal/events/clock.go                 ← Bangkok time formatting
go/internal/events/eventstest/redis.go      ← test helper: Redis DB 15 + unique stream
go/internal/store/db.go                     ← Open, Migrate (goose, embedded)
go/internal/store/migrations/00001_events.sql
go/internal/store/sqlc.yaml, queries/*.sql, sqlc/ (GENERATED)
go/internal/store/storetest/storetest.go    ← test helper: isolated schema in town_test
go/internal/erp/{principal,types,decimal,date,errors,port}.go
go/internal/erp/erpfake/fake.go             ← in-memory Port
go/internal/erp/porttest/{env,porttest}.go  ← conformance suite
go/internal/erp/erpnext/{client,errors,adapter,sales,delivery,billing,workflow,documents,identities}.go
go/internal/erp/erpnext/testdata/*.json     ← ERPNext v16 response samples
go/internal/auth/{authenticator,jwt}.go
go/internal/gateway/{gateway,errors,sales}.go
go/internal/e2e/sales_flow_test.go          ← //go:build integration
frappe-apps/erp_town/erp_town/session_api.py + tests/test_session_api.py
tests/integration/test-site.ts              ← prepare → run command → restore
infra/docker/compose.yaml (postgres, town), infra/docker/postgres/init.sql
infra/docker/town/Containerfile, infra/docker/pilot/{Caddyfile,backup.sh,RESTORE.md,Containerfile}
package.json, AGENTS.md, infra/docker/.env.example, scripts/erp.mjs
```

---

### Task 1: Go module, config, server and toolchain

**Files:**
- Create: `go/go.mod`, `go/LICENSE`, `go/.golangci.yml`, `go/cmd/town/main.go`, `go/internal/platform/config.go`, `go/internal/platform/server.go`
- Test: `go/internal/platform/config_test.go`, `go/internal/platform/server_test.go`
- Modify: `package.json` (scripts), `AGENTS.md` (Layout, Commands, Rules), `.gitignore` (`go/bin/`)

**Interfaces:**
- Produces:
  - `type Role string`; `const RoleGateway Role = "gateway"`, `RoleOutbox Role = "outbox"`; `var KnownRoles = []Role{RoleGateway, RoleOutbox}`
  - `type Roles map[Role]bool`; `func ParseRoles(s string) (Roles, error)` (comma list; empty = all known); `func (r Roles) Has(x Role) bool`
  - `type Config struct { HTTPAddr, DatabaseURL, RedisURL, Stream, ERPURL, ERPSite, JWTPrivateKey string; Roles Roles }`
  - `func LoadConfig(getenv func(string) string) (Config, error)` — env `TOWN_HTTP_ADDR`, `TOWN_DATABASE_URL`, `TOWN_REDIS_URL`, `TOWN_STREAM`, `TOWN_ERP_URL`, `TOWN_ERP_SITE`, `TOWN_JWT_PRIVATE_KEY`, `TOWN_ROLES`
  - `func NewMux() *http.ServeMux` (registers `GET /town/api/healthz`); `func Serve(ctx context.Context, addr string, h http.Handler) error` (returns on ctx cancel after `Shutdown` with 10 s grace)

- [ ] **Step 1: Write the failing tests**

```go
func TestLoadConfigDefaults(t *testing.T) {
	c, err := LoadConfig(func(string) string { return "" })
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1:8090", c.HTTPAddr)
	assert.Equal(t, "postgres://town:town@127.0.0.1:5433/town?sslmode=disable", c.DatabaseURL)
	assert.Equal(t, "redis://127.0.0.1:6380/0", c.RedisURL)
	assert.Equal(t, "town:events", c.Stream)
	assert.Equal(t, "http://127.0.0.1:8080", c.ERPURL)
	assert.Equal(t, "town.localhost", c.ERPSite)
	assert.True(t, c.Roles.Has(RoleGateway) && c.Roles.Has(RoleOutbox))
}
func TestLoadConfigOverrides(t *testing.T)   // every TOWN_* variable is read; TOWN_ROLES=outbox → only outbox
func TestParseRolesRejectsUnknown(t *testing.T) {
	_, err := ParseRoles("gateway,world")
	assert.ErrorContains(t, err, `unknown role "world"`)
	assert.ErrorContains(t, err, "gateway, outbox") // lists the known roles
}
func TestHealthz(t *testing.T) // httptest GET /town/api/healthz → 200, body {"status":"ok"}, Content-Type application/json
```

Use `github.com/stretchr/testify` (assert/require) in all Go tests of this plan.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go -C go test ./internal/platform/...`
Expected: FAIL (package does not compile: `LoadConfig` undefined)

- [ ] **Step 3: Implement config, server and `cmd/town`**

`town serve [--roles=a,b]` (flag overrides `TOWN_ROLES`), signal-cancelled context (`os.Interrupt`, `SIGTERM`), logs with `slog` JSON handler. In this task `serve` only mounts `NewMux()`; later tasks add roles. `go/go.mod`: add `tool github.com/golangci/golangci-lint/v2/cmd/golangci-lint` at the latest v2 release. `go/.golangci.yml` (v2 format): linters `govet`, `staticcheck`, `errcheck`, `depguard`; formatter `gofumpt`; depguard rules:
  - `erp-adapter`: files `**/internal/**` except `**/internal/erp/erpnext/**` and `**/internal/e2e/**` deny `github.com/atipongsena/erp-town/go/internal/erp/erpnext` ("adapters are chosen in cmd/town")
  - `devbridge-no-erp`: files `**/internal/devbridge/**` deny `github.com/atipongsena/erp-town/go/internal/erp`
  - exclude `internal/events/gen/` and `internal/store/sqlc/` from linters (generated)

`package.json` scripts (keep existing ones):

```json
"go:test": "go -C go test ./...",
"go:test:integration": "node tests/integration/test-site.ts run --scheduler -- go -C go test -p 1 -tags integration -count=1 ./...",
"go:lint": "go -C go tool golangci-lint run ./...",
"go:gen": "pnpm schema:gen && go -C go generate ./...",
"dev:town": "go -C go run ./cmd/town serve",
"lint": "biome check . && pnpm go:lint",
"test": "pnpm -r test && pnpm go:test && pnpm erp:test"
```

(`go:test:integration` becomes usable in Task 8.) AGENTS.md: add `go/` to Layout; add the `go:*` and `dev:town` rows to Commands (note: `go:test` needs `pnpm erp:up` for Postgres/Redis from Task 3 on); add to Rules the generated Go paths, the clean-room rule ("never open Frappe/ERPNext source as a model for `go/` code"), and `go/` license.

- [ ] **Step 4: Run tests and lint**

Run: `go -C go test ./internal/platform/... && pnpm lint`
Expected: PASS; Biome and golangci-lint report no issues

- [ ] **Step 5: Commit**

```bash
git add go package.json AGENTS.md .gitignore
git commit   # feat(go): module skeleton, config, healthz and toolchain — trailer Plan: go-platform / Task: 1
```

---

### Task 2: Event contract in Go

**Files:**
- Modify: `packages/event-schema/scripts/gen.mjs` (add Go outputs), `packages/event-schema/README.md` (mention Go)
- Create: `go/internal/events/envelope.go`, `go/internal/events/validate.go`, `go/internal/events/doc.go` (`//go:embed schema`)
- Generated: `go/internal/events/schema/**`, `go/internal/events/gen/data.go`, `go/internal/events/gen/types.go`
- Test: `go/internal/events/contract_test.go`

**Interfaces:**
- Produces:
  - `type Envelope struct { SpecVersion string "specversion"; ID string "id"; Type string "type"; Source string "source"; Time string "time"; SchemaVersion int "schema_version"; Company *string "company"; Actor Actor "actor"; Subject Subject "subject"; Seq int64 "seq"; CorrelationID string "correlation_id"; CausationID *string "causation_id"; Visibility Visibility "visibility"; Data json.RawMessage "data" }` (JSON names as shown; `Time` kept as the original string)
  - `type Actor struct { Kind, ID string; OnBehalfOf *string }` (`kind`, `id`, `on_behalf_of`); `type Subject struct { DocType, Name string }` (`doctype`, `name`); `type Visibility struct { Level string; Roles []string }` (`roles` always encodes as an array, never `null`)
  - `func NewValidator() (*Validator, error)`; `func (v *Validator) Validate(raw []byte) error` (returns `*SchemaError{Reasons []string}`)
  - `func Decode(v *Validator, raw []byte) (Envelope, error)`; `func DecodeData[T any](e Envelope) (T, error)`
  - `func Marshal(e Envelope) ([]byte, error)` — encoder with `SetEscapeHTML(false)`, no trailing newline
  - generated: `gen.EventTypes []string`; `gen.NewData(eventType string) (any, bool)` returning a pointer to that type's struct, e.g. `*gen.ErpSalesOrderSubmittedData`

- [ ] **Step 1: Write the failing tests**

```go
func TestValidFixturesPassAndRoundTrip(t *testing.T)   // every schema/fixtures/valid/*.json: Validate ok; Marshal(Decode(raw)) JSON-equal to raw
func TestInvalidFixturesFail(t *testing.T)             // every schema/fixtures/invalid/*.json: Validate returns *SchemaError
func TestDataDecodesForEveryType(t *testing.T)         // for each valid fixture: gen.NewData(type) ok, json.Unmarshal(e.Data) with DisallowUnknownFields succeeds
func TestEventTypesMatchSchema(t *testing.T)           // gen.EventTypes == envelope.json properties.type.enum (order-insensitive), len 13
func TestEmbeddedSchemaMatchesSource(t *testing.T)     // byte-equal to ../../../packages/event-schema/{schema,fixtures} (drift)
func TestMarshalKeepsThaiAndAmpersand(t *testing.T) {
	raw := []byte(`...system.error fixture with "message":"ส่งไม่สำเร็จ & ลองใหม่ <1>"...`)
	e, err := Decode(v, raw)
	require.NoError(t, err)
	out, _ := Marshal(e)
	assert.Contains(t, string(out), `"ส่งไม่สำเร็จ & ลองใหม่ <1>"`)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go -C go test ./internal/events/...`
Expected: FAIL (`NewValidator` undefined, `gen` package missing)

- [ ] **Step 3: Extend `gen.mjs` and implement the validator**

`gen.mjs` additions (after the Frappe copy, same `write`/LF helpers): copy `schema/` and `fixtures/` to `go/internal/events/schema/` with a README like the Frappe one ("GENERATED — edit packages/event-schema"); run `go -C go tool go-jsonschema -p gen --only-models -o internal/events/gen/data.go --schema-root-type <$id>=<Pascal>Data …` over `schema/events/*.json` (type names = TS names, e.g. `ErpSalesOrderSubmittedData`; add the tool with `go get -tool github.com/atombender/go-jsonschema@latest`); emit `gen/types.go` (header `// Code generated by packages/event-schema/scripts/gen.mjs; DO NOT EDIT.`) with `EventTypes` and `NewData`. If go-jsonschema cannot express a schema (spec §5.1), replace that step with an in-repo generator `go/tools/schemagen` writing the same file and names. Validator: `jsonschema/v6` compiler, every embedded schema added as a resource under its `$id`, compile `https://erp-town.local/schema/envelope.json`; format assertions off (schemas carry patterns, matching ajv/Python verdicts).

- [ ] **Step 4: Run generation and tests**

Run: `pnpm schema:gen && go -C go test ./internal/events/... && pnpm --filter @erp-town/event-schema test`
Expected: PASS; `git status` shows only the new Go outputs (TS/Python outputs unchanged)

- [ ] **Step 5: Commit**

```bash
git add packages/event-schema go/internal/events go/go.mod go/go.sum
git commit   # feat(events): Go envelope, generated data types and contract tests — Task: 2
```

---

### Task 3: PostgreSQL service and store

**Files:**
- Modify: `infra/docker/compose.yaml` (service `postgres`, volume `postgres-data`), `infra/docker/.env.example` (`TOWN_PG_PASSWORD=town`, `TOWN_JWT_PRIVATE_KEY=`), `AGENTS.md` (Postgres line in Rules/Layout)
- Create: `infra/docker/postgres/init.sql`, `go/internal/store/db.go`, `go/internal/store/migrations/00001_events.sql`, `go/internal/store/storetest/storetest.go`
- Test: `go/internal/store/db_test.go`

**Interfaces:**
- Produces:
  - `func Open(ctx context.Context, url string) (*pgxpool.Pool, error)`
  - `func Migrate(ctx context.Context, pool *pgxpool.Pool) error` (goose provider over embedded `migrations/`, dialect postgres)
  - `storetest.NewPool(t *testing.T) *pgxpool.Pool` — connects to `TOWN_TEST_DATABASE_URL` (default `postgres://town:town@127.0.0.1:5433/town_test?sslmode=disable`), creates schema `t_<random hex>`, sets it as `search_path` for every pool connection, runs `Migrate`, drops the schema with `t.Cleanup`; unreachable DB → `t.Fatal("postgres unreachable: run pnpm erp:up")`
  - tables (migration 00001): `town_outbox(id text pk, type text, subject_doctype text, subject_name text, payload text, status text check in ('Pending','Published','Failed','Invalid'), attempts int default 0, last_error text, next_attempt_at timestamptz, stream_id text, created_at timestamptz default now(), published_at timestamptz)` + index `(status, next_attempt_at)`; `town_subject_seq(subject_doctype text, subject_name text, last_seq bigint, pk(subject_doctype, subject_name))`; `consumed_events(consumer_group text, event_id text, consumed_at timestamptz default now(), pk(consumer_group, event_id))`; `consumer_subject_seq(consumer_group text, subject_doctype text, subject_name text, last_seq bigint, pk(consumer_group, subject_doctype, subject_name))`

- [ ] **Step 1: Write the failing tests**

```go
func TestMigrateCreatesEventTables(t *testing.T) // NewPool; to_regclass for the 4 tables is non-null
func TestMigrateIsIdempotent(t *testing.T)       // Migrate twice on the same pool → no error
func TestSchemasAreIsolated(t *testing.T)        // two NewPool: insert into town_subject_seq in one, other sees 0 rows
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go -C go test ./internal/store/...`
Expected: FAIL (`Open` undefined)

- [ ] **Step 3: Add the compose service and implement the store**

Compose `postgres`: `image: postgres:18-alpine`, no profile (shared like mariadb), `restart: unless-stopped`, env `POSTGRES_USER=town`, `POSTGRES_PASSWORD=${TOWN_PG_PASSWORD:-town}`, `POSTGRES_DB=town`, ports `127.0.0.1:5433:5432`, volume `postgres-data:/var/lib/postgresql` (PG 18 layout), `./postgres/init.sql:/docker-entrypoint-initdb.d/init.sql:ro`, healthcheck `pg_isready -U town`. `init.sql`: `CREATE DATABASE town_test;` (pilot DB is created in Task 12). Header comment in compose describing the service like the others.

- [ ] **Step 4: Start Postgres and run tests**

Run: `pnpm erp:up && go -C go test ./internal/store/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add infra/docker go/internal/store AGENTS.md go/go.mod go/go.sum
git commit   # feat(store): PostgreSQL 18 service, goose migrations and isolated test schemas — Task: 3
```

---

### Task 4: Stream consumer

**Files:**
- Create: `go/internal/events/consumer.go`, `go/internal/events/eventstest/redis.go`, `go/internal/platform/metrics.go`, `go/internal/store/sqlc.yaml`, `go/internal/store/queries/consumer.sql`, `go/internal/store/generate.go` (`//go:generate go run ../../tools/sqlcgen`), `go/tools/sqlcgen/main.go` (runs `docker run --rm -v <absolute store dir>:/src -w /src sqlc/sqlc:1.29.0 generate`; absolute path so it works from PowerShell and Git Bash)
- Generated: `go/internal/store/sqlc/*`
- Test: `go/internal/events/consumer_test.go`, `go/internal/platform/metrics_test.go`

**Interfaces:**
- Consumes: `Validator`, `Decode` (Task 2); `storetest.NewPool` (Task 3)
- Produces:
  - `type Handler func(ctx context.Context, e Envelope) error`
  - `type ConsumerConfig struct { Stream, Group, Name string; Block, ClaimIdle time.Duration; MaxDeliveries int64; DedupeWindow time.Duration }` with `func DefaultConsumerConfig(stream, group, name string) ConsumerConfig` (Block 5 s, ClaimIdle 60 s, MaxDeliveries 5, DedupeWindow 8×24 h)
  - `func NewConsumer(rdb *redis.Client, pool *pgxpool.Pool, v *Validator, m *platform.Metrics, cfg ConsumerConfig, h Handler) *Consumer`; `func (c *Consumer) Run(ctx context.Context) error` (creates the group with `MKSTREAM` from `$` if missing); `func (c *Consumer) PurgeConsumed(ctx context.Context) (int64, error)`
  - `type Metrics struct { EventsInvalid, EventsDropped, OutboxPublishFailures metric.Int64Counter }`; `func NewMetrics() (*Metrics, http.Handler, error)` (Prometheus exporter; handler mounted at `/town/metrics` in Task 10); `platform.NewTestMetrics() (*Metrics, func(name string) int64)` reads counters via an SDK manual reader
  - `eventstest.NewRedis(t *testing.T) (*redis.Client, string)` — `TOWN_TEST_REDIS_URL` (default `redis://127.0.0.1:6380/15`), stream `test:<random hex>`, deleted on cleanup

Processing of one entry (in this order): decode/validate (fail → ack, `EventsInvalid`+1) → transaction: `INSERT consumed_events … ON CONFLICT DO NOTHING` (0 rows → commit, ack, skip) → `last_seq` of `(group, subject)` `FOR UPDATE`; `e.Seq <= last_seq` → commit, ack, skip → call `h` (error → rollback, no ack) → upsert `last_seq = e.Seq` → commit → `XACK`. Pending entries idle ≥ `ClaimIdle` are taken with `XAUTOCLAIM`; an entry whose delivery count exceeds `MaxDeliveries` is acked without calling `h`, `EventsDropped`+1, logged with its id. `PurgeConsumed` deletes `consumed_events` older than `DedupeWindow`; `Run` calls it hourly.

- [ ] **Step 1: Write the failing tests**

```go
func TestConsumerAppliesInSeqOrderAndAcks(t *testing.T)    // seq 1,2,3 of one subject → handler saw [1,2,3]; XPENDING count 0
func TestConsumerRunsHandlerOnceForDuplicateID(t *testing.T) // same event XADDed twice → handler called once (Review Focus 4)
func TestConsumerSkipsStaleSeqButNeverWaits(t *testing.T)  // seq 2 then 1 → only 2; seq 1 then 3 → both
func TestConsumerAcksSchemaInvalid(t *testing.T)           // "{}" and a fixture from fixtures/invalid → acked, handler not called, town_events_invalid_total == 2
func TestConsumerDropsAfterMaxDeliveries(t *testing.T)     // handler always errors; ClaimIdle 50ms, MaxDeliveries 5 → handler called 5 times, then acked; town_events_dropped_total == 1; no consumed_events row
func TestConsumerDeliversThaiDataExactly(t *testing.T)     // data.party "ลูกค้า-001 & <x>" equals after DecodeData (Review Focus 1)
func TestPurgeConsumedRemovesOldRows(t *testing.T)         // rows consumed_at now()-9d removed, now()-7d kept
```

Test events are built from the valid fixtures with a fresh ULID `id`, the test's subject and the chosen `seq`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go -C go test ./internal/events/... ./internal/platform/...`
Expected: FAIL (`NewConsumer` undefined)

- [ ] **Step 3: Implement queries (sqlc), metrics and the consumer**

- [ ] **Step 4: Generate and run tests**

Run: `go -C go generate ./internal/store/... && go -C go test ./internal/events/... ./internal/platform/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add go
git commit   # feat(events): Redis Streams consumer with dedupe, seq gate and drop policy — Task: 4
```

---

### Task 5: Go outbox and publisher

**Files:**
- Create: `go/internal/events/outbox.go`, `go/internal/events/publisher.go`, `go/internal/events/clock.go`, `go/internal/store/queries/outbox.sql`
- Test: `go/internal/events/outbox_test.go`, `go/internal/events/publisher_test.go`

**Interfaces:**
- Consumes: `Validator`, `Marshal`, `Envelope` types (Task 2); `Metrics` (Task 4); sqlc queries
- Produces:
  - `type EmitInput struct { Type, Source string; Company *string; Actor Actor; Subject Subject; CorrelationID string; CausationID *string; Visibility Visibility; Data any }`
  - `func Emit(ctx context.Context, tx pgx.Tx, v *Validator, now time.Time, in EmitInput) (string, error)` — allocates `seq` with `INSERT … ON CONFLICT DO UPDATE SET last_seq = town_subject_seq.last_seq + 1 RETURNING last_seq` (first = 1), new ULID, `time` = `FormatBangkok(now)`, stores `Pending`
  - `var ErrERPOwnedType = errors.New(...)` (type prefix `erp.` or `workflow.` → nothing stored); `var ErrEventInvalid = errors.New(...)` (schema failure → row `Invalid` with `last_error`, plus a `Pending` `system.error` with `data.code` `EVENT_INVALID`, `causation_id` = invalid row id; returns the invalid row id and `ErrEventInvalid`)
  - `func FormatBangkok(t time.Time) string` (RFC 3339 with `+07:00`; import `time/tzdata`)
  - `type PublisherConfig struct { Stream string; RetentionDays, MaxAttempts, BatchSize int; Now func() time.Time }`
  - `func NewPublisher(pool *pgxpool.Pool, rdb *redis.Client, v *Validator, m *platform.Metrics, cfg PublisherConfig) *Publisher`; `func (p *Publisher) PublishDue(ctx context.Context) (int, error)`; `func (p *Publisher) Run(ctx context.Context, every time.Duration) error`

`PublishDue`: one transaction, `SELECT … WHERE status='Pending' AND (next_attempt_at IS NULL OR next_attempt_at <= now) ORDER BY created_at LIMIT BatchSize FOR UPDATE SKIP LOCKED`; per row `XADD stream MINID ~ <now − retention ms>-0 * event <payload>`; success → `Published`, `stream_id`, `published_at`; failure → `attempts+1`, `last_error`, `next_attempt_at = now + min(2^attempts, 300) s`, `OutboxPublishFailures`+1; at `MaxAttempts` → `Failed`, and unless the row is itself `system.error`, `Emit` a `system.error` with `data.code` `OUTBOX_PUBLISH_FAILED`, subject `{"Town Event Outbox", <row id>}`, `causation_id` = row id.

- [ ] **Step 1: Write the failing tests**

```go
func TestEmitAllocatesSeqULIDAndBangkokTime(t *testing.T)    // two emits on one subject → seq 1,2; id matches ^[0-9A-HJKMNP-TV-Z]{26}$; time ends "+07:00" with now in UTC
func TestEmitRefusesERPOwnedTypes(t *testing.T)             // "erp.sales_order.submitted", "workflow.approval.requested" → ErrERPOwnedType; town_outbox empty
func TestEmitInvalidStoresInvalidRowAndReports(t *testing.T) // system.error missing data.code → (id, ErrEventInvalid); row Invalid with last_error; one Pending system.error EVENT_INVALID with causation_id == id
func TestPublishWritesEventFieldByteExact(t *testing.T)     // message "ทดสอบ & <ok>" → XRANGE field "event" == stored payload; row Published with stream_id (Review Focus 1)
func TestPublishBacksOffWhenRedisDown(t *testing.T)          // rdb at 127.0.0.1:1 → attempts 1, Pending, next_attempt_at == now+2s; second call before due → 0 published
func TestPublishFailsAtMaxAttemptsAndReports(t *testing.T)  // attempts preset 9 → Failed; new Pending system.error OUTBOX_PUBLISH_FAILED with subject {Town Event Outbox, id}
func TestFailedSystemErrorDoesNotReport(t *testing.T)       // a system.error row reaching 10 → Failed; no new row
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go -C go test ./internal/events/...`
Expected: FAIL (`Emit` undefined)

- [ ] **Step 3: Implement queries, `Emit`, `FormatBangkok` and `Publisher`**

- [ ] **Step 4: Run tests**

Run: `go -C go generate ./internal/store/... && go -C go test ./internal/events/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add go
git commit   # feat(events): Postgres outbox and publisher matching the Frappe outbox rules — Task: 5
```

---

### Task 6: `erp` domain, Port, fake and conformance suite

**Files:**
- Create: `go/internal/erp/{principal,types,decimal,date,errors,port}.go`, `go/internal/erp/erpfake/fake.go`, `go/internal/erp/porttest/{env,porttest}.go`
- Test: `go/internal/erp/decimal_test.go`, `go/internal/erp/date_test.go`, `go/internal/erp/erpfake/fake_test.go`

**Interfaces:**
- Produces (package `erp`):
  - `type Credential interface{ credential() }`; `type SessionCredential struct { SessionID, CSRFToken string }`; `type APIKeyCredential struct { Key, Secret string }`
  - `type PrincipalKind string` = `user|agent|system|integration`; `type Principal struct { Kind PrincipalKind; ID string; OnBehalfOf string; Roles, Companies []string; Credential Credential }`
  - `type Decimal struct{...}` over `apd.Decimal`: `ParseDecimal(string) (Decimal, error)`, `MustDecimal(string) Decimal`, `String()`, `Cmp(Decimal) int`, `Add`, `Mul`, `RoundTo(places int32) Decimal` (half-up), JSON as string; `type Date struct { Year int; Month time.Month; Day int }`: `ParseDate("2006-01-02")`, `String()`, JSON as string
  - `type DocType string` = `"Sales Order"`, `"Delivery Note"`, `"Sales Invoice"`, `"Payment Entry"` (same strings as event `subject.doctype`); `type DocRef struct { DocType DocType; Name string }`; `type DocStatus string` = `Draft|Submitted|Cancelled`
  - `type LineInput struct { ItemCode string; Qty Decimal; Rate *Decimal }`; `type Line struct { ItemCode string; Qty, Rate, Amount Decimal }`
  - `type SalesOrderInput struct { Company, Customer string; TransactionDate, DeliveryDate Date; Lines []LineInput }` (Company = abbr code; ERPNext defaults fill price list, warehouse and the company's default sales tax template)
  - `type SalesOrder struct { Ref DocRef; Company, Customer string; TransactionDate, DeliveryDate Date; Lines []Line; NetTotal, TaxTotal, GrandTotal Decimal; Status DocStatus; WorkflowState string }`
  - `type DeliveryNote struct { Ref DocRef; Company, Customer string; PostingDate Date; Lines []Line; Status DocStatus; AgainstSalesOrders []string }`
  - `type SalesInvoice struct { Ref DocRef; Company, Customer string; PostingDate, DueDate Date; Lines []Line; NetTotal, TaxTotal, GrandTotal, Outstanding Decimal; Status DocStatus; IsReturn bool }`
  - `type PaymentInput struct { Invoice, ModeOfPayment string; PostingDate *Date }`; `type PaymentEntry struct { Ref DocRef; Company, Party string; PostingDate Date; Paid Decimal; ModeOfPayment string; Status DocStatus; AgainstInvoices []string }`
  - `type SubmitResult struct { Submitted bool; WorkflowState string }`; `type Identity struct { User string; Roles, Companies []string; Credential Credential }`
  - interfaces (every method takes `ctx context.Context, as Principal` first, except `Whoami`):
    - `Documents { Submit(ctx, as, DocRef) (SubmitResult, error); Cancel(ctx, as, DocRef) error }`
    - `Sales { CreateSalesOrder(ctx, as, SalesOrderInput) (DocRef, error); GetSalesOrder(ctx, as, name string) (SalesOrder, error) }`
    - `Delivery { CreateDeliveryNote(ctx, as, salesOrder DocRef, postingDate *Date) (DocRef, error); GetDeliveryNote(ctx, as, name string) (DeliveryNote, error) }`
    - `Billing { CreateSalesInvoice(ctx, as, deliveryNote DocRef, postingDate *Date) (DocRef, error); GetSalesInvoice(ctx, as, name string) (SalesInvoice, error); CreatePayment(ctx, as, PaymentInput) (DocRef, error); GetPaymentEntry(ctx, as, name string) (PaymentEntry, error) }`
    - `Workflow { Actions(ctx, as, DocRef) ([]string, error); Apply(ctx, as, DocRef, action string) (SubmitResult, error) }`
    - `Identities { Whoami(ctx context.Context, cred Credential) (Identity, error) }`
    - `type Port interface { Documents; Sales; Delivery; Billing; Workflow; Identities }`
  - errors: `ErrNotFound`, `ErrForbidden`, `ErrUnauthenticated`, `ErrConflict`, `ErrUnavailable` (sentinels) and `type ValidationError struct { Message string; Fields map[string]string }` with `errors.Is(err, ErrValidation)` true
- Produces (package `porttest`): `type Env struct { Port erp.Port; Company, Customer, LimitedCustomer, Item, BigItem, ModeOfPayment, PendingState, ApproveAction string; BigQty erp.Decimal; Sales, Approver, Stock, Accounts erp.Principal; Today erp.Date }`; `func Run(t *testing.T, env Env)`
- Produces (package `erpfake`): `func New(cfg Config) *Fake` implementing `erp.Port`; `type Config struct { Company string; Prices map[string]erp.Decimal; CreditLimits map[string]erp.Decimal; Users map[string]FakeUser /* by session id */ }`; `func DefaultEnv() porttest.Env` (values below)

Submit semantics (all adapters): no workflow → submit; with a workflow → among the transitions available to the principal take the one leading to a submitted state, else the single available transition (approval request → `Submitted=false`, `WorkflowState` = new state), else `ValidationError{Fields: {"workflow": …}}`.

`DefaultEnv` / ERPNext seed values: Company `ACME-TH`, Customer `CUST-001`, LimitedCustomer `CUST-009` (credit limit 5000), Item `ITEM-002` (rate 12), BigItem `ITEM-018` (rate 4500), BigQty `2`, ModeOfPayment `เงินสด`, PendingState `รออนุมัติ`, ApproveAction `อนุมัติ`; principals: Sales = Sales User, Approver = Sales Manager + Sales User, Stock = Stock User, Accounts = Accounts User.

- [ ] **Step 1: Write the failing tests**

```go
// decimal_test.go / date_test.go
func TestParseDecimalExact(t *testing.T)    // "1234567.89","0.1","135.0" round-trip String() exactly; JSON `"0.1"`
func TestDecimalRoundTo(t *testing.T)       // "8.405".RoundTo(2) == "8.41"
func TestParseDateRejectsBad(t *testing.T)  // "2026-13-01" error; "2026-10-09" → String() same

// porttest.Run subtests
// CreateAndGet: create SO (Customer, Item qty 3) → Get: Status Draft, 1 line, NetTotal == 36, GrandTotal == NetTotal+TaxTotal, |TaxTotal − NetTotal×0.07| ≤ 0.01
// SubmitWithinCredit: Submit → {Submitted:true}; Get → Submitted
// SubmitOverCreditNeedsApproval: LimitedCustomer, BigItem×BigQty → {Submitted:false, WorkflowState:PendingState}; Approver Apply(ApproveAction) → Submitted:true
// FullSalesFlow: SO → DN (Stock) → SI (Accounts) → Payment (Accounts, ModeOfPayment) → SI.Outstanding == 0; DN.AgainstSalesOrders ∋ SO; PE.AgainstInvoices ∋ SI
// ForbiddenForStockUser: Stock CreateSalesOrder → errors.Is ErrForbidden
// NotFound: GetSalesOrder("SO-NOPE") → ErrNotFound
// UnknownCustomerIsValidation: Customer "CUST-NOPE" → errors.Is ErrValidation
// CancelSubmittedOrder: submitted SO without downstream → Cancel → Get Status Cancelled
// WhoamiReturnsRolesAndCompanies: Whoami(Sales.Credential) → User == Sales.ID, Roles ∋ "Sales User", Companies ∋ Company; unknown session → ErrUnauthenticated
func TestFakeConformance(t *testing.T) { porttest.Run(t, erpfake.DefaultEnv()) }
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go -C go test ./internal/erp/...`
Expected: FAIL (package `erp` types undefined)

- [ ] **Step 3: Implement domain types, `erpfake` and `porttest`**

Fake: VAT 7 % exclusive on net, half-up to 2 places; roles enforced like the seed (Sales User: SO; Stock User: DN; Accounts User: SI/PE; Sales Manager: approve); credit check = grand total + customer's unpaid SI total > limit.

- [ ] **Step 4: Run tests**

Run: `go -C go test ./internal/erp/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add go/internal/erp go/go.mod go/go.sum
git commit   # feat(erp): domain types, Port, in-memory fake and porttest suite — Task: 6
```

---

### Task 7: Frappe REST client

**Files:**
- Create: `go/internal/erp/erpnext/client.go`, `go/internal/erp/erpnext/errors.go`
- Test: `go/internal/erp/erpnext/client_test.go`

**Interfaces:**
- Consumes: `erp` credentials, principal, errors (Task 6)
- Produces:
  - `type ClientConfig struct { BaseURL, Site string; Timeout time.Duration; Sleep func(time.Duration) }` (Timeout default 10 s; Sleep default `time.Sleep`, injectable for tests)
  - `func NewClient(cfg ClientConfig) *Client`
  - `func (c *Client) Call(ctx context.Context, as erp.Principal, method string, args any, out any) error` (POST `/api/method/<method>`, JSON body, unwraps `message`)
  - `func (c *Client) Get(ctx context.Context, as erp.Principal, path string, query url.Values, out any) error` (GET `/api/...`, unwraps `data` or `message`)
  - `func (c *Client) CallCred(ctx context.Context, cred erp.Credential, method string, args, out any) error` (for `Whoami`, no principal yet)
  - JSON decoding of responses uses `Decoder.UseNumber()`

Headers on every request: `X-Frappe-Site-Name: <Site>`, `Accept: application/json`; `SessionCredential` → `Cookie: sid=<id>` + `X-Frappe-CSRF-Token`; `APIKeyCredential` → `Authorization: token <key>:<secret>`; `Principal.OnBehalfOf != ""` → `X-Town-On-Behalf-Of: <OnBehalfOf>`. Error mapping by `exc_type` first, HTTP status second: `DoesNotExistError`/404 → `ErrNotFound`; `PermissionError`/403 → `ErrForbidden`; `AuthenticationError`/401 → `ErrUnauthenticated`; `TimestampMismatchError`/409 → `ErrConflict`; `ValidationError`, `LinkValidationError`, `MandatoryError` (and other `*ValidationError`)/417 → `*erp.ValidationError{Message}` (text from `_server_messages`, HTML tags stripped); 5xx, timeout, connection refused → `ErrUnavailable`; anything else → wrapped error with status and `exc_type`.

- [ ] **Step 1: Write the failing tests**

```go
func TestSessionHeaders(t *testing.T)            // httptest server records: Cookie sid=s1, X-Frappe-CSRF-Token c1, X-Frappe-Site-Name test.localhost
func TestTokenAndOnBehalfOfHeaders(t *testing.T) // Authorization "token k:s", X-Town-On-Behalf-Of "sales@acme-th.test"
func TestErrorMapping(t *testing.T)              // table: each exc_type/status above → expected sentinel; 417 with _server_messages `["{\"message\":\"<b>Customer</b> CUST-NOPE not found\"}"]` → ValidationError.Message "Customer CUST-NOPE not found"
func TestGetRetriesTwiceThenFails(t *testing.T)  // server always 503 → 3 requests, Sleep called with 200ms then 800ms, ErrUnavailable
func TestWritesAreNeverRetried(t *testing.T)     // Call to a 503 / hanging server (Timeout 50ms) → exactly 1 request, ErrUnavailable (Review Focus 2)
func TestNumbersDecodeExactly(t *testing.T)      // {"message":{"grand_total":1234567.89}} → json.Number "1234567.89"
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go -C go test ./internal/erp/erpnext/...`
Expected: FAIL (`NewClient` undefined)

- [ ] **Step 3: Implement `Client` and the error mapper**

- [ ] **Step 4: Run tests**

Run: `go -C go test ./internal/erp/erpnext/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add go/internal/erp/erpnext
git commit   # feat(erpnext): Frappe REST client with principal credentials and error mapping — Task: 7
```

---

### Task 8: ERPNext adapter, whoami endpoint and integration harness

**Files:**
- Create: `frappe-apps/erp_town/erp_town/session_api.py`, `frappe-apps/erp_town/erp_town/tests/test_session_api.py`
- Create: `go/internal/erp/erpnext/{adapter,sales,delivery,billing,workflow,documents,identities}.go`, `go/internal/erp/erpnext/testdata/*.json`
- Test: `go/internal/erp/erpnext/mapping_test.go`, `go/internal/erp/erpnext/integration_test.go` (`//go:build integration`)
- Create: `tests/integration/test-site.ts`; Modify: `tests/integration/helpers.ts` (`ErpError`: replace the `readonly status` parameter property with a declared field so Node's type stripping can load the file)

**Interfaces:**
- Consumes: `Client` (Task 7); `erp.Port`, `porttest.Run`, `porttest.Env` (Task 6)
- Produces:
  - Python: `erp_town.session_api.whoami() -> dict` (whitelisted, not for Guest) = `{"user": str, "roles": list[str], "companies": list[str] /* Company abbr the user may read */, "csrf_token": str}` (`frappe.sessions.get_csrf_token()`)
  - `func NewAdapter(c *Client) *Adapter` implementing `erp.Port`; company abbr ↔ name resolved through a cached `Company` list read with the caller's credential
  - `tests/integration/test-site.ts run [--scheduler] -- <cmd> [args...]`: `prepareTestSite({scheduler})`, spawn `<cmd>` with stdio inherited and env `TEST_API_KEY`, `TEST_API_SECRET`, `TOWN_TEST_ERP_URL=http://127.0.0.1:8080`, `TOWN_TEST_ERP_SITE=test.localhost`, `TOWN_TEST_STREAM=town:test:events`, then `restoreTestSite()` in `finally`; exits with the child's code

ERPNext calls used by the adapter (all with the principal's credential): `POST /api/resource/Sales Order` (insert; `taxes_and_charges` = company's `Sales Taxes and Charges Template` with `is_default=1`), `GET /api/resource/<doctype>/<name>`, `erpnext.selling.doctype.sales_order.sales_order.make_delivery_note`, `erpnext.stock.doctype.delivery_note.delivery_note.make_sales_invoice`, `erpnext.accounts.doctype.payment_entry.payment_entry.get_payment_entry` (then insert), `frappe.model.workflow.get_transitions`, `frappe.model.workflow.apply_workflow`, `frappe.client.submit`, `frappe.client.cancel`, `erp_town.session_api.whoami`. `Whoami`: a 403 from whoami (Guest/expired session) → `ErrUnauthenticated`.

- [ ] **Step 1: Write the failing Python test**

```python
class TestSessionApi(FrappeTestCase):
	def test_whoami_returns_roles_companies_and_csrf(self):
		frappe.set_user("Administrator")
		out = whoami()
		self.assertEqual(out["user"], "Administrator")
		self.assertIn("System Manager", out["roles"])
		self.assertTrue(out["csrf_token"])
		self.assertIsInstance(out["companies"], list)
	def test_whoami_is_whitelisted_but_not_for_guest(self):
		self.assertIn(whoami, frappe.whitelisted)
		self.assertNotIn(whoami, frappe.guest_methods)
```

- [ ] **Step 2: Run it to verify it fails, implement `session_api.py` (GPL header like the other modules), run it to pass**

Run: `pnpm erp:test -- --module erp_town.tests.test_session_api`
Expected: first FAIL (module missing), then PASS

- [ ] **Step 3: Write the failing Go tests**

```go
func TestMapSalesOrderExactDecimals(t *testing.T)   // testdata/sales_order.json (grand_total 1234567.89, rate 0.1, qty 135.0) → GrandTotal "1234567.89", Rate "0.1", Qty "135.0" (Review Focus 5)
func TestMapStatusAndWorkflowState(t *testing.T)    // docstatus 0/1/2 → Draft/Submitted/Cancelled; workflow_state "รออนุมัติ" kept
func TestCompanyAbbrMapping(t *testing.T)           // input Company "ACME-TH" → request body company "บริษัท ตัวอย่างเทรดดิ้ง จำกัด"; output maps back to "ACME-TH"
func TestSubmitChoosesTransition(t *testing.T)      // fake transitions [{action:"ยืนยัน",next_state:"อนุมัติแล้ว"(doc_status 1)}] → apply_workflow "ยืนยัน", Submitted true; single [ขออนุมัติ→รออนุมัติ] → Submitted false; two non-submitting → ValidationError fields["workflow"]
// integration_test.go (//go:build integration):
func TestERPNextConformance(t *testing.T) // log in sales@, stock@, accounts@, approver@acme-th.test via POST /api/method/login (password SEED_USER_PASSWORD or "town-dev-123") → sid; Whoami → SessionCredential with CSRF; porttest.Run(t, env with NewAdapter on TOWN_TEST_ERP_URL/SITE)
```

Testdata JSON is captured from `test.localhost` once (document shapes of ERPNext v16) and trimmed; keep Thai names as they are.

- [ ] **Step 4: Run Go unit tests to verify they fail, implement the adapter, run to pass**

Run: `go -C go test ./internal/erp/erpnext/...`
Expected: first FAIL (`NewAdapter` undefined), then PASS

- [ ] **Step 5: Run the integration suite**

Run: `pnpm go:test:integration`
Expected: test.localhost seeded, `TestERPNextConformance` PASS (all porttest subtests), site restored (`pnpm erp:test` still passes afterwards)

- [ ] **Step 6: Commit**

```bash
git add frappe-apps/erp_town go/internal/erp/erpnext tests/integration
git commit   # feat(erpnext): Port adapter, whoami endpoint and Go integration harness — Task: 8
```

---

### Task 9: Authenticator and JWT

**Files:**
- Create: `go/internal/auth/authenticator.go`, `go/internal/auth/jwt.go`
- Test: `go/internal/auth/authenticator_test.go`, `go/internal/auth/jwt_test.go`

**Interfaces:**
- Consumes: `erp.Identities`, `erp.Principal`, `erp.SessionCredential`, `erp.ErrUnauthenticated` (Task 6)
- Produces:
  - `func NewAuthenticator(ids erp.Identities, ttl time.Duration, now func() time.Time) *Authenticator` (ttl 30 s in production)
  - `func (a *Authenticator) FromRequest(r *http.Request) (erp.Principal, error)` — cookie `sid` missing, empty or `Guest` → `ErrUnauthenticated`; else cached `Whoami` per sid → `Principal{Kind: user, ID: identity.User, Roles, Companies, Credential: identity.Credential}`
  - `func NewJWTIssuer(seedBase64 string, ttl time.Duration, now func() time.Time) (*JWTIssuer, error)` (seed must decode to 32 bytes)
  - `func (j *JWTIssuer) Issue(p erp.Principal) (token string, expires time.Time, err error)`; `func (j *JWTIssuer) Verify(token string) (Claims, error)`
  - `type Claims struct { Subject string; Roles, Companies []string; ExpiresAt time.Time }`

- [ ] **Step 1: Write the failing tests**

```go
func TestFromRequestWithoutCookieIsUnauthenticated(t *testing.T) // no cookie / sid=Guest → ErrUnauthenticated, Whoami not called (Review Focus 3)
func TestFromRequestCachesWhoamiFor30s(t *testing.T)            // erpfake ids counting calls: 2 requests at t, t+29s → 1 call; t+31s → 2 calls
func TestExpiredSessionIsUnauthenticated(t *testing.T)          // Whoami returns ErrUnauthenticated → FromRequest returns it (not cached)
func TestJWTRoundTrip(t *testing.T)                             // Issue → Verify: Subject, Roles, Companies equal; ExpiresAt == now+15m
func TestJWTRejectsExpiredAndTampered(t *testing.T)             // now+16m → error; flipped payload byte → error; token signed by another key → error
func TestJWTRejectsBadSeed(t *testing.T)                        // seed of 31 bytes → error
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go -C go test ./internal/auth/...`
Expected: FAIL (`NewAuthenticator` undefined)

- [ ] **Step 3: Implement with `golang-jwt/jwt/v5` (`SigningMethodEdDSA`)**

- [ ] **Step 4: Run tests**

Run: `go -C go test ./internal/auth/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add go/internal/auth go/go.mod go/go.sum
git commit   # feat(auth): Frappe-session authenticator and Ed25519 JWT — Task: 9
```

---

### Task 10: Gateway and `town serve` wiring

**Files:**
- Create: `go/internal/gateway/gateway.go`, `go/internal/gateway/errors.go`, `go/internal/gateway/sales.go`
- Modify: `go/cmd/town/main.go`, `go/internal/platform/config.go` (gateway role requires a valid `TOWN_JWT_PRIVATE_KEY`)
- Test: `go/internal/gateway/gateway_test.go`, `go/internal/platform/config_test.go`

**Interfaces:**
- Consumes: `erp.Port` (Task 6), `Authenticator`, `JWTIssuer` (Task 9), `platform.NewMux`, `Metrics` handler (Tasks 1, 4)
- Produces:
  - `func New(port erp.Port, authn *auth.Authenticator, jwt *auth.JWTIssuer) http.Handler`
  - routes: `GET /town/api/me` → `{"user","roles","companies"}`; `POST /town/api/auth/token` → `{"token","expires_at"}`; `POST /town/api/sales-orders` body `{"company","customer","transaction_date","delivery_date","lines":[{"item_code","qty","rate"?}]}` → 201 `{"name"}`; `GET /town/api/sales-orders/{name}` → SalesOrder JSON (snake_case keys, decimals as strings); `POST /town/api/documents/{doctype}/{name}/submit` (`doctype` ∈ `sales-order|delivery-note|sales-invoice|payment-entry`) → `{"submitted","workflow_state"}`
  - `func WriteError(w http.ResponseWriter, err error)` mapping per Global Constraints; `ValidationError.Fields` → `fields`; unexpected errors logged with a request id, body message `"internal error"`
- `cmd/town`: role `gateway` → `NewAdapter(NewClient(ClientConfig{BaseURL: ERPURL, Site: ERPSite}))`, authenticator (30 s), issuer (15 min), mux with gateway + `/town/metrics`; role `outbox` → `store.Open` + `Migrate` + `Publisher.Run(ctx, 1s)` on `Stream`

- [ ] **Step 1: Write the failing tests** (gateway over `erpfake`, httptest)

```go
func TestStateChangingRequestNeedsTownHeader(t *testing.T)   // POST /town/api/sales-orders without X-Town-Request → 403 {"code":"CSRF_HEADER_MISSING"}
func TestNoSessionIs401(t *testing.T)                        // GET /town/api/me without cookie → 401 UNAUTHENTICATED (Review Focus 3)
func TestCreateGetSubmitSalesOrder(t *testing.T)             // sales session: POST → 201 name; GET → grand_total string; submit → {"submitted":true}
func TestDomainErrorsMapToHTTP(t *testing.T)                 // fake returning each sentinel → status + code table; ValidationError fields echoed
func TestUnavailableIs503AndNotRetried(t *testing.T)         // Port stub returning ErrUnavailable on create → 503 UNAVAILABLE, stub called once (Review Focus 2)
func TestTokenEndpointIssuesVerifiableJWT(t *testing.T)      // POST /town/api/auth/token → Verify(token).Subject == sales user
func TestMalformedBodyIs422(t *testing.T)                    // qty "abc" → 422 VALIDATION fields.lines[0].qty
func TestGatewayRoleRequiresJWTKey(t *testing.T)             // LoadConfig with TOWN_ROLES=gateway, no key → error mentioning TOWN_JWT_PRIVATE_KEY
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go -C go test ./internal/gateway/... ./internal/platform/...`
Expected: FAIL (`gateway.New` undefined)

- [ ] **Step 3: Implement gateway and wiring**

- [ ] **Step 4: Run tests and a manual smoke**

Run: `go -C go test ./... && pnpm lint`
Expected: PASS. Then with the dev stack up and a generated key in `infra/docker/.env` (`TOWN_JWT_PRIVATE_KEY`; document in AGENTS.md how to generate one, e.g. `node -e "console.log(require('crypto').randomBytes(32).toString('base64'))"`): `pnpm dev:town` and `curl http://127.0.0.1:8090/town/api/healthz` → `{"status":"ok"}`; `curl http://127.0.0.1:8090/town/api/me` → 401.

- [ ] **Step 5: Commit**

```bash
git add go AGENTS.md infra/docker/.env.example
git commit   # feat(gateway): authenticated HTTP API over erp.Port and town serve roles — Task: 10
```

---

### Task 11: End-to-end flow on test.localhost

**Files:**
- Test: `go/internal/e2e/sales_flow_test.go` (`//go:build integration`)

**Interfaces:**
- Consumes: `gateway.New`, `NewAdapter`, `NewAuthenticator`, `NewJWTIssuer`, `NewConsumer`, `storetest.NewPool`, test-site env vars (Tasks 4, 8–10)

- [ ] **Step 1: Write the test**

```go
func TestGatewayToStreamToConsumer(t *testing.T) {
	// gateway on httptest.Server with the real ERPNext adapter (TOWN_TEST_ERP_URL / SITE)
	// log in sales@acme-th.test → sid cookie
	// consumer group "e2e-<rand>" on TOWN_TEST_STREAM created at "$" before the request, Postgres from storetest
	// POST /town/api/sales-orders (CUST-001, ITEM-002 × 2) with X-Town-Request: 1 → 201
	// POST /town/api/documents/sales-order/{name}/submit → submitted true
	// within 15 s the handler receives type "erp.sales_order.submitted", subject {Sales Order, name},
	// actor.kind "user", actor.id "user.sales@acme-th.test", company "ACME-TH", seq ≥ 1
	// record publish→consume latency with t.Logf (informational; C's 250 ms target is measured in C)
}
```

- [ ] **Step 2: Run it**

Run: `pnpm go:test:integration`
Expected: PASS (conformance + e2e); afterwards `pnpm erp:test` passes (site restored)

- [ ] **Step 3: Commit**

```bash
git add go/internal/e2e
git commit   # test(e2e): gateway → ERPNext → stream → Go consumer on test.localhost — Task: 11
```

---

### Task 12: Pilot packaging and backup

**Files:**
- Create: `infra/docker/town/Containerfile`
- Modify: `infra/docker/compose.yaml` (service `town`, profile pilot), `infra/docker/postgres/init.sql` (`CREATE DATABASE town_pilot;` — and a note in RESTORE.md for existing volumes: `CREATE DATABASE town_pilot` once by hand), `infra/docker/pilot/Caddyfile`, `infra/docker/pilot/backup.sh`, `infra/docker/pilot/Containerfile` (add `postgresql-client-18` from the PGDG apt repo for `pg_dump`/`pg_restore`), `infra/docker/pilot/RESTORE.md`, `infra/docker/.env.example` (pilot `PILOT_TOWN_*` keys), `scripts/erp.mjs` (`pilot:build` also builds `erp-town/town:<same tag>`; `PILOT_SERVICES` gains `town`), `AGENTS.md` (commands table)

**Interfaces:**
- Consumes: `town serve` (Task 10)
- Produces: image `erp-town/town:v16-<git short sha>[-dirty]` (multi-stage `golang:1.27` → `gcr.io/distroless/static-debian12:nonroot`, `CGO_ENABLED=0`, entrypoint `town serve`); compose `town` env: `TOWN_HTTP_ADDR=0.0.0.0:8090`, `TOWN_DATABASE_URL=postgres://town:${TOWN_PG_PASSWORD}@postgres:5432/town_pilot?sslmode=disable`, `TOWN_REDIS_URL=${PILOT_REDIS_TOWN_URL}`, `TOWN_STREAM=town:events`, `TOWN_ERP_URL=http://frontend:8080`, `TOWN_ERP_SITE=${SITE_NAME}`, `TOWN_JWT_PRIVATE_KEY=${PILOT_TOWN_JWT_PRIVATE_KEY}`, `TOWN_ROLES=gateway,outbox`; Caddy routes `/town/*` to `town:8090` before the Frappe proxy

- [ ] **Step 1: Build and start**

Run: `pnpm erp:pilot:build && pnpm erp:pilot:up`
Expected: both images built; `town` healthy; `curl -k https://pilot.localhost/town/api/healthz` → `{"status":"ok"}` and `curl -k https://pilot.localhost/town/api/me` → 401; `pilot:up` requires `PILOT_TOWN_JWT_PRIVATE_KEY` (add to `PILOT_REQUIRED`)

- [ ] **Step 2: Backup drill**

`backup.sh` also runs `pg_dump -Fc town_pilot` into the same backup set and uploads it; RESTORE.md gains the `pg_restore` step and a check (row count of `town_outbox` before/after). Run: `pnpm erp:pilot:backup`, then follow RESTORE.md's drill on a fresh database.
Expected: the set in the bucket contains the `.dump`; restored `town_outbox` count equals the source count. Record the commands and counts in the commit body (Ruling Z(f) style).

- [ ] **Step 3: Tear down and lint**

Run: `pnpm erp:pilot:down && pnpm lint`
Expected: pilot containers removed, dev stack untouched; lint passes

- [ ] **Step 4: Commit**

```bash
git add infra scripts/erp.mjs AGENTS.md
git commit   # feat(pilot): town image, Caddy /town route and Postgres backup — Task: 12
```

---

## Acceptance check (after Task 12)

1. `pnpm lint`, `pnpm test` (TS + Go + Python) and `pnpm go:test:integration` pass on a fresh clone with `pnpm erp:up`.
2. `pnpm dev:town` serves `/town/api/healthz`; `/town/api/me` → 401 without a session.
3. Program overview tech stack and AGENTS.md describe the Go module, its commands and generated paths.
4. Next: write the C (World State) spec on this skeleton (spec §14).
