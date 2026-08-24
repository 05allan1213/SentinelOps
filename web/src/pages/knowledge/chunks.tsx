import React, { useState, useEffect, useCallback } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import {
  ChevronRight, Layers, RefreshCw, Loader2,
  Hash, AlignLeft, Inbox, ChevronDown, ChevronUp,
  Search, FlaskConical, X,
} from 'lucide-react'
import { cn } from '@/utils'
import { knowledgeService, type KnowledgeBase, type DocItem, type ChunkItem, type SearchResultItem } from '@/services/knowledge'
import StatCard from '@/components/common/StatCard'
import Pagination from '@/components/common/Pagination'
import CustomSelect from '@/components/common/CustomSelect'
import toast from 'react-hot-toast'

// ── RAG 检索测试模态框 ──────────────────────────────────────────────────────────

interface SearchModalProps {
  baseID: string
  onClose: () => void
}

function SearchModal({ baseID, onClose }: SearchModalProps) {
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
    } catch {
      toast.error('检索失败')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
      <div className="bg-white rounded-xl shadow-xl w-full max-w-2xl flex flex-col max-h-[85vh]">
        {/* 标题栏 */}
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
        {/* 查询输入区 */}
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
                options={[3, 5, 8, 10, 15, 20].map(n => ({ value: n, label: String(n) }))}
                className="w-20"
              />
            </div>
            <button
              onClick={handleSearch}
              disabled={loading || !query.trim()}
              className={cn(
                'ml-auto flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium text-white transition-colors',
                loading || !query.trim() ? 'bg-indigo-400 cursor-not-allowed' : 'bg-indigo-600 hover:bg-indigo-700',
              )}
            >
              {loading ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Search className="w-3.5 h-3.5" />}
              检索
            </button>
          </div>
        </div>

        {/* 结果区 */}
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
                    {/* 相似度进度条 */}
                    {r.score !== undefined && (
                      <div className="flex items-center gap-1.5">
                        <div className="w-20 h-1.5 rounded-full bg-gray-200 overflow-hidden">
                          <div className="h-full rounded-full bg-indigo-500" style={{ width: `${Math.round(r.score * 100)}%` }} />
                        </div>
                        <span className="text-xs text-indigo-600 tabular-nums font-medium">{r.score.toFixed(2)}</span>
                      </div>
                    )}
                    {r.doc_title && (
                      <span className="text-xs text-gray-500 truncate" title={r.doc_title}>{r.doc_title}</span>
                    )}
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

// ── 分块列表主页 ────────────────────────────────────────────────────────────────

export default function KnowledgeChunks() {
  const { baseId, docId } = useParams<{ baseId: string; docId: string }>()
  const navigate = useNavigate()

  const [base, setBase] = useState<KnowledgeBase | null>(null)
  const [doc, setDoc] = useState<DocItem | null>(null)
  const [chunks, setChunks] = useState<ChunkItem[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [selected, setSelected] = useState<Set<string>>(new Set())

  // 分块关键词搜索（服务端）
  const [keyword, setKeyword]       = useState('')
  const [keywordInput, setKeywordInput] = useState('')

  // RAG 检索测试模态框
  const [showSearch, setShowSearch] = useState(false)

  const fetchData = useCallback(async () => {
    if (!baseId || !docId) return
    try {
      setLoading(true)
      const [baseInfo, docInfo, { list, total }] = await Promise.all([
        knowledgeService.getBase(baseId),
        knowledgeService.getDoc(docId),
        knowledgeService.listChunk(docId, { page, pageSize, keyword: keyword || undefined }),
      ])
      setBase(baseInfo)
      setDoc(docInfo)
      setChunks(list)
      setTotal(total)
    } catch {
      toast.error('获取分块列表失败')
    } finally {
      setLoading(false)
    }
  }, [baseId, docId, page, pageSize, keyword])

  useEffect(() => { fetchData() }, [fetchData])

  const totalPages = Math.ceil(total / pageSize)
  const avgChars = chunks.length > 0
    ? Math.round(chunks.reduce((s, c) => s + c.char_count, 0) / chunks.length)
    : 0
  const sectionCount = new Set(chunks.map(c => c.section_title).filter(Boolean)).size

  const toggleExpand = (id: string) => {
    setExpanded(prev => { const next = new Set(prev); next.has(id) ? next.delete(id) : next.add(id); return next })
  }

  const toggleSelect = (id: string) => {
    setSelected(prev => { const next = new Set(prev); next.has(id) ? next.delete(id) : next.add(id); return next })
  }

  const pageAllSelected = chunks.length > 0 && chunks.every(c => selected.has(c.id))
  const pagePartialSelected = !pageAllSelected && chunks.some(c => selected.has(c.id))

  const toggleAll = () => {
    setSelected(prev => {
      const next = new Set(prev)
      if (pageAllSelected) {
        chunks.forEach(c => next.delete(c.id))
      } else {
        chunks.forEach(c => next.add(c.id))
      }
      return next
    })
  }

  const handleBatchEnable = async (enabled: boolean) => {
    const ids = [...selected]
    setChunks(prev => prev.map(c => ids.includes(c.id) ? { ...c, enabled } : c))
    try {
      const updated = await knowledgeService.enableChunks({ ids, enabled })
      toast.success(`已${enabled ? '启用' : '禁用'} ${updated} 个分块`)
      setSelected(new Set())
    } catch {
      setChunks(prev => prev.map(c => ids.includes(c.id) ? { ...c, enabled: !enabled } : c))
      toast.error('操作失败')
    }
  }

  const handleAllEnable = async (enabled: boolean) => {
    if (!docId) return
    setChunks(prev => prev.map(c => ({ ...c, enabled })))
    try {
      const updated = await knowledgeService.enableChunks({ docId, enabled })
      toast.success(`已${enabled ? '启用' : '禁用'} ${updated} 个分块`)
    } catch {
      fetchData()
      toast.error('操作失败')
    }
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

  const handleKeywordSearch = () => {
    setKeyword(keywordInput)
    setPage(1)
  }

  return (
    <div className="flex flex-col gap-5">

      {/* 面包屑 */}
      <div className="flex items-center gap-1.5 text-sm text-gray-500 flex-shrink-0">
        <button onClick={() => navigate('/knowledge')} className="breadcrumb-item">知识库</button>
        <ChevronRight className="w-3.5 h-3.5 breadcrumb-separator" />
        <button onClick={() => navigate(`/knowledge/${baseId}/docs`)} className="breadcrumb-item">
          {base?.name ?? '…'}
        </button>
        <ChevronRight className="w-3.5 h-3.5 breadcrumb-separator" />
        <span className="breadcrumb-item active">分块管理</span>
      </div>

      {/* 标题栏 */}
      <div className="flex items-center justify-between flex-shrink-0">
        <div>
          <h1 className="text-2xl font-semibold text-gray-900 tracking-tight">{doc?.name ?? '分块列表'}</h1>
          <p className="text-sm text-gray-500 mt-1">共 {total} 个分块</p>
        </div>
        <div className="flex items-center gap-2 flex-wrap">
          {/* 分块内容关键词搜索 */}
          <div className="relative">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400" />
            <input
              type="text"
              placeholder="搜索分块内容…"
              value={keywordInput}
              onChange={e => setKeywordInput(e.target.value)}
              onKeyDown={e => e.key === 'Enter' && handleKeywordSearch()}
              className="input pl-9 w-44"
            />
            {keywordInput && (
              <button
                onClick={() => { setKeywordInput(''); setKeyword(''); setPage(1) }}
                className="absolute right-2 top-1/2 -translate-y-1/2 px-1.5 py-0.5 rounded text-xs text-gray-400 hover:text-gray-600 hover:bg-gray-100"
              >
                清除
              </button>
            )}
          </div>

          {/* RAG 检索测试 */}
          {baseId && (
            <button
              onClick={() => setShowSearch(true)}
              className="inline-flex items-center gap-1.5 px-3 h-9 rounded-lg border border-indigo-200 bg-indigo-50 text-indigo-700 hover:bg-indigo-100 text-sm font-medium transition-colors"
            >
              <FlaskConical className="w-3.5 h-3.5" />
              检索测试
            </button>
          )}

          <button
            onClick={() => handleAllEnable(true)}
            className="inline-flex items-center gap-1.5 px-3 h-9 rounded-lg bg-gray-800 text-white hover:bg-gray-700 text-sm font-medium whitespace-nowrap transition-colors"
          >
            全量启用
          </button>
          <button
            onClick={() => handleAllEnable(false)}
            className="inline-flex items-center gap-1.5 px-3 h-9 rounded-lg border border-gray-200 bg-white text-gray-600 hover:bg-gray-50 text-sm font-medium whitespace-nowrap transition-colors"
          >
            全量禁用
          </button>
          <button onClick={fetchData} disabled={loading} className="btn-default">
            <RefreshCw className={cn('w-4 h-4', loading && 'animate-spin')} />
            刷新
          </button>
        </div>
      </div>

      {/* 统计卡片 */}
      <div className="grid grid-cols-2 sm:grid-cols-3 gap-4 flex-shrink-0">
        <StatCard label="分块总数" value={total} Icon={Layers} tone="gray" />
        <StatCard label="平均字符数" value={avgChars} Icon={AlignLeft} tone="blue" />
        <StatCard label="章节数" value={sectionCount} Icon={Hash} tone="indigo" />
      </div>

      {/* 批量操作栏 */}
      {selected.size > 0 && (
        <div className="flex items-center gap-3 border-y border-slate-200 bg-slate-50 px-4 py-2 rounded-lg text-sm flex-shrink-0">
          <span className="text-slate-700 font-medium">已选 {selected.size} 个</span>
          <div className="flex gap-2 ml-2">
            <button
              onClick={() => handleBatchEnable(true)}
              className="px-3 py-1.5 rounded-lg bg-emerald-500 text-white hover:bg-emerald-600 text-xs font-medium transition-colors"
            >
              批量启用
            </button>
            <button
              onClick={() => handleBatchEnable(false)}
              className="px-3 py-1.5 rounded-lg bg-gray-100 text-gray-600 hover:bg-gray-200 text-xs font-medium transition-colors"
            >
              批量禁用
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
                    <input
                      type="checkbox"
                      className="rounded border-gray-300"
                      checked={pageAllSelected}
                      ref={el => { if (el) el.indeterminate = pagePartialSelected }}
                      onChange={toggleAll}
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
                          {keyword ? '没有匹配的分块' : '暂无分块数据'}
                        </p>
                      </div>
                    </td>
                  </tr>
                ) : chunks.map(chunk => {
                  const isExpanded = expanded.has(chunk.id)
                  return (
                    <React.Fragment key={chunk.id}>
                      <tr className={cn('group', selected.has(chunk.id) && 'bg-blue-50/60')}>
                        <td className="pl-6 py-4">
                          <input
                            type="checkbox"
                            className="rounded border-gray-300"
                            checked={selected.has(chunk.id)}
                            onChange={() => toggleSelect(chunk.id)}
                          />
                        </td>
                        <td className="pl-2 py-4 text-gray-400 font-mono text-xs">
                          {chunk.chunk_index + 1}
                        </td>
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
                          ) : (
                            <span className="text-gray-400 text-sm">—</span>
                          )}
                        </td>
                        <td className="py-4 px-4 text-gray-500 text-sm tabular-nums">
                          {chunk.char_count}
                        </td>
                        <td className="py-4 px-4">
                          <button
                            type="button"
                            onClick={() => handleToggleChunk(chunk)}
                            className={cn(
                              'inline-flex h-5 w-9 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 focus:outline-none',
                              chunk.enabled ? 'bg-emerald-500' : 'bg-gray-200',
                            )}
                            title={chunk.enabled ? '点击禁用' : '点击启用'}
                          >
                            <span
                              className={cn(
                                'inline-block h-4 w-4 transform rounded-full bg-white shadow ring-0 transition duration-200',
                                chunk.enabled ? 'translate-x-4' : 'translate-x-0',
                              )}
                            />
                          </button>
                        </td>
                        <td className="py-4 px-4 text-gray-500 text-xs tabular-nums">{chunk.updated_at}</td>
                        <td className="pr-4 py-4">
                          <button
                            onClick={() => toggleExpand(chunk.id)}
                            className={cn(
                              'inline-flex items-center gap-1 px-2.5 h-7 rounded-md border text-xs font-medium whitespace-nowrap transition-all',
                              isExpanded
                                ? 'border-gray-300 bg-gray-100 text-gray-700 hover:bg-gray-200'
                                : 'border-gray-200 bg-gray-50 text-gray-600 hover:bg-gray-100',
                            )}
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

      {/* RAG 检索测试模态框 */}
      {showSearch && baseId && (
        <SearchModal baseID={baseId} onClose={() => setShowSearch(false)} />
      )}
    </div>
  )
}
