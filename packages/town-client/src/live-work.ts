import type { Presence } from './avatar.ts'
import type { DocumentView } from './documents.ts'
import { navigationLayout } from './work-navigation.ts'
import type { Department, DisplayFact } from './world-view.ts'

export type WorkActor = {
  id: string
  kind: 'human' | 'agent'
  department: Department
  activity: 'idle' | 'travelling' | 'working' | 'reading' | 'typing' | 'carry'
  travel?: { started_at: string; duration_ms: number }
  appearance?: Presence['appearance']
  from_department?: Department
  task_ref?: { doctype: string; name: string }
  simulation?: 'loop'
}
export type LiveDemo = {
  enabled: true
  mode: 'erp-test'
  motion_mode?: 'loop'
  company: string
  layout_version?: string
  stage_started_at?: string
  stage_duration_ms?: number
  round: number
  phase: string
  stage: string
  actors: WorkActor[]
  documents: { doctype: string; name: string; status: string }[]
  error?: string
}
const departments = ['shop', 'warehouse', 'bank', 'hq', 'it', 'post']
const doctypes = ['Sales Order', 'Delivery Note', 'Sales Invoice', 'Payment Entry']
const object = (v: unknown): v is Record<string, unknown> =>
  !!v && typeof v === 'object' && !Array.isArray(v)
const department = (v: unknown): v is Department => typeof v === 'string' && departments.includes(v)
export function decodeLiveDemo(raw: unknown, company: string): LiveDemo | undefined {
  if (
    !object(raw) ||
    raw.enabled !== true ||
    raw.mode !== 'erp-test' ||
    (raw.motion_mode !== undefined && raw.motion_mode !== 'loop') ||
    raw.company !== company ||
    !Number.isSafeInteger(raw.round) ||
    typeof raw.stage !== 'string' ||
    raw.stage.length > 80 ||
    typeof raw.phase !== 'string' ||
    !Array.isArray(raw.actors) ||
    raw.actors.length > 16 ||
    !Array.isArray(raw.documents) ||
    raw.documents.length > 32
  )
    return
  const utc = (v: unknown) =>
    typeof v === 'string' && /^\d{4}-\d{2}-\d{2}T.*Z$/.test(v) && Number.isFinite(Date.parse(v))
  const duration = (v: unknown) =>
    typeof v === 'number' && Number.isSafeInteger(v) && v >= 0 && v <= 120000
  if (
    raw.layout_version !== undefined &&
    (raw.layout_version !== navigationLayout.version ||
      !utc(raw.stage_started_at) ||
      !duration(raw.stage_duration_ms))
  )
    return
  const actors: WorkActor[] = []
  for (const actor of raw.actors) {
    if (
      !object(actor) ||
      Object.keys(actor).some(
        (k) =>
          ![
            'id',
            'kind',
            'department',
            'activity',
            'from_department',
            'task_ref',
            'travel',
          ].includes(k),
      ) ||
      typeof actor.id !== 'string' ||
      !/^[a-zA-Z0-9:_-]{1,80}$/.test(actor.id) ||
      !['human', 'agent'].includes(String(actor.kind)) ||
      !department(actor.department) ||
      !['idle', 'travelling', 'working'].includes(String(actor.activity)) ||
      (actor.from_department !== undefined && !department(actor.from_department))
    )
      return
    const travel = actor.travel
    if (
      travel !== undefined &&
      (raw.layout_version !== navigationLayout.version ||
        !object(travel) ||
        Object.keys(travel).some((k) => !['started_at', 'duration_ms'].includes(k)) ||
        !utc(travel.started_at) ||
        !duration(travel.duration_ms))
    )
      return
    const ref = actor.task_ref
    if (
      ref !== undefined &&
      (!object(ref) ||
        Object.keys(ref).some((k) => !['doctype', 'name'].includes(k)) ||
        !doctypes.includes(String(ref.doctype)) ||
        typeof ref.name !== 'string' ||
        ref.name.length > 140)
    )
      return
    actors.push({
      ...(travel ? { travel: travel as WorkActor['travel'] } : {}),
      id: actor.id,
      kind: actor.kind as WorkActor['kind'],
      department: actor.department,
      activity: actor.activity as WorkActor['activity'],
      ...(actor.from_department ? { from_department: actor.from_department as Department } : {}),
      ...(ref ? { task_ref: ref as WorkActor['task_ref'] } : {}),
    })
  }
  if (new Set(actors.map((a) => a.id)).size !== actors.length) return
  const documents: LiveDemo['documents'] = []
  for (const doc of raw.documents) {
    if (
      !object(doc) ||
      !doctypes.includes(String(doc.doctype)) ||
      typeof doc.name !== 'string' ||
      doc.name.length > 140 ||
      !['Draft', 'Submitted', 'Cancelled'].includes(String(doc.status))
    )
      return
    documents.push({ doctype: String(doc.doctype), name: doc.name, status: String(doc.status) })
  }
  return {
    enabled: true,
    mode: 'erp-test',
    ...(raw.motion_mode === 'loop' ? { motion_mode: 'loop' as const } : {}),
    company,
    ...(raw.layout_version
      ? {
          layout_version: String(raw.layout_version),
          stage_started_at: String(raw.stage_started_at),
          stage_duration_ms: Number(raw.stage_duration_ms),
        }
      : {}),
    round: raw.round as number,
    phase: raw.phase,
    stage: raw.stage,
    actors,
    documents,
    ...(typeof raw.error === 'string' && /^[A-Z_]{1,60}$/.test(raw.error)
      ? { error: raw.error }
      : {}),
  }
}
export function actorIntents(
  facts: DisplayFact[],
  presences: Presence[],
  document?: DocumentView,
): WorkActor[] {
  const people: WorkActor[] = presences.map((p) => ({
    id: `human:${p.id}`,
    kind: 'human',
    department: p.department,
    appearance: p.appearance,
    activity:
      p.emote === 'read'
        ? 'reading'
        : p.emote === 'type'
          ? 'typing'
          : p.emote === 'carry'
            ? 'carry'
            : 'idle',
  }))
  const ai: WorkActor[] = facts
    .filter((f) => f.kind === 'agent_run')
    .slice(-8)
    .map((f) => ({
      id: `agent:${f.name}`,
      kind: 'agent',
      department:
        f.status === 'ai-running' && f.work?.activity === 'working' ? f.work.department : 'it',
      activity: f.status === 'ai-running' && f.work?.activity === 'working' ? 'working' : 'idle',
    }))
  if (document?.activity) {
    const kind = document.activity.kind
    people.push({
      id: 'self',
      kind: 'human',
      from_department: 'hq',
      department: kind === 'sales-order' ? 'shop' : kind === 'delivery-note' ? 'warehouse' : 'bank',
      activity:
        document.activity.action === 'read'
          ? 'reading'
          : kind === 'delivery-note'
            ? 'carry'
            : 'typing',
    })
  }
  return [...people, ...ai]
}

