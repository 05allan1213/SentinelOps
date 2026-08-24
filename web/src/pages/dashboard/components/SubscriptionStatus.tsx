import { useState, useEffect } from 'react'
import { CheckCircle, AlertCircle, PauseCircle, Loader2 } from 'lucide-react'
import { cn, formatRelativeTime, getSourceTypeLabel } from '@/utils'
import { subscriptionService } from '@/services/subscription'
import type { Subscription } from '@/types'

const statusConfig = {
  active: {
    icon: CheckCircle,
    color: 'text-success-500',
    label: '运行中',
  },
  paused: {
    icon: PauseCircle,
    color: 'text-warning-500',
    label: '已暂停',
  },
  disabled: {
    icon: AlertCircle,
    color: 'text-danger-500',
    label: '已禁用',
  },
}

export default function SubscriptionStatus({ refreshKey }: { refreshKey?: number }) {
  const [subscriptions, setSubscriptions] = useState<Subscription[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    const fetchSubscriptions = async () => {
      setLoading(true)
      try {
        const res = await subscriptionService.list(1, 5)
        setSubscriptions(res.list || [])
      } catch (error) {
        console.error('获取订阅列表失败:', error)
      } finally {
        setLoading(false)
      }
    }
    fetchSubscriptions()
  }, [refreshKey])

  if (loading) {
    return (
      <div className="flex items-center justify-center py-8">
        <Loader2 className="w-6 h-6 animate-spin text-primary-500" />
      </div>
    )
  }

  if (subscriptions.length === 0) {
    return (
      <div className="text-center py-8 text-gray-500">
        暂无订阅
      </div>
    )
  }

  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-y border-slate-100 bg-slate-50">
            <th className="px-5 py-2.5 text-left text-xs font-semibold text-slate-500 uppercase tracking-wider">名称</th>
            <th className="px-5 py-2.5 text-left text-xs font-semibold text-slate-500 uppercase tracking-wider whitespace-nowrap">类型</th>
            <th className="px-5 py-2.5 text-left text-xs font-semibold text-slate-500 uppercase tracking-wider whitespace-nowrap">状态</th>
            <th className="px-5 py-2.5 text-left text-xs font-semibold text-slate-500 uppercase tracking-wider whitespace-nowrap">上次抓取</th>
            <th className="px-5 py-2.5 text-center text-xs font-semibold text-slate-500 uppercase tracking-wider whitespace-nowrap">事件数</th>
          </tr>
        </thead>
        <tbody>
          {subscriptions.map((sub) => {
            const config = statusConfig[sub.status as keyof typeof statusConfig] || statusConfig.active
            const StatusIcon = config.icon
            return (
              <tr key={sub.id} className="border-b border-slate-50 hover:bg-slate-50 cursor-pointer transition-colors last:border-b-0">
                <td className="px-5 py-3 min-w-0 max-w-0 w-full">
                  <span className="block truncate text-xs font-medium text-slate-800">{sub.name}</span>
                </td>
                <td className="px-5 py-3 whitespace-nowrap">
                  <span className="inline-flex items-center h-5 px-2 text-xs font-medium rounded bg-slate-100 text-slate-600">{getSourceTypeLabel(sub.source_type)}</span>
                </td>
                <td className="px-5 py-3 whitespace-nowrap">
                  <span className={cn('inline-flex items-center gap-1 text-xs font-medium', config.color)}>
                    <StatusIcon className="w-3 h-3 flex-shrink-0" />
                    {config.label}
                  </span>
                </td>
                <td className="px-5 py-3 whitespace-nowrap text-xs text-slate-400 tabular-nums">
                  {sub.last_fetch_at ? formatRelativeTime(sub.last_fetch_at) : '-'}
                </td>
                <td className="px-5 py-3 whitespace-nowrap text-center text-xs font-semibold tabular-nums text-slate-700">{sub.total_events || 0}</td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}
