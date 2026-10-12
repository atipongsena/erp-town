// Local, disconnected rendering evidence. No ERP/model calls or browser credentials.
// Run from the repository root: node packages/town-client/scripts/render-evidence.mjs
// Start the existing client preview first. The default target is loopback only.
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import { chromium } from '@playwright/test'

const base = new URL(process.argv[2] ?? 'http://127.0.0.1:5173')
const avatars = process.argv.includes('--avatars')
if (base.protocol !== 'http:' || base.hostname !== '127.0.0.1' || base.username || base.password)
  throw new Error('A local disconnected preview is required')
const browser = await chromium.launch({ args: ['--use-angle=d3d11', '--enable-gpu'] })
try {
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } })
  const errors = []
  page.on('pageerror', (error) => errors.push(error.message))
  if (avatars) {
    // Existing contract-checked world fixture, synthetic local identities only.
    const wire = JSON.parse(
      await readFile(new URL('../browser/fixtures/wire.json', import.meta.url), 'utf8'),
    )
    // Load the generated descriptor with its installed runtime. This keeps the
    // synthetic AI fact on the shared wire contract without copying an encoder.
    const load = createRequire(new URL('../../world-proto/package.json', import.meta.url))
    const { create, fromBinary, toBinary } = load('@bufbuild/protobuf')
    const { fileDesc } = load('@bufbuild/protobuf/codegenv2')
    const generated = await readFile(
      new URL('../../world-proto/src/gen/world/v1/world_pb.ts', import.meta.url),
      'utf8',
    )
    const encoded = generated.match(/fileDesc\("([^"]+)"/)
    if (!encoded) throw new Error('Shared world descriptor unavailable')
    const file = fileDesc(encoded[1])
    const frameSchema = file.messages.find((m) => m.typeName === 'world.v1.ServerFrame')
    const entitySchema = file.messages.find((m) => m.typeName === 'world.v1.Entity')
    if (!frameSchema || !entitySchema) throw new Error('Shared world schemas unavailable')
    const snapshot = fromBinary(frameSchema, Buffer.from(wire.snapshot, 'hex'))
    snapshot.upserts.push(
      create(entitySchema, {
        key: { doctype: 'Town Agent Run', name: 'FIXTURE-AI', kind: 'agent_run' },
        version: 1n,
        seq: 1n,
        dataJson: new TextEncoder().encode('{"state":"running","code":"OK","agent_key":"fixture"}'),
      }),
    )
    const frame = Buffer.from(toBinary(frameSchema, snapshot))
    await page.route('**/town/api/me', (r) =>
      r.fulfill({ json: { user: 'fixture', companies: ['ACME'], roles: [] } }),
    )
    await page.route('**/town/api/auth/token', (r) => r.fulfill({ json: { token: 'fixture' } }))
    await page.routeWebSocket('**/town/ws', (socket) => socket.onMessage(() => socket.send(frame)))
    let consent = false
    await page.route('**/town/api/avatar/preference**', (r) => {
      if (r.request().method() === 'PUT') consent = r.request().postDataJSON().consent
      return r.fulfill({
        json: r.request().method() === 'PUT' ? { ok: true } : { consent, appearance: 'human-blue' },
      })
    })
    await page.route('**/town/api/avatar/presence**', (r) =>
      r.fulfill({
        json:
          r.request().method() === 'POST'
            ? { ok: true }
            : consent
              ? [
                  {
                    id: '01JA0K6Y8Z9P3R4S5T6V7W8XA1',
                    department: 'shop',
                    emote: 'idle',
                    appearance: 'human-blue',
                  },
                  {
                    id: '01JA0K6Y8Z9P3R4S5T6V7W8XA2',
                    department: 'bank',
                    emote: 'wave',
                    appearance: 'human-coral',
                  },
                ]
              : [],
      }),
    )
  }
  await page.emulateMedia({ reducedMotion: 'no-preference' })
  await page.addInitScript(() => {
    const evidence = { capture: false, frame: 0, frames: [] }
    window.renderEvidence = evidence
    const raf = window.requestAnimationFrame.bind(window)
    window.requestAnimationFrame = (callback) =>
      raf((time) => {
        evidence.frame = time
        callback(time)
      })
    for (const name of [
      'drawElements',
      'drawArrays',
      'drawElementsInstanced',
      'drawArraysInstanced',
    ]) {
      const original = WebGL2RenderingContext.prototype[name]
      WebGL2RenderingContext.prototype[name] = function (...args) {
        if (evidence.capture) {
          let frame = evidence.frames.at(-1)
          if (!frame || frame.time !== evidence.frame) {
            frame = { time: evidence.frame, calls: 0, triangles: 0 }
            evidence.frames.push(frame)
          }
          frame.calls++
          if (args[0] === this.TRIANGLES) {
            const count = name.includes('Elements') ? args[1] : args[2]
            const instances =
              name === 'drawArraysInstanced'
                ? args[3]
                : name === 'drawElementsInstanced'
                  ? args[4]
                  : 1
            frame.triangles += (count / 3) * instances
          }
        }
        return original.apply(this, args)
      }
    }
  })
  await page.goto(new URL('/?lang=en', base).href)
  await page.locator('canvas[data-scene-ready="true"]').waitFor({ timeout: 45000 })
  if (avatars) {
    const loaded = ['town-human-01', 'town-human-02', 'town-agent-hq'].map((name) =>
      page.waitForResponse((r) => r.url().endsWith(`/art/${name}.glb`) && r.ok()),
    )
    await page.getByRole('button', { name: 'Connect to ERP', exact: true }).click()
    const checkbox = page.getByRole('checkbox', { name: 'Show my avatar' })
    await checkbox.check()
    await Promise.all(loaded)
    await checkbox.waitFor({ state: 'visible' })
    await page.waitForTimeout(1000)
  }
  const device = await page.evaluate(() => {
    const canvas = document.querySelector('canvas')
    const gl = canvas.getContext('webgl2')
    const extension = gl.getExtension('WEBGL_debug_renderer_info')
    return {
      renderer: extension
        ? gl.getParameter(extension.UNMASKED_RENDERER_WEBGL)
        : gl.getParameter(gl.RENDERER),
      vendor: extension
        ? gl.getParameter(extension.UNMASKED_VENDOR_WEBGL)
        : gl.getParameter(gl.VENDOR),
      canvas: { width: canvas.width, height: canvas.height },
      userAgent: navigator.userAgent,
      reducedMotion: window.matchMedia('(prefers-reduced-motion: reduce)').matches,
    }
  })
  if (/swiftshader|llvmpipe|software/i.test(device.renderer))
    throw new Error(`Hardware evidence unavailable: ${device.renderer}`)
  const percentile = (values, p) =>
    [...values].sort((a, b) => a - b)[Math.ceil(values.length * p) - 1]
  const transitions = []
  // Separate captures exclude the deliberate idle gap between demand-rendered transitions.
  for (const building of ['Shop', 'Warehouse', 'Bank']) {
    for (const target of [building, 'Whole town']) {
      await page.evaluate(() => {
        window.renderEvidence.frames = []
        window.renderEvidence.capture = true
      })
      await page
        .getByRole('button', { name: target === 'Whole town' ? target : new RegExp(target) })
        .focus()
      await page.keyboard.press('Enter')
      await page.waitForTimeout(1800)
      const frames = await page.evaluate(() => {
        window.renderEvidence.capture = false
        return window.renderEvidence.frames
      })
      const intervals = frames.slice(1).map((frame, index) => frame.time - frames[index].time)
      if (intervals.length < 10)
        throw new Error(
          `Insufficient active render frames for ${target}: ${JSON.stringify({ device, frames })}`,
        )
      transitions.push({
        target,
        frames: frames.length,
        medianFrameMs: percentile(intervals, 0.5),
        p95FrameMs: percentile(intervals, 0.95),
        maxDrawCalls: Math.max(...frames.map((frame) => frame.calls)),
        maxTriangles: Math.max(...frames.map((frame) => frame.triangles)),
      })
    }
  }
  const report = {
    recordedAt: new Date().toISOString(),
    device,
    viewport: { width: 1440, height: 900 },
    load: avatars
      ? 'Two consented human assets and one running AI asset; local world/presence fixtures, no real ERP transport'
      : 'Disconnected town, no characters',
    method:
      'Actual WebGL draw calls grouped by requestAnimationFrame timestamps during six roof/camera transitions. Includes shadow passes; excludes deliberate idle gaps. Measures active frame pacing, not GPU timer-query duration or a connected/pilot load.',
    transitions,
    limits: { maxDrawCalls: 300, maxTriangles: 750000 },
    withinRenderBudget: transitions.every(
      (row) => row.maxDrawCalls <= 300 && row.maxTriangles <= 750000,
    ),
    remaining: [
      'No human art acceptance',
      'No mobile hardware evidence',
      'No real connected-world hardware load',
      'No GPU timer queries',
    ],
  }
  const directory = new URL('../test-results/', import.meta.url)
  await mkdir(directory, { recursive: true })
  await page.screenshot({
    path: new URL(
      avatars ? 'hardware-town-avatars.png' : 'hardware-town.png',
      directory,
    ).pathname.replace(/^\/([A-Z]:)/, '$1'),
  })
  await writeFile(
    new URL(avatars ? 'hardware-render-avatars.json' : 'hardware-render.json', directory),
    `${JSON.stringify(report, null, 2)}\n`,
  )
  if (errors.length) throw new Error(`Render errors: ${errors.join('; ')}`)
  console.log(JSON.stringify(report, null, 2))
} finally {
  await browser.close()
}
