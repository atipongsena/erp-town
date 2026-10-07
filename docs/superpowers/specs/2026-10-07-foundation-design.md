# B: Foundation — Design Spec

วันที่: 2026-10-07 · สถานะ: รอผู้ใช้รีวิว · บริบทโปรแกรม: [program-overview](2026-10-07-program-overview.md)

## 1. เป้าหมายและขอบเขต

สร้างฐานที่ทุกโปรเจกต์ย่อยพึ่งพา:

1. ERPNext v16 + HRMS รันได้ด้วย Docker Compose บนเครื่อง dev (Windows) และ VPS สำหรับ pilot
2. Frappe app `erp_town`: ส่ง event ของ flow ขายออกทาง outbox → Redis Streams อย่างถูกต้อง ไม่หาย ไม่หลอก
3. Thai localization ขั้นต่ำที่ทำให้บริษัทซื้อมาขายไปใช้ flow ขายจริงและทำรายงานภาษีขายได้
4. Event schema กลาง (`packages/event-schema`) ที่ C/D/E/G/H ใช้ร่วม
5. Monorepo + เครื่องมือให้ Claude Code และ Codex ทำงานสลับกันได้

**นอกขอบเขต B:** World State/WebSocket (C), ฉาก 3D (D), agent runtime (E), PromptPay/LINE (F), avatar presence/Replay UI (G), dev-agent bridge (H), หัก ณ ที่จ่าย/ภ.ง.ด.3/53, e-Tax Invoice, ภาษีซื้อ/ภ.พ.30, BE ใน Desk ทั้งระบบ, CI บน cloud

## 2. โครง repo

```
D:\ERP_Town
├─ AGENTS.md                    ← คำสั่งหลักสำหรับ AI agents (Codex อ่านตรง)
├─ CLAUDE.md                    ← "@AGENTS.md" + ส่วนเฉพาะ Claude (ถ้ามี)
├─ .mcp.json                    ← MCP สำหรับ Claude (blender)
├─ .codex/config.toml           ← MCP สำหรับ Codex (blender ชุดเดียวกัน)
├─ package.json                 ← pnpm workspace root + scripts ร่วม
├─ pnpm-workspace.yaml
├─ biome.json
├─ frappe-apps/erp_town/        ← Python, LICENSE = GPLv3
├─ packages/event-schema/       ← JSON Schema + fixtures + TS types ที่ generate
├─ infra/docker/                ← compose.yaml, apps.json, Dockerfile, .env.example
├─ tests/                       ← Playwright (print formats, e2e)
└─ docs/superpowers/{specs,plans}/
```

**Scripts ร่วม (root `package.json`):** `erp:up`, `erp:down`, `erp:logs`, `erp:seed`, `erp:scenario`, `erp:test`, `erp:shell`, `schema:gen`, `test` (รวม Vitest + contract + `erp:test`), `test:e2e`, `lint`, `format`

`package.json` ปัจจุบัน (npm + Playwright) จะถูกแปลงเป็น pnpm workspace root; ตัวอย่าง `tests/example.spec.ts` ถูกแทนด้วยเทสต์จริง

## 3. Event contract (`packages/event-schema`)

### 3.1 Envelope (อิง CloudEvents 1.0)

```json
{
  "specversion": "1.0",
  "id": "01J9Z...",
  "type": "erp.sales_order.submitted",
  "source": "erp/selling",
  "time": "2026-10-07T10:15:00+07:00",
  "schema_version": 1,
  "company": "ACME-TH",
  "actor": { "kind": "user", "id": "user.somchai", "on_behalf_of": null },
  "subject": { "doctype": "Sales Order", "name": "SO6910-0042" },
  "seq": 3,
  "correlation_id": "SO6910-0042",
  "causation_id": "01J9Y...",
  "visibility": { "level": "public", "roles": [] },
  "data": { }
}
```

