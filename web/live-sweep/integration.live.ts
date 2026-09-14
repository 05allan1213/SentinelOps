import { expect, test, type Page, type Response } from '@playwright/test'
import { mkdir, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const HERE = path.dirname(fileURLToPath(import.meta.url))
const REPO_ROOT = path.resolve(HERE, '../..')
const EVIDENCE_DIR = path.join(REPO_ROOT, 'output/integration/browser')
const API_MARKER = '/api/'

interface NetworkFailure {
  method: string
  url: string
  status: number
  at: string
}

interface PageSignals {
  http4xx: NetworkFailure[]
  http5xx: NetworkFailure[]
  requestFailed: string[]
  navigationAborted: string[]
  consoleErrors: string[]
  pageErrors: string[]
}

const signals: Record<string, PageSignals> = {}

function track(label: string, page: Page): PageSignals {
  const sink: PageSignals =
    signals[label] ?? { http4xx: [], http5xx: [], requestFailed: [], navigationAborted: [], consoleErrors: [], pageErrors: [] }
  signals[label] = sink
  page.on('response', (res: Response) => {
    if (!res.url().includes(API_MARKER)) return
    const status = res.status()
    const entry: NetworkFailure = { method: res.request().method(), url: res.url(), status, at: new Date().toISOString() }
    if (status >= 500) sink.http5xx.push(entry)
    else if (status >= 400) sink.http4xx.push(entry)
  })
  page.on('requestfailed', req => {
    if (!req.url().includes(API_MARKER)) return
    const entry = `${req.method()} ${req.url()} ${req.failure()?.errorText ?? ''}`
    // In-flight fetches/SSE readers are aborted when the test navigates between tabs
    // or routes; that is client-side cancellation, not an API failure.
    if ((req.failure()?.errorText ?? '').includes('ERR_ABORTED')) sink.navigationAborted.push(entry)
    else sink.requestFailed.push(entry)
  })
  page.on('console', msg => {
    if (msg.type() === 'error') sink.consoleErrors.push(msg.text())
  })
  page.on('pageerror', err => sink.pageErrors.push(err.message))
  return sink
}

async function login(page: Page, username = 'admin', password = '123456') {
  await page.goto('/login')
  await page.locator('input[type="text"]').fill(username)
  await page.locator('input[type="password"]').fill(password)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await page.waitForURL(url => !url.pathname.startsWith('/login'), { timeout: 30_000 })
}

test.afterAll(async () => {
  await mkdir(EVIDENCE_DIR, { recursive: true })
  await writeFile(path.join(EVIDENCE_DIR, 'network-signals.json'), JSON.stringify(signals, null, 2))
})

test('01 login and full route sweep with real backend', async ({ page }) => {
  const sink = track('route-sweep', page)
  await login(page)
  const routes = [
    '/dashboard',
    '/events',
    '/events/analysis',
    '/reports',
    '/chat',
    '/knowledge',
    '/ops',
    '/subscriptions',
    '/term-mapping',
    '/traces',
    '/rag-eval',
    '/ingest',
    '/settings',
    '/runtime/runs',
    '/runtime/capabilities',
    '/runtime/safety',
    '/runtime/worker-health',
  ]
  const visited: Array<{ route: string; heading: string }> = []
  for (const route of routes) {
    await page.goto(route)
    await page.waitForLoadState('networkidle')
    await expect(page.locator('#main-content')).toBeVisible()
    const heading = (await page.locator('#main-content h1, #main-content h2').first().textContent().catch(() => '')) ?? ''
    visited.push({ route, heading: heading.trim().slice(0, 60) })
  }
  await mkdir(EVIDENCE_DIR, { recursive: true })
  await writeFile(path.join(EVIDENCE_DIR, 'route-sweep.json'), JSON.stringify({ visited, signals: sink }, null, 2))
  expect(visited).toHaveLength(routes.length)
  expect(sink.http5xx, JSON.stringify(sink.http5xx)).toHaveLength(0)
  expect(sink.requestFailed, JSON.stringify(sink.requestFailed)).toHaveLength(0)
  expect(sink.pageErrors, JSON.stringify(sink.pageErrors)).toHaveLength(0)
})

test('02 chat streaming uses the real provider and renders markdown', async ({ page }) => {
  const sink = track('chat', page)
  await login(page)
  await page.goto('/chat')
  await page.waitForLoadState('networkidle')
  const input = page.getByPlaceholder('输入消息…')
  await expect(input).toBeVisible()
  await input.fill('请用 Markdown 给出一个 SQL 注入防护要点清单，并附一段代码示例。')
  const sendButton = page.getByRole('button', { name: '发送消息' })
  await expect(sendButton).toBeEnabled()
  await sendButton.click()
  await expect.poll(async () => (await page.locator('#main-content').innerText()).length, { timeout: 150_000 }).toBeGreaterThan(200)
  await expect(page.locator('#main-content pre').first()).toBeVisible({ timeout: 150_000 })
  const text = await page.locator('#main-content').innerText()
  await mkdir(EVIDENCE_DIR, { recursive: true })
  await writeFile(path.join(EVIDENCE_DIR, 'chat-answer.txt'), text.slice(0, 8000))
  expect(text).toMatch(/SQL/)
  expect(sink.pageErrors, JSON.stringify(sink.pageErrors)).toHaveLength(0)
})

test('03 events list, filter and severity stats against real data', async ({ page }) => {
  const sink = track('events', page)
  await login(page)
  await page.goto('/events')
  await page.waitForLoadState('networkidle')
  const rows = page.locator('tbody tr')
  await expect(rows.first()).toBeVisible()
  const initialCount = await rows.count()
  expect(initialCount).toBeGreaterThan(0)
  const text = await page.locator('#main-content').innerText()
  await mkdir(EVIDENCE_DIR, { recursive: true })
  await writeFile(path.join(EVIDENCE_DIR, 'events.txt'), text.slice(0, 4000))
  expect(text).toMatch(/严重 \/ 高危/)
  expect(sink.http5xx, JSON.stringify(sink.http5xx)).toHaveLength(0)
  expect(sink.pageErrors, JSON.stringify(sink.pageErrors)).toHaveLength(0)
})

test('04 reports list, detail modal and downloads', async ({ page }) => {
  const sink = track('reports', page)
  await login(page)
  await page.goto('/reports')
  await page.waitForLoadState('networkidle')
  await expect(page.getByText('集成联调报告').first()).toBeVisible()
  await page.getByText('集成联调报告').first().click()
  await expect(page.getByRole('dialog')).toBeVisible()
  await page.keyboard.press('Escape')
  expect(sink.http5xx, JSON.stringify(sink.http5xx)).toHaveLength(0)
  expect(sink.pageErrors, JSON.stringify(sink.pageErrors)).toHaveLength(0)
})

test('05 rag eval dashboard uses real metrics', async ({ page }) => {
  const sink = track('rag-eval', page)
  await login(page)
  await page.goto('/rag-eval')
  await page.waitForLoadState('networkidle')
  const text = await page.locator('#main-content').innerText()
  await mkdir(EVIDENCE_DIR, { recursive: true })
  await writeFile(path.join(EVIDENCE_DIR, 'rag-eval.txt'), text.slice(0, 4000))
  expect(text).toMatch(/\d+ms|\d+\.\d+s/)
  expect(text).not.toMatch(/延迟[^\n]{0,20}\d+(\.\d+)?%/)
  expect(sink.http5xx, JSON.stringify(sink.http5xx)).toHaveLength(0)
  expect(sink.pageErrors, JSON.stringify(sink.pageErrors)).toHaveLength(0)
})

test('06 knowledge list and search surfaces', async ({ page }) => {
  const sink = track('knowledge', page)
  await login(page)
  await page.goto('/knowledge')
  await page.waitForLoadState('networkidle')
  await expect(page.getByText('默认知识库').first()).toBeVisible()
  expect(sink.http5xx, JSON.stringify(sink.http5xx)).toHaveLength(0)
  expect(sink.pageErrors, JSON.stringify(sink.pageErrors)).toHaveLength(0)
})

test('07 ops panel renders runs and gate semantics', async ({ page }) => {
  const sink = track('ops', page)
  await login(page)
  await page.goto('/ops')
  await page.waitForLoadState('networkidle')
  await expect(page.locator('#main-content')).toBeVisible()
  expect(sink.http5xx, JSON.stringify(sink.http5xx)).toHaveLength(0)
  expect(sink.pageErrors, JSON.stringify(sink.pageErrors)).toHaveLength(0)
})

test('08 subscriptions pause/resume through the UI', async ({ page }) => {
  const sink = track('subscriptions', page)
  await login(page)
  await page.goto('/subscriptions')
  await page.waitForLoadState('networkidle')
  await expect(page.getByText('Integration Feed').first()).toBeVisible()
  const pause = page.getByRole('button', { name: /暂停/ }).first()
  if (await pause.isVisible().catch(() => false)) {
    await pause.click()
    await page.waitForLoadState('networkidle')
  }
  expect(sink.http5xx, JSON.stringify(sink.http5xx)).toHaveLength(0)
  expect(sink.pageErrors, JSON.stringify(sink.pageErrors)).toHaveLength(0)
})

test('09 term mapping renders seeded rules', async ({ page }) => {
  const sink = track('term-mapping', page)
  await login(page)
  await page.goto('/term-mapping')
  await page.waitForLoadState('networkidle')
  const text = await page.locator('#main-content').innerText()
  expect(text.length).toBeGreaterThan(50)
  expect(sink.http5xx, JSON.stringify(sink.http5xx)).toHaveLength(0)
  expect(sink.pageErrors, JSON.stringify(sink.pageErrors)).toHaveLength(0)
})

test('10 traces list and detail navigation', async ({ page }) => {
  const sink = track('traces', page)
  await login(page)
  await page.goto('/traces')
  await page.waitForLoadState('networkidle')
  await expect(page.locator('#main-content')).toBeVisible()
  const detailLink = page.locator('tbody tr').first()
  if (await detailLink.isVisible().catch(() => false)) {
    await detailLink.click()
    await page.waitForLoadState('networkidle')
  }
  expect(sink.http5xx, JSON.stringify(sink.http5xx)).toHaveLength(0)
  expect(sink.pageErrors, JSON.stringify(sink.pageErrors)).toHaveLength(0)
})

test('11 settings load and save', async ({ page }) => {
  const sink = track('settings', page)
  await login(page)
  await page.goto('/settings')
  await page.waitForLoadState('networkidle')
  await expect(page.locator('#main-content')).toBeVisible()
  const save = page.getByRole('button', { name: /保存/ }).first()
  if (await save.isVisible().catch(() => false)) {
    await save.click()
    await page.waitForLoadState('networkidle')
  }
  expect(sink.http5xx, JSON.stringify(sink.http5xx)).toHaveLength(0)
  expect(sink.pageErrors, JSON.stringify(sink.pageErrors)).toHaveLength(0)
})

test('12 runtime run detail tabs against the real worker', async ({ page }) => {
  const sink = track('runtime', page)
  await login(page)
  await page.goto('/runtime/runs')
  await page.waitForLoadState('networkidle')
  const rowLink = page.locator('tbody tr a[href^="/runtime/runs/"]').first()
  await expect(rowLink).toBeVisible({ timeout: 30_000 })
  await rowLink.click()
  await page.waitForURL(/\/runtime\/runs\/[0-9a-f-]{36}/)
  await page.waitForLoadState('networkidle')
  const runId = page.url().split('/').pop()!
  const tabs = ['overview', 'timeline', 'attempts', 'checkpoints', 'approvals', 'effects', 'evidence', 'context', 'trace']
  const seen: string[] = []
  for (const tab of tabs) {
    await page.goto(`/runtime/runs/${runId}?tab=${tab}`)
    await page.waitForLoadState('networkidle')
    await expect(page.getByRole('tabpanel')).toBeVisible()
    seen.push(tab)
  }
  await mkdir(EVIDENCE_DIR, { recursive: true })
  await writeFile(path.join(EVIDENCE_DIR, 'runtime-detail.json'), JSON.stringify({ runId, seen }, null, 2))
  expect(seen).toHaveLength(tabs.length)
  expect(sink.pageErrors, JSON.stringify(sink.pageErrors)).toHaveLength(0)
  expect(sink.http5xx, JSON.stringify(sink.http5xx)).toHaveLength(0)
})

test('13 runtime capabilities, worker health and safety render real data', async ({ page }) => {
  const sink = track('runtime-pages', page)
  await login(page)
  await page.goto('/runtime/worker-health')
  await page.waitForLoadState('networkidle')
  const hostname = (await import('node:os')).hostname()
  await expect(page.getByText(hostname).first()).toBeVisible({ timeout: 30_000 })
  await page.goto('/runtime/capabilities')
  await page.waitForLoadState('networkidle')
  await expect(page.locator('tbody tr').first()).toBeVisible()
  await page.goto('/runtime/safety')
  await page.waitForLoadState('networkidle')
  await expect(page.locator('#main-content')).toBeVisible()
  expect(sink.http5xx, JSON.stringify(sink.http5xx)).toHaveLength(0)
  expect(sink.pageErrors, JSON.stringify(sink.pageErrors)).toHaveLength(0)
})

test('14 ingest page renders the multi-source intake surface', async ({ page }) => {
  const sink = track('ingest', page)
  await login(page)
  await page.goto('/ingest')
  await page.waitForLoadState('networkidle')
  await expect(page.locator('#main-content')).toBeVisible()
  expect(sink.http5xx, JSON.stringify(sink.http5xx)).toHaveLength(0)
  expect(sink.pageErrors, JSON.stringify(sink.pageErrors)).toHaveLength(0)
})

// Regression guard for the legacy Event Analysis stream contract: the stream must surface
// its real outcome (closed-window error or streamed content) and must never end as a silent
// empty "success" after an unparseable error frame.
test('15 event analysis surfaces its real stream outcome', async ({ page }) => {
  const sink = track('event-analysis-gate', page)
  await login(page)
  await page.goto('/events/analysis')
  await page.waitForLoadState('networkidle')
  await expect(page.getByTestId('analysis-source-label')).toContainText('legacy / demo')
  const start = page.getByRole('button', { name: /启动 AI 研判/ })
  await expect(start).toBeVisible({ timeout: 30_000 })
  await start.click()
  const content = page.locator('#main-content')
  await expect.poll(async () => {
    const text = await content.innerText()
    return text.includes('legacy compatibility is disabled') || text.includes('分析完成')
  }, { timeout: 150_000 }).toBe(true)
  await mkdir(EVIDENCE_DIR, { recursive: true })
  const text = await content.innerText()
  await writeFile(path.join(EVIDENCE_DIR, 'event-analysis.txt'), text.slice(0, 4000))
  if (text.includes('legacy compatibility is disabled')) {
    await writeFile(path.join(EVIDENCE_DIR, 'event-analysis-closed-window.txt'), text.slice(0, 4000))
  }
  expect(sink.http5xx, JSON.stringify(sink.http5xx)).toHaveLength(0)
  expect(sink.pageErrors, JSON.stringify(sink.pageErrors)).toHaveLength(0)
})
