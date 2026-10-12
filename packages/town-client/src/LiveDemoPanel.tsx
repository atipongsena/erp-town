import { useEffect, useState } from 'react'
import { departmentNames, type Language } from './copy.ts'
import { decodeLiveDemo, type LiveDemo } from './live-work.ts'
import { classicPath } from './world-view.ts'

export function useLiveDemo(company: string, enabled: boolean): LiveDemo | undefined {
  const [view, setView] = useState<LiveDemo>()
  useEffect(() => {
    setView(undefined)
    if (!enabled || !company) return
    const abort = new AbortController()
    let timer: ReturnType<typeof setTimeout> | undefined
    const poll = async () => {
      try {
        const response = await fetch('/town/api/live-demo', {
          credentials: 'same-origin',
          signal: AbortSignal.any([abort.signal, AbortSignal.timeout(5000)]),
        })
        if (!response.ok) throw new Error('unavailable')
        const text = await response.text()
        if (text.length > 65536) throw new Error('invalid')
        const decoded = decodeLiveDemo(JSON.parse(text), company)
        if (!abort.signal.aborted) setView(decoded)
      } catch {
        if (!abort.signal.aborted) setView(undefined)
      }
      if (!abort.signal.aborted) timer = setTimeout(() => void poll(), 1000)
    }
    void poll()
    return () => {
      abort.abort()
      clearTimeout(timer)
    }
  }, [company, enabled])
  return view
}
const stages: Record<string, [string, string]> = {
  'sales-review': ['ฝ่ายขายตรวจและยืนยันใบสั่งขาย', 'Sales reviews and confirms order'],
  'stock-prepare': ['คลังเตรียมและตรวจใบส่งของ', 'Warehouse prepares and checks delivery'],
  dispatch: ['ส่งต่องานจากใบส่งของที่ยืนยันแล้ว', 'Handoff from confirmed delivery'],
  'invoice-review': ['บัญชีจัดทำและตรวจใบแจ้งหนี้', 'Accounts prepares and reviews invoice'],
  'payment-review': ['บัญชีตรวจและบันทึกรับเงิน', 'Accounts reviews and records receipt'],
  'round-completed': ['จบรอบงานแล้ว', 'Round completed'],
  'return-to-hq': ['กลับจุดพักก่อนรับงานถัดไป', 'Returning before the next task'],
  completed: ['ดำเนินงานครบแล้ว', 'Workflow completed'],
  starting: ['เตรียมระบบ', 'Preparing'],
  'ai-order': ['AI เตรียมใบสั่งขาย', 'AI prepares order'],
  'review-order': ['ฝ่ายขายตรวจใบสั่งขาย', 'Sales reviews order'],
  'submit-order': ['ฝ่ายขายยืนยันใบสั่งขาย', 'Sales confirms order'],
  'create-delivery': ['คลังเตรียมใบส่งของ', 'Warehouse prepares delivery'],
  'review-delivery': ['คลังตรวจใบส่งของ', 'Warehouse reviews delivery'],
  'submit-delivery': ['คลังยืนยันการส่งของ', 'Warehouse confirms delivery'],
  'ai-invoice': ['AI เตรียมใบแจ้งหนี้', 'AI prepares invoice'],
  'review-invoice': ['บัญชีตรวจใบแจ้งหนี้', 'Accounts reviews invoice'],
  'submit-invoice': ['บัญชียืนยันใบแจ้งหนี้', 'Accounts confirms invoice'],
  'create-payment': ['บัญชีเตรียมรายการรับเงิน', 'Accounts prepares receipt'],
  'review-payment': ['บัญชีตรวจรายการรับเงิน', 'Accounts reviews receipt'],
  'submit-payment': ['บัญชียืนยันรับเงิน', 'Accounts confirms receipt'],
  complete: ['จบรอบงานแล้ว', 'Round completed'],
  failed: ['หยุดเพื่อตรวจสอบ', 'Stopped for review'],
}
export function LiveDemoPanel({
  view,
  language,
  looping = false,
}: {
  view: LiveDemo
  language: Language
  looping?: boolean
}) {
  const th = language === 'th'
  const active = view.actors.filter((a) => a.activity !== 'idle')
  const title = stages[view.stage]?.[th ? 0 : 1] ?? (th ? 'ดำเนินงานใน ERP' : 'ERP work in progress')
  return (
    <section
      className="live-demo-panel"
      aria-label={th ? 'การทำงานจริงในไซต์ทดสอบ' : 'Live ERP test workflow'}
    >
      <span className="eyebrow">
        LIVE ERP · {th ? 'รอบ' : 'ROUND'} {view.round}
      </span>
      <h2>{title}</h2>
      {looping && (
        <p className="demo-motion-notice">
          {th
            ? 'ภาพสาธิตหลายทีมทำงานพร้อมกัน · วนซ้ำโดยไม่สร้างรายการ ERP เพิ่ม'
            : 'Concurrent demo motion · loops without repeating ERP transactions'}
        </p>
      )}
      <p>
        {th
          ? 'บันทึกเอกสารใน ERP จริงของ test.localhost คนใช้บัญชีสาธิตตามบทบาท และ AI ใช้ตัวทดสอบในเครื่อง'
          : 'Real documents on test.localhost. People use demonstration staff roles; AI runs locally.'}
      </p>
      <ul>
        {active.map((a) => (
          <li key={a.id}>
            <strong>{a.kind === 'agent' ? 'AI' : th ? 'พนักงานสาธิต' : 'Demo staff'}</strong> ·{' '}
            {departmentNames[language][a.department]} ·{' '}
            {a.activity === 'travelling'
              ? th
                ? 'กำลังไปทำงาน'
                : 'Going to work'
              : th
                ? 'กำลังดำเนินงาน'
                : 'Working'}
          </li>
        ))}
      </ul>
      {view.documents.length > 0 && (
        <ul aria-label={th ? 'เอกสารจาก ERP จริง' : 'Actual ERP documents'}>
          {view.documents.map((doc) => (
            <li key={`${doc.doctype}:${doc.name}`}>
              <a href={classicPath(doc.doctype, doc.name)}>{doc.name} ↗</a> ·{' '}
              {doc.status === 'Draft'
                ? th
                  ? 'ร่าง'
                  : 'Draft'
                : doc.status === 'Submitted'
                  ? th
                    ? 'ยืนยันแล้ว'
                    : 'Submitted'
                  : th
                    ? 'ยกเลิก'
                    : 'Cancelled'}
            </li>
          ))}
        </ul>
      )}
      {view.error && (
        <p role="alert">
          {th ? 'หยุดการทำงานแล้ว กรุณาตรวจสอบก่อนเริ่มใหม่' : 'Work stopped. Review before restarting.'} ·{' '}
          {view.error}
        </p>
      )}
    </section>
  )
}
