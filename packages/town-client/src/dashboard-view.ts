import type { DeliverySimulationView } from './delivery-simulation.ts'
import { operationsFlow } from './operations-flow.ts'
import type { WorkSimulationView } from './work-simulation.ts'
import type { Department, DisplayFact } from './world-view.ts'
export type TownSelection =
  | { type: 'department'; department: Department }
  | { type: 'actor'; id: string }
  | { type: 'trip'; id: string }
  | { type: 'document'; doctype: string; name: string }
export type DashboardView = ReturnType<typeof dashboardView>
const rank = (f: DisplayFact) =>
  ['error', 'ai-failed', 'dev-failed'].includes(f.status)
    ? 0
    : ['approval', 'ai-needs_human', 'overdue'].includes(f.status)
      ? 1
      : ['ai-running', 'dev-running', 'draft'].includes(f.status)
        ? 2
        : 3
export function dashboardView(input: {
  facts: DisplayFact[]
  phase: string
  replaying: boolean
  company: string
  version?: bigint
  work: WorkSimulationView
  delivery?: DeliverySimulationView
}) {
  const available = input.phase === 'live' && !!input.company
  const facts = available
    ? [...input.facts].sort((a, b) => rank(a) - rank(b) || a.id.localeCompare(b.id))
    : []
  const work =
    available && !input.replaying ? input.work : { actors: [], overflow: 0, reservations: [] }
  const delivery =
    available && !input.replaying
      ? (input.delivery ?? { trips: [], overflow: 0 })
      : { trips: [], overflow: 0 }
  return {
    company: input.company,
    version: input.version,
    available,
    source: input.replaying
      ? ('historical' as const)
      : available
        ? ('live' as const)
        : ('unavailable' as const),
    phase: input.phase,
    facts,
    work,
    delivery,
    queues: operationsFlow(facts, available),
    documents: available ? facts.filter((f) => f.kind === 'document').length : undefined,
    signals: available
      ? facts.filter((f) => ['approval', 'overdue', 'stock', 'error'].includes(f.kind)).length
      : undefined,
    aiRuns: available
      ? facts.filter((f) => f.kind === 'agent_run' && f.status === 'ai-running').length
      : undefined,
    working:
      available && !input.replaying
        ? work.actors.filter((a) => a.phase === 'working').length
        : undefined,
  }
}
export function selectionVisible(selection: TownSelection | undefined, view: DashboardView) {
  if (!selection) return true
  if (!view.available) return false
  switch (selection.type) {
    case 'department':
      return true
    case 'actor':
      return view.work.actors.some((a) => a.id === selection.id)
    case 'trip':
      return view.delivery.trips.some((t) => t.id === selection.id)
    case 'document':
      return view.facts.some(
        (f) =>
          f.kind === 'document' && f.doctype === selection.doctype && f.name === selection.name,
      )
  }
}
/** Reuse the immutable business projection for each 250-ms presentation tick. */
export function dashboardPresentation(
  base: DashboardView,
  work: WorkSimulationView,
  delivery: DeliverySimulationView,
): DashboardView {
  if (!base.available || base.source === 'historical') return base
  return {
    ...base,
    work,
    delivery,
    working: work.actors.filter((a) => a.phase === 'working').length,
  }
}
export function selectionDepartment(
  selection: TownSelection | undefined,
  view: DashboardView,
): Department | undefined {
  if (!selection) return
  switch (selection.type) {
    case 'department':
      return selection.department
    case 'actor':
      return view.work.actors.find((a) => a.id === selection.id)?.intent.department
    case 'trip':
      return 'warehouse'
    case 'document':
      return view.facts.find((f) => f.doctype === selection.doctype && f.name === selection.name)
        ?.department
  }
}
