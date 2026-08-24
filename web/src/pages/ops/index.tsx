import { useState, useEffect } from 'react'
import { Bot, Play, CheckCircle2, XCircle, TrendingUp } from 'lucide-react'
import { opsService, type OpsStats } from '@/services/ops'
import { cn } from '@/utils'
import RunsPanel from './RunsPanel'

export default function SoarPage() {
  const [stats, setStats] = useState<OpsStats>({ total_runs: 0, success_runs: 0, failed_runs: 0 })

  useEffect(() => {
    opsService.getStats().then(setStats).catch(() => {})
  }, [])

  return (
    <div className="flex flex-col gap-5 pb-8">
      <div className="flex items-center gap-3 flex-shrink-0">
        <div className="p-2 rounded-lg bg-violet-50">
          <Bot className="w-5 h-5 text-violet-600" />
        </div>
        <div>
          <h1 className="text-2xl font-semibold text-gray-900 tracking-tight">AI 智能运维</h1>
          <p className="text-sm text-gray-500 mt-0.5">在安全事件列表点击 ⚡ 触发，AI 自动分析事件并执行通知、封禁等响应动作</p>
        </div>
      </div>

      <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 flex-shrink-0">
        {[
          { label: '总执行次数', value: stats.total_runs, icon: Play, color: 'text-indigo-600 bg-indigo-50' },
          { label: '成功', value: stats.success_runs, icon: CheckCircle2, color: 'text-emerald-600 bg-emerald-50' },
          { label: '失败', value: stats.failed_runs, icon: XCircle, color: 'text-red-600 bg-red-50' },
          {
            label: '成功率',
            value: stats.total_runs > 0 ? `${Math.round((stats.success_runs / stats.total_runs) * 100)}%` : '-',
            icon: TrendingUp,
            color: 'text-violet-600 bg-violet-50',
          },
        ].map(s => (
          <div key={s.label} className="bg-white rounded-xl border border-slate-200 p-4 flex items-center gap-3">
            <div className={cn('p-2 rounded-lg', s.color)}>
              <s.icon className="w-4 h-4" />
            </div>
            <div>
              <p className="text-2xl font-bold text-gray-900">{s.value}</p>
              <p className="text-xs text-gray-500">{s.label}</p>
            </div>
          </div>
        ))}
      </div>

      <RunsPanel />
    </div>
  )
}
