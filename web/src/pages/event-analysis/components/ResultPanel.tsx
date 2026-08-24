import { useState, useEffect, useMemo, useRef, useCallback } from 'react'
import ReactMarkdown from 'react-markdown'
import { cn, normalizeMarkdown } from '@/utils'
import { ExternalLink, Shield, ChevronRight, AlertTriangle, Scan, X, Loader2 } from 'lucide-react'

interface EventData {
  id: number
  event_id: string
  title: string
  desc: string
  cve_id: string
  cvss: number
  severity: string
  vendor: string
  product: string
  source: string
  source_url: string
  recommendation?: string
  recommendationComplete?: boolean
  similar_events?: Array<{ title: string; similarity: number }>
}

interface SolutionEntry {
  content: string
  complete: boolean
}

interface Props {
  data: {
    maxCVSS: number
    count: number
    avgRisk: number
    critical?: number
    highRisk?: number
    events?: EventData[]
  } | null
  isProcessing?: boolean
  onSolutionUpdate?: (eventId: string, content: string, complete: boolean) => void
}

const severityConfig: Record<string, { color: string; label: string }> = {
  critical: { color: '#ef4444', label: 'CRITICAL' },
  high:     { color: '#f97316', label: 'HIGH' },
  medium:   { color: '#eab308', label: 'MEDIUM' },
  low:      { color: '#22c55e', label: 'LOW' },
  info:     { color: '#6366f1', label: 'INFO' },
}

function SkeletonCard() {
  return (
    <div className="px-4 py-2.5 rounded-lg border border-gray-200 dark:border-[#30363D]/30 bg-gray-50 dark:bg-[#080C13]/95">
      <div className="flex items-center gap-2 animate-pulse">
        <div className="w-14 h-4 bg-gray-200 dark:bg-[#30363D]/50 rounded" />
        <div className="flex-1 h-4 bg-gray-200 dark:bg-[#30363D]/30 rounded" />
      </div>
    </div>
  )
}

