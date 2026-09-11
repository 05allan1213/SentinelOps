import { describe, expect, it } from 'vitest'
import { normalizeDashboardPayload } from '@/services/rageval'
const valid = { success_rate: 0.5, avg_latency_ms: 30, p95_latency_ms: 40, total_runs: 2, avg_retrieved_docs: 3, avg_top_score: 0.8, success_rate_status: 'good', latency_status: 'good', trends: [] }
describe('dashboard boundary', () => {
  it.each([null, {}, [], 'bad'])('unavailable instead of invented metrics: %j', value => {
    const state = normalizeDashboardPayload(value)
    expect(state.availability).toBe('unavailable')
    expect(state.metrics).toBeNull()
  })
  it('preserves valid zero and explicitly empty trends', () => {
    expect(normalizeDashboardPayload({ ...valid, total_runs: 0 })).toMatchObject({ availability: 'available', data_quality: 'complete', metrics: { total_runs: 0 }, trends: [] })
  })
  it('missing or malformed trends are partial, never successful empty chart', () => {
    for (const trends of [undefined, null, {}, [null], [{ timestamp: 'x', success_rate: 'no' }]]) {
      expect(normalizeDashboardPayload({ ...valid, trends })).toMatchObject({ data_quality: 'partial', trends: null })
    }
  })
  it('missing metrics remain null', () => {
    expect(normalizeDashboardPayload({ ...valid, avg_latency_ms: undefined })).toMatchObject({ data_quality: 'partial', metrics: { avg_latency_ms: null } })
  })
})
