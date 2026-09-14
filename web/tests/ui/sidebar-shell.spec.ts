import { expect, test, type Page } from '@playwright/test'

// Exercise the actual shell with unavailable APIs; do not invent business resources.
async function openShell(page: Page) {
  await page.addInitScript(() => {
    if (!localStorage.getItem('token')) {
      localStorage.setItem('token', 'shell-test-token')
      localStorage.setItem('app-storage', JSON.stringify({
        state: { theme: 'dark', sidebarWidth: 288, sidebarCollapsed: false }, version: 1,
      }))
    }
  })
  await page.route('**/api/**', route => route.abort('connectionrefused'))
  await page.goto('/runtime/runs')
  await expect(page.getByText('正在加载页面…', { exact: true })).toHaveCount(0)
  await expect(page.getByTestId('runtime-runs-error')).toBeVisible()
}

async function expectFrame(page: Page, sidebarWidth: number) {
  await expect(page.locator('aside')).toHaveCSS('width', `${sidebarWidth}px`)
  await expect.poll(() => page.locator('main').evaluate(el => el.getBoundingClientRect().left)).toBe(sidebarWidth)
  const geometry = await page.locator('main').evaluate(el => {
    const frame = el.firstElementChild as HTMLElement
    const bounds = frame.getBoundingClientRect()
    return {
      padding: getComputedStyle(el).padding,
      width: bounds.width,
      centered: Math.abs(bounds.left - el.getBoundingClientRect().left - (el.clientWidth - bounds.width) / 2) < 1,
      overflow: el.scrollWidth > el.clientWidth,
      expected: Math.min(1440, el.clientWidth - 64),
    }
  })
  expect(geometry).toMatchObject({ padding: '32px', centered: true, overflow: false })
  expect(geometry.width).toBe(geometry.expected)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
}

for (const width of [1280, 1440, 1920]) {
  test(`sidebar hierarchy, collapse, keyboard and shell geometry at ${width}`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width, height: 1000 })
    const errors: string[] = []
    page.on('pageerror', error => errors.push(error.message))
    await openShell(page)
    const nav = page.getByRole('navigation', { name: '主导航' })
    const groups = nav.getByRole('button')
    await expect(groups).toHaveText(['Agent Runtime', 'AI 能力', '数据管理', '系统监控'])
    const typography = await groups.evaluateAll(elements => elements.map(el => {
      const style = getComputedStyle(el.querySelector('span:last-child')!)
      return [style.fontSize, style.fontWeight, style.letterSpacing, style.textTransform]
    }))
    for (const style of typography) expect(style).toEqual(['12px', '600', '0.3px', 'none'])
    const runLink = nav.getByRole('link', { name: 'Runs', exact: true })
    await expect(runLink).toHaveAttribute('aria-current', 'page')
    const activeColor = await runLink.locator('svg').evaluate(el => getComputedStyle(el).color)
    await expectFrame(page, 288)
    await page.screenshot({ path: testInfo.outputPath('expanded.png'), fullPage: true })

    const runtimeGroup = groups.filter({ hasText: 'Agent Runtime' })
    await runtimeGroup.focus()
    await runtimeGroup.press('Enter')
    await expect(runtimeGroup).toHaveAttribute('aria-expanded', 'false')
    await expect(runLink).toBeHidden()
    await runtimeGroup.press('Space')
    await expect(runLink).toBeVisible()
    await page.getByRole('button', { name: '收起侧边栏', exact: true }).click()
    await expectFrame(page, 72)
    await expect(nav.getByRole('link')).toHaveCount(17)
    await expect(runLink).toHaveAttribute('title', 'Runs')
    await expect(runLink.locator('svg')).toHaveCSS('color', activeColor)
    await runLink.focus()
    await page.keyboard.press('Tab')
    await page.keyboard.press('Shift+Tab')
    await expect(runLink).toBeFocused()
    await expect(runLink).toHaveCSS('outline-width', '2px')
    await expect(runLink).toHaveCSS('outline-style', 'solid')
    await page.screenshot({ path: testInfo.outputPath('collapsed.png'), fullPage: true })
    await nav.getByRole('link', { name: 'Agent 分析', exact: true }).click()
    await expect(page).toHaveURL(/\/events\/analysis$/)
    await expect(nav.getByRole('link', { name: 'Agent 分析', exact: true })).toHaveAttribute('aria-current', 'page')
    await expect(nav.getByRole('link', { name: '安全事件', exact: true })).not.toHaveAttribute('aria-current')
    await expectFrame(page, 72)
    await page.getByRole('button', { name: '展开侧边栏', exact: true }).click()
    await expectFrame(page, 288)
    await page.goto('/runtime/runs/unavailable')
    await expect(runLink).toHaveAttribute('aria-current', 'page')
    await expectFrame(page, 288)
    expect(errors).toEqual([])
  })
}

test('resize limits, persistence, group state and reduced motion preserve light migration', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce', colorScheme: 'dark' })
  await openShell(page)
  const handle = page.getByRole('separator', { name: '调整侧边栏宽度' })
  await handle.focus()
  await handle.press('End')
  await expectFrame(page, 370)
  await handle.press('ArrowRight')
  await expectFrame(page, 370)
  await handle.press('Home')
  await handle.press('ArrowLeft')
  await expectFrame(page, 200)
  await handle.press('ArrowRight')
  await expectFrame(page, 210)
  const bounds = (await handle.boundingBox())!
  await page.mouse.move(bounds.x + bounds.width / 2, 200)
  await page.mouse.down()
  await page.mouse.move(bounds.x + bounds.width / 2 + 110, 200, { steps: 5 })
  await page.mouse.up()
  await expectFrame(page, 320)
  await expect(page.locator('body')).toHaveCSS('cursor', 'auto')
  await page.getByRole('button', { name: 'Agent Runtime', exact: true }).click()
  await page.getByRole('button', { name: '收起侧边栏', exact: true }).click()
  await expect(page.getByRole('link', { name: 'Runs', exact: true })).toBeVisible()
  await page.getByRole('button', { name: '展开侧边栏', exact: true }).click()
  await expect(page.getByRole('link', { name: 'Runs', exact: true })).toBeHidden()
  await page.getByRole('button', { name: '收起侧边栏', exact: true }).click()
  await page.reload()
  await expectFrame(page, 72)
  await page.getByRole('button', { name: '展开侧边栏', exact: true }).click()
  await expectFrame(page, 320)
  await expect(page.locator('html')).not.toHaveClass(/dark/)
  await expect(page.locator('html')).toHaveCSS('color-scheme', 'light')
  expect(await page.evaluate(() => JSON.parse(localStorage.getItem('app-storage')!)))
    .toMatchObject({ state: { theme: 'light', sidebarWidth: 320, sidebarCollapsed: false }, version: 2 })
  expect(await page.locator('aside, aside *, main, main > div').evaluateAll(elements => elements.every(el => {
    const css = getComputedStyle(el)
    return css.animationName === 'none' && (css.transitionProperty === 'none' || css.transitionDuration.split(',').every(duration => parseFloat(duration) === 0))
  }))).toBe(true)
})
