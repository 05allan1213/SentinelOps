import api from './api'
import {
  SecurityEvent,
  EventFilter,
  ApiResponse,
  PaginatedResponse,
} from '@/types'

// 后端 /event/v1/list 返回格式
interface EventListResponse {
  total: number
  events: Array<{
    id: string
    title: string
    event_type?: string
    severity: string
    source: string
    source_url?: string
    status: string
    cve_id?: string
    risk_score?: number
    created_at: string
  }>
}

// 后端 /event/v1/stats 返回格式
interface EventStatsResponse {
  total: number
  today_count: number
  critical_count: number
  by_severity: Record<string, number>
  new_7days: number
  pending: number
}

// 后端 /event/v1/trend 返回格式
interface EventTrendResponse {
  items: Array<{
    date: string
    total: number
    critical: number
    high: number
    medium: number
    low: number
  }>
}

export const eventService = {
  // 获取事件列表（后端 GET /event/v1/list?limit=&offset=&severity=&status=&keyword=&order_by=&order_dir=）
  async list(filter?: EventFilter): Promise<PaginatedResponse<SecurityEvent>> {
    const page = filter?.page || 1
    const size = filter?.size || 20
    const params: Record<string, unknown> = {
      limit: size,
      offset: (page - 1) * size,
    }
    if (filter?.severity) params.severity = filter.severity
    if (filter?.status) params.status = filter.status
    if (filter?.keyword) params.keyword = filter.keyword
    if (filter?.order_by) params.order_by = filter.order_by
    if (filter?.order_dir) params.order_dir = filter.order_dir

    const res = await api.get<ApiResponse<EventListResponse>>('/event/v1/list', { params })
    const data = res.data.data
    const events = data?.events || []

    const list: SecurityEvent[] = events.map(item => ({
      id: item.id,
      subscription_id: 0,
      title: item.title,
      event_type: item.event_type,
      severity: (item.severity || 'medium') as SecurityEvent['severity'],
      status: (item.status || 'new') as SecurityEvent['status'],
      source: item.source || '',
      source_url: item.source_url || '',
      event_time: item.created_at,
      cve_id: item.cve_id,
      cvss_score: item.risk_score,
      created_at: item.created_at,
    }))

    return {
      list,
      total: data?.total ?? list.length,
      page,
      size,
    }
  },

  // 获取单个事件（后端无此接口，从列表查找）
  async get(id: string): Promise<SecurityEvent> {
    const res = await this.list({ size: 100 })
    const found = res.list.find(e => e.id === id)
    if (!found) throw new Error('事件不存在')
    return found
  },

  // 更新事件状态（POST /event/v1/update_status）
  async updateStatus(id: string, status: string): Promise<void> {
    await api.post('/event/v1/update_status', { id, status })
  },

  // 删除安全事件（POST /event/v1/delete）
  async delete(id: string): Promise<void> {
    await api.post('/event/v1/delete', { id })
  },

  // 批量删除安全事件（POST /event/v1/batch_delete）
  async batchDelete(ids: string[]): Promise<void> {
    await api.post('/event/v1/batch_delete', { ids })
  },

  // 批量更新事件状态（POST /event/v1/batch_update_status）
  async batchUpdateStatus(ids: string[], status: string): Promise<void> {
    await api.post('/event/v1/batch_update_status', { ids, status })
  },

  // 获取事件统计（调用后端 GET /event/v1/stats）
  async getStats(): Promise<{
    total: number
    today_count: number
    critical_count: number
    by_severity: Record<string, number>
    by_status: Record<string, number>
    new_7days: number
    pending: number
  }> {
    try {
      const res = await api.get<ApiResponse<EventStatsResponse>>('/event/v1/stats')
      const data = res.data.data
      return {
        total: data?.total || 0,
        today_count: data?.today_count || 0,
        critical_count: data?.critical_count || 0,
        by_severity: data?.by_severity || {},
        by_status: {},
        new_7days: data?.new_7days || 0,
        pending: data?.pending || 0,
      }
    } catch {
      return { total: 0, today_count: 0, critical_count: 0, by_severity: {}, by_status: {}, new_7days: 0, pending: 0 }
    }
  },

  // 获取事件趋势（调用后端 GET /event/v1/trend?days=N）
  async getTrend(days = 30): Promise<Array<{
    date: string
    total: number
    critical: number
    high: number
    medium: number
    low: number
    info: number
  }>> {
    try {
      const res = await api.get<ApiResponse<EventTrendResponse>>('/event/v1/trend', { params: { days } })
      return (res.data.data?.items || []).map(item => ({ ...item, info: 0 }))
    } catch {
      return []
    }
  },

  // AI分析事件（后端无此接口，暂时 mock）
  async analyze(_id: string): Promise<{ risk_score: number; severity: string; recommendation: string }> {
    return { risk_score: 0, severity: 'medium', recommendation: '' }
  },

  // 多Agent流水线处理（后端无此接口，改用 pipeline stream）
  async processPipeline(): Promise<{ total_count: number; dedup_count: number; new_count: number; processed_at: string; steps: Array<{agent: string; status: string; message: string; count: number}> }> {
    // 后端无 /event/pipeline/process，返回空结果
    return {
      total_count: 0,
      dedup_count: 0,
      new_count: 0,
      processed_at: new Date().toISOString(),
      steps: [],
    }
  },
}
