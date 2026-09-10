import { expect, test } from '@playwright/test'

declare global {
  interface Window {
    runtimeSSEFixture: {
      urls: string[]
      cancels: number
      runFetches: number
      emit: (seq: number, type: string, extra?: Record<string, unknown>) => void
      close: () => void
    }
  }
}

for (const width of [1280, 1440]) {
  test(`single reader, dedupe, terminal close and visibility at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    const errors: string[] = []
    page.on('pageerror', error => errors.push(error.message))

    await page.goto('/tests/ui/fixtures/runtime-sse.html')
    await expect(page.getByTestId('connected')).toHaveText('true')
    await expect.poll(() => page.evaluate(() => window.runtimeSSEFixture.urls.length)).toBe(1)
    expect(await page.evaluate(() => window.runtimeSSEFixture.urls[0])).toBe('/api/runtime/v1/runs/run-1/events?after_seq=0')

    // Duplicate and out-of-order frames must not create duplicate timeline rows.
    await page.evaluate(() => {
      const fixture = window.runtimeSSEFixture
      fixture.emit(1, 'run.started')
      fixture.emit(2, 'agent.step')
      fixture.emit(2, 'agent.step')
      fixture.emit(1, 'run.started')
      fixture.emit(3, 'budget.reserved')
    })
    await expect(page.getByTestId('timeline-row')).toHaveCount(3)
    await expect(page.getByTestId('after-seq')).toHaveText('3')
    expect(await page.evaluate(() => window.runtimeSSEFixture.urls.length)).toBe(1)

    // Hidden tabs add no request; foreground restore refreshes the Run exactly once.
    const beforeHidden = await page.evaluate(() => window.runtimeSSEFixture.runFetches)
    await page.evaluate(() => {
      Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => 'hidden' })
      document.dispatchEvent(new Event('visibilitychange'))
    })
    expect(await page.evaluate(() => window.runtimeSSEFixture.runFetches)).toBe(beforeHidden)
    await page.evaluate(() => {
      Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => 'visible' })
      document.dispatchEvent(new Event('visibilitychange'))
    })
    await expect.poll(() => page.evaluate(() => window.runtimeSSEFixture.runFetches)).toBe(beforeHidden + 1)
    expect(await page.evaluate(() => window.runtimeSSEFixture.urls.length)).toBe(1)

    // Terminal server fact closes the reader; transport close alone is never success.
    await page.evaluate(() => window.runtimeSSEFixture.emit(4, 'run.completed', { operation_id: 'op-1' }))
    await expect(page.getByTestId('connected')).toHaveText('false')
    await expect(page.getByTestId('timeline-row')).toHaveCount(4)
    await page.waitForTimeout(400)
    expect(await page.evaluate(() => window.runtimeSSEFixture.urls.length)).toBe(1)
    expect(await page.evaluate(() => window.runtimeSSEFixture.cancels)).toBeGreaterThanOrEqual(0)

    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width)
    expect(errors).toEqual([])
  })
}
