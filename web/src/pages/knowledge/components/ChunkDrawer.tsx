import { useState, useEffect } from 'react'
import { X, Loader2, Hash, AlignLeft, BookOpen } from 'lucide-react'
import { knowledgeService, type ChunkItem } from '@/services/knowledge'
import toast from 'react-hot-toast'

interface Props {
  docID: string
  docName: string
  onClose: () => void
}

const PAGE_SIZE = 20

export default function ChunkDrawer({ docID, docName, onClose }: Props) {
  const [chunks, setChunks] = useState<ChunkItem[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    const fetch = async () => {
      try {
        setLoading(true)
        const res = await knowledgeService.listChunk(docID, { page, pageSize: PAGE_SIZE })
        setChunks(res.list)
        setTotal(res.total)
      } catch {
        toast.error('获取分块列表失败')
      } finally {
        setLoading(false)
      }
    }
    fetch()
  }, [docID, page])

  return (
    <div className="fixed inset-0 z-50 flex justify-end">
      {/* 遮罩 */}
      <div className="absolute inset-0 bg-black/30" onClick={onClose} />

      {/* 抽屉面板 */}
      <div className="relative w-full max-w-xl bg-white h-full flex flex-col shadow-2xl">
        {/* 头部 */}
        <div className="flex items-start justify-between px-6 py-4 border-b border-gray-100 flex-shrink-0">
          <div className="min-w-0 pr-4">
            <h3 className="text-base font-semibold text-gray-900">文档分块详情</h3>
            <p className="text-xs text-gray-500 mt-0.5 truncate" title={docName}>{docName}</p>
          </div>
          <button onClick={onClose} className="p-1.5 rounded-lg hover:bg-gray-100 transition-colors flex-shrink-0">
            <X className="w-4 h-4 text-gray-500" />
          </button>
        </div>

        {/* 统计栏 */}
        <div className="px-6 py-2.5 bg-gray-50/80 border-b border-gray-100 flex items-center gap-4 text-sm text-gray-600 flex-shrink-0">
          <span className="flex items-center gap-1.5">
            <AlignLeft className="w-3.5 h-3.5 text-indigo-400" />
            共 <strong className="text-gray-800">{total}</strong> 个分块
          </span>
          <span className="text-gray-300">|</span>
          <span className="flex items-center gap-1.5">
            第 {(page - 1) * PAGE_SIZE + 1}–{Math.min(page * PAGE_SIZE, total)} 块
          </span>
        </div>

        {/* 内容区 */}
        <div className="flex-1 overflow-y-auto">
          {loading ? (
            <div className="flex items-center justify-center py-12 text-gray-400">
              <Loader2 className="w-5 h-5 animate-spin mr-2" />
              加载中…
            </div>
          ) : (
            <div className="divide-y divide-gray-50">
              {chunks.map(chunk => (
                <div key={chunk.id} className="px-6 py-4 space-y-2 hover:bg-gray-50/60 transition-colors">
                  {/* 块头 */}
                  <div className="flex items-center gap-3">
                    <span className="inline-flex items-center gap-1 px-2 py-0.5 bg-indigo-50 text-indigo-600 rounded-md text-xs font-mono font-medium">
                      <Hash className="w-3 h-3" />
                      {chunk.chunk_index}
                    </span>
                    {chunk.section_title && (
                      <span className="flex items-center gap-1 text-xs text-violet-600 bg-violet-50 px-2 py-0.5 rounded-md truncate max-w-[280px]" title={chunk.section_title}>
                        <BookOpen className="w-3 h-3 flex-shrink-0" />
                        {chunk.section_title}
                      </span>
                    )}
                    <span className="ml-auto text-xs text-gray-400">{chunk.char_count} 字符</span>
                  </div>

                  {/* 内容预览 */}
                  <p className="text-sm text-gray-700 leading-relaxed bg-gray-50 rounded-lg px-3 py-2.5 whitespace-pre-wrap break-words">
                    {chunk.content_preview}
                    {chunk.char_count > 200 && (
                      <span className="text-gray-400 italic">…（已截断）</span>
                    )}
                  </p>
                </div>
              ))}
            </div>
          )}
        </div>

        {/* 分页 */}
        {total > PAGE_SIZE && (
          <div className="flex items-center justify-between px-6 py-3 border-t border-gray-100 flex-shrink-0">
            <button
              disabled={page === 1}
              onClick={() => setPage(p => p - 1)}
              className="px-3 py-1.5 text-sm rounded-lg border border-gray-200 disabled:opacity-40 hover:bg-gray-50 transition-colors"
            >
              上一页
            </button>
            <span className="text-xs text-gray-500">第 {page} / {Math.ceil(total / PAGE_SIZE)} 页</span>
            <button
              disabled={page * PAGE_SIZE >= total}
              onClick={() => setPage(p => p + 1)}
              className="px-3 py-1.5 text-sm rounded-lg border border-gray-200 disabled:opacity-40 hover:bg-gray-50 transition-colors"
            >
              下一页
            </button>
          </div>
        )}
      </div>
    </div>
  )
}
