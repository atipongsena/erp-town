import { expect, it, vi } from 'vitest'
import { AvatarSession, type Presence } from '../src/avatar.ts'

const response = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status })

it('publishes consented work and idle department changes without waiting for the heartbeat', async () => {
  vi.useFakeTimers()
  const sent: unknown[] = []
  let presence: Presence = {
    id: '01JA0K6Y8Z9P3R4S5T6V7W8XA1',
    department: 'hq',
    emote: 'idle',
    appearance: 'human-blue',
  }
  const fetcher: typeof fetch = async (path, init) => {
    if (String(path).includes('preference'))
      return response({ consent: true, appearance: 'human-blue' })
    if (init?.method === 'POST') {
      const body = JSON.parse(String(init.body))
      sent.push(body)
      presence = { ...presence, department: body.department, emote: body.emote }
      return response(presence)
    }
    return response([presence])
  }
  const s = new AvatarSession(fetcher)
  try {
    await s.scope('ACME', true, 'hq')
    for (const [department, emote] of [
      ['shop', 'read'],
      ['shop', 'type'],
      ['warehouse', 'carry'],
      ['warehouse', 'idle'],
    ] as const) {
      const previous = sent.length
      await s.scope('ACME', true, department, emote)
      expect(sent).toHaveLength(previous + 1)
      expect(sent.at(-1)).toEqual({ company: 'ACME', department, emote })
      expect(s.view().presences).toEqual([{ ...presence, department, emote }])
    }
    const count = sent.length
    await s.scope('ACME', true, 'warehouse', 'idle')
    expect(sent).toHaveLength(count)
    expect(vi.getTimerCount()).toBe(1)
    await s.scope('', false, 'hq')
    expect(s.view().presences).toEqual([])
    expect(vi.getTimerCount()).toBe(0)
  } finally {
    await s.scope('', false, 'hq')
    vi.useRealTimers()
  }
})

it('work changes and heartbeats never publish people without appearance consent', async () => {
  vi.useFakeTimers()
  const fetcher = vi.fn(async (path: RequestInfo | URL, _init?: RequestInit) =>
    response(
      String(path).includes('preference') ? { consent: false, appearance: 'human-blue' } : [],
    ),
  )
  const s = new AvatarSession(fetcher)
  try {
    await s.scope('ACME', true, 'hq')
    await s.scope('ACME', true, 'shop', 'read')
    await s.scope('ACME', true, 'shop', 'type')
    await s.scope('ACME', true, 'warehouse', 'carry')
    await vi.advanceTimersByTimeAsync(30000)
    expect(fetcher.mock.calls.filter((call) => call[1]?.method === 'POST')).toEqual([])
    expect(s.view().presences).toEqual([])
    expect(s.view().preference.consent).toBe(false)
  } finally {
    await s.scope('', false, 'hq')
    vi.useRealTimers()
  }
})

