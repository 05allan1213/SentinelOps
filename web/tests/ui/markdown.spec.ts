import { expect, test } from '@playwright/test'

test.describe('shared Markdown renderer desktop fixture', () => {
  for (const width of [1280, 1440]) {
    test(`bounds tables/code, highlights and copies source at ${width}`, async ({ page, context }) => {
      await page.setViewportSize({ width, height: 900 })
      await context.grantPermissions(['clipboard-read', 'clipboard-write'])
      const errors: string[] = []
      page.on('pageerror', (error) => errors.push(error.message))
      await page.goto('/tests/ui/fixtures/markdown.html')
      await expect(page.getByRole('heading', { name: 'Markdown fixture' })).toBeVisible()
      const raw = `const long = "${'source'.repeat(250)}"\n\n`
      const headers = Array.from({ length: 16 }, (_, i) => `Long_column_name_${i}`)
      const content = [
        '# Incident report', '- [x] collected', '> evidence retained',
        `| ${headers.join(' | ')} |\n| ${headers.map(() => '---').join(' | ')} |\n| ${headers.map(() => 'unknown').join(' | ')} |`,
        `\`\`\`typescript\n${raw}\`\`\``,
        `https://example.com/${'longurl'.repeat(220)}`,
        '[unsafe](javascript:alert)', '<img src=x onerror="alert(1)">',
        '![blocked](http://example.com/tracker.png)',
      ].join('\n\n')
      await page.evaluate((content) => window.renderMarkdownFixture({ content, variant: 'report' }), content)
      await expect(page.getByRole('heading', { name: 'Incident report' })).toBeVisible()
      await expect(page.locator('pre .hljs-keyword')).toHaveText('const')
      await expect(page.getByRole('checkbox')).toBeChecked()
      await expect(page.getByText('unsafe')).not.toHaveAttribute('href')
      await expect(page.locator('img')).toHaveCount(0)
      await expect(page.getByText('图片不可用：blocked')).toBeVisible()
      for (const locator of [page.getByTestId('markdown-table-scroll'), page.getByTestId('code-block').locator('pre')]) {
        const metrics = await locator.evaluate((element) => ({ scroll: element.scrollWidth, client: element.clientWidth, overflow: getComputedStyle(element).overflowX }))
        expect(metrics.scroll).toBeGreaterThan(metrics.client)
        expect(metrics.overflow).toBe('auto')
        await locator.evaluate((element) => { element.scrollLeft = 100 })
        expect(await locator.evaluate((element) => element.scrollLeft)).toBeGreaterThan(0)
      }
      expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width)
      await page.getByRole('button', { name: '复制代码' }).click()
      await expect(page.getByRole('button', { name: '代码已复制' })).toBeVisible()
      expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(raw)
      expect(errors).toEqual([])
    })
  }

  test('streaming guards and unsupported languages stay raw in the browser', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 900 })
    await page.goto('/tests/ui/fixtures/markdown.html')
    await expect(page.getByRole('heading', { name: 'Markdown fixture' })).toBeVisible()
    await page.evaluate(() => window.renderMarkdownFixture({ content: '# Pending\n\n```ts\nconst a =', streaming: true, complete: false }))
    await expect(page.getByRole('heading')).toHaveCount(0)
    await expect(page.getByRole('button')).toHaveCount(0)
    await page.evaluate(() => window.renderMarkdownFixture({ content: '```ts\nconst a = true\n```', streaming: true, complete: false }))
    await expect(page.getByTestId('code-block')).toHaveAttribute('data-highlight-skipped', 'streaming')
    await expect(page.locator('.hljs-keyword')).toHaveCount(0)
    await page.evaluate(() => window.renderMarkdownFixture({ content: '```rust\nfn main() {}\n```' }))
    await expect(page.getByTestId('code-block')).toHaveAttribute('data-highlight-skipped', 'unsupported-language')
    await expect(page.locator('pre code')).toHaveText('fn main() {}\n')
  })
})
