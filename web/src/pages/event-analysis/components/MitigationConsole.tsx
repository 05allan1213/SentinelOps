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
    <div className="w-[320px] border-l border-[#30363D] bg-[#0D1117] flex flex-col">
      <div className="p-3 border-b border-[#30363D]">
        <div className="flex items-center gap-1.5 text-xs font-bold text-[#E6EDF3]"><Eye className="w-3.5 h-3.5" />Proposal Preview</div>
      </div>

      <div className="flex-1 p-3 overflow-auto">
        {ruleYaml ? (
          <div className="relative">
            <button onClick={copyRule} className="absolute top-2 right-2 p-1 hover:bg-[#30363D] rounded">
              <Copy className="w-3 h-3 text-[#8B949E]" />
            </button>
            <pre className="text-[11px] font-mono text-[#8B949E] bg-[#010409] p-3 rounded border border-[#30363D] overflow-x-auto">
              {ruleYaml}
            </pre>
          </div>
        ) : (
          <div className="text-[#8B949E] text-xs text-center py-8">完成分析后生成防御规则</div>
        )}
      </div>

      <div className="p-3 border-t border-[#30363D]">
        <div className="rounded border border-[#30363D] bg-[#161B22] px-3 py-2 text-center text-[10px] text-[#8B949E]">仅 Proposal Preview；未连接 Effect 执行，也不会显示成功。</div>
      </div>
    </div>
  )
}
