import { create, toBinary } from '@bufbuild/protobuf'
import { describe, expect, it } from 'vitest'
import { WorldClientState, WorldSyncError } from '../src/client-state.ts'
import {
  EntityKeySchema,
  EntitySchema,
  FrameKind,
  ServerFrameSchema,
} from '../src/gen/world/v1/world_pb.ts'

const key = (name = 'SO-1', kind = 'document', scope = '') =>
  create(EntityKeySchema, { doctype: 'Sales Order', name, kind, scope })
const entity = (version = 1n, name = 'SO-1', kind = 'document', scope = '') =>
  create(EntitySchema, {
    key: key(name, kind, scope),
    version,
    eventType: 'world.projection.document',
    dataJson: new TextEncoder().encode('{"status":"Submitted"}'),
  })
const snapshot = (version = 1n) =>
  create(ServerFrameSchema, {
    company: 'ACME-TH',
    kind: FrameKind.SNAPSHOT,
    version,
    upserts: [entity(version)],
    resumeToken: 'resume-1',
  })
const patch = (baseVersion = 1n, version = 2n) =>
  create(ServerFrameSchema, {
    company: 'ACME-TH',
    kind: FrameKind.PATCH,
    version,
    baseVersion,
    resumeToken: 'resume-2',
  })

describe('client world protocol state', () => {
  it('consumes binary snapshots and a coalesced patch without losing uint64 precision', () => {
    const state = new WorldClientState('ACME-TH')
    const version = (1n << 54n) + 7n
    state.acceptBinary(toBinary(ServerFrameSchema, snapshot(version)))
    const next = patch(version, version + 15n)
    next.upserts = [entity(version + 15n)]
    state.accept(next)
    expect(state.ready).toBe(true)
    expect(state.version).toBe(version + 15n)
    expect(state.entities()[0]?.version).toBe(version + 15n)
    expect(state.reconnectCursor()).toEqual({ lastVersion: version + 15n, resumeToken: 'resume-2' })
  })

  it('accepts an equal-version no-op resume and rotates only the memory cursor', () => {
    const state = new WorldClientState('ACME-TH')
    state.accept(snapshot())
    state.accept(patch(1n, 1n))
    expect(state.entities()).toHaveLength(1)
    expect(state.reconnectCursor()).toEqual({ lastVersion: 1n, resumeToken: 'resume-2' })
  })

  it('keeps document/approval and stock warehouse scopes separate', () => {
    const state = new WorldClientState('ACME-TH')
    const frame = snapshot(4n)
    frame.upserts = [
      entity(1n),
      entity(2n, 'SO-1', 'approval'),
      entity(3n, 'ITEM-1', 'stock', 'WH-1'),
      entity(4n, 'ITEM-1', 'stock', 'WH-2'),
    ]
    state.accept(frame)
    const next = patch(4n, 5n)
    next.removes = [key('SO-1', 'approval'), key('ITEM-1', 'stock', 'WH-1')]
    state.accept(next)
    expect(
      state.entities().map((value) => [value.key?.name, value.key?.kind, value.key?.scope]),
    ).toEqual([
      ['ITEM-1', 'stock', 'WH-2'],
      ['SO-1', 'document', ''],
    ])
  })

  it('replaces the entire view on a fresh snapshot, including lost private visibility', () => {
    const state = new WorldClientState('ACME-TH')
    const initial = snapshot(9n)
    initial.upserts.push(entity(8n, 'SO-1', 'approval'))
    state.accept(initial)
    state.accept(snapshot(2n)) // A rebuilt room may have a new, lower version.
    expect(state.entities()).toHaveLength(1)
    expect(state.version).toBe(2n)
    const empty = snapshot(0n)
    empty.upserts = []
    state.accept(empty)
    expect(state.ready).toBe(true)
    expect(state.entities()).toEqual([])
    expect(state.version).toBe(0n)
  })

  it('owns payload bytes, key objects and reconnect cursor values', () => {
    const state = new WorldClientState('ACME-TH')
    const incoming = snapshot()
    state.accept(incoming)
    const first = incoming.upserts[0]
    if (!first?.key) throw new Error('Fixture key missing')
    first.key.name = 'MUTATED'
    first.dataJson.fill(0)
    const output = state.entities()[0]
    if (!output?.key) throw new Error('Stored key missing')
    output.key.name = 'CHANGED'
    output.dataJson.fill(1)
    const cursor = state.reconnectCursor()
    if (!cursor) throw new Error('Cursor missing')
    cursor.resumeToken = 'changed'
    const stored = state.entities()[0]
    expect(stored?.key?.name).toBe('SO-1')
    expect(new TextDecoder().decode(stored?.dataJson)).toBe('{"status":"Submitted"}')
    expect(state.reconnectCursor()?.resumeToken).toBe('resume-1')
  })

  it('uses collision-free composite keys even when document identifiers contain separators', () => {
    const state = new WorldClientState('ACME-TH')
    const frame = snapshot(2n)
    const first = entity(1n)
    const second = entity(2n)
    first.key = create(EntityKeySchema, { doctype: 'A/B', name: 'C', kind: 'document' })
    second.key = create(EntityKeySchema, { doctype: 'A', name: 'B/C', kind: 'document' })
    frame.upserts = [first, second]
    state.accept(frame)
    expect(state.entities()).toHaveLength(2)
  })

  it('clears stale state after a version gap, then accepts only a fresh snapshot', () => {
    const state = new WorldClientState('ACME-TH')
    state.accept(snapshot())
    expect(() => state.accept(patch(3n, 4n))).toThrow(WorldSyncError)
    expect(state.ready).toBe(false)
    expect(state.entities()).toEqual([])
    expect(state.reconnectCursor()).toBeUndefined()
    expect(() => state.accept(patch(0n, 1n))).toThrow('snapshot_required')
    state.accept(snapshot(7n))
    expect(state.ready).toBe(true)
    expect(state.version).toBe(7n)
  })

  it('requires a snapshot before the initial patch and clears on explicit session reset', () => {
    const state = new WorldClientState('ACME-TH')
    expect(() => state.accept(patch(0n, 1n))).toThrow('snapshot_required')
    state.accept(snapshot())
    state.reset()
    expect(state.ready).toBe(false)
    expect(state.version).toBe(0n)
    expect(state.entities()).toEqual([])
    expect(state.reconnectCursor()).toBeUndefined()
    expect(() => new WorldClientState(' ')).toThrow()
  })

  it.each([
    ['company_mismatch', () => ({ ...patch(), company: 'OTHER' })],
    ['invalid_frame', () => ({ ...patch(), kind: FrameKind.UNSPECIFIED })],
    ['invalid_frame', () => ({ ...patch(), kind: 99 })],
    ['invalid_frame', () => ({ ...patch(), upserts: [create(EntitySchema, { version: 2n })] })],
    ['invalid_frame', () => ({ ...patch(), upserts: [entity(3n)] })],
    ['invalid_frame', () => ({ ...patch(), upserts: [entity(0n)] })],
    ['invalid_frame', () => ({ ...patch(), upserts: [entity(2n, 'SO-1', 'unknown')] })],
    ['invalid_frame', () => ({ ...patch(), upserts: [entity(2n), entity(2n)] })],
    ['invalid_frame', () => ({ ...patch(), removes: [key(), key()] })],
    ['invalid_frame', () => ({ ...patch(), removes: [key()], upserts: [entity(2n)] })],
    ['invalid_frame', () => ({ ...snapshot(), baseVersion: 1n })],
    ['invalid_frame', () => ({ ...snapshot(), removes: [key()] })],
    ['version_regression', () => patch(1n, 0n)],
    ['version_regression', () => ({ ...patch(), upserts: [entity(1n)] })],
    ['version_regression', () => ({ ...patch(1n, 1n), removes: [key()] })],
  ])('fails closed on %s without keeping old visible data', (reason, invalidFrame) => {
    const state = new WorldClientState('ACME-TH')
    state.accept(snapshot())
    expect(() => state.accept(invalidFrame())).toThrow(reason)
    expect(state.ready).toBe(false)
    expect(state.entities()).toEqual([])
    expect(state.reconnectCursor()).toBeUndefined()
  })

  it('rejects malformed protobuf and erases the previous view without echoing bytes', () => {
    const state = new WorldClientState('ACME-TH')
    state.accept(snapshot())
    expect(() => state.acceptBinary(new Uint8Array([0x0a, 0xff]))).toThrow('invalid_binary')
    expect(state.ready).toBe(false)
    expect(state.entities()).toEqual([])
    expect(state.reconnectCursor()).toBeUndefined()
  })
})
