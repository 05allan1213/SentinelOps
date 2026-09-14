import { expect, test, type Locator, type Page } from '@playwright/test'
import { mkdir, writeFile } from 'node:fs/promises'
import path from 'node:path'
import {
  ANALYSIS_EVENTS,
  CRITICAL_EVENT,
  installEventAnalysisFixture,
  type EventAnalysisFixture,
} from './fixtures/event-analysis'

const OUTPUT_ROOT = path.resolve(process.cwd(), '..', 'output', 'frontend-round3-b6v')
const EVIDENCE_ROOT = path.join(OUTPUT_ROOT, 'evidence')
const SCREENSHOT_WIDTHS = [1280, 1440, 1920]

interface ResolvedColor {
  raw: string
  rgba: [number, number, number, number] | null
}

interface SurfaceSample {
  surface: string
  background: ResolvedColor
  effectiveBackground: ResolvedColor
  color: ResolvedColor
  luminance: number | null
  classification: 'light' | 'dark' | 'transparent'
}

interface PageAudit {
  mainBackground: string
  mainEffectiveBackground: string
  bodyBackground: string
  darkSurfaces: Array<{ label: string; background: string; width: number; height: number; viewportShare: number }>
  lowContrastText: Array<{ text: string; color: string; background: string; contrast: number; fontSize: number }>
  invisibleText: Array<{ text: string; color: string; background: string; contrast: number; fontSize: number }>
  infiniteAnimations: Array<{ name: string; duration: string; target: string }>
  documentOverflow: { scrollWidth: number; clientWidth: number }
}

function luminance([r, g, b]: [number, number, number, number]) {
  const channel = (value: number) => {
    const c = value / 255
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4
  }
  return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b)
}

function contrastRatio(a: [number, number, number, number], b: [number, number, number, number]) {
  const la = luminance(a)
  const lb = luminance(b)
  const [hi, lo] = la > lb ? [la, lb] : [lb, la]
  return (hi + 0.05) / (lo + 0.05)
}

function classify(background: ResolvedColor): { luminance: number | null; classification: SurfaceSample['classification'] } {
  const rgba = background.rgba
  if (!rgba || rgba[3] < 0.5) return { luminance: null, classification: 'transparent' }
  const value = luminance(rgba)
  return { luminance: value, classification: value < 0.2 ? 'dark' : 'light' }
}

async function sampleSurface(surface: string, locator: Locator): Promise<SurfaceSample> {
  const raw = await locator.first().evaluate((el) => {
    const style = getComputedStyle(el)
    const canvas = document.createElement('canvas')
    canvas.width = 1
    canvas.height = 1
    const ctx = canvas.getContext('2d')!
    const resolve = (value: string): [number, number, number, number] | null => {
      if (!value || value === 'transparent' || !CSS.supports('color', value)) return null
      ctx.globalCompositeOperation = 'copy'
      ctx.fillStyle = value
      ctx.fillRect(0, 0, 1, 1)
      const data = ctx.getImageData(0, 0, 1, 1).data
      ctx.globalCompositeOperation = 'source-over'
      return [data[0], data[1], data[2], data[3] / 255]
    }
    let composed: [number, number, number, number] = [255, 255, 255, 1]
    let node: Element | null = el
    while (node) {
      const layer = resolve(getComputedStyle(node).backgroundColor)
      if (layer && layer[3] > 0) {
        composed = [
          layer[0] * layer[3] + composed[0] * (1 - layer[3]),
          layer[1] * layer[3] + composed[1] * (1 - layer[3]),
          layer[2] * layer[3] + composed[2] * (1 - layer[3]),
          1,
        ]
        if (layer[3] >= 0.995) break
      }
      node = node.parentElement
    }
    return {
      background: style.backgroundColor,
      color: style.color,
      resolvedBackground: resolve(style.backgroundColor),
      resolvedColor: resolve(style.color),
      effectiveBackground: composed,
    }
  })
  const background: ResolvedColor = { raw: raw.background, rgba: raw.resolvedBackground } as ResolvedColor
  const color: ResolvedColor = { raw: raw.color, rgba: raw.resolvedColor } as ResolvedColor
  const effectiveBackground: ResolvedColor = {
    raw: `rgb(${raw.effectiveBackground[0]}, ${raw.effectiveBackground[1]}, ${raw.effectiveBackground[2]})`,
    rgba: raw.effectiveBackground as [number, number, number, number],
  }
  const { luminance: lum, classification } = classify(effectiveBackground)
  return { surface, background, effectiveBackground, color, luminance: lum, classification }
}

