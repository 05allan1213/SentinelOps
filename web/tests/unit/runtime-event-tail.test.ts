import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, renderHook, waitFor } from '@testing-library/react'
import { createElement, type ReactNode } from 'react'
import { runtimeQueryKeys } from '@/hooks/useRuntimeQueries'
import { useRuntimeEventTail } from '@/hooks/useRuntimeEventTail'
import type { RuntimeEventDTO, TimelineRes } from '@/types/runtime'

const page = { page: 1, page_size: 50, total: 0, has_next: false }

const frame = (seq: number, type: string, dto: Partial<RuntimeEventDTO> = {}) =>
  `id: ${seq}\nevent: ${type}\ndata: ${JSON.stringify({
    seq,
    run_id: 'run-1',
    event_type: type,
    attempt: 1,
    generation: 1,
    trace_id: '',
    summary: `event-${seq}`,
    created_at: `2026-09-10T00:00:0${seq}.000Z`,
    availability: 'available',
    data_quality: 'complete',
    ...dto,
  })}\n\n`

interface ControlledStream {
  emit: (chunk: string) => void
  close: () => void
  cancel: ReturnType<typeof vi.fn>
  urls: string[]
}

function installStream(): ControlledStream {
  const encoder = new TextEncoder()
  const cancel = vi.fn()
  const control: ControlledStream = { emit: () => {}, close: () => {}, cancel, urls: [] }
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    control.urls.push(String(input))
    let controller: ReadableStreamDefaultController<Uint8Array>
    const stream = new ReadableStream<Uint8Array>({
      start(next) {
        controller = next
        control.emit = chunk => controller.enqueue(encoder.encode(chunk))
        control.close = () => controller.close()
      },
      cancel,
    })
    return new Response(stream, { headers: { 'Content-Type': 'text/event-stream' } })
  }))
  return control
}

function wrapper(client: QueryClient) {
  return ({ children }: { children: ReactNode }) => createElement(QueryClientProvider, { client }, children)
}

function seedTimeline(client: QueryClient) {
  const key = runtimeQueryKeys.timeline('run-1', { page: 1, page_size: 50 })
  client.setQueryData<TimelineRes>(key, { items: [], page, availability: 'available', data_quality: 'complete' })
  return key
}

const itemsOf = (client: QueryClient, key: readonly unknown[]) => client.getQueryData<TimelineRes>(key)?.items ?? []

beforeEach(() => {
  localStorage.setItem('token', 'runtime-tail-token')
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

it('merges accepted events into the timeline cache in seq order', async () => {
  const control = installStream()
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const key = seedTimeline(client)
  const invalidate = vi.spyOn(client, 'invalidateQueries')

  const { result } = renderHook(() => useRuntimeEventTail({ runId: 'run-1', enabled: true }), { wrapper: wrapper(client) })
  await waitFor(() => expect(control.urls.length).toBe(1))
  expect(control.urls[0]).toBe('/api/runtime/v1/runs/run-1/events?after_seq=0')

  act(() => control.emit(frame(1, 'run.started') + frame(2, 'agent.step') + frame(3, 'budget.reserved')))

  await waitFor(() => expect(itemsOf(client, key).map(item => item.seq)).toEqual([1, 2, 3]))
  expect(result.current.afterSeq).toBe(3)
  expect(result.current.lastEventAt).toBe('2026-09-10T00:00:03.000Z')
  expect(invalidate).toHaveBeenCalledWith(expect.objectContaining({ queryKey: runtimeQueryKeys.run('run-1') }))
})

it('deduplicates repeated frames and ignores out-of-order sequences', async () => {
  const control = installStream()
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const key = seedTimeline(client)

  const { result } = renderHook(() => useRuntimeEventTail({ runId: 'run-1', enabled: true }), { wrapper: wrapper(client) })
  await waitFor(() => expect(control.urls.length).toBe(1))

  act(() => control.emit(frame(1, 'run.started') + frame(2, 'agent.step') + frame(2, 'agent.step') + frame(1, 'run.started') + frame(3, 'agent.step')))

  await waitFor(() => expect(itemsOf(client, key)).toHaveLength(3))
  expect(itemsOf(client, key).map(item => item.seq)).toEqual([1, 2, 3])
  expect(result.current.afterSeq).toBe(3)
})

it('does not duplicate an event already present in the fetched timeline', async () => {
  const control = installStream()
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const key = runtimeQueryKeys.timeline('run-1', { page: 1, page_size: 50 })
  client.setQueryData<TimelineRes>(key, {
    items: [{ seq: 1, run_id: 'run-1', event_type: 'run.started', attempt: 1, generation: 1, trace_id: '', summary: 'event-1', created_at: '2026-09-10T00:00:01.000Z', availability: 'available', data_quality: 'complete' }],
    page,
    availability: 'available',
    data_quality: 'complete',
  })

  renderHook(() => useRuntimeEventTail({ runId: 'run-1', enabled: true }), { wrapper: wrapper(client) })
  await waitFor(() => expect(control.urls.length).toBe(1))
  act(() => control.emit(frame(2, 'agent.step')))

  await waitFor(() => expect(itemsOf(client, key).map(item => item.seq)).toEqual([1, 2]))
})

it('refetches the run once when the tab becomes visible again', async () => {
  const control = installStream()
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const invalidate = vi.spyOn(client, 'invalidateQueries')
  const visibility = vi.spyOn(document, 'visibilityState', 'get')

  renderHook(() => useRuntimeEventTail({ runId: 'run-1', enabled: true }), { wrapper: wrapper(client) })
  await waitFor(() => expect(control.urls.length).toBe(1))

  visibility.mockReturnValue('hidden')
  act(() => { document.dispatchEvent(new Event('visibilitychange')) })
  expect(invalidate).not.toHaveBeenCalled()

  visibility.mockReturnValue('visible')
  act(() => { document.dispatchEvent(new Event('visibilitychange')) })
  expect(invalidate).toHaveBeenCalledWith(expect.objectContaining({ queryKey: runtimeQueryKeys.run('run-1') }))
})

it('closes the reader on terminal server facts and never infers success from transport close', async () => {
  const control = installStream()
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const key = seedTimeline(client)

  const { result } = renderHook(() => useRuntimeEventTail({ runId: 'run-1', enabled: true }), { wrapper: wrapper(client) })
  await waitFor(() => expect(control.urls.length).toBe(1))

  act(() => control.emit(frame(1, 'run.completed', { operation_id: 'op-1' })))

  await waitFor(() => expect(result.current.connected).toBe(false))
  expect(result.current.afterSeq).toBe(1)
  expect(itemsOf(client, key)[0].event_type).toBe('run.completed')

  // A transport close without a terminal event must never flip any success fact.
  const control2 = installStream()
  const client2 = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const key2 = seedTimeline(client2)
  const second = renderHook(() => useRuntimeEventTail({ runId: 'run-1', enabled: true }), { wrapper: wrapper(client2) })
  await waitFor(() => expect(control2.urls.length).toBe(1))
  act(() => { control2.emit(frame(1, 'agent.step')); control2.close() })
  await waitFor(() => expect(itemsOf(client2, key2).map(item => item.seq)).toEqual([1]))
  expect(itemsOf(client2, key2).some(item => item.event_type === 'run.completed')).toBe(false)
  expect(second.result.current.afterSeq).toBe(1)
  second.unmount()
})

it('aborts the reader on unmount', async () => {
  const control = installStream()
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })

  const { unmount } = renderHook(() => useRuntimeEventTail({ runId: 'run-1', enabled: true }), { wrapper: wrapper(client) })
  await waitFor(() => expect(control.urls.length).toBe(1))

  unmount()

  await waitFor(() => expect(control.cancel).toHaveBeenCalled())
})

