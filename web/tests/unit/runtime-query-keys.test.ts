import EffectsPanel from '@/pages/runtime/components/EffectsPanel'
import AttemptsPanel from '@/pages/runtime/components/AttemptsPanel'
import CheckpointPanel from '@/pages/runtime/components/CheckpointPanel'
import EvidenceInspector from '@/pages/runtime/components/EvidenceInspector'
import TracePanel from '@/pages/runtime/components/TracePanel'
import OperationProgress from '@/pages/runtime/components/OperationProgress'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, renderHook, waitFor } from '@testing-library/react'
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

it.each([
  [EffectsPanel, '暂无 Effect 记录'], [AttemptsPanel, '暂无 Attempt 记录'],
  [CheckpointPanel, '暂无 Checkpoint 记录'], [EvidenceInspector, '暂无 Evidence 记录'],
  [TracePanel, '暂无 Trace 记录'],
])('shows query failure instead of an empty resource list: %s', async (Panel, emptyText) => {
  vi.mocked(api.get).mockRejectedValue(new Error('HTTP 500'))
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(createElement(Panel, { runId: 'run-1' }), { wrapper: wrapper(client) })
  expect(await screen.findByRole('alert')).toHaveTextContent('查询失败')
  expect(screen.queryByText(emptyText)).toBeNull()
  expect(screen.getByRole('button', { name: '重试' })).toBeEnabled()
})

it('invalidates related resources after each consecutive terminal operation', async () => {
  vi.mocked(api.get).mockImplementation(async url => envelope({ item: {
    operation_id: String(url).split('/').pop(), run_id: 'run-1', terminal: true, status: 'failed', action: 'resume',
  } }))
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const terminal = vi.fn()
  const invalidate = vi.spyOn(client, 'invalidateQueries')
  const view = render(createElement(OperationProgress, { operationId: 'op-1', onTerminal: terminal }), { wrapper: wrapper(client) })
  await waitFor(() => expect(terminal).toHaveBeenCalledTimes(1))
  invalidate.mockClear()
  view.rerender(createElement(OperationProgress, { operationId: 'op-2', onTerminal: terminal }))
  await waitFor(() => expect(terminal).toHaveBeenCalledTimes(2))
  expect(invalidate).toHaveBeenCalledWith({ queryKey: ['runtime', 'attempts', 'run-1'] })
  expect(invalidate).toHaveBeenCalledWith({ queryKey: ['runtime', 'effects', 'run-1'] })
})

it('shares the same Runs cache entry for equivalent local and RFC3339 filters', () => {
  const local = '2026-09-11T10:30'
  expect(runtimeQueryKeys.runs({ from: local })).toEqual(runtimeQueryKeys.runs({ from: new Date(local).toISOString() }))
})