| Field | กติกา |
|---|---|
| `id` | ULID, idempotency key — ผู้รับทุกตัวต้องข้าม id ที่เคยเห็น |
| `type` | `erp.<doctype_snake>.<action>`, `workflow.*`, `agent.*`, `presence.*`, `dev.*`, `system.error` |
| `time` | ISO 8601 พร้อม offset ตาม timezone ระบบ (`Asia/Bangkok`) |
| `actor.kind` | `user` \| `agent` \| `system` \| `dev` |
| `actor.on_behalf_of` | ตั้งได้เฉพาะเมื่อ `kind = agent` |
| `seq` | เริ่มที่ 1 ต่อ subject เพิ่มทีละ 1 ไม่มีช่องว่าง; ผู้รับเรียงตาม seq |
| `correlation_id` | ชื่อเอกสารราก (root) ของสาย flow |
| `causation_id` | `id` ของ event ล่าสุดของเอกสารต้นทางที่ทำให้เกิด event นี้ หรือ `null` |
| `visibility.level` | `public` \| `roles` \| `private`; `roles` ใช้คู่กับรายชื่อ role |
| `data` | เฉพาะข้อมูลที่ปลอดภัยต่อการแสดงผลตาม visibility |

### 3.2 กติกาของ `data`

- ไม่มีข้อมูลส่วนบุคคล: อ้างคู่ค้าด้วยรหัส (`CUST-001`) เท่านั้น; ชื่อแสดงผลให้ client ดึงจาก ERP API ด้วยสิทธิ์ผู้ชม
- **schema อนุญาต field ยอดเงินเฉพาะเมื่อ `visibility.level != public`**; mapper ใน slice นี้**ไม่ใส่ยอดเงิน** — ตัวเลขเงินบน dashboard ให้ D ดึงจาก ERP API ด้วยสิทธิ์ผู้ชม
- เปลี่ยน schema แบบเพิ่ม field เท่านั้น; การเปลี่ยนแบบอื่นต้องขึ้น `schema_version`

### 3.3 Event ของ slice ขาย

| type | subject | actor | data |
|---|---|---|---|
| `erp.sales_order.submitted` | Sales Order | user/agent | `status`, `party`, `item_count`, `delivery_date` |
| `erp.sales_order.cancelled` | Sales Order | user/agent | `status` |
| `erp.delivery_note.submitted` | Delivery Note | user/agent | `status`, `party`, `item_count`, `warehouse`, `against` (SO names) |
| `erp.sales_invoice.submitted` | Sales Invoice | user/agent | `status`, `party`, `due_date`, `against` (SO/DN names) |
| `erp.sales_invoice.overdue` | Sales Invoice | system (`system.scheduler`) | `due_date`, `days_overdue` |
| `erp.payment_entry.submitted` | Payment Entry | user/agent | `party`, `mode_of_payment`, `against` (SI names) |
| `erp.stock.below_reorder` | Item | system | `warehouse`, `fill_ratio` (actual_qty / reorder_level, ทศนิยม 2 ตำแหน่ง) |
| `workflow.approval.requested` | เอกสารที่เข้า workflow | user/agent | `from_state`, `to_state`, `approver_role` |
| `workflow.approval.decided` | เอกสารที่เข้า workflow | user | `from_state`, `to_state`, `decision` (`approved`/`rejected`) |
| `system.error` | เอกสารที่เกี่ยวข้อง หรือ `{doctype: "Town Event Outbox"}` | system | `code`, `message` (ไม่มี stack trace) |

`cancelled` ใช้กับ DN/SI/PE ด้วยรูปแบบเดียวกับ SO (`erp.<doctype>.cancelled`)

Causation ของ event ที่ระบบสร้าง: `erp.sales_invoice.overdue` → event ล่าสุดของ SI ใบนั้น, correlation เดียวกับ SI; `erp.stock.below_reorder` → event `erp.delivery_note.submitted` ที่ทำให้สต็อกข้ามเส้น, correlation เดียวกับ DN

### 3.4 แหล่งความจริงและการ generate

