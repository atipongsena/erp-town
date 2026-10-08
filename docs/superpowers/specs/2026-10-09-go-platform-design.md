# Go Platform — Design Spec

วันที่: 2026-10-09 · สถานะ: รอผู้ใช้รีวิว · บริบทโปรแกรม: [program-overview](2026-10-07-program-overview.md) · ต่อจาก: [Foundation (B)](2026-10-07-foundation-design.md)

เอกสารนี้เปลี่ยน tech stack ระดับโปรแกรม: บริการใหม่ทั้งหมดของ ERP Town เขียนด้วย **Go** และวางทางให้ย้าย ERP core จาก ERPNext ไปเป็น Go ได้ในเฟส 2 แก้ตาราง "Tech stack ที่ตกลงแล้ว" ของ program overview ตามหัวข้อ 14

## 1. ที่มา เป้าหมาย และขอบเขต

เหตุผลที่ผู้ใช้เลือก Go (ทั้งหมดใช้ร่วมกัน):

1. ประสิทธิภาพและ concurrency (WebSocket, event ปริมาณสูง, latency ต่ำ)
2. deploy/ดูแลง่าย (binary เดียว, image เล็ก, typed)
3. ระยะยาวหลุดจาก Frappe/GPL — เป็นเจ้าของ core เอง ปรับ custom ระดับ enterprise ได้
4. เป็น portfolio

ลำดับความสำคัญ (ผู้ใช้กำหนด): **ทำระบบเกมให้สมบูรณ์ก่อน** ERPNext ยังเป็น system of record ในเฟส 1 เพราะมีระบบไทยหลายอย่างพร้อมแล้ว ใช้เป็นตัวเทียบผลลัพธ์ตอนย้ายในเฟส 2

**เฟส 1 (ในขอบเขต):** Go module ใน monorepo, `erp.Port` + adapter ERPNext, event pipeline ฝั่ง Go, World State (C) ที่เขียนเองแทน Colyseus, gateway/auth แทน Hono, Postgres สำหรับข้อมูลที่ Go เป็นเจ้าของ, agents (E), integrations (F), dev bridge (H) — ทั้งหมดเป็น Go

**นอกขอบเขตเฟส 1:** ERP core ที่เขียนด้วย Go, การแก้งาน Foundation (B) ที่เป็น Python, client 3D (D) ยังเป็น TS/React ใน browser, Analytics/Replay ด้วย DuckDB (ตัดสินใน spec ของ G)

## 2. การตัดสินใจหลัก

| เรื่อง | เลือก | เหตุผล / ทางที่ไม่เลือก |
|---|---|---|
| รูปแบบ | **Go modular monolith** — module เดียว, binary เดียว เปิดบทบาทด้วย `--roles` | microservice ต่อโปรเจกต์ย่อย + gRPC: ต้นทุน deploy/contract/tracing เกินทีม 2–3 คน |
| ERP | ERPNext ยังเป็น system of record; Go เข้าถึงผ่าน `erp.Port` เท่านั้น | เขียน core ใหม่ทันที: ชนเป้าหมาย pilot |
| ย้าย core | เฟส 2: shadow ledger → cut-over ทั้งบริษัทที่รอยต่องวด (หัวข้อ 13) | ย้ายทีละโดเมนที่แตะ ledger: SI ลง GL+สต็อก+ภาษีใน transaction เดียว ต้อง sync สองทาง |
| World State | เขียนเองบน WebSocket + protobuf; ศึกษา Colyseus (MIT) เป็นแนวปฏิบัติ | Colyseus (Node), Nakama (หนักเกิน, ต้องมี DB/plugin ของตัวเอง) |
| ข้อมูลของ Go | **PostgreSQL 18** | เก็บใน Frappe doctype: ผูก core เดิม และไม่ได้ฝึก DB ที่เฟส 2 ใช้ |
| License โค้ด Go | **proprietary** (ยังไม่เปิด) | เปิดทีหลังได้เสมอ ปิดทีหลังไม่ได้ |
| HTTP API | **stdlib `net/http` + Huma v2** (OpenAPI 3.1 อัตโนมัติ) | Gin/Echo: แทบไม่เพิ่มอะไรจาก ServeMux ของ Go 1.22+; Fiber: fasthttp ไม่เข้ากับ `net/http`; Connect-RPC: protobuf ใช้เฉพาะ wire ของ world |
| งานเบื้องหลัง | **River** (job queue บน Postgres, enqueue ใน transaction เดียวกับข้อมูล) — เริ่มใช้ใน E/F | Temporal: ต้องมี server + DB แยก (พิจารณาใหม่ใน spec ของ E ถ้าจำเป็น); Watermill: กติกา outbox/seq ของเราเฉพาะเกินไป |
| การทดสอบ | testify + table-driven, `testing/synctest`, `-race`, goleak, native fuzzing, rapid (property-based), gotestsum, **testcontainers-go** สำหรับ Postgres/Redis | Ginkgo/Gomega: ไม่ใช่สไตล์ Go มาตรฐาน; gomock: ใช้ fake + `porttest` แทน mock |

