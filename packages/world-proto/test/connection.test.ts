import { create, fromBinary, toBinary } from '@bufbuild/protobuf'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { WorldConnection, type WorldSocket } from '../src/connection.ts'
import { ClientFrameSchema, EntitySchema, FrameKind, ServerFrameSchema } from '../src/index.ts'

class Socket extends EventTarget implements WorldSocket {
  binaryType = 'blob'
  sent: Uint8Array[] = []
  closed = false
  send(bytes: Uint8Array) {
    this.sent.push(bytes)
  }
  close() {
    this.closed = true
  }
  open() {
    this.dispatchEvent(new Event('open'))
  }
  frame(version = 1n, baseVersion = 0n, kind = FrameKind.SNAPSHOT) {
    this.binary(
      toBinary(
        ServerFrameSchema,
        create(ServerFrameSchema, {
          company: 'ACME-TH',
          kind,
          version,
          baseVersion,
          resumeToken: `resume-${version}`,
          upserts:
            kind === FrameKind.SNAPSHOT
              ? [
                  create(EntitySchema, {
                    key: { doctype: 'Sales Order', name: 'SO-1', kind: 'document' },
                    version,
                    dataJson: new TextEncoder().encode('{"status":"Submitted"}'),
                  }),
                ]
              : [],
        }),
      ),
    )
  }
  binary(bytes: Uint8Array) {
    const event = new Event('message')
    Object.assign(event, { data: bytes.slice().buffer })
    this.dispatchEvent(event)
  }
  end(code = 1001, reason = 'world shutting down') {
    const event = new Event('close')
    Object.assign(event, { code, reason })
    this.dispatchEvent(event)
  }
}
const credentials = { company: 'ACME-TH', jwt: 'private-jwt' }
const setup = () => {
  const sockets: Socket[] = []
  const urls: string[] = []
  const client = new WorldConnection('ws://localhost:8090/town/ws', (url) => {
    urls.push(url)
    const socket = new Socket()
    sockets.push(socket)
    return socket
  })
  client.connect(credentials)
  const first = sockets[0]
  if (!first) throw new Error('Socket missing')
  return { client, sockets, urls, first }
}

afterEach(() => vi.useRealTimers())

