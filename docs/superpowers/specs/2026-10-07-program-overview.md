# ERP Town — ภาพรวมโปรแกรมเฟส 1 (Pilot)

วันที่: 2026-10-07 · สถานะ: อนุมัติแล้วระหว่าง brainstorming · เอกสารต้นทาง: `ERP Town — ระบบ ERP สไตล์เกมจำลองออฟฟิศ.md`

เอกสารนี้รวบรวมการตัดสินใจระดับโปรแกรมที่ทุกโปรเจกต์ย่อยต้องยึด แต่ละโปรเจกต์ย่อยมี spec → plan → implement ของตัวเอง

## เป้าหมายเฟส 1

- **Pilot กับบริษัทจริง** บน ERPNext และข้อมูลจริง (ไม่ใช่เดโม mock)
- บริษัท pilot: **ซื้อมาขายไป มีสต็อก** (VAT 7%, มีใบส่งของ)
- Vertical slice แรก: **ขาย → ส่งของ → วางบิล → รับเงิน** (SO → DN → SI → PE) ผ่านทุกชั้นจริง
- ทีมสมมติ 2–3 คน, งานพัฒนาสลับระหว่าง Claude Code และ Codex ได้

## หลักการที่ทุกโปรเจกต์ย่อยต้องยึด

1. **ERP ก่อน เกมทีหลัง** — เมืองเป็น projection ของ event ห้ามเก็บข้อมูลธุรกิจ และห้ามทำให้ธุรกรรม ERP ล้มเหลว
2. **ความสวยงามเป็นอันดับหนึ่ง** (ผู้ใช้กำหนด) — ภาพต้องดูเหมือนมี artist ออกแบบ ไม่ใช่ AI slop:
   - asset ตระกูลเดียว (KayKit City Builder Bits + KayKit characters/animations, CC0) retint ด้วย palette atlas เดียว ไม่ผสมค่ายอื่นถ้าไม่ retopo/รีเพนต์
   - อาคารประจำแผนกปั้นเองใน Blender ตาม grammar เดียวกับ kit แต่ละหลังมี silhouette จำง่าย + prop ที่ขยับตามข้อมูลจริง
   - ตัวละคร rig เดียว แยกบทบาทด้วยหมวก/อุปกรณ์/สี; agent ต่างจากคนจริงด้วยตาเปล่า
   - กล้อง orthographic isometric, bake lightmap/AO ใน Blender Cycles, AgX tone mapping, N8AO, SMAA, bloom เฉพาะไฟกลางคืน, LUT grading เดียว
   - motion design มี anticipation/easing; มี reduce motion
   - กระบวนการ: mood board → art bible → style frames → greybox → รีวิว screenshot ทุก gate ก่อนเขียนโค้ดฉาก
3. **ทุกการเปลี่ยนแปลงผ่าน ERP API เดียวกัน** — RBAC + audit เหมือนกันทั้งคน เกม agent
4. **XP/level ใช้กับ AI agents เท่านั้น** ไม่มี leaderboard คน; avatar คนจริงเป็น opt-in (PDPA)

## โปรเจกต์ย่อยและลำดับ

| # | โปรเจกต์ย่อย | ขึ้นกับ |
|---|---|---|
| A | Art bible + style frames + greybox (ทำคู่ขนาน B) | — |
| B | Foundation: Docker Compose, Frappe app `erp_town` (outbox, Thai localization), event schema, monorepo, เครื่องมือ AI | — |
| C | World State Service (Colyseus) | B |
| D | Town client 3D | A, C |
| E | Agent runtime หลาย provider (Claude API / OpenAI Responses / Codex SDK) + policy proxy | B, C |
| F | PromptPay QR + LINE OA | B |
| G | Avatar คนจริง (opt-in) + Replay | C, D |
| H | Dev-agent bridge (Claude Code/Codex hooks → อาคาร IT, ไม่มีสิทธิ์ ERP) | C |

