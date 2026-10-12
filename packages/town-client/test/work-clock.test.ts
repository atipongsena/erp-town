import { expect, it } from 'vitest'
import { WorkClock } from '../src/work-clock.ts'

it('uses real foreground time even below 20 FPS and drops hidden/paused time', () => {
  const clock = new WorkClock()
  expect(clock.step(1000, true)).toBe(0)
  expect(clock.step(1250, true)).toBe(0.25)
  expect(clock.step(1500, true)).toBe(0.25)
  expect(clock.step(1600, false)).toBe(0)
  expect(clock.step(100000, true)).toBe(0)
  expect(clock.step(100250, true)).toBe(0.25)
  clock.reset()
  expect(clock.step(200000, true)).toBe(0)
  expect(clock.step(200250, true)).toBe(0.25)
  expect(clock.step(300000, true)).toBe(0.5)
  expect(clock.step(299000, true)).toBe(0)
})
