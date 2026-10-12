import type { Entity } from '@erp-town/world-proto'

export type Department = 'shop' | 'warehouse' | 'bank' | 'hq' | 'it' | 'post'
export type DisplayStatus =
  | 'draft'
  | 'submitted'
  | 'cancelled'
  | 'delivery-billing'
  | 'delivery'
  | 'billing'
  | 'approval'
  | 'overdue'
  | 'stock'
  | 'error'
  | 'ai-queued'
  | 'ai-running'
  | 'ai-completed'
  | 'ai-needs_human'
  | 'ai-failed'
  | 'ai-cancelled'
  | 'ai-progress'
  | 'dev-started'
  | 'dev-running'
  | 'dev-completed'
  | 'dev-failed'
  | 'unknown'
export type DisplayFact = {
  id: string
  doctype: string
  name: string
  kind: string
  scope: string
  department: Department
  status: DisplayStatus
  aiProgress?: { completed: number; xp: string; level: number }
  eventId?: string
  eventType?: string
  work?: { department: Department; activity: 'working' | 'idle' }
}
const routes: Record<string, string> = {
  'Sales Order': 'sales-order',
  'Delivery Note': 'delivery-note',
  'Sales Invoice': 'sales-invoice',
  'Payment Entry': 'payment-entry',
  Item: 'item',
}
export function classicPath(doctype: string, name: string): string | undefined {
  const route = Object.hasOwn(routes, doctype) ? routes[doctype] : undefined
  return route ? `/desk/${route}/${encodeURIComponent(name)}` : undefined
}
export function displayFacts(entities: Entity[]): DisplayFact[] {
  return entities.flatMap((entity) => {
    const key = entity.key
    if (!key?.doctype || !key.name) return []
    let department: Department
    switch (key.kind) {
      case 'approval':
        department = 'hq'
        break
      case 'stock':
        department = 'warehouse'
        break
      case 'error':
        department = 'it'
        break
      case 'agent_run':
        if (key.doctype !== 'Town Agent Run') return []
        department = 'it'
        break
      case 'agent_progress':
        if (key.doctype !== 'Town Agent') return []
        department = 'it'
        break
      case 'dev_task':
        if (key.doctype !== 'Town Dev Task') return []
        department = 'it'
        break
      default:
        if (key.doctype === 'Sales Order') department = 'shop'
        else if (key.doctype === 'Delivery Note') department = 'warehouse'
        else if (['Sales Invoice', 'Payment Entry'].includes(key.doctype)) department = 'bank'
        else return []
    }
    let status: DisplayStatus = 'unknown'
    let aiProgress: DisplayFact['aiProgress']
    let work: DisplayFact['work']
    if (['approval', 'overdue', 'stock', 'error'].includes(key.kind))
      status = key.kind as DisplayStatus
    else {
      // Read one display field only. Never copy payloads or infer invoice settlement.
      try {
        const data: unknown = JSON.parse(
          new TextDecoder('utf-8', { fatal: true }).decode(entity.dataJson),
        )
        if (data && typeof data === 'object') {
          if (
            key.kind === 'agent_run' &&
            'department' in data &&
            'activity' in data &&
            typeof data.department === 'string' &&
            ['shop', 'warehouse', 'bank'].includes(data.department) &&
            (data.activity === 'working' || data.activity === 'idle')
          )
            work = { department: data.department as Department, activity: data.activity }
          if (
            key.kind === 'dev_task' &&
            'state' in data &&
            typeof data.state === 'string' &&
            ['started', 'running', 'completed', 'failed'].includes(data.state)
          )
            status = `dev-${data.state}` as DisplayStatus
          if (
            key.kind === 'agent_run' &&
            'state' in data &&
            typeof data.state === 'string' &&
            ['queued', 'running', 'completed', 'needs_human', 'failed', 'cancelled'].includes(
              data.state,
            )
          )
            status = `ai-${data.state}` as DisplayStatus
          if (
            key.kind === 'agent_progress' &&
            'successful_runs' in data &&
            typeof data.successful_runs === 'number' &&
            Number.isSafeInteger(data.successful_runs) &&
            data.successful_runs > 0
          ) {
            status = 'ai-progress'
            aiProgress = {
              completed: data.successful_runs,
              xp: (BigInt(data.successful_runs) * 10n).toString(),
              level: 1 + Math.floor(Math.sqrt(data.successful_runs / 5)),
            }
          }
        }
        if (
          key.kind === 'document' &&
          data &&
          typeof data === 'object' &&
          'status' in data &&
          typeof data.status === 'string'
        ) {
          const allowed: Record<string, DisplayStatus> = {
            Draft: 'draft',
            Submitted: 'submitted',
            Cancelled: 'cancelled',
          }
          if (Object.hasOwn(allowed, data.status)) status = allowed[data.status] ?? 'unknown'
          if (key.doctype === 'Sales Order') {
            if (data.status === 'To Deliver and Bill') status = 'delivery-billing'
            else if (data.status === 'To Deliver') status = 'delivery'
            else if (data.status === 'To Bill') status = 'billing'
          } else if (key.doctype === 'Delivery Note' && data.status === 'To Bill')
            status = 'billing'
        }
      } catch {
        /* A malformed display field stays unknown, never becomes a financial fact. */
      }
    }
    return [
      {
        id: JSON.stringify([key.doctype, key.name, key.kind, key.scope]),
        doctype: key.doctype,
        name: key.name,
        kind: key.kind,
        scope: key.scope,
        department,
        status,
        ...(aiProgress ? { aiProgress } : {}),
        ...(entity.eventId ? { eventId: entity.eventId } : {}),
        ...(entity.eventType ? { eventType: entity.eventType } : {}),
        ...(work ? { work } : {}),
      },
    ]
  })
}
