import { type Language, statusLabel } from './copy.ts'
import type { DashboardView } from './dashboard-view.ts'
import type { WorkActor } from './live-work.ts'
import type { Department, DisplayFact } from './world-view.ts'
import './town-activity.css'

export type TownActivityPanelProps = {
  language: Language
  facts: DisplayFact[]
  live: boolean
  replaying: boolean
  selectedTruck: boolean
  onSelectTruck: () => void
  onCloseTruck: () => void
  onOpenFact: (fact: DisplayFact) => void
  actors?: WorkActor[]
  view?: DashboardView
}

const workers: { department: Department; th: string; en: string; symbol: string }[] = [
  { department: 'shop', th: 'ฝ่ายขาย', en: 'Sales', symbol: '▤' },
  { department: 'warehouse', th: 'ทีมคลังสินค้า', en: 'Warehouse team', symbol: '▣' },
  { department: 'bank', th: 'ฝ่ายบัญชี', en: 'Accounts', symbol: '◇' },
]

export function TownActivityPanel({
  language,
  facts,
  live,
  replaying,
  selectedTruck,
  onSelectTruck,
  onCloseTruck,
  onOpenFact,
  actors = [],
  view,
}: TownActivityPanelProps) {
  const th = language === 'th'
  // A disconnected view must not retain names or statuses from an earlier company.
  const visible = view ? view.facts : live ? facts : []
  const observedActors = view ? view.work.actors.map((a) => a.intent) : actors
  const demoMotion = observedActors.some((a) => a.simulation === 'loop')
  const deliveries = visible.filter(
    (fact) => fact.doctype === 'Delivery Note' && fact.kind === 'document',
  )
  const runs = visible.filter(
    (fact) => fact.doctype === 'Town Agent Run' && fact.kind === 'agent_run',
  )
  const activeAI =
    live && !replaying
      ? observedActors.filter((a) => a.kind === 'agent' && a.activity !== 'idle').length
      : 0

  return (
    <section className="town-activity" aria-label={th ? 'งานในเมือง' : 'Work in town'}>
      <div className="town-activity-heading">
        <h2>{th ? 'งานในเมือง' : 'Work in town'}</h2>
        <span className="town-activity-mode">
          {replaying
            ? th
              ? 'ย้อนหลัง'
              : 'Earlier view'
            : demoMotion
              ? th
                ? 'ภาพสาธิตหลายทีม'
                : 'Concurrent demo motion'
              : th
                ? 'สถานะการทำงาน'
                : 'Live work'}
        </span>
      </div>
      <ul className="town-crew-list">
        {workers.map((worker) => {
          const count =
            live && !replaying
              ? observedActors.filter(
                  (actor) =>
                    actor.kind === 'human' &&
                    actor.department === worker.department &&
                    actor.activity !== 'idle',
                ).length
              : 0
          return (
            <li key={worker.department}>
              <span className={`town-crew-icon ${worker.department}`} aria-hidden="true">
                {worker.symbol}
              </span>
              <div>
                <strong>{th ? worker.th : worker.en}</strong>
                <small>
                  {count > 0
                    ? th
                      ? `${demoMotion ? 'ภาพสาธิต' : 'กำลังดำเนินงาน'} ${count} คน`
                      : `${count} ${demoMotion ? 'demo actors' : count === 1 ? 'person active' : 'people active'}`
                    : th
                      ? 'ไม่มีคนแสดงสถานะกำลังทำงาน'
                      : 'No active people reported'}
                </small>
              </div>
            </li>
          )
        })}
        <li>
          <span className="town-crew-icon it" aria-hidden="true">
            ⌘
          </span>
          <div>
            <strong>{th ? 'ผู้ช่วย AI' : 'AI helper'}</strong>
            <small>
              {activeAI > 0
                ? th
                  ? `${demoMotion ? 'ภาพสาธิต AI' : 'AI กำลังดำเนินงาน'} ${activeAI} งาน`
                  : `${activeAI} ${demoMotion ? 'demo AI actors' : 'active AI tasks'}`
                : runs.length > 0
                  ? th
                    ? `มีสถานะงานจาก ERP ${runs.length} รายการ`
                    : `${runs.length} ERP run ${runs.length === 1 ? 'status' : 'statuses'} available`
                  : th
                    ? 'พร้อมทดลองในเครื่อง · ยังไม่มีงาน AI จาก ERP'
                    : 'Local test ready · no ERP AI runs visible'}
            </small>
          </div>
        </li>
      </ul>
      {runs.length > 0 && (
        <ul
          className="town-agent-statuses"
          aria-label={th ? 'สถานะงาน AI จาก ERP' : 'ERP AI run statuses'}
        >
          {runs.map((fact) => (
            <li key={fact.id}>
              <strong>{fact.name}</strong>
              <span>{statusLabel(language, fact.status)}</span>
            </li>
          ))}
        </ul>
      )}
      <p className="town-activity-note">
        {demoMotion
          ? th
            ? 'ตัวละครและรถวนภาพสาธิตพร้อมกัน สถานะเอกสารและงาน AI จริงแสดงแยกตามข้อมูล ERP'
            : 'Actors and the truck repeat concurrent demo motion. Actual documents and AI runs retain their ERP statuses.'
          : th
            ? 'ตัวละครไปยังแผนกตามงานที่กำลังทำ จำนวนเอกสารไม่ใช่จำนวนคนทำงาน เส้นทางแสดงเฉพาะระหว่างเดินทาง'
            : 'Actors move to their active work department. Document counts are not active staff counts. Routes appear only during travel.'}
      </p>
      <button
        type="button"
        className="town-truck-button"
        aria-expanded={selectedTruck}
        aria-controls="town-truck-status"
        onClick={selectedTruck ? onCloseTruck : onSelectTruck}
      >
        {th ? 'ดูสถานะรถบรรทุก' : 'Inspect delivery truck'}
        <span aria-hidden="true">{selectedTruck ? '−' : '→'}</span>
      </button>
      {selectedTruck && (
        <section
          id="town-truck-status"
          className="town-truck-status"
          aria-label={th ? 'งานส่งของ' : 'Delivery work'}
        >
          <div className="town-activity-heading">
            <h3>{th ? 'งานส่งของ' : 'Delivery work'}</h3>
            <button
              type="button"
              className="town-truck-close"
              onClick={onCloseTruck}
              aria-label={th ? 'ปิดสถานะรถบรรทุก' : 'Close truck status'}
            >
              ×
            </button>
          </div>
          <p className="town-activity-note">
            {demoMotion
              ? th
                ? 'รถกำลังวิ่งวนเพื่อสาธิต ไม่มีการสร้างใบส่งของเพิ่ม สถานะเอกสารด้านล่างมาจาก ERP'
                : 'The demo truck circulates without creating deliveries. Document statuses below come from ERP.'
              : th
                ? 'รถแสดงภาพงานขนส่งของเมือง ไม่ใช่ข้อมูล GPS หรือการยืนยันว่าของถึงแล้ว สถานะด้านล่างมาจากใบส่งของที่คุณมีสิทธิ์ดู'
                : 'The truck visualizes transport work. It is not GPS tracking or proof of arrival. These statuses come from delivery notes you can view.'}
          </p>
          {!live ? (
            <p className="town-truck-empty">
              {th
                ? 'เชื่อมต่อ ERP ก่อน เพื่อดูใบส่งของของคุณ'
                : 'Connect to ERP to view your delivery notes.'}
            </p>
          ) : deliveries.length === 0 ? (
            <p className="town-truck-empty">
              {th ? 'ยังไม่มีใบส่งของที่คุณดูได้' : 'No delivery notes are visible to you yet.'}
            </p>
          ) : (
            <ul className="town-delivery-list">
              {deliveries.map((fact) => (
                <li key={fact.id}>
                  <div>
                    <strong>{fact.name}</strong>
                    <span>{statusLabel(language, fact.status)}</span>
                  </div>
                  {!replaying && (
                    <button
                      type="button"
                      aria-label={`${th ? 'ดูใบส่งของ' : 'Inspect delivery note'} ${fact.name}`}
                      onClick={() => onOpenFact(fact)}
                    >
                      {th ? 'ดูใบส่งของ' : 'Inspect'}
                    </button>
                  )}
                </li>
              ))}
            </ul>
          )}
          {replaying && (
            <p className="town-activity-note">
              {th
                ? 'กำลังดูข้อมูลย้อนหลัง กลับสู่ปัจจุบันก่อนเปิดเอกสาร'
                : 'This is an earlier view. Return to live before opening documents.'}
            </p>
          )}
        </section>
      )}
    </section>
  )
}
