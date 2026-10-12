import { expect, test } from '@playwright/test'
import wire from './fixtures/wire.json' with { type: 'json' }

test('operations counts follow authorized live and replay states, then clear on disconnect', async ({
  page,
}) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  let send: (hex: string) => void = () => {}
  const writes: string[] = []
  page.on('request', (request) => {
    if (
      request.method() !== 'GET' &&
      /town\/api\/(sales-orders|delivery-notes|sales-invoices|payment-entries)/.test(request.url())
    )
      writes.push(request.url())
  })
  await page.route('**/town/api/me', (route) => route.fulfill({ json: { companies: ['ACME'] } }))
  await page.route('**/town/api/auth/token', (route) =>
    route.fulfill({ json: { token: 'fixture-jwt' } }),
  )
  await page.route('**/town/api/avatar/preference**', (route) =>
    route.fulfill({ json: { consent: false, appearance: 'human-blue' } }),
  )
  await page.routeWebSocket('**/town/ws', (socket) => {
    send = (hex) => socket.send(Buffer.from(hex, 'hex'))
    socket.onMessage(() => send(wire.snapshot))
  })
  await page.goto('/?lang=en')
  const flow = page.getByRole('region', { name: 'Operations flow' })
  await expect(flow.getByText('Connect to ERP', { exact: true })).toHaveCount(4)
  await page.getByRole('button', { name: 'Connect to ERP', exact: true }).click()
  await expect(flow.getByText('Submitted · 1', { exact: true })).toBeVisible()
  await expect(flow.getByText('No visible documents', { exact: true })).toHaveCount(3)
  await flow.getByRole('button', { name: /Orders/ }).focus()
  await page.keyboard.press('Enter')
  await expect(page.getByRole('heading', { name: 'Shop', exact: true })).toBeVisible()
  await expect(page.getByText('SO-TEST', { exact: true })).toBeVisible()
  const cancelled = wire.snapshot
    .replace('0801120441434d451801', '0802120441434d4518022001')
    .replace('1001222d', '1002222d')
    .replace(Buffer.from('Submitted').toString('hex'), Buffer.from('Cancelled').toString('hex'))
  send(cancelled)
  await expect(flow.getByText('Cancelled · 1', { exact: true })).toBeVisible()
  await page.getByRole('combobox', { name: 'Recent states', exact: true }).selectOption('1')
  await expect(flow.getByText('Earlier view', { exact: true })).toBeVisible()
  await expect(flow.getByText('Submitted · 1', { exact: true })).toBeVisible()
  await page.getByRole('combobox', { name: 'Recent states', exact: true }).selectOption('live')
  await expect(flow.getByText('Cancelled · 1', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Disconnect', exact: true }).click()
  await expect(flow.getByText('Connect to ERP', { exact: true })).toHaveCount(4)
  await expect(flow.getByText('Cancelled · 1', { exact: true })).toHaveCount(0)
  expect(writes).toEqual([])
})

test('lighting changes real rendered pixels and returns to day without starting ERP work', async ({
  page,
}) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  const errors: string[] = []
  const requests: string[] = []
  page.on('pageerror', (error) => errors.push(error.message))
  page.on('request', (request) => {
    if (request.url().includes('/town/api/')) requests.push(request.url())
  })
  await page.goto('/?lang=en')
  const canvas = page.locator('canvas[data-scene-ready="true"]')
  await expect(canvas).toBeVisible()
  const lighting = page.getByRole('combobox', { name: 'Town lighting', exact: true })
  // Locator screenshots include HTML overlays above the canvas. Hide the toolbar
  // and its shadow during image capture only; compare the actual town exactly.
  const rendered = () =>
    canvas.screenshot({
      style:
        '.scene-tools,.town-dashboard,.scene-caption,.scene-footer,.sidebar { visibility: hidden !important; }',
    })
  const day = await rendered()
  await test.info().attach('initial-day', { body: day, contentType: 'image/png' })
  await lighting.selectOption('night')
  await expect(lighting).toHaveValue('night')
  const night = await rendered()
  expect(night.equals(day)).toBe(false)
  await lighting.selectOption('sunset')
  const sunset = await rendered()
  expect(sunset.equals(night)).toBe(false)
  expect(sunset.equals(day)).toBe(false)
  await lighting.selectOption('day')
  await expect(lighting).toHaveValue('day')
  const restored = await rendered()
  await test.info().attach('restored-day', { body: restored, contentType: 'image/png' })
  expect(restored.equals(day)).toBe(true)
  await page.screenshot({ path: test.info().outputPath('town-operations-flow.png') })
  await lighting.selectOption('night')
  await page.screenshot({ path: test.info().outputPath('town-night.png') })
  await expect(page.locator('[data-delivery-trip]')).toHaveCount(0)
  expect(requests).toEqual([])
  expect(errors).toEqual([])
})

test('Thai mobile controls stay visible and usable with reduced motion', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/?lang=th')
  await expect(page.locator('canvas[data-scene-ready="true"]')).toBeVisible()
  const lighting = page.getByRole('combobox', { name: 'แสงในเมือง', exact: true })
  await expect(lighting).toBeVisible()
  await lighting.selectOption('night')
  await expect(lighting).toHaveValue('night')
  const flow = page.getByRole('region', { name: 'ภาพรวมขั้นตอนงาน' })
  await flow.getByRole('button', { name: /ใบส่งของ/ }).click()
  await expect(page.locator('.inspection')).toContainText('โกดัง')
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  )
  const bounds = await lighting.boundingBox()
  expect(bounds?.x).toBeGreaterThanOrEqual(0)
  expect((bounds?.x ?? 0) + (bounds?.width ?? 0)).toBeLessThanOrEqual(390)
})
