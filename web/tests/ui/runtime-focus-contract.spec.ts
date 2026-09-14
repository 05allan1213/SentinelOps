import { expect, test, type Page } from '@playwright/test'
import { RUNS, installAuth, installFallback, slicePage } from './fixtures/runtime-pagination'

// Controls exercised only through real Tab / Shift+Tab presses.
const REQUIRED = ['状态', 'Session ID', 'Agent', 'Scope', '开始时间', '结束时间', '排序', '方向', '包含历史记录', '重置', '每页条数', '上一页', '下一页']
const WATCHED = new Set([...REQUIRED, '跳转页码', '确定'])

type ActiveControl = {
  tag: string
  type: string
  label: string
  focusVisible: boolean
  outlineStyle: string
  outlineWidth: string
  boxShadow: string
  borderColor: string
  disabled: boolean
}

const readActiveControl = (page: Page): Promise<ActiveControl | null> => page.evaluate(() => {
  const el = document.activeElement as HTMLElement | null
  if (!el || el === document.body) return null
  const style = getComputedStyle(el)
  return {
    tag: el.tagName,
    type: (el as HTMLInputElement).type || '',
    label: el.getAttribute('aria-label') || el.getAttribute('name') || (el.closest('label')?.textContent || el.textContent || '').trim(),
    focusVisible: el.matches(':focus-visible'),
    outlineStyle: style.outlineStyle,
    outlineWidth: style.outlineWidth,
    boxShadow: style.boxShadow,
    borderColor: style.borderColor,
    disabled: (el as HTMLButtonElement).disabled === true,
  }
})

test('runtime filter and pagination controls expose exactly one keyboard focus indicator', async ({ page }) => {
  await installAuth(page)
  await installFallback(page)
  await page.route('**/api/runtime/v1/runs**', route => route.fulfill({
    json: { data: { ...slicePage(RUNS, route.request().url(), 20), availability: 'available', data_quality: 'complete' } },
  }))
  // Page 2 of 3 keeps both pagination buttons enabled and focusable.
  await page.goto('/runtime/runs?page=2&page_size=20')
  await expect(page.getByTestId('runtime-run-row')).toHaveCount(20)
  await expect(page.locator('main nav[aria-label$="分页"]').first()).not.toHaveAttribute('aria-busy', 'true')

  // Baseline border colours sampled without focus: a focus indicator must not add a second border treatment.
  const baseline = await page.evaluate(() => {
    const map: Record<string, string> = {}
    document.querySelectorAll<HTMLElement>('main input, main select').forEach(el => {
      const label = el.getAttribute('aria-label') || (el.closest('label')?.textContent || '').trim()
      if (label) map[label] = getComputedStyle(el).borderColor
    })
    return map
  })

  const seen = new Set<string>()
  let shiftTabRoundTrip = false
  // datetime-local inputs expose several internal tab stops, so the budget is generous.
  for (let i = 0; i < 200 && seen.size < REQUIRED.length; i += 1) {
    await page.keyboard.press('Tab')
    const active = await readActiveControl(page)
    if (!active || !WATCHED.has(active.label)) continue

    // Chromium moves focus through datetime-local internal fields; those tab stops are not the
    // control's own focus state. They must still never stack indicators.
    if (!active.focusVisible) {
      const stacked = (active.outlineStyle !== 'none' && active.outlineWidth !== '0px') && active.boxShadow !== 'none'
      expect(stacked, `${active.label} must not stack indicators in an internal focus state`).toBe(false)
      continue
    }

    expect(active.disabled, `${active.label} is disabled and must not take focus`).toBe(false)
    expect(active.focusVisible, `${active.label} must match :focus-visible after a keyboard Tab`).toBe(true)
    const hasOutline = active.outlineStyle !== 'none' && active.outlineWidth !== '0px'
    const hasRing = active.boxShadow !== 'none'
    // Exactly one indicator: no border + ring + open ring stacking.
    expect([hasOutline, hasRing].filter(Boolean).length, `${active.label} focus indicator count`).toBe(1)
    if (hasRing) {
      expect(active.boxShadow, `${active.label} must use the shared primary focus ring`).toContain('rgb(22, 119, 255)')
    }
    if (active.tag === 'SELECT' || (active.tag === 'INPUT' && active.type !== 'checkbox')) {
      expect(active.borderColor, `${active.label} must not change its border colour on focus`).toBe(baseline[active.label])
    }

    if (active.label === '重置' && !shiftTabRoundTrip) {
      shiftTabRoundTrip = true
      // The next tab stop is the first run row link (the table precedes the pagination bar).
      await page.keyboard.press('Tab')
      const forward = await readActiveControl(page)
      expect(forward?.label).toBe('run-21')
      await page.keyboard.press('Shift+Tab')
      const back = await readActiveControl(page)
      expect(back?.label).toBe('重置')
    }
    seen.add(active.label)
  }

  expect([...seen].sort()).toEqual([...REQUIRED].sort())
  expect(shiftTabRoundTrip).toBe(true)
})
