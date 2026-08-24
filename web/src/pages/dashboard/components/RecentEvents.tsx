import { useState, useEffect } from 'react'
import { ExternalLink, Loader2 } from 'lucide-react'
import { formatDate } from '@/utils'
import { eventService } from '@/services/event'
import type { SecurityEvent } from '@/types'

const severityConfig: Record<string, { label: string; class: string }> = {
  critical: { label: '严重', class: 'severity-critical' },
  high: { label: '高危', class: 'severity-high' },
  medium: { label: '中危', class: 'severity-medium' },
  low: { label: '低危', class: 'severity-low' },
  info: { label: '信息', class: 'severity-info' },
}

export default function RecentEvents({ refreshKey }: { refreshKey?: number }) {
  const [events, setEvents] = useState<SecurityEvent[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    const fetchEvents = async () => {
      setLoading(true)
      try {
        const res = await eventService.list({ page: 1, size: 5 })
        setEvents(res.list || [])
      } catch (error) {
        console.error('获取最新事件失败:', error)
      } finally {
        setLoading(false)
      }
    }
    fetchEvents()
  }, [refreshKey])

  if (loading) {
    return (
      <div className="flex items-center justify-center py-8">
        <Loader2 className="w-6 h-6 animate-spin text-primary-500" />
      </div>
    )
  }

  if (events.length === 0) {
    return (
      <div className="text-center py-8 text-gray-500">
        暂无安全事件
      </div>
    )
  }

  return (
    <div className="overflow-x-auto h-full">
      <table className="w-full text-sm h-full">
        <thead>
          <tr className="border-y border-slate-100 bg-slate-50">
            <th className="px-5 py-2.5 text-left text-xs font-semibold text-slate-500 uppercase tracking-wider">事件</th>
            <th className="px-5 py-2.5 text-left text-xs font-semibold text-slate-500 uppercase tracking-wider whitespace-nowrap">级别</th>
            <th className="px-5 py-2.5 text-left text-xs font-semibold text-slate-500 uppercase tracking-wider whitespace-nowrap">来源</th>
            <th className="px-5 py-2.5 text-left text-xs font-semibold text-slate-500 uppercase tracking-wider whitespace-nowrap">时间</th>
          </tr>
        </thead>
        <tbody>
          {events.map((event) => {
            const severity = severityConfig[event.severity] || severityConfig.info
            return (
              <tr key={event.id} className="border-b border-slate-50 hover:bg-slate-50 cursor-pointer group transition-colors last:border-b-0">
                <td className="px-5 py-3 text-slate-700 min-w-0 max-w-0 w-full">
                  <div className="flex items-center gap-2 min-w-0">
                    <span className="truncate text-slate-800 text-xs font-medium">{event.title}</span>
                    <ExternalLink className="w-3 h-3 text-slate-400 opacity-0 group-hover:opacity-100 transition-opacity flex-shrink-0" />
                  </div>
                </td>
                <td className="px-5 py-3 whitespace-nowrap">
                  <span className={severity.class}>{severity.label}</span>
                </td>
                <td className="px-5 py-3 whitespace-nowrap">
                  <span className="inline-flex items-center h-5 px-2 text-xs font-medium rounded bg-slate-100 text-slate-600">{event.source || '-'}</span>
                </td>
                <td className="px-5 py-3 whitespace-nowrap text-xs text-slate-400 tabular-nums">
                  {formatDate(event.created_at, 'MM-DD HH:mm')}
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}
