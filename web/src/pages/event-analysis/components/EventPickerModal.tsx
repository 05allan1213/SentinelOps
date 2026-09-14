import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import Button from '@/components/common/Button'
import { useEffect, useRef, useState } from 'react'
import { X, Search, Check, Loader2 } from 'lucide-react'
import { eventService } from '@/services/event'
import { SecurityEvent } from '@/types'
import { cn, formatDate } from '@/utils'

interface Props {
  visible: boolean
  onClose: () => void
  onConfirm: (ids: string[]) => void
  selectedIds: string[]
}

const severityMap: Record<string, { label: string; color: string }> = {
  critical: { label: 'CRIT', color: '#F43F5E' },
  high: { label: 'HIGH', color: '#F97316' },
  medium: { label: 'MED', color: '#EAB308' },
  low: { label: 'LOW', color: '#3B82F6' },
  info: { label: 'INFO', color: '#6B7280' },
}

export default function EventPickerModal({ visible, onClose, onConfirm, selectedIds }: Props) {
  const [events, setEvents] = useState<SecurityEvent[]>([])
  const [loading, setLoading] = useState(true)
  // 打开弹窗时由父级 key 重新挂载，草稿选中集直接以 selectedIds 初始化。
  const [selected, setSelected] = useState<Set<string>>(() => new Set(selectedIds))
  const [keyword, setKeyword] = useState('')
  const [searchNonce, setSearchNonce] = useState(0)
  const keywordRef = useRef('')

  useEffect(() => {
    if (!visible) return
    let cancelled = false
    void (async () => {
      try {
        const res = await eventService.list({ page: 1, size: 50, keyword: keywordRef.current || undefined })
        if (!cancelled) {
          setEvents(res.list)
          setLoading(false)
        }
      } catch {
        // 忽略事件列表加载失败
        if (!cancelled) setLoading(false)
      }
    })()
    return () => { cancelled = true }
  }, [visible, searchNonce])

  const toggle = (id: string) => {
    setSelected(prev => {
      const next = new Set(prev)
      next.has(id) ? next.delete(id) : next.add(id)
      return next
    })
  }

  const handleSearch = () => { keywordRef.current = keyword; setSearchNonce(n => n + 1) }

  if (!visible) return null

  return (
    <Dialog open onOpenChange={open => { if (!open) onClose() }}>
      <DialogContent aria-describedby={undefined} className="max-w-2xl overflow-y-auto p-4 sm:p-6">
        {/* 弹窗标题 */}
        <DialogHeader className="flex items-start justify-between gap-3 space-y-0">
          <DialogTitle>选择要分析的事件</DialogTitle>
          <Button variant="icon" aria-label="关闭" onClick={onClose}>
            <X className="w-5 h-5" />
          </Button>
        </DialogHeader>

        {/* 事件搜索 */}
        <div className="px-5 py-3 border-b border-gray-200 border-gray-200/30">
          <div className="flex items-center gap-2">
            <div className="flex-1 flex items-center gap-2 px-4 py-2 rounded-lg bg-gray-50 bg-gray-50 border border-gray-200 border-gray-200">
              <Search className="w-4 h-4 text-gray-500 text-gray-500" />
              <input
                value={keyword}
                onChange={e => setKeyword(e.target.value)}
                onKeyDown={e => e.key === 'Enter' && handleSearch()}
                placeholder="搜索事件标题、CVE..."
                className="flex-1 bg-transparent text-sm text-gray-900 text-gray-900 outline-none placeholder:text-gray-500 placeholder:text-gray-500"
              />
            </div>
            <button
              onClick={handleSearch}
              className="px-4 py-2 rounded-lg text-sm font-medium bg-gray-100 bg-gray-100 text-gray-700 text-gray-700 border border-gray-200 border-gray-200 hover:border-gray-300 hover:border-[#8B949E]"
            >
              搜索
            </button>
          </div>
        </div>

        {/* 事件列表 */}
        <div className="flex-1 overflow-y-auto min-h-0">
          {loading ? (
            <div className="flex items-center justify-center py-12">
              <Loader2 className="w-6 h-6 text-gray-500 text-gray-500 animate-spin" />
            </div>
          ) : events.length === 0 ? (
            <div className="text-center py-12 text-sm text-gray-500 text-gray-500">暂无事件数据</div>
          ) : (
            events.map(ev => {
              const isSelected = selected.has(ev.id)
              const sev = severityMap[ev.severity] || severityMap.info
              return (
                <button
                  key={ev.id}
                  onClick={() => toggle(ev.id)}
                  className={cn(
                    'w-full flex items-center gap-3 px-5 py-3 text-left transition-colors border-b border-gray-200 border-gray-200/20',
                    isSelected ? 'bg-teal-600/5' : 'hover:bg-gray-50 hover:bg-gray-50',
                  )}
                >
                  <div className={cn(
                    'w-5 h-5 rounded border flex items-center justify-center shrink-0 transition-colors',
                    isSelected ? 'bg-teal-600 border-[#00F0E0]' : 'border-gray-300 border-gray-200',
                  )}>
                    {isSelected && <Check className="w-3.5 h-3.5 text-[#010409]" />}
                  </div>
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <span
                        className="text-xs font-bold px-1.5 py-0.5 rounded"
                        style={{ backgroundColor: sev.color + '20', color: sev.color }}
                      >
                        {sev.label}
                      </span>
                      <span className="text-sm text-gray-900 text-gray-900 truncate">{ev.title}</span>
                    </div>
                    <div className="flex items-center gap-3 mt-0.5">
                      {ev.cve_id && <span className="text-xs text-teal-600 font-mono">{ev.cve_id}</span>}
                      <span className="text-xs text-gray-500 text-gray-500">{formatDate(ev.event_time)}</span>
                    </div>
                  </div>
                </button>
              )
            })
          )}
        </div>

        {/* 底部操作区 */}
        <div className="px-5 py-4 border-t border-gray-200 border-gray-200/50 flex items-center justify-between">
          <span className="text-sm text-gray-500 text-gray-500">
            已选择 <span className="text-teal-600 font-mono">{selected.size}</span> 个事件
          </span>
          <div className="flex items-center gap-2">
            <button
              onClick={onClose}
              className="px-5 py-2 rounded-lg text-sm font-medium text-gray-700 text-gray-700 border border-gray-200 border-gray-200 hover:border-gray-300 hover:border-[#8B949E]"
            >
              取消
            </button>
            <button
              onClick={() => { onConfirm(Array.from(selected)); onClose() }}
              disabled={selected.size === 0}
              className={cn(
                'px-5 py-2 rounded-lg text-sm font-bold transition-all',
                selected.size > 0
                  ? 'bg-teal-600 text-[#010409] hover:shadow-lg hover:shadow-[#00F0E0]/20'
                  : 'bg-gray-200 bg-gray-100 text-gray-500 text-gray-500 cursor-not-allowed',
              )}
            >
              确认选择
            </button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}
