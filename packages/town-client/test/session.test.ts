import { describe, expect, it, vi } from 'vitest'
import wire from '../browser/fixtures/wire.json' with { type: 'json' }
import { TownSession } from '../src/session.ts'

const response = (data: unknown, status = 200) => new Response(JSON.stringify(data), { status })
const port = () => ({
  binaryType: 'blob',
  send: vi.fn(),
  close: vi.fn(),
  addEventListener: vi.fn(),
})
describe('gateway session boundary', () => {
  it('reports ERP identity and roles, then clears them on disconnect', async () => {
    const fetcher: typeof fetch = async (path) =>
      response(
        path === '/town/api/me'
          ? { user: 'operator@example.test', roles: ['System Manager'], companies: ['ACME'] }
          : { token: 'fixture' },
      )
    const session = new TownSession('http://localhost', fetcher, port)
    await session.start()
    expect(session.view().user).toBe('operator@example.test')
    expect(session.view().roles).toEqual(['System Manager'])
    session.view().roles.push('injected')
    expect(session.view().roles).toEqual(['System Manager'])
    session.disconnect()
    expect(session.view().user).toBeUndefined()
    expect(session.view().roles).toEqual([])
  })
  it('does not mint a token or reconnect forever when the host has no world service', async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValue(response({ companies: ['ACME'], world_enabled: false }))
    const factory = vi.fn(port)
    const session = new TownSession('http://localhost', fetcher, factory)
    await session.start()
    expect(session.view().error).toBe('town-unavailable')
    expect(fetcher).toHaveBeenCalledTimes(1)
    expect(factory).not.toHaveBeenCalled()
    session.disconnect()
  })
  it('expires replay without a new frame, preserves live state and clears its timer on disconnect', async () => {
    vi.useFakeTimers()
    const socket = Object.assign(new EventTarget(), {
      binaryType: 'blob',
      send: vi.fn(),
      close: vi.fn(),
    })
    const fetcher: typeof fetch = async (path) =>
      response(path === '/town/api/me' ? { companies: ['ACME'] } : { token: 'fixture-jwt' })
    const session = new TownSession('http://localhost', fetcher, () => socket)
    const changed = vi.fn()
    session.subscribe(changed)
    try {
      await session.start()
      socket.dispatchEvent(new Event('open'))
      socket.dispatchEvent(
        new MessageEvent('message', {
          data: Uint8Array.from(Buffer.from(wire.snapshot, 'hex')).buffer,
        }),
      )
      session.selectReplay(1n)
      expect(session.view().replay.selected).toBe(1n)
      expect(session.view().replay.facts[0]?.name).toBe('SO-TEST')
      changed.mockClear()
      vi.advanceTimersByTime(15 * 60 * 1000 + 1)
      expect(changed).toHaveBeenCalledTimes(1)
      expect(session.view().replay.points).toEqual([])
      expect(session.view().replay.selected).toBeUndefined()
      expect(session.view().entities).toHaveLength(1)
      expect(session.view().phase).toBe('live')
      expect(vi.getTimerCount()).toBe(0)
      // A fresh point schedules a timer, which must be cancelled on logout.
      session.disconnect()
      await session.start()
      socket.dispatchEvent(new Event('open'))
      socket.dispatchEvent(
        new MessageEvent('message', {
          data: Uint8Array.from(Buffer.from(wire.snapshot, 'hex')).buffer,
        }),
      )
      expect(vi.getTimerCount()).toBe(1)
      session.disconnect()
      expect(vi.getTimerCount()).toBe(0)
    } finally {
      session.disconnect()
      vi.useRealTimers()
    }
  })
  it('bounds an unresponsive gateway and cancels all pending work', async () => {
    vi.useFakeTimers()
    const fetcher: typeof fetch = (_url, options) =>
      new Promise((_resolve, reject) => {
        options?.signal?.addEventListener('abort', () =>
          reject(new DOMException('Aborted', 'AbortError')),
        )
      })
    const session = new TownSession('http://localhost', fetcher, port)
    try {
      const pending = session.start()
      vi.advanceTimersByTime(45000)
      expect(session.view().error).toBe('unavailable')
      await pending
      expect(session.view().phase).toBe('idle')
      expect(vi.getTimerCount()).toBe(0)
    } finally {
      session.disconnect()
      vi.useRealTimers()
    }
  })
  it('does not contact a gateway until explicitly requested and keeps token out of the public view', async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(response({ user: 'test@example.com', companies: ['ACME'], roles: [] }))
      .mockResolvedValueOnce(response({ token: 'private-jwt', expires_at: '2026-10-10T16:00:00Z' }))
    const session = new TownSession('http://localhost:5173', fetcher, port)
    expect(fetcher).not.toHaveBeenCalled()
    await session.start()
    expect(fetcher.mock.calls[0]?.[0]).toBe('/town/api/me')
    expect(fetcher.mock.calls[1]?.[1]).toMatchObject({
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'X-Town-Request': '1' },
    })
    expect(session.view().company).toBe('ACME')
    expect(
      JSON.stringify(session.view(), (_k, v) => (typeof v === 'bigint' ? v.toString() : v)),
    ).not.toContain('private-jwt')
    session.disconnect()
  })
  it('denies an unlisted company before requesting a token or opening a socket', async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValue(response({ user: 'test', companies: ['ACME'], roles: [] }))
    const factory = vi.fn(port)
    const session = new TownSession('http://localhost', fetcher, factory)
    await session.start('OTHER')
    expect(session.view().error).toBe('forbidden')
    expect(fetcher).toHaveBeenCalledTimes(1)
    expect(factory).not.toHaveBeenCalled()
    session.disconnect()
  })
  it('does not open a late session after logout or a second selection', async () => {
    let resolve: (r: Response) => void = () => {}
    const fetcher = vi.fn(
      () =>
        new Promise<Response>((r) => {
          resolve = r
        }),
    )
    const factory = vi.fn(port)
    const session = new TownSession('http://localhost', fetcher, factory)
    const pending = session.start()
    session.disconnect()
    resolve(response({ user: 'test', companies: ['ACME'], roles: [] }))
    await pending
    expect(session.view().phase).toBe('idle')
    expect(session.view().companies).toEqual([])
    expect(factory).not.toHaveBeenCalled()
  })
  it.each([401, 403, 503])(
    'sanitizes %s responses without copying server error messages',
    async (status) => {
      const session = new TownSession(
        'http://localhost',
        async () => response({ message: 'private-jwt' }, status),
        port,
      )
      await session.start()
      expect(session.view().error).toBe(
        status === 401 ? 'sign-in' : status === 403 ? 'forbidden' : 'unavailable',
      )
      expect(
        JSON.stringify(session.view(), (_k, v) => (typeof v === 'bigint' ? v.toString() : v)),
      ).not.toContain('private-jwt')
      session.disconnect()
    },
  )
})