## 3. โครง module

```
go/                           ← Go module เดียว: github.com/atipongsena/erp-town/go, go 1.27
  cmd/town/                   ← main: town serve --roles=world,gateway,agents,integrations,devbridge
  internal/
    platform/                 ← config (env), slog, OpenTelemetry, redis client, pg pool, http server, graceful shutdown
    events/                   ← envelope + validator + consumer + Go outbox publisher
      gen/                    ← GENERATED จาก packages/event-schema (ห้ามแก้มือ)
    erp/                      ← Port interfaces + domain types + domain errors
      erpnext/                ← adapter: Frappe REST client, mapping doctype ↔ domain
      erpfake/                ← in-memory fake สำหรับ unit test
      porttest/               ← conformance suite ที่ทุก adapter ต้องผ่าน
    store/                    ← Postgres: migrations (goose) + queries
      sqlc/                   ← GENERATED โดย sqlc (ห้ามแก้มือ)
    auth/                     ← Principal, Frappe session check, JWT
    gateway/                  ← HTTP API สำหรับ client (/town/api/*)
    world/                    ← C: projection, rooms, sync, visibility
    agents/                   ← E: runtime, providers, tools, policy, MCP server
    integrations/promptpay/   ← F
    integrations/line/        ← F
    devbridge/                ← H
  tools/                      ← เครื่องมือ codegen/loadtest ที่เป็น Go
packages/world-proto/         ← .proto ของ wire format world (แหล่งความจริงเดียว)
```

กติกาการ import (บังคับด้วย depguard ใน golangci-lint):

- `erp` (Port/domain) ห้าม import `erp/erpnext`; โค้ดนอก `cmd/town` และ `erp/erpnext` ห้าม import `erp/erpnext` — adapter ถูกเลือกที่ `main` เท่านั้น
- `devbridge` ห้าม import `erp` (H ไม่มีสิทธิ์ ERP ตาม program overview)
- `world` ห้ามเขียน ERP: constructor ของ `world` รับเฉพาะ `erp.Projection` (interface อ่าน) — บังคับด้วย type ไม่ใช่ depguard

`town serve --roles=...` ค่าเริ่มต้นตอน dev = ทุกบทบาทใน process เดียว; compose ของ pilot แยก container ต่อบทบาทได้ (เช่น `world` แยกจากที่เหลือ) ด้วย image เดียวกัน

## 4. `erp.Port`

### 4.1 Interface

Interface เล็กแยกตาม bounded context: `Sales` (SO), `Delivery` (DN), `Billing` (SI, PE), `Stock`, `Parties`, `Workflow` และ `Projection` (การอ่านแบบรวมสำหรับ rebuild world ในหัวข้อ 5.3) ตัวอย่าง:

```go
type Sales interface {
    CreateSalesOrder(ctx context.Context, as Principal, in SalesOrderInput) (DocRef, error)
    SubmitSalesOrder(ctx context.Context, as Principal, ref DocRef) error
    GetSalesOrder(ctx context.Context, as Principal, name string) (SalesOrder, error)
}
```

