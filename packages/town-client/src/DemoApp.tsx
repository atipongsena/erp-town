import { useReducedMotion } from 'motion/react'
import { lazy, Suspense, useEffect, useState } from 'react'
import { copy, departmentNames, statusLabel } from './copy.ts'
import { DemoErpDesk } from './DemoErpDesk.tsx'
import {
  canAct,
  canView,
  type DemoAction,
  type DemoRole,
  demoDepartments,
  demoSnapshot,
  demoSteps,
  permissions,
  roleNames,
} from './demo.ts'
import { EnvironmentControl } from './EnvironmentControl.tsx'
import type { WorkActor } from './live-work.ts'
import './demo.css'
import tokens from './tokens.json'
import { useUi } from './ui-store.ts'
import { usePresentation } from './use-presentation.ts'
import type { Department } from './world-view.ts'

const Scene = lazy(() => import('./Scene.tsx').then((m) => ({ default: m.Scene })))
const showcaseActors: WorkActor[] = [
  {
    id: 'demo-shop',
    kind: 'human',
    department: 'shop',
    activity: 'idle',
    appearance: 'human-coral',
  },
  {
    id: 'demo-warehouse',
    kind: 'human',
    department: 'warehouse',
    activity: 'idle',
    appearance: 'human-green',
  },
  {
    id: 'demo-bank',
    kind: 'human',
    department: 'bank',
    activity: 'idle',
    appearance: 'human-blue',
  },
  { id: 'demo-assistant', kind: 'agent', department: 'it', activity: 'idle' },
]
const noTrips: { id: string; name: string }[] = []
export function DemoApp() {
  const { language, setLanguage, economy, sceneTime, setEconomy } = useUi()
  const th = language === 'th'
  const t = (thai: string, english: string) => (th ? thai : english)
  const reduced = useReducedMotion() ?? false
  const [role, setRole] = useState<DemoRole>('observer')
  const [workspace, setWorkspace] = useState(() =>
    new URLSearchParams(window.location.search).get('view') === 'erp' ? 'erp' : 'town',
  )
  const [step, setStep] = useState(() => {
    const scene = Number(new URLSearchParams(window.location.search).get('scene'))
    return Number.isInteger(scene) && scene >= 1 && scene <= demoSteps.length ? scene - 1 : 0
  })
  const [playing, setPlaying] = useState(false)
  const [selected, select] = useState<Department>()
  const [tab, setTab] = useState<'work' | 'rights'>('work')
  const [assetError, setAssetError] = useState(false)
  const [ready, setReady] = useState(false)
  const [notice, setNotice] = useState(false)
  const [motionEnabled, setMotionEnabled] = useState(!reduced)
  const presentation = usePresentation(
    showcaseActors,
    noTrips,
    `showcase:${role}`,
    role === 'observer',
    true,
    motionEnabled && workspace === 'town',
  )
  const staticDemo = import.meta.env.VITE_TOWN_STATIC_DEMO === 'true'
  const view = demoSnapshot(step, role)
  const event = demoSteps[view.step]
  const nextEvent = demoSteps[view.step + 1]
  const canComplete = nextEvent?.action && canAct(role, nextEvent.action, nextEvent.department)
  const visibleEvent = event && canView(role, event.department)
  const routes: [Department, Department][] = [
    ['shop', 'it'],
    ['it', 'hq'],
    ['hq', 'warehouse'],
    ['warehouse', 'bank'],
    ['it', 'bank'],
    ['bank', 'post'],
    ['bank', 'shop'],
  ]
  const route = routes[view.step]
  const handoff =
    route && canView(role, route[0]) && canView(role, route[1])
      ? { from: route[0], to: route[1] }
      : undefined
  const current = view.facts.filter((f) => f.department === selected)
  const actorName =
    event?.actor === 'ai' ? 'AI' : view.people.find((p) => p.id === event?.actor)?.name[language]
  useEffect(() => {
    document.documentElement.lang = language
  }, [language])
  useEffect(() => {
    if (reduced) setMotionEnabled(false)
  }, [reduced])
  useEffect(() => {
    if (!playing) return
    if (step >= demoSteps.length - 1) {
      setPlaying(false)
      return
    }
    const timer = window.setTimeout(
      () => setStep((s) => Math.min(s + 1, demoSteps.length - 1)),
      5000,
    )
    return () => window.clearTimeout(timer)
  }, [playing, step])
  useEffect(() => {
    const onEscape = (e: KeyboardEvent) => {
      if (e.key === 'Escape') select(undefined)
    }
    window.addEventListener('keydown', onEscape)
    return () => window.removeEventListener('keydown', onEscape)
  }, [])
  const move = (index: number) => {
    setPlaying(false)
    setStep(index)
    setNotice(false)
  }
  const actionNames: Record<DemoAction, string> = {
    draft: t('เตรียมร่างและส่งต่อ', 'Prepare draft and hand off'),
    approve: t('ตรวจและยืนยัน', 'Review and confirm'),
    payment: t('ตรวจหลักฐานและรับเงิน', 'Check evidence and record payment'),
  }
  const complete = () => {
    if (!nextEvent || !canComplete) return
    move(step + 1)
    select(nextEvent.department)
    setNotice(true)
  }
  return (
    <div className="app demo-app">
      <a className="skip-link" href="#demo-controls">
        {t('ข้ามไปส่วนควบคุม', 'Skip to controls')}
      </a>
      <header className="masthead">
        <div className="brand">
          <span className="brand-mark" aria-hidden="true">
            ◈
          </span>
          <div>
            <strong>ERP Town</strong>
            <span className="brand-subtitle">
              {t('ลองทำงานร่วมกันในเมือง', 'A working day in the town')}
            </span>
          </div>
        </div>
        <div className="header-actions">
          <fieldset
            className="demo-workspace-switch"
            aria-label={t('หน้าจอทำงาน', 'Workspace view')}
          >
            <button
              type="button"
              aria-pressed={workspace === 'town'}
              onClick={() => setWorkspace('town')}
            >
              {t('เมือง 3D', '3D town')}
            </button>
            <button
              type="button"
              aria-pressed={workspace === 'erp'}
              onClick={() => setWorkspace('erp')}
            >
              {t('ระบบ ERP', 'ERP workspace')}
            </button>
          </fieldset>
          <fieldset className="language-switch" aria-label={t('ภาษา', 'Language')}>
            <button type="button" aria-pressed={th} onClick={() => setLanguage('th')}>
              ไทย
            </button>
            <button type="button" aria-pressed={!th} onClick={() => setLanguage('en')}>
              EN
            </button>
          </fieldset>
          <a
            className="classic"
            href={
              staticDemo
                ? 'https://github.com/atipongsena/erp-town#full-stack-local-demo'
                : `/?lang=${language}`
            }
          >
            {staticDemo ? t('วิธีเปิดระบบจริง', 'Run full stack') : t('กลับระบบจริง', 'Live workspace')} ↗
          </a>
        </div>
      </header>
      <div className="demo-banner">
        <strong>{t('โหมดจำลอง', 'DEMO')}</strong>
        <span>
          {t(
            'ข้อมูลตัวอย่างทั้งหมด · ไม่มีการส่งรายการเข้า ERP หรือเรียกโมเดล AI จริง',
            'Sample data only · No ERP transactions or live AI calls',
          )}
        </span>
      </div>
      <main className="workspace">
        {workspace === 'erp' ? (
          <DemoErpDesk
            language={language}
            role={role}
            view={view}
            onCompleteExample={() => move(6)}
            onShowTown={(department) => {
              select(department)
              setWorkspace('town')
            }}
          />
        ) : (
          <section className="town-view" aria-label={t('เมืองจำลอง', 'Demo town')}>
            <div className="scene-container">
              <Suspense fallback={null}>
                <Scene
                  sceneTime={sceneTime}
                  language={language}
                  selected={selected}
                  reduced={reduced}
                  economy={economy}
                  motionEnabled={motionEnabled}
                  workActors={presentation.looping ? presentation.actors : undefined}
                  workSimulation={presentation.looping ? presentation.work : undefined}
                  deliveryTrips={presentation.trips}
                  deliverySimulation={presentation.delivery}
                  onSelectTrip={() => select('warehouse')}
                  handoff={handoff}
                  onSelect={(d) => {
                    select(d)
                    setNotice(false)
                  }}
                  onError={() => setAssetError(true)}
                  onReady={() => setReady(true)}
                  presences={view.people
                    .toSorted(
                      (a, b) => Number(b.id === event?.actor) - Number(a.id === event?.actor),
                    )
                    .map((p) => ({
                      id: p.id,
                      department: p.department,
                      appearance: p.appearance,
                      emote: event?.actor === p.id ? 'wave' : 'idle',
                    }))}
                  aiCount={canView(role, 'it') && (step === 1 || step === 4) ? 1 : 0}
                />
              </Suspense>
            </div>
            <div className="scene-caption">
              <span className="live-dot historical" /> DEMO COMPANY · {roleNames[role][language]}
            </div>
            <div className="demo-scene-story" aria-live="polite">
              <small>
                {t('ฉาก', 'Scene')} {step + 1} / {demoSteps.length}
              </small>
              <strong>
                {visibleEvent
                  ? event.title[language]
                  : t('งานในแผนกอื่น', 'Work in another department')}
              </strong>
              <span>
                {visibleEvent
                  ? event.detail[language]
                  : t(
                      'บทบาทนี้ดูรายละเอียดส่วนนี้ไม่ได้ สลับเป็นผู้ชมเพื่อดูภาพรวม',
                      'This role cannot view these details. Switch to Observer for the full picture.',
                    )}
              </span>
            </div>
            <div className="scene-tools">
              <EnvironmentControl language={language} />
              <label className="demo-motion">
                <input
                  type="checkbox"
                  checked={motionEnabled}
                  onChange={(e) => setMotionEnabled(e.target.checked)}
                />
                {t('ภาพเคลื่อนไหวในเมือง', 'Town animation')}
              </label>
              <button type="button" onClick={() => select(undefined)}>
                {copy[language].overview} ⌂
              </button>
              <label>
                {copy[language].quality}
                <select
                  value={economy ? 'low' : 'standard'}
                  onChange={(e) => setEconomy(e.target.value === 'low')}
                >
                  <option value="standard">{copy[language].standard}</option>
                  <option value="low">{copy[language].low}</option>
                </select>
              </label>
            </div>
            <div className="scene-footer">
              <span>
                {assetError
                  ? copy[language].assetError
                  : ready
                    ? t(
                        'เลือกอาคารเพื่อดูงาน · ตัวละครในภาพเป็นตัวอย่าง',
                        'Select a building to see its work · Characters are illustrative',
                      )
                    : copy[language].loading}
              </span>
            </div>
          </section>
        )}
        <aside className="sidebar">
          <section id="demo-controls" className="demo-controls">
            <span className="eyebrow">A DAY AT DEMO COMPANY</span>
            <h1>{t('คนกับ AI ทำงานด้วยกัน', 'People and AI at work')}</h1>
            <p>{t('ออเดอร์เดียว ตั้งแต่ขายจนรับเงิน', 'One order, from sale to payment')}</p>
            {presentation.looping && (
              <p>
                {t(
                  'ภาพสาธิตหลายทีมทำงานพร้อมกัน · ไม่สร้างรายการ ERP',
                  'Concurrent demo motion · no ERP writes',
                )}
              </p>
            )}
            <p className="demo-fineprint">
              {t(
                'คนเดินและ AI เคลื่อนไหวในเมือง เอกสารลอยแสดงการส่งงาน ปุ่มหยุดเรื่องราวหยุดเฉพาะการเปลี่ยนฉาก',
                'People walk, AI moves, and a floating document shows each handoff. Pause stops scene changes; Town animation controls motion.',
              )}
            </p>
            {reduced && !motionEnabled && (
              <p>
                {t(
                  'เครื่องตั้งค่าลดการเคลื่อนไหวอยู่ เปิดภาพเคลื่อนไหวในเมืองได้ตามต้องการ',
                  'Reduced motion is enabled on this device. You can turn on Town animation if you wish.',
                )}
              </p>
            )}
            <label>
              {t('ดูในบทบาท', 'View as')}
              <select
                value={role}
                onChange={(e) => {
                  setRole(e.target.value as DemoRole)
                  setNotice(false)
                }}
              >
                {(Object.keys(roleNames) as DemoRole[]).map((r) => (
                  <option key={r} value={r}>
                    {roleNames[r][language]}
                  </option>
                ))}
              </select>
            </label>
            <div className="demo-playback">
              <button
                type="button"
                onClick={() => {
                  if (step === 6) setStep(0)
                  setPlaying((p) => !p)
                }}
              >
                {playing ? t('หยุดชั่วคราว', 'Pause') : t('เล่นเรื่องราว', 'Play story')}
              </button>
              <button type="button" disabled={step === 0} onClick={() => move(step - 1)}>
                {t('ก่อนหน้า', 'Back')}
              </button>
              <button type="button" disabled={step === 6} onClick={() => move(step + 1)}>
                {t('ฉากถัดไป', 'Next scene')}
              </button>
              <button type="button" onClick={() => move(0)}>
                {t('เริ่มใหม่', 'Reset')}
              </button>
            </div>
            <progress
              aria-label={t('ความคืบหน้าตัวอย่าง', 'Story progress')}
              value={step + 1}
              max={7}
            />
            <p className="demo-fineprint">
              {t(
                'ปุ่มเล่นเปลี่ยนฉากตัวอย่างเท่านั้น ไม่ได้ให้สิทธิทำรายการแก่บทบาทที่เลือก',
                'Playback changes the sample scene; it does not grant transaction permissions to this role.',
              )}
            </p>
          </section>
          {tab === 'work' && (
            <section className="demo-next-task" aria-label={t('ทดลองทำงาน', 'Try a task')}>
              <span className="eyebrow">{t('งานถัดไป', 'NEXT HANDOFF')}</span>
              <h2>
                {nextEvent && canView(role, nextEvent.department)
                  ? nextEvent.title[language]
                  : nextEvent
                    ? t('งานในแผนกอื่น', 'Work in another department')
                    : t('งานนี้เสร็จแล้ว', 'This order is complete')}
              </h2>
              {nextEvent?.action && (
                <button type="button" disabled={!canComplete} onClick={complete}>
                  {actionNames[nextEvent.action]}
                </button>
              )}
              {nextEvent && !canComplete && (
                <p>
                  {t(
                    'บทบาทนี้ทำขั้นตอนนี้ไม่ได้ ลองสลับบทบาท หรือใช้ฉากถัดไปเพื่อดูตัวอย่าง',
                    'This role cannot perform this step. Switch roles, or use Next scene to preview it.',
                  )}
                </p>
              )}
              {notice && (
                <p role="status">
                  {t(
                    'ทำงานจำลองสำเร็จและส่งต่อแล้ว ไม่มีรายการใน ERP จริง',
                    'Demo task completed and handed off. No live ERP transaction was created.',
                  )}
                </p>
              )}
            </section>
          )}
          <fieldset className="demo-tabs" aria-label={t('มุมมอง', 'View')}>
            <button type="button" aria-pressed={tab === 'work'} onClick={() => setTab('work')}>
              {t('งานในเมือง', 'Town work')}
            </button>
            <button type="button" aria-pressed={tab === 'rights'} onClick={() => setTab('rights')}>
              {t('สิทธิแต่ละส่วน', 'Access & permissions')}
            </button>
          </fieldset>
          {tab === 'rights' ? (
            <section className="demo-rights">
              <h2>{t('สิทธิของบทบาทนี้', 'This role’s permissions')}</h2>
              <p>
                {t(
                  'ตัวอย่างนโยบายสำหรับ mockup สิทธิจริงขึ้นกับบัญชี บริษัท และเอกสารใน ERP',
                  'Illustrative mockup policy. Live access depends on the ERP account, company and document.',
                )}
              </p>
              <div className="demo-table-scroll">
                <table>
                  <caption>{roleNames[role][language]}</caption>
                  <thead>
                    <tr>
                      <th>{t('แผนก', 'Department')}</th>
                      <th>{t('ดู', 'View')}</th>
                      <th>{t('ร่าง', 'Draft')}</th>
                      <th>{t('ยืนยัน', 'Confirm')}</th>
                      <th>{t('รับเงิน', 'Payment')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {demoDepartments.map((d) => (
                      <tr key={d}>
                        <th>{departmentNames[language][d]}</th>
                        {[
                          canView(role, d),
                          canAct(role, 'draft', d),
                          canAct(role, 'approve', d),
                          canAct(role, 'payment', d),
                        ].map((allowed, index) => (
                          <td key={['view', 'draft', 'approve', 'payment'][index]}>
                            {allowed ? t('ได้', 'Yes') : '—'}
                          </td>
                        ))}
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              <p>
                {permissions[role].amounts
                  ? t(
                      'ดูยอดเงินและภาษีได้ในเอกสารของแผนกที่มีสิทธิ',
                      'Can see amounts and tax in accessible departments.',
                    )
                  : t(
                      'ซ่อนราคา ภาษี และยอดเงิน เห็นเฉพาะข้อมูลที่ใช้จัดส่ง',
                      'Prices, tax and totals are hidden; only dispatch information is shown.',
                    )}
              </p>
              <div className="demo-rule">
                {t(
                  'AI เตรียมร่างและตรวจข้อมูลได้ แต่ต้องส่งให้คนยืนยันเอกสารและรับเงิน',
                  'AI can check data and prepare drafts. People confirm documents and record payments.',
                )}
              </div>
            </section>
          ) : (
            <>
              <nav className="department-list" aria-label={copy[language].departments}>
                {demoDepartments.map((d) => (
                  <button
                    type="button"
                    key={d}
                    className={`department ${selected === d ? 'selected' : ''}`}
                    style={{ '--department': tokens.dept[d].base } as React.CSSProperties}
                    aria-pressed={selected === d}
                    onClick={() => {
                      select(selected === d ? undefined : d)
                      setNotice(false)
                    }}
                  >
                    <span className="department-symbol" aria-hidden="true">
                      {canView(role, d)
                        ? { shop: '▤', warehouse: '▣', bank: '◈', hq: '♜', it: '⌘', post: '✉' }[d]
                        : '×'}
                    </span>
                    <span className="department-text">
                      <strong>{departmentNames[language][d]}</strong>
                      <small>
                        {canView(role, d)
                          ? copy[language].description[d]
                          : t('ไม่มีสิทธิดู', 'No access')}
                      </small>
                    </span>
                    <span className="department-count">
                      {canView(role, d) ? view.facts.filter((f) => f.department === d).length : '—'}
                    </span>
                  </button>
                ))}
              </nav>
              {selected && (
                <section className="inspection">
                  <div className="inspection-title">
                    <h2>{departmentNames[language][selected]}</h2>
                    <button
                      type="button"
                      aria-label={copy[language].closeRoof}
                      onClick={() => select(undefined)}
                    >
                      ×
                    </button>
                  </div>
                  {!canView(role, selected) ? (
                    <p className="demo-rule">
                      {t(
                        'บทบาทนี้ไม่มีสิทธิดูข้อมูลในแผนกนี้',
                        'This role cannot view this department’s data.',
                      )}
                    </p>
                  ) : (
                    <>
                      <ul className="fact-list">
                        {current.map((f) => (
                          <li key={f.id}>
                            <div>
                              <strong>{f.name}</strong>
                              <small>{f.doctype} · DEMO</small>
                              <span className={`status ${f.status}`}>
                                {statusLabel(language, f.status)}
                              </span>
                            </div>
                          </li>
                        ))}
                      </ul>
                      {current.length === 0 && (
                        <p>{t('ยังไม่มีงานในฉากนี้', 'No work here in this scene.')}</p>
                      )}
                      {['shop', 'warehouse', 'bank'].includes(selected) && (
                        <div className="demo-document">
                          <strong>
                            {t('ร้านบ้านสวน · สมุด 20 เล่ม', 'Garden Shop · 20 notebooks')}
                          </strong>
                          {permissions[role].amounts && selected !== 'warehouse' ? (
                            <dl>
                              <div>
                                <dt>{t('ราคาต่อเล่ม', 'Unit price')}</dt>
                                <dd>฿100.00</dd>
                              </div>
                              <div>
                                <dt>{t('ค่าสินค้า', 'Subtotal')}</dt>
                                <dd>฿2,000.00</dd>
                              </div>
                              <div>
                                <dt>{t('VAT 7% (ตัวอย่าง)', 'VAT 7% (sample)')}</dt>
                                <dd>฿140.00</dd>
                              </div>
                              <div>
                                <dt>{t('ยอดรวม', 'Total')}</dt>
                                <dd>฿2,140.00</dd>
                              </div>
                            </dl>
                          ) : (
                            <p>
                              {t(
                                'จำนวน 20 เล่ม · ข้อมูลสำหรับจัดส่ง',
                                'Quantity: 20 · Dispatch information',
                              )}
                            </p>
                          )}
                        </div>
                      )}
                      {selected === 'it' && (
                        <div className="demo-rule">
                          {t(
                            'AI รอรับงาน → เตรียมร่าง → รอคนตรวจ → เสร็จงาน · ไม่มีการเรียกโมเดลจริง',
                            'AI: queued → drafting → human review → complete. No live model calls.',
                          )}
                        </div>
                      )}
                      {selected === 'post' && (
                        <p>
                          {t(
                            'แจ้งส่งของหลังฉากที่ 4 และแจ้งปิดงานหลังฉากที่ 7',
                            'Dispatch notice after scene 4; completion notice after scene 7.',
                          )}
                        </p>
                      )}
                    </>
                  )}
                </section>
              )}
              <section className="demo-team">
                <h2>{t('ใครกำลังทำอะไร', 'Who is doing what')}</h2>
                <ul>
                  {view.people.map((p) => (
                    <li key={p.id}>
                      <span className={`demo-person ${p.appearance}`}>
                        {p.name[language].slice(0, 1)}
                      </span>
                      <div>
                        <strong>
                          {p.name[language]} · {p.role[language]}
                        </strong>
                        <small>
                          {event?.actor === p.id
                            ? t('กำลังทำงานในฉากนี้', 'Active in this scene')
                            : demoSteps.slice(0, step + 1).some((e) => e.actor === p.id)
                              ? t('ส่งงานต่อแล้ว', 'Handed off')
                              : t('รอรับงาน', 'Waiting for handoff')}
                        </small>
                      </div>
                    </li>
                  ))}
                  {canView(role, 'it') && (
                    <li>
                      <span className="demo-person demo-ai">AI</span>
                      <div>
                        <strong>{t('AI ผู้ช่วยเอกสาร', 'AI document assistant')}</strong>
                        <small>
                          {step === 1 || step === 4
                            ? t('กำลังเตรียมร่าง', 'Preparing a draft')
                            : step >= 5
                              ? t('ส่งงานให้คนตรวจแล้ว', 'Handed to a person')
                              : t('รอรับงาน', 'Waiting for work')}
                        </small>
                      </div>
                    </li>
                  )}
                </ul>
                <p className="demo-fineprint">
                  {t(
                    'รายชื่อและสถานะเป็นตัวอย่าง ผู้ชมเห็นภาพหลายทีมวนซ้ำ บทบาทอื่นเห็นภาพส่งงานตามเรื่องราว',
                    'Names and statuses are fictional. Observer shows concurrent loops; other roles show illustrative story handoffs.',
                  )}
                </p>
              </section>
              <section className="demo-timeline">
                <h2>{t('งานที่ผ่านมาของบทบาทนี้', 'Work visible to this role')}</h2>
                <ol>
                  {view.events.map((e) => (
                    <li key={e.title.en}>
                      <strong>{e.title[language]}</strong>
                      <p>{e.detail[language]}</p>
                    </li>
                  ))}
                </ol>
              </section>
            </>
          )}
          <p className="demo-fineprint">
            {t(
              'เปลี่ยนบทบาทแล้วข้อมูลและสิทธิใน mockup เปลี่ยนตามทันที กลับระบบจริงเพื่อดูสิทธิ ERP ของบัญชีที่เข้าสู่ระบบ',
              'Switching roles updates mockup data and permissions immediately. Return to the live workspace for your actual ERP access.',
            )}
          </p>
          {visibleEvent && (
            <span className="demo-actor">
              {t('ผู้ทำงานในฉากนี้', 'Current actor')}: {actorName ?? 'AI'}
            </span>
          )}
        </aside>
      </main>
    </div>
  )
}
