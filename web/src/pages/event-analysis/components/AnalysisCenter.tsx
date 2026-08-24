import { cn } from '@/utils'
import { Database, Filter, Shield } from 'lucide-react'

interface Log { agent: string; status: string; message: string; data?: Record<string, unknown> }
interface Props { logs: Log[] }

const agentConfig: Record<string, { icon: typeof Database; color: string }> = {
  '数据采集Agent': { icon: Database, color: 'text-[#58A6FF]' },
  '提取Agent': { icon: Filter, color: 'text-[#A371F7]' },
  '去重Agent': { icon: Filter, color: 'text-[#3FB950]' },
  '风险评估Agent': { icon: Shield, color: 'text-[#F85149]' },
}

export default function AnalysisCenter({ logs }: Props) {
  const grouped = logs.reduce((acc, log) => {
    if (!acc[log.agent]) acc[log.agent] = []
    acc[log.agent].push(log)
    return acc
  }, {} as Record<string, Log[]>)

  const agents = Object.keys(grouped)

  if (agents.length === 0) {
    return <div className="flex-1 flex items-center justify-center bg-[#010409] text-[#8B949E] text-sm">点击启动开始分析</div>
  }

  return (
    <div className="flex-1 p-3 overflow-auto bg-[#010409] space-y-3">
      {agents.map(agent => {
        const agentLogs = grouped[agent]
        const completeLog = agentLogs.find(l => l.status === 'success')
        const data = completeLog?.data as Record<string, unknown> | undefined
        const config = agentConfig[agent] || { icon: Database, color: 'text-[#8B949E]' }
        const Icon = config.icon
        const isRisk = agent === '风险评估Agent'

        return (
          <div key={agent} className={cn('rounded border p-3', isRisk ? 'border-[#EF4444]/40 bg-[#EF4444]/5' : 'border-[#30363D] bg-[#0D1117]')}>
            <div className="flex items-center gap-2 mb-2">
              <Icon className={cn('w-4 h-4', config.color)} />
              <span className="text-xs font-bold text-[#E6EDF3]">{agent.replace('Agent', '')}</span>
              {completeLog && <span className="text-[10px] text-[#3FB950]">✓</span>}
            </div>

            {/* 最新消息 */}
            <div className="text-[11px] text-[#8B949E] mb-2">{agentLogs[agentLogs.length - 1]?.message}</div>

            {/* 数据采集 */}
            {agent === '数据采集Agent' && data && (
              <div className="flex gap-2 text-xs">
                <span className="px-2 py-0.5 bg-[#58A6FF]/20 text-[#58A6FF] rounded">{data.count as number} 事件</span>
                <span className="text-[#8B949E]">{(data.sources as string[])?.join(' / ')}</span>
              </div>
            )}

            {/* 提取 */}
            {agent === '提取Agent' && data && (
              <div className="flex gap-1 flex-wrap">
                {Object.entries((data.severity as Record<string, number>) || {}).filter(([,v]) => v > 0).map(([k, v]) => (
                  <span key={k} className={cn('px-1.5 py-0.5 rounded text-[10px]', k === 'critical' ? 'bg-[#F85149]/20 text-[#F85149]' : k === 'high' ? 'bg-[#F0883E]/20 text-[#F0883E]' : 'bg-[#30363D] text-[#8B949E]')}>{k}:{v}</span>
                ))}
              </div>
            )}

            {/* 风险评估 */}
            {isRisk && data && (
              <div className="grid grid-cols-4 gap-2 text-center text-[10px]">
                <div className="p-1.5 bg-[#0D1117] border border-[#30363D] rounded">
                  <div className="text-base font-mono font-bold text-[#EF4444]">{(data.maxCVSS as number)?.toFixed(1)}</div>
                  <div className="text-[#8B949E]">CVSS</div>
                </div>
                <div className="p-1.5 bg-[#0D1117] border border-[#30363D] rounded">
                  <div className="text-base font-mono font-bold text-[#F97316]">{data.critical as number}</div>
                  <div className="text-[#8B949E]">严重</div>
                </div>
                <div className="p-1.5 bg-[#0D1117] border border-[#30363D] rounded">
                  <div className="text-base font-mono font-bold text-[#F59E0B]">{data.highRisk as number}</div>
                  <div className="text-[#8B949E]">高危</div>
                </div>
                <div className="p-1.5 bg-[#0D1117] border border-[#30363D] rounded">
                  <div className="text-base font-mono font-bold text-[#E6EDF3]">{data.avgRisk as number}</div>
                  <div className="text-[#8B949E]">均分</div>
                </div>
              </div>
            )}
          </div>
        )
      })}
    </div>
  )
}
