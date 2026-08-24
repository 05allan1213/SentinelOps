import { GitBranch, Database, Shield, Brain } from 'lucide-react'

interface Props {
  visible: boolean
}

const traceData = [
  { agent: '数据采集', icon: Database, color: 'text-blue-400', desc: '从订阅源获取原始事件' },
  { agent: '提取Agent', icon: GitBranch, color: 'text-green-400', desc: '提取CVE、厂商、产品信息' },
  { agent: '去重Agent', icon: Shield, color: 'text-yellow-400', desc: '语义相似度>0.85则合并' },
  { agent: '风险评估', icon: Brain, color: 'text-red-400', desc: '基于CVSS+资产匹配评分' },
]

export default function AgentTraceGraph({ visible }: Props) {
  if (!visible) return null

  return (
    <div className="mt-4 p-4 bg-gray-800/50 rounded-lg border border-gray-700">
      <div className="text-sm font-medium text-gray-300 mb-3">Agent 逻辑溯源</div>
      <div className="flex items-center justify-between">
        {traceData.map((item, i) => (
          <div key={item.agent} className="flex items-center">
            <div className="text-center">
              <div className={`w-10 h-10 rounded-full bg-gray-700 flex items-center justify-center ${item.color}`}>
                <item.icon className="w-5 h-5" />
              </div>
              <div className="text-xs text-gray-400 mt-1">{item.agent}</div>
              <div className="text-xs text-gray-500 mt-0.5 max-w-[80px]">{item.desc}</div>
            </div>
            {i < traceData.length - 1 && (
              <div className="w-8 h-0.5 bg-gray-600 mx-2" />
            )}
          </div>
        ))}
      </div>
    </div>
  )
}
