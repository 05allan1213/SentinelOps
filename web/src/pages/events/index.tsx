import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Search,
  ExternalLink,
  Shield,
  Trash2,
  RefreshCw,
  Loader2,
  ChevronUp,
  ChevronDown as ChevronDownIcon,
  AlertOctagon,
  CalendarDays,
  Clock,
  Inbox,
  Pencil,
  Zap,
} from 'lucide-react'
import { cn, formatDate } from '@/utils'
import Pagination from '@/components/common/Pagination'
import StatCard from '@/components/common/StatCard'
import ConfirmDialog from '@/components/common/ConfirmDialog'
import { useContextStore } from '@/stores/contextStore'
import { useSettingsStore } from '@/stores/settingsStore'
import EventDetailModal from './components/EventDetailModal'
import type { SecurityEvent } from '@/types'
import { eventService } from '@/services/event'
import { opsService } from '@/services/ops'
import toast from 'react-hot-toast'
import CustomSelect, { type SelectOption } from '@/components/common/CustomSelect'

type SortField = 'severity' | 'source' | 'created_at'

const severityConfig: Record<string, { label: string; class: string; dot: string }> = {
  critical: { label: '严重', class: 'severity-critical', dot: 'bg-red-500' },
  high:     { label: '高危', class: 'severity-high',     dot: 'bg-orange-500' },
  medium:   { label: '中危', class: 'severity-medium',   dot: 'bg-yellow-500' },
  low:      { label: '低危', class: 'severity-low',      dot: 'bg-blue-400' },
  info:     { label: '信息', class: 'severity-info',     dot: 'bg-gray-400' },
}

const statusOptions = [
  { value: 'new',        label: '待处置' },
  { value: 'processing', label: '处理中' },
  { value: 'resolved',   label: '已解决' },
  { value: 'ignored',    label: '已忽略' },
]

interface EventStats {
  total: number
  critical_count: number
  new_7days: number
  pending: number
}

