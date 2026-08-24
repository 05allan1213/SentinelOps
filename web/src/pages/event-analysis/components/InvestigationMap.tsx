import { cn } from '@/utils'
import { Database, Filter, Shield, CheckCircle, Loader2 } from 'lucide-react'
import { AgentLog } from '@/types/agent'

interface Props {
  logs: AgentLog[]
  selected: string | null
  onSelect: (agent: string) => void
}

const agents = [
  { id: '数据采集Agent', icon: Database, color: '#58A6FF' },
  { id: '提取Agent', icon: Filter, color: '#A371F7' },
  { id: '去重Agent', icon: Filter, color: '#3FB950' },
  { id: '风险评估Agent', icon: Shield, color: '#F85149' },
]

export default function InvestigationMap({ logs, selected, onSelect }: Props) {
  const getStatus = (id: string) => {
    const agentLogs = logs.filter(l => l.agent === id)
    if (agentLogs.some(l => l.status === 'success')) return 'completed'
    if (agentLogs.some(l => l.status === 'running')) return 'running'
    return 'pending'
  }

  return (
    <div className="w-[200px] border-r border-[#30363D] bg-[#0D1117] p-3 flex flex-col">
      <div className="text-xs font-bold text-[#E6EDF3] mb-4">研判路径</div>
      <div className="flex-1 relative">
        {/* 连接线 */}
        <div className="absolute left-5 top-6 bottom-6 w-px bg-[#30363D]" />

        {agents.map((agent) => {
          const status = getStatus(agent.id)
          const Icon = agent.icon
          const isSelected = selected === agent.id

          return (
            <button
              key={agent.id}
              onClick={() => onSelect(agent.id)}
              className={cn(
                'relative w-full flex items-center gap-3 p-2 rounded mb-2 transition-all',
                isSelected ? 'bg-[#30363D]' : 'hover:bg-[#161B22]'
              )}
            >
              <div className={cn(
                'w-6 h-6 rounded-full flex items-center justify-center z-10',
                status === 'completed' ? 'bg-[#3FB950]/20' : 'bg-[#0D1117] border border-[#30363D]'
              )}>
                {status === 'running' ? (
                  <Loader2 className="w-3 h-3 animate-spin" style={{ color: agent.color }} />
                ) : status === 'completed' ? (
                  <CheckCircle className="w-3 h-3 text-[#3FB950]" />
                ) : (
                  <Icon className="w-3 h-3" style={{ color: agent.color }} />
                )}
              </div>
              <span className={cn('text-xs', isSelected ? 'text-[#E6EDF3]' : 'text-[#8B949E]')}>
                {agent.id.replace('Agent', '')}
              </span>
            </button>
          )
        })}
      </div>
    </div>
  )
}
