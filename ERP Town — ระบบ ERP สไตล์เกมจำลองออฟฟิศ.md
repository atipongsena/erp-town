# ERP Town — ระบบ ERP สไตล์เกมจำลองออฟฟิศ

Oct 7, 2026 · @Atipong Sena

## คอนเซ็ปต์หลัก

ERP Town คือ ERP จริงที่มีหน้าต่างเป็นเมืองจำลอง: ทุกแผนกคืออาคาร ทุกคนและทุก AI agent คือตัวละคร และทุก animation เกิดจาก event จริงในระบบ ไม่ใช่ของตกแต่ง

หลักการออกแบบ 5 ข้อที่ทำให้มันเป็น ERP ที่ใช้งานได้จริง ไม่ใช่แค่เดโมน่ารัก:

1. **ERP ก่อน เกมทีหลัง** — แกนกลางคือระบบบัญชี/คลัง/จัดซื้อที่ถูกต้องและตรวจสอบได้ โลกเกมเป็น "มุมมอง" (projection) ที่อ่าน event มาวาด ไม่เก็บข้อมูลธุรกิจเอง
2. **สองโหมดสลับได้ตลอด** — Town view ไว้ดูภาพรวม สั่งงาน อนุมัติ ส่วน Classic view (ฟอร์ม+ตาราง) ไว้คีย์งานเร็ว เพราะคนทำบัญชีไม่ควรต้องเดินตัวละครไปพิมพ์ใบแจ้งหนี้
3. **ตัวละครสองประเภท** — avatar ของพนักงานจริง (สะท้อนสิ่งที่เขาทำในระบบ) และ AI agent ที่ทำงานอัตโนมัติใต้สิทธิ์ที่กำหนด
4. **ทุกการเปลี่ยนแปลงผ่าน ERP API** — คลิกอนุมัติในเกม = เรียก API เดียวกับปุ่ม Approve ในฟอร์ม ผ่าน RBAC และ audit log เหมือนกัน
5. **ไม่เขียน ERP core เองตั้งแต่ศูนย์** — แนะนำใช้ ERPNext/Frappe เป็นแกน (โมดูลครบ ฟรีทั้งหมด) แล้วลงแรงที่ game layer + agent layer ซึ่งเป็นจุดต่างจริง

คุณค่าทางธุรกิจ: ผู้บริหารเห็นคอขวดทันทีจากภาพ (กองเอกสารรออนุมัติหน้าบ้านใคร) พนักงานเห็นว่า AI กำลังทำอะไรแทนเขา และ onboarding พนักงานใหม่ง่ายขึ้นเพราะเห็น flow งานเป็นภาพ

## กลุ่มเป้าหมายและตัววัดความสำเร็จ

สมมติฐานตั้งต้น (ต้อง validate ในขั้น PRD): เริ่มที่ SME ไทยขนาด 20–200 คน ที่ยังใช้ Excel คู่กับโปรแกรมบัญชีแยก และเจ้าของอยากเห็นว่างานติดตรงไหนโดยไม่ต้องอ่านรายงาน

| ผู้ใช้ | ปัญหาวันนี้ | สิ่งที่ ERP Town ให้ |
| --- | --- | --- |
| เจ้าของ / ผู้บริหาร | ไม่รู้ว่างานค้างที่แผนกไหน | ทั้งเมืองคือ dashboard ที่ดูออกในไม่กี่วินาที |
| หัวหน้าแผนก | ต้องไล่ตามเอกสารรออนุมัติ | กองเอกสารหน้าโต๊ะ + อนุมัติจาก bubble หรือ LINE |
| พนักงานปฏิบัติการ | คีย์ข้อมูลซ้ำ จับคู่เอกสารด้วยมือ | agent ร่างให้ คนตรวจแล้วกดยืนยัน |
| พนักงานใหม่ | ไม่เข้าใจ flow งานข้ามแผนก | Replay วันทำงานจริงแบบ time-lapse |

จุดต่าง: ERP ใหญ่ทำ agentic AI แล้วแต่ไม่ทำให้ "มองเห็น" ส่วน visualizer ฝั่ง dev ไม่ใช่ ERP; ERP Town อยู่ตรงกลางพอดี

ตัววัดของ MVP (ตั้งค่าเป้าใน PRD): จำนวนวันปิดงวดบัญชี, เอกสารค้างอนุมัติเกิน SLA, สัดส่วน draft ของ agent ที่ถูกยืนยันโดยไม่แก้, จำนวนผู้บริหารที่เปิด Town view ทุกสัปดาห์

คำถามเปิด: ขายแบบ SaaS ต่อผู้ใช้ หรือค่า implement + subscription รายเดือน

## เคสที่เป็นกระแส

กระแสนี้มาจากสองทางบรรจบกัน: ฝั่ง dev ทำ "agent visualizer" ที่เปลี่ยน Claude Code/AI agents เป็นตัวละครในออฟฟิศ และฝั่ง enterprise ที่ ERP ใหญ่ทุกเจ้าหันไปทำ agentic ERP ยังไม่มีใครเอาสองอย่างมารวมกันเป็น ERP เต็มระบบ นี่คือช่องว่าง

