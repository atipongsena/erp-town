import { expect, test } from '@playwright/test'
import { OrthographicCamera, Vector3 } from 'three'
import layout from '../src/layout.json' with { type: 'json' }
import wire from './fixtures/wire.json' with { type: 'json' }

test('working monitors draw shared counts, select their department and update with language and scope', async ({
  page,
}) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  // Observe real 2D painting used by CanvasTexture without replacing WebGL.
  await page.addInitScript(() => {
    const original = CanvasRenderingContext2D.prototype.fillText
    const lines: string[] = []
    Object.assign(window, { screenPaints: lines })
    CanvasRenderingContext2D.prototype.fillText = function (text, ...args) {
      if (this.canvas.width === 512 && this.canvas.height === 256) lines.push(text)
      return Reflect.apply(original, this, [text, ...args])
    }
  })
  await page.route('**/town/api/me', (r) => r.fulfill({ json: { companies: ['ACME'] } }))
  await page.route('**/town/api/auth/token', (r) => r.fulfill({ json: { token: 'fixture' } }))
  await page.route('**/town/api/avatar/preference**', (r) =>
    r.fulfill({ json: { consent: false, appearance: 'human-blue' } }),
  )
  await page.route('**/town/api/live-demo', (r) =>
    r.fulfill({
      json: {
        enabled: true,
        mode: 'erp-test',
        company: 'ACME',
        round: 1,
        stage: 'sales-review',
        phase: 'working',
        actors: [
          {
            id: 'monitor-staff',
            kind: 'human',
            department: 'shop',
            from_department: 'shop',
            activity: 'working',
          },
        ],
        documents: [],
      },
    }),
  )
  await page.routeWebSocket('**/town/ws', (s) =>
    s.onMessage(() => s.send(Buffer.from(wire.snapshot, 'hex'))),
  )
  await page.goto('/?lang=en&liveDemo=1')
  await expect(page.locator('[data-work-actor="monitor-staff"]')).toHaveAttribute(
    'data-work-phase',
    'working',
  )
  await expect
    .poll(() => page.evaluate(() => Reflect.get(window, 'screenPaints')))
    .toContain('Sales Order: 1')
  await page.getByRole('button', { name: 'Select actor monitor-staff', exact: true }).click()
  await expect(page.locator('[data-selection]')).toHaveAttribute('data-selection', 'actor')
  await page.waitForTimeout(300)
  const canvas = await page.locator('canvas[data-scene-ready=true]').boundingBox()
  const shop = layout.buildings.find((b) => b.name === 'Shop')
  if (!canvas || !shop) throw new Error('Screen camera bounds missing')
  const target = new Vector3(shop.position[0], shop.position[1], shop.position[2]).add(
    new Vector3(0, 3, 0),
  )
  const camera = new OrthographicCamera(
    -canvas.width / 2,
    canvas.width / 2,
    canvas.height / 2,
    -canvas.height / 2,
    0.1,
    700,
  )
  camera.position.copy(new Vector3(118, 116, 112).add(target.clone().sub(new Vector3(3, 0, -3))))
  camera.zoom = Math.min(canvas.width / 52, canvas.height / 40)
  camera.lookAt(target)
  camera.updateProjectionMatrix()
  camera.updateMatrixWorld()
  const monitor = new Vector3(-21, 0.12 + 1.06 * 2.4, -8.2 + 0.89 * 2.4).project(camera)
  // Hit the actual monitor mesh through the real renderer's raycaster.
  await page.mouse.click(
    canvas.x + ((monitor.x + 1) * canvas.width) / 2,
    canvas.y + ((1 - monitor.y) * canvas.height) / 2,
  )
  await expect(page.locator('[data-selection]')).toHaveAttribute('data-selection', 'department')
  await page.getByRole('button', { name: 'ไทย', exact: true }).click()
  await expect
    .poll(() => page.evaluate(() => Reflect.get(window, 'screenPaints')))
    .toContain('ร้านค้า')
  await page.getByRole('combobox', { name: 'แสงในเมือง', exact: true }).selectOption('night')
  await page.screenshot({ path: test.info().outputPath('department-monitor-th-night.png') })
  await page.getByRole('button', { name: 'ตัดการเชื่อมต่อ', exact: true }).click()
  await expect(page.locator('[data-work-actor]')).toHaveCount(0)
})
