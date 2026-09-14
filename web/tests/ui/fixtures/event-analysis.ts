import type { Page } from '@playwright/test'

// Test-only controlled Event Analysis transport.
// axios endpoints (list / report create) are fulfilled through page.route;
// the two SSE endpoints are fulfilled with controlled ReadableStreams so a
// test can deliver chunks over time instead of one synchronous response.

export interface FixtureEvent {
  id: string
  title: string
  severity: 'critical' | 'high' | 'medium' | 'low' | 'info'
  cve_id?: string
  risk_score?: number
  source?: string
  source_url?: string
  created_at?: string
}

export const CRITICAL_EVENT: FixtureEvent = {
  id: 'evt-critical-1',
  title: 'Apache Tomcat 远程代码执行漏洞',
  severity: 'critical',
  cve_id: 'CVE-2026-0001',
  risk_score: 9.8,
  source: 'NVD',
  source_url: 'https://nvd.nist.gov/vuln/detail/CVE-2026-0001',
  created_at: '2026-09-14T02:00:00Z',
}

export const HIGH_EVENT: FixtureEvent = {
  id: 'evt-high-1',
  title: 'OpenSSL 拒绝服务漏洞',
  severity: 'high',
  cve_id: 'CVE-2026-0002',
  risk_score: 7.5,
  source: 'CISA',
  source_url: 'https://example.invalid/CVE-2026-0002',
  created_at: '2026-09-14T01:00:00Z',
}

export const MEDIUM_EVENT: FixtureEvent = {
  id: 'evt-medium-1',
  title: 'Nginx 配置信息泄露',
  severity: 'medium',
  cve_id: 'CVE-2026-0003',
  risk_score: 5.3,
  source: 'Vendor',
  created_at: '2026-09-13T22:00:00Z',
}

export const ANALYSIS_EVENTS: FixtureEvent[] = [CRITICAL_EVENT, HIGH_EVENT, MEDIUM_EVENT]

export type PipelineMode = 'stream' | 'sse-error' | 'http-500'

export interface EventAnalysisFixture {
  setListEvents(events: FixtureEvent[], total?: number): void
  setListStatus(status: number): void
  setPipelineMode(mode: PipelineMode): Promise<void>
  pipelineQueries(): Promise<Array<string | undefined>>
  pipelineCancelCount(): Promise<number>
  pipelineIsOpen(): Promise<boolean>
  emitContent(text: string): Promise<void>
  emitDone(): Promise<void>
  closePipeline(): Promise<void>
  reportSaves: Array<{ title?: string; content?: string; type?: string }>
  listRequests: string[]
}

declare global {
  interface Window {
    eventAnalysisFixture: {
      listFailures: boolean
      pipelineRequests: Array<{ query?: string }>
      pipelineCancels: number
      pipelineOpen: boolean
      pipelineMode: PipelineMode
      analyzeRequests: Array<{ event_id?: string }>
      emitContent: (text: string) => void
      emitDone: () => void
      emitError: (message: string) => void
      closePipeline: () => void
    }
  }
}

interface InstallOptions {
  events?: FixtureEvent[]
  total?: number
  clearStoredResult?: boolean
}

function toListPayload(events: FixtureEvent[]) {
  return events.map((event) => ({
    id: event.id,
    title: event.title,
    event_type: 'vulnerability',
    severity: event.severity,
    source: event.source ?? '',
    source_url: event.source_url ?? '',
    status: 'new',
    cve_id: event.cve_id ?? '',
    risk_score: event.risk_score,
    created_at: event.created_at ?? '2026-09-14T00:00:00Z',
  }))
}

