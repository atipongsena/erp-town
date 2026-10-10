import { create, fromBinary, toBinary } from '@bufbuild/protobuf'
import { describe, expect, it } from 'vitest'
import { ClientFrameSchema, FrameKind, ServerFrameSchema } from '../src/index.ts'

describe('world binary contract', () => {
  it('matches the Go JOIN golden', () => {
    const golden = '0a100a05746f6b656e120741434d452d5448'
    const frame = create(ClientFrameSchema, {
      message: { case: 'join', value: { jwt: 'token', company: 'ACME-TH' } },
    })
    expect(Buffer.from(toBinary(ClientFrameSchema, frame)).toString('hex')).toBe(golden)
    expect(fromBinary(ClientFrameSchema, Buffer.from(golden, 'hex'))).toEqual(frame)
  })

  it('preserves uint64 versions beyond JavaScript number precision and exact payload bytes', () => {
    const frame = create(ServerFrameSchema, {
      kind: FrameKind.PATCH,
      company: 'ACME-TH',
      version: (1n << 54n) + 7n,
      baseVersion: (1n << 54n) + 6n,
      upserts: [
        {
          key: { doctype: 'Sales Order', name: 'SO-1', kind: 'document' },
          version: (1n << 54n) + 7n,
          eventType: 'erp.sales_order.submitted',
          dataJson: new TextEncoder().encode('{"status":"Submitted"}'),
        },
      ],
      removes: [{ doctype: 'Item', name: 'ITEM-1', kind: 'stock', scope: 'WH-1' }],
      resumeToken: 'resume',
    })
    expect(fromBinary(ServerFrameSchema, toBinary(ServerFrameSchema, frame))).toEqual(frame)
    expect(() => fromBinary(ServerFrameSchema, new Uint8Array([0x0a, 0xff]))).toThrow()
  })
})
