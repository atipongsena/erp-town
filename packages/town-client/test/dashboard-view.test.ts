import { describe, expect, it } from 'vitest'
import { dashboardPresentation, dashboardView, selectionVisible } from '../src/dashboard-view.ts'
import type { DisplayFact } from '../src/world-view.ts'

const fact = (
  id: string,
  status: DisplayFact['status'] = 'draft',
  kind = 'document',
): DisplayFact => ({
  id,
  name: id,
  doctype: 'Sales Order',
  department: 'shop',
  scope: '',
  status,
  kind,
})
const input = {
  facts: [
    fact('order'),
    fact('risk', 'overdue', 'overdue'),
    fact('agent', 'ai-running', 'agent_run'),
  ],
  phase: 'live',
  replaying: false,
  company: 'A',
  version: 9007199254740993n,
  work: { actors: [], overflow: 0, reservations: [] },
}
describe('one authorized dashboard source', () => {
  it('retains immutable facts and queues when only presentation progress changes', () => {
    const base = dashboardView(input)
    const next = dashboardPresentation(
      base,
      { actors: [], reservations: ['crossing'], overflow: 2 },
      { trips: [], overflow: 1 },
    )
    expect(next.facts).toBe(base.facts)
    expect(next.queues).toBe(base.queues)
    expect(next.work.overflow).toBe(2)
    expect(next.delivery.overflow).toBe(1)
  })
  it('counts visible documents independently from signals and AI work', () => {
    const view = dashboardView(input)
    expect(view.documents).toBe(1)
    expect(view.signals).toBe(1)
    expect(view.aiRuns).toBe(1)
    expect(view.working).toBe(0)
    expect(view.queues[0]?.total).toBe(1)
    expect(view.version).toBe(9007199254740993n)
  })
  it('orders observed risk first and uses unavailable rather than invented zero', () => {
    expect(dashboardView(input).facts[0]?.id).toBe('risk')
    const empty = dashboardView({ ...input, phase: 'offline' })
    expect(empty.documents).toBeUndefined()
    expect(empty.facts).toEqual([])
    expect(dashboardView({ ...input, facts: [] }).documents).toBe(0)
  })
  it('drops actor activity and current links in historical mode and invalid selections on permission loss', () => {
    const view = dashboardView({ ...input, replaying: true })
    expect(view.source).toBe('historical')
    expect(view.work.actors).toEqual([])
    expect(
      selectionVisible({ type: 'document', doctype: 'Sales Order', name: 'order' }, view),
    ).toBe(true)
    expect(
      selectionVisible({ type: 'document', doctype: 'Sales Order', name: 'missing' }, view),
    ).toBe(false)
    expect(selectionVisible({ type: 'actor', id: 'someone' }, view)).toBe(false)
  })
})
