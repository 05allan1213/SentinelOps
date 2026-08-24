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
  avg_retrieved_docs: number  // P1
  avg_top_score: number       // P1
  success_rate_status: 'good' | 'warning' | 'bad'
  latency_status: 'good' | 'warning' | 'bad'
  trends: TrendPoint[]
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
  async getDashboard(window = '24h'): Promise<DashboardMetrics> {
    const res = await api.get<ApiWrap<DashboardMetrics>>('/rageval/v1/dashboard', { params: { window } })
    return res.data.data ?? {
      success_rate: 0, avg_latency_ms: 0, p95_latency_ms: 0, total_runs: 0,
      avg_retrieved_docs: 0, avg_top_score: 0,
      success_rate_status: 'good', latency_status: 'good', trends: [],
    }
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