เฟส 1 สร้าง method เท่าที่ slice ขาย (SO → DN → SI → PE) และโปรเจกต์ย่อยที่กำลังทำต้องใช้ เพิ่ม method ทีละตัวพร้อมเคสใน `porttest`

### 4.2 กติกา

1. **ทุก call รับ `Principal`** = `{Kind: user|agent|system|integration, ID, OnBehalfOf, Credential}` adapter ERPNext เรียก Frappe ด้วย credential ของ principal นั้นเสมอ — ห้ามใช้ admin key ทำแทนใคร RBAC + audit เกิดใน ERP ตามหลักการข้อ 3 ของโปรแกรม Go core ในเฟส 2 ต้องบังคับกติกาเดียวกัน
2. **Domain types เป็นของเราเอง:** ชื่อ field ตามความหมายธุรกิจ ไม่ใช่ชื่อ field ERPNext; ยอดเงินและจำนวนใช้ decimal (`github.com/cockroachdb/apd/v3`) ไม่ใช้ float; วันที่ใช้ `civil.Date`-style type ของเราเอง (ไม่มีเวลา/โซน)
3. **Domain errors:** `ErrNotFound`, `ErrForbidden`, `ErrValidation{Fields map[string]string}`, `ErrConflict` (เอกสารถูกแก้ระหว่างทาง), `ErrUnavailable` (ERP ล่ม/timeout) — adapter แปล exception ของ Frappe (`DoesNotExistError`, `PermissionError`, `ValidationError`, `TimestampMismatchError`, HTTP 5xx/timeout) เป็นชุดนี้; error อื่นห่อด้วย `%w` ไม่ทิ้ง
4. **อ่านยอดเงินเพื่อแสดงผลผ่าน Port ด้วยสิทธิ์ผู้ชม** (กติกา §3.2 ของ Foundation: event สาธารณะไม่มียอดเงิน)
5. **`porttest`** คือสัญญาของการย้าย: suite เดียวรันกับ `erpfake` (unit, ทุกครั้ง), `erpnext` บน `test.localhost` (integration) และ Go core ในเฟส 2 เคสต้องเขียนด้วยภาษา domain เท่านั้น

### 4.3 Adapter ERPNext

- HTTP client ของ stdlib, timeout ต่อ call 10 s, retry เฉพาะ GET ที่ได้ `ErrUnavailable` (สูงสุด 2 ครั้ง, backoff 200/800 ms) — ห้าม retry การเขียน
- credential ของผู้ใช้ = cookie `sid` + `X-Frappe-CSRF-Token`; ของ agent/integration = `Authorization: token key:secret`
- mapping doctype ↔ domain อยู่ใน adapter เท่านั้น มี unit test ด้วย JSON ตัวอย่างจาก ERPNext v16

## 5. Event pipeline ฝั่ง Go

### 5.1 Contract

- `pnpm schema:gen` เพิ่มขั้น generate Go types ด้วย `github.com/atombender/go-jsonschema` ลง `go/internal/events/gen/` (มี header ไฟล์ generate) — task แรกของ plan พิสูจน์ว่า generate schema ปัจจุบันได้ครบ ถ้าไม่ได้ให้เขียน generator ใน `go/tools/schemagen` แทน (ผลลัพธ์ path เดิม)
- schema ฝังด้วย `go:embed` (copy ด้วย `schema:gen` เหมือนฝั่ง Python) และ validate ตอน runtime ด้วย `github.com/santhosh-tekuri/jsonschema/v6` (2020-12)
- contract test: golden fixtures ชุดเดิมใน `packages/event-schema/fixtures` ต้องได้ผล valid/invalid ตรงกันใน ajv, Python jsonschema และ Go
- drift test: schema ที่ฝังใน Go ต้องตรงกับต้นฉบับ (แบบเดียวกับ drift test ของ Python)

### 5.2 Consumer

