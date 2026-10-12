import { type ReactNode, useEffect, useState } from 'react'
import { createPortal } from 'react-dom'
import { departmentNames, type Language, statusLabel } from './copy.ts'
import type { DashboardView, TownSelection } from './dashboard-view.ts'
import { OperationsFlow } from './OperationsFlow.tsx'
import './dashboard.css'
export function TownDashboard({
  view,
  selection,
  language,
  onSelect,
  onOpen,
  onFollow,
  following,
  children,
  journey,
}: {
  view: DashboardView
  selection?: TownSelection
  language: Language
  onSelect: (s: TownSelection | undefined) => void
  onOpen: (doctype: string, name: string) => void
  onFollow: () => void
  following: boolean
  children?: ReactNode
  journey?: ReactNode
}) {
  const th = language === 'th'
  const demoMotion = view.work.actors.some((a) => a.intent.simulation === 'loop')
  const [expanded, setExpanded] = useState(false)
  const [drawer, setDrawer] = useState(true)
  const [drawerTarget, setDrawerTarget] = useState<Element | null>(null)
  useEffect(() => {
    const query = window.matchMedia('(max-width: 1099px)')
    const update = () =>
      setDrawerTarget(query.matches ? document.getElementById('town-dashboard-drawer') : null)
    update()
    query.addEventListener('change', update)
    return () => query.removeEventListener('change', update)
  }, [])
  const selected =
    selection?.type === 'department'
      ? departmentNames[language][selection.department]
      : selection?.type === 'document'
        ? selection.name
        : selection?.id
  const actor =
    selection?.type === 'actor' ? view.work.actors.find((a) => a.id === selection.id) : undefined
  const trip =
    selection?.type === 'trip' ? view.delivery.trips.find((t) => t.id === selection.id) : undefined
  const panels = (
    <>
      <div className="hud-flow hud-panel">
        <OperationsFlow
          language={language}
          facts={view.facts}
          queues={view.queues}
          available={view.available}
          replaying={view.source === 'historical'}
          onSelect={(department) => onSelect({ type: 'department', department })}
        />
      </div>
      <section
        className="hud-selection hud-panel"
        aria-label={th ? 'รายการที่เลือก' : 'Selected object'}
        data-selection={selection?.type ?? 'none'}
      >
        <strong>
          {th ? 'เลือก: ' : 'Selected: '}
          {selected ?? (th ? 'ภาพรวมเมือง' : 'Town overview')}
        </strong>
        {actor && (
          <p>
            {actor.phase} · {departmentNames[language][actor.intent.department]}
            {actor.intent.simulation === 'loop' ? (th ? ' · ภาพสาธิต' : ' · Demo motion') : ''}
            {actor.waiting ? ` · ${th ? 'รอพื้นที่ในฉาก' : 'Scene resource wait'}` : ''}
          </p>
        )}
        {trip && (
          <p>
            {trip.loop
              ? th
                ? 'รถสาธิต · วิ่งวน'
                : 'Demo truck · circulating'
              : `${trip.name} · ${th ? 'บันทึกส่งของ' : 'Dispatch recorded'}`}{' '}
            · {trip.phase}
          </p>
        )}
        {(actor || trip) && (
          <button type="button" aria-pressed={following} onClick={onFollow}>
            {following
              ? th
                ? 'หยุดติดตาม'
                : 'Stop following'
              : th
                ? 'ติดตามในฉาก'
                : 'Follow in scene'}
          </button>
        )}
        {selection?.type === 'document' && view.source === 'live' && (
          <button type="button" onClick={() => onOpen(selection.doctype, selection.name)}>
            {th ? 'ตรวจเอกสาร' : 'Inspect document'}
          </button>
        )}
        {selection && (
          <button type="button" onClick={() => onSelect(undefined)}>
            {th ? 'ปิดรายการ' : 'Clear selection'}
          </button>
        )}
      </section>
      <section
        className={`hud-queue hud-panel ${expanded ? 'expanded' : ''}`}
        aria-label={th ? 'คิวงานในเมือง' : 'Town work queue'}
      >
        <div className="hud-heading">
          <h2>{th ? 'คิวงานในเมือง' : 'Town work queue'}</h2>
          <button type="button" aria-expanded={expanded} onClick={() => setExpanded(!expanded)}>
            {expanded ? (th ? 'ย่อ' : 'Collapse') : th ? 'ขยาย' : 'Expand'}
          </button>
        </div>
        <div className="hud-queue-scroll">
          {view.work.actors.map((a) => (
            <button
              type="button"
              key={`actor:${a.id}`}
              onClick={() => onSelect({ type: 'actor', id: a.id })}
              aria-label={`${th ? 'เลือกตัวละคร' : 'Select actor'} ${a.id}`}
            >
              <span>
                {a.intent.kind === 'agent' ? 'AI' : th ? 'พนักงาน' : 'Staff'} ·{' '}
                {departmentNames[language][a.intent.department]}
              </span>
              <small>
                {a.phase}
                {a.intent.simulation === 'loop' ? (th ? ' · ภาพสาธิต' : ' · Demo motion') : ''}
              </small>
            </button>
          ))}
          {view.delivery.trips
            .filter((t) => t.phase !== 'finished')
            .map((t) => (
              <button
                type="button"
                key={`trip:${t.id}`}
                onClick={() => onSelect({ type: 'trip', id: t.id })}
              >
                <span>
                  {t.loop
                    ? th
                      ? 'รถสาธิต · วิ่งวน'
                      : 'Demo truck · circulating'
                    : `${t.name} · ${th ? 'บันทึกส่งของ' : 'Dispatch recorded'}`}
                </span>
                <small>{t.phase}</small>
              </button>
            ))}
          {view.facts.map((f) => (
            <button
              type="button"
              key={f.id}
              aria-label={`${th ? 'เลือก' : 'Select'} ${f.name}`}
              onClick={() =>
                onSelect(
                  f.kind === 'document'
                    ? { type: 'document', doctype: f.doctype, name: f.name }
                    : { type: 'department', department: f.department },
                )
              }
            >
              <span>
                {f.name} · {statusLabel(language, f.status)}
              </span>
              <small>{f.doctype}</small>
            </button>
          ))}
          {!view.facts.length && !view.work.actors.length && (
            <p>
              {view.available
                ? th
                  ? 'ไม่มีรายการที่มองเห็น'
                  : 'No visible work'
                : th
                  ? 'เชื่อมต่อ ERP เพื่อดูงาน'
                  : 'Connect ERP to inspect work'}
            </p>
          )}
          {(view.work.overflow > 0 || view.delivery.overflow > 0) && (
            <p>
              {th ? 'งานเกินจำนวนที่แสดงในฉาก' : 'More work than scene capacity'}:{' '}
              {view.work.overflow + view.delivery.overflow}
            </p>
          )}
        </div>
        <small>
          {th
            ? 'สถานะเดินทางเป็นภาพจำลอง ไม่ยืนยันการส่งถึงลูกค้า'
            : 'Scene travel does not prove delivery to a customer'}
        </small>
      </section>
      {journey && <div className="hud-journey hud-panel">{journey}</div>}
    </>
  )
  return (
    <div
      className="town-dashboard"
      data-source={view.source}
      data-version={view.version?.toString()}
      data-drawer={drawer ? 'open' : 'closed'}
    >
      <section
        className="hud-kpis hud-panel"
        aria-label={th ? 'สถานะงานที่มองเห็น' : 'Visible work metrics'}
      >
        {[
          [th ? 'เอกสารที่มองเห็น' : 'Visible documents', view.documents, 'documents'],
          [th ? 'สัญญาณงาน' : 'Work signals', view.signals, 'signals'],
          [th ? 'AI ที่กำลังรัน' : 'Active AI runs', view.aiRuns, 'ai'],
          [
            demoMotion
              ? th
                ? 'ภาพสาธิตกำลังทำงาน'
                : 'Demo actors working'
              : th
                ? 'กำลังทำงานในฉาก'
                : 'Working in scene',
            view.working,
            'working',
          ],
        ].map(([label, count, id]) => (
          <div key={String(id)}>
            <span>{label}</span>
            <strong data-kpi={id}>{count ?? '—'}</strong>
          </div>
        ))}
        <small>
          {view.source === 'historical'
            ? th
              ? 'ย้อนหลัง · อ่านอย่างเดียว'
              : 'Historical · read only'
            : view.available
              ? `${view.company} · ERP${demoMotion ? (th ? ' · ภาพสาธิตวนซ้ำ' : ' · Demo motion loop') : ''}`
              : th
                ? 'ยังไม่มีข้อมูลที่เชื่อมต่อ'
                : 'Connected data unavailable'}
        </small>
      </section>
      {drawerTarget ? (
        <>
          <button
            className="hud-drawer-toggle hud-panel"
            type="button"
            aria-expanded={drawer}
            aria-controls="town-work-controls"
            onClick={() => setDrawer(!drawer)}
          >
            {drawer
              ? th
                ? 'ปิดแผงงาน'
                : 'Close work drawer'
              : th
                ? 'เปิดแผงงาน'
                : 'Open work drawer'}
          </button>
          {createPortal(<div className="hud-drawer">{panels}</div>, drawerTarget)}
        </>
      ) : (
        panels
      )}
      <div className="hud-timeline">{children}</div>
    </div>
  )
}
