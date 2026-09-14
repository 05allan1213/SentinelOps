import { expect, test } from '@playwright/test'

for (const width of [1280, 1920]) {
  test(`business surfaces keep light theme and local focus at ${width}`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width, height: 1000 })
    await page.addInitScript(() => {
      localStorage.setItem('token', 'interaction-test-only')
      localStorage.setItem('app-storage', '{broken')
    })
    // Exercise real unavailable states; do not manufacture API results.
    await page.route('**/api/**', route => route.abort())
    const errors: string[] = []
    page.on('pageerror', error => errors.push(error.message))
    for (const path of ['/chat', '/knowledge', '/settings', '/term-mapping', '/ingest', '/traces', '/ops']) {
      await page.goto(path)
      await expect(page.locator('main')).toBeVisible()
      await expect(page.locator('html')).not.toHaveClass(/dark/)
      expect(await page.evaluate(() => JSON.parse(localStorage.getItem('app-storage')!).state.theme)).toBe('light')
      const control = page.locator('main .control:visible').first()
      if (await control.count()) {
        await control.focus()
        await page.keyboard.press('Shift+Tab')
        await page.keyboard.press('Tab')
        await expect(control).toBeFocused()
        expect(await control.evaluate(el => getComputedStyle(el).boxShadow)).not.toBe('none')
        // The parent must not paint a second focus halo.
        expect(await control.evaluate(el => getComputedStyle(el.parentElement!).boxShadow)).not.toMatch(/0px 0px 0px [234]px/)
      }
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
      await page.screenshot({ path: testInfo.outputPath(`${path.slice(1)}-${width}.png`) })
    }
    expect(errors).toEqual([])
  })
}
