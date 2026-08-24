import { useState } from 'react'
import { ShieldOff, Copy, Check } from 'lucide-react'
import { AgentLog } from '@/types/agent'
import toast from 'react-hot-toast'

interface Props {
  selected: string | null
  logs: AgentLog[]
}

export default function MitigationConsole({ logs }: Props) {
  const [holding, setHolding] = useState(false)
  const [holdProgress, setHoldProgress] = useState(0)

  const riskLog = logs.find(l => l.agent === '风险评估Agent' && l.status === 'success')
  const events = riskLog?.data?.events || []

  const ruleYaml = events.length > 0 ? `# 自动生成的防御规则
rules:
${events.slice(0, 3).map(e => `  - id: block_${e.cve_id}
    action: alert
    cve: "${e.cve_id}"
    severity: ${e.severity}`).join('\n')}` : ''

  const handleHoldStart = () => {
    setHolding(true)
    let progress = 0
    const interval = setInterval(() => {
      progress += 5
      setHoldProgress(progress)
      if (progress >= 100) {
        clearInterval(interval)
        toast.success('规则已应用')
        setHolding(false)
        setHoldProgress(0)
      }
    }, 50)
  }

  const copyRule = () => {
    navigator.clipboard.writeText(ruleYaml)
    toast.success('已复制')
  }

  return (
    <div className="w-[320px] border-l border-[#30363D] bg-[#0D1117] flex flex-col">
      <div className="p-3 border-b border-[#30363D]">
        <div className="text-xs font-bold text-[#E6EDF3]">防御沙盒</div>
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
        <button
          onMouseDown={handleHoldStart}
          onMouseUp={() => { setHolding(false); setHoldProgress(0) }}
          onMouseLeave={() => { setHolding(false); setHoldProgress(0) }}
          disabled={!ruleYaml}
          className="w-full relative py-2 bg-[#F85149] rounded text-white text-xs font-bold flex items-center justify-center gap-2 disabled:opacity-50 overflow-hidden"
        >
          {holding && (
            <div className="absolute inset-0 bg-[#da3633]" style={{ width: `${holdProgress}%` }} />
          )}
          <span className="relative flex items-center gap-2">
            {holdProgress >= 100 ? <Check className="w-4 h-4" /> : <ShieldOff className="w-4 h-4" />}
            {holding ? '长按确认...' : '应用规则'}
          </span>
        </button>
        <div className="text-[10px] text-[#8B949E] text-center mt-2">长按按钮确认执行</div>
      </div>
    </div>
  )
}
