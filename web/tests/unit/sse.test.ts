import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { SSECursor, streamFetch } from '@/utils/sse'
const frame = (id: number | string, event = 'message.delta', data: unknown = { text: 'hello' }) => `id:${id}\nevent:${event}\ndata:${JSON.stringify(data)}\n\n`
const response = (...chunks: string[]) => new Response(new ReadableStream({ start(c) { chunks.forEach(chunk => c.enqueue(new TextEncoder().encode(chunk))); c.close() } }))
const flush = async () => { for (let i = 0; i < 30; i++) await Promise.resolve() }
beforeEach(() => { vi.useFakeTimers(); vi.stubGlobal('fetch', vi.fn()); localStorage.clear() })
afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals() })
describe('shared SSE transport', () => {
  it('parses byte-split UTF-8, CRLF, multiline JSON and final frames; dedupes', async () => {
    const bytes = new TextEncoder().encode('id:1\r\nevent:message.delta\r\ndata:{"text":\r\ndata:"你好"}\r\n\r\n' + frame(1) + 'id:2\nevent:run.completed\ndata:{}')
    vi.mocked(fetch).mockResolvedValue(new Response(new ReadableStream({ start(c) { for (const byte of bytes) c.enqueue(new Uint8Array([byte])); c.close() } })))
    const chunk = vi.fn(), done = vi.fn()
    const handle = streamFetch('/events', { method: 'GET', runId: 'r' }, chunk, done)
    await handle.finished
    expect(chunk).toHaveBeenCalledTimes(2)
    expect(JSON.parse(chunk.mock.calls[0][1])).toEqual({ text: '你好' })
    expect(handle.cursor.lastSeq).toBe(2)
    expect(done).toHaveBeenCalledOnce()
    expect(fetch).toHaveBeenCalledWith('/events?after_seq=0', expect.objectContaining({ method: 'GET' }))
    expect(vi.mocked(fetch).mock.calls[0][1]).not.toHaveProperty('body')
  })
  it('reconnects exactly five times with bounded delays and latest cursor, preserving content', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(response(frame(4))).mockRejectedValue(new TypeError('offline'))
    const chunk = vi.fn(), error = vi.fn(), retry = vi.fn(), done = vi.fn()
    const handle = streamFetch('/events?limit=20&after_seq=1', { method: 'GET', runId: 'r', maxRetries: 99, onRetry: retry }, chunk, done, error)
    await flush()
    for (const [i, delay] of [250, 500, 1000, 2000, 4000].entries()) {
      expect(retry).toHaveBeenLastCalledWith(i + 1, delay)
      await vi.advanceTimersByTimeAsync(delay - 1)
      expect(fetch).toHaveBeenCalledTimes(i + 1)
      await vi.advanceTimersByTimeAsync(1)
    }
    await handle.finished
    expect(fetch).toHaveBeenCalledTimes(6)
    expect(vi.mocked(fetch).mock.calls.slice(1).every(([url]) => url === '/events?limit=20&after_seq=4')).toBe(true)
    expect(chunk).toHaveBeenCalledOnce()
    expect(handle.cursor.lastSeq).toBe(4)
    expect(error).toHaveBeenCalledWith(expect.objectContaining({ code: 'retries_exhausted' }))
    expect(done).not.toHaveBeenCalled()
    expect(vi.getTimerCount()).toBe(0)
  })
  it.each([401, 403, 429])('stops HTTP %s without retry', async status => {
    vi.mocked(fetch).mockResolvedValue(new Response('', { status }))
    const error = vi.fn()
    await streamFetch('/events', { method: 'GET', runId: 'r' }, vi.fn(), vi.fn(), error).finished
    expect(fetch).toHaveBeenCalledOnce()
    expect(error).toHaveBeenCalledWith(expect.objectContaining({ code: 'http', status }))
  })
  it('retries 503 and ignores replayed IDs on recovery', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response('', { status: 503 })).mockResolvedValueOnce(response(frame(2) + frame(3, 'run.completed')))
    const chunk = vi.fn()
    const handle = streamFetch('/events', { method: 'GET', runId: 'r', initialAfterSeq: 2 }, chunk, vi.fn())
    await vi.advanceTimersByTimeAsync(250)
    await handle.finished
    expect(chunk).toHaveBeenCalledOnce()
    expect(chunk.mock.calls[0][2]).toBe('3')
  })
  it.each(['garbage', '-1', '1.2', '9007199254740992', '1e3', '0'])('rejects invalid cursor %s without discarding content', async id => {
    vi.mocked(fetch).mockResolvedValue(response(frame(1) + frame(id)))
    const chunk = vi.fn(), error = vi.fn()
    const handle = streamFetch('/events', { method: 'GET', runId: 'r' }, chunk, vi.fn(), error)
    await handle.finished
    expect(chunk).toHaveBeenCalledOnce()
    expect(handle.cursor.lastSeq).toBe(1)
    expect(error).toHaveBeenCalledWith(expect.objectContaining({ code: 'protocol' }))
    expect(fetch).toHaveBeenCalledOnce()
  })
  it('rejects malformed JSON before advancing cursor', async () => {
    vi.mocked(fetch).mockResolvedValue(response(frame(1) + 'id:2\nevent:message.delta\ndata:{bad\n\n'))
    const chunk = vi.fn(), error = vi.fn()
    const handle = streamFetch('/events', { method: 'GET', runId: 'r' }, chunk, vi.fn(), error)
    await handle.finished
    expect(chunk).toHaveBeenCalledOnce()
    expect(handle.cursor.lastSeq).toBe(1)
    expect(error).toHaveBeenCalledWith(expect.objectContaining({ code: 'protocol' }))
  })
  it.each(['run.completed', 'run.parked', 'run.failed'])('cancels reader on terminal %s', async terminal => {
    const cancel = vi.fn()
    vi.mocked(fetch).mockResolvedValue(new Response(new ReadableStream({ start(c) { c.enqueue(new TextEncoder().encode(frame(1, terminal) + frame(2))) }, cancel })))
    const chunk = vi.fn(), done = vi.fn()
    await streamFetch('/events', { method: 'GET', runId: 'r' }, chunk, done).finished
    expect(chunk).toHaveBeenCalledOnce()
    expect(done).toHaveBeenCalledOnce()
    expect(cancel).toHaveBeenCalledOnce()
  })
  it('continues retryable failure/reconciling/operation completion; DONE is transport-only', async () => {
    vi.mocked(fetch).mockResolvedValue(response(frame(1, 'run.failed', { data: { retryable: true } }) + frame(2, 'run.reconciling') + frame(3, 'operation.completed') + 'data: [DONE]\n\n'))
    const chunk = vi.fn(), done = vi.fn()
    await streamFetch('/events', { method: 'GET', runId: 'r' }, chunk, done).finished
    expect(chunk.mock.calls.map(call => call[0])).toEqual(['run.failed', 'run.reconciling', 'operation.completed'])
    expect(done).toHaveBeenCalledOnce()
  })
  it('accepts DONE-only boundary without inventing events', async () => {
    vi.mocked(fetch).mockResolvedValue(response('data: [DONE]\n\n'))
    const chunk = vi.fn(), done = vi.fn()
    await streamFetch('/events', { method: 'GET', runId: 'r', initialAfterSeq: 10 }, chunk, done).finished
    expect(chunk).not.toHaveBeenCalled()
    expect(done).toHaveBeenCalledOnce()
  })
  it('aborts pending retry and cleans timer', async () => {
    vi.mocked(fetch).mockRejectedValue(new TypeError('offline'))
    const controller = new AbortController(), error = vi.fn()
    const handle = streamFetch('/events', { method: 'GET', runId: 'r', signal: controller.signal }, vi.fn(), vi.fn(), error)
    await flush(); controller.abort(); await handle.finished
    await vi.advanceTimersByTimeAsync(10000)
    expect(fetch).toHaveBeenCalledOnce()
    expect(vi.getTimerCount()).toBe(0)
    expect(error).toHaveBeenCalledWith(expect.objectContaining({ code: 'aborted' }))
  })
  it('aborts blocked reader, releasing lock and signal listener', async () => {
    const cancel = vi.fn(), controller = new AbortController(), body = new ReadableStream({ cancel })
    const remove = vi.spyOn(controller.signal, 'removeEventListener')
    vi.mocked(fetch).mockResolvedValue(new Response(body))
    const handle = streamFetch('/events', { method: 'GET', runId: 'r', signal: controller.signal }, vi.fn(), vi.fn(), vi.fn())
    await flush(); controller.abort(); await handle.finished
    expect(cancel).toHaveBeenCalledOnce()
    expect(body.locked).toBe(false)
    expect(remove).toHaveBeenCalledWith('abort', expect.any(Function))
  })
  it('retains positional POST text callbacks, never replaying POST', async () => {
    const chunk = vi.fn(), done = vi.fn(), error = vi.fn()
    vi.mocked(fetch).mockResolvedValueOnce(response('id:9007199254740999\nevent:token\ndata:plain text\n\n'))
    await streamFetch('/command', { query: 'q' }, chunk, done, error).finished
    expect(chunk).toHaveBeenCalledWith('token', 'plain text', '9007199254740999')
    expect(done).toHaveBeenCalledOnce()
    expect(fetch).toHaveBeenCalledWith('/command', expect.objectContaining({ method: 'POST', body: '{"query":"q"}' }))
    vi.mocked(fetch).mockRejectedValue(new TypeError('offline'))
    await streamFetch('/command', { method: 'POST', body: { query: 'q' }, maxRetries: 5 }, chunk, done, error).finished
    expect(fetch).toHaveBeenCalledTimes(2)
    expect(error).toHaveBeenCalledOnce()
    expect(vi.getTimerCount()).toBe(0)
  })
})
describe('SSECursor', () => {
  it('keeps monotonic high-water marks independently per Run', () => {
    const cursor = new SSECursor()
    cursor.reset('a', 2)
    expect(cursor.accept('a', 3)).toBe(true)
    expect(cursor.accept('a', 3)).toBe(false)
    expect(cursor.accept('a', 1)).toBe(false)
    expect(cursor.seen('a', 2)).toBe(true)
    expect(cursor.lastSeq).toBe(3)
    expect(cursor.accept('b', 1)).toBe(true)
    expect(cursor.accept('a', 2)).toBe(false)
    expect(cursor.lastSeq).toBe(3)
    cursor.reset('a')
    expect(cursor.lastSeq).toBe(0)
  })
  it.each([-1, NaN, Infinity, 1.2, Number.MAX_SAFE_INTEGER + 1])('rejects invalid numeric seq %s', seq => {
    const cursor = new SSECursor()
    expect(cursor.accept('a', seq)).toBe(false)
    expect(() => cursor.reset('a', seq)).toThrow()
    expect(cursor.lastSeq).toBe(0)
  })
})

