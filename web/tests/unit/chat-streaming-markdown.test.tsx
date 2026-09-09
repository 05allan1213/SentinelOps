import { act, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import Chat from '@/pages/chat'

const fixture = vi.hoisted(() => ({ chunk: undefined as undefined | ((agent: string, text: string) => void), done: undefined as undefined | (() => void), parses: 0, fail: false }))
vi.mock('@/services', () => ({ chatService: {
  listSessions: () => [{ id: 'session', title: 'Test' }], getSessionId: () => 'session', updateSession: vi.fn(),
  multiAgentChat: (_text: string, _index: number, _deep: boolean, _web: boolean, chunk: typeof fixture.chunk, done: typeof fixture.done) => { fixture.chunk = chunk; fixture.done = done },
} }))
vi.mock('remark-gfm', async (original) => {
  const module = await original<typeof import('remark-gfm')>()
  return { default: function (...args: Parameters<typeof module.default>) {
    fixture.parses++
    if (fixture.fail) throw new Error('fixture parser failure')
    return module.default.apply(this, args)
  } }
})
vi.mock('@/pages/chat/components/ChatInput', () => ({ default: ({ onSend }: { onSend: (text: string) => void }) => <button onClick={() => onSend('question')}>Send fixture</button> }))
vi.mock('@/pages/chat/components/SessionList', () => ({ default: () => null }))
vi.mock('@/pages/chat/components/WelcomeScreen', () => ({ default: ({ inputSlot }: { inputSlot: React.ReactNode }) => inputSlot }))

function start() {
  const view = render(<MemoryRouter><Chat /></MemoryRouter>)
  fireEvent.click(screen.getByText('Send fixture'))
  return view
}
function chunk(text: string, agent = 'assistant') { act(() => fixture.chunk!(agent, text)) }
function tick(ms = 120) { act(() => vi.advanceTimersByTime(ms)) }
beforeEach(() => { vi.useFakeTimers(); localStorage.clear(); fixture.parses = 0; fixture.fail = false })
afterEach(() => { vi.restoreAllMocks(); vi.useRealTimers() })

it('batches split fence chunks, preserves the raw pre and highlights closure before completion', () => {
  const { container } = start()
  chunk('```ts\nconst value = '); tick()
  const raw = container.querySelector('pre[data-streaming-markdown]')
  expect(raw).toHaveTextContent('const value =')
  const parses = fixture.parses
  for (const text of ['1', '2', '3', '\n`', '`']) chunk(text)
  expect(fixture.parses).toBe(parses)
  tick()
  expect(container.querySelector('pre[data-streaming-markdown]')).toBe(raw)
  expect(raw?.textContent).toBe('```ts\nconst value = 123\n``')
  chunk('`'); tick()
  expect(container.querySelector('.hljs-keyword')).toHaveTextContent('const')
  expect(container.querySelector('pre[data-streaming-markdown]')).toBeNull()
})

it('bounds parses, ignores empty updates, flushes completion once and retains renderer and code DOM', () => {
  const { container } = start()
  chunk('```js\nconst value = 1\n```'); tick()
  const renderer = container.querySelector('[data-markdown-variant="chat"]')
  const code = container.querySelector('[data-testid="code-block"]')
  const parses = fixture.parses
  for (let index = 0; index < 20; index++) chunk('')
  tick()
  expect(fixture.parses).toBe(parses)
  chunk('\n\nFinal')
  act(() => fixture.done!())
  expect(container.textContent).toContain('Final')
  expect(fixture.parses).toBe(parses + 1)
  expect(container.querySelector('[data-markdown-variant="chat"]')).toBe(renderer)
  expect(container.querySelector('[data-testid="code-block"]')).toBe(code)
  act(() => fixture.done!())
  tick(1000)
  expect(fixture.parses).toBe(parses + 1)
  expect(container.querySelector('[data-message-id] [class*="animate-"]')).toBeNull()
  expect(renderer?.parentElement).toHaveClass('min-w-0', 'max-w-full', 'w-full')
})

it('keeps all accumulated text and inline error on completion parser failure', () => {
  const { container } = start()
  chunk('```ts\nconst readable = true'); tick()
  fixture.fail = true
  const error = vi.spyOn(console, 'error').mockImplementation(() => {})
  act(() => fixture.done!())
  expect(screen.getByRole('status')).toHaveTextContent('Markdown 暂时无法解析')
  expect(container.textContent).toContain('const readable = true')
  error.mockRestore()
})

it('does not parse prose for every token or animate completed thinking', () => {
  const { container } = start()
  for (let index = 0; index < 30; index++) chunk('word ')
  expect(fixture.parses).toBe(0)
  tick()
  expect(fixture.parses).toBe(1)
  expect(container.querySelector('[data-markdown-variant="chat"]')?.textContent).toBe('word '.repeat(30).trimEnd())
  chunk(JSON.stringify({ type: 'think', content: 'retained thought' }), 'plan_step'); tick()
  act(() => fixture.done!())
  expect(container.querySelector('[data-message-id] [class*="animate-"]')).toBeNull()
  expect(container.textContent).toContain('retained thought')
})

it('keeps pending messages independent and cancels scheduled renders on unmount', () => {
  const { unmount } = start()
  const oldChunk = fixture.chunk!
  const oldDone = fixture.done!
  chunk('old response ')
  act(() => oldDone())
  fireEvent.click(screen.getByText('Send fixture'))
  chunk('new response')
  act(() => oldChunk('assistant', 'tail'))
  tick()
  expect(screen.getByText('old response tail')).toBeInTheDocument()
  expect(screen.getByText('new response')).toBeInTheDocument()
  act(() => oldDone())
  expect(document.querySelectorAll('[data-message-id] [class*="animate-"]')).toHaveLength(1)
  chunk(' pending')
  unmount()
  tick(1000)
  expect(vi.getTimerCount()).toBe(0)
})

it('waits for both the 100 ms interval and an animation frame', () => {
  const frames: FrameRequestCallback[] = []
  const raf = vi.spyOn(window, 'requestAnimationFrame').mockImplementation((callback) => { frames.push(callback); return frames.length })
  start()
  chunk('bounded prose')
  tick(99)
  expect(fixture.parses).toBe(0)
  expect(frames).toHaveLength(0)
  tick(1)
  expect(fixture.parses).toBe(0)
  expect(frames).toHaveLength(1)
  act(() => frames[0](performance.now()))
  expect(fixture.parses).toBe(1)
  raf.mockRestore()
})

it('retries a failed parser once on completion even when the final text is identical', () => {
  const { container } = start()
  fixture.fail = true
  const error = vi.spyOn(console, 'error').mockImplementation(() => {})
  chunk('# Readable answer'); tick()
  expect(screen.getByRole('status')).toHaveTextContent('Markdown 暂时无法解析')
  fixture.fail = false
  act(() => fixture.done!())
  expect(screen.getByRole('heading', { name: 'Readable answer' })).toBeInTheDocument()
  expect(container.querySelector('[data-message-id] [class*="animate-"]')).toBeNull()
  error.mockRestore()
})
