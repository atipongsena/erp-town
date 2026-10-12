import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import { validateEvent } from '../src/index.ts'

const base = JSON.parse(
  readFileSync(join(import.meta.dirname, '../fixtures/valid/system.error.json'), 'utf8'),
)
const event = () => ({
  ...structuredClone(base),
  type: 'agent.run.started',
  source: 'town/agents',
  actor: { kind: 'agent', id: 'helper', on_behalf_of: null },
  subject: { doctype: 'Town Agent Run', name: 'RUN-1' },
  data: { agent_key: 'helper', state: 'running', code: 'OK' },
})

describe('Go-owned agent event boundary', () => {
  it('accepts coarse, role-scoped AI run metadata', () => {
    expect(validateEvent(event())).toEqual({ valid: true })
  })
  it('accepts paired coarse work metadata but rejects forged fields and unknown activity', () => {
    const work = event()
    Object.assign(work.data, { department: 'warehouse', activity: 'working' })
    expect(validateEvent(work).valid).toBe(true)
    Object.assign(work.data, { department: 'payroll' })
    expect(validateEvent(work).valid).toBe(false)
    Object.assign(work.data, { department: 'warehouse', activity: 'delivered' })
    expect(validateEvent(work).valid).toBe(false)
    const unpaired = event()
    Object.assign(unpaired.data, { department: 'warehouse' })
    expect(validateEvent(unpaired).valid).toBe(false)
    const queued = event()
    Object.assign(queued.data, { department: 'warehouse', activity: 'working', state: 'queued' })
    expect(validateEvent(queued).valid).toBe(false)
  })
  it.each(['Sales Order', 'Delivery Note', 'Sales Invoice', 'Payment Entry'])(
    'rejects an agent event in the ERP sequence namespace %s',
    (doctype) => {
      const value = event()
      value.subject.doctype = doctype
      expect(validateEvent(value).valid).toBe(false)
    },
  )
  it('rejects wrong producer, public scope and human attribution', () => {
    const source = event()
    source.source = 'erp/town'
    expect(validateEvent(source).valid).toBe(false)
    const scope = event()
    scope.visibility = { level: 'public', roles: [] }
    expect(validateEvent(scope).valid).toBe(false)
    const actor = event()
    actor.actor.kind = 'user'
    expect(validateEvent(actor).valid).toBe(false)
    const behalf = event()
    behalf.actor.on_behalf_of = 'human@test'
    expect(validateEvent(behalf).valid).toBe(false)
  })
  it.each(['prompt', 'amount', 'filename', 'credential', 'customer'])(
    'rejects private payload field %s',
    (field) => {
      const value = event()
      Object.assign(value.data, { [field]: 'private' })
      expect(validateEvent(value).valid).toBe(false)
    },
  )
})
