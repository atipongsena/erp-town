import { useState } from 'react'
import { type Language, statusLabel } from './copy.ts'
import { type DemoRole, type demoSnapshot, roleNames } from './demo.ts'
import type { Department } from './world-view.ts'
import './demo-erp.css'

const documentNames: Record<string, { th: string; en: string }> = {
  'Sales Order': { th: 'ใบสั่งขาย', en: 'Sales Order' },
  'Delivery Note': { th: 'ใบส่งของ', en: 'Delivery Note' },
  'Sales Invoice': { th: 'ใบแจ้งหนี้', en: 'Sales Invoice' },
  'Payment Entry': { th: 'ใบรับชำระเงิน', en: 'Payment Entry' },
}
const money = (value: number) =>
  `฿${value.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`

/** Fictional ERP presentation over the same role-filtered story as the town. */
export function DemoErpDesk({
  language,
  role,
  view,
  onCompleteExample,
  onShowTown,
}: {
  language: Language
  role: DemoRole
  view: ReturnType<typeof demoSnapshot>
  onCompleteExample: () => void
  onShowTown: (department: Department) => void
}) {
  const t = (th: string, en: string) => (language === 'th' ? th : en)
  const [selectedName, setSelectedName] = useState('')
  const documents = view.facts.filter((f) => Object.hasOwn(documentNames, f.doctype))
  // Derive selection from the current scope; never retain a previous role's details.
  const selected =
    documents.find((f) => f.name === selectedName) ??
    documents.find((f) => f.doctype === 'Sales Invoice') ??
    documents[0]
  const invoice = documents.find((f) => f.doctype === 'Sales Invoice')
  const payment = documents.find((f) => f.doctype === 'Payment Entry')
  const stock = view.facts.find((f) => f.doctype === 'Bin')
  const postedInvoice = invoice?.status === 'submitted'
  const showAmounts = view.total !== undefined
  const balance = postedInvoice && showAmounts ? money(payment ? 0 : (view.total ?? 0)) : '—'

  return (
    <section className="demo-erp" aria-label={t('ระบบ ERP', 'ERP workspace')}>
      <div className="erp-heading">
        <div>
          <span className="eyebrow">DEMO COMPANY / ERP</span>
          <h1>{t('จากคำสั่งซื้อ สู่การรับชำระ', 'From order to payment')}</h1>
          <p>{t('ฝ่ายขาย · คลังสินค้า · บัญชี', 'Sales · Inventory · Accounting')}</p>
        </div>
        <span className="erp-sample">{t('ข้อมูลตัวอย่าง', 'Sample records')}</span>
      </div>
      <p className="erp-context">
        {t('ฉาก', 'Scene')} {view.step + 1}/7 · {roleNames[role][language]} ·{' '}
        {t('เอกสารชุดเดียวกับเมือง 3D', 'The same documents as the 3D town')}
      </p>
      <div className="erp-metrics">
        <div>
          <span>{t('เอกสารที่ดูได้', 'Visible documents')}</span>
          <strong>{documents.length.toString().padStart(2, '0')}</strong>
        </div>
        {stock && (
          <div>
            <span>{t('สมุดคงเหลือ', 'Notebooks in stock')}</span>
            <strong>{view.step >= 3 ? 80 : 100}</strong>
          </div>
        )}
        {showAmounts && (
          <div>
            <span>{t('ยอดค้างชำระ', 'Outstanding amount')}</span>
            <output aria-label={t('ยอดค้างชำระ', 'Outstanding amount')}>{balance}</output>
          </div>
        )}
      </div>
      <div className="erp-workspace">
        <nav className="erp-register" aria-label={t('เอกสารตัวอย่าง', 'Sample documents')}>
          <h2>{t('เอกสารธุรกิจ', 'Business documents')}</h2>
          <p>SO → DN → SI → PE</p>
          {documents.map((doc) => (
            <button
              key={doc.id}
              type="button"
              aria-pressed={selected?.id === doc.id}
              onClick={() => setSelectedName(doc.name)}
            >
              <span>{documentNames[doc.doctype]?.[language]}</span>
              <strong>{doc.name}</strong>
              <small className={`status ${doc.status}`}>{statusLabel(language, doc.status)}</small>
            </button>
          ))}
          {documents.length === 0 && <p>{t('ยังไม่มีเอกสารในฉากนี้', 'No documents in this scene.')}</p>}
        </nav>
        {selected ? (
          <article className="erp-document" aria-label={t('รายละเอียดเอกสาร', 'Document details')}>
            <div className="erp-document-heading">
              <div>
                <h2>{documentNames[selected.doctype]?.[language]}</h2>
                <h3>{selected.name}</h3>
              </div>
              <span className={`status ${selected.status}`}>
                {statusLabel(language, selected.status)}
              </span>
            </div>
            <dl className="erp-fields">
              <div>
                <dt>{t('ลูกค้า', 'Customer')}</dt>
                <dd>{t('ร้านบ้านสวน', 'Garden Shop')}</dd>
              </div>
              <div>
                <dt>{t('บริษัท', 'Company')}</dt>
                <dd>DEMO COMPANY</dd>
              </div>
              <div>
                <dt>{t('สถานะเอกสาร', 'Document status')}</dt>
                <dd>{statusLabel(language, selected.status)}</dd>
              </div>
              <div>
                <dt>{t('แหล่งข้อมูล', 'Source')}</dt>
                <dd>{t('เรื่องราวตัวอย่างในเบราว์เซอร์', 'Browser-local sample story')}</dd>
              </div>
            </dl>
            {selected.doctype === 'Payment Entry' ? (
              <div className="erp-payment">
                <span>{t('รับชำระจากลูกค้า', 'Customer payment received')}</span>
                {showAmounts && <strong>{money(view.total ?? 0)}</strong>}
                <p>
                  {t('อ้างอิงใบแจ้งหนี้', 'Invoice reference')}: {invoice?.name ?? '—'}
                </p>
              </div>
            ) : (
              <div className="erp-items">
                <table>
                  <caption>{t('รายการสินค้า', 'Items')}</caption>
                  <thead>
                    <tr>
                      <th scope="col">{t('สินค้า', 'Item')}</th>
                      <th scope="col">{t('จำนวน', 'Qty')}</th>
                      {showAmounts && <th scope="col">{t('ราคาต่อหน่วย', 'Unit price')}</th>}
                      {showAmounts && <th scope="col">{t('มูลค่า', 'Amount')}</th>}
                    </tr>
                  </thead>
                  <tbody>
                    <tr>
                      <td>
                        <strong>DEMO-NOTEBOOK</strong>
                        <span>{t('สมุด', 'Notebook')}</span>
                      </td>
                      <td>20</td>
                      {showAmounts && <td>{money(100)}</td>}
                      {showAmounts && <td>{money(2000)}</td>}
                    </tr>
                  </tbody>
                </table>
              </div>
            )}
            {showAmounts && selected.doctype !== 'Payment Entry' && (
              <dl className="erp-totals">
                <div>
                  <dt>{t('ค่าสินค้า', 'Subtotal')}</dt>
                  <dd>{money(2000)}</dd>
                </div>
                <div>
                  <dt>{t('VAT 7% (ตัวอย่าง)', 'VAT 7% (sample)')}</dt>
                  <dd>{money(140)}</dd>
                </div>
                <div>
                  <dt>{t('ยอดรวม', 'Grand total')}</dt>
                  <dd>{money(view.total ?? 0)}</dd>
                </div>
                {selected.doctype === 'Sales Invoice' && (
                  <div>
                    <dt>{t('การชำระเงิน', 'Payment status')}</dt>
                    <dd>
                      {postedInvoice
                        ? payment
                          ? t('ชำระแล้ว', 'Paid')
                          : t('รอรับชำระ', 'Awaiting payment')
                        : t('ยังไม่ยืนยัน', 'Not posted')}
                    </dd>
                  </div>
                )}
              </dl>
            )}
            <button
              type="button"
              className="erp-show-town"
              onClick={() => onShowTown(selected.department)}
            >
              {t('ดูแผนกนี้ในเมือง 3D', 'Show department in 3D town')} ↗
            </button>
          </article>
        ) : (
          <div className="erp-empty">
            <span className="eyebrow">ORDER → DELIVERY → INVOICE → PAYMENT</span>
            <h2>{t('เอกสารเกิดขึ้นตามงาน', 'Documents follow the work')}</h2>
            <p>
              {t(
                'ใช้ฉากถัดไปเพื่อดู AI เตรียมร่าง คนยืนยัน และบัญชีรับชำระ หรือเปิดตัวอย่างที่เสร็จแล้ว',
                'Use Next scene to follow AI drafts, human review and payment, or open the completed example.',
              )}
            </p>
          </div>
        )}
      </div>
      <div className="erp-footer">
        <p>
          {t(
            'ตัวอย่างหน้าจอ ERP Town · ดูเอกสารจริงใน Classic ERP ผ่าน full-stack local demo',
            'ERP Town sample workspace · Inspect real records in Classic ERP through the full-stack local demo.',
          )}
        </p>
        {view.step < 6 && (
          <button type="button" onClick={onCompleteExample}>
            {t('ดูตัวอย่างที่เสร็จแล้ว', 'View completed example')}
          </button>
        )}
      </div>
    </section>
  )
}
