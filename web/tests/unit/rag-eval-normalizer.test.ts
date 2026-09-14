import { describe, expect, it } from 'vitest'
import { normalizeDashboardPayload, formatDurationMs, formatPercentage, formatMetricScore, formatMetricCount } from '@/services/rageval'
const valid = { success_rate: 0.5, avg_latency_ms: 30, p95_latency_ms: 40, total_runs: 2, avg_retrieved_docs: 3, avg_top_score: 0.8, success_rate_status: 'good', latency_status: 'good', trends: [] }
describe('dashboard boundary', () => {
  it('formats metric kinds without collapsing zero into unavailable', () => {
    expect(formatDurationMs(0)).toBe('0ms')
    expect(formatDurationMs(30)).toBe('30ms')
    expect(formatDurationMs(2832)).toBe('2.8s')
    expect(formatDurationMs(null)).toBe('—')
    expect(formatDurationMs(Number.NaN)).toBe('—')
    expect(formatDurationMs(-1)).toBe('—')
    expect(formatPercentage(0)).toBe('0.0%')
    expect(formatPercentage(0.875)).toBe('87.5%')
    expect(formatPercentage(null)).toBe('—')
    expect(formatMetricScore(0)).toBe('0.00')
    expect(formatMetricScore(0.8)).toBe('0.80')
    expect(formatMetricScore(null)).toBe('—')
    expect(formatMetricCount(0)).toBe('0')
    expect(formatMetricCount(1200)).toBe('1,200')
    expect(formatMetricCount(null)).toBe('—')
  })
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