it('resumes after reader failure, dropping only uncommitted partial frame bytes', async () => {
  let controller!: ReadableStreamDefaultController<Uint8Array>
  const body = new ReadableStream<Uint8Array>({ start(c) { controller = c; c.enqueue(new TextEncoder().encode(frame(1) + 'id:2\nevent:message.delta\ndata:{')) } })
  vi.mocked(fetch).mockResolvedValueOnce(new Response(body)).mockResolvedValueOnce(response(frame(1) + frame(2, 'run.completed')))
  const chunk = vi.fn()
  const handle = streamFetch('/events', { method: 'GET', runId: 'r' }, chunk, vi.fn())
  await flush()
  controller.error(new Error('socket lost'))
  await vi.advanceTimersByTimeAsync(250)
  await handle.finished
  expect(chunk.mock.calls.map(call => call[2])).toEqual(['1', '2'])
  expect(fetch).toHaveBeenLastCalledWith('/events?after_seq=1', expect.anything())
  expect(body.locked).toBe(false)
})

it('rejects invalid initial cursor before any request', async () => {
  const error = vi.fn()
  await streamFetch('/events', { method: 'GET', initialAfterSeq: Number.MAX_SAFE_INTEGER + 1 }, vi.fn(), vi.fn(), error).finished
  expect(fetch).not.toHaveBeenCalled()
  expect(error).toHaveBeenCalledWith(expect.objectContaining({ code: 'protocol' }))
})

