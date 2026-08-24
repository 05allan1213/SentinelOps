import { useState } from 'react'
import { CheckCircle, Loader2, AlertCircle, Edit3 } from 'lucide-react'

interface Props {
  agent: string
  status: 'running' | 'success' | 'error'
  message: string
  onIntervene?: (feedback: string) => void
}

export default function AgentReasoningCard({ agent, status, message, onIntervene }: Props) {
  const [showFeedback, setShowFeedback] = useState(false)
  const [feedback, setFeedback] = useState('')

  const handleSubmit = () => {
    if (feedback.trim() && onIntervene) {
      onIntervene(feedback)
      setFeedback('')
      setShowFeedback(false)
    }
  }

  return (
    <div className="p-4 bg-gray-800 rounded-lg border-l-4 border-primary-500">
      <div className="flex items-start gap-3">
        <div className="mt-0.5">
          {status === 'running' && <Loader2 className="w-5 h-5 text-primary-400 animate-spin" />}
          {status === 'success' && <CheckCircle className="w-5 h-5 text-green-500" />}
          {status === 'error' && <AlertCircle className="w-5 h-5 text-red-500" />}
        </div>
        <div className="flex-1">
          <div className="font-medium text-white">{agent}</div>
          <div className="text-sm text-gray-400 mt-1">{message}</div>
        </div>
        {status === 'success' && onIntervene && (
          <button
            onClick={() => setShowFeedback(!showFeedback)}
            className="text-xs text-gray-500 hover:text-primary-400 flex items-center gap-1"
          >
            <Edit3 className="w-3 h-3" />
            修正
          </button>
        )}
      </div>
      {showFeedback && (
        <div className="mt-3 pt-3 border-t border-gray-700">
          <input
            type="text"
            value={feedback}
            onChange={(e) => setFeedback(e.target.value)}
            placeholder="输入修正信息..."
            className="w-full bg-gray-900 border border-gray-600 rounded px-3 py-2 text-sm text-white"
          />
          <div className="flex gap-2 mt-2">
            <button onClick={handleSubmit} className="btn-primary text-xs py-1 px-3">提交</button>
            <button onClick={() => setShowFeedback(false)} className="btn-default text-xs py-1 px-3">取消</button>
          </div>
        </div>
      )}
    </div>
  )
}
