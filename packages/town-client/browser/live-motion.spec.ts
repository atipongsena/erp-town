import { expect, test } from '@playwright/test'

// Produced with the pinned world descriptor and protobuf serializer: one running AI fact.
const snapshot =
  '0801120441434d4518012a420a270a0e546f776e204167656e742052756e120a41492d464958545552451a096167656e745f72756e100122137b227374617465223a2272756e6e696e67227d3001'

test('real reading animates, generic AI remains idle, and pause freezes the scene without writes', async ({
  page,
}) => {
  await page.emulateMedia({ reducedMotion: 'no-preference' })
  const loads = new Set<string>()
  const writes: string[] = []
  page.on('request', (r) => {
    if (r.url().includes('/art/town-human') || r.url().includes('/art/town-agent-hq'))
      loads.add(r.url().split('/').at(-1) ?? '')
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
  await page.route('**/town/api/avatar/presence**', (r) =>
    r.fulfill({
      json: [
        {
          id: '01JA0K6Y8Z9P3R4S5T6V7W8XA1',
          department: 'shop',
          emote: 'read',
          appearance: 'human-blue',
        },
      ],
    }),
  )
  await page.routeWebSocket('**/town/ws', (socket) =>
    socket.onMessage(() => socket.send(Buffer.from(snapshot, 'hex'))),
  )
  await page.goto('/?lang=en')
  await page.getByRole('button', { name: 'Connect to ERP', exact: true }).click()
  await expect(page.getByRole('status')).toHaveText('Connected')
  await expect.poll(() => loads.size).toBe(2)
  const canvas = page.locator('canvas[data-scene-ready="true"]')
  await expect(canvas).toBeVisible({ timeout: 45000 })
  await expect(page.locator('[data-work-actor="agent:AI-FIXTURE"]')).toHaveAttribute(
    'data-work-phase',
    'idle',
  )
  await expect(page.locator('[data-work-actor^="human:"]')).toHaveAttribute(
    'data-work-phase',
    'working',
  )
  const animation = page.getByRole('checkbox', { name: 'Town animation', exact: true })
  await expect(animation).toBeChecked()
  // Leave camera transitions behind; the reported reading clip keeps working.
  await page.waitForTimeout(1800)
  const first = await canvas.screenshot()
  await page.waitForTimeout(800)
  expect((await canvas.screenshot()).equals(first)).toBe(false)
  await animation.uncheck()
  await page.waitForTimeout(1800)
  const paused = await canvas.screenshot()
  await page.waitForTimeout(600)
  expect((await canvas.screenshot()).equals(paused)).toBe(true)
  await page.getByRole('button', { name: /^IT/ }).click()
  await expect(page.locator('.inspection').getByText('AI-FIXTURE', { exact: true })).toBeVisible()
  expect(writes).toEqual([])
})

test('reduced motion starts paused and can be enabled deliberately at phone width', async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/?lang=en')
  const control = page.getByRole('checkbox', { name: 'Town animation', exact: true })
  await expect(control).not.toBeChecked()
  await expect(
    page.getByText(
      'Reduced motion is enabled on this device. You can turn on Town animation if you wish.',
    ),
  ).toBeVisible()
  await control.check()
  await expect(control).toBeChecked()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  )
})
