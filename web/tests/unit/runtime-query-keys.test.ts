import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import { createElement, type ReactNode } from 'react'
import api from '@/services/api'
import { runtimeQueryKeys, useRuntimeRuns, useRuntimeRun, useRuntimeCapabilities } from '@/hooks/useRuntimeQueries'

vi.mock('@/services/api', async () => {
  const actual = await vi.importActual<typeof import('@/services/api')>('@/services/api')
  return { ...actual, default: { get: vi.fn(), post: vi.fn() } }
})

const page = { page: 1, page_size: 20, total: 0, has_next: false }
const envelope = (data: unknown) => ({ status: 200, data: { message: 'OK', data } })

beforeEach(() => {
  vi.mocked(api.get).mockReset()
  vi.mocked(api.post).mockReset()
})

function wrapper(client: QueryClient) {
  return ({ children }: { children: ReactNode }) => createElement(QueryClientProvider, { client }, children)
}

describe('runtimeQueryKeys', () => {
  it('keeps the frozen key namespace and stable root', () => {
    const keys = [
      runtimeQueryKeys.runs({ page: 1, page_size: 20 }),
      runtimeQueryKeys.run('run-1'),
      runtimeQueryKeys.timeline('run-1', { page: 1, page_size: 50 }),
      runtimeQueryKeys.events('run-1', 12),
      runtimeQueryKeys.attempts('run-1', { page: 1, page_size: 10 }),
      runtimeQueryKeys.checkpoints('run-1', { page: 1, page_size: 10 }),
      runtimeQueryKeys.approvals('run-1', { page: 1, page_size: 10 }),
      runtimeQueryKeys.effects('run-1', { page: 1, page_size: 10 }),
      runtimeQueryKeys.evidence('run-1', { page: 1, page_size: 10 }),
      runtimeQueryKeys.context('run-1'),
      runtimeQueryKeys.traces('run-1', { page: 1, page_size: 10 }),
      runtimeQueryKeys.operation('op-1'),
      runtimeQueryKeys.capabilities(),
      runtimeQueryKeys.safety(),
      runtimeQueryKeys.workerHealth(),
      runtimeQueryKeys.eval({ page: 1, page_size: 20 }),
      runtimeQueryKeys.release(),
      runtimeQueryKeys.retention(),
    ]

    keys.forEach(key => {
      expect(Array.isArray(key)).toBe(true)
      expect(key[0]).toBe('runtime')
    })
    expect(Object.keys(runtimeQueryKeys).sort()).toEqual([
      'approvals',
      'attempts',
      'capabilities',
      'checkpoints',
      'context',
      'effects',
      'eval',
      'events',
      'evidence',
      'operation',
      'release',
      'retention',
      'run',
      'runs',
      'safety',
      'timeline',
      'traces',
      'workerHealth',
    ])
  })

  it('includes every filter value so different requests never collide', () => {
    expect(runtimeQueryKeys.runs({ page: 1, page_size: 20 })).not.toEqual(runtimeQueryKeys.runs({ page: 1, page_size: 50 }))
    expect(runtimeQueryKeys.runs({ page: 1, page_size: 20, status: 'parked' })).not.toEqual(runtimeQueryKeys.runs({ page: 1, page_size: 20, status: 'running' }))
    expect(runtimeQueryKeys.runs({ page: 1, page_size: 20, include_legacy: true })).not.toEqual(runtimeQueryKeys.runs({ page: 1, page_size: 20 }))
    expect(runtimeQueryKeys.timeline('run-1', { page: 1, page_size: 20, event_types: ['run.completed'] })).not.toEqual(runtimeQueryKeys.timeline('run-1', { page: 1, page_size: 20 }))
    expect(runtimeQueryKeys.events('run-1', 5)).not.toEqual(runtimeQueryKeys.events('run-1', 6))
    expect(runtimeQueryKeys.context('run-1')).not.toEqual(runtimeQueryKeys.context('run-1', { include: 'history' }))
    expect(runtimeQueryKeys.run('run-1')).not.toEqual(runtimeQueryKeys.run('run-2'))
    expect(runtimeQueryKeys.operation('op-1')).not.toEqual(runtimeQueryKeys.operation('op-2'))
  })

  it('is deterministic for equal parameters and collision-free across resources', () => {
    expect(runtimeQueryKeys.runs({ page: 2, page_size: 20, agent: 'planner' })).toEqual(runtimeQueryKeys.runs({ page: 2, page_size: 20, agent: 'planner' }))
    expect(runtimeQueryKeys.evidence('run-1', { page: 1, page_size: 10 })).toEqual(runtimeQueryKeys.evidence('run-1', { page: 1, page_size: 10 }))

    const serialized = new Set(
      [
        runtimeQueryKeys.runs({ page: 1, page_size: 20 }),
        runtimeQueryKeys.run('run-1'),
        runtimeQueryKeys.timeline('run-1', { page: 1, page_size: 20 }),
        runtimeQueryKeys.events('run-1', 0),
        runtimeQueryKeys.attempts('run-1', { page: 1, page_size: 20 }),
        runtimeQueryKeys.checkpoints('run-1', { page: 1, page_size: 20 }),
        runtimeQueryKeys.approvals('run-1', { page: 1, page_size: 20 }),
        runtimeQueryKeys.effects('run-1', { page: 1, page_size: 20 }),
        runtimeQueryKeys.evidence('run-1', { page: 1, page_size: 20 }),
        runtimeQueryKeys.context('run-1'),
        runtimeQueryKeys.traces('run-1', { page: 1, page_size: 20 }),
        runtimeQueryKeys.operation('op-1'),
        runtimeQueryKeys.capabilities(),
        runtimeQueryKeys.safety(),
        runtimeQueryKeys.workerHealth(),
        runtimeQueryKeys.eval({ page: 1, page_size: 20 }),
        runtimeQueryKeys.release(),
        runtimeQueryKeys.retention(),
      ].map(key => JSON.stringify(key)),
    )
    expect(serialized.size).toBe(18)
  })
})

