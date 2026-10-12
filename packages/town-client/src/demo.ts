import type { Presence } from './avatar.ts'
import type { Language } from './copy.ts'
import type { Department, DisplayFact } from './world-view.ts'

export const demoDepartments: Department[] = ['shop', 'warehouse', 'bank', 'hq', 'it', 'post']
export type DemoRole = 'observer' | 'manager' | 'sales' | 'warehouse' | 'accountant' | 'ai'
export type DemoAction = 'draft' | 'approve' | 'payment'
type Text = Record<Language, string>
const text = (th: string, en: string): Text => ({ th, en })
export const roleNames: Record<DemoRole, Text> = {
  observer: text('ผู้ชม · ดูได้ทุกส่วน', 'Observer · all departments'),
  manager: text('ผู้จัดการ', 'Manager'),
  sales: text('ฝ่ายขาย', 'Sales'),
  warehouse: text('คลังสินค้า', 'Warehouse'),
  accountant: text('ฝ่ายบัญชี', 'Accounts'),
  ai: text('AI ผู้ช่วย', 'AI assistant'),
}
type Permission = {
  view: Department[]
  amounts: boolean
  draft: boolean
  approve: boolean
  payment: boolean
  drafts: Department[]
  approvals: Department[]
}
// Illustrative policy only. Never consumed by the live ERP or authentication.
export const permissions: Record<DemoRole, Permission> = {
  observer: {
    view: demoDepartments,
    amounts: true,
    draft: false,
    approve: false,
    payment: false,
    drafts: [],
    approvals: [],
  },
  manager: {
    view: demoDepartments,
    amounts: true,
    draft: true,
    approve: true,
    payment: false,
    drafts: ['shop', 'warehouse', 'bank'],
    approvals: ['shop', 'warehouse', 'bank', 'hq'],
  },
  sales: {
    view: ['shop', 'hq', 'post'],
    amounts: true,
    draft: true,
    approve: false,
    payment: false,
    drafts: ['shop'],
    approvals: [],
  },
  warehouse: {
    view: ['warehouse', 'post'],
    amounts: false,
    draft: true,
    approve: true,
    payment: false,
    drafts: ['warehouse'],
    approvals: ['warehouse'],
  },
  accountant: {
    view: ['shop', 'warehouse', 'bank', 'post'],
    amounts: true,
    draft: true,
    approve: true,
    payment: true,
    drafts: ['bank'],
    approvals: ['bank'],
  },
  ai: {
    view: ['shop', 'warehouse', 'bank', 'it'],
    amounts: true,
    draft: true,
    approve: false,
    payment: false,
    drafts: ['shop', 'warehouse', 'bank'],
    approvals: [],
  },
}
export function canView(role: DemoRole, department: Department): boolean {
  return permissions[role].view.includes(department)
}
export function canAct(role: DemoRole, action: DemoAction, department: Department): boolean {
  if (!canView(role, department)) return false
  const policy = permissions[role]
  return action === 'draft'
    ? policy.drafts.includes(department)
    : action === 'approve'
      ? policy.approvals.includes(department)
      : policy.payment && department === 'bank'
}
export const demoPeople = [
  {
    id: 'demo-sales',
    name: text('เมย์', 'May'),
    role: text('ฝ่ายขาย', 'Sales'),
    department: 'shop',
    appearance: 'human-coral',
  },
  {
    id: 'demo-manager',
    name: text('นนท์', 'Non'),
    role: text('ผู้จัดการ', 'Manager'),
    department: 'hq',
    appearance: 'human-blue',
  },
  {
    id: 'demo-warehouse',
    name: text('ต้น', 'Ton'),
    role: text('คลังสินค้า', 'Warehouse'),
    department: 'warehouse',
    appearance: 'human-green',
  },
  {
    id: 'demo-accounts',
    name: text('แพร', 'Prae'),
    role: text('ฝ่ายบัญชี', 'Accounts'),
    department: 'bank',
    appearance: 'human-coral',
  },
] satisfies {
  id: string
  name: Text
  role: Text
  department: Department
  appearance: Presence['appearance']
}[]
export const demoSteps: {
  title: Text
  detail: Text
  department: Department
  actor: string
  action?: DemoAction
}[] = [
  {
    title: text('รับคำสั่งซื้อ', 'Order received'),
    detail: text(
      'เมย์รับคำสั่งซื้อจากร้านบ้านสวน: สมุด 20 เล่ม แล้วมอบหมายให้ AI เตรียมใบสั่งขาย',
      'May receives an order from Garden Shop for 20 notebooks and asks AI to prepare a sales order.',
    ),
    department: 'shop',
    actor: 'demo-sales',
  },
  {
    title: text('AI เตรียมใบสั่งขาย', 'AI prepares the order'),
    detail: text(
      'AI ตรวจสินค้าและสต็อก แล้วเตรียม DEMO-SO-001 เป็นฉบับร่าง ส่งให้นนท์ตรวจราคาและเงื่อนไข',
      'AI checks the item and stock, drafts DEMO-SO-001, and hands it to Non to review prices and terms.',
    ),
    department: 'shop',
    actor: 'ai',
    action: 'draft',
  },
  {
    title: text('คนตรวจและอนุมัติ', 'Human review'),
    detail: text(
      'นนท์ตรวจใบสั่งขายและยืนยัน เมย์ส่งงานต่อให้คลัง AI ไม่มีสิทธิยืนยันแทน',
      'Non reviews and confirms the order. May hands it to the warehouse. AI cannot confirm it.',
    ),
    department: 'hq',
    actor: 'demo-manager',
    action: 'approve',
  },
  {
    title: text('คลังส่งสินค้า', 'Warehouse dispatch'),
    detail: text(
      'ต้นตรวจนับสมุด 20 เล่ม ยืนยัน DEMO-DN-001 และส่งแจ้งเตือนให้ฝ่ายขาย สต็อกลดจาก 100 เป็น 80 เล่ม',
      'Ton counts 20 notebooks, confirms DEMO-DN-001, and notifies Sales. Stock changes from 100 to 80.',
    ),
    department: 'warehouse',
    actor: 'demo-warehouse',
    action: 'approve',
  },
  {
    title: text('AI เตรียมใบแจ้งหนี้', 'AI drafts the invoice'),
    detail: text(
      'AI ใช้ใบส่งของเตรียม DEMO-SI-001 เป็นฉบับร่าง แล้วส่งให้แพรตรวจ ไม่บันทึกรับเงินเอง',
      'AI drafts DEMO-SI-001 from the delivery note and asks Prae to check it. It cannot record a payment.',
    ),
    department: 'bank',
    actor: 'ai',
    action: 'draft',
  },
  {
    title: text('บัญชีตรวจและวางบิล', 'Accounts issues the invoice'),
    detail: text(
      'แพรตรวจยอดสินค้าและภาษี แล้วยืนยันใบแจ้งหนี้ รอรับชำระจากลูกค้า',
      'Prae checks the items and tax, confirms the invoice, and awaits the customer’s payment.',
    ),
    department: 'bank',
    actor: 'demo-accounts',
    action: 'approve',
  },
  {
    title: text('รับเงินและปิดงาน', 'Payment and completion'),
    detail: text(
      'แพรตรวจหลักฐานโอนเงิน แล้วยืนยัน DEMO-PE-001 งานขายนี้เสร็จแล้ว ผู้ชมดูได้แต่ทำรายการแทนไม่ได้',
      'Prae checks the transfer evidence and confirms DEMO-PE-001. The sale is complete. Observers can view it but cannot act.',
    ),
    department: 'bank',
    actor: 'demo-accounts',
    action: 'payment',
  },
]
const fact = (
  doctype: string,
  name: string,
  department: Department,
  status: DisplayFact['status'],
): DisplayFact => ({ id: name, doctype, name, kind: 'demo', scope: 'DEMO', department, status })
export function demoSnapshot(index: number, role: DemoRole) {
  const step = Math.max(
    0,
    Math.min(demoSteps.length - 1, Number.isFinite(index) ? Math.trunc(index) : 0),
  )
  const facts: DisplayFact[] = [
    fact('Item', 'DEMO-NOTEBOOK · 20', 'shop', 'stock'),
    fact('Bin', `DEMO-STOCK · ${step >= 3 ? 80 : 100}`, 'warehouse', 'stock'),
    fact(
      'Agent Run',
      'DEMO-AI-001',
      'it',
      step === 1 || step === 4 ? 'ai-running' : step >= 5 ? 'ai-completed' : 'ai-queued',
    ),
  ]
  if (step >= 1)
    facts.push(fact('Sales Order', 'DEMO-SO-001', 'shop', step >= 2 ? 'submitted' : 'draft'))
  if (step === 1) facts.push(fact('Approval', 'DEMO-REVIEW-001', 'hq', 'approval'))
  if (step >= 3)
    facts.push(
      fact('Delivery Note', 'DEMO-DN-001', 'warehouse', 'submitted'),
      fact('Notification', 'DEMO-DISPATCH-001', 'post', 'submitted'),
    )
  if (step >= 4)
    facts.push(fact('Sales Invoice', 'DEMO-SI-001', 'bank', step >= 5 ? 'submitted' : 'draft'))
  if (step >= 6)
    facts.push(
      fact('Payment Entry', 'DEMO-PE-001', 'bank', 'submitted'),
      fact('Notification', 'DEMO-PAID-001', 'post', 'submitted'),
    )
  return {
    step,
    facts: facts.filter((f) => canView(role, f.department)),
    people: demoPeople.filter((p) => canView(role, p.department)),
    events: demoSteps.slice(0, step + 1).filter((e) => canView(role, e.department)),
    ...(permissions[role].amounts ? { total: 2140 } : {}),
  }
}
