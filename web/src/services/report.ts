import api from './api'
import {
  Report,
  GenerateReportRequest,
  ApiResponse,
  PaginatedResponse,
} from '@/types'
import { parseReportPayload } from '@/utils'

// 后端 /report/v1/list 返回格式
interface ReportListResponse {
  total: number
  reports: Array<{
    id: string
    title: string
    type: string
    period: string
    content: string
    created_at: string
  }>
}

export const reportService = {
  // 生成报告（后端 POST /report/v1/create，参数 title, content, type, period）
  async generate(data: GenerateReportRequest): Promise<Report> {
    const res = await api.post<ApiResponse<{ id: string }>>('/report/v1/create', {
      title: data.title || '新报告',
      content: '',
      type: data.type || 'custom',
      period: data.start_time && data.end_time ? `${data.start_time}~${data.end_time}` : '',
    })
    return {
      id: res.data.data?.id || '',
      title: data.title || '新报告',
      type: (data.type || 'custom') as Report['type'],
      status: 'completed',
      start_time: data.start_time || '',
      end_time: data.end_time || '',
      content: '',
      summary: '',
      event_ids: '',
      subscription_ids: '',
      event_count: 0,
      critical_count: 0,
      high_count: 0,
      generated_by: 'manual',
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    }
  },

  // 获取报告列表（后端 GET /report/v1/list?limit=&offset=&type=）
  async list(page = 1, pageSize = 20, type?: string): Promise<PaginatedResponse<Report>> {
    const params: Record<string, unknown> = {
      limit: pageSize,
      offset: (page - 1) * pageSize,
    }
    if (type && type !== 'all') params.type = type

    const res = await api.get<ApiResponse<ReportListResponse>>('/report/v1/list', { params })
    const data = res.data.data
    const reports = data?.reports || []

    const list: Report[] = reports.map((item) => {
      const payload = parseReportPayload(item.content || '')
      return {
        id: item.id,
        title: item.title,
        type: (item.type || 'custom') as Report['type'],
        status: 'completed',
        start_time: '',
        end_time: '',
        content: item.content || '',
        summary: '',
        event_ids: '',
        subscription_ids: '',
        event_count: payload?.meta.event_count ?? 0,
        critical_count: payload?.meta.critical_count ?? 0,
        high_count: payload?.meta.high_count ?? 0,
        generated_by: 'manual',
        created_at: item.created_at,
        updated_at: item.created_at,
      }
    })

    return {
      list,
      total: data?.total ?? list.length,
      page,
      size: pageSize,
    }
  },

  // 获取单个报告（后端 GET /report/v1/get?id=）
  async get(id: string): Promise<Report> {
    const res = await api.get<ApiResponse<{ report: { id: string; title: string; type: string; period: string; content: string; created_at: string } }>>('/report/v1/get', { params: { id } })
    const item = res.data.data?.report
    if (!item) throw new Error('报告不存在')
    return {
      id: item.id,
      title: item.title,
      type: (item.type || 'custom') as Report['type'],
      status: 'completed',
      start_time: '',
      end_time: '',
      content: item.content || '',
      summary: '',
      event_ids: '',
      subscription_ids: '',
      event_count: 0,
      critical_count: 0,
      high_count: 0,
      generated_by: 'manual',
      created_at: item.created_at,
      updated_at: item.created_at,
    }
  },

  // 删除报告（后端 POST /report/v1/delete）
  async delete(id: string): Promise<void> {
    await api.post('/report/v1/delete', { id })
  },

  // 批量删除报告（无批量接口，并发单条删除）
  async batchDelete(ids: string[]): Promise<void> {
    await Promise.all(ids.map(id => api.post('/report/v1/delete', { id })))
  },

  // 导出报告（后端无此接口，暂时 mock）
  async export(_id: string, _format: string): Promise<Blob> {
    return new Blob([''])
  },

  // 保存 Agent 分析生成的报告到数据库（POST /report/v1/create）
  async save(title: string, content: string, type = 'custom'): Promise<string> {
    const res = await api.post<ApiResponse<{ id: string }>>('/report/v1/create', { title, content, type })
    return res.data.data?.id || ''
  },
}
