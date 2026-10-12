import { describe, expect, it, vi } from 'vitest'
import { businessJourney, JourneyLoader, readJourneyDocument } from '../src/business-journey.ts'
import type { DocumentRef, TownDocument } from '../src/documents.ts'

const record = (
  kind: TownDocument['kind'],
  name: string,
  extra: Partial<TownDocument> = {},
): TownDocument => ({ kind, name, company: 'A', status: 'Submitted', ...extra })
describe('verified many-to-many business journey', () => {
  it('keeps the selected root inside the node limit even after other records', () => {
    const root = record('sales-order', 'ROOT')
    const view = businessJourney(root, [
      ...Array.from({ length: 40 }, (_, i) => record('sales-order', `SO-${i}`)),
      root,
    ])
    expect(view.nodes[0]?.document.name).toBe('ROOT')
    expect(view.limited).toBe(true)
  })
  it('cancels an oversized response stream before buffering the whole response', async () => {
    let chunks = 0
    const cancel = vi.fn()
    const stream = new ReadableStream({
      pull(c) {
        chunks++
        c.enqueue(new Uint8Array(40000))
        if (chunks > 10) c.close()
      },
      cancel,
    })
    await expect(
      readJourneyDocument(
        { kind: 'sales-order', name: 'SO' },
        'A',
        new AbortController().signal,
        vi.fn(async () => new Response(stream)),
      ),
    ).rejects.toThrow('too large')
    expect(cancel).toHaveBeenCalledOnce()
    expect(chunks).toBeLessThan(5)
  })
  it('joins only referenced records, preserves branches and does not infer settlement from payments', () => {
    const records = [
      record('sales-order', 'SO'),
      record('delivery-note', 'DN-1', { against_sales_orders: ['SO'] }),
      record('delivery-note', 'DN-2', { against_sales_orders: ['SO'] }),
      record('sales-invoice', 'SI', {
        against_delivery_notes: ['DN-1', 'DN-2'],
        outstanding: '10.00',
      }),
      record('payment-entry', 'PE', { against_invoices: ['SI'] }),
    ]
    const view = businessJourney({ kind: 'sales-order', name: 'SO' }, records)
    expect(view.nodes).toHaveLength(5)
    expect(view.edges).toHaveLength(5)
    expect(view.nodes.find((n) => n.document.name === 'SI')?.settled).toBe(false)
    expect(
      businessJourney({ kind: 'sales-invoice', name: 'SI' }, [
        record('sales-invoice', 'SI', { outstanding: '0.00', is_return: false }),
      ]).nodes[0]?.settled,
    ).toBe(true)
  })
  it('does not disclose missing or cross-company references or label returns as settled', () => {
    const view = businessJourney({ kind: 'delivery-note', name: 'DN' }, [
      record('delivery-note', 'DN', { against_sales_orders: ['PRIVATE'] }),
      { ...record('sales-order', 'PRIVATE'), company: 'B' },
    ])
    expect(view.nodes).toHaveLength(1)
    expect(view.edges).toHaveLength(0)
    expect(view.partial).toBe(true)
    expect(JSON.stringify(view)).not.toContain('PRIVATE')
    expect(
      businessJourney({ kind: 'sales-invoice', name: 'RETURN' }, [
        record('sales-invoice', 'RETURN', { is_return: true, outstanding: '0' }),
      ]).nodes[0]?.settled,
    ).toBe(false)
  })
  it('loads only bounded visible candidates with two concurrent reads and drops cancelled responses', async () => {
    let active = 0,
      peak = 0
    const read = vi.fn(async (ref: DocumentRef, signal: AbortSignal) => {
      active++
      peak = Math.max(peak, active)
      await new Promise((r) => setTimeout(r, 2))
      active--
      if (signal.aborted) throw new Error('aborted')
      return record(ref.kind, ref.name)
    })
    const loader = new JourneyLoader(read)
    const refs = Array.from({ length: 40 }, (_, i) => ({
      kind: 'sales-order' as const,
      name: `SO-${i}`,
    }))
    const result = await loader.load({ kind: 'sales-order', name: 'SO-0' }, refs, 'A')
    expect(read).toHaveBeenCalledTimes(32)
    expect(peak).toBe(2)
    expect(result?.limited).toBe(true)
    const pending = loader.load({ kind: 'sales-order', name: 'SO-0' }, refs, 'A')
    loader.cancel()
    expect(await pending).toBeUndefined()
  })
})
