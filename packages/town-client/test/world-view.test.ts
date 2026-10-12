import type { Entity, EntityKey } from '@erp-town/world-proto'
import { describe, expect, it } from 'vitest'
import { classicPath, displayFacts } from '../src/world-view.ts'

function fixture(
  fields: Partial<Omit<Entity, 'key'>> & { key?: Partial<Omit<EntityKey, '$typeName'>> } = {},
): Entity {
  const { key, ...rest } = fields
  return {
    $typeName: 'world.v1.Entity',
    version: 0n,
    eventType: '',
    dataJson: new Uint8Array(),
    time: '',
    seq: 0n,
    eventId: '',
    correlationId: '',
    actorId: '',
    ...rest,
    key: key
      ? { $typeName: 'world.v1.EntityKey', doctype: '', name: '', kind: '', scope: '', ...key }
      : undefined,
  }
}

describe('display-only world mapping', () => {
  it('shows real ERP sales and delivery work without inferring invoice payment', () => {
    const make = (doctype: string, status: string) =>
      fixture({
        key: { doctype, name: 'DOC-1', kind: 'document' },
        dataJson: new TextEncoder().encode(JSON.stringify({ status })),
      })
    expect(displayFacts([make('Sales Order', 'To Deliver and Bill')])[0]?.status).toBe(
      'delivery-billing',
    )
    expect(displayFacts([make('Sales Order', 'To Deliver')])[0]?.status).toBe('delivery')
    expect(displayFacts([make('Sales Order', 'To Bill')])[0]?.status).toBe('billing')
    expect(displayFacts([make('Delivery Note', 'To Bill')])[0]?.status).toBe('billing')
    expect(displayFacts([make('Sales Invoice', 'To Bill')])[0]?.status).toBe('unknown')
    expect(displayFacts([make('Sales Invoice', 'Paid')])[0]?.status).toBe('unknown')
  })
  it('maps departments and whitelists status without forwarding financial or actor data', () => {
    const entity = fixture({
      key: { doctype: 'Sales Order', name: 'SO-1', kind: 'document' },
      dataJson: new TextEncoder().encode(
        JSON.stringify({
          status: 'Submitted',
          grand_total: '9999',
          customer: 'private',
          actor: 'private',
        }),
      ),
      actorId: 'user.private',
      version: (1n << 54n) + 1n,
    })
    expect(displayFacts([entity])).toEqual([
      {
        id: JSON.stringify(['Sales Order', 'SO-1', 'document', '']),
        doctype: 'Sales Order',
        name: 'SO-1',
        kind: 'document',
        scope: '',
        department: 'shop',
        status: 'submitted',
      },
    ])
  })
  it('keeps approval facets and different warehouses independent', () => {
    const entities = [
      fixture({ key: { doctype: 'Sales Order', name: 'SO-1', kind: 'approval' } }),
      ...['WH-1', 'WH-2'].map((scope) =>
        fixture({ key: { doctype: 'Item', name: 'ITEM-1', kind: 'stock', scope } }),
      ),
    ]
    const facts = displayFacts(entities)
    expect(facts.map((f) => f.department)).toEqual(['hq', 'warehouse', 'warehouse'])
    expect(new Set(facts.map((f) => f.id)).size).toBe(3)
  })
  it('keeps malformed data visible as an unknown signal and does not invent settlement', () => {
    const entities = [
      fixture({
        key: { doctype: 'Sales Invoice', name: 'SI-1', kind: 'document' },
        eventType: 'erp.payment_entry.submitted',
        dataJson: new Uint8Array([255]),
      }),
    ]
    expect(displayFacts(entities)[0]?.status).toBe('unknown')
    expect(displayFacts(entities)[0]?.department).toBe('bank')
  })
  it('does not infer submitted state from a reconciled event or render arbitrary statuses', () => {
    const make = (status: unknown) =>
      fixture({
        key: { doctype: 'Sales Order', name: 'SO-2', kind: 'document' },
        eventType: 'world.projection.document',
        dataJson: new TextEncoder().encode(JSON.stringify({ status })),
      })
    for (const status of ['Paid', '<script>alert(1)</script>', 100, {}, null])
      expect(displayFacts([make(status)])[0]?.status).toBe('unknown')
    expect(displayFacts([make('Draft')])[0]?.status).toBe('draft')
  })
  it('uses only recognized document routes and encodes path names', () => {
    expect(classicPath('Sales Order', 'SO/1?x')).toBe('/desk/sales-order/SO%2F1%3Fx')
    expect(classicPath('Unknown', 'one')).toBeUndefined()
    expect(displayFacts([fixture()])).toEqual([])
  })
})