- `packages/event-schema/schema/*.json` — JSON Schema 2020-12 (envelope + data ต่อ type)
- `packages/event-schema/fixtures/*.json` — golden examples ทั้งที่ valid และ invalid
- `pnpm schema:gen` → TS types (json-schema-to-typescript) และ copy schema ไป `frappe-apps/erp_town/erp_town/events/schema/` (ไฟล์ generate, ห้ามแก้มือ, มี header บอก)
- contract test: fixtures ชุดเดียวผ่าน ajv (TS) และ jsonschema (Python)

## 4. Frappe app `erp_town`

### 4.1 DocTypes

| DocType | ประเภท | Fields หลัก |
|---|---|---|
| `Town Event Outbox` | ปกติ | `event_id` (unique), `event_type`, `subject_doctype`, `subject_name`, `seq`, `correlation_id`, `payload` (JSON), `status` (`Pending`/`Published`/`Failed`/`Invalid`), `attempts`, `next_attempt_at`, `last_error`, `stream_id`, `published_at`; index (`status`, `next_attempt_at`), (`subject_doctype`, `subject_name`, `seq`) |
| `Town Subject Seq` | ปกติ | `subject_doctype`, `subject_name` (unique คู่), `last_seq` |
| `Town Agent Identity` | ปกติ | `user` (Link User, unique), `agent_key` (เช่น `agent.sales_assistant`), `enabled` |
| `Town Settings` | Single | `redis_town_url`, `stream_name` (ค่าเริ่มต้น `town:events`), `stream_retention_days` (7), `outbox_retention_days` (400), `max_attempts` (10), `overdue_job_hour` (8), ตาราง `naming_rules` (doctype, prefix) |

### 4.2 การจับ event

- `hooks.py` → `doc_events` ของ Sales Order, Delivery Note, Sales Invoice, Payment Entry: `on_submit`, `on_cancel` (`on_update_after_submit` ยังไม่ใช้ใน slice นี้เพราะไม่มี event type รองรับ — เพิ่มพร้อม schema เมื่อ slice ถัดไปต้องการ)
- workflow: hook `on_change` ตรวจการเปลี่ยน `workflow_state` → `workflow.approval.requested` เมื่อเข้าสถานะรออนุมัติ, `workflow.approval.decided` เมื่อออกจากสถานะนั้น
- `erp.stock.below_reorder`: ตรวจตอน DN submit — ต่อ item ที่อยู่ใน DN เทียบ `Bin.actual_qty` กับ reorder level ของ Item ใน warehouse นั้น; ออกเฉพาะเมื่อข้ามเส้นลงมา (ก่อนหน้า ≥ เกณฑ์, หลัง < เกณฑ์)
- mapper หนึ่งไฟล์ต่อ doctype (`erp_town/events/mappers/<doctype>.py`) ลงทะเบียนใน registry; ฟังก์ชันกลาง `emit(doc, action)`:
  1. สร้าง envelope ผ่าน mapper
  2. ระบุ actor: `frappe.session.user` → ถ้าตรงกับ `Town Agent Identity` ที่ enabled → `kind=agent`, `id=agent_key` และอ่าน header `X-Town-On-Behalf-Of` (ต้องเป็น User ที่มีอยู่จริง); ถ้าไม่ใช่ agent แต่ส่ง header มา → ไม่สนใจ header และออก `system.error` code `ON_BEHALF_OF_REJECTED`; งานจาก scheduler → `kind=system`
  3. หา `causation_id` / `correlation_id` จากเอกสารต้นทาง (DN: `items.against_sales_order`, SI: `items.sales_order`/`items.delivery_note`, PE: `references.reference_name`) → แถว outbox ล่าสุดของเอกสารต้นทาง; ถ้าต้นทางหลายใบใช้ใบแรกตามลำดับรายการ และใส่ทั้งหมดใน `data.against`; ถ้าไม่มีต้นทาง → `correlation_id = ชื่อเอกสารเอง`, `causation_id = null`
  4. จอง `seq` ด้วย `SELECT … FOR UPDATE` บน `Town Subject Seq` (insert ถ้ายังไม่มี) ใน transaction เดียวกับเอกสาร
  5. validate กับ JSON Schema → ผ่าน: insert outbox `Pending`; ไม่ผ่านหรือ mapper ขว้าง exception: insert outbox `Invalid` พร้อม `last_error` และออก `system.error` — **ไม่บล็อกการ submit เอกสาร**
  6. `frappe.enqueue("erp_town.events.publisher.publish", queue="short", event_ids=[...], enqueue_after_commit=True)`