- `github.com/redis/go-redis/v9`, consumer group ต่อบทบาท: `world`, `agents`, `integrations` บน stream ตาม Town Settings (`town:events`; tests ใช้ `town:test:events`)
- ลำดับ: `XREADGROUP` → validate → dedupe ด้วย `id` → apply → `XACK`
- dedupe: ชุด id ที่เห็นแล้วใน Postgres (`consumed_events(group, id)` เก็บ 8 วัน > retention ของ stream 7 วัน)
- seq: ต่อ subject รับเฉพาะ `seq` มากกว่าตัวล่าสุดที่ apply; น้อยกว่าหรือเท่ากับ = ข้าม; ไม่รอ seq ที่หายไป (Ruling AE)
- event ไม่ผ่าน schema: log + metric `town_events_invalid_total` แล้ว ack — ไม่บล็อก stream
- pending ที่ค้างเกิน 60 s (consumer ตาย) ถูก `XAUTOCLAIM` โดย consumer อื่นของกลุ่มเดียวกัน

### 5.3 Go outbox

event ที่ Go ผลิต (`agent.*`, `presence.*`, `dev.*` และ `system.error` ของบริการ Go) ใช้ outbox ใน Postgres: เขียนแถว `town_outbox` ใน transaction เดียวกับข้อมูล → publisher `XADD` → mark published; envelope, ULID, seq ต่อ subject (`town_subject_seq` แบบ upsert เหมือน Frappe), retry/backoff, `max_attempts` 10 และ error code เดิม (`EVENT_INVALID`, `OUTBOX_PUBLISH_FAILED`) — พฤติกรรมเหมือน outbox ของ Frappe ทุกข้อ ยกเว้น `presence.*` ที่ไม่ผ่าน outbox (ชั่วคราว, `XADD` ตรง, หายได้)

## 6. World State (C)

### 6.1 Projection

- `Apply(state, event) state` เป็นฟังก์ชันบริสุทธิ์ ทดสอบด้วย fixture ล้วน
- snapshot ทุก 30 s ลง Postgres (`world_snapshots(company, stream_id, state bytea, taken_at)`) — เริ่ม process: โหลด snapshot ล่าสุด → อ่าน stream ต่อจาก `stream_id`
- ไม่มี snapshot หรือ `stream_id` เก่ากว่าสิ่งที่ stream ยังเก็บ → rebuild ผ่าน `erp.Projection` ด้วย principal `system.town` (Frappe user อ่านอย่างเดียว ไม่มีสิทธิ์เห็นยอดเงิน): SO/DN ที่ค้าง, SI เกินกำหนด, สต็อกต่ำกว่าจุดสั่งซื้อ แล้วอ่าน stream ต่อจากปลายปัจจุบัน

### 6.2 Server

- WebSocket ด้วย `github.com/coder/websocket` ที่ `/town/ws`, ยืนยันตัวด้วย JWT (หัวข้อ 7)
- 1 room = 1 บริษัท (company code); ผู้ใช้เลือก room ได้เฉพาะบริษัทที่ JWT ระบุ
- actor model: goroutine เดียวเป็นเจ้าของ state ของ room (ไม่มี lock บน state); แต่ละ client มี goroutine เขียนและ buffer 256 ข้อความ — เต็มเมื่อไร ตัด client พร้อม close reason `resync` และ client ต้องขอ snapshot ใหม่
- reconnection: token ต่อ session อายุ 30 s หลังหลุด กลับมาได้ที่ room เดิมโดยได้ patch ต่อ ถ้าเกิน → snapshot ใหม่ (แนวเดียวกับ reconnection token ของ Colyseus)

### 6.3 Sync และ visibility

- เข้า room: snapshot ที่กรองตาม visibility ของผู้ชม (`company` + `visibility.level` `public`/`roles`/`private` + roles ใน JWT)
- จากนั้น patch ทุก tick 10 Hz เฉพาะ entity ที่เปลี่ยน; server เก็บ version ล่าสุดที่ส่งต่อ entity ต่อ client (เทียบเท่า StateView) — entity ที่เพิ่งมองไม่เห็นได้ op `remove`
- wire format: protobuf ใน `packages/world-proto/*.proto` → `buf generate` เป็น Go (`protoc-gen-go`) และ TS (`@bufbuild/protobuf` / protobuf-es) ลงไฟล์ generate ทั้งสองฝั่ง
- ข้อความจาก client รับเฉพาะ intent การแสดงผล (เดินตัวละคร, เปิดอาคาร, โฟกัสเอกสาร) — การเปลี่ยนแปลง ERP ผ่าน gateway HTTP → Port เท่านั้น ห้ามผ่าน WS
- แหล่งอ้างอิง: Colyseus (MIT) ด้าน room lifecycle, seat reservation, reconnection, StateView, patch rate — ศึกษาได้อิสระ ถ้าดัดแปลงโค้ดต้องเก็บ license notice

