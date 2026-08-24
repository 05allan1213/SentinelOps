import { useState, useEffect, useCallback, useRef } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import {
  Upload, Trash2, RefreshCw, Loader2, FileText, Layers,
  CheckCircle2, XCircle, Clock, Search,
  Inbox, RotateCcw, ChevronRight, Filter, Timer,
} from 'lucide-react'
import { cn } from '@/utils'
import { knowledgeService, type KnowledgeBase, type DocItem } from '@/services/knowledge'
import StatCard from '@/components/common/StatCard'
import Pagination from '@/components/common/Pagination'
import DocUploadModal from './components/DocUploadModal'
import RebuildModal from './components/RebuildModal'
import ConfirmDialog from '@/components/common/ConfirmDialog'
import CustomSelect from '@/components/common/CustomSelect'
import toast from 'react-hot-toast'

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
  { value: 'docx', label: 'DOCX' },
  { value: 'go',   label: 'Go' },
  { value: 'py',   label: 'Python' },
  { value: 'java', label: 'Java' },
]

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

/** 索引进度条：索引中状态展示 indexed_chunks / chunk_count */
function IndexProgress({ doc }: { doc: DocItem }) {
  if (doc.index_status !== 'indexing' && doc.index_status !== 'pending') return null
  if (doc.chunk_count === 0) return <span className="text-xs text-gray-400">准备中…</span>
  const pct = Math.min(Math.round((doc.indexed_chunks / doc.chunk_count) * 100), 100)
  return (
    <div className="flex items-center gap-1.5 mt-0.5 pl-6">
      <div className="flex-1 h-1 rounded-full bg-gray-200 overflow-hidden" style={{ maxWidth: 80 }}>
        <div
          className="h-full rounded-full bg-blue-500 transition-all duration-500"
          style={{ width: `${pct}%` }}
        />
      </div>
      <span className="text-xs text-blue-600 tabular-nums">{pct}%</span>
    </div>
  )
}