it('does not reconnect after consumer completion throws', async () => {
  vi.mocked(fetch).mockResolvedValue(response('data:[DONE]\n\n'))
  const error = vi.fn()
  await streamFetch('/events', { method: 'GET' }, vi.fn(), () => { throw new Error('consumer bug') }, error).finished
  expect(error).toHaveBeenCalledWith(expect.objectContaining({ code: 'consumer' }))
  expect(fetch).toHaveBeenCalledOnce()
})

it('keeps legacy positional abort silent and never resubmits', async () => {
  const controller = new AbortController(), error = vi.fn(), done = vi.fn(), cancel = vi.fn()
  vi.mocked(fetch).mockResolvedValue(new Response(new ReadableStream({ cancel })))
  const handle = streamFetch('/command', { query: 'q' }, vi.fn(), done, error, controller.signal)
  await flush(); controller.abort(); await handle.finished
  expect(error).not.toHaveBeenCalled()
  expect(done).not.toHaveBeenCalled()
  expect(cancel).toHaveBeenCalledOnce()
  expect(fetch).toHaveBeenCalledOnce()
})

it('stops buffered frame delivery when the consumer aborts', async () => {
  const controller = new AbortController(), chunk = vi.fn(() => controller.abort()), error = vi.fn()
  vi.mocked(fetch).mockResolvedValue(response(frame(1) + frame(2)))
  const handle = streamFetch('/events', { method: 'GET', runId: 'r', signal: controller.signal }, chunk, vi.fn(), error)
  await handle.finished
  expect(chunk).toHaveBeenCalledOnce()
  expect(handle.cursor.lastSeq).toBe(1)
  expect(error).toHaveBeenCalledWith(expect.objectContaining({ code: 'aborted' }))
})