async function auditPage(page: Page): Promise<PageAudit> {
  return page.evaluate(() => {
    const canvas = document.createElement('canvas')
    canvas.width = 1
    canvas.height = 1
    const ctx = canvas.getContext('2d')!

    const resolve = (value: string): [number, number, number, number] | null => {
      if (!value || value === 'transparent' || !CSS.supports('color', value)) return null
      ctx.globalCompositeOperation = 'copy'
      ctx.fillRect(0, 0, 1, 1)
      ctx.fillStyle = value
      ctx.fillRect(0, 0, 1, 1)
      const data = ctx.getImageData(0, 0, 1, 1).data
      ctx.globalCompositeOperation = 'source-over'
      return [data[0], data[1], data[2], data[3] / 255]
    }

    const lum = ([r, g, b]: [number, number, number, number]) => {
      const channel = (value: number) => {
        const c = value / 255
        return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4
      }
      return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b)
    }

    const ratio = (a: [number, number, number, number], b: [number, number, number, number]) => {
      const la = lum(a)
      const lb = lum(b)
      const [hi, lo] = la > lb ? [la, lb] : [lb, la]
      return (hi + 0.05) / (lo + 0.05)
    }

    const effectiveBackground = (el: Element): [number, number, number, number] => {
      let composed: [number, number, number, number] = [255, 255, 255, 1]
      let node: Element | null = el
      while (node) {
        const layer = resolve(getComputedStyle(node).backgroundColor)
        if (layer && layer[3] > 0) {
          composed = [
            layer[0] * layer[3] + composed[0] * (1 - layer[3]),
            layer[1] * layer[3] + composed[1] * (1 - layer[3]),
            layer[2] * layer[3] + composed[2] * (1 - layer[3]),
            1,
          ]
          if (layer[3] >= 0.995) break
        }
        node = node.parentElement
      }
      return composed
    }

    const label = (el: Element) => {
      const tag = el.tagName.toLowerCase()
      const cls = typeof el.className === 'string' ? el.className.split(/\s+/).slice(0, 4).join('.') : ''
      const text = (el.textContent || '').trim().slice(0, 40)
      return `${tag}${cls ? `.${cls}` : ''}${text ? ` "${text}"` : ''}`
    }

    // Gradient surfaces cannot be resolved through background-color; skip them instead of
    // reporting a false "white text on white background".
    const gradientBacked = (el: Element) => {
      let node: Element | null = el
      while (node) {
        const style = getComputedStyle(node)
        if (style.backgroundImage !== 'none') return true
        const layer = resolve(style.backgroundColor)
        if (layer && layer[3] >= 0.995) return false
        node = node.parentElement
      }
      return false
    }

    const main = document.querySelector('main')
    const viewportArea = window.innerWidth * window.innerHeight
    const darkSurfaces: PageAudit['darkSurfaces'] = []
    const lowContrastText: PageAudit['lowContrastText'] = []
    const invisibleText: PageAudit['invisibleText'] = []

    const scope = main ?? document.body
    for (const el of Array.from(scope.querySelectorAll('*'))) {
      const style = getComputedStyle(el)
      if (style.visibility === 'hidden' || style.display === 'none' || Number(style.opacity) === 0) continue
      const rect = el.getBoundingClientRect()
      if (rect.width < 1 || rect.height < 1) continue

      const background = resolve(style.backgroundColor)
      if (background && background[3] >= 0.5 && lum(background) < 0.12) {
        const area = rect.width * rect.height
        if (area / viewportArea >= 0.02) {
          darkSurfaces.push({
            label: label(el),
            background: style.backgroundColor,
            width: Math.round(rect.width),
            height: Math.round(rect.height),
            viewportShare: Number((area / viewportArea).toFixed(3)),
          })
        }
      }

      const hasOwnText = Array.from(el.childNodes).some(
        (node) => node.nodeType === Node.TEXT_NODE && (node.textContent || '').trim().length > 0,
      )
      if (!hasOwnText) continue
      if (el.closest('[aria-hidden="true"]')) continue
      if (gradientBacked(el)) continue

      const color = resolve(style.color)
      if (!color) continue
      const textBackground = effectiveBackground(el)
      const value = ratio(color, textBackground)
      const record = {
        text: (el.textContent || '').trim().slice(0, 60),
        color: style.color,
        background: `rgb(${textBackground[0]}, ${textBackground[1]}, ${textBackground[2]})`,
        contrast: Number(value.toFixed(2)),
        fontSize: Number.parseFloat(style.fontSize),
      }
      if (value < 1.6) invisibleText.push(record)
      else if (value < 3) lowContrastText.push(record)
    }

    const infiniteAnimations = (document.getAnimations?.() ?? [])
      .filter((animation) => {
        const timing = animation.effect?.getComputedTiming?.()
        return timing?.iterations === Infinity && timing.duration !== 0
      })
      .map((animation) => {
        const target = animation.effect?.target
        return {
          name: (animation as CSSAnimation).animationName || animation.id || 'unknown',
          duration: String(animation.effect?.getComputedTiming?.().duration ?? ''),
          target: target ? label(target as Element) : 'unknown',
        }
      })

    const mainEffective = main ? effectiveBackground(main) : [255, 255, 255, 1]

    return {
      mainBackground: main ? getComputedStyle(main).backgroundColor : '',
      mainEffectiveBackground: `rgb(${mainEffective[0]}, ${mainEffective[1]}, ${mainEffective[2]})`,
      bodyBackground: getComputedStyle(document.body).backgroundColor,
      darkSurfaces,
      lowContrastText,
      invisibleText,
      infiniteAnimations,
      documentOverflow: {
        scrollWidth: document.documentElement.scrollWidth,
        clientWidth: document.documentElement.clientWidth,
      },
    }
  })
}

