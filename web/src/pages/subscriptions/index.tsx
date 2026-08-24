import { useState, useEffect, useMemo } from 'react'
import {
  Plus,
  Search,
  Trash2,
  RefreshCw,
  Github,
  Rss,
  Loader2,
  Inbox,
  Database,
  Activity,
  PauseCircle,
  BarChart3,
} from 'lucide-react'
import { cn, formatRelativeTime, getSourceTypeLabel, formatCronInterval } from '@/utils'
import Pagination from '@/components/common/Pagination'
import StatCard from '@/components/common/StatCard'
import ConfirmDialog from '@/components/common/ConfirmDialog'
import AddSubscriptionModal from './components/AddSubscriptionModal'
import { subscriptionService } from '@/services/subscription'
import type { Subscription } from '@/types'
import toast from 'react-hot-toast'
import CustomSelect, { type SelectOption } from '@/components/common/CustomSelect'

const sourceTypeIcons: Record<string, typeof Github> = {
  github_repo: Github,
  rss: Rss,
}

// ── 行内 Toggle 开关 ────────────────────────────────────────────────────────
function Toggle({ enabled, onClick }: { enabled: boolean; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      title={enabled ? '点击暂停' : '点击启动'}
      className={cn(
        'relative inline-flex h-5 w-9 items-center rounded-full transition-colors focus:outline-none',
        enabled ? 'bg-emerald-500' : 'bg-gray-200'
      )}
    >
      <span
        className={cn(
          'inline-block h-3.5 w-3.5 rounded-full bg-white shadow transition-transform',
          enabled ? 'translate-x-5' : 'translate-x-0.5'
        )}
      />
    </button>
  )
}

