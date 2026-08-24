import { useState, useEffect, useCallback } from 'react'
import {
  FileText,
  Trash2,
  RefreshCw,
  Loader2,
  CheckCircle2,
  Clock,
  AlertCircle,
  Layers,
  ChevronRight,
} from 'lucide-react'
import { cn } from '@/utils'
import { knowledgeService, type DocItem } from '@/services/knowledge'
import ConfirmDialog from '@/components/common/ConfirmDialog'
import ChunkDrawer from './ChunkDrawer'
import toast from 'react-hot-toast'

const STATUS_CONFIG = {
  pending:   { label: '等待索引', icon: Clock,         color: 'text-amber-500',  bg: 'bg-amber-50' },
  indexing:  { label: '索引中',   icon: Loader2,       color: 'text-blue-500',   bg: 'bg-blue-50'  },
  completed: { label: '已完成',   icon: CheckCircle2,  color: 'text-emerald-600', bg: 'bg-emerald-50' },
  failed:    { label: '失败',     icon: AlertCircle,   color: 'text-red-500',    bg: 'bg-red-50'   },
} as const

function StatusBadge({ status }: { status: DocItem['index_status'] }) {
  const cfg = STATUS_CONFIG[status] ?? STATUS_CONFIG.pending
  const Icon = cfg.icon
  return (
    <span className={cn('inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium', cfg.bg, cfg.color)}>
      <Icon className={cn('w-3 h-3', status === 'indexing' && 'animate-spin')} />
      {cfg.label}
    </span>
  )
}

