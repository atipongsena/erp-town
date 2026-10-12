import assert from 'node:assert/strict'
import { test } from 'node:test'
import { demoRounds, startOutboxPump } from './live-demo.ts'

test('live demo accepts only bounded rounds and no environment overrides', () => {
  assert.equal(demoRounds([]), 3)
  assert.equal(demoRounds(['--rounds=1']), 1)
  assert.equal(demoRounds(['--rounds=2']), 2)
  for (const args of [
    ['--rounds=0'],
    ['--rounds=4'],
    ['--site=town.localhost'],
    ['--rounds=1', '--rounds=3'],
  ])
    assert.throws(() => demoRounds(args))
})

test('outbox pump is serial and stop awaits an in-flight publication', async () => {
  let calls = 0
  let active = 0
  let maximum = 0
  let release: (() => void) | undefined
  const pending = new Promise<void>((resolve) => {
    release = resolve
  })
  const pump = startOutboxPump(
    async () => {
      calls++
      active++
      maximum = Math.max(maximum, active)
      await pending
      active--
    },
    () => assert.fail('unexpected publication failure'),
    1,
  )
  assert.equal(calls, 1)
  let stopped = false
  const stopping = pump.stop().then(() => {
    stopped = true
  })
  await new Promise((resolve) => setTimeout(resolve, 10))
  assert.equal(stopped, false)
  release?.()
  await stopping
  assert.equal(active, 0)
  assert.equal(maximum, 1)
  assert.equal(calls, 1)
})

test('outbox pump halts after an error without a silent retry', async () => {
  let calls = 0
  let failures = 0
  const pump = startOutboxPump(
    async () => {
      calls++
      throw new Error('publication failed')
    },
    () => {
      failures++
    },
    1,
  )
  await new Promise((resolve) => setTimeout(resolve, 10))
  await pump.stop()
  assert.equal(calls, 1)
  assert.equal(failures, 1)
})
