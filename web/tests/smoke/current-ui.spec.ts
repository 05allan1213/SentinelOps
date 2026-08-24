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

async function installAuthenticatedFixture(page: Page, dark = false) {
  await page.addInitScript(({ enableDark }) => {
    localStorage.setItem('token', 'p02-smoke-token')
    if (enableDark) {
      document.addEventListener('DOMContentLoaded', () => {
        document.documentElement.classList.add('dark')
      })
    }
  }, { enableDark: dark })
  await page.route('**/api/**', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ code: 0, message: 'OK', data: {} }),
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

test('dark class 继续驱动暗色 variant', async ({ page }) => {
  const runtimeErrors = captureRuntimeErrors(page)
  await installAuthenticatedFixture(page, true)

  await page.goto('/events/analysis')

  await expect(page.locator('html')).toHaveClass(/dark/)
  const thinkingHeader = page.getByText('思考链路', { exact: true }).locator('..')
  await expect(thinkingHeader).toBeVisible()
  await expect.poll(() => thinkingHeader.evaluate((element) => getComputedStyle(element).backgroundColor))
    .toBe('rgb(22, 27, 34)')
  expect(runtimeErrors).toEqual([])
})
