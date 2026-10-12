import { expect, test } from '@playwright/test'
import wire from './fixtures/wire.json' with { type: 'json' }

test('loads the real town, supports bilingual keyboard inspection and returns to overview', async ({
  page,
}) => {
  const errors: string[] = []
  page.on('pageerror', (error) => errors.push(error.message))
  await page.goto('/?lang=th')
  await expect(page.locator('canvas[data-scene-ready="true"]')).toBeVisible({ timeout: 45000 })
  await expect(page.getByRole('heading', { name: 'แผนกในเมือง' })).toBeVisible()
  await page.getByRole('button', { name: /ร้านค้า/ }).focus()
  await page.keyboard.press('Enter')
  await expect(page.getByRole('heading', { name: 'ร้านค้า', exact: true })).toBeVisible()
  await expect(page.getByText('เชื่อมต่อ ERP เพื่อดูข้อมูลของบริษัทคุณ')).toBeVisible()
  await page.waitForTimeout(1200)
  await page.screenshot({ path: 'test-results/town-shop-th.png' })
  await page.keyboard.press('Escape')
  await expect(page.getByRole('heading', { name: 'ร้านค้า', exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: 'EN', exact: true }).click()
  await expect(page.locator('html')).toHaveAttribute('lang', 'en')
  await expect(page.getByRole('heading', { name: 'Around town' })).toBeVisible()
  await page
    .getByRole('navigation')
    .getByRole('button', { name: /Warehouse/ })
    .click()
  await expect(page.getByRole('heading', { name: 'Warehouse', exact: true })).toBeVisible()
  await page.reload()
  await expect(page.locator('html')).toHaveAttribute('lang', 'en')
  await expect(page.locator('canvas[data-scene-ready="true"]')).toBeVisible({ timeout: 45000 })
  await page.waitForTimeout(1000)
  await page.screenshot({ path: 'test-results/town-overview-en.png' })
  expect(errors).toEqual([])
})

test('mobile reduced motion keeps the building list accessible without horizontal overflow', async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/?lang=th')
  await expect(page.locator('canvas[data-scene-ready="true"]')).toBeVisible({ timeout: 45000 })
  await page.getByRole('button', { name: /ธนาคาร/ }).click()
  await expect(page.getByRole('heading', { name: 'ธนาคาร', exact: true })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  )
  await page.screenshot({ path: 'test-results/town-mobile-th.png', fullPage: true })
})

test('a narrow workspace keeps the full-width town above its document controls', async ({
  page,
}) => {
  await page.setViewportSize({ width: 680, height: 956 })
  await page.goto('/?lang=th')
  await expect(page.locator('canvas[data-scene-ready="true"]')).toBeVisible({ timeout: 45000 })
  const town = await page.locator('.town-view').boundingBox()
  const sidebar = await page.locator('.sidebar').boundingBox()
  if (!town || !sidebar) throw new Error('Town and document controls must both be visible')
  expect(town.width).toBeGreaterThan(600)
  expect(sidebar.y).toBeGreaterThanOrEqual(town.y + town.height - 1)
  await page.getByRole('button', { name: /ร้านค้า/ }).click()
  await expect(page.getByRole('heading', { name: 'ร้านค้า', exact: true })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  )
})

test('an unavailable gateway gives a clear error and never treats disconnected signals as zero', async ({
  page,
}) => {
  let tokenCalls = 0
  await page.route('**/town/api/me', (route) =>
    route.fulfill({
      status: 401,
      contentType: 'application/json',
      body: '{"message":"secret fixture"}',
    }),
  )
  await page.route('**/town/api/auth/token', (route) => {
    tokenCalls++
    return route.fulfill({ status: 500 })
  })
  await page.goto('/?lang=en')
  await page.getByRole('button', { name: 'Connect to ERP', exact: true }).click()
  await expect(page.getByRole('status')).toHaveText('Sign in to ERP, then connect again.')
  await expect(page.getByRole('link', { name: 'Sign in to ERP' })).toHaveAttribute(
    'href',
    '/login?redirect-to=%2F%3Flang%3Den%26connect%3D1',
  )
  expect(tokenCalls).toBe(0)
  await expect(page.locator('.department-count').first()).toHaveText('—')
  await expect(page.getByText('secret fixture')).toHaveCount(0)
})

