import { expect, type Page, test } from '@playwright/test'
import wire from './fixtures/wire.json' with { type: 'json' }

async function connected(page: Page) {
  await page.route('**/town/api/me', (route) =>
    route.fulfill({ json: { user: 'fixture', companies: ['ACME'], roles: [] } }),
  )
  await page.route('**/town/api/auth/token', (route) =>
    route.fulfill({ json: { token: 'fixture-jwt' } }),
  )
  await page.routeWebSocket('**/town/ws', (socket) =>
    socket.onMessage(() => socket.send(Buffer.from(wire.snapshot, 'hex'))),
  )
  await page.goto('/?lang=th')
  await page.getByRole('button', { name: 'เชื่อมต่อ ERP', exact: true }).click()
  await expect(page.getByRole('status')).toHaveText('เชื่อมต่อแล้ว')
  await page.getByRole('button', { name: /ร้านค้า/ }).click()
}
test('mobile without WebGL creates and reviews a draft before explicit confirmation in both languages', async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.addInitScript(() => {
    const original = HTMLCanvasElement.prototype.getContext
    HTMLCanvasElement.prototype.getContext = function (type: string, ...args: unknown[]) {
      if (type === 'webgl2') return null
      return Reflect.apply(original, this, [type, ...args])
    } as typeof original
  })
  let status = 'Draft'
  let creates = 0
  let submits = 0
  await page.route('**/town/api/sales-orders', (route) => {
    creates++
    expect(route.request().headers()['x-town-request']).toBe('1')
    expect(route.request().postDataJSON()).toMatchObject({
      company: 'ACME',
      customer: 'CUST-001',
      lines: [{ item_code: 'ITEM-002', qty: '1' }],
    })
    return route.fulfill({ status: 201, json: { name: 'SO-DRAFT' } })
  })
  await page.route('**/town/api/sales-orders/SO-DRAFT', (route) =>
    route.fulfill({
      json: {
        name: 'SO-DRAFT',
        company: 'ACME',
        status,
        customer: 'CUST-001',
        workflow_state: '',
        transaction_date: '2026-10-11',
        delivery_date: '2026-10-12',
        lines: [
          {
            item_code: 'ITEM-002',
            qty: '1',
            rate: '9007199254740993.25',
            amount: '9007199254740993.25',
          },
        ],
        net_total: '9007199254740993.25',
        tax_total: '0',
        grand_total: '9007199254740993.25',
        rounded_total: '9007199254740993.25',
      },
    }),
  )
  await page.route('**/town/api/documents/sales-order/SO-DRAFT/submit', (route) => {
    submits++
    status = 'Submitted'
    return route.fulfill({ json: { submitted: true, workflow_state: '' } })
  })
  await connected(page)
  const desk = page.locator('.document-desk')
  await expect(desk.getByRole('combobox', { name: 'ประเภทเอกสาร', exact: true })).toHaveCount(1)
  await desk
    .getByRole('combobox', { name: 'ประเภทเอกสาร', exact: true })
    .selectOption('sales-order')
  await desk.getByLabel('เลขที่เอกสาร', { exact: true }).fill('SO-DRAFT')
  await desk.getByRole('button', { name: 'เปิดรายการ', exact: true }).click()
  await expect(desk.getByRole('heading', { name: 'SO-DRAFT' })).toBeVisible()
  await desk.getByRole('button', { name: 'ปิดรายละเอียด', exact: true }).click()
  await desk.getByRole('button', { name: 'สร้างใบสั่งขาย', exact: true }).click()
  await desk.getByLabel('รหัสลูกค้า', { exact: true }).fill('CUST-001')
  await desk.getByLabel('รหัสสินค้า', { exact: true }).fill('ITEM-002')
  await desk.getByRole('button', { name: 'บันทึกฉบับร่าง', exact: true }).focus()
  await page.keyboard.press('Enter')
  await expect(desk.getByRole('heading', { name: 'SO-DRAFT' })).toBeVisible()
  await expect(desk.locator('[data-amount="rounded_total"]')).toHaveText('9007199254740993.25')
  expect(creates).toBe(1)
  expect(submits).toBe(0)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.getByRole('button', { name: 'EN', exact: true }).click()
  await expect(desk.getByText('Draft', { exact: true })).toBeVisible()
  await desk.getByRole('button', { name: 'Confirm / request approval', exact: true }).click()
  await expect(desk.getByText('Submitted', { exact: true })).toBeVisible()
  expect(submits).toBe(1)
  await page.screenshot({ path: 'test-results/town-document-mobile-en.png', fullPage: true })
  await page.getByRole('button', { name: 'Disconnect', exact: true }).click()
  await expect(page.getByText('SO-DRAFT', { exact: true })).toHaveCount(0)
  await expect(page.locator('[data-amount]')).toHaveCount(0)
})
test('an unknown write result blocks another write until the user checks ERP', async ({ page }) => {
  let writes = 0
  await page.route('**/town/api/sales-orders', (route) => {
    writes++
    return route.abort('connectionreset')
  })
  await connected(page)
  const desk = page.locator('.document-desk')
  await desk.getByRole('button', { name: 'สร้างใบสั่งขาย', exact: true }).click()
  await desk.getByLabel('รหัสลูกค้า', { exact: true }).fill('CUST-001')
  await desk.getByLabel('รหัสสินค้า', { exact: true }).fill('ITEM-002')
  await desk.getByRole('button', { name: 'บันทึกฉบับร่าง', exact: true }).click()
  await expect(desk.getByText(/รายการอาจบันทึกไปแล้ว/)).toBeVisible()
  await expect(desk.getByRole('button', { name: 'บันทึกฉบับร่าง', exact: true })).toBeDisabled()
  expect(writes).toBe(1)
  await page.getByRole('button', { name: 'ตัดการเชื่อมต่อ', exact: true }).click()
  await page.getByRole('button', { name: 'เชื่อมต่อ ERP', exact: true }).click()
  await expect(desk).toHaveCount(0)
  await page.getByRole('button', { name: /ร้านค้า/ }).click()
  await expect(desk.getByText(/รายการอาจบันทึกไปแล้ว/)).toBeVisible()
  await expect(desk.getByRole('button', { name: 'สร้างใบสั่งขาย', exact: true })).toBeDisabled()
  await desk.getByRole('button', { name: 'ตรวจใน ERP แล้ว เริ่มรายการใหม่', exact: true }).click()
  await expect(desk.getByRole('button', { name: 'สร้างใบสั่งขาย', exact: true })).toBeEnabled()
  expect(writes).toBe(1)
})