### 6.4 เป้าหมายประสิทธิภาพ

- event เข้า stream → client ได้ patch: **p95 < 250 ms**
- **200 client ต่อ room** บนเครื่อง pilot
- วัดด้วย load test ที่เขียนเป็น Go (`go/tools/loadtest`); พลาดเพราะโค้ด → แก้โค้ด; พลาดเพราะสภาพแวดล้อม dev บน Windows → รายงานพร้อมตัวเลข ไม่ลดเกณฑ์ (แนวเดียวกับ Ruling Y)

## 7. Gateway และ Auth

- `net/http` ของ stdlib + **Huma v2** (adapter `humago` บน ServeMux): request/response แบบ typed, validate input อัตโนมัติ, OpenAPI 3.1 ที่ `GET /town/api/openapi.json` และหน้าเอกสาร `/town/api/docs`; client (D) generate TS client ด้วย `openapi-typescript`; ทุก route อยู่ใต้ `/town/api/*`; error body ใช้รูปแบบของเรา (`{"code","message","fields"}`) แทน problem+json ของ Huma
- Caddy (pilot) / dev proxy ทำ same-origin: `/` → Frappe, `/town/*` → Go — cookie `sid` ของ Frappe จึงมาถึง gateway
- Principal ของผู้ใช้: gateway ตรวจ `sid` กับ Frappe (`frappe.auth.get_logged_user` + roles) cache 30 s ต่อ sid; การเขียนผ่าน Port ส่ง `sid` + CSRF token ของผู้ใช้
- `POST /town/api/auth/token` → JWT EdDSA (Ed25519) อายุ 15 นาที claims: `sub` (user), `roles`, `companies`; ใช้กับ WebSocket เท่านั้น — key อยู่ใน `.env` (`TOWN_JWT_PRIVATE_KEY`, ชื่อ key ลง `.env.example`)
- Principal ของ agent: API key ของ Frappe user ที่ผูกกับ Town Agent Identity (มีใน B) + `on_behalf_of`
- Principal ของ integration (LINE/PromptPay): Frappe user ของ integration เอง role จำกัด
- เฟส 2: credential เปลี่ยนเป็น session ที่ Go ออกเอง — interface ของ Port ไม่เปลี่ยน

## 8. PostgreSQL

- PostgreSQL 18 เป็น service ใหม่ใน compose; dev bind `127.0.0.1:5433`; database `town` (dev), `town_pilot` (pilot); เทสต์ใช้ testcontainers
- เก็บเฉพาะข้อมูลที่ไม่ใช่ข้อมูลธุรกิจ ERP: agent runs, tool-call audit, XP/level, ความจำ agent, world snapshots, `consumed_events`, `town_outbox`, `town_subject_seq`
- `github.com/jackc/pgx/v5` + sqlc (query type-safe) + goose (migration ใน `go/internal/store/migrations`, รันตอน start)
- Go ไม่แตะ MariaDB ของ ERPNext โดยตรงเด็ดขาด
- backup: ขยาย `infra/docker/pilot/backup.sh` ให้ `pg_dump` ไป S3 bucket เดียวกัน และเพิ่มขั้น restore ใน `RESTORE.md`
- เฟส 2: เป็นบ้านของ shadow ledger และ Go core

## 9. Agents (E)

