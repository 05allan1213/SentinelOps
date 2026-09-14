import { cn } from '@/utils'
import { Database, Filter, Shield } from 'lucide-react'

interface Log { agent: string; status: string; message: string; data?: Record<string, unknown> }
interface Props { logs: Log[] }

const agentConfig: Record<string, { icon: typeof Database; color: string }> = {
  '数据采集Agent': { icon: Database, color: 'text-blue-600' },
  '提取Agent': { icon: Filter, color: 'text-purple-600' },
  '去重Agent': { icon: Filter, color: 'text-green-600' },
  '风险评估Agent': { icon: Shield, color: 'text-red-600' },
}

export default function AnalysisCenter({ logs }: Props) {
  const grouped = logs.reduce((acc, log) => {
    if (!acc[log.agent]) acc[log.agent] = []
    acc[log.agent].push(log)
    return acc
  }, {} as Record<string, Log[]>)

  const agents = Object.keys(grouped)

  if (agents.length === 0) {
    return <div className="flex-1 flex items-center justify-center bg-gray-50 text-gray-500 text-sm">点击启动开始分析</div>
  }

  return (
    <div className="flex-1 p-3 overflow-auto bg-gray-50 space-y-3">
      {agents.map(agent => {
        const agentLogs = grouped[agent]
        const completeLog = agentLogs.find(l => l.status === 'success')
        const data = completeLog?.data as Record<string, unknown> | undefined
        const config = agentConfig[agent] || { icon: Database, color: 'text-gray-500' }
        const Icon = config.icon
        const isRisk = agent === '风险评估Agent'

        return (
          <div key={agent} className={cn('rounded border p-3', isRisk ? 'border-[#EF4444]/40 bg-[#EF4444]/5' : 'border-gray-200 bg-white')}>
            <div className="flex items-center gap-2 mb-2">
              <Icon className={cn('w-4 h-4', config.color)} />
              <span className="text-xs font-bold text-gray-900">{agent.replace('Agent', '')}</span>
              {completeLog && <span className="text-[10px] text-green-600">✓</span>}
            </div>

            {/* 最新消息 */}
            <div className="text-[11px] text-gray-500 mb-2">{agentLogs[agentLogs.length - 1]?.message}</div>

            {/* 数据采集 */}
            {agent === '数据采集Agent' && data && (
              <div className="flex gap-2 text-xs">
                <span className="px-2 py-0.5 bg-blue-600/20 text-blue-600 rounded">{data.count as number} 事件</span>
                <span className="text-gray-500">{(data.sources as string[])?.join(' / ')}</span>
              </div>
            )}

            {/* 提取 */}
            {agent === '提取Agent' && data && (
              <div className="flex gap-1 flex-wrap">
                {Object.entries((data.severity as Record<string, number>) || {}).filter(([,v]) => v > 0).map(([k, v]) => (
                  <span key={k} className={cn('px-1.5 py-0.5 rounded text-[10px]', k === 'critical' ? 'bg-red-600/20 text-red-600' : k === 'high' ? 'bg-orange-600/20 text-orange-600' : 'bg-gray-100 text-gray-500')}>{k}:{v}</span>
                ))}
              </div>
            )}

            {/* 风险评估 */}
            {isRisk && data && (
              <div className="grid grid-cols-4 gap-2 text-center text-[10px]">
                <div className="p-1.5 bg-white border border-gray-200 rounded">
                  <div className="text-base font-mono font-bold text-[#EF4444]">{(data.maxCVSS as number)?.toFixed(1)}</div>
                  <div className="text-gray-500">CVSS</div>
                </div>
                <div className="p-1.5 bg-white border border-gray-200 rounded">
                  <div className="text-base font-mono font-bold text-orange-600">{data.critical as number}</div>
                  <div className="text-gray-500">严重</div>
                </div>
                <div className="p-1.5 bg-white border border-gray-200 rounded">
                  <div className="text-base font-mono font-bold text-amber-600">{data.highRisk as number}</div>
                  <div className="text-gray-500">高危</div>
                </div>
                <div className="p-1.5 bg-white border border-gray-200 rounded">
                  <div className="text-base font-mono font-bold text-gray-900">{data.avgRisk as number}</div>
                  <div className="text-gray-500">均分</div>
                </div>
              </div>
            )}
          </div>
        )
      })}
    </div>
  )
}