export default function Subscriptions() {
  const [showAddModal, setShowAddModal] = useState(false)
  const [editingSubscription, setEditingSubscription] = useState<Subscription | null>(null)
  const [searchQuery, setSearchQuery] = useState('')
  const [filterType, setFilterType] = useState<string>('all')
  const [filterStatus, setFilterStatus] = useState<string>('all')

  const [subscriptions, setSubscriptions] = useState<Subscription[]>([])
  const [loading, setLoading] = useState(true)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)

  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [confirmDelete, setConfirmDelete] = useState<{ open: boolean; id?: string; isBatch?: boolean }>({ open: false })

  const fetchSubscriptions = async () => {
    try {
      setLoading(true)
      const res = await subscriptionService.list(1, 200)
      setSubscriptions(res.list || [])
    } catch (error) {
      console.error('[Subscriptions] 获取订阅列表失败:', error)
      setSubscriptions([])
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { fetchSubscriptions() }, [])

  const handleToggle = async (sub: Subscription) => {
    try {
      if (sub.status === 'active') {
        await subscriptionService.pause(sub.id)
        toast.success('已暂停订阅')
      } else {
        await subscriptionService.resume(sub.id)
        toast.success('已恢复订阅')
      }
      fetchSubscriptions()
    } catch {
      toast.error('操作失败')
    }
  }

  const handleDelete = async (id: string) => {
    try {
      await subscriptionService.delete(id)
      toast.success('已删除订阅')
      fetchSubscriptions()
    } catch {
      toast.error('删除失败')
    }
  }

  const handleEdit = (subscription: Subscription) => {
    setEditingSubscription(subscription)
    setShowAddModal(true)
  }

  const handleBatchPause = async () => {
    const ids = [...selected].filter(id => subscriptions.find(s => s.id === id)?.status === 'active')
    if (ids.length === 0) { toast('没有运行中的订阅可暂停'); return }
    try {
      await Promise.all(ids.map(id => subscriptionService.pause(id)))
      toast.success(`已暂停 ${ids.length} 个订阅`)
      setSelected(new Set())
      fetchSubscriptions()
    } catch { toast.error('批量操作失败') }
  }

  const handleBatchResume = async () => {
    const ids = [...selected].filter(id => subscriptions.find(s => s.id === id)?.status !== 'active')
    if (ids.length === 0) { toast('没有已暂停的订阅可恢复'); return }
    try {
      await Promise.all(ids.map(id => subscriptionService.resume(id)))
      toast.success(`已恢复 ${ids.length} 个订阅`)
      setSelected(new Set())
      fetchSubscriptions()
    } catch { toast.error('批量操作失败') }
  }

  const handleBatchDelete = async () => {
    const ids = [...selected]
    try {
      await Promise.all(ids.map(id => subscriptionService.delete(id)))
      toast.success(`已删除 ${ids.length} 个订阅`)
      setSelected(new Set())
      fetchSubscriptions()
    } catch { toast.error('批量删除失败') }
  }

  const toggleSelect = (id: string) => {
    setSelected(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const filteredSubscriptions = useMemo(() => subscriptions.filter((sub) => {
    const matchesSearch = sub.name.toLowerCase().includes(searchQuery.toLowerCase())
    const matchesType = filterType === 'all' || sub.source_type === filterType
    const matchesStatus = filterStatus === 'all' || sub.status === filterStatus
    return matchesSearch && matchesType && matchesStatus
  }), [subscriptions, searchQuery, filterType, filterStatus])

  const pagedSubscriptions = filteredSubscriptions.slice((page - 1) * pageSize, page * pageSize)
  const totalPages = Math.ceil(filteredSubscriptions.length / pageSize)

  const toggleSelectAll = () => {
    if (selected.size === pagedSubscriptions.length && pagedSubscriptions.length > 0) {
      setSelected(new Set())
    } else {
      setSelected(new Set(pagedSubscriptions.map(s => s.id)))
    }
  }

  const stats = useMemo(() => ({
    total: subscriptions.length,
    active: subscriptions.filter(s => s.status === 'active').length,
    paused: subscriptions.filter(s => s.status !== 'active').length,
    totalEvents: subscriptions.reduce((sum, s) => sum + (s.total_events || 0), 0),
  }), [subscriptions])

  return (
    // h-full + flex col：填满 main 剩余高度，表格卡片用 flex-1 撑满底部
    <div className="flex flex-col gap-5 h-full">

      {/* 页面标题栏 */}
      <div className="flex items-center justify-between flex-shrink-0">
        <div>
          <h1 className="text-2xl font-semibold text-gray-900 tracking-tight">订阅管理</h1>
          <p className="text-sm text-gray-500 mt-1">管理安全事件数据源订阅</p>
        </div>
        <div className="flex items-center gap-2">
          <div className="relative">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400" />
            <input
              type="text"
              placeholder="搜索订阅..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="input pl-9 w-44"
            />
          </div>
          <CustomSelect value={filterType} onChange={v => setFilterType(v)} className="w-32" options={[
            { value: 'all', label: '全部类型' },
            { value: 'github_repo', label: 'GitHub' },
            { value: 'rss', label: 'RSS' },
          ] satisfies SelectOption[]} />
          <CustomSelect value={filterStatus} onChange={v => setFilterStatus(v)} className="w-28" options={[
            { value: 'all', label: '全部状态' },
            { value: 'active', label: '运行中' },
            { value: 'paused', label: '已暂停' },
          ] satisfies SelectOption[]} />
          <button onClick={fetchSubscriptions} disabled={loading} className="btn-default" title="刷新">
            <RefreshCw className={cn('w-4 h-4', loading && 'animate-spin')} />
          </button>
          <button onClick={() => setShowAddModal(true)} className="btn-primary">
            <Plus className="w-4 h-4" />
            添加订阅
          </button>
        </div>
      </div>

      {/* 统计卡片 */}
      <div className="grid grid-cols-2 sm:grid-cols-4 gap-4 flex-shrink-0">
        <StatCard label="订阅总数" value={stats.total} Icon={Database} tone="gray" />
        <StatCard label="运行中" value={stats.active} Icon={Activity} tone="emerald" />
        <StatCard label="已暂停" value={stats.paused} Icon={PauseCircle} tone="amber" />
        <StatCard label="累计抓取事件" value={stats.totalEvents.toLocaleString()} Icon={BarChart3} tone="blue" />
      </div>

      {/* 批量操作栏 */}
      {selected.size > 0 && (
        <div className="flex items-center gap-3 border border-slate-200 bg-slate-50 px-4 py-2 rounded-lg text-sm flex-shrink-0">
          <span className="text-slate-700 font-medium">已选 {selected.size} 个</span>
          <div className="flex gap-2 ml-2">
            <button onClick={handleBatchResume} className="px-3 py-1.5 rounded-lg bg-emerald-100 text-emerald-700 hover:bg-emerald-200 text-xs font-medium transition-colors">
              批量恢复
            </button>
            <button onClick={handleBatchPause} className="px-3 py-1.5 rounded-lg bg-amber-100 text-amber-700 hover:bg-amber-200 text-xs font-medium transition-colors">
              批量暂停
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

      {/* 订阅表格卡片：flex-1 min-h-0 填满剩余高度 */}
      <div className="card flex flex-col flex-1 min-h-0">
        {loading ? (
          <div className="flex items-center justify-center py-12">
            <Loader2 className="w-8 h-8 animate-spin text-gray-400" />
          </div>
        ) : (
          <>
          <div className="flex-1 overflow-auto min-h-0 overflow-x-auto">
            <table className="table w-full" style={{ tableLayout: 'fixed', minWidth: '720px' }}>
              <colgroup>
                <col style={{ width: '44px' }} />
                  <col style={{ width: '28%' }} />
                  <col style={{ width: '9%' }} />
                  <col style={{ width: '9%' }} />
                  <col style={{ width: '13%' }} />
                  <col style={{ width: '16%' }} />
                  <col style={{ width: '10%' }} />
                  <col style={{ width: '80px' }} />
                </colgroup>
                <thead>
                  <tr>
                    <th className="pl-4 w-11">
                      <input
                        type="checkbox"
                        checked={selected.size > 0 && selected.size === pagedSubscriptions.length}
                        ref={(el) => {
                          if (el) el.indeterminate = selected.size > 0 && selected.size < pagedSubscriptions.length
                        }}
                        onChange={toggleSelectAll}
                        className="rounded border-gray-300"
                      />
                    </th>
                    <th className="text-left px-3">名称</th>
                    <th className="text-left px-3">类型</th>
                    <th className="text-left px-3">状态</th>
                    <th className="text-left px-3">拉取间隔</th>
                    <th className="text-left px-3">上次抓取</th>
                    <th className="text-left px-3">事件数</th>
                    <th className="text-left pl-2 pr-4">操作</th>
                  </tr>
                </thead>
                <tbody>
                  {pagedSubscriptions.length === 0 ? (
                    <tr>
                      <td colSpan={8}>
                        <div className="py-20 flex flex-col items-center text-gray-400">
                          <Inbox className="w-16 h-16 mb-4 text-gray-200" />
                          <p className="text-base font-medium text-gray-500 mb-1">暂无订阅数据源</p>
                          <p className="text-sm text-gray-400">添加 RSS、GitHub 等数据源，自动采集安全事件</p>
                        </div>
                      </td>
                    </tr>
                  ) : pagedSubscriptions.map((sub) => {
                    const SourceIcon = sourceTypeIcons[sub.source_type] || Rss
                    const isActive = sub.status === 'active'

                    return (
                      <tr key={sub.id} className={cn('group', selected.has(sub.id) && 'bg-blue-50/60')}>
                        <td className="pl-4 py-3.5">
                          <input
                            type="checkbox"
                            checked={selected.has(sub.id)}
                            onChange={() => toggleSelect(sub.id)}
                            className="rounded border-gray-300"
                          />
                        </td>
                        <td className="py-3.5 px-3 min-w-0 overflow-hidden">
                          <div className="flex items-center gap-3">
                            <div className="w-8 h-8 rounded bg-gray-100 flex items-center justify-center flex-shrink-0">
                              <SourceIcon className="w-4 h-4 text-gray-500" />
                            </div>
                            <div className="min-w-0 flex-1">
                              <p className="font-medium text-gray-900 mb-0.5 truncate">{sub.name}</p>
                              <a
                                href={sub.source_url}
                                target="_blank"
                                rel="noopener noreferrer"
                                className="text-xs text-gray-400 hover:text-blue-600 truncate block"
                                title={sub.source_url}
                              >
                                {sub.source_url}
                              </a>
                            </div>
                          </div>
                        </td>
                        <td className="py-3.5 px-3 whitespace-nowrap">
                          <span className="tag tag-default">{getSourceTypeLabel(sub.source_type)}</span>
                        </td>
                        <td className="py-3.5 px-3">
                          <Toggle enabled={isActive} onClick={() => handleToggle(sub)} />
                        </td>
                        <td className="py-3.5 px-3 text-gray-900 whitespace-nowrap">
                          {formatCronInterval(sub.cron_expr)}
                        </td>
                        <td className="py-3.5 px-3 text-gray-900 whitespace-nowrap">
                          {sub.last_fetch_at ? formatRelativeTime(sub.last_fetch_at) : ''}
                        </td>
                        <td className="py-3.5 px-3 text-gray-900 font-medium whitespace-nowrap">
                          {(sub.total_events || 0).toLocaleString()}
                        </td>
                        <td className="py-3.5 pl-2 pr-4">
                          <div className="flex gap-1.5">
                            <button
                              onClick={() => handleEdit(sub)}
                              className="inline-flex items-center justify-center w-7 h-7 rounded-md border border-gray-200 bg-gray-50 text-gray-600 hover:bg-gray-100 transition-all"
                              title="编辑"
                            >
                              <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" />
                              </svg>
                            </button>
                            <button
                              onClick={() => setConfirmDelete({ open: true, id: sub.id })}
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

            {filteredSubscriptions.length > 0 && (
              <Pagination
                page={page}
                totalPages={totalPages}
                total={filteredSubscriptions.length}
                pageSize={pageSize}
                onChange={setPage}
                onPageSizeChange={size => { setPageSize(size); setPage(1) }}
              />
            )}
          </>
        )}
      </div>

      {/* 添加/编辑弹窗 */}
      <AddSubscriptionModal
        key={editingSubscription ? `edit-${editingSubscription.id}` : 'new'}
        isOpen={showAddModal}
        onClose={() => { setShowAddModal(false); setEditingSubscription(null) }}
        onSuccess={fetchSubscriptions}
        editMode={!!editingSubscription}
        initialData={editingSubscription || undefined}
      />

      {/* 删除确认弹窗 */}
      <ConfirmDialog
        open={confirmDelete.open}
        title={confirmDelete.isBatch ? `删除 ${selected.size} 个订阅` : '删除订阅'}
        description={
          confirmDelete.isBatch
            ? `确定要删除选中的 ${selected.size} 个订阅吗？删除后数据无法恢复。`
            : '确定要删除这个订阅吗？删除后数据无法恢复。'
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
