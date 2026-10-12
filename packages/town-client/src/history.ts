export type Transition = {
  time: string
  doctype: string
  name: string
  kind: string
  status: string
}
export type HistoryView = { transitions: Transition[]; truncated: boolean }
const object = (v: unknown): v is Record<string, unknown> =>
  !!v && typeof v === 'object' && !Array.isArray(v)
/** Read-only coarse business transitions; no document reads or export. */
export async function readHistory(
  fetcher: typeof fetch,
  company: string,
  signal: AbortSignal,
  now = new Date(),
): Promise<HistoryView> {
  const q = new URLSearchParams({
    company,
    from: new Date(now.getTime() - 15 * 60000).toISOString(),
    until: now.toISOString(),
    limit: '100',
  })
  const r = await fetcher(`/town/api/history?${q}`, {
    credentials: 'same-origin',
    signal: AbortSignal.any([signal, AbortSignal.timeout(5000)]),
  })
  if (!r.ok) throw new Error(r.status === 404 ? 'disabled' : 'denied')
  if (!r.body) throw new Error('invalid')
  const reader = r.body.getReader()
  const chunks: Uint8Array[] = []
  let bytes = 0
  try {
    for (;;) {
      const chunk = await reader.read()
      if (chunk.done) break
      bytes += chunk.value.byteLength
      if (bytes > 65536) {
        await reader.cancel()
        throw new Error('invalid')
      }
      chunks.push(chunk.value)
    }
  } finally {
    reader.releaseLock()
  }
  const joined = new Uint8Array(bytes)
  let offset = 0
  for (const c of chunks) {
    joined.set(c, offset)
    offset += c.length
  }
  const data: unknown = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(joined))
  if (
    !object(data) ||
    Object.keys(data).length !== 2 ||
    typeof data.truncated !== 'boolean' ||
    !Array.isArray(data.transitions) ||
    data.transitions.length > 100
  )
    throw new Error('invalid')
  const rows: Transition[] = []
  for (const row of data.transitions) {
    if (
      !object(row) ||
      Object.keys(row).length !== 5 ||
      !['time', 'doctype', 'name', 'kind', 'status'].every(
        (k) => typeof row[k] === 'string' && row[k].length > 0 && row[k].length <= 140,
      ) ||
      !['document', 'approval', 'overdue', 'stock', 'agent_run'].includes(String(row.kind)) ||
      ![
        'submitted',
        'cancelled',
        'updated',
        'approval',
        'decided',
        'overdue',
        'stock',
        'ai-queued',
        'ai-running',
        'ai-completed',
        'ai-needs_human',
        'ai-failed',
        'ai-cancelled',
      ].includes(String(row.status)) ||
      !Number.isFinite(Date.parse(String(row.time)))
    )
      throw new Error('invalid')
    const transition = {
      time: String(row.time),
      doctype: String(row.doctype),
      name: String(row.name),
      kind: String(row.kind),
      status: String(row.status),
    }
    if (!rows.some((row) => JSON.stringify(row) === JSON.stringify(transition)))
      rows.push(transition)
  }
  return { transitions: rows, truncated: data.truncated }
}
