import { useState, useEffect, useMemo } from 'react'
import {
  Plus,
  Search,
  RefreshCw,
  Trash2,
  Pencil,
  X,
  Loader2,
  BookOpen,
  CheckCircle2,
  Star,
  Inbox,
} from 'lucide-react'
import { cn } from '@/utils'
import toast from 'react-hot-toast'
import StatCard from '@/components/common/StatCard'
import ConfirmDialog from '@/components/common/ConfirmDialog'
import Pagination from '@/components/common/Pagination'
import CustomSelect, { type SelectOption } from '@/components/common/CustomSelect'
import { termMappingService, type TermMappingItem, type CreateTermMappingReq } from '@/services/termMapping'

// ── 优先级徽章 ────────────────────────────────────────────────────────────────
function PriorityBadge({ priority }: { priority: number }) {
  const cls =
    priority >= 90
      ? 'bg-blue-100 text-blue-700'
      : priority >= 60
        ? 'bg-green-100 text-green-700'
        : 'bg-gray-100 text-gray-600'
  return (
    <span className={cn('inline-flex items-center px-2 py-0.5 rounded-full text-xs font-semibold', cls)}>
      {priority}
    </span>
  )
}

// ── 启用状态指示器 ────────────────────────────────────────────────────────────
function EnabledDot({ enabled }: { enabled: boolean }) {
  return (
    <span className="inline-flex items-center gap-1.5">
      <span className={cn('inline-block w-2 h-2 rounded-full flex-shrink-0', enabled ? 'bg-emerald-500' : 'bg-gray-300')} />
      <span className={cn('text-sm', enabled ? 'text-emerald-700' : 'text-gray-500')}>
        {enabled ? '启用' : '禁用'}
      </span>
    </span>
  )
}

// ── 新增/编辑模态框 ────────────────────────────────────────────────────────────
interface ModalProps {
  item?: TermMappingItem | null
  onClose: () => void
  onSuccess: () => void
}

