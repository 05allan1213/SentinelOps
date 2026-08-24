import axios from 'axios'

export interface IngestResult {
  id: string
  is_new: boolean
  dedup_key: string
}

// 接入端点使用 X-API-Key 认证，不走 JWT 拦截器，避免 401 跳登录页
const ingestApi = axios.create({ baseURL: '/api', timeout: 30000 })

// 从 localStorage 读取已缓存的 API Key（由 ingest 页面加载后写入）
function getStoredKey(): string {
  return localStorage.getItem('ingest_api_key') || ''
}

export function cacheIngestKey(key: string) {
  localStorage.setItem('ingest_api_key', key)
}

function headers(extra?: Record<string, string>) {
  return { 'X-API-Key': getStoredKey(), ...extra }
}

export const ingestService = {
  // 通用 JSON Webhook 推送
  async webhook(payload: Record<string, unknown>, sourceName?: string): Promise<IngestResult> {
    const res = await ingestApi.post('/ingest/v1/webhook', payload, {
      headers: headers(),
      params: sourceName ? { source_name: sourceName } : {},
    })
    return res.data.data
  },

  // 标准化 REST API 推送（推荐新系统使用）
  async push(alert: {
    title: string
    content?: string
    severity?: string
    source: string
    cve_id?: string
    extra_fields?: Record<string, string>
  }): Promise<IngestResult> {
    const res = await ingestApi.post('/ingest/v1/push', alert, { headers: headers() })
    return res.data.data
  },

  // CEF 格式推送（纯文本）
  async cef(line: string, sourceName?: string): Promise<IngestResult> {
    const res = await ingestApi.post('/ingest/v1/cef', line, {
      headers: headers({ 'Content-Type': 'text/plain' }),
      params: sourceName ? { source_name: sourceName } : {},
    })
    return res.data.data
  },

  // LEEF 格式推送（纯文本）
  async leef(line: string, sourceName?: string): Promise<IngestResult> {
    const res = await ingestApi.post('/ingest/v1/leef', line, {
      headers: headers({ 'Content-Type': 'text/plain' }),
      params: sourceName ? { source_name: sourceName } : {},
    })
    return res.data.data
  },
}
