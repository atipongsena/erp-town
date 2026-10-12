export type AgentRunStatus =
  | 'queued'
  | 'running'
  | 'completed'
  | 'needs_human'
  | 'failed'
  | 'cancelled'
export type AgentRun = {
  id: string
  agent: string
  status: AgentRunStatus
  calls: number
  created_at: string
  finished_at: string
  unresolved: boolean
}
export type AgentRuns = { runs: AgentRun[]; truncated: boolean }
const object = (value: unknown): value is Record<string, unknown> =>
  !!value && typeof value === 'object' && !Array.isArray(value)
const id = (value: unknown): value is string =>
  typeof value === 'string' && /^[A-Za-z0-9_.:-]{1,128}$/.test(value)
export async function readAgentRuns(
  fetcher: typeof fetch,
  company: string,
  signal: AbortSignal,
): Promise<AgentRuns> {
  const response = await fetcher(`/town/api/agent-runs?company=${encodeURIComponent(company)}`, {
    credentials: 'same-origin',
    signal: AbortSignal.any([signal, AbortSignal.timeout(5000)]),
    cache: 'no-store',
  })
  if (!response.ok)
    throw new Error(
      response.status === 403 ? 'forbidden' : response.status === 401 ? 'sign-in' : 'unavailable',
    )
  if (!response.body) throw new Error('invalid')
  const reader = response.body.getReader()
  const chunks: Uint8Array[] = []
  let bytes = 0
  try {
    for (;;) {
      const chunk = await reader.read()
      if (chunk.done) break
      bytes += chunk.value.byteLength
      if (bytes > 16384) {
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
  for (const chunk of chunks) {
    joined.set(chunk, offset)
    offset += chunk.length
  }
  const value: unknown = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(joined))
  if (
    !object(value) ||
    Object.keys(value).length !== 2 ||
    !Array.isArray(value.runs) ||
    value.runs.length > 25 ||
    typeof value.truncated !== 'boolean'
  )
    throw new Error('invalid')
  const runs: AgentRun[] = []
  const seen = new Set<string>()
  for (const row of value.runs) {
    if (
      !object(row) ||
      Object.keys(row).length !== 7 ||
      !id(row.id) ||
      !id(row.agent) ||
      seen.has(row.id) ||
      !['queued', 'running', 'completed', 'needs_human', 'failed', 'cancelled'].includes(
        String(row.status),
      ) ||
      !Number.isInteger(row.calls) ||
      Number(row.calls) < 0 ||
      Number(row.calls) > 16 ||
      typeof row.created_at !== 'string' ||
      !Number.isFinite(Date.parse(row.created_at)) ||
      typeof row.finished_at !== 'string' ||
      (row.finished_at !== '' && !Number.isFinite(Date.parse(row.finished_at))) ||
      typeof row.unresolved !== 'boolean'
    )
      throw new Error('invalid')
    seen.add(row.id)
    runs.push({
      id: row.id,
      agent: row.agent,
      status: row.status as AgentRunStatus,
      calls: Number(row.calls),
      created_at: row.created_at,
      finished_at: row.finished_at,
      unresolved: row.unresolved,
    })
  }
  return { runs, truncated: value.truncated }
}