export async function installEventAnalysisFixture(
  page: Page,
  options: InstallOptions = {},
): Promise<EventAnalysisFixture> {
  const state = {
    events: options.events ?? ANALYSIS_EVENTS,
    total: options.total ?? (options.events ?? ANALYSIS_EVENTS).length,
    listStatus: 200,
  }
  const reportSaves: EventAnalysisFixture['reportSaves'] = []
  const listRequests: string[] = []

  await page.addInitScript(({ clearStoredResult }) => {
    localStorage.setItem('token', 'event-analysis-fixture')
    localStorage.setItem('app-storage', JSON.stringify({ state: { theme: 'light' }, version: 2 }))
    if (clearStoredResult) localStorage.removeItem('analyze-result-v1')

    const encoder = new TextEncoder()
    const asJson = (data: unknown, status = 200) =>
      new Response(JSON.stringify(data), { status, headers: { 'Content-Type': 'application/json' } })

    const fixture: Window['eventAnalysisFixture'] = {
      listFailures: false,
      pipelineRequests: [],
      pipelineCancels: 0,
      pipelineOpen: false,
      pipelineMode: 'stream',
      analyzeRequests: [],
      emitContent: () => {},
      emitDone: () => {},
      emitError: () => {},
      closePipeline: () => {},
    }
    window.eventAnalysisFixture = fixture

    const realFetch = window.fetch.bind(window)
    window.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
      const url =
        typeof input === 'string' ? input
          : input instanceof URL ? input.toString()
            : input.url
      const bodyText = typeof init?.body === 'string' ? init.body : '{}'

      if (url.includes('/api/event/v1/pipeline/stream')) {
        let parsed: { query?: string } = {}
        try { parsed = JSON.parse(bodyText) } catch { /* ignore malformed fixture body */ }
        fixture.pipelineRequests.push({ query: parsed.query })

        if (fixture.pipelineMode === 'http-500') {
          return asJson({ code: 500, message: 'internal error', data: null }, 500)
        }

        let streamController: ReadableStreamDefaultController<Uint8Array> | null = null
        const stream = new ReadableStream<Uint8Array>({
          start(controller) {
            streamController = controller
            fixture.pipelineOpen = true
            fixture.emitContent = (text: string) => {
              controller.enqueue(encoder.encode(`data: ${JSON.stringify({ type: 'content', content: text })}\n\n`))
            }
            fixture.emitDone = () => {
              controller.enqueue(encoder.encode('data: [DONE]\n\n'))
            }
            fixture.emitError = (message: string) => {
              controller.enqueue(encoder.encode(`data: ${JSON.stringify({ type: 'error', content: message })}\n\n`))
            }
            fixture.closePipeline = () => {
              fixture.pipelineOpen = false
              try { controller.close() } catch { /* already closed */ }
            }
          },
          cancel() {
            fixture.pipelineCancels += 1
            fixture.pipelineOpen = false
          },
        })

        if (fixture.pipelineMode === 'sse-error') {
          setTimeout(() => {
            try { fixture.emitError('分析失败：上游模型超时') } catch { /* stream already gone */ }
          }, 30)
        }

        init?.signal?.addEventListener('abort', () => {
          fixture.pipelineCancels += 1
          fixture.pipelineOpen = false
          try { streamController?.error(new DOMException('Aborted', 'AbortError')) } catch { /* already closed */ }
        })

        return new Response(stream, { status: 200, headers: { 'Content-Type': 'text/event-stream' } })
      }

      if (url.includes('/api/event/v1/analyze/stream')) {
        let parsed: { event_id?: string } = {}
        try { parsed = JSON.parse(bodyText) } catch { /* ignore malformed fixture body */ }
        fixture.analyzeRequests.push({ event_id: parsed.event_id })

        const chunks = ['## 处置建议\n', '- 升级到已修复版本\n', '- 在边界设备临时拦截利用流量\n']
        const stream = new ReadableStream<Uint8Array>({
          start(controller) {
            let index = 0
            const timer = setInterval(() => {
              if (index < chunks.length) {
                controller.enqueue(encoder.encode(`data: ${JSON.stringify({ type: 'content', content: chunks[index++] })}\n\n`))
                return
              }
              clearInterval(timer)
              controller.enqueue(encoder.encode('data: [DONE]\n\n'))
              try { controller.close() } catch { /* already closed */ }
            }, 20)
          },
        })
        return new Response(stream, { status: 200, headers: { 'Content-Type': 'text/event-stream' } })
      }

      // Unrelated endpoints keep the real transport so nothing is silently faked.
      return realFetch(input as RequestInfo, init)
    }
  }, {
    clearStoredResult: options.clearStoredResult ?? true,
  })

  await page.route('**/api/**', async (route) => {
    const url = route.request().url()
    if (url.includes('/api/event/v1/list')) {
      listRequests.push(url)
      if (state.listStatus !== 200) {
        await route.fulfill({
          status: state.listStatus,
          json: { code: state.listStatus, message: 'list unavailable', data: null },
        })
        return
      }
      const keyword = new URL(url).searchParams.get('keyword')
      const events = keyword
        ? state.events.filter(e => e.title.includes(keyword) || (e.cve_id ?? '').includes(keyword))
        : state.events
      await route.fulfill({
        json: {
          code: 0,
          message: 'OK',
          data: { total: keyword ? events.length : state.total, events: toListPayload(events) },
        },
      })
      return
    }
    if (url.includes('/api/report/v1/create')) {
      const payload = route.request().postData()
      reportSaves.push(payload ? JSON.parse(payload) : {})
      await route.fulfill({ json: { code: 0, message: 'OK', data: { id: 'REPORT-TEST-1' } } })
      return
    }
    await route.fulfill({ json: { code: 0, message: 'OK', data: {} } })
  })

  const pageState = async <T>(fn: () => T) => page.evaluate(fn)

  return {
    setListEvents(events, total) {
      state.events = events
      state.total = total ?? events.length
    },
    setListStatus(status) {
      state.listStatus = status
    },
    async setPipelineMode(mode) {
      await page.evaluate((value) => {
        window.eventAnalysisFixture.pipelineMode = value
      }, mode)
    },
    pipelineQueries() {
      return pageState(() => window.eventAnalysisFixture.pipelineRequests.map(r => r.query))
    },
    pipelineCancelCount() {
      return pageState(() => window.eventAnalysisFixture.pipelineCancels)
    },
    pipelineIsOpen() {
      return pageState(() => window.eventAnalysisFixture.pipelineOpen)
    },
    emitContent(text) {
      return page.evaluate((value) => window.eventAnalysisFixture.emitContent(value), text)
    },
    emitDone() {
      return page.evaluate(() => window.eventAnalysisFixture.emitDone())
    },
    closePipeline() {
      return page.evaluate(() => window.eventAnalysisFixture.closePipeline())
    },
    reportSaves,
    listRequests,
  }
}
