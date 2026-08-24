import { ReactNode } from 'react'
import { Rss } from 'lucide-react'

interface Props {
  title: string
  source: string
  confidence: number
  actions: ReactNode
}

export default function GlobalInsightHeader({ title, source, confidence, actions }: Props) {
  const getColor = () => {
    if (confidence < 40) return 'text-green-400'
    if (confidence < 70) return 'text-yellow-400'
    return 'text-red-400'
  }

  return (
    <div className="bg-gray-900 border-b border-gray-700 px-6 py-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-4">
          <div>
            <h1 className="text-xl font-bold text-white">{title}</h1>
            <span className="inline-flex items-center gap-1 text-xs text-gray-400 mt-1">
              <Rss className="w-3 h-3" /> {source}
            </span>
          </div>
        </div>

        {/* 风险仪表盘 */}
        <div className="flex flex-col items-center">
          <div className="relative w-32 h-16 overflow-hidden">
            <svg viewBox="0 0 100 50" className="w-full h-full">
              <path d="M 10 50 A 40 40 0 0 1 90 50" fill="none" stroke="#334155" strokeWidth="8" />
              <path
                d="M 10 50 A 40 40 0 0 1 90 50"
                fill="none"
                stroke="currentColor"
                strokeWidth="8"
                strokeDasharray={`${confidence * 1.26} 126`}
                className={`transition-all duration-500 ${getColor()}`}
              />
            </svg>
            <div className="absolute inset-0 flex items-end justify-center pb-1">
              <span className={`text-lg font-bold ${getColor()}`}>{confidence}%</span>
            </div>
          </div>
          <span className="text-xs text-gray-500">置信度评分</span>
        </div>

        {actions}
      </div>
    </div>
  )
}