function formatDuration(ms?: number) {
  if (!ms || ms <= 0) return null
  if (ms < 1000) return `${ms}ms`
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`
  const m = Math.floor(ms / 60000)
  const s = Math.round((ms % 60000) / 1000)
  return s > 0 ? `${m}m${s}s` : `${m}m`
}

function formatSize(b: number) {
  if (b < 1024) return `${b} B`
  if (b < 1024 * 1024) return `${(b / 1024).toFixed(1)} KB`
  return `${(b / 1024 / 1024).toFixed(1)} MB`
}

interface Props {
  baseID: string
}

export default function DocList({ baseID }: Props) {
  const [docs, setDocs] = useState<DocItem[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [loading, setLoading] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState<{ open: boolean; id?: string }>({ open: false })
  const [chunkDocID, setChunkDocID] = useState<string | null>(null)
  const [chunkDocName, setChunkDocName] = useState('')

  const PAGE_SIZE = 20

  const fetchDocs = useCallback(async () => {
    if (!baseID) return
    try {
      setLoading(true)
      const res = await knowledgeService.listDoc(baseID, { page, pageSize: PAGE_SIZE })
      setDocs(res.list)
      setTotal(res.total)
    } catch {
      toast.error('获取文档列表失败')
    } finally {
      setLoading(false)
    }
  }, [baseID, page])

  useEffect(() => { fetchDocs() }, [fetchDocs])

  // 有 indexing/pending 的文档时每 3s 轮询
  useEffect(() => {
    const hasActive = docs.some(d => d.index_status === 'indexing' || d.index_status === 'pending')
    if (!hasActive) return
    const t = setInterval(fetchDocs, 3000)
    return () => clearInterval(t)
  }, [docs, fetchDocs])

  const handleDelete = async (id: string) => {
    try {
      await knowledgeService.deleteDoc(id)
      toast.success('文档已删除')
      fetchDocs()
    } catch {
      toast.error('删除失败')
    }
  }

  const handleRebuild = async (id: string) => {
    try {
      await knowledgeService.rebuildDoc(id, '')
      toast.success('已重新提交索引')
      fetchDocs()
    } catch {
      toast.error('操作失败')
    }
  }

  if (loading && docs.length === 0) {
    return (
      <div className="flex items-center justify-center py-12 text-gray-400">
        <Loader2 className="w-5 h-5 animate-spin mr-2" />
        加载中…
      </div>
    )
  }

  if (!loading && docs.length === 0) {
    return (
      <div className="absolute inset-0 flex flex-col items-center justify-center text-gray-400 space-y-3">
        <FileText className="w-12 h-12 text-gray-200" />
        <p className="text-sm text-gray-400">暂无文档，点击上传添加第一个文档</p>
      </div>
    )
  }

  return (
    <>
      <div className="divide-y divide-gray-50">
        {docs.map(doc => (
          <div key={doc.id} className="flex items-center gap-4 px-4 py-3 hover:bg-gray-50/60 transition-colors group">
            {/* 图标 */}
            <div className="w-9 h-9 rounded-lg bg-indigo-50 flex items-center justify-center flex-shrink-0">
              <FileText className="w-4 h-4 text-indigo-500" />
            </div>

            {/* 主信息 */}
            <div className="flex-1 min-w-0">
              <div className="flex items-center gap-2 mb-1">
                <span className="text-sm font-medium text-gray-800 truncate flex-shrink" title={doc.name}>
                  {doc.name}
                </span>
                <span className="flex-shrink-0">
                  <StatusBadge status={doc.index_status} />
                </span>
              </div>
              <div className="flex items-center gap-3 text-xs text-gray-400">
                <span>{doc.file_type?.toUpperCase()}</span>
                <span>{formatSize(doc.file_size)}</span>
                {doc.index_status === 'completed' && (
                  <span className="flex items-center gap-1">
                    <Layers className="w-3 h-3" />
                    {doc.chunk_count} 块
                  </span>
                )}
                {doc.index_status === 'completed' && formatDuration(doc.index_duration_ms) && (
                  <span className="flex items-center gap-1">
                    <Clock className="w-3 h-3" />
                    {formatDuration(doc.index_duration_ms)}
                  </span>
                )}
                {doc.index_status === 'failed' && doc.index_error && (
                  <span className="text-red-400 truncate max-w-[200px]" title={doc.index_error}>
                    {doc.index_error}
                  </span>
                )}
              </div>
            </div>

            {/* 操作 */}
            <div className="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
              {doc.index_status === 'completed' && (
                <button
                  onClick={() => { setChunkDocID(doc.id); setChunkDocName(doc.name) }}
                  className="flex items-center gap-1 px-2 py-1 text-xs text-indigo-600 hover:bg-indigo-50 rounded-lg transition-colors"
                  title="查看分块"
                >
                  <ChevronRight className="w-3 h-3" />
                  分块
                </button>
              )}
              <button
                onClick={() => handleRebuild(doc.id)}
                className="p-1.5 text-gray-400 hover:text-blue-600 hover:bg-blue-50 rounded-lg transition-colors"
                title="重新索引"
              >
                <RefreshCw className="w-3.5 h-3.5" />
              </button>
              <button
                onClick={() => setConfirmDelete({ open: true, id: doc.id })}
                className="p-1.5 text-gray-400 hover:text-red-600 hover:bg-red-50 rounded-lg transition-colors"
                title="删除"
              >
                <Trash2 className="w-3.5 h-3.5" />
              </button>
            </div>
          </div>
        ))}
      </div>

      {/* 分页 */}
      {total > PAGE_SIZE && (
        <div className="flex items-center justify-between px-4 py-3 border-t border-gray-100 text-sm text-gray-500">
          <span>共 {total} 个文档</span>
          <div className="flex gap-1">
            <button
              disabled={page === 1}
              onClick={() => setPage(p => p - 1)}
              className="px-3 py-1 rounded-lg border border-gray-200 disabled:opacity-40 hover:bg-gray-50 transition-colors"
            >
              上一页
            </button>
            <button
              disabled={page * PAGE_SIZE >= total}
              onClick={() => setPage(p => p + 1)}
              className="px-3 py-1 rounded-lg border border-gray-200 disabled:opacity-40 hover:bg-gray-50 transition-colors"
            >
              下一页
            </button>
          </div>
        </div>
      )}

      <ConfirmDialog
        open={confirmDelete.open}
        title="删除文档"
        description="删除后将同步清除 Milvus 向量，无法恢复。确认删除？"
        onConfirm={() => { handleDelete(confirmDelete.id!) }}
        onClose={() => setConfirmDelete({ open: false })}
      />

      {chunkDocID && (
        <ChunkDrawer
          docID={chunkDocID}
          docName={chunkDocName}
          onClose={() => setChunkDocID(null)}
        />
      )}
    </>
  )
}
