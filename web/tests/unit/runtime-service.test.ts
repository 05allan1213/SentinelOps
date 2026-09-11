import api, { ApiRequestError } from '@/services/api'
import { runtimeService, runtimeLocalTimestamp } from '@/services/runtime'

vi.mock('@/services/api', async () => {
  const actual = await vi.importActual<typeof import('@/services/api')>('@/services/api')
  return { ...actual, default: { get: vi.fn(), post: vi.fn() } }
})

const ok = (data: unknown) => ({ status: 200, data: { message: 'OK', data } })
const page = { page: 1, page_size: 20, total: 0, has_next: false }

beforeEach(() => {
  vi.mocked(api.get).mockReset()
  vi.mocked(api.post).mockReset()
})

describe('runtimeService URLs and parameters', () => {
  it('lists runs on the frozen route and drops only empty filters', async () => {
    vi.mocked(api.get).mockResolvedValue(ok({ items: [], page, availability: 'available', data_quality: 'complete' }))

    await runtimeService.listRuns({ status: 'parked', session_id: '', agent: undefined, page: 1, page_size: 20, include_legacy: false })

    expect(api.get).toHaveBeenCalledTimes(1)
    expect(vi.mocked(api.get).mock.calls[0][0]).toBe('/runtime/v1/runs')
    expect(vi.mocked(api.get).mock.calls[0][1]?.params).toEqual({ status: 'parked', page: 1, page_size: 20, include_legacy: false })
  })

  it('maps every run sub-resource to its frozen route', async () => {
    vi.mocked(api.get).mockResolvedValue(ok({ items: [], page, availability: 'available', data_quality: 'complete' }))

    await runtimeService.getRun('run-1')
    await runtimeService.getTimeline('run-1', { page: 1, page_size: 50, sort: 'seq', direction: 'asc' })
    await runtimeService.getAttempts('run-1', { page: 2, page_size: 10 })
    await runtimeService.getCheckpoints('run-1', { page: 1, page_size: 10 })
    await runtimeService.getApprovals('run-1', { page: 1, page_size: 10 })
    await runtimeService.getEffects('run-1', { effect_role: 'primary', attempt: 2, page: 1, page_size: 10 })
    await runtimeService.getEvidence('run-1', { page: 1, page_size: 10 })
    await runtimeService.getTraces('run-1', { page: 1, page_size: 10 })
    await runtimeService.getCapabilities({ page: 1, page_size: 100 })
    await runtimeService.getSafety()
    await runtimeService.getWorkerHealth()
    await runtimeService.getEval({ suite: 'agent', page: 1, page_size: 20 })
    await runtimeService.getRelease()
    await runtimeService.getRetention()

    expect(vi.mocked(api.get).mock.calls.map(call => call[0])).toEqual([
      '/runtime/v1/runs/run-1',
      '/runtime/v1/runs/run-1/timeline',
      '/runtime/v1/runs/run-1/attempts',
      '/runtime/v1/runs/run-1/checkpoints',
      '/runtime/v1/runs/run-1/approvals',
      '/runtime/v1/runs/run-1/effects',
      '/runtime/v1/runs/run-1/evidence',
      '/runtime/v1/runs/run-1/traces',
      '/runtime/v1/capabilities',
      '/runtime/v1/safety',
      '/runtime/v1/worker-health',
      '/runtime/v1/eval',
      '/runtime/v1/release',
      '/runtime/v1/retention',
    ])
    expect(vi.mocked(api.get).mock.calls[1][1]?.params).toEqual({ page: 1, page_size: 50, sort: 'seq', direction: 'asc' })
    expect(vi.mocked(api.get).mock.calls[5][1]?.params).toEqual({ effect_role: 'primary', attempt: 2, page: 1, page_size: 10 })
  })

  it('expands evidence and context only through their explicit include parameters', async () => {
    vi.mocked(api.get).mockResolvedValue(ok({ item: { evidence_id: 'ev-1' }, availability: 'available', data_quality: 'complete' }))

    await runtimeService.expandEvidence('run-1', 'ev-1')
    await runtimeService.expandEvidence('run-1', 'ev-1', { include: 'quote' })
    await runtimeService.getContext('run-1')
    await runtimeService.getContext('run-1', { include: 'history' })
    await runtimeService.getOperation('op-1')

    expect(vi.mocked(api.get).mock.calls[0][0]).toBe('/runtime/v1/runs/run-1/evidence/ev-1')
    expect(vi.mocked(api.get).mock.calls[0][1]?.params).toEqual({})
    expect(vi.mocked(api.get).mock.calls[1][1]?.params).toEqual({ include: 'quote' })
    expect(vi.mocked(api.get).mock.calls[2][0]).toBe('/runtime/v1/runs/run-1/context')
    expect(vi.mocked(api.get).mock.calls[2][1]?.params).toEqual({})
    expect(vi.mocked(api.get).mock.calls[3][1]?.params).toEqual({ include: 'history' })
    expect(vi.mocked(api.get).mock.calls[4][0]).toBe('/runtime/v1/operations/op-1')
  })

  it('posts a recovery command once and performs no optimistic cache or extra request', async () => {
    vi.mocked(api.post).mockResolvedValue(ok({ operation: { operation_id: 'op-1', run_id: 'run-1', action: 'resume', status: 'accepted' } }))

    const result = await runtimeService.recoverRun('run-1', {
      action: 'resume',
      idempotency_key: 'key-1',
      reason: 'retry after worker restart',
      expected_generation: 7,
    })

    expect(api.post).toHaveBeenCalledTimes(1)
    expect(vi.mocked(api.post).mock.calls[0][0]).toBe('/runtime/v1/runs/run-1/recovery')
    expect(vi.mocked(api.post).mock.calls[0][1]).toEqual({
      action: 'resume',
      idempotency_key: 'key-1',
      reason: 'retry after worker restart',
      expected_generation: 7,
    })
    expect(api.get).not.toHaveBeenCalled()
    expect(result.operation.operation_id).toBe('op-1')
  })
})

