import { expect, test, type Page } from '@playwright/test'

declare global {
  interface Window {
    durableFixture: {
      emit: (seq: number, type: string, summary?: string, data?: object) => void
      disconnect: () => void
      done: () => void
      urls: string[]
      canceled: number
      rejectNext: boolean
    }
  }
}
async function setup(page: Page) {
  const creates: string[] = []
  await page.addInitScript(() => {
    localStorage.setItem('token', 'controlled-chat-token')
    const realFetch = window.fetch.bind(window)
    const fixture = window.durableFixture = { emit: () => {}, disconnect: () => {}, done: () => {}, urls: [] as string[], canceled: 0, rejectNext: false }
    window.fetch = async (input, init) => {
      if (!String(input).startsWith('/api/chat/v2/runs/')) return realFetch(input, init)
      fixture.urls.push(String(input))
      if (fixture.rejectNext) { fixture.rejectNext = false; return new Response('', { status: 403 }) }
      const encoder = new TextEncoder()
      return new Response(new ReadableStream<Uint8Array>({
        start(controller) {
          fixture.emit = (seq, type, summary = '', data = {}) => controller.enqueue(encoder.encode(`id: ${seq}\nevent: ${type}\ndata: ${JSON.stringify({ summary, data })}\n\n`))
          fixture.disconnect = () => controller.close()
          fixture.done = () => { controller.enqueue(encoder.encode('data: [DONE]\n\n')); controller.close() }
        },
        cancel() { fixture.canceled++ },
      }), { headers: { 'Content-Type': 'text/event-stream' } })
    }
  })
  await page.route('**/api/**', route => {
    let data = {}
    if (route.request().url().endsWith('/chat/v2/runs')) {
      creates.push(route.request().postDataJSON().query)
      data = { run_id: `run-${creates.length}`, session_id: route.request().postDataJSON().session_id, status: 'pending' }
    }
    return route.fulfill({ contentType: 'application/json', body: JSON.stringify({ message: 'OK', data }) })
  })
  await page.goto('/chat')
  return creates
}
async function send(page: Page, query = 'Inspect a controlled workflow') {
  await page.locator('textarea').fill(query); await page.locator('textarea').press('Enter')
  await expect.poll(() => page.evaluate(() => window.durableFixture.urls.length)).toBeGreaterThan(0)
}
for (const width of [1280, 1440]) {
  test(`dedupe, disconnect, reload and deliberate next turn at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    const creates = await setup(page)
    await send(page)
    await page.evaluate(() => {
      const f = window.durableFixture
      f.emit(1, 'agent.plan', '{"steps":["inspect fixture"]}')
      f.emit(1, 'agent.plan', '{"steps":["inspect fixture"]}')
      f.emit(3, 'agent.plan', '{"response":"# Retained body\\n\\nControlled evidence."}')
      f.emit(2, 'agent.plan', '{"response":"STALE"}')
      f.emit(4, 'agent.tool_result', '\n\nUnique tool evidence')
      f.emit(4, 'agent.tool_result', '\n\nUnique tool evidence')
      f.emit(5, 'operation.completed', '', { to_status: 'succeeded' })
      f.disconnect()
    })
    const renderer = page.locator('[data-markdown-variant="chat"]')
    await expect(renderer).toContainText('Controlled evidence.')
    await expect(renderer).not.toContainText('STALE')
    expect((await renderer.innerText()).match(/Unique tool evidence/g)).toHaveLength(1)
    await expect(page.getByTestId('run-status')).not.toContainText('已成功完成')
    await expect.poll(() => page.evaluate(() => window.durableFixture.urls.length)).toBe(2)
    expect(await page.evaluate(() => window.durableFixture.urls[1])).toContain('after_seq=5')
    expect(creates).toHaveLength(1)
    // Reload while a live recovery tail is open: accepted seq/body survive.
    await page.reload()
    await expect.poll(() => page.evaluate(() => window.durableFixture.urls.length)).toBe(1)
    expect(await page.evaluate(() => window.durableFixture.urls[0])).toContain('after_seq=5')
    await expect(renderer).toContainText('Controlled evidence.')
    await page.evaluate(() => {
      window.durableFixture.emit(4, 'agent.tool_result', 'Duplicate after reload')
      window.durableFixture.emit(6, 'agent.plan', '{"response":" Continued after reload."}')
      window.durableFixture.emit(7, 'run.completed', '', { to_status: 'succeeded' })
    })
    await expect(page.getByTestId('run-status')).toContainText('已成功完成')
    await expect(renderer).not.toContainText('Duplicate after reload')
    await expect(renderer).toContainText('Continued after reload.')
    expect(creates).toHaveLength(1)
    await send(page, 'An intentional next turn')
    await expect.poll(() => creates.length).toBe(2)
    await expect.poll(() => page.evaluate(() => window.durableFixture.urls.at(-1))).toBe('/api/chat/v2/runs/run-2/events?after_seq=0')
    await page.evaluate(() => window.durableFixture.emit(1, 'run.failed', 'Canceled by server', { to_status: 'canceled' }))
    await expect(page.getByTestId('run-status').last()).toContainText('已取消')
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width)
  })

  test(`preserves body and actionable error; retry only tails and cleanup at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    const creates = await setup(page)
    await send(page)
    await page.evaluate(() => {
      window.durableFixture.emit(1, 'agent.plan', '{"response":"Never discard this body"}')
      window.durableFixture.rejectNext = true
      window.durableFixture.disconnect()
    })
    await expect(page.getByRole('alert')).toContainText('403')
    await expect(page.locator('[data-markdown-variant="chat"]')).toContainText('Never discard this body')
    await page.getByRole('button', { name: '重试连接', exact: true }).click()
    await expect.poll(() => page.evaluate(() => window.durableFixture.urls.length)).toBe(3)
    expect(creates).toHaveLength(1)
    await page.evaluate(() => window.durableFixture.done())
    await expect(page.getByTestId('run-status')).not.toContainText('已成功完成')
    await page.getByRole('button', { name: '重试连接', exact: true }).click()
    await expect.poll(() => page.evaluate(() => window.durableFixture.urls.length)).toBe(4)
    const before = await page.evaluate(() => window.durableFixture.canceled)
    await page.getByRole('button', { name: '新建对话 从空白开始' }).click()
    await expect.poll(() => page.evaluate(() => window.durableFixture.canceled)).toBeGreaterThan(before)
    expect(creates).toHaveLength(1)
    await page.getByText('Inspect a controlled workflow', { exact: true }).click()
    await expect.poll(() => page.evaluate(() => window.durableFixture.urls.length)).toBe(5)
    const beforeUnmount = await page.evaluate(() => window.durableFixture.canceled)
    await page.evaluate(() => { window.history.pushState({}, '', '/events'); window.dispatchEvent(new PopStateEvent('popstate')) })
    await expect.poll(() => page.evaluate(() => window.durableFixture.canceled)).toBeGreaterThan(beforeUnmount)
    expect(creates).toHaveLength(1)
  })
  test(`server recovery states and DONE-only reload stay truthful at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    const creates = await setup(page)
    await send(page)
    await page.evaluate(() => {
      window.durableFixture.emit(1, 'agent.plan', '{"response":"Recoverable body"}')
      window.durableFixture.emit(2, 'run.failed', 'Temporary worker failure', { to_status: 'retryable_failed', retryable: true })
    })
    await expect(page.getByTestId('run-status')).toContainText('失败，等待恢复')
    await expect(page.getByRole('alert')).toContainText('Temporary worker failure')
    await page.evaluate(() => window.durableFixture.emit(3, 'run.reconciling', '', { to_status: 'reconciling' }))
    await expect(page.getByTestId('run-status')).toContainText('正在核对执行结果')
    await expect(page.getByRole('alert')).toHaveCount(0)
    await page.evaluate(() => window.durableFixture.emit(4, 'run.parked', 'Needs operator review', { to_status: 'parked' }))
    await expect(page.getByTestId('run-status')).toContainText('已搁置')
    await expect(page.locator('[data-markdown-variant="chat"]')).toContainText('Recoverable body')
    await page.reload()
    await expect.poll(() => page.evaluate(() => window.durableFixture.urls.length)).toBe(1)
    await page.evaluate(() => window.durableFixture.done())
    await expect(page.getByTestId('run-status')).toContainText('已搁置')
    await expect(page.getByRole('alert')).toContainText('Needs operator review')
    expect(creates).toHaveLength(1)
  })

  test(`accepted create after session switch retains its own identity at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    await setup(page)
    let release!: () => void
    const gate = new Promise<void>(resolve => { release = resolve })
    let requestCount = 0
    let originalSession = ''
    await page.route('**/api/chat/v2/runs', async route => {
      requestCount++
      originalSession = route.request().postDataJSON().session_id
      await gate
      await route.fulfill({ contentType: 'application/json', body: JSON.stringify({ message: 'OK', data: {
        run_id: 'accepted-after-switch', session_id: originalSession, status: 'pending',
      } }) })
    })
    await page.locator('textarea').fill('Pending create')
    await page.locator('textarea').press('Enter')
    await expect.poll(() => requestCount).toBe(1)
    await page.getByRole('button', { name: '新建对话 从空白开始' }).click()
    release()
    await expect.poll(() => page.evaluate(sid => sessionStorage.getItem(`chat_run_id_${sid}`), originalSession)).toBe('accepted-after-switch')
    expect(await page.evaluate(() => window.durableFixture.urls)).toEqual([])
    await expect(page.getByTestId('run-status')).toHaveCount(0)
    await page.getByText('Pending create', { exact: true }).click()
    await expect.poll(() => page.evaluate(() => window.durableFixture.urls[0])).toBe('/api/chat/v2/runs/accepted-after-switch/events?after_seq=0')
    await page.evaluate(() => window.durableFixture.emit(1, 'run.failed', 'Definitive failure', { to_status: 'failed' }))
    await expect(page.getByTestId('run-status')).toContainText('执行失败')
    expect(requestCount).toBe(1)
  })

  test(`switch back before create returns joins one request at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    await setup(page)
    let release!: () => void
    const gate = new Promise<void>(resolve => { release = resolve })
    let creates = 0
    await page.route('**/api/chat/v2/runs', async route => {
      creates++
      const sid = route.request().postDataJSON().session_id
      await gate
      await route.fulfill({ contentType: 'application/json', body: JSON.stringify({ message: 'OK', data: { run_id: 'late-run', session_id: sid, status: 'pending' } }) })
    })
    await page.locator('textarea').fill('Return before response')
    await page.locator('textarea').press('Enter')
    await expect.poll(() => creates).toBe(1)
    await page.getByRole('button', { name: '新建对话 从空白开始' }).click()
    await page.getByText('Return before response', { exact: true }).click()
    release()
    await expect.poll(() => page.evaluate(() => window.durableFixture.urls), { timeout: 3000 }).toEqual(['/api/chat/v2/runs/late-run/events?after_seq=0'])
    await page.evaluate(() => window.durableFixture.emit(1, 'run.claimed', '', { owner: 'worker', lease_generation: 1, attempt: 1 }))
    await expect(page.getByTestId('run-status')).toContainText('执行中')
    await page.evaluate(() => window.durableFixture.emit(2, 'approval.requested', '', { approval_id: 'a', proposal_hash: 'hash', checkpoint_id: 'cp', checkpoint_payload_sha256: 'sha', checkpoint_lease_generation: 1 }))
    await expect(page.getByTestId('run-status')).toContainText('等待审批')
    expect(creates).toBe(1)
  })

  for (const priorRun of [false, true]) {
    test(`unacknowledged ${priorRun ? 'next' : 'first'} turn stays uncertain through switch and reload at ${width}px`, async ({ page }) => {
      await page.setViewportSize({ width, height: 900 })
      await setup(page)
      if (priorRun) {
        await send(page, 'Completed earlier turn')
        await page.evaluate(() => window.durableFixture.emit(1, 'run.completed', '', { to_status: 'succeeded' }))
        await expect(page.getByTestId('run-status')).toContainText('已成功完成')
      }
      let release!: () => void
      const gate = new Promise<void>(resolve => { release = resolve })
      let creates = 0
      await page.route('**/api/chat/v2/runs', async route => {
        creates++
        await gate
        await route.abort().catch(() => {})
      })
      await page.locator('textarea').fill('Unacknowledged turn')
      await page.locator('textarea').press('Enter')
      await expect.poll(() => creates).toBe(1)
      await page.getByRole('button', { name: '新建对话 从空白开始' }).click()
      await page.getByText(priorRun ? 'Completed earlier turn' : 'Unacknowledged turn', { exact: true }).click()
      await page.reload()
      try {
        await expect(page.getByRole('alert')).toContainText('创建结果尚未确认', { timeout: 3000 })
        expect(await page.evaluate(() => window.durableFixture.urls)).toEqual([])
        expect(creates).toBe(1)
      } finally { release() }
    })
  }

  test(`legacy pending snapshot stays uncertain after restore at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    const creates = await setup(page)
    // Snapshot written before the create outcome was recorded explicitly.
    await page.evaluate(() => {
      localStorage.setItem('current_session_id', 'legacy-session')
      localStorage.setItem('chat_sessions', JSON.stringify([{ id: 'legacy-session', title: 'Legacy', createdAt: 1, lastMessageAt: 1, messageCount: 2 }]))
      localStorage.setItem('chat_messages_legacy-session', JSON.stringify([
        { id: 'prior-user', role: 'user', content: 'Legacy question', timestamp: 1 },
        { id: 'prior-assistant', role: 'assistant', content: '', timestamp: 1, isStreaming: true },
      ]))
    })
    await page.reload()

    await expect(page.getByRole('alert')).toContainText('创建结果尚未确认')
    expect(await page.evaluate(() => window.durableFixture.urls)).toEqual([])
    expect(creates).toEqual([])
    const [restored] = await page.evaluate(() => JSON.parse(localStorage.getItem('chat_messages_legacy-session')!).slice(-1))
    expect(restored).toMatchObject({ createUnconfirmed: true, isStreaming: false })
  })

  test(`upload preserves the accepted snapshot before the render frame at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    await setup(page)
    await send(page)
    await expect(page.getByTestId('run-status')).toContainText('等待执行')
    await page.locator('button').filter({ has: page.locator('svg.lucide-paperclip') }).click()
    await page.locator('input[type="file"]').setInputFiles({ name: 'fixture.md', mimeType: 'text/markdown', buffer: Buffer.from('# uploaded fixture') })
    let release!: () => void
    const gate = new Promise<void>(resolve => { release = resolve })
    let uploads = 0
    await page.route('**/api/knowledge/v1/docs/upload', async route => {
      uploads++
      await gate
      await route.fulfill({ contentType: 'application/json', body: JSON.stringify({ message: 'OK', data: { doc_id: 'fixture-doc' } }) })
    })
    await page.getByRole('button', { name: '上传并索引' }).click()
    await expect.poll(() => uploads).toBe(1)
    await page.clock.install()
    await page.clock.pauseAt(Date.now() + 1000)
    await page.evaluate(() => {
      window.durableFixture.emit(1, 'agent.plan', '{"response":"Accepted before upload"}')
      window.durableFixture.emit(2, 'agent.plan', '{"steps":["Preserved plan"]}')
    })
    await expect.poll(() => page.evaluate(() => sessionStorage.getItem(`chat_last_seq_${localStorage.getItem('current_session_id')}`))).toBe('2')
    // Render time is paused: the callback sees the previous React message value.
    release()
    await expect(page.getByText('文件已上传到知识库', { exact: true })).toBeVisible()
    const snapshot = await page.evaluate(() => JSON.parse(localStorage.getItem(`chat_messages_${localStorage.getItem('current_session_id')}`)!))
    expect(snapshot[1].content).toBe('Accepted before upload')
    expect(snapshot[1].planning).toContain('Preserved plan')
    await page.clock.resume()
    await page.reload()
    await expect.poll(() => page.evaluate(() => window.durableFixture.urls[0])).toBe('/api/chat/v2/runs/run-1/events?after_seq=2')
    await expect(page.locator('[data-markdown-variant="chat"]').first()).toContainText('Accepted before upload')
    const restored = await page.evaluate(() => JSON.parse(localStorage.getItem(`chat_messages_${localStorage.getItem('current_session_id')}`)!))
    expect(restored[1].planning).toBe(snapshot[1].planning)
    await page.evaluate(() => window.durableFixture.done())
  })

}
