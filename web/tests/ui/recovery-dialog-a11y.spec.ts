import { expect, test } from '@playwright/test'
import { envelope, meta, runDetailRes } from './fixtures/runtime-detail'
for (const width of [1280, 1440]) {
  test(`Recovery focus and dismissal at ${width}`, async ({ page }) => {
    await page.setViewportSize({ width, height: 1000 })
    await page.addInitScript(() => {
      localStorage.setItem('token', 'fixture')
      localStorage.setItem('auth-storage', JSON.stringify({ state: { token: 'fixture', role: 'admin', userID: 'u-1' }, version: 0 }))
    })
    await page.route('**/api/**', route => route.fulfill({ json: { data: { items: [], page: { total: 0, has_next: false }, ...meta() } } }))
    await page.route('**/api/runtime/v1/runs/run-1', route => route.fulfill({ body: envelope(runDetailRes({ summary: { status: 'retryable_failed' }, allowed_recovery_actions: ['resume'] })), contentType: 'application/json' }))
    await page.goto('/runtime/runs/run-1?tab=attempts')
    const trigger = page.getByTestId('runtime-recovery-button')
    await trigger.click()
    const dialog = page.getByRole('dialog', { name: 'Resume Run' })
    await expect(dialog).toHaveAccessibleDescription(/Worker/)
    await expect(page.getByLabel('恢复理由')).toBeFocused()
    for (let i = 0; i < 12; i++) {
      await page.keyboard.press('Tab')
      expect(await dialog.evaluate(el => el.contains(document.activeElement))).toBe(true)
    }
    await page.keyboard.press('Escape')
    await expect(dialog).toHaveCount(0)
    await expect(trigger).toBeFocused()
    await trigger.click()
    await page.mouse.click(10, 10)
    await expect(dialog).toHaveCount(0)
    await expect(trigger).toBeFocused()
  })
}