describe('runtimeService response fidelity', () => {
  it('preserves partial availability metadata, not_run evidence and nullable budget counters', async () => {
    vi.mocked(api.get).mockResolvedValue(ok({
      item: {
        summary: { run_id: 'run-1', status: 'parked', current_phase: 'unknown', budget: { model_calls: null, cost_cny: null, exhausted: false } },
        allowed_recovery_actions: [],
      },
      availability: 'partial',
      data_quality: 'reconstructed',
      reason_code: 'attempt_projection_rebuilt',
      not_run: true,
    }))

    const result = await runtimeService.getRun('run-1')

    expect(result.availability).toBe('partial')
    expect(result.data_quality).toBe('reconstructed')
    expect(result.reason_code).toBe('attempt_projection_rebuilt')
    expect(result.not_run).toBe(true)
    expect(result.item.summary.budget.model_calls).toBeNull()
    expect(result.item.summary.budget.cost_cny).toBeNull()
    expect(result.item.summary.status).toBe('parked')
  })

  it('does not fabricate arrays or zero values for an absent source', async () => {
    vi.mocked(api.get).mockResolvedValue(ok({ availability: 'unavailable', data_quality: 'unknown', reason_code: 'not_observed', not_run: true }))

    const result = await runtimeService.getCapabilities({ page: 1, page_size: 20 })

    expect(result.items).toBeUndefined()
    expect(result.reason_code).toBe('not_observed')
    expect(result.not_run).toBe(true)
  })

  it('propagates ApiRequestError without swallowing the availability payload', async () => {
    const error = new ApiRequestError('无权限查看', 403, { message: '无权限查看', data: { availability: 'unavailable', data_quality: 'unknown' } })
    vi.mocked(api.get).mockRejectedValue(error)

    await expect(runtimeService.getRun('run-1')).rejects.toBe(error)
    await expect(runtimeService.getRun('run-1')).rejects.toMatchObject({ status: 403 })
  })

  it('forwards an AbortSignal for cancellable queries', async () => {
    vi.mocked(api.get).mockResolvedValue(ok({ items: [], page, availability: 'available', data_quality: 'complete' }))
    const controller = new AbortController()

    await runtimeService.listRuns({ page: 1, page_size: 20 }, { signal: controller.signal })

    expect(vi.mocked(api.get).mock.calls[0][1]?.signal).toBe(controller.signal)
  })
})

it('serializes local date filters as RFC3339 and round-trips zoned URL values', async () => {
  vi.mocked(api.get).mockResolvedValue(ok({ items: [], page }))
  const local = '2026-09-11T10:30'
  await runtimeService.listRuns({ from: local, to: '2026-09-12T00:00:00+08:00' })
  expect(vi.mocked(api.get).mock.calls[0][1]?.params).toEqual({
    from: new Date(local).toISOString(), to: '2026-09-12T00:00:00+08:00',
  })
  expect(runtimeLocalTimestamp(new Date(local).toISOString())).toBe(local + ':00')
})
