import { expect, it } from 'vitest'
import { readAgentRuns } from '../src/agent-runs.ts'

const row = {
  id: 'run-1',
  agent: 'helper',
  status: 'needs_human',
  calls: 1,
  created_at: '2026-10-11T00:00:00Z',
  finished_at: '',
  unresolved: true,
}
it('requests only company-scoped metadata and rejects private or unbounded responses', async () => {
  const view = { runs: [row], truncated: false }
  const out = await readAgentRuns(
    async (url, init) => {
      expect(String(url)).toBe('/town/api/agent-runs?company=ACME')
      expect(init?.credentials).toBe('same-origin')
      return new Response(JSON.stringify(view))
    },
    'ACME',
    new AbortController().signal,
  )
  expect(out).toEqual(view)
  for (const invalid of [
    { runs: [{ ...row, delegator: 'private-human' }], truncated: false },
    { runs: [{ ...row, status: 'force-completed' }], truncated: false },
    { runs: Array(26).fill(row), truncated: false },
    { runs: [row, row], truncated: false },
  ]) {
    await expect(
      readAgentRuns(
        async () => new Response(JSON.stringify(invalid)),
        'ACME',
        new AbortController().signal,
      ),
    ).rejects.toThrow()
  }
})
it('does not expose backend error text and distinguishes access denial', async () => {
  await expect(
    readAgentRuns(
      async () => new Response('private-provider-error', { status: 403 }),
      'ACME',
      new AbortController().signal,
    ),
  ).rejects.toThrow('forbidden')
})
