import { expect, test } from '@playwright/test'

test('scheduled work resumes at the server deadline after a hidden reduced-motion interval', async ({
  page,
}) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  const start = Date.parse('2026-10-12T00:00:00Z')
  await page.clock.setFixedTime(new Date(start))
  await page.addInitScript(() => {
    Object.defineProperty(document, 'visibilityState', {
      get: () => Reflect.get(window, 'testVisibility') ?? 'visible',
      configurable: true,
    })
  })
  let activity = 'travelling'
  const writes: string[] = []
  page.on('request', (r) => {
    if (
      r.method() !== 'GET' &&
      /sales-orders|delivery-notes|sales-invoices|payment-entries/.test(r.url())
    )
      writes.push(r.url())
  })
  await page.route('**/town/api/me', (r) => r.fulfill({ json: { companies: ['ACME'] } }))
  await page.route('**/town/api/auth/token', (r) => r.fulfill({ json: { token: 'fixture' } }))
  await page.route('**/town/api/avatar/preference**', (r) =>
    r.fulfill({ json: { consent: false, appearance: 'human-blue' } }),
  )
  await page.route('**/town/api/live-demo', (r) =>
    r.fulfill({
      json: {
        enabled: true,
        mode: 'erp-test',
        company: 'ACME',
        round: 1,
        stage: 'sales-review',
        phase: activity,
        layout_version: 'town-walk-v1',
        stage_started_at: '2026-10-12T00:00:00Z',
        stage_duration_ms: 10000,
        documents: [],
        actors: [
          {
            id: 'scheduled',
            kind: 'human',
            from_department: 'hq',
            department: 'shop',
            activity,
            travel: { started_at: '2026-10-12T00:00:00Z', duration_ms: 10000 },
          },
        ],
      },
    }),
  )
  await page.routeWebSocket('**/town/ws', (s) =>
    s.onMessage(() => s.send(Buffer.from('0801120441434d451801', 'hex'))),
  )
  await page.goto('/?lang=en&liveDemo=1')
  const actor = page.locator('[data-work-actor=scheduled]')
  await expect(actor).toHaveAttribute('data-work-phase', 'travelling')
  await expect(
    page.getByRole('checkbox', { name: 'Town animation', exact: true }),
  ).not.toBeChecked()
  await page.evaluate(() => {
    Reflect.set(window, 'testVisibility', 'hidden')
    document.dispatchEvent(new Event('visibilitychange'))
  })
  await page.clock.setFixedTime(new Date(start + 10000))
  await page.evaluate(() => {
    Reflect.set(window, 'testVisibility', 'visible')
    document.dispatchEvent(new Event('visibilitychange'))
  })
  await expect(actor).toHaveAttribute('data-work-phase', 'arrived')
  activity = 'working'
  await expect(actor).toHaveAttribute('data-work-phase', 'working')
  await page.getByRole('button', { name: 'Disconnect', exact: true }).click()
  await expect(actor).toHaveCount(0)
  expect(writes).toEqual([])
})
