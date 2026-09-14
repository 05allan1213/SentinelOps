import { Copy, Eye } from 'lucide-react'
import { AgentLog } from '@/types/agent'
import toast from 'react-hot-toast'

interface Props {
  selected: string | null
  logs: AgentLog[]
}

export default function MitigationConsole({ logs }: Props) {
  const riskLog = logs.find(l => l.agent === '风险评估Agent' && l.status === 'success')
  const events = riskLog?.data?.events || []

  const ruleYaml = events.length > 0 ? `# 自动生成的防御规则
rules:
${events.slice(0, 3).map(e => `  - id: block_${e.cve_id}
    action: alert
    cve: "${e.cve_id}"
    severity: ${e.severity}`).join('\n')}` : ''

  const copyRule = () => {
    navigator.clipboard.writeText(ruleYaml)
    toast.success('已复制')
  }

  return (
    <div className="w-[320px] border-l border-gray-200 bg-white flex flex-col">
      <div className="p-3 border-b border-gray-200">
        <div className="flex items-center gap-1.5 text-xs font-bold text-gray-900"><Eye className="w-3.5 h-3.5" />Proposal Preview</div>
      </div>

      <div className="flex-1 p-3 overflow-auto">
        {ruleYaml ? (
          <div className="relative">
            <button onClick={copyRule} className="absolute top-2 right-2 p-1 hover:bg-gray-100 rounded">
              <Copy className="w-3 h-3 text-gray-500" />
            </button>
            <pre className="text-[11px] font-mono text-gray-500 bg-gray-50 p-3 rounded border border-gray-200 overflow-x-auto">
              {ruleYaml}
            </pre>
          </div>
        ) : (
          <div className="text-gray-500 text-xs text-center py-8">完成分析后生成防御规则</div>
        )}
      </div>

      <div className="p-3 border-t border-gray-200">
        <div className="rounded border border-gray-200 bg-gray-50 px-3 py-2 text-center text-[10px] text-gray-500">仅 Proposal Preview；未连接 Effect 执行，也不会显示成功。</div>
      </div>
    </div>
  )
}
