import { expect, test, type Page } from '@playwright/test'

declare global {
  interface Window {
    chatFixtureChunk: (text: string) => Promise<void>
    chatFixtureDone: () => void
  }
}

async function installDelayedStream(page: Page) {
  await page.addInitScript(() => {
    localStorage.setItem('token', 'chat-markdown-fixture-token')
    const realFetch = window.fetch.bind(window)
    window.fetch = async (input, init) => {
      if (!String(input).startsWith('/api/chat/v2/runs/')) return realFetch(input, init)
      const encoder = new TextEncoder()
      let seq = 0
      const stream = new ReadableStream<Uint8Array>({ start(controller) {
        // Exercise the real Chat caller and existing service/reader with delayed
        // bytes. This is a browser fixture, not provider/runtime evidence.
        window.chatFixtureChunk = (text) => new Promise((resolve) => {
          setTimeout(() => {
            controller.enqueue(encoder.encode(`id: ${++seq}\nevent: agent.plan\ndata: ${JSON.stringify({ summary: JSON.stringify({ response: text }) })}\n\n`))
            resolve()
          }, 40)
        })
        window.chatFixtureDone = () => {
          controller.enqueue(encoder.encode(`id: ${++seq}\nevent: run.completed\ndata: ${JSON.stringify({ data: { to_status: 'succeeded' } })}\n\n`))
          controller.close()
        }
      } })
      return new Response(stream, { headers: { 'Content-Type': 'text/event-stream' } })
    }
  })
  await page.route('**/api/**', (route) => route.fulfill({ contentType: 'application/json', body: JSON.stringify({ message: 'OK', data: route.request().url().endsWith('/chat/v2/runs') ? { run_id: 'markdown-run', session_id: route.request().postDataJSON().session_id, status: 'pending' } : {} }) }))
}

for (const width of [1280, 1440]) {
  test(`actual Chat delayed fence remains stable at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    await page.emulateMedia({ reducedMotion: 'reduce' })
    const errors: string[] = []
    page.on('pageerror', (error) => errors.push(error.message))
    await installDelayedStream(page)
    await page.goto('/chat')
    await page.locator('textarea').fill('Show a delayed code sample')
    await page.locator('textarea').press('Enter')
    await expect.poll(() => page.evaluate(() => typeof window.chatFixtureChunk)).toBe('function')
    const code = `const long = "${'source'.repeat(200)}"\n${'// retained evidence\n'.repeat(55)}`
    await page.evaluate((text) => window.chatFixtureChunk(text), `# Retained answer\n\n\`\`\`ts\n${code}`)
    const raw = page.locator('pre[data-streaming-markdown]')
    await expect(raw).toContainText('retained evidence')
    const renderer = page.locator('[data-markdown-variant="chat"]')
    const bubble = renderer.locator('..')
    const initialWidth = await bubble.evaluate((el) => el.getBoundingClientRect().width)
    const rawHandle = await raw.elementHandle()
    await page.evaluate(() => window.chatFixtureChunk('`'))
    await page.evaluate(() => window.chatFixtureChunk('`'))
    await expect(raw).toContainText('``')
    expect(await rawHandle!.evaluate((el) => el.isConnected)).toBe(true)
    expect(await page.locator('[data-message-id]').evaluate((el) => el.getAnimations({ subtree: true }).filter((a) => a.playState === 'running').length)).toBe(0)
    await page.evaluate(() => window.chatFixtureChunk('`\n\n'))
    await expect(page.locator('pre .hljs-keyword')).toHaveText('const')
    await expect(raw).toHaveCount(0)
    expect(await bubble.evaluate((el) => el.getBoundingClientRect().width)).toBe(initialWidth)
    const rendererHandle = await renderer.elementHandle()
    const codeHandle = await page.getByTestId('code-block').elementHandle()
    const scroll = page.getByTestId('chat-scroll')
    await expect.poll(() => scroll.evaluate((el) => el.scrollHeight - el.scrollTop - el.clientHeight)).toBeLessThan(2)
    // Scrolling up must remain under user control as more content arrives.
    await scroll.evaluate((el) => { el.scrollTop = 30; el.dispatchEvent(new Event('scroll')) })
    await page.evaluate(() => window.chatFixtureChunk(' More evidence retained.'))
    await expect(renderer).toContainText('More evidence retained.')
    expect(await scroll.evaluate((el) => el.scrollTop)).toBe(30)
    await page.evaluate(() => window.chatFixtureDone())
    await expect(page.locator('[data-message-id] [class*="animate-"]')).toHaveCount(0)
    expect(await rendererHandle!.evaluate((el) => el.isConnected)).toBe(true)
    expect(await codeHandle!.evaluate((el) => el.isConnected)).toBe(true)
    expect(await bubble.evaluate((el) => el.getBoundingClientRect().width)).toBe(initialWidth)
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width)
    expect(await scroll.evaluate((el) => el.scrollTop)).toBe(30)
    expect(errors).toEqual([])
  })
}
