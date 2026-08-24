import { useEventStore } from '@/stores/eventStore'
import { CheckCircle, Loader2, AlertCircle, Edit3 } from 'lucide-react'
import { useState } from 'react'
import toast from 'react-hot-toast'

export default function ReasoningStream() {
  const { agentLogs } = useEventStore()
  const [editingIdx, setEditingIdx] = useState<number | null>(null)
  const [feedback, setFeedback] = useState('')

  const handleSubmit = (agent: string) => {
    if (feedback.trim()) {
      toast.success(`已提交修正: ${agent} - ${feedback}`)
      setFeedback('')
      setEditingIdx(null)
    }
  }

  if (agentLogs.length === 0) {
    return <div className="text-center text-gray-500 py-8">点击"启动分析"开始多Agent协作处理</div>
  }

  return (
    <div className="relative pl-6 max-h-[500px] overflow-y-auto">
      {/* 时间线竖线 */}
      <div className="absolute left-2 top-0 bottom-0 w-0.5 bg-gray-700" />

      {agentLogs.map((log, i) => (
        <div key={i} className="relative pb-4 last:pb-0">
          {/* 时间线节点 */}
          <div className="absolute -left-4 w-4 h-4 rounded-full bg-gray-900 border-2 border-gray-700 flex items-center justify-center">
            {log.status === 'running' && <div className="w-2 h-2 bg-primary-400 rounded-full animate-pulse" />}
            {log.status === 'success' && <div className="w-2 h-2 bg-green-500 rounded-full" />}
            {log.status === 'error' && <div className="w-2 h-2 bg-red-500 rounded-full" />}
          </div>

          {/* 内容卡片 */}
          <div className="ml-4 p-3 bg-gray-800/50 rounded-lg border border-gray-700/50">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                {log.status === 'running' && <Loader2 className="w-4 h-4 text-primary-400 animate-spin" />}
                {log.status === 'success' && <CheckCircle className="w-4 h-4 text-green-500" />}
                {log.status === 'error' && <AlertCircle className="w-4 h-4 text-red-500" />}
                <span className="font-medium text-sm text-white">{log.agent}</span>
              </div>
              {log.status === 'success' && (
                <button onClick={() => setEditingIdx(editingIdx === i ? null : i)} className="text-xs text-gray-500 hover:text-primary-400">
                  <Edit3 className="w-3 h-3" />
                </button>
              )}
            </div>
            <p className="text-xs text-gray-400 mt-1">{log.message}</p>

            {editingIdx === i && (
              <div className="mt-2 flex gap-2">
                <input
                  value={feedback}
                  onChange={(e) => setFeedback(e.target.value)}
                  placeholder="输入修正..."
                  className="flex-1 bg-gray-900 border border-gray-600 rounded px-2 py-1 text-xs text-white"
                />
                <button onClick={() => handleSubmit(log.agent)} className="btn-primary text-xs py-1 px-2">提交</button>
              </div>
            )}
          </div>
        </div>
      ))}
    </div>
  )
}
