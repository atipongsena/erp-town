export type DocumentKind = 'sales-order' | 'delivery-note' | 'sales-invoice' | 'payment-entry'
export type DocumentRef = { kind: DocumentKind; name: string }
export type DocumentLine = {
  id: string
  item_code: string
  qty: string
  rate: string
  amount: string
}
export type TownDocument = DocumentRef & {
  company: string
  status: 'Draft' | 'Submitted' | 'Cancelled'
  customer?: string
  party?: string
  lines?: DocumentLine[]
  transaction_date?: string | null
  delivery_date?: string | null
  posting_date?: string | null
  due_date?: string | null
  net_total?: string
  tax_total?: string
  grand_total?: string
  rounded_total?: string
  outstanding?: string
  paid?: string
  mode_of_payment?: string
  workflow_state?: string
  against_sales_orders?: string[]
  against_delivery_notes?: string[]
  against_invoices?: string[]
  is_return?: boolean
}
export type DocumentError =
  | 'sign-in'
  | 'forbidden'
  | 'not-found'
  | 'conflict'
  | 'validation'
  | 'unavailable'
  | 'invalid-response'
export type DocumentView = {
  activity?: { kind: DocumentKind; action: 'read' | 'edit' | 'create' | 'submit' }
  busy: boolean
  uncertain: boolean
  error?: DocumentError
  ref?: DocumentRef
  document?: TownDocument
  approval?: boolean
}
export const documentRoutes: Record<DocumentKind, string> = {
  'sales-order': 'sales-orders',
  'delivery-note': 'delivery-notes',
  'sales-invoice': 'sales-invoices',
  'payment-entry': 'payment-entries',
}
export const documentTypes: Record<DocumentKind, string> = {
  'sales-order': 'Sales Order',
  'delivery-note': 'Delivery Note',
  'sales-invoice': 'Sales Invoice',
  'payment-entry': 'Payment Entry',
}
export function documentKind(doctype: string): DocumentKind | undefined {
  return (Object.keys(documentTypes) as DocumentKind[]).find(
    (kind) => documentTypes[kind] === doctype,
  )
}
class DocumentFailure extends Error {
  constructor(
    readonly code: DocumentError,
    readonly uncertain = false,
  ) {
    super(code)
  }
}
const decimal = (value: unknown): value is string =>
  typeof value === 'string' && /^-?[0-9]+(\.[0-9]+)?$/.test(value)
const object = (value: unknown): value is Record<string, unknown> =>
  !!value && typeof value === 'object' && !Array.isArray(value)
const strings = (value: unknown) =>
  Array.isArray(value) && value.every((v) => typeof v === 'string')