export default function KnowledgeDocs() {
  const { baseId } = useParams<{ baseId: string }>()
  const navigate = useNavigate()

  const [base, setBase] = useState<KnowledgeBase | null>(null)
  const [docs, setDocs] = useState<DocItem[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)

  // 过滤条件（服务端过滤）
  const [search, setSearch]       = useState('')
  const [searchInput, setSearchInput] = useState('')  // 未提交的输入
  const [statusFilter, setStatusFilter]   = useState('')
  const [fileTypeFilter, setFileTypeFilter] = useState('')

  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [showUpload, setShowUpload] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState<{ open: boolean; doc?: DocItem; isBatch?: boolean }>({ open: false })
  const [rebuildTarget, setRebuildTarget] = useState<{ docId?: string; docIds?: string[]; currentStrategy?: string } | null>(null)
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const fetchDocs = useCallback(async () => {
    if (!baseId) return
    try {
      setLoading(true)
      const [baseInfo, { list, total }] = await Promise.all([
        knowledgeService.getBase(baseId),
        knowledgeService.listDoc(baseId, {
          page,
          pageSize,
          keyword:  search     || undefined,
          status:   statusFilter   || undefined,
          fileType: fileTypeFilter || undefined,
        }),
      ])
      setBase(baseInfo)
      setDocs(list)
      setTotal(total)
    } catch {
      toast.error('获取文档列表失败')
    } finally {
      setLoading(false)
    }
  }, [baseId, page, pageSize, search, statusFilter, fileTypeFilter])

  useEffect(() => { fetchDocs() }, [fetchDocs])

  // 有进行中的索引时，每 3 秒自动轮询
  useEffect(() => {
    const hasActive = docs.some(d => d.index_status === 'pending' || d.index_status === 'indexing')
    if (hasActive) {
      pollRef.current = setInterval(fetchDocs, 3000)
    } else {
      if (pollRef.current) { clearInterval(pollRef.current); pollRef.current = null }
    }
    return () => { if (pollRef.current) clearInterval(pollRef.current) }
  }, [docs, fetchDocs])

  // 过滤条件变更时重置到第一页
  const handleSearchSubmit = () => {
    setSearch(searchInput)
    setPage(1)
  }

  const handleStatusChange = (v: string) => { setStatusFilter(v); setPage(1) }
  const handleFileTypeChange = (v: string) => { setFileTypeFilter(v); setPage(1) }

  const totalPages = Math.ceil(total / pageSize)

  const stats = {
    total,
    indexing:  docs.filter(d => d.index_status === 'indexing' || d.index_status === 'pending').length,
    completed: docs.filter(d => d.index_status === 'completed').length,
    failed:    docs.filter(d => d.index_status === 'failed').length,
  }

  const pageAllSelected = docs.length > 0 && docs.every(d => selected.has(d.id))
  const pagePartialSelected = !pageAllSelected && docs.some(d => selected.has(d.id))

  const toggleSelect = (id: string) => {
    setSelected(prev => { const next = new Set(prev); next.has(id) ? next.delete(id) : next.add(id); return next })
  }

  const toggleAll = () => {
    setSelected(prev => {
      const next = new Set(prev)
      if (pageAllSelected) {
        docs.forEach(d => next.delete(d.id))
      } else {
        docs.forEach(d => next.add(d.id))
      }
      return next
    })
  }

  const handleDeleteDoc = async (doc: DocItem) => {
    try {
      await knowledgeService.deleteDoc(doc.id)
      toast.success(`文档「${doc.name}」已删除`)
      setDocs(prev => prev.filter(d => d.id !== doc.id))
      setSelected(prev => { const next = new Set(prev); next.delete(doc.id); return next })
      setTotal(t => t - 1)
    } catch {
      toast.error('删除失败')
    }
  }

  const handleBatchDelete = async () => {
    const ids = [...selected]
    try {
      const result = await knowledgeService.batchDeleteDocs(ids)
      toast.success(`已删除 ${result.deleted} 个文档${result.failed > 0 ? `，${result.failed} 个失败` : ''}`)
      setDocs(prev => prev.filter(d => !ids.includes(d.id)))
      setTotal(t => t - result.deleted)
      setSelected(new Set())
    } catch {
      toast.error('批量删除失败')
    }
  }

  const handleBatchRebuild = () => {
    setRebuildTarget({ docIds: [...selected] })
  }

  const handleRebuildDoc = (doc: DocItem) => {
    setRebuildTarget({ docId: doc.id, currentStrategy: doc.chunk_strategy })
  }

  const handleToggleEnabled = async (doc: DocItem) => {
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

  const hasActiveFilters = search || statusFilter || fileTypeFilter

  return (
    <div className="flex flex-col gap-5">

      {/* 面包屑 */}
      <div className="flex items-center gap-1.5 text-sm text-gray-500 flex-shrink-0">
        <button onClick={() => navigate('/knowledge')} className="breadcrumb-item">知识库</button>
        <ChevronRight className="w-3.5 h-3.5 breadcrumb-separator" />
        <span className="breadcrumb-item active">{base?.name ?? '…'}</span>
      </div>

      {/* 标题栏 */}
      <div className="flex items-center justify-between flex-shrink-0">
        <div>
          <h1 className="text-2xl font-semibold text-gray-900 tracking-tight">{base?.name ?? '文档管理'}</h1>
          <p className="text-sm text-gray-500 mt-1">共 {total} 个文档</p>
        </div>
        <div className="flex items-center gap-2 flex-wrap">
          {/* 搜索框（Enter 触发服务端搜索） */}
          <div className="relative">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400" />
            <input
              type="text"
              placeholder="搜索文档…"
              value={searchInput}
              onChange={e => setSearchInput(e.target.value)}
              onKeyDown={e => e.key === 'Enter' && handleSearchSubmit()}
              className="input pl-9 w-48"
            />
            {searchInput && (
              <button
                onClick={() => { setSearchInput(''); setSearch(''); setPage(1) }}
                className="absolute right-2 top-1/2 -translate-y-1/2 px-1.5 py-0.5 rounded text-xs text-gray-400 hover:text-gray-600 hover:bg-gray-100"
              >
                清除
              </button>
            )}
          </div>

          {/* 状态过滤 */}
          <CustomSelect
            value={statusFilter}
            onChange={handleStatusChange}
            options={STATUS_OPTIONS}
            className="w-32"
            prefix={<Filter className="w-3.5 h-3.5" />}
          />

          {/* 文件类型过滤 */}
          <CustomSelect
            value={fileTypeFilter}
            onChange={handleFileTypeChange}
            options={FILE_TYPE_OPTIONS}
            className="w-28"
          />

          {/* 清除过滤 */}
          {hasActiveFilters && (
            <button
              onClick={() => { setSearch(''); setSearchInput(''); setStatusFilter(''); setFileTypeFilter(''); setPage(1) }}
              className="text-xs text-gray-500 hover:text-gray-700 underline"
            >
              清除过滤
            </button>
          )}

          <button onClick={fetchDocs} disabled={loading} className="btn-default">
            <RefreshCw className={cn('w-4 h-4', loading && 'animate-spin')} />
            刷新
          </button>
          <button onClick={() => setShowUpload(true)} className="btn btn-primary">
            <Upload className="w-4 h-4" />
            上传文档
          </button>
        </div>
      </div>

      {/* 统计卡片 */}
      <div className="grid grid-cols-2 sm:grid-cols-4 gap-4 flex-shrink-0">
        <StatCard label="文档总数" value={stats.total} Icon={FileText} tone="gray" />
        <StatCard label="索引中" value={stats.indexing} Icon={Loader2} tone="blue" />
        <StatCard label="已完成" value={stats.completed} Icon={CheckCircle2} tone="emerald" />
        <StatCard label="失败" value={stats.failed} Icon={XCircle} tone={stats.failed > 0 ? 'red' : 'gray'} />
      </div>

      {/* 批量操作栏 */}
      {selected.size > 0 && (
        <div className="flex items-center gap-3 border-y border-slate-200 bg-slate-50 px-4 py-2 rounded-lg text-sm flex-shrink-0">
          <span className="text-slate-700 font-medium">已选 {selected.size} 个</span>
          <div className="flex gap-2 ml-2">
            <button
              onClick={handleBatchRebuild}
              className="px-3 py-1.5 rounded-lg bg-blue-100 text-blue-700 hover:bg-blue-200 text-xs font-medium transition-colors"
            >
              批量重建
            </button>
            <button
              onClick={() => setConfirmDelete({ open: true, isBatch: true })}
              className="px-3 py-1.5 rounded-lg bg-red-100 text-red-700 hover:bg-red-200 text-xs font-medium transition-colors"
            >
              批量删除
            </button>
          </div>
          <button onClick={() => setSelected(new Set())} className="ml-auto text-xs text-gray-500 hover:text-gray-700 underline">
            取消选择
          </button>
        </div>
      )}

      {/* 表格卡片 */}
      <div className="card flex flex-col">
        {loading ? (
          <div className="flex items-center justify-center py-12">
            <Loader2 className="w-8 h-8 animate-spin text-gray-400" />
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="table w-full" style={{ tableLayout: 'fixed', minWidth: '1280px' }}>
              <colgroup>
                <col style={{ width: '44px' }} />
                <col style={{ width: '240px' }} />
                <col style={{ width: '64px' }} />
                <col style={{ width: '80px' }} />
                <col style={{ width: '100px' }} />
                <col style={{ width: '80px' }} />
                <col style={{ width: '80px' }} />
                <col style={{ width: '80px' }} />
                <col style={{ width: '80px' }} />
                <col style={{ width: '56px' }} />
                <col style={{ width: '148px' }} />
                <col style={{ width: '216px' }} />
              </colgroup>
              <thead>
                <tr>
                  <th className="pl-4 w-11">
                    <input
                      type="checkbox"
                      className="rounded border-gray-300"
                      checked={pageAllSelected}
                      ref={el => { if (el) el.indeterminate = pagePartialSelected }}
                      onChange={toggleAll}
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
                          {hasActiveFilters ? '没有匹配的文档' : '暂无文档'}
                        </p>
                        <p className="text-sm text-gray-400">点击「上传文档」添加知识文档</p>
                      </div>
                    </td>
                  </tr>
                ) : docs.map(doc => {
                  const sc = STATUS_CONFIG[doc.index_status] ?? STATUS_CONFIG.pending
                  const StatusIcon = sc.Icon
                  return (
                    <tr key={doc.id} className={cn('group', selected.has(doc.id) && 'bg-blue-50/60')}>
                      <td className="pl-4 py-3">
                        <input
                          type="checkbox"
                          className="rounded border-gray-300"
                          checked={selected.has(doc.id)}
                          onChange={() => toggleSelect(doc.id)}
                        />
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
                          <span className="tag bg-indigo-50 text-indigo-700">
                            <Layers className="w-3 h-3 mr-1" />{doc.chunk_count}
                          </span>
                        ) : '—'}
                      </td>
                      <td className="py-3 px-2">
                        {doc.index_status === 'completed' && formatDuration(doc.index_duration_ms)
                          ? (
                            <span className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded-md bg-emerald-50 border border-emerald-100 text-emerald-700 text-xs font-medium">
                              <Timer className="w-3 h-3" />
                              {formatDuration(doc.index_duration_ms)}
                            </span>
                          )
                          : <span className="text-gray-300 text-xs">—</span>}
                      </td>
                      <td className="py-3 px-3 text-sm text-gray-500 whitespace-nowrap">
                        <span className="tag bg-gray-50 text-gray-600">本地上传</span>
                      </td>
                      <td className="py-3 px-2 text-sm text-gray-700 truncate">
                        {STRATEGY_LABEL[doc.chunk_strategy] ?? doc.chunk_strategy ?? '—'}
                      </td>
                      <td className="py-3 px-2">
                        <button
                          type="button"
                          onClick={() => handleToggleEnabled(doc)}
                          className={cn(
                            'inline-flex h-5 w-9 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 focus:outline-none',
                            doc.enabled ? 'bg-emerald-500' : 'bg-gray-200',
                          )}
                          title={doc.enabled ? '点击禁用' : '点击启用'}
                        >
                          <span
                            className={cn(
                              'inline-block h-4 w-4 transform rounded-full bg-white shadow ring-0 transition duration-200',
                              doc.enabled ? 'translate-x-4' : 'translate-x-0',
                            )}
                          />
                        </button>
                      </td>
                      <td className="py-3 px-3 text-gray-500 text-xs whitespace-nowrap">
                        {doc.indexed_at ?? doc.created_at}
                      </td>
                      <td className="py-3 pr-4">
                        <div className="flex items-center gap-1.5">
                          {doc.index_status === 'completed' && (
                            <button
                              onClick={() => navigate(`/knowledge/${baseId}/docs/${doc.id}/chunks`)}
                              className="inline-flex items-center gap-1 px-2.5 h-7 rounded-md border border-indigo-200 bg-indigo-50 text-indigo-700 hover:bg-indigo-100 text-xs font-medium whitespace-nowrap transition-all"
                            >
                              <Layers className="w-3 h-3" />
                              查看分块
                            </button>
                          )}
                          <button
                            onClick={() => handleRebuildDoc(doc)}
                            className="inline-flex items-center gap-1 px-2.5 h-7 rounded-md border border-amber-200 bg-amber-50 text-amber-700 hover:bg-amber-100 text-xs font-medium whitespace-nowrap transition-all"
                          >
                            <RotateCcw className="w-3 h-3" />
                            重建
                          </button>
                          <button
                            onClick={() => setConfirmDelete({ open: true, doc })}
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

        {!loading && total > 0 && (
          <Pagination
            page={page}
            totalPages={totalPages}
            total={total}
            pageSize={pageSize}
            onChange={setPage}
            onPageSizeChange={size => { setPageSize(size); setPage(1) }}
          />
        )}
      </div>

      {showUpload && baseId && (
        <DocUploadModal
          baseID={baseId}
          baseName={base?.name ?? ''}
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
            setSelected(new Set())
            setTimeout(fetchDocs, 500)
          }}
        />
      )}

      <ConfirmDialog
        open={confirmDelete.open}
        title={confirmDelete.isBatch ? `删除 ${selected.size} 个文档` : '删除文档'}
        description={
          confirmDelete.isBatch
            ? `确定要删除选中的 ${selected.size} 个文档及其向量吗？此操作无法撤销。`
            : `确定要删除文档「${confirmDelete.doc?.name}」吗？其向量数据将一并删除，此操作无法撤销。`
        }
        onConfirm={() => {
          if (confirmDelete.isBatch) handleBatchDelete()
          else if (confirmDelete.doc) handleDeleteDoc(confirmDelete.doc)
        }}
        onClose={() => setConfirmDelete({ open: false })}
      />
    </div>
  )
}
