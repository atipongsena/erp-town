// The Frappe app validates events against a generated copy of schema/ + fixtures/
// (frappe-apps/erp_town/erp_town/events/schema/). This test fails when that copy is stale:
// run `pnpm schema:gen` from the repo root after editing schema/ or fixtures/.
import { readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

const pkgDir = join(import.meta.dirname, '..')
const frappeCopy = join(
  pkgDir,
  '..',
  '..',
  'frappe-apps',
  'erp_town',
  'erp_town',
  'events',
  'schema',
)

const DIRS: [source: string, copy: string][] = [
  [join(pkgDir, 'schema'), frappeCopy],
  [join(pkgDir, 'schema', 'events'), join(frappeCopy, 'events')],
  [join(pkgDir, 'fixtures', 'valid'), join(frappeCopy, 'fixtures', 'valid')],
  [join(pkgDir, 'fixtures', 'invalid'), join(frappeCopy, 'fixtures', 'invalid')],
]

const jsonFiles = (dir: string) =>
  readdirSync(dir)
    .filter((f) => f.endsWith('.json'))
    .sort()

describe('generated Frappe copy of the contract', () => {
  for (const [source, copy] of DIRS) {
    const label = copy.slice(frappeCopy.length).replace(/\\/g, '/') || '/'
    it(`${label} has the same JSON files`, () => {
      expect(jsonFiles(copy)).toEqual(jsonFiles(source))
    })
    for (const f of jsonFiles(source)) {
      it(`${label}/${f} is byte-identical`, () => {
        const expected = readFileSync(join(source, f))
        const actual = readFileSync(join(copy, f))
        expect(actual.equals(expected), `${f} differs: run pnpm schema:gen`).toBe(true)
      })
    }
  }
})
