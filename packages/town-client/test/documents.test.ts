import { describe, expect, it, vi } from 'vitest'
import { DocumentSession, decodeDocument } from '../src/documents.ts'

const response = (data: unknown, status = 200) => new Response(JSON.stringify(data), { status })
const order = {
  name: 'SO-1',
  company: 'ACME-TH',
  customer: 'CUST-001',
  status: 'Draft',
  transaction_date: '2026-10-11',
  delivery_date: '2026-10-12',
  lines: [
    { item_code: 'ITEM-002', qty: '1', rate: '9007199254740993.25', amount: '9007199254740993.25' },
  ],
  net_total: '9007199254740993.25',
  tax_total: '0',
  grand_total: '9007199254740993.25',
  rounded_total: '9007199254740993.25',
  workflow_state: '',
}
describe('document command boundary', () => {
  it('validates invoice lineage and excludes arbitrary ERP details', () => {
    const ref = { kind: 'sales-invoice' as const, name: 'SI' }
    const invoice = {
      ...order,
      name: 'SI',
      posting_date: null,
      due_date: null,
      outstanding: '0',
      is_return: false,
      against_delivery_notes: ['DN-a', 'DN-b'],
      private_payload: 'hidden',
    }
    expect(decodeDocument(invoice, ref, 'ACME-TH').against_delivery_notes).toEqual(['DN-a', 'DN-b'])
    expect(JSON.stringify(decodeDocument(invoice, ref, 'ACME-TH'))).not.toContain('hidden')
    for (const bad of [[''], [8], ['x'.repeat(141)], 'DN'])
      expect(() =>
        decodeDocument({ ...invoice, against_delivery_notes: bad }, ref, 'ACME-TH'),
      ).toThrow()
    expect(() => decodeDocument(invoice, ref, 'OTHER')).toThrow()
  })
  it('publishes the requested work immediately, keeps read activity during review, and clears on close', async () => {
    let resolve: (value: Response) => void = () => {}
    const fetcher = vi.fn(
      () =>
        new Promise<Response>((done) => {
          resolve = done
        }),
    )
    const session = new DocumentSession(fetcher)
    session.scope('ACME-TH', true)
    expect(session.view().activity).toBeUndefined()
    const pending = session.open({ kind: 'sales-order', name: 'SO-1' })
    expect(session.view()).toMatchObject({
      busy: true,
      activity: { kind: 'sales-order', action: 'read' },
    })
    expect(session.view().document).toBeUndefined()
    resolve(response(order))
    await pending
    expect(session.view()).toMatchObject({
      busy: false,
      activity: { kind: 'sales-order', action: 'read' },
    })
    expect(session.view().document?.name).toBe('SO-1')
    session.close()
    expect(session.view().activity).toBeUndefined()
    expect(session.view().document).toBeUndefined()
  })

  it('form editing is local work and late data from an old company cannot overwrite it', async () => {
    let resolve: (value: Response) => void = () => {}
    const fetcher = vi.fn(
      () =>
        new Promise<Response>((done) => {
          resolve = done
        }),
    )
    const session = new DocumentSession(fetcher)
    session.edit('sales-order')
    expect(session.view().activity).toBeUndefined()
    session.scope('ACME-TH', true)
    session.edit('delivery-note')
    expect(session.view()).toMatchObject({
      busy: false,
      activity: { kind: 'delivery-note', action: 'edit' },
    })
    expect(fetcher).not.toHaveBeenCalled()
    const pending = session.open({ kind: 'sales-order', name: 'SO-1' })
    session.edit('payment-entry')
    expect(session.view().activity).toEqual({ kind: 'sales-order', action: 'read' })
    session.scope('OTHER', true)
    expect(session.view().activity).toBeUndefined()
    session.edit('payment-entry')
    resolve(response(order))
    await pending
    expect(session.view().activity).toEqual({ kind: 'payment-entry', action: 'edit' })
    expect(session.view().document).toBeUndefined()
    session.scope('', false)
    expect(session.view().activity).toBeUndefined()
  })

  it('create and submit show their actual action while pending, then return to document review', async () => {
    let finishCreate: (value: Response) => void = () => {}
    let finishSubmit: (value: Response) => void = () => {}
    const fetcher = vi
      .fn()
      .mockImplementationOnce(
        () =>
          new Promise<Response>((done) => {
            finishCreate = done
          }),
      )
      .mockResolvedValueOnce(response(order))
      .mockImplementationOnce(
        () =>
          new Promise<Response>((done) => {
            finishSubmit = done
          }),
      )
      .mockResolvedValueOnce(response({ ...order, status: 'Submitted' }))
    const session = new DocumentSession(fetcher)
    session.scope('ACME-TH', true)
    session.edit('sales-order')
    const create = session.create('sales-order', { customer: 'CUST-001' })
    expect(session.view()).toMatchObject({
      busy: true,
      activity: { kind: 'sales-order', action: 'create' },
    })
    finishCreate(response({ name: 'SO-1' }, 201))
    await create
    expect(session.view().activity).toEqual({ kind: 'sales-order', action: 'read' })
    const submit = session.submit()
    expect(session.view()).toMatchObject({
      busy: true,
      activity: { kind: 'sales-order', action: 'submit' },
    })
    finishSubmit(response({ submitted: true, workflow_state: '' }))
    await submit
    expect(session.view()).toMatchObject({
      busy: false,
      activity: { kind: 'sales-order', action: 'read' },
    })
    expect(session.view().document?.status).toBe('Submitted')
    session.close()
  })

  it('failed reads and writes clear work instead of leaving a person working on inaccessible data', async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(response(order))
      .mockResolvedValueOnce(response({}, 403))
      .mockRejectedValueOnce(new Error('connection lost'))
    const session = new DocumentSession(fetcher)
    session.scope('ACME-TH', true)
    await session.open({ kind: 'sales-order', name: 'SO-1' })
    await session.open({ kind: 'delivery-note', name: 'DN-DENIED' })
    expect(session.view()).toMatchObject({ busy: false, error: 'forbidden' })
    expect(session.view().activity).toBeUndefined()
    session.edit('delivery-note')
    await session.create('delivery-note', { sales_order: 'SO-1' })
    expect(session.view()).toMatchObject({ busy: false, uncertain: true })
    expect(session.view().activity).toBeUndefined()
    session.edit('sales-order')
    expect(session.view().activity).toBeUndefined()
    session.scope('', false)
  })

  it('uses the latest ERP status when approval completes between acknowledgement and read', async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(response(order))
      .mockResolvedValueOnce(response({ submitted: false, workflow_state: 'Pending Approval' }))
      .mockResolvedValueOnce(
        response({ ...order, status: 'Submitted', workflow_state: 'Approved' }),
      )
    const session = new DocumentSession(fetcher)
    session.scope('ACME-TH', true)
    await session.open({ kind: 'sales-order', name: 'SO-1' })
    await session.submit()
    expect(session.view().document?.status).toBe('Submitted')
    expect(session.view().approval).toBe(false)
  })

  it('keeps an interrupted write blocked after reconnect and ignores its late acknowledgement', async () => {
    let resolve: (r: Response) => void = () => {}
    const fetcher = vi.fn(
      () =>
        new Promise<Response>((r) => {
          resolve = r
        }),
    )
    const session = new DocumentSession(fetcher)
    session.scope('ACME-TH', true)
    const pending = session.create('sales-order', {})
    session.scope('OTHER', true)
    expect(session.view().uncertain).toBe(true)
    resolve(response({ name: 'SO-1' }, 201))
    await pending
    expect(session.view().ref).toBeUndefined()
    expect(session.view().document).toBeUndefined()
    await session.create('sales-order', {})
    expect(fetcher).toHaveBeenCalledTimes(1)
    session.scope('', false)
  })

  it('uses session cookies and CSRF header, then reads the actual draft without rounding amounts', async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(response({ name: 'SO-1' }, 201))
      .mockResolvedValueOnce(response(order))
    const session = new DocumentSession(fetcher)
    session.scope('ACME-TH', true)
    await session.create('sales-order', { customer: 'CUST-001' })
    expect(fetcher.mock.calls[0]).toEqual([
      '/town/api/sales-orders',
      expect.objectContaining({
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json', 'X-Town-Request': '1' },
        body: JSON.stringify({ customer: 'CUST-001', company: 'ACME-TH' }),
      }),
    ])
    expect(session.view().document?.rounded_total).toBe('9007199254740993.25')
    expect(session.view().document?.status).toBe('Draft')
  })
  it('blocks duplicate clicks and never retries a write whose result is unknown', async () => {
    let reject: (e: Error) => void = () => {}
    const fetcher = vi.fn(
      () =>
        new Promise<Response>((_r, j) => {
          reject = j
        }),
    )
    const session = new DocumentSession(fetcher)
    session.scope('ACME-TH', true)
    const pending = session.create('sales-order', {})
    await session.create('sales-order', {})
    expect(fetcher).toHaveBeenCalledTimes(1)
    reject(new Error('connection lost'))
    await pending
    expect(session.view().uncertain).toBe(true)
    await session.create('sales-order', {})
    expect(fetcher).toHaveBeenCalledTimes(1)
    session.scope('ACME-TH', false)
    session.scope('ACME-TH', true)
    expect(session.view().uncertain).toBe(true)
    session.acknowledge()
    expect(session.view().uncertain).toBe(false)
  })
  it('clears private details and ignores late responses after company or connection changes', async () => {
    let resolve: (r: Response) => void = () => {}
    const fetcher = vi.fn(
      () =>
        new Promise<Response>((r) => {
          resolve = r
        }),
    )
    const session = new DocumentSession(fetcher)
    session.scope('ACME-TH', true)
    const pending = session.open({ kind: 'sales-order', name: 'SO-1' })
    session.scope('OTHER', true)
    resolve(response(order))
    await pending
    expect(session.view().document).toBeUndefined()
    expect(session.view().ref).toBeUndefined()
  })
  it('rejects a readable document from another company and malformed numeric amounts', async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(response({ ...order, company: 'OTHER' }))
      .mockResolvedValueOnce(response({ ...order, grand_total: 123 }))
    const session = new DocumentSession(fetcher)
    session.scope('ACME-TH', true)
    await session.open({ kind: 'sales-order', name: 'SO-1' })
    expect(session.view().error).toBe('forbidden')
    expect(session.view().document).toBeUndefined()
    await session.open({ kind: 'sales-order', name: 'SO-1' })
    expect(session.view().error).toBe('invalid-response')
  })
  it('retains a known created name when reading the draft fails, without repeating creation', async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(response({ name: 'SO-1' }, 201))
      .mockResolvedValueOnce(response({}, 503))
      .mockResolvedValueOnce(response(order))
    const session = new DocumentSession(fetcher)
    session.scope('ACME-TH', true)
    await session.create('sales-order', {})
    expect(session.view().ref).toEqual({ kind: 'sales-order', name: 'SO-1' })
    expect(session.view().uncertain).toBe(false)
    expect(session.view().error).toBe('unavailable')
    await session.refresh()
    expect(fetcher.mock.calls.filter((c) => c[1]?.method === 'POST')).toHaveLength(1)
    expect(session.view().document?.name).toBe('SO-1')
  })
  it('does not claim submission when ERP advances an approval workflow only', async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(response(order))
      .mockResolvedValueOnce(response({ submitted: false, workflow_state: 'Pending Approval' }))
      .mockResolvedValueOnce(response({ ...order, workflow_state: 'Pending Approval' }))
    const session = new DocumentSession(fetcher)
    session.scope('ACME-TH', true)
    await session.open({ kind: 'sales-order', name: 'SO-1' })
    await session.submit()
    expect(session.view().approval).toBe(true)
    expect(session.view().document?.status).toBe('Draft')
  })
  it('shows fixed permission copy instead of ERP HTML and treats server write failures as uncertain', async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(response({ message: '<script>private error</script>' }, 403))
      .mockResolvedValueOnce(response({}, 503))
    const session = new DocumentSession(fetcher)
    session.scope('ACME-TH', true)
    await session.create('sales-order', {})
    expect(session.view().error).toBe('forbidden')
    expect(session.view().uncertain).toBe(false)
    expect(JSON.stringify(session.view())).not.toContain('private error')
    await session.create('sales-order', {})
    expect(session.view().uncertain).toBe(true)
  })
  it('marks an aborted in-flight write uncertain and bounds an unresponsive request', async () => {
    vi.useFakeTimers()
    const fetcher: typeof fetch = (_url, options) =>
      new Promise((_r, reject) => {
        options?.signal?.addEventListener('abort', () =>
          reject(new DOMException('Aborted', 'AbortError')),
        )
      })
    const session = new DocumentSession(fetcher)
    try {
      session.scope('ACME-TH', true)
      const pending = session.create('sales-order', {})
      await vi.advanceTimersByTimeAsync(45000)
      await pending
      expect(session.view().uncertain).toBe(true)
      expect(session.view().busy).toBe(false)
      expect(vi.getTimerCount()).toBe(0)
    } finally {
      session.scope('', false)
      vi.useRealTimers()
    }
  })
})
