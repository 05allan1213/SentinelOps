import { useState } from 'react'
import { MessageCircle, Database, Filter, Shield, FileText, CheckCircle, Edit3 } from 'lucide-react'
import { cn } from '@/utils'
import toast from 'react-hot-toast'

interface Log {
  agent: string
  status: string
  message: string
  data?: Record<string, unknown>
}

interface Props {
  logs: Log[]
  selectedNode: string | null
  onSelectNode: (node: string | null) => void
}

const nodeConfig: Record<string, { icon: typeof Database; label: string; color: string }> = {
  '数据采集Agent': { icon: Database, label: '情报采集', color: 'bg-blue-500' },
  '信息提取Agent': { icon: Filter, label: '信息提取', color: 'bg-purple-500' },
  '去重Agent': { icon: Filter, label: '聚合去重', color: 'bg-cyan-500' },
  '风险评估Agent': { icon: Shield, label: '风险评估', color: 'bg-orange-500' },
  '报告生成Agent': { icon: FileText, label: '报告生成', color: 'bg-green-500' },
}

export default function ReasoningCanvas({ logs, selectedNode, onSelectNode }: Props) {
  const [confirmed, setConfirmed] = useState<Record<string, boolean>>({})

  const groupedLogs = logs.reduce((acc, log) => {
    if (!acc[log.agent]) acc[log.agent] = []
    acc[log.agent].push(log)
    return acc
  }, {} as Record<string, Log[]>)

  const agents = Object.keys(groupedLogs)

  if (agents.length === 0) {
    return <div className="flex items-center justify-center h-full text-gray-500">点击"启动分析"开始情报研判</div>
  }

  return (
    <div className="space-y-2">
      {agents.map((agent, i) => {
        const config = nodeConfig[agent] || { icon: Database, label: agent, color: 'bg-gray-500' }
        const Icon = config.icon
        const agentLogs = groupedLogs[agent]
        const lastLog = agentLogs[agentLogs.length - 1]
        const isSelected = selectedNode === agent
        const isSuccess = lastLog.status === 'success'

        return (
          <div key={agent}>
            {/* 节点卡片 */}
            <div
              onClick={() => onSelectNode(isSelected ? null : agent)}
              className={cn(
                'p-4 rounded-lg border cursor-pointer transition-all',
                isSelected ? 'bg-gray-800 border-primary-500' : 'bg-gray-800/50 border-gray-700 hover:border-gray-600'
              )}
            >
              <div className="flex items-center gap-3">
                <div className={cn('w-10 h-10 rounded-lg flex items-center justify-center', config.color)}>
                  <Icon className="w-5 h-5 text-white" />
                </div>
                <div className="flex-1">
                  <div className="flex items-center gap-2">
                    <span className="font-medium text-white">{config.label}</span>
                    {isSuccess && <span className="text-xs px-2 py-0.5 bg-green-500/20 text-green-400 rounded">完成</span>}
                  </div>
                  <p className="text-sm text-gray-400 mt-0.5">{lastLog.message}</p>
                </div>
                <button className="text-gray-500 hover:text-primary-400" title="AI解释">
                  <MessageCircle className="w-4 h-4" />
                </button>
              </div>

              {/* 风险评估人机确认 */}
              {agent === '风险评估Agent' && isSuccess && !confirmed[agent] && (
                <div className="mt-3 pt-3 border-t border-gray-700 flex gap-2">
                  <button
                    onClick={(e) => { e.stopPropagation(); setConfirmed({ ...confirmed, [agent]: true }); toast.success('已确认评估结果'); }}
                    className="flex-1 btn-primary text-xs py-1.5"
                  >
                    <CheckCircle className="w-3 h-3" /> 确认评估
                  </button>
                  <button onClick={(e) => e.stopPropagation()} className="flex-1 btn-default text-xs py-1.5">
                    <Edit3 className="w-3 h-3" /> 调整评分
                  </button>
                </div>
              )}
              {agent === '风险评估Agent' && confirmed[agent] && (
                <div className="mt-2 text-xs text-green-400 flex items-center gap-1">
                  <CheckCircle className="w-3 h-3" /> 已人工确认
                </div>
              )}
            </div>

            {/* 连接线 */}
            {i < agents.length - 1 && (
              <div className="flex justify-center py-1">
                <div className="w-0.5 h-6 bg-gradient-to-b from-primary-500 to-primary-500/30 animate-pulse" />
              </div>
            )}
          </div>
        )
      })}
    </div>
  )
}
