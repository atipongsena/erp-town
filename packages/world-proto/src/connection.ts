import { create, toBinary } from '@bufbuild/protobuf'
import { WorldClientState } from './client-state.ts'
import { ClientFrameSchema, type Entity } from './gen/world/v1/world_pb.ts'

/** Minimal browser WebSocket port; injected so credentials never reach a test network. */
export interface WorldSocket extends Pick<EventTarget, 'addEventListener'> {
  binaryType: string
  send(bytes: Uint8Array<ArrayBuffer>): void
  close(): void
}

export type WorldConnectionPhase = 'idle' | 'connecting' | 'live' | 'offline' | 'auth-required'
export type WorldConnectionView = {
  phase: WorldConnectionPhase
  company: string
  version: bigint
  entities: Entity[]
}

/** Owns a single company/session. Neither credentials nor cursors leave memory. */
export class WorldConnection {
  readonly #url: string
  readonly #factory: (url: string) => WorldSocket
  #state: WorldClientState | undefined
  #jwt = ''
  #socket: WorldSocket | undefined
  #generation = 0
  #phase: WorldConnectionPhase = 'idle'
  #retry: ReturnType<typeof setTimeout> | undefined
  #handshake: ReturnType<typeof setTimeout> | undefined
  #ack: ReturnType<typeof setTimeout> | undefined
  #attempt = 0
  #listeners = new Set<() => void>()

  constructor(url: string, factory: (url: string) => WorldSocket) {
    let parsed: URL
    try {
      parsed = new URL(url)
    } catch {
      throw new Error('World connection: invalid_url')
    }
    if (
      !['ws:', 'wss:'].includes(parsed.protocol) ||
      parsed.username ||
      parsed.password ||
      parsed.search ||
      parsed.hash
    )
      throw new Error('World connection: invalid_url')
    this.#url = parsed.href
    this.#factory = factory
  }

  connect(session: { company: string; jwt: string }): void {
    const state = new WorldClientState(session.company)
    if (!session.jwt.trim()) throw new Error('World connection: jwt is required')
    this.disconnect()
    this.#state = state
    this.#jwt = session.jwt
    this.#attempt = 0
    this.#open()
  }

  disconnect(): void {
    this.#retire()
    this.#jwt = ''
    this.#state = undefined
    this.#phase = 'idle'
    this.#notify()
  }

  view(): WorldConnectionView {
    const live = this.#phase === 'live'
    return {
      phase: this.#phase,
      company: this.#state?.company ?? '',
      version: live ? (this.#state?.version ?? 0n) : 0n,
      entities: live ? (this.#state?.entities() ?? []) : [],
    }
  }

  subscribe(listener: () => void): () => void {
    this.#listeners.add(listener)
    return () => {
      this.#listeners.delete(listener)
    }
  }

  /** Mirrors C's allowlist and UTF-8 byte bound; no document commands or offline queue. */
  displayIntent(name: string, target: string): boolean {
    if (
      this.#phase !== 'live' ||
      !this.#socket ||
      !['camera.focus', 'avatar.emote'].includes(name) ||
      new TextEncoder().encode(target).byteLength > 128
    )
      return false
    try {
      this.#socket.send(
        toBinary(
          ClientFrameSchema,
          create(ClientFrameSchema, {
            message: { case: 'displayIntent', value: { name, target } },
          }),
        ),
      )
      return true
    } catch {
      this.#lost(false)
      return false
    }
  }

  #notify(): void {
    for (const listener of this.#listeners) listener()
  }

  #clearTimers(): void {
    clearTimeout(this.#retry)
    clearTimeout(this.#handshake)
    clearTimeout(this.#ack)
    this.#retry = undefined
    this.#handshake = undefined
    this.#ack = undefined
  }

  #retire(): void {
    // Invalidate callbacks before close: some adapters dispatch close synchronously.
    this.#generation++
    this.#clearTimers()
    const socket = this.#socket
    this.#socket = undefined
    socket?.close()
  }

  #lost(fresh: boolean, policy = false): void {
    this.#retire()
    if (fresh || policy) this.#state?.reset()
    this.#phase = policy ? 'auth-required' : 'offline'
    if (policy) this.#jwt = ''
    else {
      const delay = Math.min(250 * 2 ** Math.min(this.#attempt++, 5), 5000)
      const generation = this.#generation
      this.#retry = setTimeout(() => {
        this.#retry = undefined
        if (generation === this.#generation) this.#open()
      }, delay)
    }
    this.#notify()
  }

  #open(): void {
    if (!this.#state || !this.#jwt) return
    this.#phase = 'connecting'
    const generation = ++this.#generation
    let socket: WorldSocket
    try {
      socket = this.#factory(this.#url)
      this.#socket = socket
      socket.binaryType = 'arraybuffer'
    } catch {
      this.#lost(false)
      return
    }
    const active = () => generation === this.#generation && socket === this.#socket
    this.#handshake = setTimeout(() => {
      if (active()) this.#lost(false)
    }, 15000)
    socket.addEventListener('open', () => {
      if (!active() || !this.#state) return
      clearTimeout(this.#handshake)
      this.#handshake = undefined
      const cursor = this.#state.reconnectCursor()
      try {
        socket.send(
          toBinary(
            ClientFrameSchema,
            create(ClientFrameSchema, {
              message: {
                case: 'join',
                value: {
                  company: this.#state.company,
                  jwt: this.#jwt,
                  lastVersion: cursor?.lastVersion ?? 0n,
                  resumeToken: cursor?.resumeToken ?? '',
                },
              },
            }),
          ),
        )
      } catch {
        this.#lost(false)
        return
      }
      this.#ack = setTimeout(() => {
        if (active()) this.#lost(false)
      }, 5000)
    })
    socket.addEventListener('message', (event) => {
      if (!active() || !this.#state) return
      const data = (event as Event & { data: unknown }).data
      try {
        if (!(data instanceof ArrayBuffer)) throw new Error('invalid_binary')
        this.#state.acceptBinary(new Uint8Array(data))
      } catch {
        this.#lost(true)
        return
      }
      clearTimeout(this.#ack)
      this.#ack = undefined
      this.#attempt = 0
      this.#phase = 'live'
      this.#notify()
    })
    socket.addEventListener('close', (event) => {
      if (!active()) return
      const close = event as Event & { code: number; reason: string }
      const resync = close.code === 1008 && close.reason === 'resync required'
      this.#lost(resync, close.code === 1008 && !resync)
    })
    this.#notify()
  }
}
