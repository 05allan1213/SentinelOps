import api from './api'
import { useAuthStore } from '@/stores/authStore'

export interface TrendPoint {
  timestamp: string
  success_rate: number
  avg_latency_ms: number
}

export interface DashboardMetrics {
  success_rate: number
  avg_latency_ms: number
  p95_latency_ms: number
  total_runs: number
  avg_retrieved_docs: number  // P1 阶段指标
  avg_top_score: number       // P1 阶段指标
  success_rate_status: 'good' | 'warning' | 'bad'
  latency_status: 'good' | 'warning' | 'bad'
  trends: TrendPoint[]
}

const metricKeys = ['success_rate', 'avg_latency_ms', 'p95_latency_ms', 'total_runs', 'avg_retrieved_docs', 'avg_top_score'] as const
type MetricKey = typeof metricKeys[number]
export type SafeDashboardMetrics = Record<MetricKey, number | null> & {
  success_rate_status?: DashboardMetrics['success_rate_status']
  latency_status?: DashboardMetrics['latency_status']
}
export interface DashboardMetricsState {
  metrics: SafeDashboardMetrics | null
  trends: TrendPoint[] | null
  availability: 'available' | 'partial' | 'unavailable'
  data_quality: 'complete' | 'partial' | 'unknown'
  reason_code?: string
  not_run?: string
}
const numeric = (value: unknown): number | null => {
  if (typeof value !== 'number' && !(typeof value === 'string' && value.trim() !== '')) return null
  const number = Number(value)
  return Number.isFinite(number) && number >= 0 ? number : null
}
export function normalizeDashboardPayload(value: unknown): DashboardMetricsState {
  const absent: DashboardMetricsState = { metrics: null, trends: null, availability: 'unavailable', data_quality: 'unknown', reason_code: 'invalid_dashboard_payload', not_run: 'No valid dashboard metrics received' }
  if (!value || typeof value !== 'object' || Array.isArray(value)) return absent
  const input = value as Record<string, unknown>
  const metrics = { success_rate: numeric(input.success_rate), avg_latency_ms: numeric(input.avg_latency_ms), p95_latency_ms: numeric(input.p95_latency_ms), total_runs: numeric(input.total_runs), avg_retrieved_docs: numeric(input.avg_retrieved_docs), avg_top_score: numeric(input.avg_top_score) } as SafeDashboardMetrics
  if (metricKeys.every(key => metrics[key] === null)) return absent
  for (const key of ['success_rate_status', 'latency_status'] as const) {
    if (input[key] === 'good' || input[key] === 'warning' || input[key] === 'bad') metrics[key] = input[key]
  }
  let trends: TrendPoint[] | null = null
  if (Array.isArray(input.trends)) {
    const rows = input.trends.map(row => {
      if (!row || typeof row !== 'object' || typeof row.timestamp !== 'string') return null
      const rate = numeric(row.success_rate), latency = numeric(row.avg_latency_ms)
      return rate === null || rate > 1 || latency === null ? null : { timestamp: row.timestamp, success_rate: rate, avg_latency_ms: latency }
    })
    if (rows.every(row => row !== null)) trends = rows as TrendPoint[]
  }
  if (metrics.success_rate !== null && metrics.success_rate > 1) metrics.success_rate = null
  if (metrics.avg_top_score !== null && metrics.avg_top_score > 1) metrics.avg_top_score = null
  const complete = metricKeys.every(key => metrics[key] !== null) && trends !== null
  return { metrics, trends, availability: complete ? 'available' : 'partial', data_quality: complete ? 'complete' : 'partial', reason_code: complete ? undefined : 'incomplete_dashboard_payload' }
}

export interface TraceItem {
  trace_id: string
  trace_name: string
  session_id: string
  status: string
  duration_ms: number
  start_time: string
  feedback_vote: number  // 0=无 1=赞 -1=踩
}

// P0: Trace 节点树
export interface TraceNodeItem {
  node_id: string
  parent_node_id?: string
  depth: number
  node_type: string
  node_name: string
  status: string
  duration_ms: number
  error_message?: string
  model_name?: string
  input_tokens?: number
  output_tokens?: number
  cost_usd?: number
  cache_hit?: boolean
  final_top_k?: number
  doc_count?: number
  avg_vector_score?: number
  max_vector_score?: number
  rerank_used?: boolean
  avg_rerank_score?: number
  retrieved_docs?: string
  children?: TraceNodeItem[]
}

// P0: Trace 详情
export interface TraceDetail {
  trace_id: string
  trace_name: string
  session_id: string
  query_text: string
  status: string
  duration_ms: number
  total_input_tokens: number
  total_output_tokens: number
  estimated_cost_usd: number
  start_time: string
  nodes: TraceNodeItem[]
  feedback_vote: number
}

export interface RecentFeedback {
  vote: number
  reason?: string
  created_at: string
}

export interface FeedbackStats {
  like_rate: number
  dislike_rate: number
  no_vote_rate: number
  total: number
  recent: RecentFeedback[]
}

interface ApiWrap<T> { data: T }

export const ragevalService = {
  async getDashboard(window = '24h'): Promise<DashboardMetricsState> {
    const res = await api.get<ApiWrap<unknown>>('/rageval/v1/dashboard', { params: { window } })
    return normalizeDashboardPayload(res.data.data)
  },

  async listTraces(params?: {
    page?: number
    pageSize?: number
    status?: string
  }): Promise<{ list: TraceItem[]; total: number }> {
    const res = await api.get<ApiWrap<{ list: TraceItem[]; total: number }>>('/rageval/v1/traces', {
      params: {
        page: params?.page ?? 1,
        page_size: params?.pageSize ?? 5,
        status: params?.status ?? '',
      },
    })
    return { list: res.data.data?.list ?? [], total: res.data.data?.total ?? 0 }
  },

  async getTraceDetail(traceId: string): Promise<TraceDetail> {
    const res = await api.get<ApiWrap<TraceDetail>>('/rageval/v1/traces/detail', { params: { trace_id: traceId } })
    return res.data.data!
  },

  async deleteTrace(traceId: string): Promise<void> {
    await api.delete('/rageval/v1/traces', { data: { trace_id: traceId } })
  },

  async submitFeedback(sessionId: string, messageIndex: number, vote: 1 | -1 | 0, reasons?: string[]): Promise<void> {
    const userID = useAuthStore.getState().userID ?? ''
    await api.post('/rageval/v1/feedback', {
      session_id: sessionId,
      message_index: messageIndex,
      vote,
      reasons: reasons ?? [],
      user_id: userID,
    })
  },

  async getFeedbackStats(): Promise<FeedbackStats> {
    const res = await api.get<ApiWrap<FeedbackStats>>('/rageval/v1/feedback_stats')
    return res.data.data!
  },
}