function thinkingEntries(page: Page): Locator {
  return page.getByText(/^\d+ entries$/)
}

async function entryCount(page: Page): Promise<number> {
  const text = await thinkingEntries(page).first().textContent()
  return Number.parseInt((text || '0').replace(/\D/g, ''), 10) || 0
}

async function startAnalysis(page: Page, fixture: EventAnalysisFixture) {
  await page.getByRole('button', { name: /启动 AI 研判|重新分析/ }).click()
  await expect.poll(() => fixture.pipelineQueries()).not.toHaveLength(0)
}

async function finishAnalysis(page: Page, fixture: EventAnalysisFixture, text = '综合研判结论：优先处置严重漏洞。') {
  await fixture.emitContent(text)
  await fixture.emitDone()
  await expect(page.getByText(CRITICAL_EVENT.title, { exact: true })).toBeVisible()
}

function resultHeader(page: Page): Locator {
  return page.locator('span', { hasText: /^高危事件$/ })
}

test.describe('Event Analysis Batch 6 verification', () => {
  test.beforeAll(async () => {
    await mkdir(EVIDENCE_ROOT, { recursive: true })
  })

  test('legacy/demo disclosure and simulated stage fact stay visible', async ({ page }) => {
    await installEventAnalysisFixture(page)
    await page.goto('/events/analysis')

    const disclosure = page.getByTestId('analysis-source-label')
    await expect(disclosure).toBeVisible()
    await expect(disclosure).toContainText('legacy / demo')
    await expect(disclosure).toContainText('模拟')
    await expect(disclosure).toContainText('不代表 Durable Runtime')
    await expect(disclosure).toContainText('旧 pipeline')

    const sample = await sampleSurface('legacy/demo disclosure', disclosure)
    expect(sample.classification).toBe('light')
    expect(sample.background.rgba && luminance(sample.background.rgba)).toBeGreaterThan(0.7)
  })

  test('initial empty result is a light Security Console surface', async ({ page }, testInfo) => {
    await installEventAnalysisFixture(page)
    await page.goto('/events/analysis')
    await expect(page.getByText('思考链路', { exact: true })).toBeVisible()

    const audit = await auditPage(page)
    const surfaces: SurfaceSample[] = []
    surfaces.push(await sampleSurface('page root', page.locator('main > div > div').first()))
    surfaces.push(await sampleSurface('stats card', page.getByText('最高CVSS', { exact: true }).locator('xpath=../../..')))
    surfaces.push(await sampleSurface('thinking console', page.getByText('思考链路', { exact: true }).locator('xpath=../..')))
    surfaces.push(await sampleSurface('thinking header', page.getByText('思考链路', { exact: true }).locator('xpath=..')))
    surfaces.push(await sampleSurface('result empty', page.getByText('暂无研判结果', { exact: true }).locator('..')))
    surfaces.push(await sampleSurface('mitigation console', page.getByText('仅 Proposal Preview；未连接 Effect 执行，也不会显示成功。').locator('xpath=../..')))

    await writeFile(
      path.join(EVIDENCE_ROOT, `surfaces-empty-${testInfo.project.name}.json`),
      JSON.stringify({ surfaces, audit }, null, 2),
    )

    for (const sample of surfaces) {
      expect(sample.classification, `${sample.surface} should be a light surface (background ${sample.background.raw})`).toBe('light')
    }
    // Historic cyber gradient stops must not paint a dark gradient on the KPI cards.
    const statsCardBackgroundImage = await page
      .getByText('最高CVSS', { exact: true })
      .locator('xpath=../../..')
      .evaluate((el) => getComputedStyle(el).backgroundImage)
    expect(statsCardBackgroundImage).toBe('none')
    expect(audit.mainEffectiveBackground).toBe('rgb(245, 245, 247)')
    expect(audit.darkSurfaces, `dark surfaces: ${JSON.stringify(audit.darkSurfaces)}`).toEqual([])
    expect(audit.invisibleText, `invisible text: ${JSON.stringify(audit.invisibleText)}`).toEqual([])
    expect(audit.documentOverflow.scrollWidth).toBeLessThanOrEqual(audit.documentOverflow.clientWidth)
  })

  test('empty event store keeps the explicit empty-state actions', async ({ page }) => {
    await installEventAnalysisFixture(page, { events: [], total: 0 })
    await page.goto('/events/analysis')

    await expect(page.getByText('暂无安全事件', { exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: /查看安全事件/ })).toBeVisible()
    await expect(page.getByRole('button', { name: /导入事件/ })).toBeVisible()
    await expect(page.getByRole('button', { name: /启动 AI 研判/ })).toHaveCount(0)
    await expect(page.getByText('思考链路', { exact: true })).toHaveCount(0)
  })

  test('latest mode keeps the existing pipeline query semantics', async ({ page }) => {
    const fixture = await installEventAnalysisFixture(page)
    await page.goto('/events/analysis')
    await expect(page.getByRole('button', { name: '最近10条' })).toBeVisible()

    await startAnalysis(page, fixture)
    await expect(page.getByRole('button', { name: '终止' })).toBeVisible()
    expect(await fixture.pipelineQueries()).toEqual([`分析最新 ${ANALYSIS_EVENTS.length} 条安全事件，重点关注高危漏洞`])

    await page.getByRole('button', { name: '终止' }).click()
    await expect(page.getByRole('button', { name: /启动 AI 研判|重新分析/ })).toBeVisible()
  })

  test('specific mode requires a picker selection and analyzes the chosen event', async ({ page }) => {
    const fixture = await installEventAnalysisFixture(page)
    await page.goto('/events/analysis')

    await page.getByRole('button', { name: '最近10条' }).click()
    await page.getByRole('button', { name: /指定事件/ }).click()

    const picker = page.getByText('选择要分析的事件', { exact: true })
    await expect(picker).toBeVisible()
    expect(await fixture.pipelineQueries()).toHaveLength(0)

    const pickerSample = await sampleSurface('event picker modal', page.locator('.fixed.inset-0.z-50 > div').first())
    expect(pickerSample.classification).toBe('light')

    await page.getByRole('button', { name: new RegExp(CRITICAL_EVENT.title) }).click()
    await page.getByRole('button', { name: '确认选择' }).click()
    await expect(picker).toHaveCount(0)
    await expect(page.getByRole('button', { name: /指定事件/ })).toContainText('1')

    await page.getByRole('button', { name: /启动 AI 研判/ }).click()
    await expect.poll(() => fixture.pipelineQueries()).toHaveLength(1)
    const [query] = await fixture.pipelineQueries()
    expect(query).toContain(CRITICAL_EVENT.title)
    expect(query).toContain('共 1 条')

    await page.getByRole('button', { name: '终止' }).click()

    // Reopening and dismissing the picker must not submit anything by itself.
    await page.getByRole('button', { name: /指定事件/ }).click()
    await page.getByRole('button', { name: /指定事件/ }).last().click()
    await expect(page.getByText('选择要分析的事件', { exact: true })).toBeVisible()
    await page.mouse.click(8, 8)
    await expect(page.getByText('选择要分析的事件', { exact: true })).toHaveCount(0)
    expect(await fixture.pipelineQueries()).toHaveLength(1)
  })

  test('mode selector, picker and close control stay keyboard reachable', async ({ page }) => {
    await installEventAnalysisFixture(page)
    await page.goto('/events/analysis')

    const modeButton = page.getByRole('button', { name: '最近10条' })
    // Shift+Tab then Tab keeps the last interaction on the keyboard so :focus-visible applies.
    await modeButton.focus()
    await page.keyboard.press('Shift+Tab')
    await page.keyboard.press('Tab')
    await expect(modeButton).toBeFocused()
    const focusStyle = await modeButton.evaluate((el) => {
      const style = getComputedStyle(el)
      return { outlineStyle: style.outlineStyle, outlineWidth: style.outlineWidth, boxShadow: style.boxShadow }
    })
    expect(
      focusStyle.outlineStyle !== 'none' || focusStyle.boxShadow !== 'none',
      `focus indicator ${JSON.stringify(focusStyle)}`,
    ).toBe(true)

    await page.keyboard.press('Enter')
    await expect(page.getByRole('button', { name: /指定事件/ })).toBeVisible()
    await page.keyboard.press('Tab')
    await page.keyboard.press('Tab')
    await page.keyboard.press('Enter')
    await expect(page.getByText('选择要分析的事件', { exact: true })).toBeVisible()

    const closeButton = page.locator('.fixed.inset-0.z-50 > div > div').first().locator('button').first()
    await expect(closeButton).toBeVisible()
    const closeName = await closeButton.evaluate((el) => ({
      ariaLabel: el.getAttribute('aria-label'),
      title: el.getAttribute('title'),
      text: (el.textContent || '').trim(),
    }))
    expect(
      (closeName.ariaLabel || closeName.title || closeName.text || '').trim().length,
      `close button accessible name ${JSON.stringify(closeName)}`,
    ).toBeGreaterThan(0)

    await closeButton.click()
    await expect(page.getByText('选择要分析的事件', { exact: true })).toHaveCount(0)
  })

  test('streaming delivers staged agent logs and stop halts further appends', async ({ page }) => {
    const fixture = await installEventAnalysisFixture(page)
    await page.goto('/events/analysis')

    await startAnalysis(page, fixture)
    await fixture.emitContent('第一段流式内容')

    await expect(page.getByText('终止')).toBeVisible()
    await expect(page.getByText(/正在解析事件特征/)).toBeVisible({ timeout: 10_000 })
    const duringStream = await entryCount(page)
    expect(duringStream).toBeGreaterThanOrEqual(3)

    const consoleSample = await sampleSurface('thinking console (streaming)', page.getByText('思考链路', { exact: true }).locator('xpath=../..'))
    expect(consoleSample.classification).toBe('light')
    await expect(page.getByText('[数据采集]').first()).toBeVisible()
    await expect(page.getByText(/^\d{2}:\d{2}:\d{2}$/).first()).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true)

    await page.getByRole('button', { name: '终止' }).click()
    await expect(page.getByRole('button', { name: /启动 AI 研判|重新分析/ })).toBeVisible()
    await expect(page.getByText(/已被用户终止/).first()).toBeVisible()
    expect(await fixture.pipelineCancelCount()).toBeGreaterThanOrEqual(1)

    const afterStop = await entryCount(page)
    await page.waitForTimeout(2500)
    expect(await entryCount(page)).toBe(afterStop)
  })

  test('success result keeps risk semantics, report actions and readable surfaces', async ({ page }, testInfo) => {
    const fixture = await installEventAnalysisFixture(page)
    await page.goto('/events/analysis')

    await startAnalysis(page, fixture)
    await finishAnalysis(page, fixture)

    const audit = await auditPage(page)
    const surfaces: SurfaceSample[] = []
    surfaces.push(await sampleSurface('result panel', resultHeader(page).locator('xpath=../..')))
    surfaces.push(await sampleSurface('stats card', page.getByText('最高CVSS', { exact: true }).locator('xpath=../../..')))
    surfaces.push(await sampleSurface('event card (critical)', page.getByText(CRITICAL_EVENT.title, { exact: true }).locator('xpath=..')))

    await writeFile(
      path.join(EVIDENCE_ROOT, `surfaces-success-${testInfo.project.name}.json`),
      JSON.stringify({ surfaces, audit }, null, 2),
    )
    for (const sample of surfaces) {
      expect(sample.classification, `${sample.surface} (${sample.background.raw})`).toBe('light')
    }

    const statsCard = page.getByText('最高CVSS', { exact: true }).locator('xpath=../../..')
    await expect(statsCard).toContainText('9.0')
    const criticalCard = page.getByText('严重事件', { exact: true }).locator('xpath=../../..')
    await expect(criticalCard).toContainText('1')
    const highCard = page.getByText('高危事件', { exact: true }).first().locator('xpath=../../..')
    await expect(highCard).toContainText('1')
    const totalCard = page.getByText('分析总数', { exact: true }).locator('xpath=../../..')
    await expect(totalCard).toContainText(String(ANALYSIS_EVENTS.length))

    await expect(page.getByText(CRITICAL_EVENT.title, { exact: true })).toBeVisible()
    await expect(page.getByText('CRITICAL', { exact: true })).toBeVisible()
    await expect(page.getByText('HIGH', { exact: true })).toBeVisible()

    expect(audit.darkSurfaces).toEqual([])
    expect(audit.invisibleText).toEqual([])

    await page.getByRole('button', { name: /生成报告/ }).click()
    const skipWaiting = page.getByRole('button', { name: '跳过等待，立即生成' })
    // The shared report flow auto-saves once every solution is ready; the skip button only
    // appears while solutions are still streaming.
    await skipWaiting.waitFor({ state: 'visible', timeout: 3000 }).catch(() => {})
    if (await skipWaiting.isVisible().catch(() => false)) await skipWaiting.click()

    await expect.poll(() => fixture.reportSaves.length, { timeout: 30_000 }).toBe(1)
    await expect(page.getByText('报告已保存到报告库')).toBeVisible()
    expect(fixture.reportSaves[0].title).toContain('安全事件分析报告')
    expect(fixture.reportSaves[0].type).toBe('custom')

    await expect(page.getByText('安全事件分析报告', { exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: /复制内容/ })).toBeVisible()
    await expect(page.getByRole('button', { name: /下载 Markdown/ })).toBeVisible()
    await page.getByRole('button', { name: '关闭', exact: true }).click()

    // Event detail keeps CVSS and summary information on a light surface.
    await page.getByText(CRITICAL_EVENT.title, { exact: true }).click()
    await expect(page.getByText('事件详情', { exact: true })).toBeVisible()
    await expect(page.getByText(/CVSS 9\.8/)).toBeVisible()
    const detail = await sampleSurface('event detail', page.getByText('事件详情', { exact: true }).locator('xpath=../..'))
    expect(detail.classification).toBe('light')
  })

  test('critical risk keeps semantic color without infinite decorative pulse', async ({ page }) => {
    const fixture = await installEventAnalysisFixture(page)
    await page.goto('/events/analysis')
    await startAnalysis(page, fixture)
    await finishAnalysis(page, fixture)

    const criticalCard = page.getByText(CRITICAL_EVENT.title, { exact: true }).locator('xpath=..')
    const animationName = await criticalCard.evaluate((el) => getComputedStyle(el).animationName)
    expect(animationName).toBe('none')

    const badge = page.getByText('CRITICAL', { exact: true })
    const badgeColor = await badge.evaluate((el) => getComputedStyle(el).color)
    expect(badgeColor).toBe('rgb(239, 68, 68)')

    const infinite = await page.evaluate(() => (document.getAnimations?.() ?? [])
      .filter((animation) => animation.effect?.getComputedTiming?.().iterations === Infinity)
      .map((animation) => (animation as CSSAnimation).animationName))
    expect(infinite).toEqual([])
  })

  test('API failures keep failure semantics on light surfaces', async ({ page }) => {
    const fixture = await installEventAnalysisFixture(page)
    await page.goto('/events/analysis')

    await fixture.setPipelineMode('sse-error')
    await startAnalysis(page, fixture)

    await expect(page.getByText('分析失败：上游模型超时')).toBeVisible()
    await expect(page.getByRole('button', { name: /启动 AI 研判/ })).toBeVisible()
    await expect(page.getByText('暂无研判结果', { exact: true })).toBeVisible()
    const errorRow = page.getByText('分析失败：上游模型超时')
    const errorSample = await sampleSurface('error log row', errorRow.locator('xpath=../..'))
    expect(errorSample.classification).toBe('light')

    await fixture.setPipelineMode('http-500')
    await page.getByRole('button', { name: /启动 AI 研判/ }).click()
    await expect.poll(() => fixture.pipelineQueries()).toHaveLength(2)
    await expect(page.getByRole('button', { name: /启动 AI 研判/ })).toBeVisible()
    await expect(page.getByText('暂无研判结果', { exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: /重新分析/ })).toHaveCount(0)
  })

  test('reduced motion stops decorative animation and keeps state feedback', async ({ page }) => {
    const fixture = await installEventAnalysisFixture(page)
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.goto('/events/analysis')

    const idleAudit = await auditPage(page)
    expect(idleAudit.infiniteAnimations, JSON.stringify(idleAudit.infiniteAnimations)).toEqual([])

    await startAnalysis(page, fixture)
    await fixture.emitContent('reduced motion chunk')
    await expect(page.getByText('终止')).toBeVisible()
    await expect(page.getByText(/正在解析事件特征/)).toBeVisible({ timeout: 10_000 })

    const streamingAudit = await auditPage(page)
    expect(streamingAudit.infiniteAnimations, JSON.stringify(streamingAudit.infiniteAnimations)).toEqual([])

    const transitionDuration = await page.getByRole('button', { name: '终止' }).evaluate((el) => getComputedStyle(el).transitionDuration)
    expect(transitionDuration.split(',').every((value) => value.trim() === '0s')).toBe(true)
    await expect(page.getByText('思考链路', { exact: true })).toBeVisible()
    await page.getByRole('button', { name: '终止' }).click()
    await expect(page.getByRole('button', { name: /启动 AI 研判|重新分析/ })).toBeVisible()
  })

  test('responsive layout keeps screenshots and no document overflow', async ({ page }) => {
    const fixture = await installEventAnalysisFixture(page)
    const report: Array<Record<string, unknown>> = []

    for (const width of SCREENSHOT_WIDTHS) {
      await page.setViewportSize({ width, height: 1000 })
      await page.goto('/events/analysis')
      await expect(page.getByText('思考链路', { exact: true })).toBeVisible({ timeout: 5000 }).catch(async (error: Error) => {
        const text = await page.locator('main').innerText().catch(() => '<no main>')
        throw new Error(`${error.message}\n--- main text at ${width} ---\n${text}`)
      })

      const dir = path.join(OUTPUT_ROOT, String(width))
      await mkdir(dir, { recursive: true })
      await page.screenshot({ path: path.join(dir, 'initial-empty.png'), fullPage: true })

      const initial = await page.evaluate(() => {
        const main = document.querySelector('main')!
        const routeRoot = main.querySelector(':scope > div > div') as HTMLElement | null
        const routeRect = routeRoot?.getBoundingClientRect()
        return {
          documentScrollWidth: document.documentElement.scrollWidth,
          documentClientWidth: document.documentElement.clientWidth,
          bodyScrollWidth: document.body.scrollWidth,
          mainScrollWidth: main.scrollWidth,
          mainClientWidth: main.clientWidth,
          routeMarginLeft: routeRoot ? getComputedStyle(routeRoot).marginLeft : null,
          routeLeft: routeRect ? Math.round(routeRect.left) : null,
          mainLeft: Math.round(main.getBoundingClientRect().left),
        }
      })
      expect(initial.documentScrollWidth, `document overflow at ${width}`).toBeLessThanOrEqual(initial.documentClientWidth)
      expect(initial.mainScrollWidth, `main overflow at ${width}`).toBeLessThanOrEqual(initial.mainClientWidth)
      // The page must follow the shared Layout padding instead of escaping it with a negative margin.
      expect(initial.routeMarginLeft, `layout padding escape at ${width}`).not.toBe('-32px')

      // Event Picker is opened while the page is idle: the mode select is disabled during processing.
      await page.getByRole('button', { name: '最近10条' }).click()
      await page.getByRole('button', { name: /指定事件/ }).click()
      const modal = page.locator('.fixed.inset-0.z-50 > div').first()
      await expect(modal).toBeVisible()
      const box = await modal.boundingBox()
      expect(box, `picker box at ${width}`).not.toBeNull()
      expect((box?.x ?? 0) >= 0 && (box?.x ?? 0) + (box?.width ?? 0) <= width).toBe(true)
      await page.screenshot({ path: path.join(dir, 'event-picker.png'), fullPage: false })
      await page.getByRole('button', { name: '取消' }).click()
      await expect(modal).toHaveCount(0)
      // Back to latest mode so the primary action starts the pipeline without a selection.
      await page.getByRole('button', { name: /指定事件/ }).click()
      await page.getByRole('button', { name: /最近10条/ }).last().click()

      await page.getByRole('button', { name: /启动 AI 研判/ }).click()
      await expect.poll(() => fixture.pipelineQueries()).not.toHaveLength(0)
      await fixture.emitContent('流式分析中')
      await expect(page.getByText(/正在解析事件特征/)).toBeVisible({ timeout: 10_000 })
      await page.screenshot({ path: path.join(dir, 'processing.png'), fullPage: true })

      await finishAnalysis(page, fixture)
      // Let the stats count-up settle before the evidence screenshot.
      await page.waitForTimeout(1200)
      await page.screenshot({ path: path.join(dir, 'result-success.png'), fullPage: true })

      await page.getByText(CRITICAL_EVENT.title, { exact: true }).click()
      await expect(page.getByText('事件详情', { exact: true })).toBeVisible()
      await expect(page.getByText(/CVSS 9\.8/)).toBeVisible()
      await page.waitForTimeout(400)
      await page.screenshot({ path: path.join(dir, 'event-detail-critical.png'), fullPage: true })

      const finalState = await page.evaluate(() => ({
        documentScrollWidth: document.documentElement.scrollWidth,
        documentClientWidth: document.documentElement.clientWidth,
      }))
      expect(finalState.documentScrollWidth, `document overflow (result) at ${width}`).toBeLessThanOrEqual(finalState.documentClientWidth)

      report.push({ width, ...initial, ...finalState })
      await page.getByRole('button', { name: '清除结果' }).click()
    }

    await writeFile(path.join(EVIDENCE_ROOT, 'responsive.json'), JSON.stringify(report, null, 2))
  })
})
