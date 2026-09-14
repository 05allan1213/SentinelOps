import { expect, test, type Page } from '@playwright/test'

const protectedRoutes = [
  '/dashboard',
  '/chat',
  '/events/analysis',
  '/settings',
]

function captureRuntimeErrors(page: Page) {
  const errors: string[] = []
  page.on('pageerror', (error) => errors.push(error.message))
  return errors
}

async function installLegacyDarkClass(page: Page) {
  await page.addInitScript(() => {
    const apply = () => {
      if (!document.documentElement) return false
      document.documentElement.classList.add('dark')
      document.documentElement.dataset.legacyThemeInjected = 'true'
      return true
    }
    if (!apply()) {
      const observer = new MutationObserver(() => {
        if (apply()) observer.disconnect()
      })
      observer.observe(document, { childList: true })
    }
  })
}

async function installAuthenticatedFixture(page: Page, dark = false) {
  await page.addInitScript(({ enableDark }) => {
    localStorage.setItem('token', 'smoke-test-token')
    localStorage.setItem('app-storage', JSON.stringify({ state: { theme: enableDark ? 'dark' : 'light' }, version: 1 }))
  }, { enableDark: dark })
  await page.route('**/api/**', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ code: 0, message: 'OK', data: { total: 1, events: [] } }),
    })
  })
}

test('未认证访问受保护路由时保留登录语义', async ({ page }) => {
  const runtimeErrors = captureRuntimeErrors(page)

  await page.goto('/dashboard')

  await expect(page).toHaveURL(/\/login$/)
  await expect(page.getByRole('heading', { name: '欢迎回来' })).toBeVisible()
  await expect(page.locator('form').getByRole('button', { name: '登录', exact: true })).toBeVisible()
  expect(runtimeErrors).toEqual([])
})

test('关键路由可达且主布局保持渲染', async ({ page }) => {
  const runtimeErrors = captureRuntimeErrors(page)
  await installAuthenticatedFixture(page)

  for (const route of protectedRoutes) {
    await page.goto(route)
    await expect(page).toHaveURL(new RegExp(`${route.replace('/', '\\/')}$`))
    await expect(page.getByText('Security Console', { exact: true })).toBeVisible()
    await expect(page.locator('main')).toBeVisible()
  }

  await page.goto('/chat')
  await expect(page.getByRole('heading', { name: /把威胁变成/ })).toBeVisible()
  expect(runtimeErrors).toEqual([])
})

for (const width of [undefined, 1920]) {
  test(`persisted dark 自动迁移为 light${width ? ` at ${width}` : ''}`, async ({ page }, testInfo) => {
    if (width) await page.setViewportSize({ width, height: 1000 })
    const runtimeErrors = captureRuntimeErrors(page)
    await installAuthenticatedFixture(page, true)
    await installLegacyDarkClass(page)

    await page.goto('/events/analysis')

    await expect(page.locator('html')).not.toHaveClass(/dark/)
    await expect(page.locator('html')).toHaveAttribute('data-legacy-theme-injected', 'true')
    await expect(page.locator('html')).toHaveCSS('color-scheme', 'light')
    await expect(page.locator('body')).toHaveCSS('background-color', 'rgb(245, 245, 247)')
    expect(await page.evaluate(() => JSON.parse(localStorage.getItem('app-storage')!)))
      .toMatchObject({ state: { theme: 'light' }, version: 2 })
    const thinkingHeader = page.getByText('思考链路', { exact: true }).locator('..')
    await expect(thinkingHeader).toBeVisible()
    await expect(thinkingHeader).toHaveCSS('background-color', 'rgb(250, 250, 250)')
    await expect(thinkingHeader.locator('..')).toHaveCSS('background-color', 'rgb(255, 255, 255)')
    await expect(page.getByText('暂无研判结果', { exact: true }).locator('..')).toHaveCSS('background-color', 'rgb(255, 255, 255)')
    expect(await page.locator('main').evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true)
    await page.screenshot({ path: testInfo.outputPath('theme-light.png'), fullPage: true })
    expect(runtimeErrors).toEqual([])
  })
}

test('malformed storage 恢复 light 且保留登录重定向', async ({ page }) => {
  const runtimeErrors = captureRuntimeErrors(page)
  await page.addInitScript(() => {
    localStorage.setItem('app-storage', '{broken')
  })
  await installLegacyDarkClass(page)
  await page.goto('/dashboard')
  await expect(page).toHaveURL(/\/login$/)
  await expect(page.getByRole('heading', { name: '欢迎回来' })).toBeVisible()
  await expect(page.locator('html')).not.toHaveClass(/dark/)
  expect(await page.evaluate(() => JSON.parse(localStorage.getItem('app-storage')!)))
    .toMatchObject({ state: { theme: 'light' }, version: 2 })
  expect(runtimeErrors).toEqual([])
})

test('受保护路由在 dark 系统偏好及 API 不可用时保持 light', async ({ page }) => {
  const runtimeErrors = captureRuntimeErrors(page)
  await page.emulateMedia({ colorScheme: 'dark' })
  await page.addInitScript(() => {
    localStorage.setItem('token', 'smoke-test-token')
    localStorage.setItem('app-storage', JSON.stringify({ state: { theme: 'dark' }, version: 1 }))
  })
  await page.route('**/api/**', route => route.abort('connectionrefused'))
  for (const route of [
    '/dashboard', '/subscriptions', '/events', '/events/analysis', '/reports', '/chat', '/settings',
    '/term-mapping', '/traces', '/traces/unavailable', '/knowledge', '/rag-eval', '/ingest', '/ops',
    '/runtime/runs', '/runtime/runs/unavailable', '/runtime/capabilities', '/runtime/safety', '/runtime/worker-health',
  ]) {
    await page.goto(route)
    await expect(page.locator('main')).toBeVisible()
    await expect(page.getByText('正在加载页面…', { exact: true })).toHaveCount(0)
    await expect(page.getByText('页面暂时无法显示', { exact: true })).toHaveCount(0)
    await expect(page.locator('html')).not.toHaveClass(/dark/)
    await expect(page.locator('html')).toHaveCSS('color-scheme', 'light')
    await expect(page.locator('body')).toHaveCSS('color', 'rgb(31, 31, 31)')
  }
  expect(runtimeErrors).toEqual([])
})
