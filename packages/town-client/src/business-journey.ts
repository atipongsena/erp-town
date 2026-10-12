import {
  type DocumentKind,
  type DocumentRef,
  decodeDocument,
  documentRoutes,
  type TownDocument,
} from './documents.ts'
export type JourneyEdge = { from: string; to: string; evidence: string }
export type BusinessJourneyView = {
  nodes: { id: string; document: TownDocument; settled: boolean }[]
  edges: JourneyEdge[]
  partial: boolean
  limited?: boolean
}
const key = (ref: DocumentRef) => `${ref.kind}:${ref.name}`
const references = (
  doc: TownDocument,
): { kind: DocumentKind; names: readonly string[]; field: string }[] =>
  doc.kind === 'delivery-note'
    ? [
        {
          kind: 'sales-order',
          names: doc.against_sales_orders ?? [],
          field: 'against_sales_orders',
        },
      ]
    : doc.kind === 'sales-invoice'
      ? [
          {
            kind: 'delivery-note',
            names: doc.against_delivery_notes ?? [],
            field: 'against_delivery_notes',
          },
        ]
      : doc.kind === 'payment-entry'
        ? [{ kind: 'sales-invoice', names: doc.against_invoices ?? [], field: 'against_invoices' }]
        : []
export function businessJourney(
  root: DocumentRef,
  records: readonly TownDocument[],
): BusinessJourneyView {
  const rootDoc = records.find((d) => key(d) === key(root))
  if (!rootDoc) return { nodes: [], edges: [], partial: true }
  const docs = new Map(
    [rootDoc, ...records]
      .filter((d) => d.company === rootDoc.company)
      .filter((d, i, all) => all.findIndex((other) => key(other) === key(d)) === i)
      .slice(0, 32)
      .map((d) => [key(d), d]),
  )
  const edges: JourneyEdge[] = []
  const incomplete = new Set<string>()
  for (const doc of docs.values()) {
    if (doc.kind === 'sales-invoice' && doc.against_delivery_notes === undefined)
      incomplete.add(key(doc))
    for (const ref of references(doc))
      for (const name of new Set(ref.names)) {
        const source = key({ kind: ref.kind, name })
        if (docs.has(source))
          edges.push({ from: source, to: key(doc), evidence: `${key(doc)}.${ref.field}` })
        else incomplete.add(key(doc))
      }
  }
  const connected = new Set([key(root)])
  let changed = true
  while (changed) {
    changed = false
    for (const edge of edges)
      if (connected.has(edge.from) || connected.has(edge.to)) {
        if (!connected.has(edge.from) || !connected.has(edge.to)) changed = true
        connected.add(edge.from)
        connected.add(edge.to)
      }
  }
  const nodes = [...docs.entries()]
    .filter(([id]) => connected.has(id))
    .map(([id, document]) => ({
      id,
      document: {
        ...document,
        against_sales_orders: undefined,
        against_delivery_notes: undefined,
        against_invoices: undefined,
      },
      settled:
        document.kind === 'sales-invoice' &&
        document.status === 'Submitted' &&
        !document.is_return &&
        typeof document.outstanding === 'string' &&
        /^-?0+(\.0+)?$/.test(document.outstanding),
    }))
  return {
    nodes,
    edges: edges.filter((e) => connected.has(e.from) && connected.has(e.to)),
    partial: [...connected].some((id) => incomplete.has(id)),
    limited: records.length > 32,
  }
}
export async function readJourneyDocument(
  ref: DocumentRef,
  company: string,
  signal: AbortSignal,
  fetcher: typeof fetch = fetch,
) {
  const response = await fetcher(
    `/town/api/${documentRoutes[ref.kind]}/${encodeURIComponent(ref.name)}`,
    { credentials: 'same-origin', signal: AbortSignal.any([signal, AbortSignal.timeout(5000)]) },
  )
  if (!response.ok) throw new Error('Document detail unavailable')
  if (!response.body) throw new Error('Document detail unavailable')
  const reader = response.body.getReader()
  const decoder = new TextDecoder('utf-8', { fatal: true })
  let bytes = 0,
    text = ''
  try {
    for (;;) {
      const chunk = await reader.read()
      if (chunk.done) break
      bytes += chunk.value.byteLength
      if (bytes > 65536) {
        await reader.cancel()
        throw new Error('Document response too large')
      }
      text += decoder.decode(chunk.value, { stream: true })
    }
    text += decoder.decode()
  } finally {
    reader.releaseLock()
  }
  return decodeDocument(JSON.parse(text), ref, company)
}
export class JourneyLoader {
  #abort?: AbortController
  #generation = 0
  constructor(
    private readonly read: (ref: DocumentRef, signal: AbortSignal) => Promise<TownDocument>,
  ) {}
  cancel() {
    this.#generation++
    this.#abort?.abort()
    this.#abort = undefined
  }
  async load(
    root: DocumentRef,
    candidates: readonly DocumentRef[],
    company: string,
  ): Promise<BusinessJourneyView | undefined> {
    this.cancel()
    const generation = this.#generation
    const abort = new AbortController()
    this.#abort = abort
    const refs = [...new Map([root, ...candidates].map((r) => [key(r), r])).values()]
    const bounded = refs.slice(0, 32)
    let cursor = 0,
      missing = false
    const records: TownDocument[] = []
    const worker = async () => {
      while (cursor < bounded.length && !abort.signal.aborted) {
        const ref = bounded[cursor++]
        if (!ref) break
        try {
          const doc = await this.read(ref, abort.signal)
          if (doc.company === company && key(doc) === key(ref)) records.push(doc)
          else missing = true
        } catch {
          missing = true
        }
      }
    }
    await Promise.all([worker(), worker()])
    if (generation !== this.#generation || abort.signal.aborted) return
    const view = businessJourney(root, records)
    return { ...view, partial: view.partial || missing, limited: refs.length > 32 }
  }
}
