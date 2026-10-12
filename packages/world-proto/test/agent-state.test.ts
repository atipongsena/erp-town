import { create } from '@bufbuild/protobuf'
import { describe, expect, it } from 'vitest'
import { WorldClientState } from '../src/client-state.ts'
import { EntitySchema, FrameKind, ServerFrameSchema } from '../src/gen/world/v1/world_pb.ts'

describe('AI facts in the existing protobuf frame', () => {
  it.each([
    ['agent_run', 'Town Agent Run'],
    ['agent_progress', 'Town Agent'],
  ])('accepts a scoped %s fact and removes it on visibility refresh', (kind, doctype) => {
    const state = new WorldClientState('ACME')
    const entity = create(EntitySchema, {
      key: { kind, doctype, name: 'AI-1' },
      version: 1n,
      seq: 1n,
    })
    state.accept(
      create(ServerFrameSchema, {
        kind: FrameKind.SNAPSHOT,
        company: 'ACME',
        version: 1n,
        upserts: [entity],
      }),
    )
    expect(state.entities()).toHaveLength(1)
    if (!entity.key) throw new Error('AI fixture key missing')
    state.accept(
      create(ServerFrameSchema, {
        kind: FrameKind.PATCH,
        company: 'ACME',
        version: 2n,
        baseVersion: 1n,
        removes: [entity.key],
      }),
    )
    expect(state.entities()).toEqual([])
  })
  it('rejects AI kinds on financial document identities', () => {
    const state = new WorldClientState('ACME')
    const entity = create(EntitySchema, {
      key: { kind: 'agent_run', doctype: 'Sales Invoice', name: 'SI-1' },
      version: 1n,
      seq: 1n,
    })
    expect(() =>
      state.accept(
        create(ServerFrameSchema, {
          kind: FrameKind.SNAPSHOT,
          company: 'ACME',
          version: 1n,
          upserts: [entity],
        }),
      ),
    ).toThrow()
    expect(state.entities()).toEqual([])
  })
})