```
A ─────────────────────────────┐
B → C → D → G                  │ (D ต้องผ่าน gate ของ A ก่อนเขียนฉาก)
    ├→ E
    ├→ H
    └→ F
```

## Tech stack ที่ตกลงแล้ว

| ชั้น | เลือก | หมายเหตุ |
|---|---|---|
| ERP core | **ERPNext v16** + HRMS (Frappe v16), MariaDB 11 | Classic view = Frappe Desk + ธีม CSS ของเรา |
| Event bus | Redis Streams บน `redis-town` แยกจาก Redis ของ Frappe | |
| World State | Node 24 + **Colyseus** (binary delta, StateView กรองตาม visibility) | |
| API/BFF | **Hono บน Node** | runtime เดียวกับ Colyseus; ไม่ใช้ Elysia เพราะผูก Bun |
| Client | **Vite + React 19 + R3F v9 + drei + koota (ECS) + Zustand (UI) + Web Worker** | WebGL2; ยังไม่ใช้ R3F v10/WebGPU (alpha) |
| Assets | KayKit + Blender (ผ่าน Blender MCP) → gltf-transform (meshopt + KTX2) | |
| Agents | TS service, adapters: Claude API tool runner, OpenAI Responses, Codex SDK | ทุก adapter เข้าถึง ERP ผ่าน MCP tools + policy proxy ตัวเดียว |
| Analytics | DuckDB (ใช้กับ Replay/BI) | |
| Auth | Frappe session → JWT อายุสั้นสำหรับ WS/API | ไม่ใช้ Keycloak ในเฟส 1 |
| Monorepo | pnpm workspaces, Biome, Vitest, Playwright | |
| Deploy | Docker Compose (profile `dev` / `test` / `pilot`) | ไม่ใช้ K8s ในเฟส 1 |

**ตัดออกจากเฟส 1:** Next.js, PixiJS 2D fallback, ClickHouse, Keycloak, Kubernetes

**ทางเลือกที่บันทึกไว้:** ถ้า localization ภาษีไทยกลายเป็นคอขวด ให้ประเมิน Odoo Community + OCA `l10n-thailand` (LGPL) — event model ออกแบบให้ไม่ผูกกับ ERP เปลี่ยนแค่ชั้น mapper

## สภาพแวดล้อมพัฒนา

- repo อยู่ที่ `D:\ERP_Town` (Windows) เพื่อให้ Claude desktop, Codex, Blender MCP, browser ทำงานบนเครื่องเดียวกันโดยตรง
- Frappe/ERPNext/HRMS อยู่ใน image/named volume; bind-mount เฉพาะ `frappe-apps/erp_town` (reload แบบ polling)
- `AGENTS.md` เป็นแหล่งคำสั่งหลัก; `CLAUDE.md` import `@AGENTS.md`
- MCP: `.mcp.json` (Claude) และ `.codex/config.toml` (Codex) — Blender MCP (`mcp-for-blender`, telemetry ปิด)
- คำสั่งร่วม: `pnpm dev`, `pnpm test`, `pnpm erp:up`, `pnpm erp:seed`, `pnpm erp:test`
- plan ใช้ checkbox + commit ทุก task เพื่อให้สลับ agent ทำต่อได้

## ความเสี่ยงระดับโปรแกรม

- G2 (ปิดงวดบัญชีจริง) ต้องมี flow จัดซื้อ + ภาษีซื้อ + ภ.พ.30 ด้วย ซึ่งอยู่นอก slice แรก
- กฎภาษี/แบบฟอร์มต้องให้นักบัญชีตรวจก่อนใช้จริง
- GPLv3: โค้ดใน `frappe-apps/erp_town` เป็น GPLv3; บริการ C/D/E/H แยก process คุยผ่าน API — ให้นักกฎหมายตรวจก่อนขายเชิงพาณิชย์
