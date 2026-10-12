import { expect, test } from '@playwright/test'
import { OrthographicCamera, Vector3 } from 'three'

// Generated with the pinned protobuf descriptor: one authorized Delivery Note document.
const snapshot =
  '0801120441434d4518012a410a250a0d44656c6976657279204e6f7465120a444e2d56495349424c451a08646f63756d656e74100122167b22737461747573223a225375626d6974746564227d'

test('backlog does not invent workers and a truck mesh opens authorized delivery work', async ({
  page,
}) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  const writes: string[] = []
  const errors: string[] = []
  page.on('pageerror', (e) => errors.push(e.message))
  page.on('request', (r) => {
    if (
      r.method() !== 'GET' &&
      /delivery-notes|agent-runs|avatar\/(?:preference|presence)/.test(r.url())
    )
      writes.push(r.url())
  })
  await page.route('**/town/api/me', (r) =>
    r.fulfill({
      json: { user: 'operator@example.test', roles: ['System Manager'], companies: ['ACME'] },
    }),
  )
  await page.route('**/town/api/auth/token', (r) => r.fulfill({ json: { token: 'fixture' } }))
  await page.route('**/town/api/avatar/preference**', (r) =>
    r.fulfill({ json: { consent: false, appearance: 'human-blue' } }),
  )
  await page.route('**/town/api/avatar/presence**', (r) => r.fulfill({ json: [] }))
  await page.routeWebSocket('**/town/ws', (s) =>
    s.onMessage(() => s.send(Buffer.from(snapshot, 'hex'))),
  )
  await page.goto('/?lang=en')
  await page.getByRole('button', { name: 'Connect to ERP', exact: true }).click()
  await expect(page.getByRole('status')).toHaveText('Connected')
  await expect(page.getByText('operator@example.test', { exact: true })).toBeVisible()
  await expect(page.getByRole('checkbox', { name: 'Show my avatar' })).not.toBeChecked()
  await expect(page.locator('[data-work-actor]')).toHaveCount(0)
  await expect(page.locator('[data-delivery-trip]')).toHaveCount(0)
  const canvas = page.locator('canvas[data-scene-ready="true"]')
  await expect(canvas).toBeVisible()
  // Project a point on the parked cab's front face using the public overview camera.
  // This clicks the actual Three mesh rather than its HTML label or sidebar fallback.
  const box = await canvas.boundingBox()
  if (!box) throw new Error('Canvas unavailable')
  const camera = new OrthographicCamera(
    -box.width / 2,
    box.width / 2,
    box.height / 2,
    -box.height / 2,
    0.1,
    700,
  )
  camera.position.set(118, 116, 112)
  camera.lookAt(3, 0, -3)
  camera.zoom = Math.min(box.width / 155, box.height / 100)
  camera.updateProjectionMatrix()
  camera.updateMatrixWorld()
  const cab = new Vector3(14.05, 1.5, 25.2).project(camera)
  await page.mouse.click(
    box.x + ((cab.x + 1) * box.width) / 2,
    box.y + ((1 - cab.y) * box.height) / 2,
  )
  const work = page.getByRole('region', { name: 'Delivery work', exact: true })
  await expect(work).toBeVisible()
  await expect(work.getByText('DN-VISIBLE', { exact: true })).toBeVisible()
  await expect(work.getByText('Submitted', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Inspect delivery note DN-VISIBLE' })).toBeVisible()
  await page.getByRole('button', { name: 'Disconnect', exact: true }).click()
  await expect(page.getByText('DN-VISIBLE', { exact: true })).toHaveCount(0)
  await expect(page.getByText('operator@example.test', { exact: true })).toHaveCount(0)
  expect(writes).toEqual([])
  expect(errors).toEqual([])
})