function RuleModal({ item, onClose, onSuccess }: ModalProps) {
  const isEdit = !!item
  const [sourceTerm, setSourceTerm] = useState(item?.source_term || '')
  const [targetTerm, setTargetTerm] = useState(item?.target_term || '')
  const [priority, setPriority] = useState(item?.priority ?? 50)
  const [enabled, setEnabled] = useState(item?.enabled ?? true)
  const [saving, setSaving] = useState(false)

  const handleSave = async () => {
    if (!sourceTerm.trim()) { toast.error('原始词不能为空'); return }
    if (!targetTerm.trim()) { toast.error('目标词不能为空'); return }
    if (sourceTerm.length > 128) { toast.error('原始词不超过 128 字符'); return }
    if (targetTerm.length > 256) { toast.error('目标词不超过 256 字符'); return }

    setSaving(true)
    try {
      if (isEdit) {
        await termMappingService.update({ id: item!.id, target_term: targetTerm.trim(), priority, enabled })
        toast.success('规则已更新')
      } else {
        await termMappingService.create({ source_term: sourceTerm.trim(), target_term: targetTerm.trim(), priority, enabled } as CreateTermMappingReq)
        toast.success('规则已创建')
      }
      onSuccess()
      onClose()
    } catch (e: any) {
      toast.error(e?.message || '操作失败')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
      <div className="bg-white rounded-2xl shadow-xl w-full max-w-md mx-4 p-6">
        <div className="flex items-center justify-between mb-5">
          <h3 className="text-base font-semibold text-gray-900">{isEdit ? '编辑规则' : '新增规则'}</h3>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-600"><X className="w-5 h-5" /></button>
        </div>

        <div className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">
              原始词 <span className="text-red-500">*</span>
              <span className="ml-1 text-gray-400 font-normal text-xs">（例：rce, K8s）</span>
            </label>
            <input
              type="text"
              value={sourceTerm}
              onChange={(e) => setSourceTerm(e.target.value)}
              disabled={isEdit}
              className={cn('input w-full', isEdit && 'bg-gray-50 cursor-not-allowed text-gray-500')}
              placeholder="输入需要归一化的原始写法"
              maxLength={128}
            />
          </div>

          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">
              目标词 <span className="text-red-500">*</span>
              <span className="ml-1 text-gray-400 font-normal text-xs">（例：远程代码执行(RCE)）</span>
            </label>
            <input
              type="text"
              value={targetTerm}
              onChange={(e) => setTargetTerm(e.target.value)}
              className="input w-full"
              placeholder="输入标准/展开写法"
              maxLength={256}
            />
          </div>

          <div className="flex gap-4">
            <div className="flex-1">
              <label className="block text-sm font-medium text-gray-700 mb-1">
                优先级
                <span className="ml-1 text-gray-400 font-normal text-xs">（0–100，数值越大越优先）</span>
              </label>
              <input
                type="number"
                value={priority}
                onChange={(e) => setPriority(Math.max(0, Math.min(100, Number(e.target.value))))}
                className="input w-full"
                min={0}
                max={100}
              />
            </div>
            <div className="flex flex-col justify-end pb-0.5">
              <label className="block text-sm font-medium text-gray-700 mb-2">启用</label>
              <button
                type="button"
                onClick={() => setEnabled(!enabled)}
                className={cn('relative inline-flex h-6 w-11 items-center rounded-full transition-colors', enabled ? 'bg-emerald-500' : 'bg-gray-200')}
              >
                <span className={cn('inline-block h-4 w-4 rounded-full bg-white shadow transition-transform', enabled ? 'translate-x-6' : 'translate-x-1')} />
              </button>
            </div>
          </div>
        </div>

        <div className="flex justify-end gap-3 mt-6">
          <button onClick={onClose} className="btn-default">取消</button>
          <button onClick={handleSave} disabled={saving} className="btn-primary">
            {saving ? <Loader2 className="w-4 h-4 animate-spin" /> : null}
            保存
          </button>
        </div>
      </div>
    </div>
  )
}

// ── 主页面 ────────────────────────────────────────────────────────────────────
export default function TermMappingPage() {
  const [items, setItems] = useState<TermMappingItem[]>([])
  const [loading, setLoading] = useState(true)
  const [reloading, setReloading] = useState(false)
  const [searchQuery, setSearchQuery] = useState('')
  const [filterEnabled, setFilterEnabled] = useState<string>('all')
  const [showModal, setShowModal] = useState(false)
  const [editingItem, setEditingItem] = useState<TermMappingItem | null>(null)
  const [selected, setSelected] = useState<Set<number>>(new Set())
  const [confirmDelete, setConfirmDelete] = useState<{ open: boolean; id?: number; isBatch?: boolean }>({ open: false })

  // 分页状态
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)

  const fetchItems = async () => {
    try {
      setLoading(true)
      const data = await termMappingService.list()
      setItems(data)
    } catch (e) {
      console.error('[TermMapping] 获取规则失败:', e)
      setItems([])
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { fetchItems() }, [])

  // 过滤结果
  const filtered = useMemo(() => {
    return items.filter((item) => {
      const matchSearch =
        searchQuery === '' ||
        item.source_term.toLowerCase().includes(searchQuery.toLowerCase()) ||
        item.target_term.toLowerCase().includes(searchQuery.toLowerCase())
      const matchEnabled =
        filterEnabled === 'all' ||
        (filterEnabled === 'enabled' && item.enabled) ||
        (filterEnabled === 'disabled' && !item.enabled)
      return matchSearch && matchEnabled
    })
  }, [items, searchQuery, filterEnabled])

  // 过滤条件变化时重置到第 1 页
  useEffect(() => { setPage(1) }, [searchQuery, filterEnabled])

  // 当前页数据
  const pagedItems = useMemo(
    () => filtered.slice((page - 1) * pageSize, page * pageSize),
    [filtered, page, pageSize]
  )
  const totalPages = Math.ceil(filtered.length / pageSize)

  // 统计
  const enabledCount = items.filter((i) => i.enabled).length
  const highPriorityCount = items.filter((i) => i.priority >= 80).length

  const handleToggleEnabled = async (item: TermMappingItem) => {
    try {
      await termMappingService.update({ id: item.id, target_term: item.target_term, priority: item.priority, enabled: !item.enabled })
      fetchItems()
    } catch (e: any) {
      toast.error(e?.message || '状态切换失败')
    }
  }

  const handleDelete = async (id: number) => {
    try {
      await termMappingService.delete(id)
      toast.success('规则已删除')
      fetchItems()
    } catch (e: any) {
      toast.error(e?.message || '删除失败')
    }
  }

  const handleReload = async () => {
    setReloading(true)
    try {
      const res = await termMappingService.reload()
      toast.success(`已加载 ${res.count} 条规则`)
    } catch (e: any) {
      toast.error(e?.message || '重载失败')
    } finally {
      setReloading(false)
    }
  }

  const handleBatchToggle = async (enable: boolean) => {
    const ids = [...selected]
    if (ids.length === 0) return
    try {
      await Promise.all(
        ids.map((id) => {
          const item = items.find((i) => i.id === id)
          if (!item) return Promise.resolve()
          return termMappingService.update({ id, target_term: item.target_term, priority: item.priority, enabled: enable })
        })
      )
      toast.success(`已批量${enable ? '启用' : '禁用'} ${ids.length} 条规则`)
      setSelected(new Set())
      fetchItems()
    } catch {
      toast.error('批量操作失败')
    }
  }

  const handleBatchDelete = async () => {
    const ids = [...selected]
    try {
      await Promise.all(ids.map((id) => termMappingService.delete(id)))
      toast.success(`已删除 ${ids.length} 条规则`)
      setSelected(new Set())
      fetchItems()
    } catch {
      toast.error('批量删除失败')
    }
  }

  const toggleSelect = (id: number) => {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  // 全选/取消全选当前页
  const toggleSelectAll = () => {
    const pageIds = pagedItems.map((i) => i.id)
    const allSelected = pageIds.length > 0 && pageIds.every((id) => selected.has(id))
    setSelected((prev) => {
      const next = new Set(prev)
      if (allSelected) {
        pageIds.forEach((id) => next.delete(id))
      } else {
        pageIds.forEach((id) => next.add(id))
      }
      return next
    })
  }

  const pageAllSelected = pagedItems.length > 0 && pagedItems.every((i) => selected.has(i.id))
  const pagePartialSelected = !pageAllSelected && pagedItems.some((i) => selected.has(i.id))

  return (
    <div className="flex flex-col gap-5 h-full">

      {/* 页面标题栏 */}
      <div className="flex items-center justify-between flex-shrink-0">
        <div>
          <h1 className="text-2xl font-semibold text-gray-900 tracking-tight">术语规则管理</h1>
          <p className="text-sm text-gray-500 mt-1">管理检索安全域术语的归一化规则</p>
        </div>
        <div className="flex items-center gap-2">
          <div className="relative">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400" />
            <input
              type="text"
              placeholder="搜索原始词或目标词..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="input pl-9 w-52"
            />
          </div>
          <CustomSelect value={filterEnabled} onChange={v => setFilterEnabled(v)} className="w-28" options={[
            { value: 'all', label: '全部状态' },
            { value: 'enabled', label: '仅启用' },
            { value: 'disabled', label: '仅禁用' },
          ] satisfies SelectOption[]} />
          {searchQuery && (
            <button onClick={() => setSearchQuery('')} className="btn-default"><X className="w-4 h-4" /></button>
          )}
          <button onClick={handleReload} disabled={reloading} className="btn-default" title="将数据库规则热重载到进程内存">
            <RefreshCw className={cn('w-4 h-4', reloading && 'animate-spin')} />
            重载规则
          </button>
          <button onClick={() => { setEditingItem(null); setShowModal(true) }} className="btn-primary">
            <Plus className="w-4 h-4" />
            新增规则
          </button>
        </div>
      </div>

      {/* 统计卡片 */}
      <div className="grid grid-cols-3 gap-4 flex-shrink-0">
        <StatCard label="规则总数" value={items.length} Icon={BookOpen} tone="gray" />
        <StatCard label="已启用" value={enabledCount} Icon={CheckCircle2} tone="emerald" />
        <StatCard label="高优先级（≥80）" value={highPriorityCount} Icon={Star} tone="blue" />
      </div>

      {/* 批量操作栏 */}
      {selected.size > 0 && (
        <div className="flex items-center gap-3 border border-slate-200 bg-slate-50 px-4 py-2 rounded-lg text-sm flex-shrink-0">
          <span className="text-slate-700 font-medium">已选 {selected.size} 条</span>
          <div className="flex gap-2 ml-2">
            <button onClick={() => handleBatchToggle(true)} className="px-3 py-1.5 rounded-lg bg-emerald-100 text-emerald-700 hover:bg-emerald-200 text-xs font-medium transition-colors">
              批量启用
            </button>
            <button onClick={() => handleBatchToggle(false)} className="px-3 py-1.5 rounded-lg bg-amber-100 text-amber-700 hover:bg-amber-200 text-xs font-medium transition-colors">
              批量禁用
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

      {/* 表格卡片 */}
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
                  <col style={{ width: '20%' }} />
                  <col style={{ width: '34%' }} />
                  <col style={{ width: '9%' }} />
                  <col style={{ width: '11%' }} />
                  <col style={{ width: '14%' }} />
                  <col style={{ width: '80px' }} />
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
                    <th className="text-left px-3">原始词</th>
                    <th className="text-left px-3">目标词</th>
                    <th className="text-left px-3">优先级</th>
                    <th className="text-left px-3">状态</th>
                    <th className="text-left px-3">更新时间</th>
                    <th className="text-left pl-2 pr-4">操作</th>
                  </tr>
                </thead>
                <tbody>
                  {pagedItems.length === 0 ? (
                    <tr>
                      <td colSpan={7}>
                        <div className="py-20 flex flex-col items-center text-gray-400">
                          <Inbox className="w-16 h-16 mb-4 text-gray-200" />
                          <p className="text-base font-medium text-gray-500 mb-1">
                            {items.length === 0 ? '暂无规则' : '没有匹配的规则'}
                          </p>
                          <p className="text-sm text-gray-400">
                            {items.length === 0 ? '添加术语归一化规则，提升 RAG 检索准确率' : '尝试调整搜索条件'}
                          </p>
                        </div>
                      </td>
                    </tr>
                  ) : pagedItems.map((item) => (
                    <tr key={item.id} className={cn('group', selected.has(item.id) && 'bg-blue-50/60')}>
                      <td className="pl-4 py-3.5">
                        <input
                          type="checkbox"
                          checked={selected.has(item.id)}
                          onChange={() => toggleSelect(item.id)}
                          className="rounded border-gray-300"
                        />
                      </td>
                      <td className="py-3.5 px-3">
                        <code className="inline-block px-2 py-0.5 rounded-md bg-slate-100 text-slate-800 text-xs font-mono font-semibold tracking-wide max-w-[128px] truncate align-middle" title={item.source_term}>
                          {item.source_term}
                        </code>
                      </td>
                      <td className="py-3.5 px-3">
                        <span className="text-sm text-gray-700 block truncate" title={item.target_term}>
                          {item.target_term}
                        </span>
                      </td>
                      <td className="py-3.5 px-3"><PriorityBadge priority={item.priority} /></td>
                      <td className="py-3.5 px-3">
                        <button onClick={() => handleToggleEnabled(item)} className="cursor-pointer hover:opacity-75 transition-opacity" title="点击切换">
                          <EnabledDot enabled={item.enabled} />
                        </button>
                      </td>
                      <td className="py-3.5 px-3 text-sm text-gray-500">{item.updated_at.slice(0, 16)}</td>
                      <td className="py-3.5 pl-2 pr-4">
                        <div className="flex w-full items-center gap-1.5">
                          <button
                            onClick={() => { setEditingItem(item); setShowModal(true) }}
                            className="inline-flex items-center justify-center w-7 h-7 rounded-md border border-gray-200 bg-gray-50 text-gray-600 hover:bg-gray-100 transition-all"
                            title="编辑"
                          >
                            <Pencil className="w-3.5 h-3.5" />
                          </button>
                          <button
                            onClick={() => setConfirmDelete({ open: true, id: item.id })}
                            className="inline-flex items-center justify-center w-7 h-7 rounded-md border border-red-200 bg-red-50 text-red-600 hover:bg-red-100 transition-all"
                            title="删除"
                          >
                            <Trash2 className="w-3.5 h-3.5" />
                          </button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>

            {/* 分页 */}
            {filtered.length > 0 && (
              <Pagination
                page={page}
                totalPages={totalPages}
                total={filtered.length}
                pageSize={pageSize}
                onChange={(p) => { setPage(p); setSelected(new Set()) }}
                onPageSizeChange={size => { setPageSize(size); setPage(1) }}
              />
            )}
          </>
        )}
      </div>

      {/* 新增/编辑模态框 */}
      {showModal && (
        <RuleModal
          key={editingItem ? `edit-${editingItem.id}` : 'new'}
          item={editingItem}
          onClose={() => { setShowModal(false); setEditingItem(null) }}
          onSuccess={fetchItems}
        />
      )}

      {/* 删除确认弹窗 */}
      <ConfirmDialog
        open={confirmDelete.open}
        title={confirmDelete.isBatch ? `删除 ${selected.size} 条规则` : '删除规则'}
        description={
          confirmDelete.isBatch
            ? `确定要删除选中的 ${selected.size} 条规则吗？删除后数据无法恢复。`
            : '确定要删除这条规则吗？删除后数据无法恢复。'
        }
        onConfirm={() => {
          if (confirmDelete.isBatch) handleBatchDelete()
          else if (confirmDelete.id !== undefined) handleDelete(confirmDelete.id)
        }}
        onClose={() => setConfirmDelete({ open: false })}
      />
    </div>
  )
}
