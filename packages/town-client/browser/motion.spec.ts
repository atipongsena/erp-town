import { expect, test } from '@playwright/test'

test('camera and roof transitions still render multiple frames after a demand-rendering idle gap', async ({
  page,
}) => {
  await page.emulateMedia({ reducedMotion: 'no-preference' })
  await page.addInitScript(() => {
    const probe = { recording: false, lastTime: 0, frames: new Set<number>() }
    Object.assign(window, { motionProbe: probe })
    const raf = window.requestAnimationFrame.bind(window)
    window.requestAnimationFrame = (callback) =>
      raf((time) => {
        probe.lastTime = time
        callback(time)
      })
    const draw = WebGL2RenderingContext.prototype.drawElements
    WebGL2RenderingContext.prototype.drawElements = function (...args) {
      if (probe.recording) probe.frames.add(probe.lastTime)
      return draw.apply(this, args)
    }
  })
  await page.goto('/?lang=en')
  await expect(page.locator('canvas[data-scene-ready="true"]')).toBeVisible({ timeout: 45000 })
  // Keep ambient animation paused so it cannot satisfy the transition frame count.
  await page.getByRole('checkbox', { name: 'Town animation', exact: true }).uncheck()
  for (const target of [/^Shop/, /^Whole town/]) {
    // Let all previous drawing settle; an idle delta must not finish the next
    // transition in one step. Count actual WebGL frames, not UI animation RAFs.
    await page.waitForTimeout(2200)
    await page.evaluate(() => {
      const probe = (
        window as unknown as { motionProbe: { recording: boolean; frames: Set<number> } }
      ).motionProbe
      probe.frames.clear()
      probe.recording = true
    })
    await page.getByRole('button', { name: target }).focus()
    await page.keyboard.press('Enter')
    await expect
      .poll(
        () =>
          page.evaluate(
            () =>
              (window as unknown as { motionProbe: { frames: Set<number> } }).motionProbe.frames
                .size,
          ),
        { timeout: 15000 },
      )
      .toBeGreaterThanOrEqual(10)
    await page.evaluate(() => {
      ;(window as unknown as { motionProbe: { recording: boolean } }).motionProbe.recording = false
    })
  }
  await expect(page.getByRole('heading', { name: 'Shop', exact: true })).toHaveCount(0)
})
