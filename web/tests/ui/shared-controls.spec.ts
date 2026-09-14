import { expect, test } from '@playwright/test'

for (const width of [390, 1280, 1440, 1920]) {
  test(`control geometry, keyboard, focus and reduced motion at ${width}`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width, height: 1000 })
    const errors: string[] = []
    page.on('pageerror', error => errors.push(error.message))
    await page.goto('/tests/ui/fixtures/controls.html')
    for (const label of ['Standard input', 'Native select', 'Custom select', 'Local date time']) {
      const control = page.getByLabel(label, { exact: true })
      await expect(control).toHaveCSS('height', '36px')
      await expect(control).toHaveCSS('border-radius', '8px')
      await expect(control).toHaveCSS('font-size', '14px')
      await expect(control).toHaveCSS('border-color', 'rgb(209, 213, 219)')
    }
    for (const label of ['Compact input', 'Compact native select', 'Compact custom select', 'Compact local date time']) {
      await expect(page.getByLabel(label, { exact: true })).toHaveCSS('height', '32px')
    }
    const input = page.getByLabel('Standard input', { exact: true })
    await page.keyboard.press('Tab')
    await expect(input).toBeFocused()
    const ring = await input.evaluate(el => getComputedStyle(el).boxShadow)
    expect(ring).toContain('4px')
    await expect(input).toHaveCSS('outline-style', 'none')
    await expect(input).toHaveCSS('border-color', 'rgb(209, 213, 219)')
    await input.hover()
    await expect(input).toHaveCSS('background-color', 'rgb(250, 250, 250)')
    await expect.poll(() => input.evaluate(el => getComputedStyle(el, '::placeholder').color)).toBe('rgb(140, 140, 140)')

    const custom = page.getByRole('combobox', { name: 'Custom select', exact: true })
    await custom.focus()
    await custom.press('ArrowDown')
    await expect(custom).toHaveCSS('box-shadow', ring)
    await expect(custom).toHaveCSS('border-color', 'rgb(209, 213, 219)')
    await expect(custom).toHaveCSS('outline-style', 'none')
    await expect(custom).toBeFocused()
    await custom.press('ArrowDown')
    await custom.press('Enter')
    await expect(custom).toContainText('Alpha')
    await expect(page.getByTestId('control-values')).toContainText('"mode":"1"')
    await custom.press('Space')
    await custom.press('End')
    await custom.press('Escape')
    await expect(custom).toContainText('Alpha')
    await expect(custom).toHaveAttribute('aria-expanded', 'false')
    await expect(custom).toHaveCSS('box-shadow', ring)
    await custom.press('ArrowDown')
    await custom.press('Tab')
    await expect(page.getByRole('combobox', { name: 'Compact custom select' })).toBeFocused()
    await expect(custom).toHaveAttribute('aria-expanded', 'false')
    await custom.click()
    await page.getByRole('listbox', { name: 'Custom select', exact: true }).getByRole('option', { name: 'Beta', exact: true }).click()
    await expect(custom).toContainText('Beta')
    await expect(custom).toBeFocused()
    await custom.click()
    await page.getByRole('heading').click()
    await expect(custom).toHaveAttribute('aria-expanded', 'false')

    await page.getByLabel('Native select', { exact: true }).selectOption('b')
    await page.getByLabel('Local date time', { exact: true }).fill('2026-09-14T12:34')
    await page.getByLabel('Checkbox', { exact: true }).check()
    await expect(page.getByTestId('control-values')).toContainText('"time":"2026-09-14T12:34"')
    await expect(page.getByTestId('control-values')).toContainText('"checked":true')
    await page.getByRole('button', { name: 'Toggle disabled' }).click()
    await expect(custom).toBeDisabled()
    await expect(custom).toHaveCSS('opacity', '0.5')
    for (const label of ['Disabled input', 'Disabled select', 'Disabled date time', 'Disabled checkbox']) {
      await expect(page.getByLabel(label, { exact: true })).toBeDisabled()
      await expect(page.getByLabel(label, { exact: true })).toHaveCSS('cursor', 'not-allowed')
    }
    await page.emulateMedia({ reducedMotion: 'reduce' })
    for (const el of await page.locator('.control').all()) {
      expect(await el.evaluate(node => getComputedStyle(node).transitionDuration.split(',').every(value => Number.parseFloat(value) === 0))).toBe(true)
    }
    await expect(custom.locator('svg').last()).toHaveCSS('transition-duration', '0s')
    await expect(page.getByRole('combobox', { name: 'Missing selection' })).toHaveAccessibleDescription('Selection required')
    await page.getByRole('combobox', { name: 'Long label' }).click()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await page.screenshot({ path: testInfo.outputPath('controls.png'), fullPage: true })
    expect(errors).toEqual([])
  })
}

test('existing Reports caller retains its filter-to-request contract', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('token', 'control-regression-token'))
  const requests: string[] = []
  await page.route('**/api/**', route => {
    requests.push(route.request().url())
    return route.abort('connectionrefused')
  })
  await page.goto('/reports')
  await page.getByRole('combobox', { name: '全部类型', exact: true }).click()
  await page.getByRole('option', { name: '周报', exact: true }).click()
  await expect(page.getByRole('combobox', { name: '周报', exact: true })).toBeVisible()
  await expect.poll(() => requests.some(raw => {
    const url = new URL(raw)
    return url.pathname === '/api/report/v1/list' && url.searchParams.get('type') === 'weekly' && url.searchParams.get('offset') === '0'
  })).toBe(true)
})
