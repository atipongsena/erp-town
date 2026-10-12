import { expect, test } from '@playwright/test'

test('live demo keeps concurrent actors and one circulating truck after ERP completes, pauses and clears safely', async ({
  page,
}) => {
  await page.emulateMedia({ reducedMotion: 'no-preference' })
  await page.addInitScript(() => {
    Object.defineProperty(document, 'visibilityState', {
      get: () => Reflect.get(window, 'testVisibility') ?? 'visible',
      configurable: true,
    })
  })
  let failed = false
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
        motion_mode: 'loop',
        company: 'ACME',
        round: 1,
        stage: failed ? 'failed' : 'completed',
        phase: failed ? 'failed' : 'idle',
        ...(failed ? { error: 'ERP_STEP_FAILED' } : {}),
        documents: [],
        actors: [
          { id: 'demo-shop', kind: 'human', department: 'hq', activity: 'idle' },
          { id: 'demo-warehouse', kind: 'human', department: 'hq', activity: 'idle' },
          { id: 'demo-bank', kind: 'human', department: 'hq', activity: 'idle' },
          { id: 'demo-assistant', kind: 'agent', department: 'it', activity: 'idle' },
        ],
      },
    }),
  )
  await page.routeWebSocket('**/town/ws', (s) =>
    s.onMessage(() => s.send(Buffer.from('0801120441434d451801', 'hex'))),
  )
  await page.goto('/?lang=en&liveDemo=1')
  await expect(page.getByRole('status')).toHaveText('Connected')
  await expect(
    page.getByText('Concurrent demo motion · loops without repeating ERP transactions', {
      exact: true,
    }),
  ).toBeVisible()
  await expect(page.locator('[data-work-actor]')).toHaveCount(4)
  await expect
    .poll(() => page.locator('[data-work-phase=travelling], [data-work-phase=working]').count())
    .toBeGreaterThanOrEqual(3)
  const truck = page.locator('[data-delivery-trip=demo-traffic]')
  await expect(truck).toBeVisible()
  const distance = await truck.getAttribute('data-delivery-distance')
  await expect.poll(() => truck.getAttribute('data-delivery-distance')).not.toBe(distance)
  await page.getByRole('checkbox', { name: 'Town animation', exact: true }).uncheck()
  await page.waitForTimeout(600)
  const paused = await truck.getAttribute('data-delivery-distance')
  await page.waitForTimeout(1200)
  expect(await truck.getAttribute('data-delivery-distance')).toBe(paused)
  await page.getByRole('checkbox', { name: 'Town animation', exact: true }).check()
  await expect.poll(() => truck.getAttribute('data-delivery-distance')).not.toBe(paused)
  await page.evaluate(() => {
    Reflect.set(window, 'testVisibility', 'hidden')
    document.dispatchEvent(new Event('visibilitychange'))
  })
  await page.waitForTimeout(600)
  const hidden = await truck.getAttribute('data-delivery-distance')
  await page.waitForTimeout(1200)
  expect(await truck.getAttribute('data-delivery-distance')).toBe(hidden)
  await page.evaluate(() => {
    Reflect.set(window, 'testVisibility', 'visible')
    document.dispatchEvent(new Event('visibilitychange'))
  })
  await expect.poll(() => truck.getAttribute('data-delivery-distance')).not.toBe(hidden)
  await page.getByRole('combobox', { name: 'Recent states', exact: true }).selectOption('1')
  await expect(truck).toHaveCount(0)
  await expect(page.locator('[data-work-actor]')).toHaveCount(0)
  await page.getByRole('button', { name: 'Back to live', exact: true }).click()
  await expect(truck).toBeVisible()
  failed = true
  await expect(page.getByRole('alert')).toContainText('ERP_STEP_FAILED')
  await expect(truck).toHaveCount(0)
  await expect(page.locator('[data-work-phase=idle]')).toHaveCount(4)
  await page.getByRole('button', { name: 'Disconnect', exact: true }).click()
  await expect(truck).toHaveCount(0)
  await expect(page.locator('[data-work-actor]')).toHaveCount(0)
  expect(writes).toEqual([])
})
