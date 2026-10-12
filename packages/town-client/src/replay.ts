import type { DisplayFact } from './world-view.ts'

export type ReplayPoint = { version: bigint; receivedAt: number }
export type ReplayView = {
  points: ReplayPoint[]
  selected: bigint | undefined
  facts: DisplayFact[]
}
export type ReplaySource = { company: string; phase: string; version: bigint; facts: DisplayFact[] }
export type ReplayLimits = { maxPoints: number; maxBytes: number; maxAgeMs: number }
type Entry = ReplayPoint & { facts: DisplayFact[]; bytes: number }
export const REPLAY_MAX_AGE_MS = 15 * 60 * 1000
const ownFact = (f: DisplayFact): DisplayFact => ({
  id: f.id,
  doctype: f.doctype,
  name: f.name,
  kind: f.kind,
  scope: f.scope,
  department: f.department,
  status: f.status,
})
const size = (facts: DisplayFact[]) => new TextEncoder().encode(JSON.stringify(facts)).byteLength

/** Stores display statuses from this live session only, never raw world payloads.
 * Removed identities are also removed from every historical point. This history
 * is not an ERP audit, durable replay, presence log or authorization authority.
 */
export class ReplayHistory {
  readonly #now: () => number
  readonly #limits: ReplayLimits
  #company = ''
  #lastVersion: bigint | undefined
  #entries: Entry[] = []
  #selected: bigint | undefined
  constructor(
    now: () => number = Date.now,
    limits: ReplayLimits = {
      maxPoints: 64,
      maxBytes: 1024 * 1024,
      maxAgeMs: REPLAY_MAX_AGE_MS,
    },
  ) {
    if (
      ![limits.maxPoints, limits.maxBytes, limits.maxAgeMs].every(
        (n) => Number.isSafeInteger(n) && n > 0,
      )
    )
      throw new Error('Replay: invalid retention limits')
    this.#now = now
    this.#limits = { ...limits }
  }
  observe(source: ReplaySource): void {
    if (source.phase !== 'live' || !source.company || source.version < 0n) {
      this.#clear()
      return
    }
    if (
      this.#company !== source.company ||
      (this.#lastVersion !== undefined && source.version < this.#lastVersion)
    )
      this.#clear()
    this.#company = source.company
    const visible = new Set(source.facts.map((f) => f.id))
    for (const point of this.#entries) {
      point.facts = point.facts.filter((f) => visible.has(f.id))
      point.bytes = size(point.facts)
    }
    if (this.#lastVersion !== source.version) {
      const facts = source.facts.map(ownFact)
      this.#entries.push({
        version: source.version,
        receivedAt: this.#now(),
        facts,
        bytes: size(facts),
      })
      this.#lastVersion = source.version
    }
    this.#prune()
  }
  select(version: bigint | undefined): void {
    this.#prune()
    this.#selected = this.#entries.some((p) => p.version === version) ? version : undefined
  }
  view(): ReplayView {
    this.#prune()
    return {
      points: this.#entries.map(({ version, receivedAt }) => ({ version, receivedAt })),
      selected: this.#selected,
      facts: (this.#entries.find((p) => p.version === this.#selected)?.facts ?? []).map(ownFact),
    }
  }
  #prune(): void {
    const cutoff = this.#now() - this.#limits.maxAgeMs
    let bytes = this.#entries.reduce((sum, p) => sum + p.bytes, 0)
    while (
      this.#entries.length &&
      (this.#entries.length > this.#limits.maxPoints ||
        bytes > this.#limits.maxBytes ||
        (this.#entries[0]?.receivedAt ?? Infinity) < cutoff)
    ) {
      bytes -= this.#entries.shift()?.bytes ?? 0
    }
    if (!this.#entries.some((p) => p.version === this.#selected)) this.#selected = undefined
  }
  #clear(): void {
    this.#company = ''
    this.#lastVersion = undefined
    this.#selected = undefined
    this.#entries = []
  }
}