it('surfaces a transport error and retries from the committed cursor', async () => {
  const control = installStream()
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const onError = vi.fn()
  seedTimeline(client)

  const { result } = renderHook(() => useRuntimeEventTail({ runId: 'run-1', enabled: true, onError }), { wrapper: wrapper(client) })
  await waitFor(() => expect(control.urls.length).toBe(1))
  act(() => control.emit(frame(4, 'agent.step')))
  await waitFor(() => expect(result.current.afterSeq).toBe(4))

  vi.mocked(fetch).mockImplementationOnce(async (input: RequestInfo | URL) => {
    control.urls.push(String(input))
    return new Response('', { status: 403 })
  })
  act(() => result.current.retry())

  await waitFor(() => expect(onError).toHaveBeenCalled())
  expect(result.current.error).not.toBeNull()
  await waitFor(() => expect(control.urls.some(url => url.includes('after_seq=4'))).toBe(true))
})

it('invalidates the correlated operation without creating a run', async () => {
  const control = installStream()
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  seedTimeline(client)
  const invalidate = vi.spyOn(client, 'invalidateQueries')

  renderHook(() => useRuntimeEventTail({ runId: 'run-1', enabled: true }), { wrapper: wrapper(client) })
  await waitFor(() => expect(control.urls.length).toBe(1))
  act(() => control.emit(frame(1, 'operation.accepted', { operation_id: 'op-9' })))

  await waitFor(() => expect(invalidate).toHaveBeenCalledWith(expect.objectContaining({ queryKey: runtimeQueryKeys.operation('op-9') })))
  const requested = vi.mocked(fetch).mock.calls.map(call => `${call[0]}`)
  expect(requested.every(url => url.includes('/events'))).toBe(true)
})

it('does not open a reader when disabled and stops after a terminal fact', async () => {
  const control = installStream()
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })

  const { result, rerender } = renderHook(
    ({ enabled }: { enabled: boolean }) => useRuntimeEventTail({ runId: 'run-1', enabled }),
    { wrapper: wrapper(client), initialProps: { enabled: false } },
  )
  expect(result.current.connected).toBe(false)
  expect(control.urls).toHaveLength(0)

  rerender({ enabled: true })
  await waitFor(() => expect(control.urls.length).toBe(1))

  act(() => control.emit(frame(1, 'run.parked')))
  await waitFor(() => expect(result.current.connected).toBe(false))
  expect(result.current.afterSeq).toBe(1)
})
