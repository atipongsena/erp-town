import { expect, test } from '@playwright/test'
import wire from './fixtures/journey-wire.json' with { type: 'json' }

test('journey uses verified read links and makes no present-day reads in replay', async ({
  page,
}) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  let send: (hex: string) => void = () => {}
  let reads = 0
  await page.route('**/town/api/me', (r) => r.fulfill({ json: { companies: ['ACME'] } }))
  await page.route('**/town/api/auth/token', (r) => r.fulfill({ json: { token: 'fixture' } }))
  await page.route('**/town/api/avatar/preference**', (r) =>
    r.fulfill({ json: { consent: false, appearance: 'human-blue' } }),
  )
  const common = {
    company: 'ACME',
    status: 'Submitted',
    customer: 'C',
    posting_date: null,
    lines: [],
    net_total: '100',
    tax_total: '0',
    grand_total: '100',
    rounded_total: '100',
  }
  const records = [
    [
      'sales-orders/SO',
      { ...common, name: 'SO', transaction_date: null, delivery_date: null, workflow_state: '' },
    ],
    ['delivery-notes/DN', { ...common, name: 'DN', against_sales_orders: ['SO'] }],
    [
      'sales-invoices/SI',
      {
        ...common,
        name: 'SI',
        due_date: null,
        is_return: false,
        outstanding: '10',
        against_delivery_notes: ['DN'],
      },
    ],
    [
      'payment-entries/PE',
      {
        company: 'ACME',
        status: 'Submitted',
        name: 'PE',
        party: 'C',
        posting_date: null,
        paid: '90',
        mode_of_payment: 'Cash',
        against_invoices: ['SI'],
      },
    ],
  ] as const
  for (const [route, doc] of records)
    await page.route(`**/town/api/${route}`, (r) => {
      reads++
      return r.fulfill({ json: doc })
    })
  await page.routeWebSocket('**/town/ws', (s) => {
    send = (hex) => s.send(Buffer.from(hex, 'hex'))
    s.onMessage(() => send(wire.snapshot))
  })
  await page.goto('/?lang=en&connect=1')
  await expect(page.getByRole('status')).toHaveText('Connected')
  expect(reads).toBe(0)
  await page.getByRole('button', { name: 'Select SO', exact: true }).click()
  const journey = page.getByRole('region', { name: 'Business journey' })
  await expect(journey.locator('[data-journey-edge]')).toHaveCount(3)
  await expect(journey).not.toContainText('Zero outstanding')
  expect(reads).toBe(4)
  const inspect = page.getByRole('button', { name: 'Inspect document', exact: true })
  await inspect.click()
  await expect(page.locator('.document-detail h4')).toBeFocused()
  await page.getByRole('button', { name: 'Close details', exact: true }).click()
  await expect(inspect).toBeFocused()
  expect(reads).toBe(5)
  send(wire.updated)
  await page.getByRole('combobox', { name: 'Recent states', exact: true }).selectOption('1')
  await page.getByRole('button', { name: 'Select SO', exact: true }).click()
  await expect(journey).toContainText('Historical linkage evidence was not captured')
  await page.waitForTimeout(600)
  expect(reads).toBe(5)
  await page.getByRole('button', { name: 'Disconnect', exact: true }).click()
  await expect(journey).toHaveCount(0)
})
