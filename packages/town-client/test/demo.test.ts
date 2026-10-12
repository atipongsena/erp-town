import { describe, expect, it } from 'vitest'
import { canView, type DemoRole, demoSnapshot, demoSteps, permissions } from '../src/demo.ts'

describe('isolated workflow demo', () => {
  it('shows all six departments to the observer without granting workflow actions', () => {
    for (const department of ['shop', 'warehouse', 'bank', 'hq', 'it', 'post'] as const) {
      expect(canView('observer', department)).toBe(true)
    }
    expect(permissions.observer.draft).toBe(false)
    expect(permissions.observer.approve).toBe(false)
    expect(permissions.observer.payment).toBe(false)
    expect(permissions.ai.approve).toBe(false)
    expect(permissions.ai.payment).toBe(false)
  })

  it('filters facts, people, events and amounts when roles change at any stage', () => {
    for (const role of Object.keys(permissions) as DemoRole[]) {
      for (let step = 0; step < demoSteps.length; step++) {
        const view = demoSnapshot(step, role)
        expect(view.facts.every((f) => canView(role, f.department))).toBe(true)
        expect(view.people.every((p) => canView(role, p.department))).toBe(true)
        expect(view.events.every((e) => canView(role, e.department))).toBe(true)
        if (!permissions[role].amounts) expect(view.total).toBeUndefined()
      }
    }
    const warehouse = demoSnapshot(6, 'warehouse')
    expect(warehouse.facts.some((f) => f.doctype === 'Sales Invoice')).toBe(false)
    expect(JSON.stringify(warehouse)).not.toContain('2140')
  })

  it('keeps AI drafts separate from human approval, delivery and payment', () => {
    expect(demoSnapshot(0, 'observer').facts.some((f) => f.doctype === 'Sales Order')).toBe(false)
    expect(demoSnapshot(1, 'observer').facts.find((f) => f.doctype === 'Sales Order')?.status).toBe(
      'draft',
    )
    expect(demoSnapshot(2, 'observer').facts.find((f) => f.doctype === 'Sales Order')?.status).toBe(
      'submitted',
    )
    expect(
      demoSnapshot(6, 'observer').facts.find((f) => f.doctype === 'Payment Entry')?.status,
    ).toBe('submitted')
    expect(demoSteps.filter((s) => s.actor === 'ai').every((s) => s.action === 'draft')).toBe(true)
    expect(demoSteps.filter((s) => s.action === 'payment').every((s) => s.actor !== 'ai')).toBe(
      true,
    )
    expect(demoSnapshot(999, 'observer').step).toBe(6)
    expect(demoSnapshot(-1, 'observer').step).toBe(0)
  })
})