- Provider adapters: Claude → `github.com/anthropics/anthropic-sdk-go`; OpenAI Responses → `github.com/openai/openai-go`; Codex ไม่มี Go SDK → สั่ง `codex app-server` (JSON-RPC บน stdio) แยกเป็น adapter เดี่ยว (เปราะกว่า มีเทสต์ด้วย process ปลอม)
- Tool registry เดียว: แต่ละ tool map ไปที่ method ของ Port เปิดเป็น native tool ของ Claude/OpenAI และเป็น MCP server ด้วย `github.com/modelcontextprotocol/go-sdk` สำหรับ Codex/client ภายนอก
- Policy proxy (ทุก tool call ไม่มีทางอ้อม): allowlist action ต่อ agent → เพดานยอดเงิน/จำนวน (เกิน = ส่ง workflow ขออนุมัติแทน submit) → rate limit → audit ลง Postgres → Port ด้วย Principal ของ agent; error code เดิม `ON_BEHALF_OF_REJECTED` / `ON_BEHALF_OF_INVALID`
- agent run แบบยาว/ตั้งเวลา/retry ใช้ **River** (job อยู่ใน Postgres, enqueue ใน transaction เดียวกับ audit)
- XP/level คำนวณจาก `agent.*` event ใช้กับ AI agents เท่านั้น (หลักการข้อ 4)
- รายละเอียด (prompt, ชุด tool, ค่า policy) อยู่ใน spec ของ E

## 10. Integrations (F) และ Dev bridge (H)

- **PromptPay:** package `promptpay` สร้าง payload EMVCo (tag 29 เบอร์/เลขบัตร, tag 30 bill payment) + CRC16-CCITT ทดสอบด้วย test vector; การยืนยันสลิป/API ธนาคารตัดสินใน spec ของ F
- **LINE OA:** `github.com/line/line-bot-sdk-go/v8`, ตรวจลายเซ็น webhook (HMAC-SHA256 ด้วย channel secret) ก่อนอ่าน body; รับ webhook แล้ว enqueue งานลง River ตอบ 200 ทันที; บันทึก Payment Entry ผ่าน Port ด้วย Principal ของ integration
- **Dev bridge:** hooks ของ Claude Code/Codex `POST /town/api/dev/events` (bind เฉพาะ 127.0.0.1, token ใน `.env`) → event `dev.*` ผ่าน Go outbox; `devbridge` ไม่ import `erp` (depguard)

## 11. Toolchain, การทดสอบ และ deploy

- `go.mod`: `go 1.27`; lint: golangci-lint v2 (gofumpt, govet, staticcheck, errcheck, depguard ตามหัวข้อ 3)
- คำสั่ง pnpm ใหม่ (ผ่าน `scripts/erp.mjs` หรือ script ใหม่ใน root):
  - `pnpm go:test` = `go test ./...` ใน `go/`
  - `pnpm go:lint` = golangci-lint
  - `pnpm go:gen` = Go types จาก schema + sqlc + buf
  - `pnpm go:test:integration` = `go test -tags integration ./...` กับ `test.localhost`, `town:test:events` (Postgres จาก testcontainers)
  - `pnpm dev:town` = `air` (hot reload ของ `town serve`) บน Windows โดยตรง ต่อ `redis-town` (127.0.0.1:6380) และ Postgres (127.0.0.1:5433); HTTP ที่ `127.0.0.1:8090`
