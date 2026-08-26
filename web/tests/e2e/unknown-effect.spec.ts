import { expect, test } from '@playwright/test'

test('unknown effect remains parked until admin evidence resolution', async ({ page }) => {
  test.skip(!process.env.SENTINELOPS_E2E_UNKNOWN_EFFECT_FIXTURE, 'requires provider-double unknown-effect fixture')
  await page.goto('/dashboard')
  await expect(page.locator('[data-testid^="effect-"]').first()).toContainText('结果未知')
  await expect(page.getByText('仍然未知')).toBeVisible()
})
