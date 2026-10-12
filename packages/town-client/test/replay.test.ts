import { describe, expect, it } from 'vitest'
import { ReplayHistory, type ReplayLimits } from '../src/replay.ts'
import type { DisplayFact } from '../src/world-view.ts'

const fact = (status: DisplayFact['status'] = 'draft', name = 'SO-1'): DisplayFact => ({
  id: name,
  name,
  doctype: 'Sales Order',
  kind: 'document',
  scope: '',
  department: 'shop',
  status,
})
const live = (version: bigint, facts = [fact()], company = 'ACME') => ({
  company,
  phase: 'live',
  version,
  facts,
})
describe('session replay privacy and bounds', () => {
  it('requires every retention bound even when called with an incomplete JS configuration', () => {
    for (const config of [{}, { maxPoints: 2 }, { maxPoints: 2, maxBytes: 1000 }])
      expect(() => new ReplayHistory(Date.now, config as ReplayLimits)).toThrow()
  })
  it('keeps earlier statuses without changing current facts and copies both input and output', () => {
    const history = new ReplayHistory(() => 100)
    const input = live(1n)
    history.observe(input)
    const original = input.facts[0]
    if (!original) throw new Error('Missing input fixture')
    original.status = 'cancelled'
    history.observe(live(2n, [fact('submitted')]))
    history.select(1n)
    expect(history.view().facts[0]?.status).toBe('draft')
    const copied = history.view()
    if (!copied.facts[0] || !copied.points[0]) throw new Error('Missing replay fixture')
    copied.facts[0].status = 'cancelled'
    copied.points[0].receivedAt = 999
    expect(history.view().facts[0]?.status).toBe('draft')
    expect(history.view().points[0]?.receivedAt).toBe(100)
    history.select(undefined)
    expect(history.view().facts).toEqual([])
  })
  it('does not record repeated acknowledgments and cannot select an unknown version', () => {
    const history = new ReplayHistory()
    history.observe(live(1n))
    history.observe(live(1n))
    expect(history.view().points).toHaveLength(1)
    history.select(2n)
    expect(history.view().selected).toBeUndefined()
  })
  it.each(['idle', 'connecting', 'loading', 'offline', 'auth-required'])(
    'erases history and selection on %s',
    (phase) => {
      const history = new ReplayHistory()
      history.observe(live(1n))
      history.select(1n)
      history.observe({ ...live(1n), phase })
      expect(history.view()).toEqual({ points: [], selected: undefined, facts: [] })
    },
  )
  it('never carries a history between companies or a regressed replacement snapshot', () => {
    const history = new ReplayHistory()
    history.observe(live(9n))
    history.select(9n)
    history.observe(live(1n, [fact('submitted', 'OTHER')], 'OTHER'))
    expect(history.view().points.map((p) => p.version)).toEqual([1n])
    expect(history.view().selected).toBeUndefined()
    history.observe(live(0n))
    expect(history.view().points.map((p) => p.version)).toEqual([0n])
  })
  it('removes no-longer-visible identities from every old point even on a same-version refresh', () => {
    const history = new ReplayHistory()
    history.observe(live(1n, [fact(), fact('submitted', 'REVOKED')]))
    history.select(1n)
    history.observe(live(1n, [fact('submitted')]))
    expect(history.view().facts.map((f) => f.name)).toEqual(['SO-1'])
    history.observe(live(2n, []))
    expect(history.view().facts).toEqual([])
    history.observe(live(3n, [fact('submitted', 'REVOKED')]))
    expect(history.view().facts).toEqual([])
  })
  it('stores only the approved display fields, never extra financial data or credentials', () => {
    const history = new ReplayHistory()
    history.observe(live(1n, [{ ...fact(), token: 'secret', amount: '999' } as DisplayFact]))
    history.select(1n)
    expect(Object.keys(history.view().facts[0] ?? {}).sort()).toEqual(Object.keys(fact()).sort())
  })
  it('evicts the oldest point at the count bound and returns live if the selected point is evicted', () => {
    const history = new ReplayHistory(() => 100, { maxPoints: 2, maxBytes: 10000, maxAgeMs: 1000 })
    history.observe(live(1n))
    history.select(1n)
    history.observe(live(2n))
    history.observe(live(3n))
    expect(history.view().points.map((p) => p.version)).toEqual([2n, 3n])
    expect(history.view().selected).toBeUndefined()
  })
  it('expires history even when no new frames arrive', () => {
    let now = 100
    const history = new ReplayHistory(() => now, { maxPoints: 2, maxBytes: 10000, maxAgeMs: 1000 })
    history.observe(live(1n))
    history.select(1n)
    now = 1101
    expect(history.view().points).toEqual([])
    expect(history.view().selected).toBeUndefined()
  })
  it('bounds encoded bytes and never retains an oversized point', () => {
    const history = new ReplayHistory(() => 100, { maxPoints: 64, maxBytes: 200, maxAgeMs: 1000 })
    history.observe(live(1n))
    history.observe(live(2n))
    expect(history.view().points.map((p) => p.version)).toEqual([2n])
    history.observe(live(3n, [fact('draft', 'ก'.repeat(200))]))
    expect(history.view().points).toEqual([])
  })
  it.each([0, -1, NaN, Infinity, 1.5])('rejects invalid retention limits %s', (n) => {
    expect(
      () => new ReplayHistory(Date.now, { maxPoints: n, maxBytes: 1000, maxAgeMs: 1000 }),
    ).toThrow()
    expect(
      () => new ReplayHistory(Date.now, { maxPoints: 2, maxBytes: n, maxAgeMs: 1000 }),
    ).toThrow()
    expect(
      () => new ReplayHistory(Date.now, { maxPoints: 2, maxBytes: 1000, maxAgeMs: n }),
    ).toThrow()
  })
})