ถ้าเอกสาร rollback แถว outbox และ seq rollback ไปด้วย และ job ไม่ถูก enqueue

### 4.3 Publisher และ sweeper

- `publish(event_ids)`: เลือกแถว `Pending` ตาม id ด้วย `FOR UPDATE SKIP LOCKED` → `XADD <stream> MINID ~ <now - stream_retention_days> * event <payload>` → อัปเดต `Published`, `stream_id`, `published_at`; ถ้า Redis error → `attempts += 1`, `next_attempt_at = now + min(2^attempts, 300) วินาที`, เก็บ `last_error`; ถ้า `attempts >= max_attempts` → `Failed` + `system.error` code `OUTBOX_PUBLISH_FAILED`
- `sweep()` (scheduler ทุก 1 นาที): แถว `Pending` ที่ `creation < now - 30s` และ `next_attempt_at <= now` เรียง `creation`, ครั้งละไม่เกิน 500 → `publish`
- `emit_overdue()` (รายวันตาม `overdue_job_hour`): SI ที่ `docstatus=1`, `outstanding_amount > 0`, `due_date < today` → `erp.sales_invoice.overdue` ได้ไม่เกิน 1 ครั้งต่อใบต่อวัน (ตรวจซ้ำจาก outbox)
- `purge()` (รายวัน): ลบแถว `Published` ที่เก่ากว่า `outbox_retention_days`
- การเชื่อม `redis-town` ใช้ client แยกจาก Redis ของ Frappe

### 4.4 Town Status

Workspace "ERP Town" ใน Desk: number cards (Pending, Failed, Invalid, อายุแถว Pending ที่เก่าสุด), report "Town Outbox Status" พร้อมปุ่ม "ส่งใหม่" สำหรับแถว `Failed` (ตั้งกลับเป็น `Pending`, รีเซ็ต attempts) สิทธิ์: System Manager

### 4.5 ธีม Desk

`app_include_css` โหลด `erp_town/public/css/town-desk.css` ที่ใช้ CSS custom properties (สี, ฟอนต์, รัศมีมุม) ค่าใน B เป็นค่ากลาง; ค่าจริงมาจาก art bible (A) โดยแก้เฉพาะ tokens

### 4.6 License

`frappe-apps/erp_town/LICENSE` = GPLv3 ไม่มีโค้ดของ C/D/E/H อยู่ใน app นี้

## 5. Thai localization

### 5.1 ภาษีและการตั้งค่า

- Fixture ผังบัญชีตั้งต้นตามโครง TFRS for NPAEs (ให้นักบัญชีตรวจรายการบัญชี)
- Sales Taxes and Charges Template: "ภาษีขาย 7%" (แยก VAT) และ "ภาษีขาย 7% (ราคารวม VAT)" (`included_in_print_rate`)
- Item Tax Template: ยกเว้น VAT และ VAT 0%
- วิธีตีมูลค่าสต็อกตั้งต่อบริษัท ค่าเริ่มต้น Moving Average
- Custom fields (prefix `th_`):
  - `th_branch_code` (Data 5 หลัก, ค่าเริ่มต้น `00000` = สำนักงานใหญ่) บน Company, Customer, Address
  - validation เลขผู้เสียภาษี 13 หลัก (checksum mod 11) บน Company.`tax_id` และ Customer.`tax_id` — ไม่ผ่านให้บล็อกการบันทึกพร้อมข้อความภาษาไทย; ว่างได้สำหรับลูกค้าบุคคลที่ไม่ขอใบกำกับเต็มรูป

