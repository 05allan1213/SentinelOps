import { expect, test, type Page } from '@playwright/test'
import { mkdir } from 'node:fs/promises'
import { CAPABILITIES, RUNS, WORKERS, WORKER_AGGREGATE, installAuth, installFallback, slicePage } from './fixtures/runtime-pagination'

const outDir = '../output/frontend-round3-b4v'

async function expectNoDocumentOverflow(page: Page) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true)
  expect(await page.locator('main').evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
}

async function expectSidebarExpanded(page: Page) {
  await expect(page.locator('aside')).toHaveCSS('width', '288px')
  expect(await page.locator('main').evaluate(el => Math.round(el.getBoundingClientRect().left))).toBe(288)
}

async function expectFilterToolbarLocal(page: Page) {
  const section = page.locator('section[aria-label="Run 筛选"]')
  await expect(section).toBeVisible()
  const geometry = await section.evaluate(el => {
    const sectionBox = el.getBoundingClientRect()
    const mainBox = document.querySelector('main')!.getBoundingClientRect()
    const children = Array.from(el.children).map((child, index) => ({ index, box: child.getBoundingClientRect() }))
    const overlaps: string[] = []
    for (let i = 0; i < children.length; i += 1) {
      for (let j = i + 1; j < children.length; j += 1) {
        const a = children[i].box
        const b = children[j].box
        if (a.left < b.right - 1 && b.left < a.right - 1 && a.top < b.bottom - 1 && b.top < a.bottom - 1) {
          overlaps.push(`${children[i].index}-${children[j].index}`)
        }
      }
    }
    const resetBox = (el.querySelector('button:last-of-type') as HTMLElement).getBoundingClientRect()
    const datetimes = Array.from(el.querySelectorAll<HTMLInputElement>('input[type="datetime-local"]')).map(input => input.getBoundingClientRect())
    return {
      overlaps,
      overflow: el.scrollWidth > el.clientWidth + 1,
      insideMain: sectionBox.left >= mainBox.left - 1 && sectionBox.right <= mainBox.right + 1,
      resetVisible: resetBox.width > 0 && resetBox.height > 0,
      resetInside: resetBox.left >= sectionBox.left - 1 && resetBox.right <= sectionBox.right + 1 && resetBox.bottom <= sectionBox.bottom + 1,
      resetInsideViewport: resetBox.right <= document.documentElement.clientWidth + 1,
      datetimeCount: datetimes.length,
      datetimeWidths: datetimes.map(box => Math.round(box.width)),
    }
  })
  expect(geometry.overlaps).toEqual([])
  expect(geometry.overflow).toBe(false)
  expect(geometry.insideMain).toBe(true)
  expect(geometry.resetVisible).toBe(true)
  expect(geometry.resetInside).toBe(true)
  expect(geometry.resetInsideViewport).toBe(true)
  expect(geometry.datetimeCount).toBe(2)
  for (const width of geometry.datetimeWidths) expect(width).toBeGreaterThan(140)
}

async function expectPaginationBarLocal(page: Page) {
  const nav = page.locator('main nav[aria-label$="分页"]').first()
  await expect(nav).toBeVisible()
  const geometry = await nav.evaluate(navEl => {
    const bar = navEl.parentElement as HTMLElement
    const main = document.querySelector('main') as HTMLElement
    const barBox = bar.getBoundingClientRect()
    const mainBox = main.getBoundingClientRect()
    return {
      barOverflow: bar.scrollWidth > bar.clientWidth + 1,
      navOverflow: navEl.scrollWidth > navEl.clientWidth + 1,
      insideMain: barBox.left >= mainBox.left - 1 && barBox.right <= mainBox.right + 1,
      barWidth: barBox.width,
      mainInnerWidth: main.clientWidth,
    }
  })
  expect(geometry.barOverflow).toBe(false)
  expect(geometry.navOverflow).toBe(false)
  expect(geometry.insideMain).toBe(true)
  expect(geometry.barWidth).toBeLessThanOrEqual(geometry.mainInnerWidth)
}

