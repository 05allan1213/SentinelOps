import { useEffect, useMemo, useRef, useState } from 'react'
import { ChevronRight, ChevronDown, Loader2, CheckCircle, AlertCircle } from 'lucide-react'
import { AgentLog } from '@/types/agent'

interface Props {
  logs: AgentLog[]
  isProcessing: boolean
}

// Agent 标签颜色映射（与 Sentinel 配色对齐）
const agentColor: Record<string, string> = {
  '数据采集': '#10b981',
  '提取':     '#8b5cf6',
  '去重':     '#06b6d4',
  '风险评估': '#f43f5e',
  '解决方案': '#22c55e',
  'Pipeline': '#94a3b8',
}

function resolveColor(agent: string): string {
  for (const key of Object.keys(agentColor)) {
    if (agent.includes(key)) return agentColor[key]
  }
  return '#94a3b8'
}

// 从 agent 名称提取简短标签
function shortLabel(agent: string): string {
  return agent.replace('Agent', '').trim() || agent
}

// 时间戳格式化为 HH:MM:SS
function fmtTime(ts: string | undefined): string {
  if (!ts) return ''
  try {
    const d = new Date(ts)
    return d.toLocaleTimeString('zh-CN', { hour12: false })
  } catch {
    return ''
  }
}

function StatusDot({ status }: { status: string }) {
  if (status === 'running') return <Loader2 className="w-3.5 h-3.5 shrink-0 text-primary-500 animate-spin" />
  if (status === 'success') return <CheckCircle className="w-3.5 h-3.5 shrink-0 text-emerald-500" />
  if (status === 'error')   return <AlertCircle className="w-3.5 h-3.5 shrink-0 text-red-500" />
  return <ChevronRight className="w-3.5 h-3.5 shrink-0 text-gray-400" />
}

export default function ThinkingConsole({ logs, isProcessing }: Props) {
  const scrollRef = useRef<HTMLDivElement>(null)
  const [collapsed, setCollapsed] = useState(false)

  // 预计算每个 Agent 的最终状态，避免旧 running 条目显示转圈
  const finalAgentStatus = useMemo(() => {
    const map: Record<string, string> = {}
    for (const log of logs) {
      map[log.agent] = log.status
    }
    return map
  }, [logs])

  // 分析进行中自动展开
  useEffect(() => {
    if (isProcessing && collapsed) setCollapsed(false)
  }, [isProcessing])

  // 新日志出现时自动滚到底部
  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [logs])

  return (
    <div className={`flex flex-col border border-gray-200 rounded-xl overflow-hidden shadow-sm transition-all duration-300 ${collapsed ? 'h-[44px]' : 'min-h-[370px] max-h-[395px]'} bg-white dark:bg-[#0D1117]`}>
      {/* Header */}
      <div
        className="flex items-center gap-2 px-4 py-2.5 border-b border-gray-100 dark:border-[#21262D] bg-gray-50 dark:bg-[#161B22] cursor-pointer select-none shrink-0"
        onClick={() => setCollapsed(c => !c)}
      >
        {collapsed
          ? <ChevronRight className="w-3.5 h-3.5 text-gray-400" />
          : <ChevronDown className="w-3.5 h-3.5 text-gray-400" />}
        {/* 终端提示符风格图标 */}
        <span className="text-sm font-semibold text-gray-700 dark:text-[#E6EDF3] tracking-wide">思考链路</span>
        <span className="text-xs text-gray-400 dark:text-[#8B949E] font-mono tracking-widest uppercase ml-0.5">REASONING TRACE</span>

        <div className="ml-auto flex items-center gap-2">
          {isProcessing && (
            <span className="flex items-center gap-1 text-[10px] text-primary-600 font-mono">
              <span className="w-1.5 h-1.5 rounded-full bg-primary-500 animate-pulse" />
              LIVE
            </span>
          )}
          {logs.length > 0 && (
            <span className="text-[10px] text-gray-400 dark:text-[#8B949E] font-mono tabular-nums">
              {logs.length} entries
            </span>
          )}
        </div>
      </div>

      {/* Log stream */}
      {!collapsed && (
        <div ref={scrollRef} className="flex-1 overflow-auto px-2 py-1.5 space-y-px scrollbar-thin">
          {logs.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-12 text-gray-400 dark:text-[#8B949E]">
              <span className="font-mono text-xs opacity-50">{'// awaiting agent execution...'}</span>
            </div>
          ) : (
              logs.map((log, idx) => {
              const color = resolveColor(log.agent)
              const label = shortLabel(log.agent)
              const isEven = idx % 2 === 0
              return (
                <div
                  key={idx}
                  className={`flex items-start gap-2 px-2.5 py-1.5 rounded text-sm transition-colors ${
                    log.status === 'running'
                      ? 'bg-primary-50 dark:bg-[#1C2128]'
                      : isEven
                        ? 'bg-gray-50/60 dark:bg-[#0D1117]'
                        : 'bg-white dark:bg-[#161B22]/40'
                  }`}
                >
                  <StatusDot status={finalAgentStatus[log.agent] || log.status} />

                  {/* 彩色括号标签 */}
                  <span
                    className="font-mono font-semibold shrink-0 tabular-nums"
                    style={{ color }}
                  >
                    [{label}]
                  </span>

                  {/* 消息正文 */}
                  <span className="flex-1 min-w-0 text-gray-600 dark:text-[#C9D1D9] leading-relaxed break-all">
                    {log.message}
                  </span>

                  {/* 时间戳 HH:MM:SS */}
                  {log.timestamp && (
                    <span className="ml-2 shrink-0 text-gray-300 dark:text-[#484F58] font-mono tabular-nums">
                      {fmtTime(log.timestamp)}
                    </span>
                  )}
                </div>
              )
            })
          )}
        </div>
      )}
    </div>
  )
}
