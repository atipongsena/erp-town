// Spec §5.3 / §9.4 / §10.5: the Thai print formats of the sales flow, rendered by Desk's print view on
// test.localhost, match their committed screenshot baselines (`pnpm test:e2e --update-snapshots` to
// re-record after an intended change; the accountant's sign-off checklist is separate).

import { expect, test } from '@playwright/test'
import { localEnv } from '../integration/helpers.ts'

const SITE_URL = 'http://test.localhost:8080'

const FORMATS = [
  { doctype: 'Sales Order', format: 'ใบยืนยันคำสั่งซื้อ', shot: 'sales-order.png' },
  { doctype: 'Delivery Note', format: 'ใบส่งของ', shot: 'delivery-note.png' },
  { doctype: 'Sales Invoice', format: 'ใบกำกับภาษี/ใบแจ้งหนี้', shot: 'tax-invoice.png' },
  { doctype: 'Payment Entry', format: 'ใบเสร็จรับเงิน', shot: 'receipt.png' },
] as const

const samples = (): Record<string, string> => JSON.parse(process.env.PRINT_SAMPLES ?? '{}')

test.use({ baseURL: SITE_URL, viewport: { width: 1280, height: 900 } })

test.beforeEach(async ({ page }) => {
  const pwd = localEnv('ADMIN_PASSWORD') // infra/docker/.env (git-ignored) or the environment
  if (!pwd) throw new Error('ADMIN_PASSWORD not set (infra/docker/.env)')
  // through the browser: Node (page.request) does not resolve *.localhost on every OS, Chromium does
  await page.goto('/login')
  await page.locator('#login_email').fill('Administrator')
  await page.locator('#login_password').fill(pwd)
  const login = page.waitForResponse((r) => r.url().endsWith('/api/method/login'))
  await page.locator('#login_password').press('Enter')
  expect((await login).status()).toBe(200)
})

for (const { doctype, format, shot } of FORMATS) {
  test(`${doctype} prints with ${format}`, async ({ page }) => {
    const name = samples()[doctype]
    expect(name, `no ${doctype} sample: run through the global setup`).toBeTruthy()
    const q = new URLSearchParams({ doctype, name: name ?? '', format, no_letterhead: '1' })
    const res = await page.goto(`/printview?${q}`)
    expect(res?.ok()).toBe(true)
    await expect(page.locator('.print-format')).toBeVisible()
    await expect(page.locator('.print-format')).toContainText(name ?? '')
    await page.evaluate(() => document.fonts.ready)
    await expect(page).toHaveScreenshot(shot, { fullPage: true })
  })
}