// ─── EventDetail：纯展示组件，无 fetch 逻辑 ───────────────────────────────────
// fetch 在 ResultPanel 层管理，关闭面板不中断流式生成
function EventDetail({ event, solutionEntry, isStreaming, onClose }: {
  event: EventData
  solutionEntry: SolutionEntry | undefined
  isStreaming: boolean
  onClose: () => void
}) {
  const cfg = severityConfig[event.severity] || severityConfig.low
  const content = solutionEntry?.content || ''
  const renderedSolution = useMemo(() => normalizeMarkdown(content), [content])
  const showSkeleton = isStreaming && !content

  return (
    <div
      className="absolute inset-0 z-10 bg-white dark:bg-[#080C13]/95 flex flex-col"
      style={{ animation: 'cyber-slide-in 0.25s ease-out' }}
    >
      <div className="flex items-center justify-between px-4 py-3 border-b border-gray-200 dark:border-[#30363D]/60">
        <span className="text-sm font-semibold text-gray-900 dark:text-[#E6EDF3]">事件详情</span>
        <button onClick={onClose} className="text-gray-500 dark:text-[#8B949E] hover:text-gray-900 dark:hover:text-[#E6EDF3] transition-colors">
          <X className="w-4 h-4" />
        </button>
      </div>
      <div className="flex-1 overflow-auto p-4 space-y-3">
        <div className="flex items-center gap-2 flex-wrap">
          <span className="px-1.5 py-0.5 rounded text-xs font-bold"
            style={{ backgroundColor: cfg.color + '15', color: cfg.color }}>{cfg.label}</span>
          {event.cve_id && (
            <span className="px-1.5 py-0.5 rounded text-xs font-mono"
              style={{ backgroundColor: cfg.color + '15', color: cfg.color }}>{event.cve_id}</span>
          )}
          <span className="px-1.5 py-0.5 rounded text-xs font-bold"
            style={{ backgroundColor: cfg.color + '15', color: cfg.color }}>CVSS {event.cvss}</span>
        </div>
        <h3 className="text-base text-gray-900 dark:text-[#E6EDF3] font-semibold leading-relaxed">{event.title}</h3>
        {event.desc && <p className="text-sm text-gray-600 dark:text-[#8B949E] leading-relaxed">{event.desc}</p>}
        <div className="space-y-1.5 text-xs text-gray-500 dark:text-[#8B949E]">
          {event.vendor && <div>厂商: <span className="text-gray-600 dark:text-[#8B949E]">{event.vendor}</span></div>}
          {event.product && <div>产品: <span className="text-gray-600 dark:text-[#8B949E]">{event.product}</span></div>}
        </div>
        {event.source_url && (
          <a href={event.source_url} target="_blank" rel="noopener noreferrer"
            className="inline-flex items-center gap-1 text-xs text-[#00F0E0] hover:underline">
            <ExternalLink className="w-3.5 h-3.5" /> 查看来源
          </a>
        )}

        {/* AI 解决方案 */}
        <div className="p-2.5 rounded-lg bg-green-50 dark:bg-[#22C55E]/5 border border-green-200 dark:border-[#22C55E]/20">
          <div className="flex items-center gap-1.5 mb-2">
            <span className="text-xs text-[#22C55E] font-semibold">AI 解决方案</span>
            {isStreaming && <Loader2 className="w-3 h-3 text-[#22C55E] animate-spin" />}
          </div>
          {content ? (
            <div className="prose prose-sm max-w-none text-sm text-gray-700 dark:text-[#C9D1D9] leading-relaxed">
              <ReactMarkdown>{renderedSolution}</ReactMarkdown>
            </div>
          ) : showSkeleton ? (
            <div className="space-y-2 py-1">
              <div className="flex items-center gap-2 mb-3">
                <Loader2 className="w-4 h-4 text-[#22C55E] animate-spin shrink-0" />
                <span className="text-xs text-[#22C55E] font-medium animate-pulse">AI 正在生成解决方案...</span>
              </div>
              <div className="space-y-1.5 animate-pulse">
                <div className="h-2.5 bg-green-200/60 dark:bg-[#22C55E]/10 rounded-full w-full" />
                <div className="h-2.5 bg-green-200/60 dark:bg-[#22C55E]/10 rounded-full w-5/6" />
                <div className="h-2.5 bg-green-200/60 dark:bg-[#22C55E]/10 rounded-full w-4/5" />
                <div className="h-2.5 bg-green-200/40 dark:bg-[#22C55E]/5 rounded-full w-3/4 mt-3" />
                <div className="h-2.5 bg-green-200/40 dark:bg-[#22C55E]/5 rounded-full w-full" />
                <div className="h-2.5 bg-green-200/40 dark:bg-[#22C55E]/5 rounded-full w-2/3" />
              </div>
            </div>
          ) : (
            <p className="text-xs text-gray-400 dark:text-[#8B949E] italic">暂无解决方案</p>
          )}
        </div>

        {event.similar_events && event.similar_events.length > 0 && (
          <div className="p-2.5 rounded-lg bg-amber-50 dark:bg-[#F59E0B]/5 border border-amber-200 dark:border-[#F59E0B]/20">
            <div className="text-xs text-[#F59E0B] font-semibold mb-1.5">相似历史事件</div>
            <div className="space-y-1">
              {event.similar_events.map((se, i) => (
                <div key={i} className="text-xs text-gray-600 dark:text-[#8B949E] flex items-center gap-1.5">
                  <span className="w-1 h-1 rounded-full bg-[#F59E0B]/60 shrink-0" />
                  <span className="flex-1 truncate">{se.title}</span>
                  <span className="text-[#F59E0B] shrink-0">{(se.similarity * 100).toFixed(0)}%</span>
                </div>
              ))}
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

// Compact event card
function CompactCard({ event, onClick }: { event: EventData; onClick: () => void }) {
  const cfg = severityConfig[event.severity] || severityConfig.low
  const isCritical = event.severity === 'critical'

  return (
    <button
      onClick={onClick}
      className={cn(
        'w-full text-left px-4 py-2.5 rounded-xl border transition-all duration-200 group',
        isCritical
          ? 'border-red-200 dark:border-red-900/30 bg-red-50/30 dark:bg-red-900/5 hover:border-red-300 dark:hover:border-red-800/50'
          : 'border-gray-200 dark:border-[#30363D]/30 hover:border-gray-300 dark:hover:border-[#30363D]/60',
      )}
      style={
        isCritical
          ? { animation: 'cyber-glow-pulse 3s ease-in-out infinite', '--glow-color': 'rgba(239,68,68,0.12)' } as React.CSSProperties
          : undefined
      }
    >
      <div className="flex items-center gap-2">
        <span
          className="px-1.5 py-0.5 rounded text-xs font-bold shrink-0 tracking-wider"
          style={{ backgroundColor: cfg.color + '15', color: cfg.color }}
        >
          {cfg.label}
        </span>
        <span className="text-sm text-gray-700 dark:text-[#C9D1D9] truncate flex-1 group-hover:text-gray-900 dark:group-hover:text-[#E6EDF3] transition-colors">
          {event.title}
        </span>
        <ChevronRight className="w-3.5 h-3.5 text-gray-300 dark:text-[#30363D] group-hover:text-gray-500 dark:group-hover:text-[#8B949E] shrink-0 transition-colors" />
      </div>
    </button>
  )
}

// 并发控制：最多同时跑 MAX_CONCURRENT 个解决方案 fetch，超出部分入队等待。
// 超时控制：每条 fetch 最多等待 FETCH_TIMEOUT_MS 毫秒，超时后标记完成，停止 spinner。
const MAX_CONCURRENT = 3
const FETCH_TIMEOUT_MS = 90_000

// ─── ResultPanel：统一管理所有解决方案 fetch ───────────────────────────────────
export default function ResultPanel({ data, isProcessing, onSolutionUpdate }: Props) {
  const [selectedEventId, setSelectedEventId] = useState<string | null>(null)
  const selectedEvent = data?.events?.find(e => e.event_id === selectedEventId) ?? null

  const solutionCache = useRef<Map<string, SolutionEntry>>(new Map())
  const activeStreams = useRef<Set<string>>(new Set())
  const [cacheVersion, setCacheVersion] = useState(0)
  const onSolutionUpdateRef = useRef(onSolutionUpdate)
  useEffect(() => { onSolutionUpdateRef.current = onSolutionUpdate }, [onSolutionUpdate])

  // 并发队列
  const pendingQueue = useRef<EventData[]>([])
  const activeCount = useRef(0)

  // launchFetch：真正执行 fetch，带超时控制，完成后自动从队列取下一条
  // 用 ref 存储以避免 useCallback 的循环依赖（finally 中调用自身）
  const launchFetchRef = useRef<(event: EventData) => void>(() => {})
  launchFetchRef.current = (event: EventData) => {
    const { event_id } = event
    activeStreams.current.add(event_id)
    activeCount.current++

    const controller = new AbortController()
    const timeoutId = setTimeout(() => controller.abort(), FETCH_TIMEOUT_MS)
    let accumulated = ''

    fetch('/api/event/v1/analyze/stream', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        event_id: event.event_id,
        title: event.title,
        severity: event.severity,
        cve_id: event.cve_id,
        source: event.source,
      }),
      signal: controller.signal,
    })
      .then(async (res) => {
        if (!res.ok) return
        const reader = res.body?.getReader()
        if (!reader) return
        const decoder = new TextDecoder()
        let buf = ''
        accumulated = ''
        while (true) {
          const { done, value } = await reader.read()
          if (done) break
          buf += decoder.decode(value, { stream: true })
          const lines = buf.split('\n')
          buf = lines.pop() ?? ''
          for (const line of lines) {
            if (!line.startsWith('data: ')) continue
            const raw = line.slice(6).trim()
            if (raw === '[DONE]') return
            try {
              const msg = JSON.parse(raw)
              if (msg.type === 'content' && msg.content) {
                accumulated += msg.content
                solutionCache.current.set(event_id, { content: accumulated, complete: false })
                setCacheVersion(v => v + 1)
              }
            } catch { /* ignore */ }
          }
        }
      })
      .catch(() => { /* AbortError（超时）或网络错误，在 finally 中统一处理 */ })
      .finally(() => {
        clearTimeout(timeoutId)
        activeStreams.current.delete(event_id)
        activeCount.current--
        // 无论成功/失败/超时，都标记为 complete 让 spinner 停止
        if (!solutionCache.current.get(event_id)?.complete) {
          solutionCache.current.set(event_id, { content: accumulated, complete: true })
          if (accumulated) {
            onSolutionUpdateRef.current?.(event_id, accumulated, true)
          }
        }
        setCacheVersion(v => v + 1)
        // 从队列取下一条继续处理
        if (pendingQueue.current.length > 0) {
          const next = pendingQueue.current.shift()!
          launchFetchRef.current(next)
        }
      })
  }

  // startFetch：幂等入口，检查去重后决定立即启动或入队
  const startFetch = useCallback((event: EventData) => {
    const { event_id } = event
    if (!event_id) return
    if (activeStreams.current.has(event_id)) return
    if (pendingQueue.current.some(e => e.event_id === event_id)) return
    const cached = solutionCache.current.get(event_id)
    if (cached?.complete) return

    if (activeCount.current < MAX_CONCURRENT) {
      launchFetchRef.current(event)
    } else {
      pendingQueue.current.push(event)
    }
  }, [])

  // data 变化时：重置队列，预填已持久化的解决方案
  useEffect(() => {
    pendingQueue.current = []
    if (!data?.events) return
    for (const ev of data.events) {
      if (ev.recommendationComplete && ev.recommendation) {
        solutionCache.current.set(ev.event_id, { content: ev.recommendation, complete: true })
      }
    }
    setCacheVersion(v => v + 1)
  }, [data])

  // 分析结果到来后，自动预加载所有事件的解决方案（受并发限制，超出部分入队）
  useEffect(() => {
    const targets = (data?.events || []).slice(0, 10)
    for (const ev of targets) {
      startFetch(ev)
    }
  }, [data, startFetch])

  // 用户点击事件：立即启动 fetch（如未开始），然后打开详情面板
  const handleEventClick = (event: EventData) => {
    startFetch(event)
    setSelectedEventId(event.event_id)
  }

  // Skeleton loading
  if (isProcessing && !data?.events?.length) {
    return (
      <div className="h-full flex flex-col bg-white dark:bg-[#080C13]/95">
        <div className="px-4 py-3 border-b border-gray-200 dark:border-[#30363D]/60 flex items-center gap-2">
          <Scan className="w-4 h-4 text-[#00F0E0] animate-pulse" />
          <span className="text-sm font-semibold text-gray-900 dark:text-[#E6EDF3] tracking-wide">扫描结果</span>
          <span className="ml-auto text-xs text-[#00F0E0] font-mono animate-pulse">scanning...</span>
        </div>
        <div className="flex-1 p-2 space-y-1.5 overflow-hidden">
          <SkeletonCard /><SkeletonCard /><SkeletonCard /><SkeletonCard /><SkeletonCard />
        </div>
      </div>
    )
  }

  // Empty state
  if (!data?.events?.length) {
    return (
      <div className="flex flex-col items-center justify-center min-h-[600px] text-gray-500 dark:text-[#8B949E] bg-white dark:bg-[#080C13]/95">
        <Shield className="w-10 h-10 mb-2.5 opacity-20" />
        <p className="text-sm tracking-wide">启动分析后查看结果</p>
        <p className="text-xs text-gray-300 dark:text-[#30363D] mt-1 font-mono">AWAITING ANALYSIS</p>
      </div>
    )
  }

  // cacheVersion 变化时重新读取，确保 EventDetail 拿到最新内容
  const selectedSolution = selectedEvent ? solutionCache.current.get(selectedEvent.event_id) : undefined
  const isSelectedStreaming = selectedEvent ? activeStreams.current.has(selectedEvent.event_id) : false
  // 标注 cacheVersion 依赖（防止 linter 警告，实际读取在上方 Map.get 中）
  void cacheVersion

  return (
    <div className="relative h-full flex flex-col bg-white dark:bg-[#080C13]/95">
      <div className="px-4 py-3 border-b border-gray-200 dark:border-[#30363D]/60 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <AlertTriangle className="w-4 h-4 text-[#F43F5E]" />
          <span className="text-sm font-semibold text-gray-900 dark:text-[#E6EDF3] tracking-wide">高危事件</span>
        </div>
        <span className="text-xs text-gray-500 dark:text-[#8B949E] font-mono">{data.events.length} items</span>
      </div>
      <div className="flex-1 overflow-auto p-3 space-y-1 scrollbar-thin">
        {data.events.map((event) => (
          <CompactCard
            key={event.id}
            event={event}
            onClick={() => handleEventClick(event)}
          />
        ))}
      </div>

      {selectedEvent && (
        <EventDetail
          key={selectedEvent.event_id}
          event={selectedEvent}
          solutionEntry={selectedSolution}
          isStreaming={isSelectedStreaming}
          onClose={() => setSelectedEventId(null)}
        />
      )}
    </div>
  )
}
