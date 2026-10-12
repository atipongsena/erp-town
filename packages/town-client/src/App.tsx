import { AnimatePresence, motion, useReducedMotion } from 'motion/react'
import {
  lazy,
  Suspense,
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
} from 'react'
import { AgentRunsPanel } from './AgentRunsPanel.tsx'
import { AvatarPanel } from './AvatarPanel.tsx'
import { AvatarSession } from './avatar.ts'
import { BusinessJourney } from './BusinessJourney.tsx'
import { copy, departmentNames, statusLabel } from './copy.ts'
import { DocumentDesk } from './DocumentDesk.tsx'
import {
  dashboardPresentation,
  dashboardView,
  selectionDepartment,
  selectionVisible,
  type TownSelection,
} from './dashboard-view.ts'
import { documentCopy } from './document-copy.ts'
import { DocumentSession, documentKind } from './documents.ts'
import { EnvironmentControl } from './EnvironmentControl.tsx'
import { HistoryPanel } from './HistoryPanel.tsx'
import { LiveDemoPanel, useLiveDemo } from './LiveDemoPanel.tsx'
import { actorIntents, DeliveryTransitions } from './live-work.ts'
import { TownSession } from './session.ts'
import { TownActivityPanel } from './TownActivityPanel.tsx'
import { TownDashboard } from './TownDashboard.tsx'
import tokens from './tokens.json'
import { useUi } from './ui-store.ts'
import { usePresentation } from './use-presentation.ts'
import { classicPath, type Department, displayFacts } from './world-view.ts'

const Scene = lazy(() => import('./Scene.tsx').then((module) => ({ default: module.Scene })))

const departments: Department[] = ['shop', 'warehouse', 'bank', 'hq', 'it', 'post']
const symbols: Record<Department, string> = {
  shop: '▤',
  warehouse: '▣',
  bank: '◈',
  hq: '♜',
  it: '⌘',
  post: '✉',
}