describe('world connection lifecycle', () => {
  it('sends credentials only in a binary JOIN and shows entities after a snapshot', () => {
    const { client, first, urls } = setup()
    expect(client.view().phase).toBe('connecting')
    expect(first.binaryType).toBe('arraybuffer')
    first.open()
    expect(urls).toEqual(['ws://localhost:8090/town/ws'])
    const bytes = first.sent[0]
    if (!bytes) throw new Error('JOIN missing')
    const frame = fromBinary(ClientFrameSchema, bytes)
    expect(frame.message.case).toBe('join')
    if (frame.message.case !== 'join') throw new Error('Unexpected client message')
    expect(frame.message.value).toMatchObject({
      jwt: 'private-jwt',
      company: 'ACME-TH',
      lastVersion: 0n,
      resumeToken: '',
    })
    first.frame()
    expect(client.view().phase).toBe('live')
    expect(client.view().entities).toHaveLength(1)
    expect(
      JSON.stringify(client.view(), (_key, value) =>
        typeof value === 'bigint' ? value.toString() : value,
      ),
    ).not.toContain('private-jwt')
    client.disconnect()
  })

  it('hides stale display during disconnect and resumes from the exact memory version', () => {
    vi.useFakeTimers()
    const { client, first, sockets } = setup()
    first.open()
    first.frame(7n)
    first.end()
    expect(client.view().phase).toBe('offline')
    expect(client.view().entities).toEqual([])
    vi.advanceTimersByTime(250)
    const second = sockets[1]
    if (!second) throw new Error('Reconnect missing')
    second.open()
    const bytes = second.sent[0]
    if (!bytes) throw new Error('Resume JOIN missing')
    const frame = fromBinary(ClientFrameSchema, bytes)
    if (frame.message.case !== 'join') throw new Error('JOIN missing')
    expect(frame.message.value).toMatchObject({ lastVersion: 7n, resumeToken: 'resume-7' })
    second.frame(7n, 7n, FrameKind.PATCH)
    expect(client.view().phase).toBe('live')
    expect(client.view().entities).toHaveLength(1)
    client.disconnect()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('ignores frames and close events from the previous selected company/session', () => {
    vi.useFakeTimers()
    const { client, first, sockets } = setup()
    first.open()
    first.frame()
    client.connect({ company: 'OTHER', jwt: 'new-jwt' })
    first.frame(9n)
    first.end()
    expect(client.view().company).toBe('OTHER')
    expect(client.view().entities).toEqual([])
    expect(client.view().phase).toBe('connecting')
    expect(sockets).toHaveLength(2)
    vi.advanceTimersByTime(10000)
    expect(sockets).toHaveLength(2)
    client.disconnect()
  })

  it.each(['authentication expired', 'company forbidden', 'display intent required'])(
    'does not retry a policy close: %s',
    (reason) => {
      vi.useFakeTimers()
      const { client, first, sockets } = setup()
      first.open()
      first.frame()
      first.end(1008, reason)
      expect(client.view().phase).toBe('auth-required')
      expect(client.view().entities).toEqual([])
      vi.advanceTimersByTime(30000)
      expect(sockets).toHaveLength(1)
      expect(vi.getTimerCount()).toBe(0)
      client.disconnect()
    },
  )

  it('resyncs with a fresh JOIN after a gap and cannot reuse invalid state', () => {
    vi.useFakeTimers()
    const { client, first, sockets } = setup()
    first.open()
    first.frame()
    first.frame(4n, 3n, FrameKind.PATCH)
    expect(first.closed).toBe(true)
    expect(client.view().entities).toEqual([])
    vi.advanceTimersByTime(250)
    const second = sockets[1]
    if (!second) throw new Error('Resync missing')
    second.open()
    const bytes = second.sent[0]
    if (!bytes) throw new Error('JOIN missing')
    const frame = fromBinary(ClientFrameSchema, bytes)
    if (frame.message.case !== 'join') throw new Error('JOIN missing')
    expect(frame.message.value).toMatchObject({ lastVersion: 0n, resumeToken: '' })
    second.frame(8n)
    expect(client.view().version).toBe(8n)
    client.disconnect()
  })

  it('resyncs a server overflow but stops explicitly without leaking timers', () => {
    vi.useFakeTimers()
    const { client, first, sockets } = setup()
    first.open()
    first.frame()
    first.end(1008, 'resync required')
    expect(client.view().entities).toEqual([])
    client.disconnect()
    vi.advanceTimersByTime(60000)
    expect(sockets).toHaveLength(1)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('rejects URL credentials and query tokens before constructing a socket', () => {
    const factory = vi.fn()
    for (const url of [
      'https://localhost/town/ws',
      'ws://user:secret@localhost/town/ws',
      'ws://localhost/town/ws?jwt=secret',
      'ws://localhost/town/ws#secret',
    ])
      expect(() => new WorldConnection(url, factory)).toThrow('invalid_url')
    expect(factory).not.toHaveBeenCalled()
  })

  it('requires binary frames and reconnects without retaining malformed facts', () => {
    vi.useFakeTimers()
    const { client, first } = setup()
    first.open()
    first.frame()
    const event = new Event('message')
    Object.assign(event, { data: 'text frame' })
    first.dispatchEvent(event)
    expect(first.closed).toBe(true)
    expect(client.view().entities).toEqual([])
    client.disconnect()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('bounds retry delay and stops an unauthenticated socket that never acknowledges JOIN', () => {
    vi.useFakeTimers()
    const { client, sockets, first } = setup()
    first.open()
    vi.advanceTimersByTime(5000)
    expect(first.closed).toBe(true)
    expect(client.view().phase).toBe('offline')
    vi.advanceTimersByTime(250)
    expect(sockets).toHaveLength(2)
    // Repeated early network failures use capped backoff rather than a busy loop.
    for (const delay of [500, 1000, 2000, 4000, 5000, 5000]) {
      sockets.at(-1)?.end()
      vi.advanceTimersByTime(delay - 1)
      const count = sockets.length
      vi.advanceTimersByTime(1)
      expect(sockets).toHaveLength(count + 1)
    }
    client.disconnect()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('uses the native browser WebSocket contract without creating a network connection', () => {
    // Type-check the real adapter; lifecycle tests inject the same EventTarget port.
    const nativeFactory: (url: string) => WorldSocket = (url) => new WebSocket(url)
    expect(typeof nativeFactory).toBe('function')
  })

  it('retries factory and JOIN send failures without exposing their error payloads', () => {
    vi.useFakeTimers()
    let attempts = 0
    const socket = new Socket()
    const client = new WorldConnection('ws://localhost/town/ws', () => {
      if (++attempts === 1) throw new Error('private-jwt')
      return socket
    })
    client.connect(credentials)
    expect(client.view().phase).toBe('offline')
    vi.advanceTimersByTime(250)
    expect(client.view().phase).toBe('connecting')
    vi.spyOn(socket, 'send').mockImplementation(() => {
      throw new Error('private-jwt')
    })
    socket.open()
    expect(socket.closed).toBe(true)
    expect(client.view().phase).toBe('offline')
    client.disconnect()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('notifies subscribers with an owned view and allows unsubscribing', () => {
    const { client, first } = setup()
    const listener = vi.fn()
    const unsubscribe = client.subscribe(listener)
    first.open()
    first.frame()
    expect(listener).toHaveBeenCalledTimes(1)
    const view = client.view()
    if (view.entities[0]?.key) view.entities[0].key.name = 'changed'
    expect(client.view().entities[0]?.key?.name).toBe('SO-1')
    unsubscribe()
    client.disconnect()
    expect(listener).toHaveBeenCalledTimes(1)
  })

  it('bounds the wait for a network handshake and ignores its eventual late open', () => {
    vi.useFakeTimers()
    const { client, first, sockets } = setup()
    vi.advanceTimersByTime(15000)
    expect(first.closed).toBe(true)
    expect(client.view().phase).toBe('offline')
    first.open()
    expect(first.sent).toEqual([])
    vi.advanceTimersByTime(250)
    expect(sockets).toHaveLength(2)
    client.disconnect()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('sends only whitelisted display intents while live, without queuing offline actions', () => {
    const { client, first } = setup()
    expect(client.displayIntent('camera.focus', 'warehouse')).toBe(false)
    first.open()
    first.frame()
    expect(client.displayIntent('camera.focus', 'warehouse')).toBe(true)
    expect(client.displayIntent('avatar.emote', 'wave')).toBe(true)
    for (const [name, target] of [
      ['Sales Order.submit', 'SO-1'],
      ['camera.focus', 'ก'.repeat(43)],
    ])
      expect(client.displayIntent(name ?? '', target ?? '')).toBe(false)
    const intents = first.sent.slice(1).map((bytes) => fromBinary(ClientFrameSchema, bytes).message)
    expect(intents).toHaveLength(2)
    expect(intents[0]).toMatchObject({
      case: 'displayIntent',
      value: { name: 'camera.focus', target: 'warehouse' },
    })
    first.end()
    expect(client.displayIntent('avatar.emote', 'wave')).toBe(false)
    client.disconnect()
  })
})