export default function Events() {
  const navigate = useNavigate()
  const autoMarkRead = useSettingsStore(s => s.autoMarkRead)
  const [searchQuery, setSearchQuery] = useState('')
  const [severityFilter, setSeverityFilter] = useState('all')
  const [sortField, setSortField] = useState<SortField>('created_at')
  const [sortDir, setSortDir] = useState<'asc' | 'desc'>('desc')
  const [selectedEvent, setSelectedEvent] = useState<SecurityEvent | null>(null)
  const [confirmDelete, setConfirmDelete] = useState<{ open: boolean; id?: string; isBatch?: boolean }>({ open: false })
  const [selected, setSelected] = useState<Set<string>>(new Set())

  const [events, setEvents] = useState<SecurityEvent[]>([])
  const [loading, setLoading] = useState(true)
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const setContext = useContextStore((s) => s.setContext)

  const [stats, setStats] = useState<EventStats>({ total: 0, critical_count: 0, new_7days: 0, pending: 0 })

  const fetchEvents = async () => {
    try {
      setLoading(true)
      const filter: Record<string, unknown> = {
        page, size: pageSize,
        order_by: sortField,
        order_dir: sortDir,
      }
      if (searchQuery) filter.keyword = searchQuery
      if (severityFilter !== 'all') filter.severity = severityFilter

      // 并行请求列表和统计
      const [res, statsRes] = await Promise.all([
        eventService.list(filter),
        eventService.getStats(),
      ])
      setEvents(res.list || [])
      setTotal(res.total || 0)
      setStats({
        total: statsRes.total,
        critical_count: statsRes.critical_count,
        new_7days: statsRes.new_7days,
        pending: statsRes.pending,
      })
    } catch (error) {
      console.error('[Events] 获取事件列表失败:', error)
      setEvents([])
      setTotal(0)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { fetchEvents() }, [page, pageSize, severityFilter, sortField, sortDir])

  const handleSearch = () => { setPage(1); fetchEvents() }

  const handleSort = (field: SortField) => {
    if (sortField === field) {
      setSortDir(d => d === 'desc' ? 'asc' : 'desc')
    } else {
      setSortField(field)
      setSortDir('desc')
    }
    setPage(1)
  }

  const handleDelete = async (id: string) => {
    try {
      await eventService.delete(id)
      toast.success('已删除')
      fetchEvents()
    } catch {
      toast.error('删除失败')
    }
  }

  const handleBatchDelete = async () => {
    const ids = [...selected]
    try {
      await eventService.batchDelete(ids)
      toast.success(`已删除 ${ids.length} 条事件`)
      setSelected(new Set())
      fetchEvents()
    } catch {
      toast.error('批量删除失败')
    }
  }

  const handleBatchUpdateStatus = async (status: string) => {
    const ids = [...selected]
    try {
      await eventService.batchUpdateStatus(ids, status)
      toast.success(`已将 ${ids.length} 条事件标记为${statusOptions.find(o => o.value === status)?.label || status}`)
      setSelected(new Set())
      fetchEvents()
    } catch {
      toast.error('批量状态更新失败')
    }
  }

  const toggleSelect = (id: string) => {
    setSelected(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const toggleSelectAll = () => {
    const pageIds = events.map(e => e.id)
    const allSelected = pageIds.length > 0 && pageIds.every(id => selected.has(id))
    setSelected(prev => {
      const next = new Set(prev)
      if (allSelected) {
        pageIds.forEach(id => next.delete(id))
      } else {
        pageIds.forEach(id => next.add(id))
      }
      return next
    })
  }

  const pageAllSelected = events.length > 0 && events.every(e => selected.has(e.id))
  const pagePartialSelected = !pageAllSelected && events.some(e => selected.has(e.id))

  const handleAutoResponse = async (event: SecurityEvent) => {
    try {
      await opsService.directRun(event.id)
      navigate(`/ops`)
    } catch {
      toast.error('触发失败')
    }
  }

  const handleStatusChange = async (id: string, status: string) => {    try {
      await eventService.updateStatus(id, status)
      setEvents(prev => prev.map(e => e.id === id ? { ...e, status: status as SecurityEvent['status'] } : e))
    } catch {
      toast.error('状态更新失败')
    }
  }

  const totalPages = Math.ceil(total / pageSize)

  return (
    // h-full + flex col：填满 main 剩余高度，表格卡片用 flex-1 撑满底部
    <div className="flex flex-col gap-5 h-full">

      {/* 页面标题栏 */}
      <div className="flex items-center justify-between flex-shrink-0">
        <div>
          <h1 className="text-2xl font-semibold text-gray-900 tracking-tight">安全事件</h1>
          <p className="text-sm text-gray-500 mt-1">共 {total} 个事件</p>
        </div>
        <div className="flex items-center gap-2">
          <div className="relative">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400" />
            <input
              type="text"
              placeholder="搜索标题、CVE..."
              value={searchQuery}
              onChange={e => setSearchQuery(e.target.value)}
              onKeyDown={e => e.key === 'Enter' && handleSearch()}
              className="input pl-9 w-52"
            />
          </div>
          <CustomSelect
            value={severityFilter}
            onChange={v => { setSeverityFilter(v); setPage(1) }}
            className="w-28"
            options={[
              { value: 'all', label: '全部级别' },
              { value: 'critical', label: '严重' },
              { value: 'high', label: '高危' },
              { value: 'medium', label: '中危' },
              { value: 'low', label: '低危' },
            ] satisfies SelectOption[]}
          />
          <button onClick={fetchEvents} disabled={loading} className="btn-default">
            <RefreshCw className={cn('w-4 h-4', loading && 'animate-spin')} />
            刷新
          </button>
        </div>
      </div>

      {/* 统计卡片 */}
      <div className="grid grid-cols-2 sm:grid-cols-4 gap-4 flex-shrink-0">
        <StatCard label="事件总数" value={stats.total} Icon={Shield} tone="gray" />
        <StatCard label="严重 / 高危" value={stats.critical_count} Icon={AlertOctagon} tone="red" />
        <StatCard label="近 7 天新增" value={stats.new_7days} Icon={CalendarDays} tone="blue" />
        <StatCard label="待处置" value={stats.pending} Icon={Clock} tone="amber" />
      </div>

      {/* 批量操作栏 */}
      {selected.size > 0 && (
        <div className="flex items-center gap-3 border border-slate-200 bg-slate-50 px-4 py-2 rounded-lg text-sm flex-shrink-0">
          <span className="text-slate-700 font-medium">已选 {selected.size} 条</span>
          <div className="flex gap-2 ml-2">
            <button onClick={() => handleBatchUpdateStatus('processing')} className="px-3 py-1.5 rounded-lg bg-amber-100 text-amber-700 hover:bg-amber-200 text-xs font-medium transition-colors">
              标记处理中
            </button>
            <button onClick={() => handleBatchUpdateStatus('resolved')} className="px-3 py-1.5 rounded-lg bg-emerald-100 text-emerald-700 hover:bg-emerald-200 text-xs font-medium transition-colors">
              标记已解决
            </button>
            <button onClick={() => handleBatchUpdateStatus('ignored')} className="px-3 py-1.5 rounded-lg bg-gray-100 text-gray-600 hover:bg-gray-200 text-xs font-medium transition-colors">
              标记已忽略
            </button>
            <button onClick={() => setConfirmDelete({ open: true, isBatch: true })} className="px-3 py-1.5 rounded-lg bg-red-100 text-red-700 hover:bg-red-200 text-xs font-medium transition-colors">
              批量删除
            </button>
          </div>
          <button
            onClick={() => setSelected(new Set())}
            className="ml-auto text-xs text-gray-500 hover:text-gray-700 px-2 py-1 rounded hover:bg-gray-100 transition-colors"
          >
            取消选择
          </button>
        </div>
      )}

      {/* 事件表格卡片：flex-1 min-h-0 填满剩余高度 */}
      <div className="card flex flex-col flex-1 min-h-0">
        {loading ? (
          <div className="flex items-center justify-center py-12">
            <Loader2 className="w-8 h-8 animate-spin text-gray-400" />
          </div>
        ) : (
          <>
          <div className="flex-1 overflow-auto min-h-0 overflow-x-auto">
            <table className="table w-full whitespace-nowrap" style={{ tableLayout: 'fixed', minWidth: '720px' }}>
              <colgroup>
                <col style={{ width: '44px' }} />
                <col style={{ width: '32%' }} />
                <col style={{ width: '9%' }} />
                <col style={{ width: '16%' }} />
                <col style={{ width: '15%' }} />
                <col style={{ width: '13%' }} />
                <col style={{ width: '88px' }} />
              </colgroup>
              <thead>
                <tr>
                  <th className="pl-4 w-11">
                    <input
                      type="checkbox"
                      checked={pageAllSelected}
                      ref={(el) => { if (el) el.indeterminate = pagePartialSelected }}
                      onChange={toggleSelectAll}
                      className="rounded border-gray-300"
                    />
                  </th>
                  <th className="text-left px-3">事件</th>
                  {[
                    { id: 'severity', label: '级别' },
                    { id: 'source', label: '来源' },
                    { id: 'created_at', label: '时间' }
                  ].map(({ id, label }) => {
                    const field = id as SortField
                    const active = sortField === field
                    return (
                      <th key={field} className="text-left px-3">
                        <button
                          onClick={() => handleSort(field)}
                          className="inline-flex items-center gap-1 hover:text-gray-900 transition-colors"
                        >
                          {label}
                          <span className="flex flex-col">
                            <ChevronUp className={cn('w-2.5 h-2.5 -mb-0.5', active && sortDir === 'asc' ? 'text-primary-500' : 'text-gray-300')} />
                            <ChevronDownIcon className={cn('w-2.5 h-2.5', active && sortDir === 'desc' ? 'text-primary-500' : 'text-gray-300')} />
                          </span>
                        </button>
                      </th>
                    )
                  })}
                  <th className="text-left px-3">状态</th>
                  <th className="text-left pl-2 pr-4">操作</th>
                </tr>
              </thead>
              <tbody>
                {events.length === 0 ? (
                  <tr>
                    <td colSpan={7}>
                      <div className="py-20 flex flex-col items-center text-gray-400">
                        <Inbox className="w-16 h-16 mb-4 text-gray-200" />
                        <p className="text-base font-medium text-gray-500 mb-1">暂无安全事件</p>
                        <p className="text-sm text-gray-400">配置订阅源后系统将自动采集安全事件</p>
                      </div>
                    </td>
                  </tr>
                ) : events.map(event => {
                  const severity = severityConfig[event.severity] || severityConfig.info
                  return (
                    <tr
                      key={event.id}
                      className={cn('cursor-pointer group', selected.has(event.id) && 'bg-blue-50/60')}
                      onClick={() => {
                        setSelectedEvent(event)
                        setContext('events', event.id, event.title)
                        if (autoMarkRead && event.status === 'new') {
                          eventService.updateStatus(event.id, 'processing')
                            .then(() => setEvents(prev => prev.map(e => e.id === event.id ? { ...e, status: 'processing' as const } : e)))
                            .catch(() => {})
                        }
                      }}
                    >
                      <td className="pl-4 py-3.5" onClick={e => e.stopPropagation()}>
                        <input
                          type="checkbox"
                          checked={selected.has(event.id)}
                          onChange={() => toggleSelect(event.id)}
                          className="rounded border-gray-300"
                        />
                      </td>
                      <td className="py-3.5 px-3 min-w-0 overflow-hidden">
                        <div className="flex items-center gap-1.5 min-w-0 pr-4">
                          {event.event_type && (
                            <span className={cn(
                              'inline-flex items-center h-5 px-1.5 rounded text-[10px] font-semibold flex-shrink-0',
                              event.event_type === 'github'   && 'bg-slate-800 text-white',
                              event.event_type === 'rss'      && 'bg-orange-100 text-orange-700',
                              event.event_type === 'web'      && 'bg-teal-100 text-teal-700',
                              event.event_type === 'webhook'  && 'bg-violet-100 text-violet-700',
                              event.event_type === 'cef'      && 'bg-blue-100 text-blue-700',
                              event.event_type === 'leef'     && 'bg-cyan-100 text-cyan-700',
                              event.event_type === 'api_push' && 'bg-indigo-100 text-indigo-700',
                              event.event_type === 'manual'   && 'bg-gray-100 text-gray-500',
                              !['github','rss','web','webhook','cef','leef','api_push','manual'].includes(event.event_type) && 'bg-gray-100 text-gray-500',
                            )}>
                              {event.event_type === 'github'   ? 'GitHub'
                               : event.event_type === 'rss'    ? 'RSS'
                               : event.event_type === 'web'    ? 'Web'
                               : event.event_type === 'webhook' ? 'Webhook'
                               : event.event_type === 'cef'    ? 'CEF'
                               : event.event_type === 'leef'   ? 'LEEF'
                               : event.event_type === 'api_push' ? 'API Push'
                               : event.event_type === 'manual' ? '手动'
                               : event.event_type}
                            </span>
                          )}
                          <p className="text-sm font-medium text-gray-900 truncate" title={event.title}>{event.title}</p>
                          {event.source_url && (
                            <a href={event.source_url} target="_blank" rel="noopener noreferrer"
                              onClick={e => e.stopPropagation()}
                              className="text-gray-400 hover:text-primary-500 transition-colors shrink-0">
                              <ExternalLink className="w-3.5 h-3.5" />
                            </a>
                          )}
                        </div>
                        {event.cve_id && (
                          <p className="text-xs text-gray-400 font-mono mt-1 truncate">
                            {event.cve_id}{event.cvss_score ? ` · CVSS ${event.cvss_score}` : ''}
                          </p>
                        )}
                      </td>

                      <td className="py-3.5 px-3">
                        <span className={cn(severity.class, 'inline-flex items-center gap-1')}>
                          <span className={cn('w-1.5 h-1.5 rounded-full', severity.dot)} />
                          {severity.label}
                        </span>
                      </td>

                      <td className="py-3.5 px-3">
                        <span className="tag tag-default truncate max-w-[180px] inline-block" title={event.source}>
                          {event.source === 'web_search' ? '联网搜索' : event.source}
                        </span>
                      </td>

                      <td className="text-gray-500 text-sm py-3.5 px-3">
                        {formatDate(event.created_at, 'YYYY-MM-DD HH:mm')}
                      </td>

                      {/* 行内状态快速切换 */}
                      <td className="py-3.5 px-3" onClick={e => e.stopPropagation()}>
                        <select
                          value={event.status}
                          onChange={e => handleStatusChange(event.id, e.target.value)}
                          className={cn(
                            'text-xs rounded-lg border px-2 py-1 font-medium cursor-pointer focus:outline-none focus:ring-1 focus:ring-primary-300',
                            event.status === 'new'        && 'border-blue-200 bg-blue-50 text-blue-700',
                            event.status === 'processing' && 'border-amber-200 bg-amber-50 text-amber-700',
                            event.status === 'resolved'   && 'border-emerald-200 bg-emerald-50 text-emerald-700',
                            event.status === 'ignored'    && 'border-gray-200 bg-gray-50 text-gray-500',
                          )}
                        >
                          {statusOptions.map(o => (
                            <option key={o.value} value={o.value}>{o.label}</option>
                          ))}
                        </select>
                      </td>

                      <td className="py-3.5 pl-2 pr-4" onClick={e => e.stopPropagation()}>
                        <div className="flex items-center gap-1.5">
                          <button
                            onClick={() => handleAutoResponse(event)}
                            className="inline-flex items-center justify-center w-7 h-7 rounded-md border border-violet-200 bg-violet-50 text-violet-600 hover:bg-violet-100 transition-all"
                            title="一键AI运维"
                          >
                            <Zap className="w-3.5 h-3.5" />
                          </button>
                          <button
                            onClick={() => {
                              setSelectedEvent(event)
                              setContext('events', event.id, event.title)
                            }}
                            className="inline-flex items-center justify-center w-7 h-7 rounded-md border border-gray-200 bg-gray-50 text-gray-600 hover:bg-gray-100 transition-all"
                            title="查看详情"
                          >
                            <Pencil className="w-3.5 h-3.5" />
                          </button>
                          <button
                            onClick={() => setConfirmDelete({ open: true, id: event.id })}
                            className="inline-flex items-center justify-center w-7 h-7 rounded-md border border-red-200 bg-red-50 text-red-600 hover:bg-red-100 transition-all"
                            title="删除"
                          >
                            <Trash2 className="w-3.5 h-3.5" />
                          </button>
                        </div>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>

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
        </>
        )}
      </div>

      <EventDetailModal
        event={selectedEvent}
        onClose={() => setSelectedEvent(null)}
        onUpdate={(updated) => setSelectedEvent(updated)}
      />

      <ConfirmDialog
        open={confirmDelete.open}
        title={confirmDelete.isBatch ? `删除 ${selected.size} 条事件` : '删除安全事件'}
        description={
          confirmDelete.isBatch
            ? `确定要删除选中的 ${selected.size} 条安全事件吗？此操作无法撤销。`
            : '确定要删除该安全事件吗？此操作无法撤销。'
        }
        onConfirm={() => {
          if (confirmDelete.isBatch) handleBatchDelete()
          else if (confirmDelete.id) handleDelete(confirmDelete.id)
        }}
        onClose={() => setConfirmDelete({ open: false })}
      />
    </div>
  )
}