async function expectLocalTableScroll(page: Page, testId: string) {
  const geometry = await page.getByTestId(testId).evaluate(el => {
    const inner = el.querySelector('table') as HTMLElement
    return {
      overflowX: getComputedStyle(el).overflowX,
      containerWidth: el.clientWidth,
      innerWidth: inner.getBoundingClientRect().width,
      scrollable: el.scrollWidth > el.clientWidth,
      viewportSafe: el.getBoundingClientRect().right <= document.documentElement.clientWidth + 1,
    }
  })
  expect(geometry.overflowX).toBe('auto')
  expect(geometry.viewportSafe).toBe(true)
  // When the table is wider than its card, the card scrolls locally instead of widening the page.
  if (geometry.innerWidth > geometry.containerWidth + 1) expect(geometry.scrollable).toBe(true)
}

async function installRoutes(page: Page, requests: URL[]) {
  await installFallback(page)
  await page.route('**/api/runtime/v1/runs**', route => {
    requests.push(new URL(route.request().url()))
    return route.fulfill({ json: { data: { ...slicePage(RUNS, route.request().url(), 20), availability: 'available', data_quality: 'complete' } } })
  })
  await page.route('**/api/runtime/v1/capabilities**', route => route.fulfill({
    json: { data: { ...slicePage(CAPABILITIES, route.request().url()), availability: 'available', data_quality: 'complete' } },
  }))
  await page.route('**/api/runtime/v1/worker-health**', route => route.fulfill({
    json: { data: { ...slicePage(WORKERS, route.request().url()), aggregate: WORKER_AGGREGATE, availability: 'available', data_quality: 'complete' } },
  }))
}

for (const width of [1280, 1440, 1920]) {
  test(`runtime pagination, filters and tables stay inside the shell at ${width}`, async ({ page }) => {
    await mkdir(outDir, { recursive: true })
    await installAuth(page)
    const requests: URL[] = []
    await installRoutes(page, requests)
    await page.setViewportSize({ width, height: 1000 })

    // Agent Runtime runs
    await page.goto('/runtime/runs')
    await expect(page.getByTestId('runtime-run-row')).toHaveCount(20)
    await expectNoDocumentOverflow(page)
    await expectSidebarExpanded(page)
    await expectFilterToolbarLocal(page)
    await expectPaginationBarLocal(page)
    await expectLocalTableScroll(page, 'runtime-runs-table')

    // Resizing must not re-query or drop the URL filter state.
    const filtersBefore = await page.getByTestId('runtime-runs-filters').innerText()
    const requestsBefore = requests.length
    await page.setViewportSize({ width: width === 1280 ? 1440 : 1280, height: 1000 })
    await expectNoDocumentOverflow(page)
    expect(requests.length).toBe(requestsBefore)
    await expect(page.getByTestId('runtime-runs-filters')).toHaveText(filtersBefore)
    await page.setViewportSize({ width, height: 1000 })
    await page.screenshot({ path: `${outDir}/${width}-runs.png`, fullPage: true })

    // Capabilities
    await page.goto('/runtime/capabilities?page=1&page_size=2')
    await expect(page.getByTestId('runtime-capability-row')).toHaveCount(2)
    await expectNoDocumentOverflow(page)
    await expectPaginationBarLocal(page)
    await expectLocalTableScroll(page, 'runtime-capabilities-table')
    await page.screenshot({ path: `${outDir}/${width}-capabilities.png`, fullPage: true })

    // Worker Health (workers + global aggregate + Eval/Release/Retention facts)
    await page.goto('/runtime/worker-health?page=1&page_size=2')
    await expect(page.getByTestId('runtime-worker-row')).toHaveCount(2)
    await expectNoDocumentOverflow(page)
    await expectPaginationBarLocal(page)
    await expectLocalTableScroll(page, 'runtime-worker-table')
    await page.screenshot({ path: `${outDir}/${width}-worker-health.png`, fullPage: true })
  })
}
