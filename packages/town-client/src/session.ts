import { WorldConnection, type WorldConnectionView, type WorldSocket } from '@erp-town/world-proto'
import { REPLAY_MAX_AGE_MS, ReplayHistory, type ReplayView } from './replay.ts'
import { displayFacts } from './world-view.ts'

export type SessionError =
  | 'sign-in'
  | 'forbidden'
  | 'unavailable'
  | 'town-unavailable'
  | 'invalid-response'
export type SessionView = Omit<WorldConnectionView, 'phase'> & {
  phase: WorldConnectionView['phase'] | 'loading'
  companies: string[]
  user: string | undefined
  roles: string[]
  error: SessionError | undefined
  replay: ReplayView
}
/** Same-origin gateway only. The host's existing Frappe cookie owns authorization. */
export class TownSession {
  #connection: WorldConnection
  #fetch: typeof fetch
  #loading = false
  #error: SessionError | undefined
  #companies: string[] = []
  #user: string | undefined
  #roles: string[] = []
  #abort: AbortController | undefined
  #generation = 0
  #deadline: ReturnType<typeof setTimeout> | undefined
  #listeners = new Set<() => void>()
  #replay = new ReplayHistory()
  #replayDeadline: ReturnType<typeof setTimeout> | undefined
  constructor(origin: string, fetcher: typeof fetch, factory: (url: string) => WorldSocket) {
    const url = new URL('/town/ws', origin)
    url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:'
    this.#connection = new WorldConnection(url.href, factory)
    this.#fetch = fetcher
    this.#connection.subscribe(() => {
      const view = this.#connection.view()
      this.#replay.observe({ ...view, facts: displayFacts(view.entities) })
      this.#emit()
    })
  }
  view(): SessionView {
    const view = this.#connection.view()
    return {
      ...view,
      phase: this.#loading ? 'loading' : view.phase,
      companies: [...this.#companies],
      user: this.#user,
      roles: [...this.#roles],
      error: this.#error,
      replay: this.#replay.view(),
    }
  }
  subscribe(listener: () => void): () => void {
    this.#listeners.add(listener)
    return () => {
      this.#listeners.delete(listener)
    }
  }
  selectReplay(version: bigint | undefined): void {
    this.#replay.select(version)
    this.#emit()
  }
  #emit(): void {
    clearTimeout(this.#replayDeadline)
    this.#replayDeadline = undefined
    const oldest = this.#replay.view().points[0]
    if (oldest)
      this.#replayDeadline = setTimeout(
        () => this.#emit(),
        Math.max(1, oldest.receivedAt + REPLAY_MAX_AGE_MS + 1 - Date.now()),
      )
    for (const listener of this.#listeners) listener()
  }
  disconnect(): void {
    this.#generation++
    clearTimeout(this.#deadline)
    this.#deadline = undefined
    this.#abort?.abort()
    this.#abort = undefined
    this.#loading = false
    this.#companies = []
    this.#user = undefined
    this.#roles = []
    this.#error = undefined
    this.#connection.disconnect()
  }
  async start(company?: string): Promise<void> {
    this.disconnect()
    const generation = this.#generation
    const abort = new AbortController()
    this.#abort = abort
    this.#loading = true
    this.#emit()
    const active = () => generation === this.#generation && !abort.signal.aborted
    this.#deadline = setTimeout(() => {
      this.#deadline = undefined
      if (!active()) return
      abort.abort()
      this.#loading = false
      this.#error = 'unavailable'
      this.#companies = []
      this.#connection.disconnect()
    }, 45000)
    const request = async (path: string, post = false): Promise<unknown> => {
      const result = await this.#fetch(path, {
        method: post ? 'POST' : 'GET',
        credentials: 'same-origin',
        signal: abort.signal,
        ...(post ? { headers: { 'X-Town-Request': '1' } } : {}),
      })
      if (!result.ok)
        throw result.status === 401
          ? 'sign-in'
          : result.status === 403
            ? 'forbidden'
            : 'unavailable'
      return result.json()
    }
    try {
      const me = await request('/town/api/me')
      if (!active()) return
      if (
        !me ||
        typeof me !== 'object' ||
        !('companies' in me) ||
        !Array.isArray(me.companies) ||
        !me.companies.every((c) => typeof c === 'string' && c.trim())
      )
        throw 'invalid-response'
      this.#companies = [...new Set<string>(me.companies)]
      if ('user' in me && typeof me.user === 'string') this.#user = me.user
      if ('roles' in me && Array.isArray(me.roles) && me.roles.every((r) => typeof r === 'string'))
        this.#roles = [...me.roles]
      if ('world_enabled' in me) {
        if (typeof me.world_enabled !== 'boolean') throw 'invalid-response'
        if (!me.world_enabled) throw 'town-unavailable'
      }
      const selected = company ?? this.#companies[0]
      if (!selected || !this.#companies.includes(selected)) throw 'forbidden'
      const auth = await request('/town/api/auth/token', true)
      if (!active()) return
      if (
        !auth ||
        typeof auth !== 'object' ||
        !('token' in auth) ||
        typeof auth.token !== 'string' ||
        !auth.token.trim()
      )
        throw 'invalid-response'
      this.#loading = false
      this.#connection.connect({ company: selected, jwt: auth.token })
    } catch (error) {
      if (!active()) return
      this.#loading = false
      this.#error = [
        'sign-in',
        'forbidden',
        'unavailable',
        'town-unavailable',
        'invalid-response',
      ].includes(String(error))
        ? (error as SessionError)
        : 'unavailable'
      this.#connection.disconnect()
    } finally {
      if (generation === this.#generation) {
        clearTimeout(this.#deadline)
        this.#deadline = undefined
      }
    }
    this.#emit()
  }
}
