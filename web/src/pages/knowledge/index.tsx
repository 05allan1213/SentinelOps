import React, { useState, useEffect, useCallback, useRef } from 'react'
import { useSearchParams } from 'react-router-dom'
import {
  Plus, Database, Trash2, Loader2, FileText, Layers,
  RefreshCw, ArrowRight, Search, Inbox,
  Upload, CheckCircle2, XCircle, Clock, X, RotateCcw,
  Timer, Hash, AlignLeft, ChevronDown, ChevronUp,
  FlaskConical,
} from 'lucide-react'
import { cn } from '@/utils'
import {
  knowledgeService,
  type KnowledgeBase,
  type DocItem,
  type ChunkItem,
  type SearchResultItem,
} from '@/services/knowledge'
import StatCard from '@/components/common/StatCard'
import Pagination from '@/components/common/Pagination'
import BaseCreateModal from './components/BaseCreateModal'
import DocUploadModal from './components/DocUploadModal'
import RebuildModal from './components/RebuildModal'
import ConfirmDialog from '@/components/common/ConfirmDialog'
import toast from 'react-hot-toast'
import CustomSelect, { type SelectOption } from '@/components/common/CustomSelect'

// ── 常量 ──────────────────────────────────────────────────────────────────────

type TabType = 'bases' | 'docs' | 'chunks'

const STATUS_CONFIG = {
  pending:   { label: '待索引', Icon: Clock,        cls: 'bg-gray-100 text-gray-600 border-gray-200' },
  indexing:  { label: '索引中', Icon: Loader2,      cls: 'bg-blue-50 text-blue-700 border-blue-200' },
  completed: { label: '已完成', Icon: CheckCircle2, cls: 'bg-emerald-50 text-emerald-700 border-emerald-200' },
  failed:    { label: '失败',   Icon: XCircle,      cls: 'bg-red-50 text-red-700 border-red-200' },
}

const STRATEGY_LABEL: Record<string, string> = {
  sliding_window:       '滑动窗口',
  hierarchical:     '层级分块',
  code:             '代码切分',
}

const STATUS_OPTIONS = [
  { value: '', label: '全部状态' },
  { value: 'pending',   label: '待索引' },
  { value: 'indexing',  label: '索引中' },
  { value: 'completed', label: '已完成' },
  { value: 'failed',    label: '失败' },
]

const FILE_TYPE_OPTIONS = [
  { value: '', label: '全部类型' },
  { value: 'pdf',  label: 'PDF' },
  { value: 'md',   label: 'Markdown' },
  { value: 'docx', label: 'Word (DOCX)' },
  { value: 'doc',  label: 'Word (DOC)' },
]

// ── 辅助函数 ──────────────────────────────────────────────────────────────────

function formatDuration(ms?: number) {
  if (!ms || ms <= 0) return null
  if (ms < 1000) return `${ms}ms`
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`
  const m = Math.floor(ms / 60000)
  const s = Math.round((ms % 60000) / 1000)
  return s > 0 ? `${m}m${s}s` : `${m}m`
}

function formatSize(bytes: number) {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`
}

function IndexProgress({ doc }: { doc: DocItem }) {
  if (doc.index_status !== 'indexing' && doc.index_status !== 'pending') return null
  if (doc.chunk_count === 0) return <span className="text-xs text-gray-400">准备中…</span>
  const pct = Math.min(Math.round((doc.indexed_chunks / doc.chunk_count) * 100), 100)
  return (
    <div className="flex items-center gap-1.5 mt-0.5 pl-6">
      <div className="flex-1 h-1 rounded-full bg-gray-200 overflow-hidden" style={{ maxWidth: 80 }}>
        <div className="h-full rounded-full bg-blue-500 transition-all duration-500" style={{ width: `${pct}%` }} />
      </div>
      <span className="text-xs text-blue-600 tabular-nums">{pct}%</span>
    </div>
  )
}

// ── RAG 检索测试模态框 ──────────────────────────────────────────────────────────

function SearchModal({ baseID, onClose }: { baseID: string; onClose: () => void }) {
  const [query, setQuery] = useState('')
  const [topK, setTopK] = useState(5)
  const [loading, setLoading] = useState(false)
  const [results, setResults] = useState<SearchResultItem[] | null>(null)
  const [cached, setCached] = useState(false)

  const handleSearch = async () => {
    if (!query.trim()) { toast.error('请输入查询词'); return }
    try {
      setLoading(true)
      const res = await knowledgeService.searchDocs(baseID, query.trim(), topK)
      setResults(res.results)
      setCached(res.cached)
    } catch { toast.error('检索失败') }
    finally { setLoading(false) }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
      <div className="bg-white rounded-xl shadow-xl w-full max-w-2xl flex flex-col max-h-[85vh]">
        <div className="flex items-center justify-between px-6 py-4 border-b border-gray-100 flex-shrink-0">
          <div>
            <h3 className="text-base font-semibold text-gray-900 flex items-center gap-2">
              <FlaskConical className="w-4 h-4 text-indigo-500" />
              RAG 检索测试
            </h3>
            <p className="text-xs text-gray-500 mt-0.5">直接查询向量库，验证召回效果</p>
          </div>
          <button onClick={onClose} className="p-1.5 rounded-lg hover:bg-gray-100 transition-colors" aria-label="关闭">
            <X className="w-5 h-5 text-gray-500" />
          </button>
        </div>
        <div className="px-6 py-4 border-b border-gray-100 flex-shrink-0 space-y-3">
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">查询词</label>
            <textarea
              value={query}
              onChange={e => setQuery(e.target.value)}
              onKeyDown={e => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); handleSearch() } }}
              rows={2}
              placeholder="输入查询词，按 Enter 搜索…"
              className="w-full px-3 py-2 border border-gray-200 rounded-lg text-sm resize-none focus:outline-none focus:ring-2 focus:ring-indigo-500/30 focus:border-indigo-400"
            />
          </div>
          <div className="flex items-center gap-4">
            <div className="flex items-center gap-2">
              <label className="text-sm text-gray-600">召回数量 Top-K</label>
              <CustomSelect
                value={topK}
                onChange={v => setTopK(Number(v))}
                className="w-20"
                options={[3, 5, 8, 10, 15, 20].map(n => ({ value: n, label: String(n) }))}
              />
            </div>
            <button
              onClick={handleSearch}
              disabled={loading || !query.trim()}
              className={cn('ml-auto flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium text-white transition-colors',
                loading || !query.trim() ? 'bg-indigo-400 cursor-not-allowed' : 'bg-indigo-600 hover:bg-indigo-700')}
            >
              {loading ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Search className="w-3.5 h-3.5" />}
              检索
            </button>
          </div>
        </div>
        <div className="flex-1 overflow-y-auto px-6 py-4 space-y-3">
          {results === null ? (
            <div className="text-center py-12 text-gray-400">
              <FlaskConical className="w-12 h-12 mx-auto mb-3 text-gray-200" />
              <p className="text-sm">输入查询词，开始检索测试</p>
            </div>
          ) : results.length === 0 ? (
            <div className="text-center py-12 text-gray-400">
              <Inbox className="w-12 h-12 mx-auto mb-3 text-gray-200" />
              <p className="text-sm">未召回任何结果</p>
            </div>
          ) : (
            <>
              <div className="flex items-center gap-2 text-xs text-gray-500">
                <span>共召回 <strong className="text-gray-700">{results.length}</strong> 条</span>
                {cached && <span className="tag bg-amber-50 text-amber-600 border border-amber-200">语义缓存命中</span>}
              </div>
              {results.map((r, i) => (
                <div key={r.chunk_id} className="border border-gray-200 rounded-lg p-4 hover:border-indigo-200 transition-colors">
                  <div className="flex items-center gap-2 mb-2">
                    <span className="text-xs font-mono bg-gray-100 text-gray-600 px-1.5 py-0.5 rounded">#{i + 1}</span>
                    {r.score !== undefined && (
                      <div className="flex items-center gap-1.5">
                        <div className="w-20 h-1.5 rounded-full bg-gray-200 overflow-hidden">
                          <div className="h-full rounded-full bg-indigo-500" style={{ width: `${Math.round(r.score * 100)}%` }} />
                        </div>
                        <span className="text-xs text-indigo-600 tabular-nums font-medium">{r.score.toFixed(2)}</span>
                      </div>
                    )}
                    {r.doc_title && <span className="text-xs text-gray-500 truncate" title={r.doc_title}>{r.doc_title}</span>}
                    {r.section_title && (
                      <span className="tag bg-indigo-50 text-indigo-700 ml-auto flex-shrink-0 max-w-[140px] truncate" title={r.section_title}>
                        {r.section_title}
                      </span>
                    )}
                  </div>
                  <p className="text-sm text-gray-700 leading-relaxed whitespace-pre-wrap line-clamp-6">{r.content}</p>
                </div>
              ))}
            </>
          )}
        </div>
      </div>
    </div>
  )
}

