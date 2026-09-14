import { expect, test } from '@playwright/test'
import { readFile } from 'node:fs/promises'

const markdown = [
  '# 报告正文', '正文 **强调** 与 *斜体*，`inline`。', '> 引用保持可读',
  '- [x] 已检查\n- [ ] 未检查',
  '| ' + Array.from({ length: 16 }, (_, i) => `Long_column_${i}`).join(' | ') + ' |\n| ' + Array(16).fill('---').join(' | ') + ' |\n| ' + Array(16).fill('unknown').join(' | ') + ' |',
  '```typescript\nconst source = "' + 'source'.repeat(250) + '"\n```',
  '[安全链接](https://example.com)\n\n![blocked](http://example.com/image.png)',
  ...Array(30).fill('长内容保留在报告正文滚动容器中。'),
].join('\n\n')
const report = { id: 'test-report', title: '报告测试', type: 'custom', summary: '测试输入，不代表真实报告或执行证据', content: markdown, event_count: 1, created_at: '2026-09-14T00:00:00Z' }
const payload = {
  format: 'sentinel-report-v1', meta: { event_count: 2, critical_count: 1, high_count: 1 },
  markdown,
  risk_data: { count: 2, maxCVSS: 9, avgRisk: 7, critical: 1, highRisk: 1, events: [
    { id: 1, event_id: 'test-1', title: '已生成方案测试', severity: 'critical', cvss: 9, cve_id: 'CVE-TEST', recommendationComplete: true, recommendation: markdown },
    { id: 2, event_id: 'test-2', title: '未生成方案测试', severity: 'high', cvss: 7, recommendationComplete: false },
  ] }, agent_logs: [{ agent: 'test-agent', message: '测试日志', status: 'error' }],
}

for (const width of [375, 1280, 1440, 1920]) {
  test(`report traps/restores focus and bounds long content at ${width}`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width, height: 900 })
    const errors: string[] = []
    page.on('pageerror', error => errors.push(error.message))
    await page.goto('/tests/ui/fixtures/report.html')
    await page.evaluate(report => window.renderReportFixture(report), report)
    const opener = page.getByRole('button', { name: '打开报告测试' })
    await opener.click()
    const dialog = page.getByRole('dialog', { name: report.title })
    const close = dialog.getByRole('button', { name: '关闭报告' })
    await expect(close).toBeFocused()
    await expect(dialog).toHaveAccessibleDescription(/1 个事件/)
    await close.press('Shift+Tab')
    await expect(dialog.getByRole('button', { name: 'JSON', exact: true })).toBeFocused()
    await page.keyboard.press('Tab')
    await expect(close).toBeFocused()
    await dialog.getByRole('heading', { name: '报告正文' }).click()
    await expect(dialog).toBeVisible()
    for (const scroll of [dialog.getByTestId('markdown-table-scroll'), dialog.locator('pre[role="region"]')]) {
      expect(await scroll.evaluate(el => el.scrollWidth > el.clientWidth && getComputedStyle(el).overflowX === 'auto')).toBe(true)
      await scroll.evaluate(el => { el.scrollLeft = 100 })
      expect(await scroll.evaluate(el => el.scrollLeft)).toBeGreaterThan(0)
    }
    expect(await dialog.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    // Body typography and ancestor styles must remain readable on the white surface.
    const paragraph = dialog.locator('[data-markdown-variant] > p').first()
    expect(await paragraph.evaluate(el => {
      for (let node: HTMLElement | null = el as HTMLElement; node; node = node.parentElement) {
        const style = getComputedStyle(node)
        if (style.opacity !== '1' || style.filter !== 'none') return false
      }
      return getComputedStyle(el).color === 'rgb(67, 67, 67)'
    })).toBe(true)
    await expect(dialog).toHaveCSS('background-color', 'rgb(255, 255, 255)')
    await expect(dialog.locator('blockquote')).toHaveCSS('color', 'rgb(89, 89, 89)')
    const body = paragraph.locator('xpath=../..')
    expect(await body.evaluate(el => el.scrollHeight > el.clientHeight && getComputedStyle(el).overflowY === 'auto')).toBe(true)
    await body.evaluate(el => { el.scrollTop = 0 })
    await page.screenshot({ path: testInfo.outputPath(`report-${width}.png`) })
    await page.keyboard.press('Escape')
    await expect(dialog).toHaveCount(0)
    await expect(opener).toBeFocused()
    await opener.click()
    await page.mouse.click(2, 2)
    await expect(dialog).toHaveCount(0)
    await expect(opener).toBeFocused()
    await opener.click()
    await close.click()
    await expect(opener).toBeFocused()
    expect(errors).toEqual([])
  })
}

for (const structured of [false, true]) {
  test(`preserves ${structured ? 'structured' : 'legacy'} copy and all downloads`, async ({ page, context }) => {
    await context.grantPermissions(['clipboard-read', 'clipboard-write'])
    await page.goto('/tests/ui/fixtures/report.html')
    await page.evaluate(report => window.renderReportFixture(report), { ...report, content: structured ? JSON.stringify(payload) : markdown })
    await page.getByRole('button', { name: '打开报告测试' }).click()
    const dialog = page.getByRole('dialog')
    if (structured) {
      await expect(dialog.getByText('方案未生成', { exact: true })).toBeVisible()
      await expect(dialog.getByText('失败', { exact: true })).toHaveCount(1)
    }
    await dialog.getByRole('button', { name: '复制内容' }).click()
    await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe(markdown)
    for (const [label, extension] of [['Markdown', 'md'], ['HTML', 'html'], ['JSON', 'json']]) {
      const pending = page.waitForEvent('download')
      await dialog.getByRole('button', { name: label, exact: true }).click()
      const download = await pending
      expect(download.suggestedFilename()).toBe(`${report.title}.${extension}`)
      const content = await readFile((await download.path())!, 'utf8')
      if (extension === 'md') expect(content).toBe(markdown)
      if (extension === 'html') expect(content).toContain(markdown.replace(/\n/g, '<br>'))
      if (extension === 'json') {
        const { markdown: _, ...clean } = payload
        expect(JSON.parse(content)).toEqual(structured ? clean : { title: report.title, created_at: report.created_at, content: markdown })
      }
    }
  })
}

test('structured tables scroll locally on mobile without fading unavailable solutions', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 900 })
  await page.goto('/tests/ui/fixtures/report.html')
  await page.evaluate(report => window.renderReportFixture(report), { ...report, content: JSON.stringify(payload) })
  await page.getByRole('button', { name: '打开报告测试' }).click()
  const dialog = page.getByRole('dialog')
  expect(await dialog.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
  for (const label of ['严重漏洞（P1）事件清单', '高危漏洞（P2）事件清单', 'Agent 执行日志']) {
    const region = dialog.getByRole('region', { name: label, exact: true })
    expect(await region.evaluate(el => el.scrollWidth > el.clientWidth && getComputedStyle(el).overflowX === 'auto')).toBe(true)
  }
  await expect(dialog.getByText('方案未生成', { exact: true })).toHaveCSS('color', 'rgb(89, 89, 89)')
})