test('returns from ERP login, connects once and removes the reconnect marker', async ({ page }) => {
  let tokenCalls = 0
  await page.route('**/town/api/me', (route) =>
    route.fulfill({ json: { companies: ['ACME'], world_enabled: true } }),
  )
  await page.route('**/town/api/auth/token', (route) => {
    tokenCalls++
    return route.fulfill({ json: { token: 'fixture-jwt' } })
  })
  await page.routeWebSocket('**/town/ws', (socket) =>
    socket.onMessage(() => socket.send(Buffer.from(wire.snapshot, 'hex'))),
  )
  await page.goto('/?lang=th&connect=1')
  await expect(page.getByRole('status')).toHaveText('เชื่อมต่อแล้ว')
  await expect(page).toHaveURL(/\?lang=th$/)
  expect(tokenCalls).toBe(1)
  await page.reload()
  await expect(page.getByRole('status')).toHaveText('ยังไม่เชื่อมต่อ')
  expect(tokenCalls).toBe(1)
})

test('explains an ERP session without an enabled world instead of an endless reconnect', async ({
  page,
}) => {
  await page.route('**/town/api/me', (route) =>
    route.fulfill({ json: { companies: ['ACME'], world_enabled: false } }),
  )
  await page.goto('/?lang=th')
  await page.getByRole('button', { name: 'เชื่อมต่อ ERP', exact: true }).click()
  await expect(page.getByRole('status')).toHaveText('เข้าสู่ ERP แล้ว แต่ข้อมูลเมืองยังไม่พร้อมใช้งาน')
  await expect(page.locator('.department-count').first()).toHaveText('—')
})

test('a failed model keeps the object list and inspection available', async ({ page }) => {
  await page.route('**/art/town-shop.glb', (route) => route.fulfill({ status: 404 }))
  await page.goto('/?lang=en')
  await expect(
    page.getByText('Some models could not load. The building list is still available.'),
  ).toBeVisible({ timeout: 45000 })
  await page.getByRole('button', { name: /Shop/ }).click()
  await expect(page.getByRole('heading', { name: 'Shop', exact: true })).toBeVisible()
})

test('consumes a binary snapshot in the browser and clears visible facts on an expiry close', async ({
  page,
}) => {
  let close: () => void = () => {}
  const joins: string[] = []
  await page.route('**/town/api/me', (route) =>
    route.fulfill({ json: { user: 'test', companies: ['ACME'], roles: [] } }),
  )
  await page.route('**/town/api/auth/token', (route) =>
    route.fulfill({ json: { token: 'fixture-jwt' } }),
  )
  await page.routeWebSocket('**/town/ws', (socket) => {
    close = () => socket.close({ code: 1008, reason: 'authentication expired' })
    socket.onMessage((message) => {
      if (typeof message === 'string') throw new Error('Expected binary JOIN')
      joins.push(message.toString('hex'))
      socket.send(Buffer.from(wire.snapshot, 'hex'))
    })
  })
  await page.goto('/?lang=en')
  await page.getByRole('button', { name: 'Connect to ERP', exact: true }).click()
  await expect(page.getByRole('status')).toHaveText('Connected')
  await page.getByRole('button', { name: /Shop/ }).click()
  await expect(page.getByText('SO-TEST', { exact: true })).toBeVisible()
  await expect(page.getByText('987654')).toHaveCount(0)
  await expect(page.getByRole('link', { name: 'Open document SO-TEST' })).toHaveAttribute(
    'href',
    '/desk/sales-order/SO-TEST',
  )
  expect(joins).toEqual([wire.join])
  close()
  await expect(page.getByRole('status')).toHaveText('Reconnect to check your access')
  await expect(page.getByText('SO-TEST', { exact: true })).toHaveCount(0)
  await expect(page.locator('.department-count').first()).toHaveText('—')
})

test('a device without WebGL still has a usable building list', async ({ page }) => {
  await page.addInitScript(() => {
    const original = HTMLCanvasElement.prototype.getContext
    HTMLCanvasElement.prototype.getContext = function (type: string, ...args: unknown[]) {
      if (type === 'webgl2') return null
      return Reflect.apply(original, this, [type, ...args])
    } as typeof original
  })
  await page.goto('/?lang=en')
  await expect(
    page.getByText('3D is unavailable on this device. You can use the building list.'),
  ).toBeVisible()
  await page
    .getByRole('navigation')
    .getByRole('button', { name: /Warehouse/ })
    .click()
  await expect(page.getByRole('heading', { name: 'Warehouse', exact: true })).toBeVisible()
})
