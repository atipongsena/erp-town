import { expect, test } from '@playwright/test'

test('a live task moves the same person to its department, stops on arrival and works only while active', async ({
  page,
}) => {
  test.setTimeout(90000)
  await page.emulateMedia({ reducedMotion: 'no-preference' })
  let actor = {
    id: 'staff-test',
    kind: 'human',
    department: 'hq',
    activity: 'idle',
    from_department: 'hq',
  }
  const writes: string[] = []
  page.on('request', (r) => {
    if (
      r.method() !== 'GET' &&
      /sales-orders|delivery-notes|sales-invoices|payment-entries|agent-runs/.test(r.url())
    )
      writes.push(r.url())
  })
  await page.route('**/town/api/me', (r) => r.fulfill({ json: { companies: ['ACME'] } }))
  await page.route('**/town/api/auth/token', (r) => r.fulfill({ json: { token: 'fixture' } }))
  await page.route('**/town/api/avatar/preference**', (r) =>
    r.fulfill({ json: { consent: false, appearance: 'human-blue' } }),
  )
  await page.route('**/town/api/avatar/presence**', (r) => r.fulfill({ json: [] }))
  await page.route('**/town/api/live-demo', (r) =>
    r.fulfill({
      json: {
        enabled: true,
        mode: 'erp-test',
        company: 'ACME',
        round: 1,
        stage: 'sales-review',
        phase: actor.activity,
        actors: [actor],
        documents: [],
      },
    }),
  )
  // Empty company snapshot: no document counts can invent people or transport.
  await page.routeWebSocket('**/town/ws', (s) =>
    s.onMessage(() => s.send(Buffer.from('0801120441434d451801', 'hex'))),
  )
  await page.goto('/?lang=en&liveDemo=1')
  await expect(page.getByRole('status')).toHaveText('Connected')
  const person = page.locator('[data-work-actor="staff-test"]')
  const canvas = page.locator('canvas[data-scene-ready="true"]')
  await expect(canvas).toBeVisible()
  await expect(person).toHaveAttribute('data-work-phase', 'idle')
  await page.waitForTimeout(2200)
  const idle = await canvas.screenshot()
  await page.waitForTimeout(700)
  expect((await canvas.screenshot()).equals(idle)).toBe(true)

  actor = { ...actor, department: 'shop', activity: 'travelling' }
  await expect(person).toHaveAttribute('data-work-phase', 'travelling')
  await expect(person).toHaveAttribute('data-work-department', 'shop')
  const walking = await canvas.screenshot()
  await page.waitForTimeout(800)
  expect((await canvas.screenshot()).equals(walking)).toBe(false)
  await expect(person).toHaveAttribute('data-work-phase', 'arrived', { timeout: 30000 })
  await page.waitForTimeout(700)
  const arrived = await canvas.screenshot()
  await page.waitForTimeout(700)
  expect((await canvas.screenshot()).equals(arrived)).toBe(true)

  actor = { ...actor, activity: 'working', from_department: 'shop' }
  await expect(person).toHaveAttribute('data-work-phase', 'working')
  await page.waitForTimeout(2200)
  const working = await canvas.screenshot()
  await page.waitForTimeout(800)
  expect((await canvas.screenshot()).equals(working)).toBe(false)

  actor = { ...actor, activity: 'idle' }
  await expect(person).toHaveAttribute('data-work-phase', 'idle')
  await page.waitForTimeout(2200)
  // The automatic overview transition can take extra frames on SwiftShader.
  // It must settle exactly, then stay frozen while the live endpoint keeps polling.
  await expect
    .poll(
      async () => {
        const finished = await canvas.screenshot()
        await page.waitForTimeout(700)
        return (await canvas.screenshot()).equals(finished)
      },
      { timeout: 15000 },
    )
    .toBe(true)
  expect(await person.count()).toBe(1)
  expect(writes).toEqual([])
})
