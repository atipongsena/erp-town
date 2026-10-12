import { readFileSync } from 'node:fs'
import { create, toBinary } from '@bufbuild/protobuf'
import { describe, expect, it } from 'vitest'
import { ClientFrameSchema, FrameKind, ServerFrameSchema } from '../src/index.ts'

describe('D browser wire fixtures', () => {
  it('matches frames encoded by the shared protocol', () => {
    const snapshot = create(ServerFrameSchema, {
      company: 'ACME',
      kind: FrameKind.SNAPSHOT,
      version: 1n,
      resumeToken: 'fixture-resume',
      upserts: [
        {
          key: { doctype: 'Sales Order', name: 'SO-TEST', kind: 'document' },
          version: 1n,
          dataJson: new TextEncoder().encode('{"status":"Submitted","grand_total":"987654"}'),
        },
      ],
    })
    const join = create(ClientFrameSchema, {
      message: { case: 'join', value: { company: 'ACME', jwt: 'fixture-jwt' } },
    })
    const fixture = {
      snapshot: Buffer.from(toBinary(ServerFrameSchema, snapshot)).toString('hex'),
      join: Buffer.from(toBinary(ClientFrameSchema, join)).toString('hex'),
    }
    const actual = JSON.parse(
      readFileSync(
        new URL('../../town-client/browser/fixtures/wire.json', import.meta.url),
        'utf8',
      ),
    )
    expect(actual).toEqual(fixture)
  })
})
