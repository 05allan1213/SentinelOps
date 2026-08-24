import api from './api'

// TraceRun 链路运行记录
export interface TraceRun {
  traceId: string
  traceName: string
  entryPoint: string
  status: 'running' | 'success' | 'error'
  durationMs: number
  startTime: string
  sessionId: string
  queryText: string
  totalInputTokens: number
  cachedInputTokens: number
  totalOutputTokens: number
  reasoningTokens: number
  estimatedCostCny?: number
  errorCode?: string
  errorMessage?: string
  tags: string
}

// TraceNode 链路节点记录
export interface TraceNode {
  nodeId: string
  parentNodeId: string
  nodeType: 'LLM' | 'TOOL' | 'RETRIEVER' | 'LAMBDA' | 'AGENT' | 'CACHE' | 'EMBEDDING' | 'RERANK'
  nodeName: string
  depth: number
  status: 'running' | 'success' | 'error'
  durationMs: number
  startTime: string
  errorMessage?: string
  errorCode?: string
  errorType?: string
  // LLM 专属
  modelName?: string
  inputTokens?: number
  cachedInputTokens?: number
  outputTokens?: number
  reasoningTokens?: number
  costCny?: number
  completionText?: string
  // RETRIEVER 专属
  queryText?: string
  retrievedDocs?: string
  finalTopK?: number
  cacheHit?: boolean
  // 通用
  metadata?: string
}

// TraceStats 统计数据
export interface TraceStats {
  totalRuns: number
  successRuns: number
  errorRuns: number
  avgDurationMs: number
  p95DurationMs: number
  totalInputTokens: number
  totalOutputTokens: number
  errorRate: number
}

// 会话时间线摘要
export interface SessionTimelineSummary {
  traceId: string
  queryText: string
  status: string
  durationMs: number
  startTime: string
  errorCode?: string
}

export interface ListParams {
  page?: number
  pageSize?: number
  status?: string
  traceId?: string
  sessionId?: string
}

// 成本概览
export interface DailyCostPoint {
  date: string
  costCny: number
  inputTokens: number
  outputTokens: number
  requestCount: number
}

export interface ModelCostItem {
  modelName: string
  totalCostCny: number
  inputTokens: number
  outputTokens: number
  requestCount: number
  costPct: number
}

export interface IntentCostItem {
  traceName: string
  totalCostCny: number
  requestCount: number
  avgCostCny: number
  costPct: number
}

export interface CostOverview {
  totalCostCny: number
  totalInputTokens: number
  totalOutputTokens: number
  totalRequests: number
  avgCostPerReq: number
  prevTotalCostCny: number
  costChangePct: number
  dailyTrend: DailyCostPoint[]
  modelBreakdown: ModelCostItem[]
  intentBreakdown: IntentCostItem[]
}

export interface TokenTrendPoint {
  hour: string
  inputTokens: number
  outputTokens: number
  requestCount: number
}

export const traceService = {
  // 查询链路运行列表
  async list(params: ListParams = {}): Promise<{ total: number; list: TraceRun[] }> {
    const res = await api.get('/trace/v1/list', { params })
    return res.data?.data || { total: 0, list: [] }
  },

  // 查询链路详情（含节点树）
  async detail(traceId: string): Promise<{ traceId: string } & TraceRun & { nodes: TraceNode[] }> {
    const res = await api.get('/trace/v1/detail', { params: { traceId } })
    return res.data?.data
  },

  // 查询统计数据
  async stats(days = 7): Promise<TraceStats> {
    const res = await api.get('/trace/v1/stats', { params: { days } })
    return res.data?.data || {}
  },

  // 批量删除链路记录
  async batchDelete(traceIds: string[]): Promise<{ deleted: number }> {
    const res = await api.delete('/trace/v1/batch_delete', { data: { traceIds } })
    return res.data?.data || { deleted: 0 }
  },

  // 导出链路为 JSON 文件
  async export(traceId: string): Promise<void> {
    const res = await fetch(`/api/trace/v1/export?traceId=${encodeURIComponent(traceId)}`, {
      headers: {
        Authorization: `Bearer ${localStorage.getItem('token') || ''}`,
      },
    })
    if (!res.ok) throw new Error('export failed')
    const blob = await res.blob()
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `trace-${traceId}.json`
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(url)
  },

  // 查询会话时间线（该 session 下所有链路，按时间排序）
  async sessionTimeline(sessionId: string): Promise<{ sessionId: string; total: number; runs: SessionTimelineSummary[] }> {
    const res = await api.get('/trace/v1/session_timeline', { params: { sessionId } })
    return res.data?.data || { sessionId, total: 0, runs: [] }
  },

  // 成本概览（汇总 + 每日趋势 + 模型分布 + 意图分布）
  async costOverview(params: { days?: number; startDate?: string; endDate?: string } = {}): Promise<CostOverview> {
    const res = await api.get('/trace/v1/cost/overview', { params })
    return res.data?.data || {}
  },

  // 实时 Token 趋势（小时粒度）
  async tokenTrend(hours = 24): Promise<{ points: TokenTrendPoint[] }> {
    const res = await api.get('/trace/v1/cost/token_trend', { params: { hours } })
    return res.data?.data || { points: [] }
  },

  // 导出会话对话快照（实时从 Redis 读取）
  async exportSessionSnapshot(sessionId: string): Promise<void> {
    const res = await fetch(`/api/trace/v1/export_session_snapshot?sessionId=${encodeURIComponent(sessionId)}`, {
      headers: {
        Authorization: `Bearer ${localStorage.getItem('token') || ''}`,
      },
    })
    if (!res.ok) throw new Error('export failed')
    const blob = await res.blob()
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `session-${sessionId}-snapshot.json`
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(url)
  },
}