describe('runtime query hooks', () => {
  it('uses the frozen keys and unwrapped service data', async () => {
    vi.mocked(api.get).mockImplementation(async (url: string) => {
      if (url === '/runtime/v1/runs') return envelope({ items: [{ run_id: 'run-1' }], page, availability: 'available', data_quality: 'complete' })
      if (url === '/runtime/v1/runs/run-1') return envelope({ item: { summary: { run_id: 'run-1' } }, availability: 'available', data_quality: 'complete' })
      return envelope({ items: [], page, availability: 'unavailable', data_quality: 'unknown', reason_code: 'not_observed', not_run: true })
    })
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })

    const runs = renderHook(() => useRuntimeRuns({ page: 1, page_size: 20 }), { wrapper: wrapper(client) })
    await waitFor(() => expect(runs.result.current.data?.items?.[0]?.run_id).toBe('run-1'))
    expect(client.getQueryCache().getAll().some(query => JSON.stringify(query.queryKey) === JSON.stringify(runtimeQueryKeys.runs({ page: 1, page_size: 20 })))).toBe(true)

    const run = renderHook(() => useRuntimeRun('run-1'), { wrapper: wrapper(client) })
    await waitFor(() => expect(run.result.current.data?.item?.summary?.run_id).toBe('run-1'))
    expect(client.getQueryCache().getAll().some(query => JSON.stringify(query.queryKey) === JSON.stringify(runtimeQueryKeys.run('run-1')))).toBe(true)

    const capabilities = renderHook(() => useRuntimeCapabilities(), { wrapper: wrapper(client) })
    await waitFor(() => expect(capabilities.result.current.data?.not_run).toBe(true))
    expect(client.getQueryCache().getAll().some(query => JSON.stringify(query.queryKey) === JSON.stringify(runtimeQueryKeys.capabilities()))).toBe(true)
  })

  it('does not fetch a run detail when the id is empty', async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const { result } = renderHook(() => useRuntimeRun(''), { wrapper: wrapper(client) })

    expect(result.current.fetchStatus).toBe('idle')
    expect(api.get).not.toHaveBeenCalled()
  })
})
