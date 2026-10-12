import { expect, it } from 'vitest'
import { readHistory } from '../src/history.ts'

it('requests a bounded same-origin window and returns no financial or human payload', async () => {
  let url = ''
  const rows = {
    transitions: [
      {
        time: '2026-10-11T00:00:00Z',
        doctype: 'Sales Order',
        name: 'SO-1',
        kind: 'document',
        status: 'submitted',
      },
    ],
    truncated: true,
  }
  const out = await readHistory(
    async (path, init) => {
      url = String(path)
      expect(init?.credentials).toBe('same-origin')
      return new Response(JSON.stringify(rows))
    },
    'ACME',
    new AbortController().signal,
    new Date('2026-10-11T00:01:00Z'),
  )
  expect(new URL(url, 'http://localhost').searchParams.get('limit')).toBe('100')
  expect(out).toEqual(rows)
})
it('rejects private extras and presence even if a server sends them', async () => {
  for (const extra of [{ user: 'private@test' }, { grand_total: '123.00' }, { kind: 'presence' }]) {
    await expect(
      readHistory(
        async () =>
          new Response(
            JSON.stringify({
              transitions: [
                {
                  time: '2026-10-11T00:00:00Z',
                  doctype: 'Sales Order',
                  name: 'SO-1',
                  kind: 'document',
                  status: 'submitted',
                  ...extra,
                },
              ],
              truncated: false,
            }),
          ),
        'ACME',
        new AbortController().signal,
      ),
    ).rejects.toThrow()
  }
})
