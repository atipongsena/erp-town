import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { OperationsFlow } from '../src/OperationsFlow.tsx'
import { operationsFlow } from '../src/operations-flow.ts'
import type { DisplayFact } from '../src/world-view.ts'

const fact = (overrides: Partial<DisplayFact> = {}): DisplayFact => ({
  id: 'order',
  doctype: 'Sales Order',
  name: 'SO-PRIVATE',
  kind: 'document',
  scope: '',
  department: 'shop',
  status: 'delivery-billing',
  ...overrides,
})

describe('authorized operations flow', () => {
  it('counts document stages without counting approval, stock or overdue signals twice', () => {
    const flow = operationsFlow(
      [
        fact(),
        fact({ id: 'draft', status: 'draft' }),
        fact({ id: 'approval', kind: 'approval' }),
        fact({ id: 'overdue', kind: 'overdue', status: 'overdue' }),
        fact({ id: 'stock', doctype: 'Item', kind: 'stock' }),
        fact({
          id: 'delivery',
          doctype: 'Delivery Note',
          department: 'warehouse',
          status: 'billing',
        }),
      ],
      true,
    )
    expect(flow.map((s) => s.total)).toEqual([2, 1, 0, 0])
    expect(flow[0]?.statuses).toEqual([
      { status: 'draft', count: 1 },
      { status: 'delivery-billing', count: 1 },
    ])
    expect(flow[1]?.statuses).toEqual([{ status: 'billing', count: 1 }])
  })

  it('disconnect hides old private facts and distinguishes unavailable from zero', () => {
    expect(operationsFlow([fact()], false).map((s) => s.total)).toEqual([
      undefined,
      undefined,
      undefined,
      undefined,
    ])
    const html = renderToStaticMarkup(
      <OperationsFlow
        language="en"
        facts={[fact()]}
        available={false}
        replaying={false}
        onSelect={() => {}}
      />,
    )
    expect(html).toContain('Connect to ERP')
    expect(html).not.toContain('SO-PRIVATE')
    expect(html).not.toContain('Awaiting delivery')
    expect(html).not.toContain('No visible documents')
  })

  it('keeps cancelled and unknown states explicit and never invents settlement or arrival', () => {
    const flow = operationsFlow(
      [
        fact({ doctype: 'Payment Entry', status: 'submitted' }),
        fact({ id: 'cancelled', doctype: 'Delivery Note', status: 'cancelled' }),
        fact({ id: 'unknown', doctype: 'Sales Invoice', status: 'unknown' }),
      ],
      true,
    )
    expect(flow[1]?.statuses).toEqual([{ status: 'cancelled', count: 1 }])
    expect(flow[2]?.statuses).toEqual([{ status: 'unknown', count: 1 }])
    expect(flow[3]?.statuses).toEqual([{ status: 'submitted', count: 1 }])
    const html = renderToStaticMarkup(
      <OperationsFlow language="en" facts={[fact()]} available replaying onSelect={() => {}} />,
    )
    expect(html).toContain('Earlier view')
    expect(html).toContain('not a linked order journey')
    expect(html).not.toContain('Delivered')
    expect(html).not.toContain('Paid')
  })

  it('offers all four camera destinations in Thai without exposing document names', () => {
    const html = renderToStaticMarkup(
      <OperationsFlow
        language="th"
        facts={[fact()]}
        available
        replaying={false}
        onSelect={() => {}}
      />,
    )
    expect(html).toContain('ภาพรวมขั้นตอนงาน')
    expect(html).toContain('ใบสั่งขาย')
    expect(html).toContain('ใบส่งของ')
    expect(html).toContain('ใบแจ้งหนี้')
    expect(html).toContain('รับชำระเงิน')
    expect(html).not.toContain('SO-PRIVATE')
  })
})
