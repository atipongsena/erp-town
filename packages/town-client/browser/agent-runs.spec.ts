import { expect, test } from '@playwright/test'
import wire from './fixtures/wire.json' with { type: 'json' }

test('AI work is read only, bilingual, and clears when leaving a live session', async ({
  page,
}) => {
  await page.route('**/town/api/me', (route) => route.fulfill({ json: { companies: ['ACME'] } }))
  await page.route('**/town/api/auth/token', (route) =>
    route.fulfill({ json: { token: 'fixture' } }),
  )
  await page.routeWebSocket('**/town/ws', (socket) =>
    socket.onMessage(() => socket.send(Buffer.from(wire.snapshot, 'hex'))),
  )
  const requests: string[] = []
  await page.route('**/town/api/agent-runs?*', (route) => {
    requests.push(route.request().method())
    return route.fulfill({
      json: {
        runs: [
          {
            id: 'uncertain-run',
            agent: 'helper',
            status: 'needs_human',
            calls: 1,
            created_at: '2026-10-11T00:00:00Z',
            finished_at: '',
            unresolved: true,
          },
        ],
        truncated: false,
      },
    })
  })
  await page.goto('/?lang=th')
  await page.getByRole('button', { name: 'เชื่อมต่อ ERP', exact: true }).click()
  await expect(page.getByRole('status')).toHaveText('เชื่อมต่อแล้ว')
  await page.getByRole('button', { name: /อาคาร IT/ }).click()
  await page.getByRole('button', { name: 'ดูสถานะงาน AI', exact: true }).click()
  await expect(page.getByText('helper · รอตรวจใน ERP')).toBeVisible()
  await expect(
    page.getByText('ตรวจรายการจริงใน ERP ก่อนทำงานซ้ำ เพราะผลการทำรายการอาจยังไม่ชัดเจน'),
  ).toBeVisible()
  await page.getByRole('button', { name: 'EN', exact: true }).click()
  await expect(page.getByText('helper · Review in ERP')).toBeVisible()
  await expect(page.getByRole('button', { name: /retry|reconcile|start AI/i })).toHaveCount(0)
  expect(requests).toEqual(['GET'])
  await page.getByRole('button', { name: 'Disconnect', exact: true }).click()
  await expect(page.getByText('helper · Review in ERP')).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'View AI work', exact: true })).toHaveCount(0)
})
