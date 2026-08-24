import { CheckCircle2, Loader2, Circle } from 'lucide-react'

type StepStatus = 'pending' | 'running' | 'success' | 'error'

interface AgentLog {
  agent: string
  status: 'running' | 'success' | 'error'
}

interface StepDef {
  agentKey: string
  label: string
  en: string
  colorHex: string       // 用于 inline style 的原始色值
  runningBg: string
  runningIcon: string
  runningRing: string
  doneBg: string
  lineActive: string
}

const STEPS: StepDef[] = [
  {
    agentKey: '数据采集',
    label: '数据采集', en: 'Data Collection',
    colorHex: '#10b981',
    runningBg: 'bg-emerald-50', runningIcon: 'text-emerald-500', runningRing: 'ring-emerald-300',
    doneBg: 'bg-emerald-500', lineActive: 'bg-emerald-400',
  },
  {
    agentKey: '提取',
    label: '智能提取', en: 'Extraction',
    colorHex: '#8b5cf6',
    runningBg: 'bg-violet-50', runningIcon: 'text-violet-500', runningRing: 'ring-violet-300',
    doneBg: 'bg-violet-500', lineActive: 'bg-violet-400',
  },
  {
    agentKey: '去重',
    label: '去重过滤', en: 'Deduplication',
    colorHex: '#06b6d4',
    runningBg: 'bg-cyan-50', runningIcon: 'text-cyan-500', runningRing: 'ring-cyan-300',
    doneBg: 'bg-cyan-500', lineActive: 'bg-cyan-400',
  },
  {
    agentKey: '风险评估',
    label: '风险评估', en: 'Risk Assessment',
    colorHex: '#f43f5e',
    runningBg: 'bg-rose-50', runningIcon: 'text-rose-500', runningRing: 'ring-rose-300',
    doneBg: 'bg-rose-500', lineActive: 'bg-rose-400',
  },
  {
    agentKey: '解决方案',
    label: '解决方案', en: 'Solution',
    colorHex: '#f59e0b',
    runningBg: 'bg-amber-50', runningIcon: 'text-amber-500', runningRing: 'ring-amber-300',
    doneBg: 'bg-amber-500', lineActive: 'bg-amber-400',
  },
]

function getStatus(agentKey: string, logs: AgentLog[]): StepStatus {
  const matching = logs.filter(l => l.agent.includes(agentKey))
  if (!matching.length) return 'pending'
  const last = matching[matching.length - 1]
  return last.status as StepStatus
}

interface Props {
  logs: AgentLog[]
  isProcessing: boolean
}

export default function AgentStatusBar({ logs, isProcessing }: Props) {
  const statuses = STEPS.map(s => getStatus(s.agentKey, logs))

  return (
    <div className="flex items-center justify-center gap-0 px-8 py-3 bg-white border-b border-gray-100">
      {STEPS.map((step, i) => {
        const status = statuses[i]
        const prevDone = i === 0 || statuses[i - 1] === 'success'

        // 图标区样式
        let boxClass = `w-11 h-11 rounded-xl flex items-center justify-center transition-all duration-300 `
        let boxStyle: React.CSSProperties = {}
        let iconNode: React.ReactNode
        if (status === 'success') {
          boxClass += step.doneBg + ' shadow-sm'
          iconNode = <CheckCircle2 className="w-5 h-5 text-white" />
        } else if (status === 'running') {
          boxClass += step.runningBg + ' ring-2 ' + step.runningRing + ' shadow-md'
          iconNode = <Loader2 className={`w-5 h-5 ${step.runningIcon} animate-spin`} />
        } else {
          // pending：用各自的色调（低透明度），而非统一灰色
          boxStyle = {
            backgroundColor: step.colorHex + '12',  // ~7% opacity
            border: `1.5px solid ${step.colorHex}35`, // ~21% opacity
          }
          iconNode = <Circle className="w-5 h-5" style={{ color: step.colorHex + '80' }} />
        }

        // 标签颜色
        const labelClass = status === 'pending'
          ? 'text-gray-500'
          : status === 'running'
            ? 'text-gray-800 font-semibold'
            : 'text-gray-700 font-medium'

        const enClass = status === 'pending' ? 'text-gray-400' : 'text-gray-400'

        return (
          <div key={step.agentKey} className="flex items-center">
            {/* 步骤节点 */}
            <div className="flex flex-col items-center gap-1.5 w-24">
              <div className={boxClass} style={boxStyle}>{iconNode}</div>
              <span className={`text-xs leading-tight ${labelClass}`}>{step.label}</span>
              <span className={`text-[10px] leading-tight ${enClass}`}>{step.en}</span>
            </div>

            {/* 连接线（最后一个节点后没有） */}
            {i < STEPS.length - 1 && (
              <div className="flex items-center w-12 -mt-5">
                {/* 虚线：前一步完成用彩色实线，否则灰色虚线 */}
                <div
                  className={`h-px w-full transition-all duration-500 ${
                    prevDone && status !== 'pending'
                      ? step.lineActive
                      : 'border-t border-dashed border-gray-300'
                  } ${!(prevDone && status !== 'pending') ? 'bg-transparent' : ''}`}
                />
              </div>
            )}
          </div>
        )
      })}

      {/* 空闲状态提示 */}
      {!isProcessing && logs.length === 0 && (
        <span className="ml-4 text-xs text-gray-300 italic">等待分析启动...</span>
      )}
    </div>
  )
}
