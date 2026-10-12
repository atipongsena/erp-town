import { readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import { createAjv, ENVELOPE_SCHEMA_ID, EVENT_TYPES, validateEvent } from '../src/index.ts'

const fixturesDir = join(import.meta.dirname, '..', 'fixtures')
const schemaDir = join(import.meta.dirname, '..', 'schema')

const list = (kind: 'valid' | 'invalid') =>
  readdirSync(join(fixturesDir, kind))
    .filter((f) => f.endsWith('.json'))
    .sort()
const load = (kind: 'valid' | 'invalid', file: string): unknown =>
  JSON.parse(readFileSync(join(fixturesDir, kind, file), 'utf8'))

describe('valid fixtures', () => {
  for (const f of list('valid')) {
    it(`valid: ${f}`, () => {
      expect(validateEvent(load('valid', f))).toEqual({ valid: true })
    })
  }
})

// The error each invalid fixture is there to provoke (so a fixture cannot pass by failing for an
// unrelated reason). Every file in fixtures/invalid must have an entry.
const EXPECTED_ERROR: Record<string, string> = {
  'cancelled-with-party.json': '/data must NOT have additional properties (party)',
  'envelope-extra-field.json': '(root) must NOT have additional properties (extra)',
  'id-not-ulid.json': '/id must match pattern',
  'missing-id.json': "(root) must have required property 'id'",
  'public-event-with-grand-total.json': '/data property name must be valid (grand_total)',
  'roles-visibility-empty-roles.json': '/visibility/roles must NOT have fewer than 1 items',
  'sales-order-unknown-data-field.json':
    '/data must NOT have additional properties (customer_name)',
  'seq-zero.json': '/seq must be >= 1',
  'time-without-offset.json': '/time must match',
  'unknown-event-type.json': '/type must be equal to one of the allowed values',
  'user-actor-with-on-behalf-of.json': '/actor/on_behalf_of must be null',
}

describe('invalid fixtures', () => {
  for (const f of list('invalid')) {
    it(`invalid: ${f}`, () => {
      const result = validateEvent(load('invalid', f))
      expect(result.valid).toBe(false)
      expect(EXPECTED_ERROR[f], `add ${f} to EXPECTED_ERROR`).toBeDefined()
      if (!result.valid) expect(result.errors.join('\n')).toContain(EXPECTED_ERROR[f])
    })
  }
})

describe('coverage', () => {
  it('covers every event type with a valid fixture', () => {
    const validTypes = list('valid').map((f) => (load('valid', f) as { type: string }).type)
    expect(new Set(validTypes)).toEqual(new Set(EVENT_TYPES))
  })

  it('has exactly one data schema file per event type', () => {
    const files = readdirSync(join(schemaDir, 'events'))
      .filter((f) => f.endsWith('.json'))
      .map((f) => f.replace(/\.json$/, ''))
    expect(new Set(files)).toEqual(new Set(EVENT_TYPES))
  })

  it('exposes the ERP and approved Go-owned AI event types', () => {
    expect([...EVENT_TYPES].sort()).toEqual(
      [
        'erp.sales_order.submitted',
        'erp.sales_order.cancelled',
        'erp.delivery_note.submitted',
        'erp.delivery_note.cancelled',
        'erp.sales_invoice.submitted',
        'erp.sales_invoice.cancelled',
        'erp.sales_invoice.overdue',
        'erp.payment_entry.submitted',
        'erp.payment_entry.cancelled',
        'erp.stock.below_reorder',
        'workflow.approval.requested',
        'workflow.approval.decided',
        'system.error',
        'agent.run.started',
        'agent.run.completed',
        'agent.run.needs_human',
        'agent.run.failed',
        'agent.progress.updated',
        'dev.task.started',
        'dev.task.updated',
        'dev.task.completed',
        'dev.task.failed',
      ].sort(),
    )
  })
})

describe('money rule', () => {
  const ajv = createAjv()
  const money = ajv.getSchema(`${ENVELOPE_SCHEMA_ID}#/$defs/money`)
  const noMoneyInPublic = ajv.getSchema(`${ENVELOPE_SCHEMA_ID}#/$defs/no_money_in_public`)
  if (!money || !noMoneyInPublic) throw new Error('money definitions missing from envelope $defs')

  it('defines a shared money shape', () => {
    expect(money({ amount: 10700, currency: 'THB' })).toBe(true)
    expect(money({ amount: '10700', currency: 'THB' })).toBe(false)
    expect(money({ amount: 10700, currency: 'baht' })).toBe(false)
  })

  it('rejects reserved money fields in public events only', () => {
    const data = { grand_total: { amount: 10700, currency: 'THB' } }
    expect(noMoneyInPublic({ visibility: { level: 'public', roles: [] }, data })).toBe(false)
    expect(
      noMoneyInPublic({ visibility: { level: 'roles', roles: ['Accounts User'] }, data }),
    ).toBe(true)
    expect(noMoneyInPublic({ visibility: { level: 'private', roles: [] }, data })).toBe(true)
    expect(
      noMoneyInPublic({ visibility: { level: 'public', roles: [] }, data: { party: 'C' } }),
    ).toBe(true)
  })
})
