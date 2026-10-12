import { expect, test } from '@playwright/test'
import wire from './fixtures/wire.json' with { type: 'json' }

// Binary PATCH follows the existing golden snapshot (same subject, version 2).
const cancelled = wire.snapshot
  .replace('0801120441434d451801', '0802120441434d4518022001')
  .replace('1001222d', '1002222d')
  .replace(Buffer.from('Submitted').toString('hex'), Buffer.from('Cancelled').toString('hex'))
const removed =
  '0802120441434d451803200232200a0b53616c6573204f726465721207534f2d544553541a08646f63756d656e74'

test('replays received states without document actions, receives new data, returns live and clears on expiry', async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  let send: (hex: string) => void = () => {}
  let close: () => void = () => {}
  let writes = 0
  await page.route('**/town/api/me', (route) => route.fulfill({ json: { companies: ['ACME'] } }))
  await page.route('**/town/api/auth/token', (route) =>
    route.fulfill({ json: { token: 'fixture-jwt' } }),
  )
  await page.route('**/town/api/sales-orders', (route) => {
    writes++
    return route.abort()
  })
  await page.routeWebSocket('**/town/ws', (socket) => {
    send = (hex) => socket.send(Buffer.from(hex, 'hex'))
    close = () => socket.close({ code: 1008 })
    socket.onMessage(() => send(wire.snapshot))
  })
  await page.goto('/?lang=th')
  await page.getByRole('button', { name: 'เชื่อมต่อ ERP', exact: true }).click()
  await page.getByRole('button', { name: /ร้านค้า/ }).click()
  await expect(page.locator('.document-desk')).toBeVisible()
  await page.getByRole('combobox', { name: 'ดูสถานะย้อนหลัง', exact: true }).selectOption('1')
  await expect(page.locator('.document-desk')).toHaveCount(0)
  await expect(page.locator('.fact-detail-button')).toHaveCount(0)
  await expect(page.getByText('กำลังดูย้อนหลัง', { exact: false })).toBeVisible()
  send(cancelled)
  await expect(page.locator('.replay-panel option')).toHaveCount(3)
  await expect(page.locator('.fact-list .status')).toHaveText('ยืนยันแล้ว')
  await page.getByRole('button', { name: 'EN', exact: true }).click()
  await expect(
    page.getByText('You are viewing an earlier state. Return to live before working on documents.'),
  ).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.screenshot({ path: 'test-results/town-replay-mobile-en.png', fullPage: true })
  await page.getByRole('button', { name: 'Back to live', exact: true }).click()
  await expect(page.locator('.fact-list .status')).toHaveText('Cancelled')
  await expect(page.locator('.document-desk')).toBeVisible()
  await page.getByRole('combobox', { name: 'Recent states', exact: true }).selectOption('1')
  close()
  await expect(page.getByRole('status')).toHaveText('Reconnect to check your access')
  await expect(page.locator('.replay-panel')).toHaveCount(0)
  await expect(page.getByText('SO-TEST', { exact: true })).toHaveCount(0)
  expect(writes).toBe(0)
})

test('a removed subject disappears from old points and a new connection starts with empty history', async ({
  page,
}) => {
  let send: (hex: string) => void = () => {}
  await page.route('**/town/api/me', (route) => route.fulfill({ json: { companies: ['ACME'] } }))
  await page.route('**/town/api/auth/token', (route) =>
    route.fulfill({ json: { token: 'fixture-jwt' } }),
  )
  await page.routeWebSocket('**/town/ws', (socket) => {
    send = (hex) => socket.send(Buffer.from(hex, 'hex'))
    socket.onMessage(() => send(wire.snapshot))
  })
  await page.goto('/?lang=en')
  await page.getByRole('button', { name: 'Connect to ERP', exact: true }).click()
  await page.getByRole('button', { name: /Shop/ }).click()
  send(cancelled)
  await expect(page.locator('.replay-panel option')).toHaveCount(3)
  await page.getByRole('combobox', { name: 'Recent states', exact: true }).selectOption('1')
  await expect(page.getByText('SO-TEST', { exact: true })).toBeVisible()
  send(removed)
  await expect(page.locator('.replay-panel option')).toHaveCount(4)
  await expect(page.getByText('SO-TEST', { exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: 'Disconnect', exact: true }).click()
  await expect(page.locator('.replay-panel')).toHaveCount(0)
  await page.getByRole('button', { name: 'Connect to ERP', exact: true }).click()
  await expect(page.locator('.replay-panel option')).toHaveCount(2)
  await expect(page.getByRole('combobox', { name: 'Recent states', exact: true })).toHaveValue(
    'live',
  )
  // Reconnect clears selection together with the old authorization scope.
  await expect(page.locator('.document-desk')).toHaveCount(0)
  await page.getByRole('navigation').getByRole('button', { name: /Shop/ }).click()
  await expect(page.locator('.document-desk')).toBeVisible()
})
