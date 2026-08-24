import { useState, useEffect } from 'react'
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
  const [loading, setLoading] = useState(false)
  const [selected, setSelected] = useState<Set<string>>(new Set(selectedIds))
  const [keyword, setKeyword] = useState('')

  useEffect(() => {
    if (visible) {
      setSelected(new Set(selectedIds))
      loadEvents()
    }
  }, [visible])

  const loadEvents = async (kw?: string) => {
    setLoading(true)
    try {
      const res = await eventService.list({ page: 1, size: 50, keyword: kw })
      setEvents(res.list)
    } catch { /* ignore */ }
    setLoading(false)
  }

  const toggle = (id: string) => {
    setSelected(prev => {
      const next = new Set(prev)
      next.has(id) ? next.delete(id) : next.add(id)
      return next
    })
  }

  const handleSearch = () => loadEvents(keyword || undefined)

  if (!visible) return null

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm" onClick={onClose}>
      <div
        className="w-[700px] max-h-[70vh] rounded-2xl border border-gray-200 dark:border-[#30363D] bg-white dark:bg-[#0D1117] flex flex-col overflow-hidden shadow-2xl"
        onClick={e => e.stopPropagation()}
      >
        {/* Header */}
        <div className="px-5 py-4 border-b border-gray-200 dark:border-[#30363D]/50 flex items-center justify-between">
          <span className="text-base font-semibold text-gray-900 dark:text-[#E6EDF3]">选择要分析的事件</span>
          <button onClick={onClose} className="text-gray-500 dark:text-[#8B949E] hover:text-gray-900 dark:hover:text-[#E6EDF3] transition-colors">
            <X className="w-5 h-5" />
          </button>
        </div>

        {/* Search */}
        <div className="px-5 py-3 border-b border-gray-200 dark:border-[#30363D]/30">
          <div className="flex items-center gap-2">
            <div className="flex-1 flex items-center gap-2 px-4 py-2 rounded-lg bg-gray-50 dark:bg-[#161B22] border border-gray-200 dark:border-[#30363D]">
              <Search className="w-4 h-4 text-gray-500 dark:text-[#8B949E]" />
              <input
                value={keyword}
                onChange={e => setKeyword(e.target.value)}
                onKeyDown={e => e.key === 'Enter' && handleSearch()}
                placeholder="搜索事件标题、CVE..."
                className="flex-1 bg-transparent text-sm text-gray-900 dark:text-[#E6EDF3] outline-none placeholder:text-gray-500 dark:placeholder:text-[#8B949E]"
              />
            </div>
            <button
              onClick={handleSearch}
              className="px-4 py-2 rounded-lg text-sm font-medium bg-gray-100 dark:bg-[#21262D] text-gray-700 dark:text-[#C9D1D9] border border-gray-200 dark:border-[#30363D] hover:border-gray-300 dark:hover:border-[#8B949E]"
            >
              搜索
            </button>
          </div>
        </div>

        {/* Event List */}
        <div className="flex-1 overflow-y-auto min-h-0">
          {loading ? (
            <div className="flex items-center justify-center py-12">
              <Loader2 className="w-6 h-6 text-gray-500 dark:text-[#8B949E] animate-spin" />
            </div>
          ) : events.length === 0 ? (
            <div className="text-center py-12 text-sm text-gray-500 dark:text-[#8B949E]">暂无事件数据</div>
          ) : (
            events.map(ev => {
              const isSelected = selected.has(ev.id)
              const sev = severityMap[ev.severity] || severityMap.info
              return (
                <button
                  key={ev.id}
                  onClick={() => toggle(ev.id)}
                  className={cn(
                    'w-full flex items-center gap-3 px-5 py-3 text-left transition-colors border-b border-gray-200 dark:border-[#30363D]/20',
                    isSelected ? 'bg-[#00F0E0]/5' : 'hover:bg-gray-50 dark:hover:bg-[#161B22]',
                  )}
                >
                  <div className={cn(
                    'w-5 h-5 rounded border flex items-center justify-center shrink-0 transition-colors',
                    isSelected ? 'bg-[#00F0E0] border-[#00F0E0]' : 'border-gray-300 dark:border-[#30363D]',
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
                      <span className="text-sm text-gray-900 dark:text-[#E6EDF3] truncate">{ev.title}</span>
                    </div>
                    <div className="flex items-center gap-3 mt-0.5">
                      {ev.cve_id && <span className="text-xs text-[#00F0E0] font-mono">{ev.cve_id}</span>}
                      <span className="text-xs text-gray-500 dark:text-[#8B949E]">{formatDate(ev.event_time)}</span>
                    </div>
                  </div>
                </button>
              )
            })
          )}
        </div>

        {/* Footer */}
        <div className="px-5 py-4 border-t border-gray-200 dark:border-[#30363D]/50 flex items-center justify-between">
          <span className="text-sm text-gray-500 dark:text-[#8B949E]">
            已选择 <span className="text-[#00F0E0] font-mono">{selected.size}</span> 个事件
          </span>
          <div className="flex items-center gap-2">
            <button
              onClick={onClose}
              className="px-5 py-2 rounded-lg text-sm font-medium text-gray-700 dark:text-[#C9D1D9] border border-gray-200 dark:border-[#30363D] hover:border-gray-300 dark:hover:border-[#8B949E]"
            >
              取消
            </button>
            <button
              onClick={() => { onConfirm(Array.from(selected)); onClose() }}
              disabled={selected.size === 0}
              className={cn(
                'px-5 py-2 rounded-lg text-sm font-bold transition-all',
                selected.size > 0
                  ? 'bg-[#00F0E0] text-[#010409] hover:shadow-lg hover:shadow-[#00F0E0]/20'
                  : 'bg-gray-200 dark:bg-[#21262D] text-gray-500 dark:text-[#8B949E] cursor-not-allowed',
              )}
            >
              确认选择
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
