import { expect, test } from '@playwright/test'
import { mkdir } from 'node:fs/promises'
import { envelope, meta, runDetailRes } from './fixtures/runtime-detail'
for (const width of [1280, 1440]) {
  test(`runtime visual states and motion at ${width}`, async ({ page }) => {
    await page.setViewportSize({ width, height: 1000 })
    await page.addInitScript(() => {
      localStorage.setItem('token', 'fixture')
      localStorage.setItem('app-storage', JSON.stringify({ state: { theme: 'light' }, version: 1 }))
    })
    const errors: string[] = []
    page.on('pageerror', error => errors.push(error.message))
    const dir = `../output/playwright/runtime-${width}/${process.env.F_VISUAL_BASELINE ? 'baseline' : 'final'}`
    await mkdir(dir, { recursive: true })
    let status = 'failed'
    let quality = 'partial'
    await page.route('**/api/**', route => route.fulfill({ json: { data: { items: [], page: { total: 0, has_next: false }, ...meta() } } }))
    await page.route('**/api/runtime/v1/runs/run-1/events**', route => route.fulfill({ contentType: 'text/event-stream', body: 'data: [DONE]\n\n' }))
    await page.route('**/api/runtime/v1/runs/run-1', route => route.fulfill({ body: envelope(runDetailRes({ summary: { status, current_phase: 'unknown' }, ...meta({ availability: quality, data_quality: quality === 'available' ? 'complete' : 'partial', reason_code: 'long_reason_'.repeat(20) }), allowed_recovery_actions: [] })), contentType: 'application/json' }))
    for (const state of ['failed', 'parked', 'reconciling', 'succeeded']) {
      status = state
      await page.goto('/runtime/runs/run-1')
      await expect(page.locator(`[data-testid="runtime-status-badge"][data-status="${state}"]`).first()).toBeVisible()
      await page.screenshot({ path: `${dir}/${state}.png`, fullPage: true })
      expect(await page.locator('main').evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
      expect(await page.evaluate(() => document.documentElement.scrollWidth === document.documentElement.clientWidth)).toBe(true)
    }
    for (const state of ['available', 'unavailable']) {
      quality = state
      await page.goto('/runtime/runs/run-1?tab=timeline')
      await expect(page.getByTestId('runtime-quality-availability').first()).toBeVisible()
      await page.screenshot({ path: `${dir}/timeline-${state}.png`, fullPage: true })
    }
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.getByTestId('runtime-detail-tab').first().focus()
    expect(await page.getByTestId('runtime-detail-tab').first().evaluate(el => getComputedStyle(el).transitionDuration)).toBe('0s')
    expect(errors).toEqual([])
  })
}

test('loading, empty and unavailable evidence states', async ({ page }, testInfo) => {
  await page.addInitScript(() => localStorage.setItem('token', 'fixture'))
  await page.route('**/api/**', route => route.fulfill({ json: { data: {} } }))
  let release: (() => void) | undefined
  let state = 'loading'
  await page.route('**/api/runtime/v1/runs/run-1/events**', route => route.fulfill({ contentType: 'text/event-stream', body: 'data: [DONE]\n\n' }))
  await page.route('**/api/runtime/v1/runs/run-1', async route => {
    if (state === 'loading') await new Promise<void>(resolve => { release = resolve })
    await route.fulfill({ json: { data: { item: null, availability: state === 'unavailable' ? 'unavailable' : 'available', data_quality: 'unknown', reason_code: state === 'unavailable' ? 'not_observed' : undefined } } })
  })
  const dir = `../output/playwright/runtime-${testInfo.project.use.viewport!.width}/final`
  await mkdir(dir, { recursive: true })
  await page.goto('/runtime/runs/run-1')
  await expect(page.getByTestId('runtime-detail-loading')).toBeVisible()
  await page.screenshot({ path: `${dir}/loading.png`, fullPage: true })
  state = 'empty'; release!()
  await expect(page.getByTestId('runtime-detail-empty')).toBeVisible()
  await page.screenshot({ path: `${dir}/empty.png`, fullPage: true })
  state = 'unavailable'
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByTestId('runtime-detail-unavailable')).toBeVisible()
  await page.screenshot({ path: `${dir}/unavailable.png`, fullPage: true })
})