export function App() {
  const {
    language,
    selected,
    economy,
    sceneTime,
    selection,
    selectObject,
    following,
    follow,
    setLanguage,
    select,
    setEconomy,
  } = useUi()
  const t = copy[language]
  const reduced = useReducedMotion() ?? false
  const [motionEnabled, setMotionEnabled] = useState(!reduced)
  const [selectedTruck, setSelectedTruck] = useState(false)
  useEffect(() => {
    if (reduced) setMotionEnabled(false)
  }, [reduced])
  const session = useMemo(
    () =>
      new TownSession(
        window.location.origin,
        window.fetch.bind(window),
        (url) => new WebSocket(url),
      ),
    [],
  )
  const [view, setView] = useState(() => session.view())
  const avatars = useMemo(() => new AvatarSession(window.fetch.bind(window)), [])
  const [avatarView, setAvatarView] = useState(() => avatars.view())
  useEffect(() => avatars.subscribe(() => setAvatarView(avatars.view())), [avatars])
  const documents = useMemo(() => new DocumentSession(window.fetch.bind(window)), [])
  const documentView = useSyncExternalStore(documents.subscribe, documents.view)
  const workKind = documentView.activity?.kind
  const workDepartment: Department =
    workKind === 'sales-order'
      ? 'shop'
      : workKind === 'delivery-note'
        ? 'warehouse'
        : workKind
          ? 'bank'
          : 'hq'
  const workEmote = documentView.activity
    ? documentView.activity.action === 'read'
      ? 'read'
      : workKind === 'delivery-note'
        ? 'carry'
        : 'type'
    : 'idle'
  useEffect(() => {
    void avatars.scope(
      view.company,
      view.phase === 'live' && view.replay.selected === undefined,
      workDepartment,
      workEmote,
    )
  }, [avatars, view.company, view.phase, view.replay.selected, workDepartment, workEmote])
  useEffect(
    () => () => {
      void avatars.scope('', false, 'hq')
    },
    [avatars],
  )
  useLayoutEffect(() => {
    documents.scope(view.company, view.phase === 'live' && view.replay.selected === undefined)
    return () => documents.scope('', false)
  }, [documents, view.company, view.phase, view.replay.selected])
  const [ready, setReady] = useState(false)
  const [assetError, setAssetError] = useState(false)
  const [stats, setStats] = useState<{ triangles: number; calls: number }>()
  const onReady = useCallback((evidence: { triangles: number; calls: number }) => {
    setStats(evidence)
    setReady(true)
  }, [])
  const onError = useCallback(() => setAssetError(true), [])
  useEffect(() => {
    const unsubscribe = session.subscribe(() => setView(session.view()))
    const url = new URL(window.location.href)
    if (url.searchParams.get('connect') === '1' || url.searchParams.get('liveDemo') === '1') {
      url.searchParams.delete('connect')
      window.history.replaceState(null, '', url)
      void session.start()
    }
    return () => {
      unsubscribe()
      session.disconnect()
    }
  }, [session])
  useEffect(() => {
    document.documentElement.lang = language
  }, [language])
  useEffect(() => {
    const onEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') select(undefined)
    }
    window.addEventListener('keydown', onEscape)
    return () => window.removeEventListener('keydown', onEscape)
  }, [select])
  const replaying = view.replay.selected !== undefined
  const facts = useMemo(
    () => (replaying ? view.replay.facts : displayFacts(view.entities)),
    [replaying, view.replay.facts, view.entities],
  )
  const current = facts.filter((fact) => fact.department === selected)
  const live = view.phase === 'live'
  const demoRequested = new URL(window.location.href).searchParams.get('liveDemo') === '1'
  const demo = useLiveDemo(view.company, demoRequested && live && !replaying)
  const actors = useMemo(
    () =>
      live && !replaying
        ? (demo?.actors ??
          actorIntents(
            facts,
            avatarView.presences,
            avatarView.preference.consent ? undefined : documentView,
          ))
        : [],
    [
      live,
      replaying,
      demo,
      facts,
      avatarView.presences,
      avatarView.preference.consent,
      documentView,
    ],
  )
  const transitions = useMemo(() => new DeliveryTransitions(), [])
  const [trips, setTrips] = useState<{ id: string; name: string }[]>([])
  useEffect(() => {
    const next = transitions.observe(facts, live && !replaying)
    if (!live || replaying) setTrips([])
    else
      setTrips((old) =>
        [
          ...old.filter((trip) =>
            facts.some(
              (f) =>
                f.kind === 'document' &&
                f.doctype === 'Delivery Note' &&
                f.name === trip.name &&
                f.status !== 'cancelled',
            ),
          ),
          ...next,
        ].slice(0, 8),
      )
  }, [facts, live, replaying, transitions])
  const presentation = usePresentation(
    actors,
    trips,
    view.company,
    live && !replaying,
    demo?.motion_mode === 'loop' && demo.phase !== 'failed',
    motionEnabled,
  )
  const businessDashboard = useMemo(
    () =>
      dashboardView({
        facts,
        phase: view.phase,
        replaying,
        company: view.company,
        version: replaying ? view.replay.selected : view.version,
        work: { actors: [], overflow: 0, reservations: [] },
      }),
    [facts, view.phase, view.company, view.version, view.replay.selected, replaying],
  )
  const dashboard = useMemo(
    () =>
      dashboardPresentation(
        businessDashboard,
        presentation.snapshot.work,
        presentation.snapshot.delivery,
      ),
    [businessDashboard, presentation.snapshot],
  )
  const choose = (next: TownSelection | undefined) =>
    selectObject(next, selectionDepartment(next, dashboard))
  useLayoutEffect(() => {
    if (selection?.type !== 'department' && !selectionVisible(selection, dashboard))
      selectObject(undefined)
  }, [selection, dashboard, selectObject])
  const previousScope = useRef({ company: view.company, live })
  useLayoutEffect(() => {
    if (previousScope.current.company !== view.company || (previousScope.current.live && !live))
      select(undefined)
    previousScope.current = { company: view.company, live }
  }, [view.company, live, select])
  const selectedKind = selection?.type === 'document' ? documentKind(selection.doctype) : undefined
  const inspectionTrigger = useRef<HTMLElement | undefined>(undefined)
  const demoActive = demo?.actors.find((a) => a.activity !== 'idle')
  const demoDepartment = demoActive?.activity === 'working' ? demoActive.department : undefined
  const demoRunning = demo !== undefined
  useEffect(() => {
    if (demoRunning && !presentation.looping) select(demoDepartment)
  }, [demoRunning, demoDepartment, select, presentation.looping])
  return (
    <div className="app" data-render-triangles={stats?.triangles} data-render-calls={stats?.calls}>
      <a className="skip-link" href="#department-list">
        {t.departments}
      </a>
      <header className="masthead">
        <div className="brand">
          <span className="brand-mark" aria-hidden="true">
            ◈
          </span>
          <div>
            <strong>ERP Town</strong>
            <span className="brand-subtitle">{t.subtitle}</span>
          </div>
        </div>
        <div className="header-actions">
          <fieldset
            className="language-switch"
            aria-label={language === 'th' ? 'ภาษา' : 'Language'}
          >
            <button
              type="button"
              aria-pressed={language === 'th'}
              onClick={() => setLanguage('th')}
            >
              ไทย
            </button>
            <button
              type="button"
              aria-pressed={language === 'en'}
              onClick={() => setLanguage('en')}
            >
              EN
            </button>
          </fieldset>
          <a className="classic" href="/desk">
            {t.classic} <span aria-hidden="true">↗</span>
          </a>
        </div>
      </header>
      <main className="workspace">
        <section className="town-view" aria-label={language === 'th' ? 'เมือง' : 'Town'}>
          <div className="scene-container">
            <Suspense fallback={null}>
              <Scene
                sceneTime={sceneTime}
                language={language}
                selected={selected}
                reduced={reduced}
                economy={economy}
                motionEnabled={motionEnabled && !replaying}
                workActors={presentation.actors}
                workSimulation={presentation.work}
                deliverySimulation={presentation.delivery}
                deliveryTrips={presentation.trips}
                onTripComplete={(id) => setTrips((old) => old.filter((trip) => trip.id !== id))}
                onSelectTruck={() => setSelectedTruck(true)}
                onSelect={select}
                dashboard={dashboard}
                selection={selection}
                following={following}
                onManualCamera={() => follow(false)}
                onSelectActor={(id) => choose({ type: 'actor', id })}
                onSelectTrip={(id) => choose({ type: 'trip', id })}
                onError={onError}
                onReady={onReady}
                presences={avatarView.presences}
                aiCount={
                  facts.filter((f) => f.kind === 'agent_run' && f.status === 'ai-running').length
                }
              />
            </Suspense>
          </div>
          <TownDashboard
            journey={
              selection?.type === 'document' && selectedKind ? (
                <BusinessJourney
                  root={{ kind: selectedKind, name: selection.name }}
                  view={dashboard}
                  language={language}
                  onSelect={choose}
                />
              ) : undefined
            }
            view={dashboard}
            selection={selection}
            language={language}
            onSelect={choose}
            following={following}
            onFollow={() => follow(!following)}
            onOpen={(doctype, name) => {
              inspectionTrigger.current =
                document.activeElement instanceof HTMLElement ? document.activeElement : undefined
              const kind = documentKind(doctype)
              if (kind && live && !replaying) void documents.open({ kind, name })
            }}
          >
            {live && view.replay.points.length > 0 && (
              <section
                className={`replay-panel ${replaying ? 'replaying' : ''}`}
                aria-label={t.replay.title}
              >
                <label>
                  {t.replay.title}
                  <select
                    value={view.replay.selected?.toString() ?? 'live'}
                    onChange={(event) =>
                      session.selectReplay(
                        event.target.value === 'live' ? undefined : BigInt(event.target.value),
                      )
                    }
                  >
                    <option value="live">{t.replay.live}</option>
                    {view.replay.points.map((point, index) => (
                      <option key={point.version.toString()} value={point.version.toString()}>
                        {index + 1} ·{' '}
                        {new Intl.DateTimeFormat(language === 'th' ? 'th-TH' : 'en-GB', {
                          hour: '2-digit',
                          minute: '2-digit',
                          second: '2-digit',
                        }).format(point.receivedAt)}
                      </option>
                    ))}
                  </select>
                </label>
                <p aria-live="polite">{replaying ? t.replay.readOnly : t.replay.note}</p>
                {replaying && (
                  <button type="button" onClick={() => session.selectReplay(undefined)}>
                    {t.replay.live}
                  </button>
                )}
              </section>
            )}
          </TownDashboard>
          <div className="scene-caption">
            <span className={`live-dot ${live ? (replaying ? 'historical' : 'active') : ''}`} />
            <span>
              {live
                ? `${view.company} · ${replaying ? t.replay.active : t.connection.live}`
                : t.preview}
            </span>
          </div>
          <div className="scene-tools">
            <EnvironmentControl language={language} />
            <label className="town-motion">
              <input
                type="checkbox"
                checked={motionEnabled}
                onChange={(e) => setMotionEnabled(e.target.checked)}
              />
              {language === 'th' ? 'ภาพเคลื่อนไหวในเมือง' : 'Town animation'}
            </label>
            <button type="button" onClick={() => select(undefined)}>
              {t.overview} <span aria-hidden="true">⌂</span>
            </button>
            <label>
              {t.quality}
              <select
                value={economy ? 'low' : 'standard'}
                onChange={(event) => setEconomy(event.target.value === 'low')}
              >
                <option value="standard">{t.standard}</option>
                <option value="low">{t.low}</option>
              </select>
            </label>
          </div>
          <div className="scene-footer">
            <span aria-live="polite">
              {assetError ? t.assetError : ready ? t.roofHint : t.loading}
            </span>
          </div>
        </section>
        <aside className="sidebar" id="town-work-controls">
          <div id="town-dashboard-drawer" />
          <div className="section-heading">
            <span className="eyebrow">WORKSPACE</span>
            <h1>{t.departments}</h1>
            <p>{t.flow}</p>
          </div>
          {demo && <LiveDemoPanel view={demo} language={language} looping={presentation.looping} />}
          <nav id="department-list" aria-label={t.departments} className="department-list">
            {departments.map((department) => (
              <button
                type="button"
                key={department}
                aria-pressed={selected === department}
                className={`department ${selected === department ? 'selected' : ''}`}
                onClick={() => select(selected === department ? undefined : department)}
                style={{ '--department': tokens.dept[department].base } as React.CSSProperties}
              >
                <span className="department-symbol" aria-hidden="true">
                  {symbols[department]}
                </span>
                <span className="department-text">
                  <strong>{departmentNames[language][department]}</strong>
                  <small>{t.description[department]}</small>
                </span>
                <span className="department-count" title={t.signals}>
                  {live ? facts.filter((f) => f.department === department).length : '—'}
                </span>
              </button>
            ))}
          </nav>
          <TownActivityPanel
            view={dashboard}
            language={language}
            facts={facts}
            live={live}
            replaying={replaying}
            actors={actors}
            selectedTruck={selectedTruck}
            onSelectTruck={() => setSelectedTruck(true)}
            onCloseTruck={() => setSelectedTruck(false)}
            onOpenFact={(fact) => {
              if (!live || replaying) return
              select('warehouse')
              void documents.open({ kind: 'delivery-note', name: fact.name })
            }}
          />
          <AnimatePresence mode="wait" initial={false}>
            {selected && (
              <motion.section
                key={selected}
                className="inspection"
                initial={reduced ? false : { opacity: 0, y: 8 }}
                animate={{ opacity: 1, y: 0 }}
                exit={{ opacity: 0 }}
                transition={{ duration: reduced ? 0 : 0.16 }}
              >
                <div className="inspection-title">
                  <h2>{departmentNames[language][selected]}</h2>
                  <button type="button" onClick={() => select(undefined)} aria-label={t.closeRoof}>
                    ×
                  </button>
                </div>
                <span className="eyebrow">{t.signals}</span>
                {!live ? (
                  <p className="empty">{t.waiting}</p>
                ) : current.length === 0 ? (
                  <p className="empty">{t.empty}</p>
                ) : (
                  <ul className="fact-list">
                    {current.map((fact) => {
                      const path = classicPath(fact.doctype, fact.name)
                      return (
                        <li key={fact.id}>
                          <div>
                            <strong>{fact.name}</strong>
                            {fact.aiProgress && (
                              <small className="ai-progress">
                                {language === 'th' ? 'ผลงาน AI' : 'AI progress'} · Lv.{' '}
                                {fact.aiProgress.level} · {fact.aiProgress.xp} XP
                              </small>
                            )}
                            <small>
                              {fact.doctype}
                              {fact.scope ? ` · ${fact.scope}` : ''}
                            </small>
                            <span className={`status ${fact.status}`}>
                              {statusLabel(language, fact.status)}
                            </span>
                          </div>
                          {path && !replaying && (
                            <a href={path} aria-label={`${t.openDocument} ${fact.name}`}>
                              ↗
                            </a>
                          )}
                          {documentKind(fact.doctype) && !replaying && (
                            <button
                              type="button"
                              className="fact-detail-button"
                              aria-label={`${documentCopy[language].view} ${fact.name}`}
                              onClick={() => {
                                const kind = documentKind(fact.doctype)
                                if (kind) {
                                  if (
                                    !selected ||
                                    !['shop', 'warehouse', 'bank'].includes(selected)
                                  ) {
                                    select(
                                      kind === 'sales-order'
                                        ? 'shop'
                                        : kind === 'delivery-note'
                                          ? 'warehouse'
                                          : 'bank',
                                    )
                                  }
                                  void documents.open({ kind, name: fact.name })
                                }
                              }}
                            >
                              {documentCopy[language].view}
                            </button>
                          )}
                        </li>
                      )
                    })}
                  </ul>
                )}
              </motion.section>
            )}
          </AnimatePresence>
          {live && !replaying && selected && ['shop', 'warehouse', 'bank'].includes(selected) && (
            <DocumentDesk
              onClose={() => {
                if (inspectionTrigger.current?.isConnected)
                  inspectionTrigger.current.focus({ preventScroll: true })
              }}
              key={`${view.company}:${selected}`}
              session={documents}
              language={language}
              department={selected}
            />
          )}
          <section className="connection-panel" aria-label="ERP connection">
            <div className="connection-status" role="status">
              <span className={`live-dot ${live ? 'active' : ''}`} />
              {view.error ? t.errors[view.error] : t.connection[view.phase]}
            </div>
            {view.companies.length > 1 && (
              <label>
                {t.company}
                <select
                  value={view.company}
                  onChange={(event) => {
                    void session.start(event.target.value)
                  }}
                >
                  {view.companies.map((company) => (
                    <option key={company}>{company}</option>
                  ))}
                </select>
              </label>
            )}
            <button
              className="connect-button"
              type="button"
              disabled={view.phase === 'loading' || view.phase === 'connecting'}
              onClick={() => {
                if (live || view.phase === 'offline') session.disconnect()
                else void session.start(view.company || undefined)
              }}
            >
              {live || view.phase === 'offline' ? t.disconnect : t.connect}
            </button>
            <p>{t.source}</p>
            {live && view.user && (
              <p className="session-identity">
                {language === 'th' ? 'บัญชีที่ใช้งาน' : 'Signed in as'}: <strong>{view.user}</strong>
                <br />
                {view.roles.includes('System Manager')
                  ? language === 'th'
                    ? 'ผู้ดูแลระบบ ERP'
                    : 'ERP system manager'
                  : language === 'th'
                    ? 'สิทธิ์ตามบทบาทใน ERP'
                    : 'Permissions follow ERP roles'}
              </p>
            )}
            <p>
              {language === 'th'
                ? 'ตัวละครแสดงงานที่กำลังทำในแผนก รถคลิกดูใบส่งของได้ และ AI ใช้ตัวทดสอบในเครื่อง'
                : 'Actors show active department work. Click a truck for delivery notes. AI uses the local test provider.'}
            </p>
            {reduced && !motionEnabled && (
              <p>
                {language === 'th'
                  ? 'เครื่องตั้งค่าลดการเคลื่อนไหวอยู่ เปิดภาพเคลื่อนไหวในเมืองได้ตามต้องการ'
                  : 'Reduced motion is enabled on this device. You can turn on Town animation if you wish.'}
              </p>
            )}
            <p>
              <a className="classic" href={`/?lang=${language}&mode=demo`}>
                {language === 'th' ? 'ลองโหมดจำลองคนและ AI' : 'Try the people and AI demo'} ↗
              </a>
            </p>
            {!live && (
              <a
                className="classic"
                href={`/login?redirect-to=${encodeURIComponent(`/?lang=${language}&connect=1`)}`}
              >
                {t.signIn} <span aria-hidden="true">↗</span>
              </a>
            )}
            <AvatarPanel language={language} session={avatars} view={avatarView} />
            <HistoryPanel language={language} company={view.company} live={live && !replaying} />
            {selected === 'it' && live && !replaying && (
              <AgentRunsPanel
                key={view.company}
                language={language}
                company={view.company}
                live={live && !replaying}
              />
            )}
          </section>
        </aside>
      </main>
    </div>
  )
}