- `pnpm test` และ `pnpm lint` เรียก `go:test` / `go:lint` ด้วย; `pnpm schema:gen` เรียกขั้น Go ด้วย
- integration: แยก seed/restore ของ `test.localhost` จาก `tests/integration/setup.ts` เป็น `scripts/test-site.mjs prepare|restore` ให้ Vitest และ Go ใช้ชุดเดียว (Go เรียกผ่าน `TestMain`)
- unit test ใช้ `erpfake` + Postgres 18/Redis 7 จริงจาก **testcontainers-go** (หนึ่ง container ต่อ package, schema/DB index แยกต่อเทสต์) — ไม่ mock DB และไม่ต้องเปิด dev stack; integration กับ ERPNext ยังใช้ dev stack (`test.localhost`)
- เครื่องมือทดสอบ: testify, `testing/synctest` (ตรรกะที่ขึ้นกับเวลาแต่ไม่มี I/O จริง), `-race` ทุกครั้ง (ต้องมี gcc — dev host ใช้ MSYS2 ucrt64), goleak ใน package ที่มี goroutine, native fuzzing (decode envelope, แปลง error ของ Frappe, `Decimal`/`Date`), rapid (invariant ของ `Decimal` และ seq/dedupe; เฟส 2: บัญชีคู่สมดุล), gotestsum เป็นตัวรัน, benchmark ด้วย `b.Loop()` + benchstat
- pilot image: `infra/docker/town/Containerfile` multi-stage (`golang:1.27` → distroless static), `CGO_ENABLED=0`; service `town` ใน profile `pilot` (และ `dev` เป็นทางเลือกเมื่อไม่รัน native)
- AGENTS.md: เพิ่มคำสั่ง Go, path ไฟล์ generate ใหม่ (`go/internal/events/gen/`, `go/internal/store/sqlc/`, ไฟล์ generate ของ world-proto) ในกฎห้ามแก้มือ และ Go ต้องผ่าน `pnpm go:lint` ก่อน commit

## 12. License และ clean-room

- โค้ดใน `go/` และ `packages/world-proto`: **proprietary** — ไฟล์ `go/LICENSE` ระบุ "All rights reserved" ผู้ถือลิขสิทธิ์ = เจ้าของโปรเจกต์; ไม่ใส่ GPL header
- `frappe-apps/erp_town` ยัง GPLv3 ตามเดิม; Go คุยกับ ERPNext ผ่าน HTTP API เท่านั้น (แยก process)
- **Clean-room:** ห้ามแปลหรือดัดแปลงโค้ดของ Frappe/ERPNext/HRMS มาเป็น Go ตรรกะธุรกิจของ Go core เขียนจาก spec ที่อ้างกฎหมาย/ประกาศกรมสรรพากร/มาตรฐานบัญชี; ERPNext ใช้เป็น **oracle** ในเทสต์เท่านั้น (ผลลัพธ์ต้องตรงกัน ไม่ใช่โค้ดต้องเหมือน)
- โค้ดใน `erp_town` ที่เราเขียนเองนำแนวคิดมาใช้ได้ (เราถือลิขสิทธิ์) — ยกเว้นส่วนที่ดัดแปลงจาก ERPNext
- Colyseus/ไลบรารี MIT/Apache: ดัดแปลงได้โดยเก็บ license notice
- ให้นักกฎหมายตรวจก่อนขายเชิงพาณิชย์ (ตามความเสี่ยงที่ program overview บันทึกไว้)

## 13. Roadmap เฟส 2 (Go core)

**เงื่อนไขเริ่ม:** C, D, E เสร็จ และ pilot ปิดงวดบัญชีจริงใน ERPNext ได้ครบ 1 เดือน (ต้องมีจัดซื้อ + ภาษีซื้อ + ภ.พ.30 ตามความเสี่ยง G2 ของโปรแกรม)

| ขั้น | งาน | เกณฑ์ผ่าน |
|---|---|---|
| P2.0 | clean-room spec: ภาษีขาย/ซื้อ, ใบกำกับภาษี ม.86/4, หัก ณ ที่จ่าย, ภ.พ.30, เลขที่เอกสาร | นักบัญชีเซ็นรับรอง |
| P2.1 | ย้ายส่วนที่ไม่ใช่ ledger ผ่าน Port ทีละส่วน: Parties, การออกเลขที่เอกสาร, เอกสารพิมพ์ไทย/PDF | `porttest` ผ่าน + เอกสารพิมพ์ตรงกับ ERPNext (เทียบภาพ) |
| P2.2 | Shadow ledger: Go ลงบัญชีคู่ใน Postgres จาก event + เอกสารผ่าน Port; reconcile งบทดลองรายบัญชีกับ GL ของ ERPNext ทุกวัน ต่างกัน = แจ้งเตือน | ตรงกันทุกบัญชีครบ 3 งวดติดกัน |
| P2.3 | Shadow stock ledger (moving average) เทียบ Stock Ledger Entry | ตรงกันครบ 3 งวดติดกัน |
| P2.4 | Cut-over ทั้งบริษัทที่รอยต่องวด: ย้าย master data + ยอดยกมา; ประวัติเก่าอยู่ใน ERPNext แบบอ่านอย่างเดียว; บริษัทใหม่เริ่มบน Go core ได้เลย | `porttest` ผ่านกับ Go core + ซ้อมย้ายบนสำเนาข้อมูลจริง |