/** A snapshot describes existing ERP state, never a newly dispatched journey. */
export class DeliveryTransitions {
  #ready = false
  #seen = new Map<string, string>()
  #events = new Set<string>()
  observe(facts: DisplayFact[], live: boolean): { id: string; name: string }[] {
    if (!live) {
      this.#ready = false
      this.#seen.clear()
      this.#events.clear()
      return []
    }
    const deliveries = facts.filter((f) => f.kind === 'document' && f.doctype === 'Delivery Note')
    const trips: { id: string; name: string }[] = []
    for (const f of deliveries) {
      const previous = this.#seen.get(f.id)
      if (
        this.#ready &&
        f.eventId &&
        f.eventId !== previous &&
        !this.#events.has(f.eventId) &&
        this.#events.size < 4096 &&
        f.eventType === 'erp.delivery_note.submitted' &&
        ['submitted', 'billing'].includes(f.status)
      )
        trips.push({ id: f.eventId, name: f.name })
      this.#seen.set(f.id, f.eventId ?? '')
      if (f.eventId && this.#events.size < 4096) this.#events.add(f.eventId)
    }
    const visible = new Set(deliveries.map((f) => f.id))
    for (const id of this.#seen.keys()) if (!visible.has(id)) this.#seen.delete(id)
    this.#ready = true
    return trips.slice(-8)
  }
}
