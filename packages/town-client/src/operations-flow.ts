import type { Department, DisplayFact, DisplayStatus } from './world-view.ts'

const stages: { doctype: string; department: Department }[] = [
  { doctype: 'Sales Order', department: 'shop' },
  { doctype: 'Delivery Note', department: 'warehouse' },
  { doctype: 'Sales Invoice', department: 'bank' },
  { doctype: 'Payment Entry', department: 'bank' },
]
const statuses: DisplayStatus[] = [
  'draft',
  'delivery-billing',
  'delivery',
  'billing',
  'submitted',
  'cancelled',
  'unknown',
]

/** Independent visible document queues. No linkage, arrival or settlement is inferred. */
export function operationsFlow(facts: DisplayFact[], available: boolean) {
  return stages.map((stage) => {
    const documents = available
      ? facts.filter((f) => f.kind === 'document' && f.doctype === stage.doctype)
      : []
    const counts = new Map<DisplayStatus, number>()
    for (const document of documents) {
      const status = statuses.includes(document.status) ? document.status : 'unknown'
      counts.set(status, (counts.get(status) ?? 0) + 1)
    }
    return {
      ...stage,
      total: available ? documents.length : undefined,
      statuses: statuses.flatMap((status) => {
        const count = counts.get(status)
        return count ? [{ status, count }] : []
      }),
    }
  })
}