// ── 主组件 ────────────────────────────────────────────────────────────────────

export default function Knowledge() {
  const [searchParams, setSearchParams] = useSearchParams()

  // Tab 状态（URL 同步）
  const tabFromUrl = (searchParams.get('tab') as TabType) || 'bases'
  const [activeTab, setActiveTab] = useState<TabType>(tabFromUrl)
  const baseIdFromUrl = searchParams.get('baseId') || ''
  const docIdFromUrl  = searchParams.get('docId')  || ''

  const switchTab = (tab: TabType, extra?: Record<string, string>) => {
    setActiveTab(tab)
    const params: Record<string, string> = { tab, ...extra }
    if (tab === 'bases') { /* 清除子级参数 */ }
    else if (tab === 'docs' && !extra?.baseId) params.baseId = baseIdFromUrl
    else if (tab === 'chunks') { params.baseId = extra?.baseId ?? baseIdFromUrl; params.docId = extra?.docId ?? docIdFromUrl }
    setSearchParams(params, { replace: true })
  }

  // ── 知识库列表状态 ──────────────────────────────────────────────────────────
  const [bases, setBases] = useState<KnowledgeBase[]>([])
  const [basesLoading, setBasesLoading] = useState(false)
  const [baseSearch, setBaseSearch] = useState('')
  const [basePage, setBasePage] = useState(1)
  const [basePageSize, setBasePageSize] = useState(10)
  const [baseSelected, setBaseSelected] = useState<Set<string>>(new Set())
  const [showCreate, setShowCreate] = useState(false)
  const [confirmDeleteBase, setConfirmDeleteBase] = useState<{ open: boolean; base?: KnowledgeBase; isBatch?: boolean }>({ open: false })
  const [queueLen, setQueueLen] = useState(0)

  // ── 文档管理状态 ────────────────────────────────────────────────────────────
  const [currentBase, setCurrentBase] = useState<KnowledgeBase | null>(null)
  const [docs, setDocs] = useState<DocItem[]>([])
  const [docsTotal, setDocsTotal] = useState(0)
  const [docsLoading, setDocsLoading] = useState(false)
  const [docPage, setDocPage] = useState(1)
  const [docPageSize, setDocPageSize] = useState(10)
  const [docSearch, setDocSearch] = useState('')
  const [docSearchInput, setDocSearchInput] = useState('')
  const [statusFilter, setStatusFilter] = useState('')
  const [fileTypeFilter, setFileTypeFilter] = useState('')
  const [docSelected, setDocSelected] = useState<Set<string>>(new Set())
  const [showUpload, setShowUpload] = useState(false)
  const [confirmDeleteDoc, setConfirmDeleteDoc] = useState<{ open: boolean; doc?: DocItem; isBatch?: boolean }>({ open: false })
  const [rebuildTarget, setRebuildTarget] = useState<{ docId?: string; docIds?: string[]; currentStrategy?: string } | null>(null)
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)

  // ── 分块管理状态 ────────────────────────────────────────────────────────────
  const [currentDoc, setCurrentDoc] = useState<DocItem | null>(null)
  const [chunks, setChunks] = useState<ChunkItem[]>([])
  const [chunksTotal, setChunksTotal] = useState(0)
  const [chunksLoading, setChunksLoading] = useState(false)
  const [chunkPage, setChunkPage] = useState(1)
  const [chunkPageSize, setChunkPageSize] = useState(10)
  const [chunkKeyword, setChunkKeyword] = useState('')
  const [chunkKeywordInput, setChunkKeywordInput] = useState('')
  const [chunkSelected, setChunkSelected] = useState<Set<string>>(new Set())
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [showSearch, setShowSearch] = useState(false)

  // ── 数据加载 ────────────────────────────────────────────────────────────────

  const fetchBases = useCallback(async () => {
    try {
      setBasesLoading(true)
      setBases(await knowledgeService.listBases())
    } catch {
      // 静默处理，后端无数据时不显示错误
    }
    finally { setBasesLoading(false) }
  }, [])

  const fetchDocs = useCallback(async () => {
    if (!baseIdFromUrl) return
    try {
      setDocsLoading(true)
      const [baseInfo, { list, total }] = await Promise.all([
        knowledgeService.getBase(baseIdFromUrl),
        knowledgeService.listDoc(baseIdFromUrl, {
          page: docPage, pageSize: docPageSize,
          keyword: docSearch || undefined,
          status: statusFilter || undefined,
          fileType: fileTypeFilter || undefined,
        }),
      ])
      setCurrentBase(baseInfo)
      setDocs(list)
      setDocsTotal(total)
    } catch { toast.error('获取文档列表失败') }
    finally { setDocsLoading(false) }
  }, [baseIdFromUrl, docPage, docPageSize, docSearch, statusFilter, fileTypeFilter])

  const fetchChunks = useCallback(async () => {
    if (!docIdFromUrl) return
    try {
      setChunksLoading(true)
      const [baseInfo, docInfo, { list, total }] = await Promise.all([
        baseIdFromUrl ? knowledgeService.getBase(baseIdFromUrl) : Promise.resolve(null),
        knowledgeService.getDoc(docIdFromUrl),
        knowledgeService.listChunk(docIdFromUrl, { page: chunkPage, pageSize: chunkPageSize, keyword: chunkKeyword || undefined }),
      ])
      if (baseInfo) setCurrentBase(baseInfo)
      setCurrentDoc(docInfo)
      setChunks(list)
      setChunksTotal(total)
    } catch { toast.error('获取分块列表失败') }
    finally { setChunksLoading(false) }
  }, [baseIdFromUrl, docIdFromUrl, chunkPage, chunkPageSize, chunkKeyword])

  useEffect(() => { fetchBases() }, [fetchBases])

  useEffect(() => {
    const poll = async () => {
      try { setQueueLen(await knowledgeService.queueStatus()) } catch { /* ignore */ }
    }
    poll()
    const t = setInterval(poll, 5000)
    return () => clearInterval(t)
  }, [])

  useEffect(() => {
    if (activeTab === 'docs' && baseIdFromUrl) fetchDocs()
  }, [activeTab, baseIdFromUrl, fetchDocs])

  useEffect(() => {
    if (activeTab === 'chunks' && docIdFromUrl) fetchChunks()
  }, [activeTab, docIdFromUrl, fetchChunks])

  // 文档索引中时自动轮询
  useEffect(() => {
    const hasActive = docs.some(d => d.index_status === 'pending' || d.index_status === 'indexing')
    if (hasActive) {
      pollRef.current = setInterval(fetchDocs, 3000)
    } else {
      if (pollRef.current) { clearInterval(pollRef.current); pollRef.current = null }
    }
    return () => { if (pollRef.current) clearInterval(pollRef.current) }
  }, [docs, fetchDocs])

  // ── 知识库操作 ──────────────────────────────────────────────────────────────

  const filteredBases = bases.filter(b =>
    b.name.toLowerCase().includes(baseSearch.toLowerCase()) ||
    b.description?.toLowerCase().includes(baseSearch.toLowerCase())
  )
  const paginatedBases = filteredBases.slice((basePage - 1) * basePageSize, basePage * basePageSize)
  const baseTotalPages = Math.ceil(filteredBases.length / basePageSize)
  const totalDocs = bases.reduce((s, b) => s + b.doc_count, 0)
  const totalChunks = bases.reduce((s, b) => s + b.chunk_count, 0)

  const baseSelectableIds = paginatedBases.filter(b => b.id !== 'default').map(b => b.id)
  const basePageAllSelected = baseSelectableIds.length > 0 && baseSelectableIds.every(id => baseSelected.has(id))
  const basePagePartialSelected = !basePageAllSelected && baseSelectableIds.some(id => baseSelected.has(id))

  const toggleBaseSelect = (id: string) => {
    setBaseSelected(prev => { const next = new Set(prev); next.has(id) ? next.delete(id) : next.add(id); return next })
  }
  const toggleBaseAll = () => {
    setBaseSelected(prev => {
      const next = new Set(prev)
      if (basePageAllSelected) baseSelectableIds.forEach(id => next.delete(id))
      else baseSelectableIds.forEach(id => next.add(id))
      return next
    })
  }

  const handleDeleteBase = async (base: KnowledgeBase) => {
    try {
      await knowledgeService.deleteBase(base.id)
      toast.success(`知识库「${base.name}」已删除`)
      setBases(prev => prev.filter(b => b.id !== base.id))
      setBaseSelected(prev => { const next = new Set(prev); next.delete(base.id); return next })
    } catch { toast.error('删除失败') }
  }

  const handleBatchDeleteBase = async () => {
    const ids = [...baseSelected].filter(id => id !== 'default')
    let ok = 0
    for (const id of ids) {
      try { await knowledgeService.deleteBase(id); ok++ } catch { /* ignore */ }
    }
    toast.success(`已删除 ${ok} 个知识库`)
    setBases(prev => prev.filter(b => !ids.includes(b.id)))
    setBaseSelected(new Set())
  }

  // ── 文档操作 ────────────────────────────────────────────────────────────────

  const docTotalPages = Math.ceil(docsTotal / docPageSize)
  const docStats = {
    total: docsTotal,
    indexing:  docs.filter(d => d.index_status === 'indexing' || d.index_status === 'pending').length,
    completed: docs.filter(d => d.index_status === 'completed').length,
    failed:    docs.filter(d => d.index_status === 'failed').length,
  }
  const docPageAllSelected = docs.length > 0 && docs.every(d => docSelected.has(d.id))
  const docPagePartialSelected = !docPageAllSelected && docs.some(d => docSelected.has(d.id))
  const hasActiveDocFilters = docSearch || statusFilter || fileTypeFilter

  const toggleDocSelect = (id: string) => {
    setDocSelected(prev => { const next = new Set(prev); next.has(id) ? next.delete(id) : next.add(id); return next })
  }
  const toggleDocAll = () => {
    setDocSelected(prev => {
      const next = new Set(prev)
      if (docPageAllSelected) docs.forEach(d => next.delete(d.id))
      else docs.forEach(d => next.add(d.id))
      return next
    })
  }

  const handleDeleteDoc = async (doc: DocItem) => {
    try {
      await knowledgeService.deleteDoc(doc.id)
      toast.success(`文档「${doc.name}」已删除`)
      setDocs(prev => prev.filter(d => d.id !== doc.id))
      setDocSelected(prev => { const next = new Set(prev); next.delete(doc.id); return next })
      setDocsTotal(t => t - 1)
    } catch { toast.error('删除失败') }
  }

  const handleBatchDeleteDoc = async () => {
    const ids = [...docSelected]
    try {
      const result = await knowledgeService.batchDeleteDocs(ids)
      toast.success(`已删除 ${result.deleted} 个文档${result.failed > 0 ? `，${result.failed} 个失败` : ''}`)
      setDocs(prev => prev.filter(d => !ids.includes(d.id)))
      setDocsTotal(t => t - result.deleted)
      setDocSelected(new Set())
    } catch { toast.error('批量删除失败') }
  }

  const handleToggleDocEnabled = async (doc: DocItem) => {
    const next = !doc.enabled
    setDocs(prev => prev.map(d => d.id === doc.id ? { ...d, enabled: next } : d))
    try {
      await knowledgeService.enableDoc(doc.id, next)
      toast.success(next ? `文档「${doc.name}」已启用` : `文档「${doc.name}」已禁用`)
    } catch {
      setDocs(prev => prev.map(d => d.id === doc.id ? { ...d, enabled: !next } : d))
      toast.error('操作失败')
    }
  }

  // ── 分块操作 ────────────────────────────────────────────────────────────────

  const chunkTotalPages = Math.ceil(chunksTotal / chunkPageSize)
  const avgChars = chunks.length > 0 ? Math.round(chunks.reduce((s, c) => s + c.char_count, 0) / chunks.length) : 0
  const sectionCount = new Set(chunks.map(c => c.section_title).filter(Boolean)).size
  const chunkPageAllSelected = chunks.length > 0 && chunks.every(c => chunkSelected.has(c.id))
  const chunkPagePartialSelected = !chunkPageAllSelected && chunks.some(c => chunkSelected.has(c.id))

  const toggleChunkSelect = (id: string) => {
    setChunkSelected(prev => { const next = new Set(prev); next.has(id) ? next.delete(id) : next.add(id); return next })
  }
  const toggleChunkAll = () => {
    setChunkSelected(prev => {
      const next = new Set(prev)
      if (chunkPageAllSelected) chunks.forEach(c => next.delete(c.id))
      else chunks.forEach(c => next.add(c.id))
      return next
    })
  }

  const handleBatchEnableChunk = async (enabled: boolean) => {
    const ids = [...chunkSelected]
    setChunks(prev => prev.map(c => ids.includes(c.id) ? { ...c, enabled } : c))
    try {
      const updated = await knowledgeService.enableChunks({ ids, enabled })
      toast.success(`已${enabled ? '启用' : '禁用'} ${updated} 个分块`)
      setChunkSelected(new Set())
    } catch {
      setChunks(prev => prev.map(c => ids.includes(c.id) ? { ...c, enabled: !enabled } : c))
      toast.error('操作失败')
    }
  }

  const handleAllEnableChunk = async (enabled: boolean) => {
    if (!docIdFromUrl) return
    setChunks(prev => prev.map(c => ({ ...c, enabled })))
    try {
      const updated = await knowledgeService.enableChunks({ docId: docIdFromUrl, enabled })
      toast.success(`已${enabled ? '启用' : '禁用'} ${updated} 个分块`)
    } catch { fetchChunks(); toast.error('操作失败') }
  }

  const handleToggleChunk = async (chunk: ChunkItem) => {
    const next = !chunk.enabled
    setChunks(prev => prev.map(c => c.id === chunk.id ? { ...c, enabled: next } : c))
    try {
      await knowledgeService.enableChunks({ ids: [chunk.id], enabled: next })
    } catch {
      setChunks(prev => prev.map(c => c.id === chunk.id ? { ...c, enabled: !next } : c))
      toast.error('操作失败')
    }
  }

  // ── 渲染 ────────────────────────────────────────────────────────────────────

  return (
    <div className="flex flex-col gap-5 h-full overflow-auto">

      {/* 标题栏 */}
      <div className="flex items-center justify-between flex-shrink-0">
        <div>
          <h1 className="text-2xl font-semibold text-gray-900 tracking-tight">AI 知识库</h1>
          <p className="text-sm text-gray-500 mt-1">
            {activeTab === 'bases' && `共 ${bases.length} 个知识库`}
            {activeTab === 'docs'  && (currentBase ? `${currentBase.name} · 共 ${docsTotal} 个文档` : '文档管理')}
            {activeTab === 'chunks' && (currentDoc ? `${currentDoc.name} · 共 ${chunksTotal} 个分块` : '分块管理')}
          </p>
        </div>
        <div className="flex items-center gap-2">
          {queueLen > 0 && (
            <span className="inline-flex items-center gap-1.5 text-xs text-amber-700 bg-amber-50 border border-amber-200 px-3 h-9 rounded-lg font-medium">
              <Loader2 className="w-3.5 h-3.5 animate-spin" />
              队列 {queueLen} 个任务
            </span>
          )}
          <button
            onClick={() => {
              fetchBases()
              if (activeTab === 'docs') fetchDocs()
              if (activeTab === 'chunks') fetchChunks()
            }}
            disabled={basesLoading || docsLoading || chunksLoading}
            className="btn-default"
          >
            <RefreshCw className={cn('w-4 h-4', (basesLoading || docsLoading || chunksLoading) && 'animate-spin')} />
            刷新
          </button>
          {activeTab === 'bases' && (
            <button onClick={() => setShowCreate(true)} className="btn btn-primary">
              <Plus className="w-4 h-4" />
              新建知识库
            </button>
          )}
          {activeTab === 'docs' && baseIdFromUrl && (
            <button onClick={() => setShowUpload(true)} className="btn btn-primary">
              <Upload className="w-4 h-4" />
              上传文档
            </button>
          )}
          {activeTab === 'chunks' && baseIdFromUrl && (
            <button
              onClick={() => setShowSearch(true)}
              className="inline-flex items-center gap-1.5 px-3 h-9 rounded-lg border border-indigo-200 bg-indigo-50 text-indigo-700 hover:bg-indigo-100 text-sm font-medium transition-colors"
            >
              <FlaskConical className="w-3.5 h-3.5" />
              检索测试
            </button>
          )}
        </div>
      </div>

      {/* Tab 导航 */}
      <div className="flex items-center gap-1 border-b border-gray-200 flex-shrink-0">
        {([
          { key: 'bases',  icon: Database,  label: '知识库列表' },
          { key: 'docs',   icon: FileText,  label: '文档管理' },
          { key: 'chunks', icon: Layers,    label: '分块管理' },
        ] as { key: TabType; icon: typeof Database; label: string }[]).map(({ key, icon: Icon, label }) => (
          <button
            key={key}
            onClick={() => {
              if (key === 'bases') switchTab('bases')
              else if (key === 'docs' && baseIdFromUrl) switchTab('docs', { baseId: baseIdFromUrl })
              else if (key === 'chunks' && docIdFromUrl) switchTab('chunks', { baseId: baseIdFromUrl, docId: docIdFromUrl })
            }}
            disabled={(key === 'docs' && !baseIdFromUrl) || (key === 'chunks' && !docIdFromUrl)}
            className={cn(
              'flex items-center gap-1.5 px-4 py-2.5 text-sm font-medium border-b-2 -mb-px transition-colors',
              activeTab === key
                ? 'border-indigo-500 text-indigo-600'
                : 'border-transparent text-gray-500 hover:text-gray-700 disabled:opacity-40 disabled:cursor-default',
            )}
          >
            <Icon className="w-4 h-4" />
            {label}
            {key === 'docs' && baseIdFromUrl && currentBase && (
              <span className="ml-1 text-xs text-gray-400 font-normal truncate max-w-[80px]">{currentBase.name}</span>
            )}
            {key === 'chunks' && docIdFromUrl && currentDoc && (
              <span className="ml-1 text-xs text-gray-400 font-normal truncate max-w-[80px]">{currentDoc.name}</span>
            )}
          </button>
        ))}
      </div>

      {/* ── Tab: 知识库列表 ──────────────────────────────────────────────────── */}
      {activeTab === 'bases' && (
        <>
          {/* 统计卡片 */}
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-4 flex-shrink-0">
            <StatCard label="知识库总数" value={bases.length} Icon={Database} tone="gray" />
            <StatCard label="文档总数" value={totalDocs} Icon={FileText} tone="blue" />
            <StatCard label="分块总数" value={totalChunks} Icon={Layers} tone="indigo" />
            <StatCard label="索引队列" value={queueLen} Icon={Loader2} tone={queueLen > 0 ? 'amber' : 'gray'} />
          </div>

          {/* 搜索 + 批量操作 */}
          <div className="flex items-center gap-3 flex-shrink-0">
            <div className="relative">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400" />
              <input
                type="text"
                placeholder="搜索知识库…"
                value={baseSearch}
                onChange={e => { setBaseSearch(e.target.value); setBasePage(1) }}
                className="input pl-9 w-52"
              />
            </div>
            {baseSelected.size > 0 && (
              <>
                <span className="text-sm text-slate-700 font-medium">已选 {baseSelected.size} 个</span>
                <button
                  onClick={() => setConfirmDeleteBase({ open: true, isBatch: true })}
                  className="px-3 py-1.5 rounded-lg bg-red-100 text-red-700 hover:bg-red-200 text-xs font-medium transition-colors"
                >
                  批量删除
                </button>
                <button
                  onClick={() => setBaseSelected(new Set())}
                  className="text-xs text-gray-500 hover:text-gray-700 px-2 py-1 rounded hover:bg-gray-100 transition-colors"
                >
                  取消选择
                </button>
              </>
            )}
          </div>

          {/* 知识库表格 */}
          <div className="card flex flex-col">
            {basesLoading ? (
              <div className="flex items-center justify-center py-12">
                <Loader2 className="w-8 h-8 animate-spin text-gray-400" />
              </div>
            ) : (
              <div className="overflow-x-auto">
                <table className="table w-full" style={{ tableLayout: 'fixed', minWidth: '720px' }}>
                  <colgroup>
                    <col style={{ width: '44px' }} />
                    <col style={{ width: '22%' }} />
                    <col style={{ width: '22%' }} />
                    <col style={{ width: '80px' }} />
                    <col style={{ width: '80px' }} />
                    <col style={{ width: '140px' }} />
                    <col style={{ width: '140px' }} />
                    <col style={{ width: '160px' }} />
                  </colgroup>
                  <thead>
                    <tr>
                      <th className="pl-4 w-11">
                        <input type="checkbox" className="rounded border-gray-300"
                          checked={basePageAllSelected}
                          ref={el => { if (el) el.indeterminate = basePagePartialSelected }}
                          onChange={toggleBaseAll}
                        />
                      </th>
                      <th className="text-left px-3">名称</th>
                      <th className="text-left px-3">描述</th>
                      <th className="text-left px-2">文档数</th>
                      <th className="text-left px-2">分块数</th>
                      <th className="text-left px-3">创建时间</th>
                      <th className="text-left px-3">修改时间</th>
                      <th className="text-left pl-2 pr-4">操作</th>
                    </tr>
                  </thead>
                  <tbody>
                    {paginatedBases.length === 0 ? (
                      <tr>
                        <td colSpan={8}>
                          <div className="py-20 flex flex-col items-center text-gray-400">
                            <Inbox className="w-16 h-16 mb-4 text-gray-200" />
                            <p className="text-base font-medium text-gray-500 mb-1">暂无知识库</p>
                            <p className="text-sm text-gray-400">点击「新建知识库」创建第一个</p>
                          </div>
                        </td>
                      </tr>
                    ) : paginatedBases.map(base => (
                      <tr
                        key={base.id}
                        className={cn('cursor-pointer group', baseSelected.has(base.id) && 'bg-blue-50/60')}
                        onClick={() => switchTab('docs', { baseId: base.id })}
                      >
                        <td className="pl-4 py-3.5" onClick={e => e.stopPropagation()}>
                          {base.id !== 'default' ? (
                            <input type="checkbox" className="rounded border-gray-300"
                              checked={baseSelected.has(base.id)}
                              onChange={() => toggleBaseSelect(base.id)}
                            />
                          ) : <span className="inline-block w-4 h-4" />}
                        </td>
                        <td className="py-3.5 px-3 min-w-0">
                          <div className="flex items-center gap-2 min-w-0">
                            <div className="w-7 h-7 rounded-lg bg-indigo-50 flex items-center justify-center flex-shrink-0">
                              <Database className="w-3.5 h-3.5 text-indigo-500" />
                            </div>
                            <span className="text-sm font-medium text-gray-900 truncate">{base.name}</span>
                            {base.id === 'default' && <span className="tag tag-default flex-shrink-0 whitespace-nowrap">默认</span>}
                          </div>
                        </td>
                        <td className="py-3.5 px-3 text-gray-500 min-w-0">
                          <span className="truncate block text-sm">{base.description || ''}</span>
                        </td>
                        <td className="py-3.5 px-2">
                          <span className="tag bg-blue-50 text-blue-700"><FileText className="w-3 h-3 mr-1" />{base.doc_count}</span>
                        </td>
                        <td className="py-3.5 px-2">
                          <span className="tag bg-indigo-50 text-indigo-700"><Layers className="w-3 h-3 mr-1" />{base.chunk_count}</span>
                        </td>
                        <td className="py-3.5 px-3 text-gray-500 text-xs whitespace-nowrap">{base.created_at}</td>
                        <td className="py-3.5 px-3 text-gray-500 text-xs whitespace-nowrap">{base.updated_at}</td>
                        <td className="py-3.5 pl-2 pr-4" onClick={e => e.stopPropagation()}>
                          <div className="flex items-center gap-1.5">
                            <button
                              onClick={() => switchTab('docs', { baseId: base.id })}
                              className="inline-flex items-center gap-1 px-2.5 h-7 rounded-md border border-indigo-200 bg-indigo-50 text-indigo-700 hover:bg-indigo-100 text-xs font-medium whitespace-nowrap transition-all"
                            >
                              <ArrowRight className="w-3 h-3" />
                              管理文档
                            </button>
                            {base.id !== 'default' && (
                              <button
                                onClick={() => setConfirmDeleteBase({ open: true, base })}
                                className="inline-flex items-center gap-1 px-2.5 h-7 rounded-md border border-red-200 bg-red-50 text-red-600 hover:bg-red-100 text-xs font-medium whitespace-nowrap transition-all"
                              >
                                <Trash2 className="w-3 h-3" />
                                删除
                              </button>
                            )}
                          </div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
            {!basesLoading && filteredBases.length > 0 && (
              <Pagination page={basePage} totalPages={baseTotalPages} total={filteredBases.length}
                pageSize={basePageSize} onChange={setBasePage}
                onPageSizeChange={size => { setBasePageSize(size); setBasePage(1) }}
              />
            )}
          </div>
        </>
      )}

      {/* ── Tab: 文档管理 ────────────────────────────────────────────────────── */}
      {activeTab === 'docs' && (
        <>
          {/* 统计卡片 */}
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-4 flex-shrink-0">
            <StatCard label="文档总数" value={docStats.total} Icon={FileText} tone="gray" />
            <StatCard label="索引中" value={docStats.indexing} Icon={Loader2} tone="blue" />
            <StatCard label="已完成" value={docStats.completed} Icon={CheckCircle2} tone="emerald" />
            <StatCard label="失败" value={docStats.failed} Icon={XCircle} tone={docStats.failed > 0 ? 'red' : 'gray'} />
          </div>

          {/* 过滤栏 + 批量操作 */}
          <div className="flex flex-wrap items-center gap-3 flex-shrink-0">
            <div className="relative">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400" />
              <input
                type="text" placeholder="搜索文档…"
                value={docSearchInput}
                onChange={e => setDocSearchInput(e.target.value)}
                onKeyDown={e => e.key === 'Enter' && (setDocSearch(docSearchInput), setDocPage(1))}
                className="input pl-9 w-48"
              />
              {docSearchInput && (
                <button onClick={() => { setDocSearchInput(''); setDocSearch(''); setDocPage(1) }}
                  className="absolute right-2 top-1/2 -translate-y-1/2 px-1.5 py-0.5 rounded text-xs text-gray-400 hover:text-gray-600 hover:bg-gray-100">
                  清除
                </button>
              )}
            </div>
            <CustomSelect value={statusFilter} onChange={v => { setStatusFilter(v); setDocPage(1) }}
              className={cn(statusFilter && '!border-indigo-400 !text-indigo-700')}
              options={STATUS_OPTIONS as SelectOption[]}
            />
            <CustomSelect value={fileTypeFilter} onChange={v => { setFileTypeFilter(v); setDocPage(1) }}
              className={cn(fileTypeFilter && '!border-indigo-400 !text-indigo-700')}
              options={FILE_TYPE_OPTIONS as SelectOption[]}
            />
            {hasActiveDocFilters && (
              <button onClick={() => { setDocSearch(''); setDocSearchInput(''); setStatusFilter(''); setFileTypeFilter(''); setDocPage(1) }}
                className="inline-flex items-center gap-1 text-xs text-indigo-500 hover:text-indigo-700 bg-indigo-50 hover:bg-indigo-100 px-2 py-1 rounded-md transition-colors font-medium">
                <X className="w-3 h-3" />
                清除过滤
              </button>
            )}
            {docSelected.size > 0 && (
              <div className="flex items-center gap-2 ml-auto">
                <span className="text-sm text-slate-700 font-medium">已选 {docSelected.size} 个</span>
                <button onClick={() => setRebuildTarget({ docIds: [...docSelected] })}
                  className="px-3 py-1.5 rounded-lg bg-blue-100 text-blue-700 hover:bg-blue-200 text-xs font-medium transition-colors">
                  批量重建
                </button>
                <button onClick={() => setConfirmDeleteDoc({ open: true, isBatch: true })}
                  className="px-3 py-1.5 rounded-lg bg-red-100 text-red-700 hover:bg-red-200 text-xs font-medium transition-colors">
                  批量删除
                </button>
                <button onClick={() => setDocSelected(new Set())} className="text-xs text-gray-500 hover:text-gray-700 underline">
                  取消选择
                </button>
              </div>
            )}
          </div>

          {/* 文档表格 */}
          <div className="card flex flex-col">
            {docsLoading ? (
              <div className="flex items-center justify-center py-12">
                <Loader2 className="w-8 h-8 animate-spin text-gray-400" />
              </div>
            ) : (
              <div className="overflow-x-auto">
                <table className="table w-full" style={{ tableLayout: 'fixed', minWidth: '1280px' }}>
                  <colgroup>
                    <col style={{ width: '44px' }} />
                    <col style={{ width: '170px' }} />
                    <col style={{ width: '64px' }} />
                    <col style={{ width: '80px' }} />
                    <col style={{ width: '120px' }} />
                    <col style={{ width: '80px' }} />
                    <col style={{ width: '80px' }} />
                    <col style={{ width: '80px' }} />
                    <col style={{ width: '100px' }} />
                    <col style={{ width: '72px' }} />
                    <col style={{ width: '148px' }} />
                    <col style={{ width: '250px' }} />
                  </colgroup>
                  <thead>
                    <tr>
                      <th className="pl-4 w-11">
                        <input type="checkbox" className="rounded border-gray-300"
                          checked={docPageAllSelected}
                          ref={el => { if (el) el.indeterminate = docPagePartialSelected }}
                          onChange={toggleDocAll}
                        />
                      </th>
                      <th className="text-left px-3">文档名</th>
                      <th className="text-left px-2">类型</th>
                      <th className="text-left px-2">大小</th>
                      <th className="text-left px-2">状态/进度</th>
                      <th className="text-left px-2">分块数</th>
                      <th className="text-left px-2">耗时</th>
                      <th className="text-left px-3">来源</th>
                      <th className="text-left px-2">处理模式</th>
                      <th className="text-left px-2">启用</th>
                      <th className="text-left px-3">更新时间</th>
                      <th className="text-left pr-4">操作</th>
                    </tr>
                  </thead>
                  <tbody>
                    {docs.length === 0 ? (
                      <tr>
                        <td colSpan={12}>
                          <div className="py-20 flex flex-col items-center text-gray-400">
                            <Inbox className="w-16 h-16 mb-4 text-gray-200" />
                            <p className="text-base font-medium text-gray-500 mb-1">
                              {hasActiveDocFilters ? '没有匹配的文档' : '暂无文档'}
                            </p>
                            <p className="text-sm text-gray-400">点击「上传文档」添加知识文档</p>
                          </div>
                        </td>
                      </tr>
                    ) : docs.map(doc => {
                      const sc = STATUS_CONFIG[doc.index_status] ?? STATUS_CONFIG.pending
                      const StatusIcon = sc.Icon
                      return (
                        <tr key={doc.id} className={cn('group', docSelected.has(doc.id) && 'bg-blue-50/60')}>
                          <td className="pl-4 py-3">
                            <input type="checkbox" className="rounded border-gray-300"
                              checked={docSelected.has(doc.id)} onChange={() => toggleDocSelect(doc.id)} />
                          </td>
                          <td className="py-3 px-3 min-w-0">
                            <div className="flex items-center gap-2 min-w-0">
                              <FileText className="w-4 h-4 text-gray-400 flex-shrink-0" />
                              <span className="text-sm font-medium text-gray-900 truncate" title={doc.name}>{doc.name}</span>
                            </div>
                            {doc.index_error && (
                              <p className="text-xs text-red-500 mt-0.5 pl-6 truncate" title={doc.index_error}>{doc.index_error}</p>
                            )}
                            <IndexProgress doc={doc} />
                          </td>
                          <td className="py-3 px-2">
                            <span className="tag tag-default font-mono uppercase">{doc.file_type}</span>
                          </td>
                          <td className="py-3 px-2 text-gray-500 text-sm">{formatSize(doc.file_size)}</td>
                          <td className="py-3 px-2">
                            <span className={cn('tag border', sc.cls)}>
                              <StatusIcon className={cn('w-3 h-3 mr-1', doc.index_status === 'indexing' && 'animate-spin')} />
                              {sc.label}
                            </span>
                          </td>
                          <td className="py-3 px-2">
                            {doc.index_status === 'completed' ? (
                              <span className="tag bg-indigo-50 text-indigo-700"><Layers className="w-3 h-3 mr-1" />{doc.chunk_count}</span>
                            ) : '—'}
                          </td>
                          <td className="py-3 px-2">
                            {doc.index_status === 'completed' && formatDuration(doc.index_duration_ms) ? (
                              <span className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded-md bg-emerald-50 border border-emerald-100 text-emerald-700 text-xs font-medium">
                                <Timer className="w-3 h-3" />{formatDuration(doc.index_duration_ms)}
                              </span>
                            ) : <span className="text-gray-300 text-xs">—</span>}
                          </td>
                          <td className="py-3 px-3 text-sm text-gray-500 whitespace-nowrap">
                            <span className="tag bg-gray-50 text-gray-600">本地上传</span>
                          </td>
                          <td className="py-3 px-2 text-sm text-gray-700 truncate">
                            {STRATEGY_LABEL[doc.chunk_strategy] ?? doc.chunk_strategy ?? '—'}
                          </td>
                          <td className="py-3 px-2">
                            <button type="button" onClick={() => handleToggleDocEnabled(doc)}
                              className={cn('inline-flex h-5 w-9 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 focus:outline-none',
                                doc.enabled ? 'bg-emerald-500' : 'bg-gray-200')}
                              title={doc.enabled ? '点击禁用' : '点击启用'}>
                              <span className={cn('inline-block h-4 w-4 transform rounded-full bg-white shadow ring-0 transition duration-200',
                                doc.enabled ? 'translate-x-4' : 'translate-x-0')} />
                            </button>
                          </td>
                          <td className="py-3 px-3 text-gray-500 text-xs whitespace-nowrap">
                            {doc.indexed_at ?? doc.created_at}
                          </td>
                          <td className="py-3 pr-4">
                            <div className="flex items-center gap-1.5">
                              {doc.index_status === 'completed' && (
                                <button
                                  onClick={() => switchTab('chunks', { baseId: baseIdFromUrl, docId: doc.id })}
                                  className="inline-flex items-center gap-1 px-2.5 h-7 rounded-md border border-indigo-200 bg-indigo-50 text-indigo-700 hover:bg-indigo-100 text-xs font-medium whitespace-nowrap transition-all"
                                >
                                  <Layers className="w-3 h-3" />
                                  查看分块
                                </button>
                              )}
                              <button
                                onClick={() => setRebuildTarget({ docId: doc.id, currentStrategy: doc.chunk_strategy })}
                                className="inline-flex items-center gap-1 px-2.5 h-7 rounded-md border border-amber-200 bg-amber-50 text-amber-700 hover:bg-amber-100 text-xs font-medium whitespace-nowrap transition-all"
                              >
                                <RotateCcw className="w-3 h-3" />
                                重建
                              </button>
                              <button
                                onClick={() => setConfirmDeleteDoc({ open: true, doc })}
                                className="inline-flex items-center gap-1 px-2.5 h-7 rounded-md border border-red-200 bg-red-50 text-red-600 hover:bg-red-100 text-xs font-medium whitespace-nowrap transition-all"
                              >
                                <Trash2 className="w-3 h-3" />
                                删除
                              </button>
                            </div>
                          </td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              </div>
            )}
            {!docsLoading && docsTotal > 0 && (
              <Pagination page={docPage} totalPages={docTotalPages} total={docsTotal}
                pageSize={docPageSize} onChange={setDocPage}
                onPageSizeChange={size => { setDocPageSize(size); setDocPage(1) }}
              />
            )}
          </div>
        </>
      )}

      {/* ── Tab: 分块管理 ────────────────────────────────────────────────────── */}
      {activeTab === 'chunks' && (
        <>
          {/* 统计卡片 */}
          <div className="grid grid-cols-2 sm:grid-cols-3 gap-4 flex-shrink-0">
            <StatCard label="分块总数" value={chunksTotal} Icon={Layers} tone="gray" />
            <StatCard label="平均字符数" value={avgChars} Icon={AlignLeft} tone="blue" />
            <StatCard label="章节数" value={sectionCount} Icon={Hash} tone="indigo" />
          </div>

          {/* 过滤 + 批量操作 */}
          <div className="flex flex-wrap items-center gap-3 flex-shrink-0">
            <div className="relative">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400" />
              <input
                type="text" placeholder="搜索分块内容…"
                value={chunkKeywordInput}
                onChange={e => setChunkKeywordInput(e.target.value)}
                onKeyDown={e => e.key === 'Enter' && (setChunkKeyword(chunkKeywordInput), setChunkPage(1))}
                className="input pl-9 w-44"
              />
              {chunkKeywordInput && (
                <button onClick={() => { setChunkKeywordInput(''); setChunkKeyword(''); setChunkPage(1) }}
                  className="absolute right-2 top-1/2 -translate-y-1/2 px-1.5 py-0.5 rounded text-xs text-gray-400 hover:text-gray-600 hover:bg-gray-100">
                  清除
                </button>
              )}
            </div>
            {chunkSelected.size > 0 && (
              <div className="flex items-center gap-2 ml-auto">
                <span className="text-sm text-slate-700 font-medium">已选 {chunkSelected.size} 个</span>
                <button onClick={() => handleBatchEnableChunk(true)}
                  className="px-3 py-1.5 rounded-lg bg-emerald-500 text-white hover:bg-emerald-600 text-xs font-medium transition-colors">
                  批量启用
                </button>
                <button onClick={() => handleBatchEnableChunk(false)}
                  className="px-3 py-1.5 rounded-lg bg-gray-100 text-gray-600 hover:bg-gray-200 text-xs font-medium transition-colors">
                  批量禁用
                </button>
                <button onClick={() => setChunkSelected(new Set())} className="text-xs text-gray-500 hover:text-gray-700 underline">
                  取消选择
                </button>
              </div>
            )}
            <div className={cn('flex items-center gap-2', chunkSelected.size === 0 && 'ml-auto')}>
              <button onClick={() => handleAllEnableChunk(true)}
                className="inline-flex items-center gap-1.5 px-3 h-9 rounded-lg bg-gray-800 text-white hover:bg-gray-700 text-sm font-medium whitespace-nowrap transition-colors">
                全量启用
              </button>
              <button onClick={() => handleAllEnableChunk(false)}
                className="inline-flex items-center gap-1.5 px-3 h-9 rounded-lg border border-gray-200 bg-white text-gray-600 hover:bg-gray-50 text-sm font-medium whitespace-nowrap transition-colors">
                全量禁用
              </button>
            </div>
          </div>

          {/* 分块表格 */}
          <div className="card flex flex-col">
            {chunksLoading ? (
              <div className="flex items-center justify-center py-12">
                <Loader2 className="w-8 h-8 animate-spin text-gray-400" />
              </div>
            ) : (
              <div className="overflow-x-auto">
                <table className="table w-full" style={{ tableLayout: 'fixed', minWidth: '960px' }}>
                  <colgroup>
                    <col style={{ width: '40px' }} />
                    <col style={{ width: '48px' }} />
                    <col style={{ width: '340px' }} />
                    <col style={{ width: '160px' }} />
                    <col style={{ width: '72px' }} />
                    <col style={{ width: '64px' }} />
                    <col style={{ width: '140px' }} />
                    <col style={{ width: '72px' }} />
                  </colgroup>
                  <thead>
                    <tr>
                      <th className="w-10 pl-6">
                        <input type="checkbox" className="rounded border-gray-300"
                          checked={chunkPageAllSelected}
                          ref={el => { if (el) el.indeterminate = chunkPagePartialSelected }}
                          onChange={toggleChunkAll}
                        />
                      </th>
                      <th className="w-14 pl-2 text-left">#</th>
                      <th className="min-w-0">内容预览</th>
                      <th className="text-left px-4">章节标题</th>
                      <th className="text-left px-4">字符数</th>
                      <th className="text-left px-4">启用</th>
                      <th className="text-left px-4">更新时间</th>
                      <th className="text-left pr-4">操作</th>
                    </tr>
                  </thead>
                  <tbody>
                    {chunks.length === 0 ? (
                      <tr>
                        <td colSpan={8}>
                          <div className="py-20 flex flex-col items-center text-gray-400">
                            <Inbox className="w-16 h-16 mb-4 text-gray-200" />
                            <p className="text-base font-medium text-gray-500 mb-1">
                              {chunkKeyword ? '没有匹配的分块' : '暂无分块数据'}
                            </p>
                          </div>
                        </td>
                      </tr>
                    ) : chunks.map(chunk => {
                      const isExpanded = expanded.has(chunk.id)
                      return (
                        <React.Fragment key={chunk.id}>
                          <tr className={cn('group', chunkSelected.has(chunk.id) && 'bg-blue-50/60')}>
                            <td className="pl-6 py-4">
                              <input type="checkbox" className="rounded border-gray-300"
                                checked={chunkSelected.has(chunk.id)} onChange={() => toggleChunkSelect(chunk.id)} />
                            </td>
                            <td className="pl-2 py-4 text-gray-400 font-mono text-xs">{chunk.chunk_index + 1}</td>
                            <td className="py-4 min-w-0 overflow-hidden">
                              <p className={cn('text-sm text-gray-700 leading-relaxed pr-4', !isExpanded && 'line-clamp-2')}>
                                {chunk.content_preview}
                              </p>
                            </td>
                            <td className="py-4 px-4">
                              {chunk.section_title ? (
                                <span className="tag bg-indigo-50 text-indigo-700 max-w-[160px] truncate block" title={chunk.section_title}>
                                  {chunk.section_title}
                                </span>
                              ) : <span className="text-gray-400 text-sm">—</span>}
                            </td>
                            <td className="py-4 px-4 text-gray-500 text-sm tabular-nums">{chunk.char_count}</td>
                            <td className="py-4 px-4">
                              <button type="button" onClick={() => handleToggleChunk(chunk)}
                                className={cn('inline-flex h-5 w-9 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 focus:outline-none',
                                  chunk.enabled ? 'bg-emerald-500' : 'bg-gray-200')}
                                title={chunk.enabled ? '点击禁用' : '点击启用'}>
                                <span className={cn('inline-block h-4 w-4 transform rounded-full bg-white shadow ring-0 transition duration-200',
                                  chunk.enabled ? 'translate-x-4' : 'translate-x-0')} />
                              </button>
                            </td>
                            <td className="py-4 px-4 text-gray-500 text-xs tabular-nums">{chunk.updated_at}</td>
                            <td className="pr-4 py-4">
                              <button
                                onClick={() => setExpanded(prev => { const next = new Set(prev); next.has(chunk.id) ? next.delete(chunk.id) : next.add(chunk.id); return next })}
                                className={cn('inline-flex items-center gap-1 px-2.5 h-7 rounded-md border text-xs font-medium whitespace-nowrap transition-all',
                                  isExpanded ? 'border-gray-300 bg-gray-100 text-gray-700 hover:bg-gray-200' : 'border-gray-200 bg-gray-50 text-gray-600 hover:bg-gray-100')}
                              >
                                {isExpanded ? <ChevronUp className="w-3 h-3" /> : <ChevronDown className="w-3 h-3" />}
                                {isExpanded ? '收起' : '展开'}
                              </button>
                            </td>
                          </tr>
                          {isExpanded && (
                            <tr className="bg-gray-50/60">
                              <td colSpan={8} className="px-8 py-4">
                                <div className="card p-4">
                                  <p className="descriptions-label mb-2">完整内容</p>
                                  <p className="text-sm text-gray-700 leading-relaxed whitespace-pre-wrap">{chunk.content_preview}</p>
                                </div>
                              </td>
                            </tr>
                          )}
                        </React.Fragment>
                      )
                    })}
                  </tbody>
                </table>
              </div>
            )}
            {!chunksLoading && chunksTotal > 0 && (
              <Pagination page={chunkPage} totalPages={chunkTotalPages} total={chunksTotal}
                pageSize={chunkPageSize} onChange={setChunkPage}
                onPageSizeChange={size => { setChunkPageSize(size); setChunkPage(1) }}
              />
            )}
          </div>
        </>
      )}

      {/* ── 弹窗 ──────────────────────────────────────────────────────────────── */}
      {showCreate && (
        <BaseCreateModal
          onClose={() => setShowCreate(false)}
          onSuccess={base => { setBases(prev => [base, ...prev]); setShowCreate(false) }}
        />
      )}

      {showUpload && baseIdFromUrl && (
        <DocUploadModal
          baseID={baseIdFromUrl}
          baseName={currentBase?.name ?? ''}
          onClose={() => setShowUpload(false)}
          onSuccess={() => { setShowUpload(false); setTimeout(fetchDocs, 500) }}
        />
      )}

      {rebuildTarget && (
        <RebuildModal
          docId={rebuildTarget.docId}
          docIds={rebuildTarget.docIds}
          currentStrategy={rebuildTarget.currentStrategy}
          onClose={() => setRebuildTarget(null)}
          onSuccess={() => {
            setRebuildTarget(null)
            setDocSelected(new Set())
            setTimeout(fetchDocs, 500)
          }}
        />
      )}

      {showSearch && baseIdFromUrl && (
        <SearchModal baseID={baseIdFromUrl} onClose={() => setShowSearch(false)} />
      )}

      <ConfirmDialog
        open={confirmDeleteBase.open}
        title={confirmDeleteBase.isBatch ? `删除 ${[...baseSelected].filter(id => id !== 'default').length} 个知识库` : '删除知识库'}
        description={
          confirmDeleteBase.isBatch
            ? `确定要删除选中的 ${[...baseSelected].filter(id => id !== 'default').length} 个知识库及其所有文档和向量吗？此操作无法撤销。`
            : `确定要删除知识库「${confirmDeleteBase.base?.name}」吗？其下所有文档和向量将一并删除，此操作无法撤销。`
        }
        onConfirm={() => {
          if (confirmDeleteBase.isBatch) handleBatchDeleteBase()
          else if (confirmDeleteBase.base) handleDeleteBase(confirmDeleteBase.base)
        }}
        onClose={() => setConfirmDeleteBase({ open: false })}
      />

      <ConfirmDialog
        open={confirmDeleteDoc.open}
        title={confirmDeleteDoc.isBatch ? `删除 ${docSelected.size} 个文档` : '删除文档'}
        description={
          confirmDeleteDoc.isBatch
            ? `确定要删除选中的 ${docSelected.size} 个文档及其向量吗？此操作无法撤销。`
            : `确定要删除文档「${confirmDeleteDoc.doc?.name}」吗？其向量数据将一并删除，此操作无法撤销。`
        }
        onConfirm={() => {
          if (confirmDeleteDoc.isBatch) handleBatchDeleteDoc()
          else if (confirmDeleteDoc.doc) handleDeleteDoc(confirmDeleteDoc.doc)
        }}
        onClose={() => setConfirmDeleteDoc({ open: false })}
      />
    </div>
  )
}
