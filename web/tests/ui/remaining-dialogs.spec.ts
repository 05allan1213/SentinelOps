import { expect, test } from '@playwright/test'

for (const width of [375, 1440]) {
  test(`remaining detail/picker dialogs contain content and restore focus at ${width}`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width, height: 800 })
    await page.route('**/api/**', route => route.abort())
    await page.goto('/tests/ui/fixtures/remaining-dialogs.html')
    for (const name of ['事件详情测试', '文档分块详情', '链路详情', '选择要分析的事件', '安全事件分析报告']) {
      const opener = page.getByRole('button', { name, exact: true })
      await opener.click()
      const dialog = page.getByRole('dialog', { name, exact: true })
      const close = dialog.getByRole('button', { name: '关闭', exact: true }).first()
      await expect(dialog).toBeVisible()
      await close.focus()
      for (let i = 0; i < 12; i++) {
        await page.keyboard.press('Tab')
        expect(await dialog.evaluate(el => el.contains(document.activeElement))).toBe(true)
      }
      expect(await dialog.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
      await page.screenshot({ path: testInfo.outputPath(`${name}-${width}.png`) })
      await page.keyboard.press('Escape')
      await expect(dialog).toHaveCount(0)
      await expect(opener).toBeFocused()
    }
  })
}

test('progress dialog remains non-dismissible and does not turn pending work into success', async ({ page }) => {
  const writes: string[] = []
  await page.route('**/api/**', route => { if (route.request().method() !== 'GET') writes.push(route.request().url()); return route.abort() })
  await page.goto('/tests/ui/fixtures/remaining-dialogs.html')
  await page.getByRole('button', { name: '打开生成进度' }).click()
  const dialog = page.getByRole('dialog', { name: '安全分析报告生成' })
  await expect(dialog.getByText('AI 修复方案生成中')).toBeVisible()
  await page.keyboard.press('Escape')
  await page.mouse.click(2, 2)
  await expect(dialog).toBeVisible()
  await expect(dialog.getByText('0%', { exact: true })).toBeVisible()
  expect(writes).toEqual([])
})

test('rule dialog keeps validation and restores focus on close', async ({ page }) => {
  const writes: string[] = []
  await page.route('**/api/**', route => { if (route.request().method() !== 'GET') writes.push(route.request().url()); return route.abort() })
  await page.goto('/tests/ui/fixtures/remaining-dialogs.html')
  await page.getByRole('button', { name: '术语规则', exact: true }).click()
  const opener = page.getByRole('button', { name: '新增规则', exact: true })
  await opener.click()
  const dialog = page.getByRole('dialog', { name: '新增规则' })
  await dialog.getByRole('button', { name: '保存', exact: true }).click()
  expect(writes).toEqual([])
  await page.keyboard.press('Escape')
  await expect(dialog).toHaveCount(0)
  await expect(opener).toBeFocused()
})

test('event status menu stays actionable above the footer and keeps failure rollback', async ({ page }) => {
  const writes: string[] = []
  await page.route('**/api/**', route => { if (route.request().method() !== 'GET') writes.push(route.request().url()); return route.abort() })
  await page.goto('/tests/ui/fixtures/remaining-dialogs.html')
  await page.getByRole('button', { name: '事件详情测试', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '事件详情测试' })
  const status = dialog.getByRole('combobox')
  await status.click()
  await dialog.getByRole('option', { name: '已忽略' }).click()
  await expect.poll(() => writes.length).toBe(1)
  await expect(status).toContainText('新建')
  await expect(dialog).toBeVisible()
})