### 5.2 เลขที่เอกสาร (ปี พ.ศ.)

- autoname hook ของ SO, DN, SI, PE: `<prefix><พ.ศ. 2 หลัก><เดือน 2 หลัก>-<ลำดับ 4 หลัก>` เช่น `IV6910-0001`
- prefix ค่าเริ่มต้น: SO → `SO`, DN → `DN`, SI → `IV`, PE → `RC`; แก้ได้ใน `Town Settings.naming_rules`
- ลำดับเริ่มใหม่ทุกเดือนต่อ prefix (ใช้ตัวนับของ Frappe naming series คีย์ `<prefix><ปีเดือน>-`)
- เอกสารที่ amend ใช้รูปแบบ amend ปกติของ Frappe (`-1`, `-2`)

### 5.3 เอกสารพิมพ์

ใช้ตัวแปลง PDF แบบ Chrome ของ v16 และฟอนต์ Sarabun (OFL) ฝังในแอป

| Doctype | Print format | หมายเหตุ |
|---|---|---|
| Sales Order | ใบยืนยันคำสั่งซื้อ | |
| Delivery Note | ใบส่งของ | |
| Sales Invoice | ใบกำกับภาษี/ใบแจ้งหนี้ | ครบ ม.86/4: คำว่า "ใบกำกับภาษี", ชื่อ/ที่อยู่/เลขผู้เสียภาษี/สาขา ผู้ขายและผู้ซื้อ, เลขที่, วันที่, รายการ จำนวน ราคา, VAT แยกบรรทัด, ยอดเป็นตัวอักษร |
| Payment Entry | ใบเสร็จรับเงิน | อ้างอิงเลขใบกำกับ |

- `bahttext(amount)` เขียนเอง: เช่น `1250.50` → "หนึ่งพันสองร้อยห้าสิบบาทห้าสิบสตางค์", `100` → "หนึ่งร้อยบาทถ้วน", รองรับ "เอ็ด"/"ยี่" และหลักล้านซ้อน
- Jinja filter `be_date(date, fmt)` → วันที่ พ.ศ. เช่น "7 ตุลาคม 2569"; Desk ยังแสดง ค.ศ. ตอนคีย์

### 5.4 รายงาน

Script Report "รายงานภาษีขาย": ช่วงเดือนภาษี, คอลัมน์ ลำดับ, วันที่ (พ.ศ.), เลขที่ใบกำกับ, ชื่อผู้ซื้อ, เลขผู้เสียภาษีผู้ซื้อ, สาขา, มูลค่าสินค้า/บริการ, ภาษีมูลค่าเพิ่ม; รวมยอดท้ายรายงาน; export Excel ได้; ใบที่ cancel แสดงเป็นยกเลิกยอดศูนย์

### 5.5 ภาษา UI

ตั้ง system language `th`; ไฟล์แปล `erp_town/translations/th.csv` สำหรับข้อความของ app

## 6. Infra (`infra/docker`)

### 6.1 Image

Custom image จาก frappe_docker (`images/custom`) พร้อม `apps.json`: erpnext (`version-16`), hrms (`version-16`), erp_town (จาก path ใน repo) tag `erp-town/erpnext:v16-<git sha>`

### 6.2 Services

| Service | หน้าที่ | เปิดออกนอก |
|---|---|---|
| `mariadb` 11.x | ฐานข้อมูล | ไม่ |
| `redis-cache`, `redis-queue` | ของ Frappe | ไม่ |
| `redis-town` | Redis Streams (AOF `everysec`) | ไม่ (dev เปิด `127.0.0.1:6380` เพื่อ debug) |
| `backend`, `websocket`, `frontend` (nginx) | Frappe/ERPNext | `127.0.0.1:8080` |
| `queue-short`, `queue-long`, `scheduler` | worker | ไม่ |
| `create-site` (one-shot) | สร้าง site `town.localhost`, install apps, migrate | — |

