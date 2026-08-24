import { Play, Loader2, RotateCcw, Square } from 'lucide-react'
import { cn } from '@/utils'

interface Props {
  isProcessing: boolean
  onStart: () => void
  onStop: () => void
  hasData: boolean
}

export default function StartButton({ isProcessing, onStart, onStop, hasData }: Props) {
  return (
    <div className="flex items-center gap-2">
      {/* 终止按钮 — 仅分析进行中显示 */}
      {isProcessing && (
        <button
          onClick={onStop}
          className="h-9 inline-flex items-center gap-1.5 px-3 rounded-lg border border-red-200 bg-red-50 hover:bg-red-100 text-red-600 text-sm font-medium transition-colors"
          title="终止分析"
        >
          <Square className="w-3.5 h-3.5 fill-current" />
          终止
        </button>
      )}

      <button
        onClick={onStart}
        disabled={isProcessing}
        className={cn(
          'btn-primary relative flex items-center gap-2 text-sm overflow-hidden',
          isProcessing && 'opacity-80 cursor-wait'
        )}
      >
        {/* 处理中的加载动效 */}
        {isProcessing && (
          <div className="absolute inset-0 overflow-hidden rounded-lg">
            <div
              className="absolute left-0 right-0 h-full"
              style={{
                background: 'linear-gradient(180deg, transparent, rgba(255,255,255,0.12), transparent)',
                animation: 'cyber-scan 2s linear infinite',
              }}
            />
          </div>
        )}

        <span className="relative flex items-center gap-2">
          {isProcessing ? (
            <>
              <Loader2 className="w-4 h-4 animate-spin" />
              <span>Agent 分析中</span>
              <span className="flex gap-0.5">
                <span className="w-1 h-1 rounded-full bg-current animate-bounce" style={{ animationDelay: '0ms' }} />
                <span className="w-1 h-1 rounded-full bg-current animate-bounce" style={{ animationDelay: '150ms' }} />
                <span className="w-1 h-1 rounded-full bg-current animate-bounce" style={{ animationDelay: '300ms' }} />
              </span>
            </>
          ) : hasData ? (
            <>
              <RotateCcw className="w-4 h-4" />
              <span>重新分析</span>
            </>
          ) : (
            <>
              <Play className="w-4 h-4" />
              <span>启动 AI 研判</span>
            </>
          )}
        </span>
      </button>
    </div>
  )
}