it('ignores a late work presence response when the user has already moved to another department', async () => {
  vi.useFakeTimers()
  let releaseOld: (value: Response) => void = () => {}
  let held = false
  let presence: Presence = {
    id: '01JA0K6Y8Z9P3R4S5T6V7W8XA1',
    department: 'hq',
    emote: 'idle',
    appearance: 'human-blue',
  }
  const fetcher: typeof fetch = async (path, init) => {
    if (String(path).includes('preference'))
      return response({ consent: true, appearance: 'human-blue' })
    if (init?.method === 'POST') {
      const body = JSON.parse(String(init.body))
      presence = { ...presence, department: body.department, emote: body.emote }
      return response(presence)
    }
    if (presence.department === 'warehouse') {
      held = true
      return new Promise<Response>((done) => {
        releaseOld = done
      })
    }
    return response([presence])
  }
  const s = new AvatarSession(fetcher)
  try {
    await s.scope('ACME', true, 'hq')
    const old = s.scope('ACME', true, 'warehouse', 'carry')
    await vi.waitFor(() => expect(held).toBe(true))
    const stale = { ...presence }
    await s.scope('ACME', true, 'bank', 'read')
    expect(s.view().presences[0]).toMatchObject({ department: 'bank', emote: 'read' })
    releaseOld(response([stale]))
    await old
    expect(s.view().presences[0]).toMatchObject({ department: 'bank', emote: 'read' })
  } finally {
    await s.scope('', false, 'hq')
    vi.useRealTimers()
  }
})
it('defaults off, never sends presence before consent and clears on disconnect', async () => {
  vi.useFakeTimers()
  const calls: { path: string; init?: RequestInit }[] = []
  const fetcher: typeof fetch = async (path, init) => {
    calls.push({ path: String(path), init })
    return response(
      String(path).includes('preference') ? { consent: false, appearance: 'human-blue' } : [],
    )
  }
  const s = new AvatarSession(fetcher)
  try {
    await s.scope('ACME', true, 'it')
    expect(s.view().available).toBe(true)
    expect(s.view().preference.consent).toBe(false)
    expect(calls.filter((c) => c.init?.method === 'POST')).toEqual([])
    await vi.advanceTimersByTimeAsync(30000)
    expect(calls.filter((c) => c.init?.method === 'POST')).toEqual([])
    await s.scope('', false, 'it')
    expect(s.view().presences).toEqual([])
    expect(vi.getTimerCount()).toBe(0)
  } finally {
    await s.scope('', false, 'it')
    vi.useRealTimers()
  }
})
it('explicit consent sends only coarse company/department/emote and opt-out clears immediately', async () => {
  vi.useFakeTimers()
  let consent = false
  const calls: { path: string; init?: RequestInit }[] = []
  const fetcher: typeof fetch = async (path, init) => {
    calls.push({ path: String(path), init })
    if (init?.method === 'PUT') {
      consent = JSON.parse(String(init.body)).consent
      return response({ ok: true })
    }
    if (String(path).includes('preference')) return response({ consent, appearance: 'human-blue' })
    return response(
      init?.method === 'POST'
        ? {
            id: '01JA0K6Y8Z9P3R4S5T6V7W8XA1',
            department: 'it',
            emote: 'idle',
            appearance: 'human-blue',
          }
        : [],
    )
  }
  const s = new AvatarSession(fetcher)
  try {
    await s.scope('ACME', true, 'it')
    await s.setConsent(true)
    expect(s.view().preference.consent).toBe(true)
    const sent = calls.find((c) => c.init?.method === 'POST')
    expect(JSON.parse(String(sent?.init?.body))).toEqual({
      company: 'ACME',
      department: 'it',
      emote: 'idle',
    })
    expect(sent?.init?.headers).toEqual({
      'Content-Type': 'application/json',
      'X-Town-Request': '1',
    })
    await s.setConsent(false)
    expect(s.view().presences).toEqual([])
    expect(s.view().preference.consent).toBe(false)
  } finally {
    await s.scope('', false, 'it')
    vi.useRealTimers()
  }
})
it('rejects human identities/private payloads in presence and hides unconfigured routes', async () => {
  const s = new AvatarSession(async (path) =>
    response(
      String(path).includes('preference')
        ? { consent: false, appearance: 'human-blue' }
        : [
            {
              id: '01JA0K6Y8Z9P3R4S5T6V7W8XA1',
              department: 'it',
              emote: 'idle',
              appearance: 'human-blue',
              user: 'private@test',
            },
          ],
    ),
  )
  try {
    await s.scope('ACME', true, 'it')
    expect(s.view().presences).toEqual([])
    expect(s.view().error).toBe(true)
  } finally {
    await s.scope('', false, 'it')
  }
  const unavailable = new AvatarSession(async () => response({}, 404))
  await unavailable.scope('ACME', true, 'it')
  expect(unavailable.view().available).toBe(false)
  await unavailable.scope('', false, 'it')
})