### 6.3 Profiles

- `dev`: bind-mount `frappe-apps/erp_town` เข้า `apps/erp_town` ของ bench; frappe/erpnext/hrms อยู่ใน named volume; auto-reload แบบ polling; `developer_mode=1`
- `test`: site แยก `test.localhost` ที่ล้างได้; ใช้กับ integration tests
- `pilot`: image ที่ build แล้ว (ไม่มี bind-mount), Caddy (TLS อัตโนมัติ), job backup รายวัน `bench backup --with-files` → S3-compatible ภายนอก; ต้องผ่านการซ้อม restore ก่อนเริ่ม pilot

### 6.4 Secrets

`.env` (ไม่ commit) และ `.env.example` (commit): รหัสผ่าน DB, admin password, `REDIS_TOWN_URL`, ปลายทาง backup; `.gitignore` เพิ่ม `.env`, `infra/docker/data/`

## 7. Seed data

คำสั่ง `pnpm erp:seed` → `bench --site town.localhost execute erp_town.setup.seed.run` (idempotent: ตรวจก่อนสร้างทุกรายการ)

- บริษัทสมมติ "บริษัท ตัวอย่างเทรดดิ้ง จำกัด" (`ACME-TH`), ผังบัญชีไทย, VAT templates, คลังหลัก, ปีบัญชี
- สินค้า 20 รายการพร้อม reorder level และสต็อกเริ่มต้น (Stock Reconciliation)
- ลูกค้า 10 ราย (เลขผู้เสียภาษีสมมติที่ผ่าน checksum), credit limit บางรายต่ำเพื่อให้เกิดการอนุมัติ
- ผู้ใช้: ฝ่ายขาย, คลัง, บัญชี, ผู้อนุมัติ; Workflow "อนุมัติ SO เกินวงเงิน" (Draft → รออนุมัติ → อนุมัติแล้ว/ปฏิเสธ)
- Agent API users: `agent.sales_assistant`, `agent.ar_collector` + role แคบที่สุด + `Town Agent Identity` (API key/secret พิมพ์ออกครั้งเดียวตอน seed และเก็บใน `.env` ของเครื่อง dev เท่านั้น)

คำสั่ง `pnpm erp:scenario -- --days N [--bulk 400]` → `erp_town.setup.scenario.run`: จำลอง N วันของ flow SO→DN→SI→PE แบบสุ่มจังหวะ (seed สุ่มคงที่ได้), มีใบค้างชำระ, สต็อกต่ำกว่าเกณฑ์, SO เกินวงเงิน และ `--bulk` สร้าง SI จำนวนมากในครั้งเดียว

## 8. Error handling

| สถานการณ์ | พฤติกรรม |
|---|---|
| mapper มี bug / event ไม่ผ่าน schema | เอกสาร submit ได้ปกติ; outbox `Invalid` + `system.error` |
| `redis-town` ล่ม | แถว `Pending` สะสม; retry แบบ backoff; ไม่มี event หาย |
| publish ซ้ำ (job กับ sweeper ชนกัน) | `FOR UPDATE SKIP LOCKED`; ถ้ายังซ้ำ ผู้รับข้ามตาม `id` |
| cancel / amend | `erp.<doctype>.cancelled` ของใบเดิม; ใบ amend เป็น subject ใหม่ ใช้ `correlation_id` เดิม |
| retry ครบ `max_attempts` | `Failed` + `system.error`; แสดงใน Town Status พร้อมปุ่มส่งใหม่ |
| ข้อมูลภาษีไม่ถูกต้อง | บล็อกการบันทึก ข้อความภาษาไทย (เป็นกติกา ERP ไม่ใช่ของชั้นเมือง) |
| agent ส่ง `on_behalf_of` เป็น user ที่ไม่มีอยู่ | ไม่บล็อกเอกสาร; ตั้ง `on_behalf_of = null` และออก `system.error` code `ON_BEHALF_OF_INVALID` |

