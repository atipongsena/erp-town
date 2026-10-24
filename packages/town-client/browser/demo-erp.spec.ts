import { expect, test } from '@playwright/test'

test('ERP preview shares story documents, balances and role filtering without backend calls', async ({
  page,
}) => {
  const backend: string[] = []
  page.on('request', (request) => {
    if (/\/town\/(api|ws)|\/api\/|\/login|\/desk/.test(request.url())) backend.push(request.url())
  })
  page.on('websocket', (socket) => {
    if (new URL(socket.url()).pathname.startsWith('/town/')) backend.push(socket.url())
  })
  await page.goto('/?mode=demo&view=erp&scene=6&lang=en&connect=1')
  const desk = page.getByRole('region', { name: 'ERP workspace' })
  await expect(desk).toBeVisible()
  await expect(page.locator('canvas')).toHaveCount(0)
  await expect(desk.getByRole('button', { name: /DEMO-SI-001/ })).toHaveAttribute(
    'aria-pressed',
    'true',
  )
  await expect(desk.getByLabel('Outstanding amount')).toHaveText('฿2,140.00')
  await expect(desk.getByText('DEMO-PE-001', { exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: 'Back', exact: true }).click()
  await expect(desk.getByLabel('Outstanding amount')).toHaveText('—')
  await expect(desk.getByText('Not posted', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Next scene', exact: true }).click()
  await expect(desk.getByLabel('Outstanding amount')).toHaveText('฿2,140.00')
  await page.getByRole('button', { name: 'Next scene', exact: true }).click()
  await expect(desk.getByLabel('Outstanding amount')).toHaveText('฿0.00')
  await desk.getByRole('button', { name: /DEMO-PE-001/ }).click()
  await expect(desk.getByRole('heading', { name: 'DEMO-PE-001' })).toBeVisible()
  await page.getByRole('button', { name: 'Back', exact: true }).click()
  await expect(desk.getByRole('heading', { name: 'DEMO-SI-001' })).toBeVisible()
  await expect(desk.getByLabel('Outstanding amount')).toHaveText('฿2,140.00')
  await page.getByRole('button', { name: 'Next scene', exact: true }).click()

  await page.getByLabel('View as').selectOption('warehouse')
  await expect(desk.getByRole('heading', { name: 'DEMO-DN-001' })).toBeVisible()
  await expect(desk.getByText(/DEMO-(SO|SI|PE)-001/)).toHaveCount(0)
  await expect(desk.getByText(/฿|VAT|Unit price|Outstanding amount/)).toHaveCount(0)
  await expect(desk.getByRole('cell', { name: '20', exact: true })).toBeVisible()

  await page.getByRole('button', { name: 'Back', exact: true }).click()
  await page.getByRole('button', { name: '3D town', exact: true }).click()
  await expect(page.locator('progress')).toHaveAttribute('value', '6')
  await page.getByRole('button', { name: 'ERP workspace', exact: true }).click()
  await expect(desk.getByRole('heading', { name: 'DEMO-DN-001' })).toBeVisible()
  await page.getByRole('button', { name: 'Reset', exact: true }).click()
  await expect(desk.getByText('No documents in this scene.', { exact: true })).toBeVisible()
  expect(backend).toEqual([])
})

test('ERP preview works in Thai on a phone without WebGL and can open a completed example', async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await page.addInitScript(() => {
    const original = HTMLCanvasElement.prototype.getContext
    HTMLCanvasElement.prototype.getContext = function (type: string, ...args: unknown[]) {
      if (type.startsWith('webgl')) return null
      return original.call(this, type, ...args)
    } as typeof original
  })
  await page.goto('/?mode=demo&view=erp&scene=invalid&lang=th')
  const desk = page.getByRole('region', { name: 'ระบบ ERP' })
  await expect(desk.getByText('ยังไม่มีเอกสารในฉากนี้', { exact: true })).toBeVisible()
  await desk.getByRole('button', { name: 'ดูตัวอย่างที่เสร็จแล้ว', exact: true }).click()
  await expect(desk.getByRole('heading', { name: 'DEMO-SI-001' })).toBeVisible()
  await expect(desk.getByLabel('ยอดค้างชำระ')).toHaveText('฿0.00')
  await desk.getByRole('button', { name: /DEMO-SO-001/ }).click()
  await expect(desk.getByRole('heading', { name: 'DEMO-SO-001' })).toBeVisible()
  await page.getByRole('button', { name: 'EN', exact: true }).click()
  await expect(
    page
      .getByRole('region', { name: 'ERP workspace' })
      .getByRole('heading', { name: 'Sales Order', exact: true }),
  ).toBeVisible()
  await expect(page.locator('canvas')).toHaveCount(0)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  )
})
