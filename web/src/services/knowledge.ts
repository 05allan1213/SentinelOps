import api from './api'

export interface KnowledgeBase {
  id: string
  name: string
  description: string
  doc_count: number
  chunk_count: number
  created_at: string
  updated_at: string
}

export interface DocItem {
  id: string
  name: string
  file_size: number
  file_type: string
  chunk_count: number
  indexed_chunks: number  // 已写入 MySQL 的分块数（索引进度追踪）
  chunk_strategy: string
  index_status: 'pending' | 'indexing' | 'completed' | 'failed'
  index_error?: string
  indexed_at?: string
  index_duration_ms?: number
  enabled: boolean
  created_at: string
}

export interface ChunkItem {
  id: string
  chunk_index: number
  content_preview: string
  section_title?: string
  char_count: number
  enabled: boolean
  updated_at: string
}

export interface SearchResultItem {
  chunk_id: string
  doc_id: string
  doc_title?: string
  content: string
  section_title?: string
  base_id?: string
  score?: number  // 余弦相似度分（0-1）
}

interface ApiWrap<T> { data: T }

export interface ListDocParams {
  page?: number
  pageSize?: number
  keyword?: string
  status?: string
  fileType?: string
}

export interface ListChunkParams {
  page?: number
  pageSize?: number
  keyword?: string
}

export const knowledgeService = {
  // 知识库
  async listBases(): Promise<KnowledgeBase[]> {
    const res = await api.get<ApiWrap<{ list: KnowledgeBase[] }>>('/knowledge/v1/bases/list')
    return res.data.data?.list ?? []
  },

  async getBase(id: string): Promise<KnowledgeBase> {
    const res = await api.get<ApiWrap<KnowledgeBase>>('/knowledge/v1/bases/detail', { params: { id } })
    return res.data.data!
  },

  async createBase(name: string, description: string): Promise<KnowledgeBase> {
    const res = await api.post<ApiWrap<KnowledgeBase>>('/knowledge/v1/bases/create', { name, description })
    return res.data.data!
  },

  async deleteBase(id: string): Promise<void> {
    await api.post('/knowledge/v1/bases/delete', { id })
  },

  // 文档
  async listDoc(
    baseID: string,
    params: ListDocParams = {},
  ): Promise<{ list: DocItem[]; total: number }> {
    const { page = 1, pageSize = 20, keyword, status, fileType } = params
    const res = await api.get<ApiWrap<{ list: DocItem[]; total: number }>>('/knowledge/v1/docs/list', {
      params: {
        base_id: baseID,
        page,
        page_size: pageSize,
        ...(keyword ? { keyword } : {}),
        ...(status  ? { status }  : {}),
        ...(fileType ? { file_type: fileType } : {}),
      },
    })
    return { list: res.data.data?.list ?? [], total: res.data.data?.total ?? 0 }
  },

  async getDoc(id: string): Promise<DocItem> {
    const res = await api.get<ApiWrap<DocItem>>('/knowledge/v1/docs/detail', { params: { id } })
    return res.data.data!
  },

  async uploadDoc(baseID: string, file: File, strategy?: string, chunkSize?: number): Promise<DocItem & { doc_id: string }> {
    const form = new FormData()
    form.append('base_id', baseID)
    if (strategy) form.append('chunk_strategy', strategy)
    if (chunkSize) form.append('chunk_size', String(chunkSize))
    form.append('file', file)
    const res = await api.post<ApiWrap<{ doc_id: string; name: string; index_status: string }>>(
      '/knowledge/v1/docs/upload',
      form,
      { headers: { 'Content-Type': 'multipart/form-data' } },
    )
    const d = res.data.data!
    return {
      id: d.doc_id,
      doc_id: d.doc_id,
      name: d.name,
      file_size: file.size,
      file_type: file.name.split('.').pop() ?? '',
      chunk_count: 0,
      indexed_chunks: 0,
      chunk_strategy: strategy ?? '',
      index_status: d.index_status as DocItem['index_status'],
      enabled: true,
      created_at: new Date().toISOString(),
    }
  },

  async deleteDoc(id: string): Promise<void> {
    await api.post('/knowledge/v1/docs/delete', { id })
  },

  async rebuildDoc(id: string, chunkStrategy: string): Promise<void> {
    await api.post('/knowledge/v1/docs/rebuild', { id, chunk_strategy: chunkStrategy })
  },

  async batchDeleteDocs(ids: string[]): Promise<{ deleted: number; failed: number }> {
    const res = await api.post<ApiWrap<{ deleted: number; failed: number }>>('/knowledge/v1/docs/batch_delete', { ids })
    return res.data.data ?? { deleted: 0, failed: 0 }
  },

  async batchRebuildDocs(ids: string[], chunkStrategy: string): Promise<{ submitted: number; failed: number }> {
    const res = await api.post<ApiWrap<{ submitted: number; failed: number }>>('/knowledge/v1/docs/batch_rebuild', { ids, chunk_strategy: chunkStrategy })
    return res.data.data ?? { submitted: 0, failed: 0 }
  },

  async enableDoc(id: string, enabled: boolean): Promise<void> {
    await api.post('/knowledge/v1/docs/enable', { id, enabled })
  },

  // 分块
  async listChunk(docID: string, params: ListChunkParams = {}): Promise<{ list: ChunkItem[]; total: number }> {
    const { page = 1, pageSize = 20, keyword } = params
    const res = await api.get<ApiWrap<{ list: ChunkItem[]; total: number }>>('/knowledge/v1/chunks/list', {
      params: {
        doc_id: docID,
        page,
        page_size: pageSize,
        ...(keyword ? { keyword } : {}),
      },
    })
    return { list: res.data.data?.list ?? [], total: res.data.data?.total ?? 0 }
  },

  async enableChunks(params: { docId?: string; ids?: string[]; enabled: boolean }): Promise<number> {
    const res = await api.post<ApiWrap<{ updated: number }>>('/knowledge/v1/chunks/enable', {
      doc_id: params.docId,
      ids: params.ids,
      enabled: params.enabled,
    })
    return res.data.data?.updated ?? 0
  },

  async queueStatus(): Promise<number> {
    const res = await api.get<ApiWrap<{ queue_length: number }>>('/knowledge/v1/queue/status')
    return res.data.data?.queue_length ?? 0
  },

  // RAG 检索测试
  async searchDocs(
    baseID: string,
    query: string,
    topK = 5,
  ): Promise<{ results: SearchResultItem[]; cached: boolean }> {
    const res = await api.post<ApiWrap<{ results: SearchResultItem[]; cached: boolean }>>(
      '/knowledge/v1/search',
      { base_id: baseID, query, top_k: topK },
    )
    return res.data.data ?? { results: [], cached: false }
  },
}