export function decodeDocument(value: unknown, ref: DocumentRef, company: string): TownDocument {
  if (!object(value) || value.name !== ref.name || typeof value.company !== 'string')
    throw new DocumentFailure('invalid-response')
  if (value.company !== company) throw new DocumentFailure('forbidden')
  if (!['Draft', 'Submitted', 'Cancelled'].includes(String(value.status)))
    throw new DocumentFailure('invalid-response')
  const amountKeys =
    ref.kind === 'payment-entry'
      ? ['paid']
      : ref.kind === 'delivery-note'
        ? []
        : [
            'net_total',
            'tax_total',
            'grand_total',
            'rounded_total',
            ...(ref.kind === 'sales-invoice' ? ['outstanding'] : []),
          ]
  if (amountKeys.some((key) => !decimal(value[key]))) throw new DocumentFailure('invalid-response')
  const textKeys =
    ref.kind === 'payment-entry'
      ? ['party', 'mode_of_payment']
      : ['customer', ...(ref.kind === 'sales-order' ? ['workflow_state'] : [])]
  if (textKeys.some((key) => typeof value[key] !== 'string'))
    throw new DocumentFailure('invalid-response')
  const dates =
    ref.kind === 'sales-order'
      ? ['transaction_date', 'delivery_date']
      : ['posting_date', ...(ref.kind === 'sales-invoice' ? ['due_date'] : [])]
  if (
    dates.some(
      (key) =>
        value[key] !== null &&
        (typeof value[key] !== 'string' || !/^\d{4}-\d{2}-\d{2}$/.test(value[key])),
    )
  )
    throw new DocumentFailure('invalid-response')
  if (
    ref.kind !== 'payment-entry' &&
    (!Array.isArray(value.lines) ||
      !value.lines.every(
        (line) =>
          object(line) &&
          typeof line.item_code === 'string' &&
          ['qty', 'rate', 'amount'].every((key) => decimal(line[key])),
      ))
  )
    throw new DocumentFailure('invalid-response')
  if (ref.kind === 'delivery-note' && !strings(value.against_sales_orders))
    throw new DocumentFailure('invalid-response')
  if (ref.kind === 'payment-entry' && !strings(value.against_invoices))
    throw new DocumentFailure('invalid-response')
  if (ref.kind === 'sales-invoice' && typeof value.is_return !== 'boolean')
    throw new DocumentFailure('invalid-response')
  if (
    ref.kind === 'sales-invoice' &&
    value.against_delivery_notes !== undefined &&
    (!strings(value.against_delivery_notes) ||
      (value.against_delivery_notes as string[]).some(
        (name) => name.length < 1 || name.length > 140,
      ))
  )
    throw new DocumentFailure('invalid-response')
  // Only the typed gateway fields reach the UI; arbitrary ERP response fields are discarded.
  const allowed = [
    'name',
    'company',
    'status',
    ...amountKeys,
    ...textKeys,
    ...dates,
    ...(ref.kind === 'delivery-note' ? ['against_sales_orders'] : []),
    ...(ref.kind === 'payment-entry' ? ['against_invoices'] : []),
    ...(ref.kind === 'sales-invoice'
      ? [
          'is_return',
          ...(value.against_delivery_notes !== undefined ? ['against_delivery_notes'] : []),
        ]
      : []),
  ]
  return {
    ...Object.fromEntries(allowed.map((key) => [key, value[key]])),
    ...(Array.isArray(value.lines)
      ? {
          lines: value.lines.map((line, ordinal) => ({
            id: `${ref.name}:${ordinal}`,
            item_code: line.item_code,
            qty: line.qty,
            rate: line.rate,
            amount: line.amount,
          })),
        }
      : {}),
    kind: ref.kind,
  } as TownDocument
}

/** ERP document data stays in memory and follows the active company connection.
 * Writes are never retried: losing an acknowledgement requires checking Classic ERP.
 */
