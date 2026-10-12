import { describe, expect, it } from 'vitest'
import { dashboardView } from '../src/dashboard-view.ts'
import { departmentScreenContent } from '../src/screen-content.ts'

const base = {
  facts: [],
  phase: 'live',
  replaying: false,
  company: 'A',
  version: 1n,
  work: { actors: [], overflow: 0, reservations: [] },
}
describe('department monitor shares HUD evidence', () => {
  it('agrees on zero, unavailable and historical source', () => {
    expect(departmentScreenContent(dashboardView(base), 'shop', 'en').lines).toContain(
      'Sales Order: 0',
    )
    expect(
      departmentScreenContent(dashboardView({ ...base, phase: 'offline' }), 'shop', 'en').lines,
    ).toContain('Sales Order: —')
    expect(
      departmentScreenContent(dashboardView({ ...base, replaying: true }), 'shop', 'th').source,
    ).toBe('historical')
  })
  it('clears previous scope rather than retaining private screen content', () => {
    const before = departmentScreenContent(dashboardView(base), 'shop', 'en')
    const after = departmentScreenContent(
      dashboardView({ ...base, company: '', phase: 'idle' }),
      'shop',
      'en',
    )
    expect(after.scope).not.toBe(before.scope)
    expect(after.company).toBe('')
    expect(after.lines).not.toContain('A')
  })
})
