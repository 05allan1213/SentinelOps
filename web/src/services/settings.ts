import api from './api'
import type { ApiResponse } from '@/types'
import type { GeneralSettings } from '@/stores/settingsStore'

// 后端返回的原始字段格式（snake_case）
interface RawGeneralSettings {
  site_name: string
  auto_mark_read: boolean
}

export const settingsService = {
  async getGeneral(): Promise<GeneralSettings> {
    const res = await api.get<ApiResponse<{ settings: RawGeneralSettings }>>('/settings/v1/general')
    const raw = res.data.data?.settings
    if (!raw) return { siteName: '安全事件智能研判多智能体协同平台', autoMarkRead: true }
    return {
      siteName: raw.site_name || '安全事件智能研判多智能体协同平台',
      autoMarkRead: raw.auto_mark_read ?? true,
    }
  },

  async saveGeneral(s: GeneralSettings): Promise<void> {
    await api.post('/settings/v1/general', {
      site_name: s.siteName,
      auto_mark_read: s.autoMarkRead,
    })
  },

  async getIngestKey(): Promise<string> {
    const res = await api.get<ApiResponse<{ api_key: string }>>('/settings/v1/ingest_key')
    return res.data.data?.api_key || ''
  },

  async resetIngestKey(): Promise<string> {
    const res = await api.post<ApiResponse<{ api_key: string }>>('/settings/v1/ingest_key/reset', {})
    return res.data.data?.api_key || ''
  },
}
