import api from './api'

export interface TermMappingItem {
  id: number
  source_term: string
  target_term: string
  priority: number
  enabled: boolean
  created_at: string
  updated_at: string
}

export interface CreateTermMappingReq {
  source_term: string
  target_term: string
  priority: number
  enabled: boolean
}

export interface UpdateTermMappingReq {
  id: number
  target_term: string
  priority: number
  enabled: boolean
}

export const termMappingService = {
  async list(): Promise<TermMappingItem[]> {
    const res = await api.get<{ data: { items: TermMappingItem[] } }>('/term_mapping/v1/list')
    return res.data.data?.items || []
  },

  async create(data: CreateTermMappingReq): Promise<{ id: number }> {
    const res = await api.post<{ data: { id: number } }>('/term_mapping/v1/create', data)
    return res.data.data
  },

  async update(data: UpdateTermMappingReq): Promise<void> {
    await api.post('/term_mapping/v1/update', data)
  },

  async delete(id: number): Promise<void> {
    await api.post('/term_mapping/v1/delete', { id })
  },

  async reload(): Promise<{ count: number }> {
    const res = await api.post<{ data: { count: number } }>('/term_mapping/v1/reload')
    return res.data.data
  },
}
