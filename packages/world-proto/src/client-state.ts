import { clone, fromBinary } from '@bufbuild/protobuf'
import {
  type Entity,
  type EntityKey,
  EntitySchema,
  FrameKind,
  type ServerFrame,
  ServerFrameSchema,
} from './gen/world/v1/world_pb.ts'

export type WorldSyncFailure =
  | 'invalid_binary'
  | 'invalid_frame'
  | 'company_mismatch'
  | 'snapshot_required'
  | 'base_mismatch'
  | 'version_regression'

// Diagnostics contain only a fixed code, never a token or incoming payload.
export class WorldSyncError extends Error {
  constructor(readonly reason: WorldSyncFailure) {
    super(`World sync: ${reason}`)
    this.name = 'WorldSyncError'
  }
}

export type ReconnectCursor = { lastVersion: bigint; resumeToken: string }

function identity(key: EntityKey | undefined): string {
  if (
    !key?.doctype.trim() ||
    !key.name.trim() ||
    ![
      'document',
      'approval',
      'overdue',
      'stock',
      'error',
      'agent_run',
      'agent_progress',
      'dev_task',
    ].includes(key.kind) ||
    (key.kind === 'agent_run' && key.doctype !== 'Town Agent Run') ||
    (key.kind === 'agent_progress' && key.doctype !== 'Town Agent') ||
    (key.kind === 'dev_task' && key.doctype !== 'Town Dev Task') ||
    (key.kind === 'stock' ? !key.scope.trim() : key.scope !== '')
  )
    throw new WorldSyncError('invalid_frame')
  return JSON.stringify([key.doctype, key.name, key.kind, key.scope])
}

/** In-memory, company-bound view of C's already filtered world facts.
 * Payloads remain opaque; consumers receive owned protobuf objects/bytes.
 * Any invalid frame erases the view and requires a full snapshot.
 */
export class WorldClientState {
  readonly company: string
  #version = 0n
  #ready = false
  #resumeToken = ''
  #entities = new Map<string, Entity>()

  constructor(company: string) {
    if (!company.trim()) throw new Error('World sync: company is required')
    this.company = company
  }

  get version(): bigint {
    return this.#version
  }

  get ready(): boolean {
    return this.#ready
  }

  reset(): void {
    this.#ready = false
    this.#version = 0n
    this.#resumeToken = ''
    this.#entities.clear()
  }

  entities(): Entity[] {
    return [...this.#entities.entries()]
      .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
      .map(([, entity]) => clone(EntitySchema, entity))
  }

  reconnectCursor(): ReconnectCursor | undefined {
    if (!this.#ready || !this.#resumeToken) return undefined
    return { lastVersion: this.#version, resumeToken: this.#resumeToken }
  }

  acceptBinary(bytes: Uint8Array): void {
    let frame: ServerFrame
    try {
      frame = fromBinary(ServerFrameSchema, bytes)
    } catch {
      this.reset()
      throw new WorldSyncError('invalid_binary')
    }
    this.accept(frame)
  }

  accept(frame: ServerFrame): void {
    try {
      this.#apply(frame)
    } catch (error) {
      this.reset()
      if (error instanceof WorldSyncError) throw error
      throw new WorldSyncError('invalid_frame')
    }
  }

  #apply(frame: ServerFrame): void {
    if (frame.company !== this.company) throw new WorldSyncError('company_mismatch')
    const snapshot = frame.kind === FrameKind.SNAPSHOT
    if (!snapshot && frame.kind !== FrameKind.PATCH) throw new WorldSyncError('invalid_frame')
    if (frame.version < 0n || frame.baseVersion < 0n) throw new WorldSyncError('invalid_frame')
    if (snapshot) {
      if (frame.baseVersion !== 0n || frame.removes.length)
        throw new WorldSyncError('invalid_frame')
    } else {
      if (!this.#ready) throw new WorldSyncError('snapshot_required')
      if (frame.baseVersion !== this.#version) throw new WorldSyncError('base_mismatch')
      if (
        frame.version < this.#version ||
        (frame.version === this.#version && (frame.upserts.length || frame.removes.length))
      )
        throw new WorldSyncError('version_regression')
    }

    const mutations = new Set<string>()
    const next = snapshot ? new Map<string, Entity>() : new Map(this.#entities)
    for (const entity of frame.upserts) {
      const id = identity(entity.key)
      if (
        mutations.has(id) ||
        entity.version <= 0n ||
        entity.version > frame.version ||
        entity.seq < 0n
      )
        throw new WorldSyncError('invalid_frame')
      const previous = snapshot ? undefined : this.#entities.get(id)
      if (previous && entity.version <= previous.version)
        throw new WorldSyncError('version_regression')
      mutations.add(id)
      next.set(id, clone(EntitySchema, entity))
    }
    for (const key of frame.removes) {
      const id = identity(key)
      if (mutations.has(id)) throw new WorldSyncError('invalid_frame')
      mutations.add(id)
      next.delete(id)
    }
    this.#entities = next
    this.#version = frame.version
    this.#resumeToken = frame.resumeToken
    this.#ready = true
  }
}
