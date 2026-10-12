import type { Entity } from '@erp-town/world-proto'
import { describe, expect, it } from 'vitest'
import { classicPath, displayFacts } from '../src/world-view.ts'

function entity(kind: string, doctype: string, name: string, data: unknown): Entity {
  return {
    $typeName: 'world.v1.Entity',
    key: { $typeName: 'world.v1.EntityKey', kind, doctype, name, scope: '' },
    version: 1n,
    seq: 1n,
    eventType: '',
    eventId: '',
    actorId: '',
    correlationId: '',
    time: '',
    dataJson: new TextEncoder().encode(JSON.stringify(data)),
  }
}

describe('AI activity display', () => {
  it.each(['queued', 'running', 'completed', 'needs_human', 'failed', 'cancelled'])(
    'shows %s at IT without copying payloads or offering ERP document actions',
    (state) => {
      const ai = entity('agent_run', 'Town Agent Run', 'RUN-1', {
        state,
        code: 'OK',
        agent_key: 'helper',
        prompt: 'private',
        amount: '123',
        on_behalf_of: 'human@test',
      })
      const facts = displayFacts([ai])
      expect(facts).toHaveLength(1)
      expect(facts[0]?.department).toBe('it')
      expect(facts[0]?.status).toBe(`ai-${state}`)
      expect(JSON.stringify(facts)).not.toContain('private')
      expect(JSON.stringify(facts)).not.toContain('human@test')
      expect(classicPath('Town Agent Run', 'RUN-1')).toBeUndefined()
    },
  )
  it('shows only AI progress and rejects a human progress identity', () => {
    const ai = entity('agent_progress', 'Town Agent', 'helper', { successful_runs: 5 })
    expect(displayFacts([ai])[0]?.status).toBe('ai-progress')
    expect(displayFacts([ai])[0]?.aiProgress).toEqual({ completed: 5, xp: '50', level: 2 })
    expect(ai.key).toBeDefined()
    if (ai.key) ai.key.doctype = 'User'
    expect(displayFacts([ai])).toEqual([])
  })
  it.each(['started', 'running', 'completed', 'failed'])(
    'shows development %s as IT activity without an ERP link',
    (state) => {
      const dev = entity('dev_task', 'Town Dev Task', 'codex/TASK-1', {
        provider: 'codex',
        state,
        sequence: 1,
        prompt: 'private',
        path: 'private/file',
      })
      const facts = displayFacts([dev])
      expect(facts[0]?.status).toBe(`dev-${state}`)
      expect(facts[0]?.department).toBe('it')
      expect(JSON.stringify(facts)).not.toContain('private')
      expect(classicPath('Town Dev Task', 'codex/TASK-1')).toBeUndefined()
    },
  )
})
