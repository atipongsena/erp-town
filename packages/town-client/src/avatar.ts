import type { Department } from './world-view.ts'
export type Appearance = 'human-blue' | 'human-coral' | 'human-green'
export type Presence = {
  id: string
  department: Department
  emote: 'idle' | 'wave' | 'hello' | 'read' | 'type' | 'carry'
  appearance: Appearance
}
export type AvatarView = {
  available: boolean
  preference: { consent: boolean; appearance: Appearance }
  presences: Presence[]
  busy: boolean
  error: boolean
}
const appearances = ['human-blue', 'human-coral', 'human-green']
const departments = ['shop', 'warehouse', 'bank', 'hq', 'it', 'post']
const object = (v: unknown): v is Record<string, unknown> =>
  !!v && typeof v === 'object' && !Array.isArray(v)
const empty = (): AvatarView => ({
  available: false,
  preference: { consent: false, appearance: 'human-blue' },
  presences: [],
  busy: false,
  error: false,
})
/** An ephemeral same-origin view. No presence goes into world replay or storage. */
export class AvatarSession {
  #fetch: typeof fetch
  #view = empty()
  #company = ''
  #department: Department = 'hq'
  #emote: Presence['emote'] = 'idle'
  #generation = 0
  #abort = new AbortController()
  #timer: ReturnType<typeof setTimeout> | undefined
  #listeners = new Set<() => void>()
  constructor(fetcher: typeof fetch) {
    this.#fetch = fetcher
  }
  view(): AvatarView {
    return {
      ...this.#view,
      preference: { ...this.#view.preference },
      presences: this.#view.presences.map((p) => ({ ...p })),
    }
  }
  subscribe(fn: () => void): () => void {
    this.#listeners.add(fn)
    return () => {
      this.#listeners.delete(fn)
    }
  }
  #emit() {
    for (const fn of this.#listeners) fn()
  }
  async #request(path: string, init?: RequestInit): Promise<unknown> {
    const r = await this.#fetch(path, {
      credentials: 'same-origin',
      ...init,
      signal: AbortSignal.any([this.#abort.signal, AbortSignal.timeout(5000)]),
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
    for (const chunk of chunks) {
      joined.set(chunk, offset)
      offset += chunk.length
    }
    return JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(joined))
  }
  async scope(
    company: string,
    live: boolean,
    department: Department,
    emote: Presence['emote'] = 'idle',
  ): Promise<void> {
    const changed = this.#department !== department || this.#emote !== emote
    this.#department = department
    this.#emote = emote
    if (this.#company === company && live && company) {
      if (changed && this.#view.available && this.#view.preference.consent && !this.#view.busy) {
        clearTimeout(this.#timer)
        this.#generation++
        this.#abort.abort()
        this.#abort = new AbortController()
        await this.#refresh(this.#generation)
      }
      return
    }
    this.#generation++
    this.#abort.abort()
    this.#abort = new AbortController()
    clearTimeout(this.#timer)
    this.#timer = undefined
    this.#company = live ? company : ''
    this.#view = empty()
    this.#emit()
    if (this.#company) await this.#refresh(this.#generation)
  }
  async #refresh(generation: number): Promise<void> {
    const active = () => generation === this.#generation && !!this.#company
    try {
      const pref = await this.#request(
        `/town/api/avatar/preference?company=${encodeURIComponent(this.#company)}`,
      )
      if (!active()) return
      if (
        !object(pref) ||
        typeof pref.consent !== 'boolean' ||
        typeof pref.appearance !== 'string' ||
        !appearances.includes(pref.appearance) ||
        Object.keys(pref).some((k) => !['consent', 'appearance'].includes(k))
      )
        throw new Error('invalid')
      this.#view.available = true
      this.#view.preference = { consent: pref.consent, appearance: pref.appearance as Appearance }
      if (pref.consent)
        await this.#request('/town/api/avatar/presence', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json', 'X-Town-Request': '1' },
          body: JSON.stringify({
            company: this.#company,
            department: this.#department,
            emote: this.#emote,
          }),
        })
      if (!active()) return
      const raw = await this.#request(
        `/town/api/avatar/presence?company=${encodeURIComponent(this.#company)}`,
      )
      if (!active()) return
      if (
        !Array.isArray(raw) ||
        raw.length > 200 ||
        !raw.every(
          (p) =>
            object(p) &&
            Object.keys(p).length === 4 &&
            typeof p.id === 'string' &&
            /^[0-9A-HJKMNP-TV-Z]{26}$/.test(p.id) &&
            typeof p.department === 'string' &&
            departments.includes(p.department) &&
            typeof p.emote === 'string' &&
            ['idle', 'wave', 'hello', 'read', 'type', 'carry'].includes(p.emote) &&
            typeof p.appearance === 'string' &&
            appearances.includes(p.appearance),
        )
      )
        throw new Error('invalid')
      this.#view.presences = raw.map((p) => ({
        id: p.id,
        department: p.department,
        emote: p.emote,
        appearance: p.appearance,
      }))
      this.#view.error = false
      this.#timer = setTimeout(() => {
        void this.#refresh(generation)
      }, 30000)
    } catch (err) {
      if (!active()) return
      this.#view.presences = []
      this.#view.error = !(err instanceof Error && err.message === 'disabled')
      if (err instanceof Error && ['disabled', 'denied'].includes(err.message))
        this.#view.available = false
    }
    if (active()) this.#emit()
  }
  async setConsent(consent: boolean): Promise<void> {
    await this.#change(consent, this.#view.preference.appearance)
  }
  async setAppearance(appearance: Appearance): Promise<void> {
    await this.#change(this.#view.preference.consent, appearance)
  }
  async #change(consent: boolean, appearance: Appearance): Promise<void> {
    if (
      !this.#view.available ||
      this.#view.busy ||
      !this.#company ||
      !appearances.includes(appearance)
    )
      return
    const generation = ++this.#generation
    this.#abort.abort()
    this.#abort = new AbortController()
    this.#view.busy = true
    this.#view.preference = { consent, appearance }
    if (!consent) {
      this.#view.presences = []
      this.#view.preference.consent = false
    }
    clearTimeout(this.#timer)
    this.#emit()
    try {
      await this.#request('/town/api/avatar/preference', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json', 'X-Town-Request': '1' },
        body: JSON.stringify({ company: this.#company, consent, appearance }),
      })
      if (generation !== this.#generation) return
      await this.#refresh(generation)
    } catch {
      if (generation === this.#generation) {
        this.#view.error = true
        this.#view.preference.consent = false
        this.#view.presences = []
      }
    }
    if (generation === this.#generation) {
      this.#view.busy = false
      this.#emit()
    }
  }
}