export class DocumentSession {
  private state: DocumentView = { busy: false, uncertain: false }
  private listeners = new Set<() => void>()
  private company = ''
  private active = false
  private epoch = 0
  private pending?: AbortController
  private writing = false
  constructor(private fetcher: typeof fetch) {}
  view = (): DocumentView => this.state
  subscribe = (listener: () => void) => {
    this.listeners.add(listener)
    return () => {
      this.listeners.delete(listener)
    }
  }
  private publish(next: DocumentView) {
    this.state = next
    for (const listener of this.listeners) listener()
  }
  scope(company: string, active: boolean) {
    if (company === this.company && active === this.active) return
    this.epoch++
    const uncertain = this.state.uncertain || this.writing
    this.pending?.abort()
    this.pending = undefined
    this.writing = false
    this.company = company
    this.active = active
    this.publish({ busy: false, uncertain })
  }
  acknowledge() {
    if (!this.state.busy) this.publish({ ...this.state, uncertain: false, error: undefined })
  }
  close() {
    if (!this.state.busy) this.publish({ busy: false, uncertain: this.state.uncertain })
  }
  edit(kind: DocumentKind) {
    if (this.active && !this.state.busy && !this.state.uncertain)
      this.publish({ ...this.state, activity: { kind, action: 'edit' } })
  }
  async open(ref: DocumentRef) {
    await this.run(false, { kind: ref.kind, action: 'read' }, async (signal, epoch) =>
      this.read(ref, signal, epoch),
    )
  }
  async refresh() {
    if (this.state.ref) await this.open(this.state.ref)
  }
  async create(kind: DocumentKind, body: Record<string, unknown>) {
    await this.run(true, { kind, action: 'create' }, async (signal, epoch) => {
      const value = await this.request(`/town/api/${documentRoutes[kind]}`, signal, {
        ...body,
        company: this.company,
      })
      if (!object(value) || typeof value.name !== 'string' || !value.name)
        throw new DocumentFailure('invalid-response', true)
      if (epoch !== this.epoch) return
      this.writing = false
      const ref = { kind, name: value.name }
      this.publish({ ...this.state, ref, document: undefined })
      await this.read(ref, signal, epoch)
    })
  }
  async submit() {
    const ref = this.state.ref
    if (!ref || this.state.document?.status !== 'Draft') return
    await this.run(true, { kind: ref.kind, action: 'submit' }, async (signal, epoch) => {
      const value = await this.request(
        `/town/api/documents/${ref.kind}/${encodeURIComponent(ref.name)}/submit`,
        signal,
        {},
      )
      if (
        !object(value) ||
        typeof value.submitted !== 'boolean' ||
        typeof value.workflow_state !== 'string'
      )
        throw new DocumentFailure('invalid-response', true)
      if (epoch !== this.epoch) return
      this.writing = false
      this.publish({ ...this.state, approval: !value.submitted, document: undefined })
      await this.read(ref, signal, epoch)
    })
  }
  private async read(ref: DocumentRef, signal: AbortSignal, epoch: number) {
    const value = await this.request(
      `/town/api/${documentRoutes[ref.kind]}/${encodeURIComponent(ref.name)}`,
      signal,
    )
    if (epoch !== this.epoch) return
    const document = decodeDocument(value, ref, this.company)
    this.publish({
      ...this.state,
      ref,
      document,
      approval: this.state.approval && document.status === 'Draft',
    })
  }
  private async run(
    write: boolean,
    activity: NonNullable<DocumentView['activity']>,
    action: (signal: AbortSignal, epoch: number) => Promise<void>,
  ) {
    if (!this.active || this.state.busy || (write && this.state.uncertain)) return
    const epoch = this.epoch
    const controller = new AbortController()
    this.pending = controller
    this.writing = write
    this.publish({
      ...this.state,
      busy: true,
      activity,
      error: undefined,
      approval: undefined,
      document: write ? this.state.document : undefined,
    })
    const timeout = setTimeout(() => controller.abort(), 45000)
    try {
      await action(controller.signal, epoch)
    } catch (error) {
      if (epoch === this.epoch) {
        const failure =
          error instanceof DocumentFailure
            ? error
            : new DocumentFailure('unavailable', this.writing)
        this.publish({
          ...this.state,
          document: undefined,
          error: failure.code,
          uncertain: this.state.uncertain || failure.uncertain,
        })
      }
    } finally {
      clearTimeout(timeout)
      if (epoch === this.epoch) {
        this.pending = undefined
        this.writing = false
        this.publish({
          ...this.state,
          busy: false,
          activity:
            this.state.document && !this.state.error
              ? { kind: this.state.document.kind, action: 'read' }
              : undefined,
        })
      }
    }
  }
  private async request(
    path: string,
    signal: AbortSignal,
    body?: Record<string, unknown>,
  ): Promise<unknown> {
    const write = body !== undefined
    let response: Response
    try {
      response = await this.fetcher(path, {
        credentials: 'same-origin',
        signal,
        method: write ? 'POST' : 'GET',
        ...(write
          ? {
              headers: { 'Content-Type': 'application/json', 'X-Town-Request': '1' },
              body: JSON.stringify(body),
            }
          : {}),
      })
    } catch {
      throw new DocumentFailure('unavailable', write)
    }
    if (!response.ok) {
      const errors: Record<number, DocumentError> = {
        401: 'sign-in',
        403: 'forbidden',
        404: 'not-found',
        409: 'conflict',
        422: 'validation',
      }
      throw new DocumentFailure(
        errors[response.status] ?? 'unavailable',
        write && !errors[response.status],
      )
    }
    try {
      return await response.json()
    } catch {
      throw new DocumentFailure('invalid-response', write)
    }
  }
}
