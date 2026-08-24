import api from './api'
import {
  Subscription,
  CreateSubscriptionRequest,
  ApiResponse,
  PaginatedResponse,
  FetchLog,
  FetchStats,
} from '@/types'

// 后端 /subscription/v1/list 返回格式
interface SubscriptionListResponse {
  subscriptions: Array<{
    id: string
    name: string
    url: string
    type: string
    enabled: boolean
    cron_expr?: string
    last_fetch_at?: string
    total_events?: number
    created_at?: string
    updated_at?: string
  }>
}

export const subscriptionService = {
  // 获取订阅列表（后端 GET /subscription/v1/list）
  async list(page = 1, pageSize = 20): Promise<PaginatedResponse<Subscription>> {
    const res = await api.get<ApiResponse<SubscriptionListResponse>>('/subscription/v1/list')
    const data = res.data.data
    const subs = data?.subscriptions || []

    const list: Subscription[] = subs.map((item) => ({
      id: item.id,
      name: item.name,
      description: '',
      // 后端将 github_repo 规范化存储为 github，此处反向映射还原前端类型
      source_type: (item.type === 'github' ? 'github_repo' : item.type as Subscription['source_type']) || 'rss',
      source_url: item.url,
      status: item.enabled ? 'active' : 'paused',
      config: '{}',
      cron_expr: item.cron_expr || '',
      last_fetch_at: item.last_fetch_at || '',
      next_fetch_at: '',
      fetch_timeout: 0,
      auth_type: 'none',
      keywords: '[]',
      min_severity: 'medium',
      tags: '',
      total_events: item.total_events || 0,
      failed_fetches: 0,
      created_at: item.created_at || '',
      updated_at: item.updated_at || '',
    }))

    return {
      list,
      total: list.length, // 后端未返回 total，使用列表长度
      page,
      size: pageSize,
    }
  },

  // 获取单个订阅（从列表查找）
  async get(id: string): Promise<Subscription> {
    const res = await this.list(1, 100)
    const found = res.list.find(s => s.id === id)
    if (!found) throw new Error('订阅不存在')
    return found
  },

  // 创建订阅（后端 POST /subscription/v1/create，参数 name, url, type, cron_expr）
  async create(data: CreateSubscriptionRequest): Promise<{ id: string }> {
    const res = await api.post<ApiResponse<{ id: string }>>('/subscription/v1/create', {
      name: data.name,
      url: data.source_url,
      type: data.source_type || 'rss',
      cron_expr: data.cron_expr || '',
    })
    return { id: res.data.data.id }
  },

  // 更新订阅
  async update(id: string, data: { name?: string; url?: string; type?: string; cron_expr?: string }): Promise<void> {
    await api.post('/subscription/v1/update', { id, ...data })
  },

  // 删除订阅
  async delete(id: string): Promise<void> {
    await api.post('/subscription/v1/delete', { id })
  },

  // 暂停订阅
  async pause(id: string): Promise<void> {
    await api.post('/subscription/v1/pause', { id })
  },

  // 恢复订阅
  async resume(id: string): Promise<void> {
    await api.post('/subscription/v1/resume', { id })
  },

  // 禁用订阅
  async disable(id: string): Promise<void> {
    await api.post('/subscription/v1/pause', { id })
  },

  // 获取订阅的抓取日志（后端 GET /subscription/v1/logs?subscription_id=&limit=&offset=）
  async getFetchLogs(subscriptionId: string, page = 1, pageSize = 20): Promise<PaginatedResponse<FetchLog>> {
    const res = await api.get<ApiResponse<{ total: number; logs: Array<{
      id: number; status: string; fetched_count: number; new_count: number;
      duration_ms: number; error_msg?: string; created_at: string
    }> }>>('/subscription/v1/logs', {
      params: { subscription_id: subscriptionId, limit: pageSize, offset: (page - 1) * pageSize }
    })
    const data = res.data.data
    const list: FetchLog[] = (data?.logs || []).map(l => ({
      id: l.id,
      subscription_id: parseInt(subscriptionId, 10),
      status: l.status as FetchLog['status'],
      event_count: l.new_count,
      duration: l.duration_ms,
      error_msg: l.error_msg || '',
      created_at: l.created_at,
    }))
    return { list, total: data?.total ?? list.length, page, size: pageSize }
  },

  // 获取订阅的抓取统计（后端无此接口，暂时 mock）
  async getFetchStats(_subscriptionId: string): Promise<FetchStats> {
    return {
      total_fetches: 0,
      success_count: 0,
      failed_count: 0,
      total_events: 0,
      avg_duration_ms: 0,
    }
  },

  // 手动触发抓取
  async fetch(id: string | number): Promise<{
    fetched_count: number
    new_count: number
    total_events: number
    duration_ms: number
    message: string
  }> {
    const res = await api.post<ApiResponse<{
      fetched_count: number
      new_count: number
      total_events: number
      duration_ms: number
      message: string
    }>>('/subscription/v1/fetch', { id: id.toString() })
    return res.data.data
  },
}