ส่วนที่แตะ ledger (SO/DN/SI/PE, สต็อก, GL) **ไม่ย้ายทีละโดเมน** — cut-over ทั้งบริษัทครั้งเดียว ส่วนที่ไม่แตะ ledger (P2.1) ย้ายทีละส่วนได้

## 14. ผลกระทบต่อเอกสารและงานเดิม

แก้ตาราง tech stack ใน program overview:

| ชั้น | เดิม | ใหม่ |
|---|---|---|
| World State (C) | Node 24 + Colyseus | Go: WebSocket (`coder/websocket`) + protobuf, เขียนเอง |
| API/BFF | Hono บน Node | Go `net/http` (gateway ใน binary `town`) |
| Agents (E) | TS service | Go: anthropic-sdk-go, openai-go, Codex app-server, MCP go-sdk |
| PromptPay/LINE (F), Dev bridge (H) | ไม่ระบุภาษา | Go |
| ข้อมูลของบริการ | — | PostgreSQL 18 |
| HTTP API framework | Hono | Huma v2 บน `net/http` |
| งานเบื้องหลัง | — | River (E/F) |
| ทดสอบฝั่ง Go | — | testify, synctest, goleak, rapid, fuzz, gotestsum, testcontainers-go |
| Monorepo | pnpm, Biome, Vitest, Playwright | + Go module `go/`, golangci-lint, buf, sqlc, air |

ไม่เปลี่ยน: ERPNext v16 + HRMS + MariaDB, Redis Streams `redis-town`, event envelope, client (D) Vite/React/R3F, Blender/asset pipeline, Docker Compose profiles, `scripts/erp.mjs`, `packages/event-schema` (เพิ่มขั้น generate Go)

ลำดับงานถัดไป: **Go Platform skeleton** (plan แยก) ก่อน C — module + platform, codegen schema + consumer + contract test, `erp.Port` เท่าที่ slice ขายต้องใช้ + `erpnext` adapter + `erpfake` + `porttest`, gateway/auth/JWT, Postgres + migrations + Go outbox, compose/pnpm/AGENTS.md จากนั้นเขียน spec ของ C บนฐานนี้

## 15. ความเสี่ยง

- **เขียน sync engine เองแทน Colyseus** ใช้เวลามากกว่าคาด → จำกัดขอบเขตเฟส 1 ที่ snapshot + patch ระดับ entity (ไม่มี prediction/rollback), ใช้ Colyseus เป็นแนวอ้างอิง
- **Codex ไม่มี Go SDK** → adapter ผ่าน `codex app-server` อาจพังเมื่อ Codex อัปเดต: pin เวอร์ชัน Codex CLI และมีเทสต์ contract ของ JSON-RPC
- **Frappe session/CSRF** เป็นรายละเอียดภายในของ Frappe → แยกไว้ใน `erp/erpnext` + `auth` เท่านั้น ไม่รั่วไปที่อื่น
- **สอง runtime บน server (Python + Go)** เพิ่มภาระ ops ในเฟส 1 → ยอมรับได้เพราะ Go เป็น binary เดียว; หมดไปเมื่อ cut-over เฟส 2
- **Clean-room** พลาดได้ถ้า agent อ่านโค้ด ERPNext แล้วแปลมา → กติกาใน AGENTS.md: งาน Go core ห้ามเปิดซอร์ส ERPNext เป็นต้นแบบ ใช้ได้แค่ผลลัพธ์จาก API/รายงาน
- **License proprietary** ทำให้การเปิด portfolio ต้องตัดสินใจทีหลัง (เปิดบางส่วน, เดโม, หรือเปลี่ยน license)
