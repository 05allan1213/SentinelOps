import { useState } from 'react'
import { X, Cpu, CheckCircle, Loader2, Database, Filter, Brain, Save } from 'lucide-react'

interface PipelineStep {
  agent: string
  status: string
  message: string
  count: number
}

interface Props {
  onClose: () => void
  onComplete: () => void
}

const agentIcons: Record<string, typeof Cpu> = {
  '数据采集': Database,
  '提取Agent': Cpu,
  '去重Agent': Filter,
  '风险评估Agent': Brain,
  '数据持久化': Save,
}

export default function AgentPipelineModal({ onClose, onComplete }: Props) {
  const [running, setRunning] = useState(false)
  const [steps, setSteps] = useState<PipelineStep[]>([])
  const [result, setResult] = useState<{ total: number; new: number } | null>(null)
  const [streamContent, setStreamContent] = useState('')

  const runPipeline = async () => {
    setRunning(true)
    setSteps([{ agent: '分析Agent', status: 'running', message: '正在分析待处理事件...', count: 0 }])
    setStreamContent('')
    setResult(null)

    try {
      const response = await fetch('/api/event/v1/pipeline/stream', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ query: '处理并分析所有待处理安全事件' }),
      })
      if (!response.ok) throw new Error(`HTTP ${response.status}`)
      const reader = response.body?.getReader()
      if (!reader) throw new Error('No reader')
      const decoder = new TextDecoder()
      let buffer = ''
      let fullContent = ''
      while (true) {
        const { done, value } = await reader.read()
        if (done) break
        buffer += decoder.decode(value, { stream: true })
        const lines = buffer.split('\n')
        buffer = lines.pop() || ''
        for (const line of lines) {
          if (!line.startsWith('data: ')) continue
          const raw = line.slice(6)
          if (raw === '[DONE]') break
          try {
            const data = JSON.parse(raw)
            if (data.type === 'content' && data.content) {
              fullContent += data.content
              setStreamContent(fullContent)
            } else if (data.type === 'error') {
              setSteps([{ agent: '错误', status: 'error', message: data.content || '分析失败', count: 0 }])
              setRunning(false)
              return
            }
          } catch { /* ignore */ }
        }
      }
      setSteps([{ agent: '分析Agent', status: 'completed', message: '分析完成', count: 0 }])
      setResult({ total: 1, new: 1 })
      onComplete()
    } catch {
      setSteps([{ agent: '错误', status: 'error', message: '处理失败', count: 0 }])
    } finally {
      setRunning(false)
    }
  }

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div className="bg-gray-900 rounded-lg w-[500px] max-h-[80vh] overflow-hidden">
        <div className="flex items-center justify-between p-4 border-b border-gray-700">
          <h2 className="text-lg font-semibold flex items-center gap-2">
            <Cpu className="w-5 h-5 text-primary-400" />
            多Agent协作处理
          </h2>
          <button onClick={onClose} className="text-gray-400 hover:text-white">
            <X className="w-5 h-5" />
          </button>
        </div>

        <div className="p-4 space-y-4">
          {/* Agent流程图 */}
          <div className="bg-gray-800 rounded-lg p-4">
            <div className="text-sm text-gray-400 mb-3">处理流程</div>
            <div className="flex items-center justify-between text-xs">
              {['采集', '提取', '去重', '评估', '存储'].map((s, i) => (
                <div key={s} className="flex items-center">
                  <div className="w-12 h-12 rounded-full bg-gray-700 flex items-center justify-center text-primary-400">
                    {s}
                  </div>
                  {i < 4 && <div className="w-8 h-0.5 bg-gray-600" />}
                </div>
              ))}
            </div>
          </div>

          {/* 执行步骤 */}
          {steps.length > 0 && (
            <div className="space-y-2">
              {steps.map((step, i) => {
                const Icon = agentIcons[step.agent] || Cpu
                return (
                  <div key={i} className="flex items-center gap-3 p-3 bg-gray-800 rounded-lg">
                    <Icon className="w-5 h-5 text-primary-400" />
                    <div className="flex-1">
                      <div className="font-medium">{step.agent}</div>
                      <div className="text-sm text-gray-400">{step.message}</div>
                    </div>
                    {step.status === 'completed' ? (
                      <CheckCircle className="w-5 h-5 text-green-500" />
                    ) : step.status === 'running' ? (
                      <Loader2 className="w-5 h-5 text-primary-400 animate-spin" />
                    ) : null}
                  </div>
                )
              })}
            </div>
          )}

          {/* 流式输出 */}
          {streamContent && (
            <div className="bg-gray-800 rounded-lg p-3 max-h-32 overflow-y-auto text-xs font-mono text-gray-300 whitespace-pre-wrap">
              {streamContent}
            </div>
          )}

          {/* 结果 */}
          {result && (
            <div className="bg-green-900/30 border border-green-700 rounded-lg p-4 text-center">
              <div className="text-green-400 font-semibold">处理完成</div>
              <div className="text-sm text-gray-300 mt-1">
                共处理 {result.total} 个事件，更新 {result.new} 个
              </div>
            </div>
          )}

          {/* 操作按钮 */}
          <button
            onClick={runPipeline}
            disabled={running}
            className="w-full btn-primary py-3"
          >
            {running ? (
              <>
                <Loader2 className="w-4 h-4 animate-spin" />
                Agent处理中...
              </>
            ) : (
              <>
                <Cpu className="w-4 h-4" />
                启动多Agent处理
              </>
            )}
          </button>
        </div>
      </div>
    </div>
  )
}
