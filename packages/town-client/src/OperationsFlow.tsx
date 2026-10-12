import { type Language, statusLabel } from './copy.ts'
import { operationsFlow } from './operations-flow.ts'
import type { Department, DisplayFact } from './world-view.ts'
import './operations-flow.css'

const labels: Record<string, [string, string]> = {
  'Sales Order': ['ใบสั่งขาย', 'Orders'],
  'Delivery Note': ['ใบส่งของ', 'Deliveries'],
  'Sales Invoice': ['ใบแจ้งหนี้', 'Invoices'],
  'Payment Entry': ['รับชำระเงิน', 'Receipts'],
}

export function OperationsFlow({
  language,
  facts,
  available,
  replaying,
  onSelect,
  queues,
}: {
  language: Language
  facts: DisplayFact[]
  available: boolean
  replaying: boolean
  onSelect: (department: Department) => void
  queues?: ReturnType<typeof operationsFlow>
}) {
  const th = language === 'th'
  const title = th ? 'ภาพรวมขั้นตอนงาน' : 'Operations flow'
  return (
    <section className="operations-flow" aria-label={title}>
      <div className="operations-flow-heading">
        <h2>{title}</h2>
        <span>{available ? (replaying ? (th ? 'ย้อนหลัง' : 'Earlier view') : 'ERP') : '—'}</span>
      </div>
      <ol>
        {(queues ?? operationsFlow(facts, available)).map((stage, index) => (
          <li key={stage.doctype}>
            <button type="button" onClick={() => onSelect(stage.department)}>
              <span className="operations-flow-step" aria-hidden="true">
                {index + 1}
              </span>
              <strong>{labels[stage.doctype]?.[th ? 0 : 1]}</strong>
              <span className="operations-flow-count">{stage.total ?? '—'}</span>
              <span className="operations-flow-states">
                {stage.total === undefined
                  ? th
                    ? 'เชื่อมต่อ ERP'
                    : 'Connect to ERP'
                  : stage.total === 0
                    ? th
                      ? 'ไม่มีเอกสารที่มองเห็น'
                      : 'No visible documents'
                    : stage.statuses.map(({ status, count }) => (
                        <span key={status} data-status={status}>
                          {statusLabel(language, status)} · {count}
                        </span>
                      ))}
              </span>
            </button>
          </li>
        ))}
      </ol>
      <p>
        {th
          ? 'คลิกขั้นตอนเพื่อดูแผนกและเอกสาร นับเฉพาะรายการที่คุณมีสิทธิ์ดู แต่ละขั้นไม่ใช่เอกสารในคำสั่งซื้อเดียวกัน'
          : 'Click a stage to inspect its department and documents. Counts cover visible records, not a linked order journey.'}
      </p>
    </section>
  )
}