## 9. Testing

เขียนแบบ TDD ทีละ task

1. **Unit (Python, `pnpm erp:test` → `bench run-tests --app erp_town`)**
   - mapper ทุกตัวสร้าง envelope ที่ผ่าน schema
   - seq ต่อ subject เริ่ม 1 เพิ่มทีละ 1
   - rollback เอกสาร → ไม่มีแถว outbox และ seq ไม่ขยับ
   - actor: user ปกติ, agent, agent + on_behalf_of, user ปกติส่ง header (ถูกปฏิเสธ), scheduler
   - causation/correlation ของ DN/SI/PE รวมกรณีต้นทางหลายใบและไม่มีต้นทาง
   - publisher: สำเร็จ, Redis error → backoff, ครบ max → Failed
   - `bahttext` (ตารางกรณีอย่างน้อย 20 ค่า รวม 0, 1, 11, 21, 101, 1,000,000, 21,000,021.21), checksum เลขผู้เสียภาษี, autoname พ.ศ. (ข้ามเดือน/ข้ามปี), `be_date`
   - รายงานภาษีขาย: ยอดตรงกับ SI ที่สร้างในเทสต์ รวมใบ cancel
2. **Contract**: fixtures ใน `packages/event-schema/fixtures` ผ่าน/ไม่ผ่านตรงกันทั้ง ajv และ jsonschema (Vitest + Python test อ่านไฟล์ชุดเดียว)
3. **Integration (compose profile `test`)**
   - REST API: SO→DN→SI→PE → อ่าน `town:events` ได้ event ตามลำดับ seq และสาย causation ถูก
   - หยุด `redis-town` → submit เอกสาร → เปิดกลับ → ภายใน 2 รอบ sweep event ครบ ไม่ซ้ำ
4. **Print formats (Playwright)**: render print preview/PDF ของใบกำกับภาษี ใบส่งของ ใบเสร็จ → screenshot baseline; checklist ให้นักบัญชีเซ็นรับรองแยกต่างหาก
5. **โหลด**: `erp:scenario --bulk 400` → วัดเวลาที่ outbox เป็น `Published` ครบ

## 10. Acceptance criteria

1. `pnpm erp:up` บน Windows (Docker Desktop) ได้ Desk ที่ `http://localhost:8080` มี ERPNext v16, HRMS, erp_town ติดตั้งแล้ว
2. `pnpm erp:seed` รันซ้ำได้ไม่เกิดข้อมูลซ้ำ
3. ทำ SO→DN→SI→PE ผ่าน Desk ได้ event ครบใน `town:events` ตามลำดับ; latency submit → stream p95 < 1 วินาทีเมื่อ Redis ปกติ
4. การกู้คืนหลัง Redis ล่ม: ไม่มี event หายหรือซ้ำในตาราง outbox
5. เอกสารพิมพ์ภาษาไทยแสดงถูก (สระ/วรรณยุกต์), วันที่ พ.ศ., ยอดเป็นตัวอักษร, เลขเอกสารรูปแบบ `IV6910-0001`
6. รายงานภาษีขายตรงกับใบกำกับใน seed/scenario
7. Bulk 400 ใบ: outbox ส่งครบภายใน 60 วินาทีบนเครื่อง dev
8. Contract fixtures ผ่านทั้ง Python และ TS
9. มี `AGENTS.md`, `CLAUDE.md`, `.mcp.json`, `.codex/config.toml`; `pnpm test` รันผ่านจาก root

## 11. สิ่งที่ต้องได้จากภายนอก

- นักบัญชีตรวจ: ผังบัญชี, VAT templates, print formats, รายงานภาษีขาย
- ข้อมูลจริงของบริษัท pilot (ผังบัญชีเดิม, prefix เลขเอกสารที่ใช้อยู่) — ถ้าได้มาจะปรับผ่าน fixture/settings ไม่ต้องแก้โค้ด
- ค่า tokens ธีม Desk และฟอนต์ UI จาก art bible (A)