| เคส | คืออะไร | สิ่งที่เอามาใช้ |
| --- | --- | --- |
| คลิป CandesOyee (จาก reel ที่แนบมา) | โลก 3D ธีมขนมหวาน 16 agents ทำงานใน marketing agency มี level/XP, bubble บอกงานที่ทำ, ปุ่ม Freeze view / God view / Log / Dashboard, กลางวัน-กลางคืน และ dashboard ที่ระบุว่าดึงข้อมูลจาก hooks | ชุดปุ่มควบคุมมุมมอง, อาคารต่อแผนก, label อาชีพ+Lv, studio board แยกตามลูกค้า |
| [Pixel Agents](https://github.com/pablodelucca/pixel-agents) | VS Code extension: ทุก Claude Code terminal เกิดตัวละคร 1 ตัว อ่าน JSONL transcript, game loop บน canvas + BFS pathfinding + state machine (idle → walk → type/read), มี office layout editor และ CLI แบบ standalone | แพทเทิร์น state machine ตัวละคร และ layout editor ให้แต่ละองค์กรจัดเมืองเอง |
| [Claude Office Visualizer](https://github.com/paulrobello/claude-office) | Pixel art office: boss = main agent, employee = subagents, ตึกหลายชั้น, เครื่องพิมพ์ขยับเมื่อออกรายงาน สแต็ก Next.js + PixiJS + FastAPI + Zustand | โครง backend↔frontend ที่ใกล้เคียงที่สุด และไอเดีย "อุปกรณ์ขยับตาม event" |
| [agentviz](https://github.com/31780/agentviz) | relay เล็กๆ ที่ agent push event เข้า browser subscribe ออก + bridge จาก Claude Code/Codex hooks | แพทเทิร์น event relay ที่แยกตัวส่งกับตัววาด |
| [AI Town (a16z)](https://github.com/a16z-infra/ai-town) | เมืองที่ AI characters อยู่ คุย ใช้ชีวิต มี simulation engine, shared global state และ transactions บน Convex ต่อยอดจากงานวิจัย Generative Agents | แนวคิด world engine + memory/plan ของ agent |
| [WorkAdventure](https://github.com/workadventure/workadventure) | Virtual office แบบ RPG 16-bit สำหรับคนจริง เดินไปใกล้ใครก็คุยวิดีโอได้ open source | ฟีเจอร์ social ของคนจริง (เฟสหลัง) |
| [Agentropolis](https://github.com/Paracausal-Labs/Agentropolis) | เมือง isometric ที่ agents เสนอแผนใน Council Room ให้คนอนุมัติก่อนลงมือ ใช้ React Three Fiber + Drei | ไอเดีย "ห้องประชุมอนุมัติ" สำหรับ human-in-the-loop |
| [Agent Flow](https://getclawkit.com/skills/official-adisinghstudent-agent-flow-visualization) | แสดง tool call ของ agent เป็น node graph แบบ real-time | มุมมอง debug/trace สำหรับ admin |
| [SAP Sapphire 2026](https://www.techtarget.com/searcherp/news/366642871/SAP-unveils-agentic-AI-tools-to-partially-automate-ERP-suite) | SAP เปิด Joule assistants 50 ตัวประสานทีม agents 224 ตัว ใน finance, supply chain, procurement, HR; Oracle เปิด 22 ทีม agents | ยืนยันว่า agentic ERP เป็นทิศทางตลาด แต่ไม่มีเจ้าไหนทำให้ "มองเห็น" agents ทำงาน |

บทเรียนสำคัญ: เคสฝั่ง dev ทุกตัวใช้สถาปัตยกรรมเดียวกัน คือ event จากระบบจริง → relay → frontend ที่แปลงเป็น state ของตัวละคร ซึ่งต่อกับ ERP ได้ตรงๆ แค่เปลี่ยนแหล่ง event จาก Claude Code hooks เป็น document events ของ ERP

## แปลง ERP เป็นโลกเกม

กฎข้อเดียว: ทุกสิ่งในเมืองต้องชี้กลับไปที่เอกสารหรือตัวเลขจริงได้ คลิกแล้วเปิดเอกสารนั้นใน Classic view ได้ทันที

| ใน ERP | ในโลกเกม | ตัวอย่าง animation |
| --- | --- | --- |
| บริษัท (company) | เกาะ/เมือง 1 แห่ง; multi-company = หลายเกาะเชื่อมด้วยสะพาน | รถขนส่งข้ามสะพานเมื่อมี inter-company transfer |
| โมดูล/แผนก | อาคาร: บัญชี = ธนาคาร, คลัง = โกดัง, ขาย = ร้านค้า, จัดซื้อ = ท่าเรือ/ตลาด, ผลิต = โรงงาน, HR = ศาลากลาง, CRM = คาเฟ่ลูกค้า, ผู้บริหาร = ปราสาทกลางเมือง | ไฟในอาคารสว่างตามปริมาณงานค้าง |
| ผู้ใช้จริง | avatar ประจำตัว มีโต๊ะในอาคารแผนกตัวเอง | เปิดฟอร์ม PO → avatar เดินไปท่าเรือและนั่งพิมพ์ |
| AI agent | NPC มีอาชีพ + level + สีประจำบทบาท | พิมพ์ (เขียน), อ่าน (ค้นข้อมูล), คิด (เรียก LLM) |
| เอกสาร (SO, PO, Invoice, DN) | ไอเท็ม: ซองจดหมาย, กล่องพัสดุ, ถุงเงิน | ตัวละครถือเอกสารเดินจากร้านค้าไปโกดัง |
| Workflow / อนุมัติ | เอกสารไปต่อคิวหน้าโต๊ะผู้อนุมัติ | กองเอกสารสูงขึ้นเมื่อค้าง, ตราประทับเมื่ออนุมัติ |
| สต็อกสินค้า | กองกล่องในโกดัง | ชั้นวางเหลือน้อย + ไอคอนเตือนเมื่อต่ำกว่า reorder level |
| เงินสด / cash flow | ตู้เซฟในธนาคาร + ท้องฟ้าเมือง | เมฆครึ้มเมื่อคาดการณ์ cash flow ติดลบ (คู่กับ badge ตัวเลขเสมอ) |
| ข้อผิดพลาด / exception | ควัน + เครื่องหมาย ! เหนืออาคารหรือตัวละคร | agent หยุดนิ่ง ยกมือเรียกคน |
| กำหนดส่ง / SLA | นาฬิกาเหนือหัว | เปลี่ยนเป็นสีแดงเมื่อใกล้ครบกำหนด |
| ผลงาน agent | XP / level / badge | level-up เมื่องานผ่านการตรวจต่อเนื่อง |

ข้อควรระวัง: ใช้ XP/level กับ AI agents เท่านั้น ส่วนคนจริงไม่ควรมี leaderboard สาธารณะ เพราะกลายเป็นการจับผิดและกระทบบรรยากาศการทำงาน

ทางเลือกสำหรับคนจริง: เควสต์ระดับทีม เช่น "ปิดงวดก่อนวันที่ 5" สำเร็จแล้วทั้งเมืองมีพลุ ฉลองผลงานร่วมโดยไม่จัดอันดับบุคคล

&#91;embedded content: แผนที่เมือง · 2 เส้นทางเอกสารหลัก\]

ผังเมืองวางตาม flow เอกสาร ไม่ใช่ตามความสวย: อาคารที่ส่งงานต่อกันอยู่ติดกัน ตัวละครจึงเดินตามเส้นทางที่อ่านออกทันที ถนนไหนตัวละครออกัน = คอขวดของเส้นทางนั้น

## โมดูล ERP ครบชุด

11 โมดูลนี้ครอบคลุมงานหลักขององค์กร SME ถึง mid-market และมีอยู่แล้วใน ERPNext เกือบทั้งหมด งานของเราคือปรับ localization ไทยและต่อเข้ากับโลกเกม

| โมดูล | ฟังก์ชันหลัก | อาคารในเมือง |
| --- | --- | --- |
| บัญชีและการเงิน | GL, AP/AR, กระทบยอดธนาคาร, ภาษีมูลค่าเพิ่ม/หัก ณ ที่จ่าย (ภ.ง.ด.3/53), e-Tax Invoice, งบประมาณ, หลายสกุลเงิน, ปิดงวด | ธนาคาร |
| ขายและ CRM | Lead → ใบเสนอราคา → SO, ตารางราคา, คอมมิชชัน, POS, pipeline | ร้านค้า + คาเฟ่ลูกค้า |
| จัดซื้อ | PR → RFQ → PO → รับของ → 3-way match, ทะเบียน vendor | ท่าเรือ/ตลาด |
| คลังและคลังสินค้า | หลายคลัง, lot/serial, FIFO/ถัวเฉลี่ย, นับสต็อก, reorder level | โกดัง |
| การผลิต | BOM, work order, routing, MRP, QC | โรงงาน |
| HR และเงินเดือน | ทะเบียนพนักงาน, ลา/เข้างาน, payroll (ประกันสังคม, ภ.ง.ด.1), สรรหา, ประเมินผล | ศาลากลาง |
| โปรเจกต์ | งานย่อย, timesheet, ต้นทุนโปรเจกต์, billing | สตูดิโอ |
| สินทรัพย์และซ่อมบำรุง | ทะเบียนสินทรัพย์, ค่าเสื่อม, แผนซ่อม | อู่ซ่อม |
| Helpdesk | ticket, SLA, knowledge base | ศูนย์บริการ |
| รายงานและ BI | dashboard, งบการเงิน, KPI | หอสังเกตการณ์ |
| Platform | RBAC, workflow engine, audit log, เก็บไฟล์, แจ้งเตือน, multi-company, API/webhooks, ไทย/อังกฤษ, แสดง พ.ศ. | ปราสาทกลางเมือง |

เรื่องภาษีและกฎหมายแรงงานไทยควรให้นักบัญชี/ผู้เชี่ยวชาญตรวจ rule ก่อนใช้จริง เพราะอัตราและแบบฟอร์มเปลี่ยนได้

ส่วนเสริมสำหรับตลาดไทย: QR PromptPay บนใบแจ้งหนี้และกระทบยอดอัตโนมัติ, แจ้งเตือนและอนุมัติผ่าน LINE OA, ปฏิทิน พ.ศ. และวันหยุดราชการในโมดูล HR, แบบฟอร์มเอกสารภาษาไทยที่พิมพ์ได้ตามรูปแบบที่สรรพากรยอมรับ

## ตัวละคร: คนจริงและ AI agents

Agent ทุกตัวมีระดับความอิสระ (autonomy) ต่อ action ที่ชัดเจน เริ่มที่ "ร่างเท่านั้น" แล้วค่อยเลื่อนขึ้นเมื่อพิสูจน์ความแม่นยำแล้ว แนวเดียวกับที่ตลาดพบว่า deployment ที่ได้ผลดีที่สุดคืองานที่ scope แคบและชัด ([source](https://www.commercepundit.com/blog/ai-agents-in-erp-2026/))

**ระดับความอิสระ**

- **L0 อ่าน** — ดูข้อมูล ตอบคำถาม ทำรายงาน
- **L1 ร่าง** — สร้างเอกสารสถานะ Draft ให้คนกด Submit
- **L2 ทำเองภายในวงเงิน/เงื่อนไข** — เช่น ส่งอีเมลทวงหนี้ตาม template, ออก PO ซ้ำกับ vendor เดิมไม่เกินวงเงินที่ตั้ง
- **L3 ทำเองแล้วแจ้ง** — เฉพาะงานที่ย้อนกลับได้และความเสี่ยงต่ำ

**ทีม AI agents ตั้งต้น**

| Agent | อาคาร | งาน | ระดับเริ่มต้น |
| --- | --- | --- | --- |
| นายกเทศมนตรี (Orchestrator) | ปราสาท | รับคำสั่งภาษาคน แตกงาน ส่งต่อ agent อื่น | L0 |
| พนักงานบัญชี | ธนาคาร | จับคู่ statement กับรายการ, เสนอ journal entry | L1 |
| เจ้าหน้าที่ AP | ธนาคาร | อ่านใบแจ้งหนี้ (OCR), 3-way match | L1 |
| นักทวงหนี้ AR | ธนาคาร | แจ้งเตือนค้างชำระตาม template | L2 |
| ฝ่ายจัดซื้อ | ท่าเรือ | เสนอสั่งซื้อเมื่อต่ำกว่า reorder, ร่าง PO, เทียบราคา vendor | L1 |
| ผู้ดูแลคลัง | โกดัง | จับสต็อกผิดปกติ, วางแผนนับสต็อก | L0 |
| ผู้ช่วยขาย | ร้านค้า | ร่างใบเสนอราคา, ให้คะแนน lead | L1 |
| นักวางแผนผลิต | โรงงาน | รัน MRP, เสนอลำดับ work order | L1 |
| ผู้ช่วย HR | ศาลากลาง | ตอบคำถามนโยบาย, checklist onboarding | L0 |
| ผู้ตรวจสอบ | หอสังเกตการณ์ | ตรวจรายการผิดปกติ, ตรวจ segregation of duties | L0 |
| นักวิเคราะห์ | หอสังเกตการณ์ | สร้างรายงาน (เครื่องพิมพ์ขยับเมื่อออกรายงาน) | L0 |

**State ของตัวละคร**

&#91;embedded content: State machine ตัวละคร · 7 state\]

ทุกเส้นคือ event จริงจากหัวข้อ Event model; `break` เกิดได้เฉพาะตอนว่างจริง (แบบ "boba break" ในคลิป) และใช้แสดง agent ที่ใช้ budget รายวันหมดด้วย

**Avatar คนจริง**: สะท้อนกิจกรรมในระบบ (เปิดเอกสารไหนอยู่ มีงานรออนุมัติกี่ใบ) ไม่ติดตามนอกระบบ ให้ผู้ใช้เลือกซ่อนสถานะได้ (สอดคล้อง PDPA)

## สถาปัตยกรรมระบบ

&#91;embedded content: สถาปัตยกรรม ERP Town · 5 ชั้น\]

ทุกการเปลี่ยนแปลงจากคน เกม หรือ agent วิ่งผ่าน Gateway เดียวกัน ส่วนเมืองรับเฉพาะ event ที่ออกมาจาก bus ผ่าน World State Service จึงเปลี่ยนหน้าตาเกมได้อิสระโดยไม่กระทบข้อมูลธุรกิจ

## Event model และ data model

หัวใจของระบบคือ event กลางที่ทั้ง ERP และ agent runtime ส่งออกมาในรูปแบบเดียวกัน (อิง CloudEvents) แล้ว World State Service แปลงเป็นท่าทางตัวละคร ใช้ outbox pattern เพื่อให้ event ไม่หายแม้ bus ล่ม

```json
{
  "id": "evt_01J...",
  "type": "erp.purchase_order.submitted",
  "source": "erp/buying",
  "time": "2026-10-07T10:15:00+07:00",
  "company": "ACME-TH",
  "actor": { "kind": "agent", "id": "agent.buyer", "on_behalf_of": "user.somchai" },
  "subject": { "doctype": "Purchase Order", "name": "PO-2026-00123" },
  "correlation_id": "task_8841",
  "data": { "supplier": "SUP-001", "status": "To Receive", "visibility": "public" }
}
```

**Event → ท่าทางในเมือง** (เก็บเป็น rule table แก้ได้โดยไม่ต้อง deploy)

| Event | ท่าทางในเมือง |
| --- | --- |
| `erp.sales_order.submitted` | ตัวละครหยิบกล่องที่ร้านค้า เดินไปโกดัง |
| `erp.delivery_note.submitted` | รถขนส่งออกจากโกดังไปทางออกเมือง |
| `erp.sales_invoice.overdue` | ซองแดงที่ธนาคาร นักทวงหนี้เดินออกไป |
| `erp.payment_entry.submitted` | ถุงเงินเข้าตู้เซฟ + เสียงเหรียญ |
| `workflow.approval.requested` | bubble "รออนุมัติ" เหนือโต๊ะผู้อนุมัติ กองเอกสารสูงขึ้น |
| `agent.task.started` / `agent.tool.called` | agent เดินไปอาคารเป้าหมาย นั่งพิมพ์/อ่าน |
| `agent.task.needs_human` | agent ยกมือ + แจ้งเตือนเจ้าของงาน |
| `system.error` | ควัน + เครื่องหมาย ! ที่อาคารที่เกี่ยวข้อง |

**ตารางเพิ่มจาก ERP ปกติ**

- `agent` — อาชีพ, อาคารประจำ, model, tools ที่ใช้ได้, autonomy ต่อ action, วงเงินสูงสุด, XP/level
- `agent_run` / `agent_action` — ทุก prompt, tool call, ผลลัพธ์, token/cost (audit เต็มรูปแบบ)
- `approval_request` — คิวงานรอคนอนุมัติ ผูกกับเอกสารต้นทาง
- `event_outbox` — event ที่เขียนใน transaction เดียวกับเอกสาร รอส่งเข้า bus
- `world_layout` / `world_snapshot` — ผังเมืองที่แต่ละองค์กรจัดเอง และ snapshot สำหรับ replay/time-lapse

World state ฝั่ง client มี 3 entity: `Character {id, kind, role, level, location, state, task}`, `Building {id, module, queue, alerts}`, `Item {doc_ref, holder, position}` ส่งเป็น diff ผ่าน WebSocket

**ดึง event ออกจาก ERPNext**

1. สร้าง custom Frappe app (`erp_town`) ลงทะเบียน `doc_events` ใน `hooks.py` (on\_submit, on\_cancel, on\_update\_after\_submit, on\_update ของ workflow)
2. handler เขียนแถวลง `event_outbox` ใน transaction เดียวกับเอกสาร ถ้าเอกสาร rollback event ก็หายไปด้วย ไม่มีตัวละครวิ่งหลอก
3. background worker อ่าน outbox ส่งเข้า Redis Streams แล้ว mark ว่าส่งแล้ว
4. Webhook ในตัวของ Frappe ใช้กับระบบภายนอกได้ แต่ทางหลักของเมืองใช้ outbox เพราะคุมความถูกต้องเองได้ทั้งหมด

**กติกาความน่าเชื่อถือ**

- `id` คือ idempotency key ผู้รับทุกตัวต้องข้าม event ซ้ำ
- รับประกันลำดับต่อ `subject` (partition ตามเลขเอกสาร) ไม่ใช่ทั้งระบบ
- ทุก event มีเลข version ของ schema เปลี่ยน schema แบบเพิ่ม field เท่านั้น
- event ถี่เกินจะ animate ทัน (เช่น import 400 ใบ) ให้ World State รวมเป็นท่าทางเดียว "รับเอกสาร 400 ใบ"
- เก็บ event ย้อนหลังตามนโยบาย retention เพื่อใช้ Replay และ audit

## Game layer และ animation

แนะนำเริ่มที่ isometric 3D low-poly สีพาสเทลแบบคลิป ด้วย React Three Fiber + asset CC0 เพราะสวยพอขายได้แต่ไม่ต้องมีทีมอาร์ต ถ้าเครื่องลูกค้าสเปกต่ำ ให้มีโหมด 2D pixel (PixiJS) เป็น fallback

**มุมมอง (views)**

- **Town** — ภาพรวมเมือง หมุน/ซูมได้ คลิกอาคารเพื่อเปิด dashboard ของโมดูล
- **Building interior** — ซูมเข้าไปเห็นโต๊ะทำงาน คิวเอกสาร และใครทำอะไรอยู่
- **God view** — ตารางสถานะ agent ทุกตัว (working / waiting / error) แบบในคลิป
- **Freeze** — หยุด animation เพื่อดูรายละเอียด (แยกจาก "Pause all agents" ซึ่งเป็น kill switch จริง)
- **Overlay toggles** — Names, Bubbles, Log (event feed), Dashboard (KPI), กลางวัน/กลางคืนตามเวลาทำการจริง
- **Replay** — เล่นย้อนวันทำงานแบบ time-lapse จาก event log ใช้สอนงานและหาคอขวด

**การโต้ตอบที่ทำงานจริง**

- คลิกตัวละคร → แผงงานปัจจุบัน, ประวัติ, ลิงก์ไปเอกสารจริง
- ลากเอกสารไปวางที่ agent → มอบงาน (เช่น ลากใบแจ้งหนี้ PDF ไปให้ AP agent)
- อนุมัติ/ปฏิเสธจาก bubble ได้เลย พร้อมดู diff ก่อนกด
- สั่งงานภาษาคนที่ปราสาท → Orchestrator แตกงาน เห็น agents เดินแยกกันไปทำ

**เทคนิค animation**

- State machine ต่อตัวละคร (XState หรือ FSM เขียนเอง) ควบคุม animation clip; แนวเดียวกับ Pixel Agents
- Pathfinding บน grid ด้วย A\*/BFS เดินตามถนนระหว่างอาคาร
- ตัวละคร rigged GLTF ที่มีชุด animation สำเร็จ (idle, walk, typing, carry) จาก KayKit/Quaternius
- Label และ bubble ด้วย drei `<Html>`, ต้นไม้/ของตกแต่งใช้ instancing
- Server ส่ง state diff ไม่เกิน \~10 ครั้ง/วินาที client ทำ interpolation เอง; เป้า 60fps บนโน้ตบุ๊กทั่วไป (ตัวเลขตั้งต้น ต้องวัดจริง)
- Accessibility: ปุ่ม reduce motion, ไม่ใช้สีเป็นตัวบอกความหมายอย่างเดียว

## Tech stack และ repo อ้างอิง

ทางเลือกแกน ERP คือการตัดสินใจใหญ่ที่สุด: ERPNext เป็น GPLv3 เต็มตัวไม่มีการแยก community/enterprise ส่วน Odoo Community ตัดฟีเจอร์บางส่วนไปอยู่ใน Enterprise ([source](https://ecosire.com/blog/odoo-vs-erpnext-deep-comparison-2026)) จึงแนะนำ ERPNext สำหรับโปรเจกต์นี้

| ชั้น | ทางเลือกหลัก | ทางเลือกสำรอง |
| --- | --- | --- |
| ERP core | Frappe Framework + ERPNext + HRMS (Python, MariaDB) | เขียนเอง: NestJS/FastAPI + PostgreSQL (modular monolith) |
| Event bus | Redis Streams | NATS JetStream หรือ Kafka เมื่อสเกลใหญ่ |
| World State Service | Node.js/TypeScript + WebSocket | Python FastAPI |
| Frontend 3D | Next.js + React Three Fiber + drei + Zustand | PixiJS (2D pixel) |
| Classic view | Frappe Desk UI เดิม หรือ React + shadcn/ui | — |
| Agent runtime | Claude (API / Agent SDK) + MCP server ที่ห่อ ERP API เป็น tools | LangGraph / เขียน orchestrator เอง |
| Analytics | ClickHouse หรือ DuckDB สำหรับ dashboard/replay | Frappe Insights |
| Auth | Keycloak (OIDC) + RBAC ของ Frappe | Frappe login เดิม |
| Deploy | Docker Compose → Kubernetes | Frappe Cloud สำหรับแกน ERP |

**Repo ที่ควรศึกษา**

| Repo | ดูอะไร |
| --- | --- |
| [pablodelucca/pixel-agents](https://github.com/pablodelucca/pixel-agents) | state machine ตัวละคร, BFS pathfinding, layout editor, adapter แยก host |
| [paulrobello/claude-office](https://github.com/paulrobello/claude-office) | backend FastAPI ↔ frontend PixiJS/Zustand, ตึกหลายชั้น, skill สร้าง sprite ด้วย AI |
| [31780/agentviz](https://github.com/31780/agentviz) | relay + emitter แบบ zero-dependency |
| [a16z-infra/ai-town](https://github.com/a16z-infra/ai-town) | simulation engine, shared state, agent memory |
| [workadventure/workadventure](https://github.com/workadventure/workadventure) | virtual office หลายผู้เล่น, map editor |
| [Paracausal-Labs/Agentropolis](https://github.com/Paracausal-Labs/Agentropolis) | เมือง R3F + ห้องอนุมัติแผน agent |
| [AmanM006/revenant](https://github.com/AmanM006/revenant) | โลก isometric R3F + NPC agents + knowledge graph |
| [frappe/erpnext](https://github.com/frappe/erpnext) · [frappe/frappe](https://github.com/frappe/frappe) · [frappe/hrms](https://github.com/frappe/hrms) | แกน ERP, DocType, hooks `doc_events`, realtime |
| [pmndrs/react-three-fiber](https://github.com/pmndrs/react-three-fiber) · [pmndrs/drei](https://github.com/pmndrs/drei) | เครื่องมือ 3D บน React |
| [KayKit City Builder Bits](https://github.com/KayKit-Game-Assets/KayKit-City-Builder-Bits-1.0) | asset เมือง low-poly CC0 ใช้เชิงพาณิชย์ได้ |

**MCP server สำเร็จรูปของ ERPNext** (ไม่ต้องเขียน tool layer เองตั้งแต่ศูนย์)

| Repo | จุดเด่น |
| --- | --- |
| [Casys-AI/mcp-erpnext](https://jsr.io/@casys/mcp-erpnext) | 120 tools ใน 14 หมวด (ขาย คลัง จัดซื้อ บัญชี HR ผลิต ฯลฯ), เลือกโหลดเฉพาะหมวดได้, stdio และ HTTP, ใช้ได้กับ Frappe Cloud |
| [rakeshgangwar/erpnext-mcp-server](https://github.com/rakeshgangwar/erpnext-mcp-server) | TypeScript ขนาดเล็ก อ่านง่าย: CRUD เอกสาร, `call_method`, `run_report` |
| [vyogotech/frappe-mcp-server](https://github.com/vyogotech/frappe-mcp-server) | รองรับ LLM ในเครื่องผ่าน Ollama สำหรับลูกค้าที่ไม่อยากส่งข้อมูลออกนอก |

ข้อควรระวัง: MCP server เหล่านี้ทำงานด้วยสิทธิ์ของ API key ทั้งก้อน ต้องหุ้มด้วย policy layer ของเรา (autonomy ต่อ action, วงเงิน, segregation of duties) ก่อนให้ agent ใช้ และให้แต่ละ agent มี API user ของตัวเองเพื่อให้ audit log แยกได้

Asset เพิ่มเติม: [Kenney](https://kenney.nl), Quaternius, KayKit เป็น CC0 ทั้งหมด ไม่ต้องใส่ attribution ([source](https://app.cinevva.com/guides/free-3d-model-sites))

## Security, governance, performance

เกมเป็นแค่หน้าต่าง: สิทธิ์ทุกอย่างบังคับที่ ERP API ไม่ใช่ที่ฝั่ง client

- **Agent guardrails** — autonomy ต่อ action, วงเงินสูงสุด, segregation of duties (agent ที่สร้าง PO อนุมัติ PO ตัวเองไม่ได้), human-in-the-loop สำหรับทุกอย่างที่ลงบัญชี
- **Kill switch** — ปุ่ม Pause all agents ที่ปราสาท หยุด agent ทุกตัวทันทีและแสดงในเมืองว่าทุกตัวหยุดนิ่ง
- **Audit** — เก็บ prompt, tool call, ผลลัพธ์, ผู้สั่ง (`on_behalf_of`) ทุกครั้ง ย้อนดูได้ใน Replay
- **Prompt injection** — ใบแจ้งหนี้ อีเมล ไฟล์จาก vendor เป็นข้อมูลไม่น่าเชื่อถือ agent อ่านเป็น data เท่านั้น ห้ามทำตามคำสั่งในเอกสาร
- **การมองเห็นข้อมูล** — event มี `visibility`; bubble และป้ายในเมืองแสดงเฉพาะข้อมูลที่ผู้ชมมีสิทธิ์ เงินเดือนและข้อมูลส่วนบุคคลไม่ขึ้นในเมืองเลย
- **PDPA** — การแสดงกิจกรรมพนักงานต้องแจ้งและให้เลือกปิดได้ ไม่ใช้เป็นเครื่องมือจับเวลาคน
- **Performance** — World State Service รวม event เป็น diff, จำกัดตัวละครที่ animate พร้อมกัน, LOD ไกลแสดงเป็นจุด, snapshot ทุก N นาทีเพื่อให้ client ใหม่โหลดเร็ว

## คุณภาพ agent และต้นทุน

Agent เลื่อนระดับ L1 → L2 ด้วยตัวเลขที่วัดได้ ไม่ใช่ความรู้สึก และลดระดับอัตโนมัติเมื่อพลาดบ่อยขึ้น

1. **Eval set จากงานจริง** — เอกสารที่คนบันทึกเสร็จแล้วย้อนหลัง (เช่น ใบแจ้งหนี้ที่บัญชีลงแล้ว) ใช้เป็นเฉลย วัดว่าร่างของ agent ตรงกับที่คนทำไหม รันทุกครั้งที่เปลี่ยน prompt หรือ model
2. **Shadow mode** — agent ทำคู่ขนานโดยไม่บันทึกจริง เทียบผลกับคนช่วงหนึ่งก่อนเปิดใช้
3. **เกณฑ์เลื่อนระดับ** — สัดส่วน draft ที่ถูกยืนยันโดยไม่แก้ และไม่มีความผิดพลาดร้ายแรงในช่วงเวลาที่กำหนด (ตั้งตัวเลขใน PRD ต่อ agent)
4. **ลดระดับอัตโนมัติ** — error rate เกินเกณฑ์ → กลับเป็น L1 และตัวละครขึ้นไอคอนเตือนในเมือง

**Observability**: trace ต่อ `agent_run` (OpenTelemetry) ผูก `correlation_id` เดียวกับ event ในเมือง คลิกตัวละครแล้วเห็น trace เดียวกับที่ admin เห็น

**ต้นทุน**: เราคุม agent runtime เองจึงเก็บ token และค่าใช้จ่ายต่อ run ได้ครบ (คลิปต้นแบบทำไม่ได้เพราะ hooks ไม่ส่งข้อมูลนี้) ตั้ง budget รายวันต่อ agent; หมด budget → ตัวละครเข้า `break` ให้เห็นในเมือง งานที่เป็นกฎตายตัว (reorder level, จับคู่ยอดตรงกัน) ใช้โค้ดธรรมดา ไม่เรียก LLM

## Roadmap

&#91;embedded content: Roadmap · 4 เฟส 3 gate\]

ระยะเวลาเป็นประมาณการสำหรับทีม 2–3 คน การผ่าน gate สำคัญกว่าวันที่: G2 คือบริษัท pilot ปิดงวดบัญชีจริงในระบบได้ ก่อนจะให้ agent ทำเองมากขึ้น

ขั้นต่อไป: นำเอกสารนี้เข้า BMAD เพื่อทำ PRD และ architecture ฉบับเต็ม แล้วส่งต่อ Superpowers ทำแผน implement เฟส 1

## ความเสี่ยง

| ความเสี่ยง | ทางป้องกัน |
| --- | --- |
| เกมกลายเป็นลูกเล่นที่ทำให้คีย์งานช้าลง | Classic view เป็นพลเมืองชั้นหนึ่ง ทุกงานทำได้โดยไม่ต้องเข้าเมือง |
| ขอบเขต ERP ใหญ่เกินทีม | ใช้ ERPNext เป็นแกน ลงแรงที่ game + agent layer |
| สัญญาอนุญาต GPLv3 ของ ERPNext | แยก game/agent layer เป็นบริการที่คุยผ่าน API; ให้นักกฎหมายตรวจก่อนขายเชิงพาณิชย์ |
| Agent ลงบัญชีผิด | L1 (draft) สำหรับทุกอย่างที่กระทบงบ, ตรวจ 3-way match, audit |
| ค่า LLM บานปลาย | งาน rule-based ไม่เรียก LLM, ใช้ model เล็กกับงานจำแนก, ตั้ง budget ต่อ agent |
| เครื่องลูกค้าแรงไม่พอ | โหมด 2D และ reduce motion |
| พนักงานรู้สึกถูกจับผิด | ไม่มี leaderboard คน, opt-in, สื่อสารชัดว่าเมืองแสดงงาน ไม่ได้วัดคน |

## แหล่งอ้างอิง

- [Pixel Agents — GitHub](https://github.com/pablodelucca/pixel-agents)
- [Claude Office Visualizer — README](https://cdn.jsdelivr.net/gh/paulrobello/claude-office@main/README.md)
- [Claude Office Visualizer — EveryDev](https://www.everydev.ai/tools/claude-office-visualizer)
- [agentviz — GitHub](https://github.com/31780/agentviz)
- [AI Town — GitHub](https://github.com/a16z-infra/ai-town)
- [WorkAdventure — GitHub](https://github.com/workadventure/workadventure)
- [Agentropolis — GitHub](https://github.com/Paracausal-Labs/Agentropolis)
- [Revenant — GitHub](https://github.com/AmanM006/revenant)
- [Agent Flow visualization](https://getclawkit.com/skills/official-adisinghstudent-agent-flow-visualization)
- [Odoo vs ERPNext 2026 — ECOSIRE](https://ecosire.com/blog/odoo-vs-erpnext-deep-comparison-2026)
- [SAP unveils agentic AI tools — TechTarget](https://www.techtarget.com/searcherp/news/366642871/SAP-unveils-agentic-AI-tools-to-partially-automate-ERP-suite)
- [AI Agents in ERP 2026 — CommercePundit](https://www.commercepundit.com/blog/ai-agents-in-erp-2026/)
- [KayKit City Builder Bits — itch.io](https://kaylousberg.itch.io/city-builder-bits)
- [Best Free 3D Model Sites 2026 — Cinevva](https://app.cinevva.com/guides/free-3d-model-sites)

* [@casys/mcp-erpnext — JSR](https://jsr.io/@casys/mcp-erpnext)
* [MCP Server for ERPNext — Frappe Forum](https://discuss.frappe.io/t/mcp-server-for-erpnext-120-tools-to-connect-ai-agents-to-your-erp/161198)
* [rakeshgangwar/erpnext-mcp-server — GitHub](https://github.com/rakeshgangwar/erpnext-mcp-server)
* [vyogotech/frappe-mcp-server — GitHub](https://github.com/vyogotech/frappe-mcp-server)
