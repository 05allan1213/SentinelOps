import { useState, useEffect, useMemo } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import {
  ArrowLeft,
  Activity,
  Cpu,
  Wrench,
  Database,
  Layers,
  Sparkles,
  CheckCircle2,
  XCircle,
  Clock,
  Loader2,
  ChevronDown,
  ChevronRight,
  Zap,
  Server,
  Bot,
  Download,
  ArrowUpDown,
} from 'lucide-react'
import { cn } from '@/utils'
import { traceService, type TraceNode } from '@/services/trace'

function Loader() {
  return <Loader2 className="w-6 h-6 animate-spin text-gray-400" />
}

// 节点类型配置（浅色主题）
const nodeTypeConfig: Record<string, {
  label: string
  icon: typeof Cpu
  textClass: string
  bgClass: string
  barColor: string
}> = {
  LLM:       { label: 'LLM',      icon: Cpu,         textClass: 'text-violet-700', bgClass: 'bg-violet-50 border-violet-200',  barColor: '#7c3aed' },
  AGENT:     { label: 'Agent',    icon: Bot,         textClass: 'text-indigo-700', bgClass: 'bg-indigo-50 border-indigo-200',  barColor: '#4f46e5' },
  LAMBDA:    { label: 'Lambda',   icon: Layers,      textClass: 'text-gray-600',   bgClass: 'bg-gray-50 border-gray-200',      barColor: '#9ca3af' },
  TOOL:      { label: 'Tool',     icon: Wrench,      textClass: 'text-blue-700',   bgClass: 'bg-blue-50 border-blue-200',      barColor: '#2563eb' },
  EMBEDDING: { label: 'Embedding',icon: Sparkles,    textClass: 'text-pink-700',   bgClass: 'bg-pink-50 border-pink-200',      barColor: '#db2777' },
  RETRIEVER: { label: 'Milvus',   icon: Database,    textClass: 'text-emerald-700',bgClass: 'bg-emerald-50 border-emerald-200',barColor: '#059669' },
  RERANK:    { label: 'Rerank',   icon: ArrowUpDown, textClass: 'text-amber-700',  bgClass: 'bg-amber-50 border-amber-200',    barColor: '#d97706' },
  CACHE:     { label: 'Cache',    icon: Server,      textClass: 'text-cyan-700',   bgClass: 'bg-cyan-50 border-cyan-200',      barColor: '#0891b2' },
  DB:        { label: 'MySQL',    icon: Database,    textClass: 'text-orange-700', bgClass: 'bg-orange-50 border-orange-200',  barColor: '#ea580c' },
}

const statusBadge: Record<string, { label: string; cls: string; icon: typeof CheckCircle2 }> = {
  success: { label: '成功', cls: 'bg-emerald-50 text-emerald-700 border border-emerald-200', icon: CheckCircle2 },
  error:   { label: '失败', cls: 'bg-red-50 text-red-700 border border-red-200',             icon: XCircle },
  running: { label: '运行中', cls: 'bg-amber-50 text-amber-700 border border-amber-200',     icon: Clock },
}

function formatDuration(ms: number): string {
  if (ms <= 0) return '-'
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(2)}s`
}

// 构建父→子 map（parentNodeId → 子节点列表）
function buildChildrenMap(nodes: TraceNode[]): Map<string, TraceNode[]> {
  const map = new Map<string, TraceNode[]>()
  for (const n of nodes) {
    const pid = n.parentNodeId || ''
    const arr = map.get(pid) || []
    arr.push(n)
    map.set(pid, arr)
  }
  return map
}

// 计算节点自身耗时（总耗时 - 直接子节点耗时之和）
function computeSelfTime(node: TraceNode, childrenMap: Map<string, TraceNode[]>): number {
  const children = childrenMap.get(node.nodeId) || []
  const childrenTotal = children.reduce((sum, c) => sum + (c.durationMs || 0), 0)
  return Math.max(0, (node.durationMs || 0) - childrenTotal)
}

// 计算关键路径节点 ID 集合（每层选 endMs 最大的子节点，递归向下）
function computeCriticalPath(nodes: TraceNode[], childrenMap: Map<string, TraceNode[]>): Set<string> {
  const critical = new Set<string>()
  function dfs(nodeId: string) {
    const children = childrenMap.get(nodeId) || []
    if (children.length === 0) return
    let best: TraceNode | null = null
    let bestEndMs = -1
    for (const c of children) {
      const endMs = new Date(c.startTime).getTime() + (c.durationMs || 0)
      if (endMs > bestEndMs) { bestEndMs = endMs; best = c }
    }
    if (best) { critical.add(best.nodeId); dfs(best.nodeId) }
  }
  const nodeIds = new Set(nodes.map(n => n.nodeId))
  const roots = nodes.filter(n => !n.parentNodeId || !nodeIds.has(n.parentNodeId))
  for (const root of roots) { critical.add(root.nodeId); dfs(root.nodeId) }
  return critical
}

// 时间轴条
function TimelineBar({ node, traceStartMs, totalMs }: { node: TraceNode; traceStartMs: number; totalMs: number }) {
  const nodeStart = new Date(node.startTime).getTime() - traceStartMs
  const pct = totalMs > 0 ? (nodeStart / totalMs) * 100 : 0
  const w   = totalMs > 0 ? ((node.durationMs || 1) / totalMs) * 100 : 0
  const left  = Math.max(0, Math.min(95, pct))
  const width = Math.max(0.5, Math.min(100 - left, w))
  const cfg = nodeTypeConfig[node.nodeType] || nodeTypeConfig.LAMBDA
  return (
    <div className="relative h-3 w-full bg-gray-100 rounded">
      <div
        className="absolute top-0 h-full rounded"
        style={{ left: `${left}%`, width: `${width}%`, background: cfg.barColor, opacity: 0.7, minWidth: 3 }}
        title={`${node.nodeName}: ${formatDuration(node.durationMs)}`}
      />
    </div>
  )
}

// 节点行
function NodeRow({ node, traceStartMs, totalMs, isTopSlowest, isCritical, selfTime, offsetMs }: {
  node: TraceNode
  traceStartMs: number
  totalMs: number
  isTopSlowest?: boolean
  isCritical?: boolean
  selfTime?: number
  offsetMs: number
}) {
  // 错误节点默认展开，方便直接查看错误信息
  const [expanded, setExpanded] = useState(node.status === 'error')
  const tc = nodeTypeConfig[node.nodeType] || nodeTypeConfig.LAMBDA
  const NodeIcon = tc.icon
  const sb = statusBadge[node.status] || statusBadge.running
  const StatusIcon = sb.icon

  const hasDetails =
    (node.nodeType === 'LLM' && (node.inputTokens || node.completionText)) ||
    (node.nodeType === 'RETRIEVER' && node.retrievedDocs) ||
    (node.nodeType === 'TOOL' && !!node.metadata) ||
    !!node.errorMessage ||
    !!node.metadata

  return (
    <div>
      <div
        className={cn(
          'flex items-center gap-2 py-2 px-3 rounded-lg transition-colors',
          hasDetails && 'cursor-pointer hover:bg-gray-50',
          isTopSlowest && 'bg-amber-50/70',
          isCritical && 'border-l-2 border-amber-400',
        )}
        style={{ paddingLeft: `${node.depth * 20 + (isCritical ? 10 : 12)}px` }}
        onClick={() => hasDetails && setExpanded(e => !e)}
      >
        {/* 展开箭头 */}
        <div className="w-4 flex-shrink-0 text-gray-800 group-hover:text-gray-600 transition-colors">
          {hasDetails && (expanded
            ? <ChevronDown className="w-3.5 h-3.5" />
            : <ChevronRight className="w-3.5 h-3.5" />
          )}
        </div>

        {/* 节点类型 badge */}
        <span className={cn(
          'inline-flex items-center gap-1 px-2 py-0.5 rounded text-xs font-medium border flex-shrink-0 w-24 justify-center',
          tc.bgClass, tc.textClass,
        )}>
          <NodeIcon className="w-3 h-3" />
          {tc.label}
        </span>



        {/* 节点名 + 最慢标记 */}
        <span className="flex-1 text-sm text-gray-700 truncate flex items-center gap-1">
          {node.nodeName ? (
            <span className={node.nodeName === '(unnamed)' || node.nodeName === 'Lambda' || node.nodeName === 'Default'
              ? 'text-gray-400'
              : undefined
            }>
              {node.nodeName}
            </span>
          ) : (
            <span className="text-gray-400">(unnamed)</span>
          )}
          {isTopSlowest && (
            <span title="最慢节点" className="flex-shrink-0">
              <Zap className="w-3 h-3 text-amber-500" />
            </span>
          )}
        </span>

        {/* 状态 */}
        <StatusIcon className={cn('w-3.5 h-3.5 flex-shrink-0', node.status === 'success' ? 'text-emerald-500' : node.status === 'error' ? 'text-red-500' : 'text-amber-500')} />

        {/* 行内错误摘要（错误节点折叠时也可见） */}
        {node.status === 'error' && node.errorMessage && !expanded && (
          <span
            className="text-[10px] text-red-400 truncate max-w-[200px] flex-shrink"
            title={node.errorMessage}
          >
            {node.errorMessage.length > 60 ? node.errorMessage.slice(0, 60) + '…' : node.errorMessage}
          </span>
        )}

        {/* 耗时 + 偏移 + 自身耗时 */}
        <div className="w-16 text-right flex-shrink-0 tabular-nums">
          <p className="text-xs text-gray-500">{formatDuration(node.durationMs)}</p>
          {offsetMs > 0 && <p className="text-[10px] text-gray-400">@{formatDuration(offsetMs)}</p>}
          {selfTime !== undefined && selfTime < (node.durationMs || 0) * 0.9 && (
            <p className="text-[10px] text-cyan-500" title="自身执行耗时">⌛{formatDuration(selfTime)}</p>
          )}
        </div>

        {/* 时间轴 */}
        <div className="w-36 flex-shrink-0">
          <TimelineBar node={node} traceStartMs={traceStartMs} totalMs={totalMs} />
        </div>

        {/* Token（LLM/Embedding/Rerank） */}
        <span className="text-xs text-gray-400 w-36 text-right flex-shrink-0 tabular-nums">
          {(node.nodeType === 'LLM' || node.nodeType === 'EMBEDDING' || node.nodeType === 'RERANK') && (
            <>
              {node.inputTokens ? <span className="text-blue-500">↑{node.inputTokens}</span> : null}
              {node.outputTokens ? <span className="text-violet-500 ml-1">↓{node.outputTokens}</span> : null}
            </>
          )}
        </span>
      </div>

      {/* 折叠详情 */}
      {expanded && hasDetails && (
        <div
          className="mx-3 mb-2 p-3 rounded-lg text-xs bg-gray-50 border border-gray-100"
          style={{ marginLeft: `${node.depth * 20 + 28}px` }}
        >
          {node.nodeType === 'LLM' && (
            <div className="space-y-2 text-gray-600">
              {node.modelName && (
                <p><span className="text-gray-400 mr-1">模型：</span>{node.modelName}</p>
              )}
              <div className="flex flex-wrap gap-4">
                {(node.inputTokens ?? 0) > 0 && <p><span className="text-gray-400">输入：</span><span className="text-blue-600 font-medium">{node.inputTokens}</span></p>}
                {(node.outputTokens ?? 0) > 0 && <p><span className="text-gray-400">输出：</span><span className="text-violet-600 font-medium">{node.outputTokens}</span></p>}
                {(node.costCny ?? 0) > 0 && <p><span className="text-gray-400">成本：</span><span className="text-emerald-600 font-medium">¥{(node.costCny ?? 0).toFixed(4)}</span></p>}
              </div>
              {node.completionText && (
                <div>
                  <p className="text-gray-400 mb-1">输出内容：</p>
                  <pre className="whitespace-pre-wrap break-all rounded p-2 text-xs bg-white border border-gray-100 text-gray-700 max-h-48 overflow-auto">
                    {node.completionText}
                  </pre>
                </div>
              )}
            </div>
          )}

          {(node.nodeType === 'EMBEDDING' || node.nodeType === 'RERANK') && (
            <div className="space-y-2 text-gray-600">
              {node.modelName && (
                <p><span className="text-gray-400 mr-1">模型：</span>{node.modelName}</p>
              )}
              <div className="flex flex-wrap gap-4">
                {(node.inputTokens ?? 0) > 0 && <p><span className="text-gray-400">输入：</span><span className="text-blue-600 font-medium">{node.inputTokens}</span></p>}
                {(node.costCny ?? 0) > 0 && <p><span className="text-gray-400">成本：</span><span className="text-emerald-600 font-medium">¥{(node.costCny ?? 0).toFixed(4)}</span></p>}
              </div>
            </div>
          )}

          {node.nodeType === 'RETRIEVER' && (
            <div className="space-y-2 text-gray-600">
              {node.queryText && <p><span className="text-gray-400">查询：</span>{node.queryText}</p>}
              <div className="flex gap-4">
                {node.finalTopK !== undefined && node.finalTopK !== null && (
                  <p><span className="text-gray-400">文档数：</span>{node.finalTopK}</p>
                )}
                <p>
                  <span className="text-gray-400">缓存命中：</span>
                  <span className={node.cacheHit ? 'text-emerald-600' : 'text-gray-400'}>
                    {node.cacheHit ? '✓ 是' : '✗ 否'}
                  </span>
                </p>
              </div>
              {node.retrievedDocs && (() => {
                try {
                  const docs = JSON.parse(node.retrievedDocs) as Array<{ content: string; score?: number }>
                  return (
                    <div className="space-y-1.5">
                      <p className="text-gray-400">检索结果：</p>
                      {docs.map((d, i) => (
                        <div key={i} className="p-2 rounded bg-white border border-gray-100">
                          {/* RAG 检索质量评分可视化 */}
                          {d.score !== undefined && (
                            <div className="flex items-center gap-2 mb-0.5">
                              <div className="h-1.5 rounded-full bg-gray-100 flex-1">
                                <div
                                  className="h-full rounded-full transition-all"
                                  style={{
                                    width: `${Math.min(100, d.score * 100).toFixed(0)}%`,
                                    background: d.score >= 0.7 ? '#059669' : d.score >= 0.5 ? '#d97706' : '#dc2626',
                                  }}
                                />
                              </div>
                              <span className="text-emerald-600 font-mono text-[10px] w-10 text-right">
                                {d.score.toFixed(3)}
                              </span>
                            </div>
                          )}
                          <p className="text-gray-600">{d.content}</p>
                        </div>
                      ))}
                    </div>
                  )
                } catch { return <p className="text-gray-500">{node.retrievedDocs}</p> }
              })()}
            </div>
          )}

          {/* TOOL 节点 Input/Output 展开面板 */}
          {node.nodeType === 'TOOL' && node.metadata && (() => {
            try {
              const meta = JSON.parse(node.metadata) as Record<string, string>
              return (
                <div className="space-y-2 text-gray-600">
                  {meta.tool_input && (
                    <div>
                      <p className="text-gray-400 mb-1">输入参数：</p>
                      <pre className="whitespace-pre-wrap break-all rounded p-2 text-xs bg-white border border-gray-100 text-gray-700 max-h-40 overflow-auto">
                        {meta.tool_input}
                      </pre>
                    </div>
                  )}
                  {meta.tool_output && (
                    <div>
                      <p className="text-gray-400 mb-1">输出结果：</p>
                      <pre className="whitespace-pre-wrap break-all rounded p-2 text-xs bg-white border border-gray-100 text-gray-700 max-h-48 overflow-auto">
                        {meta.tool_output}
                      </pre>
                    </div>
                  )}
                  {!meta.tool_input && !meta.tool_output && meta.tool_name && (
                    <p><span className="text-gray-400">工具：</span>{meta.tool_name}</p>
                  )}
                </div>
              )
            } catch { return null }
          })()}

          {/* 通用 metadata（非 TOOL 节点） */}
          {node.nodeType !== 'TOOL' && node.nodeType !== 'LLM' && node.nodeType !== 'RETRIEVER' && node.metadata && (() => {
            try {
              const meta = JSON.parse(node.metadata) as Record<string, unknown>
              return (
                <div className="space-y-1 text-gray-600">
                  {Object.entries(meta).map(([k, v]) => (
                    <p key={k}><span className="text-gray-400">{k}：</span>{String(v)}</p>
                  ))}
                </div>
              )
            } catch { return <p className="text-gray-500">{node.metadata}</p> }
          })()}

          {node.errorMessage && (
            <p className="text-red-600 mt-1">[{node.errorCode}] {node.errorMessage}</p>
          )}
        </div>
      )}
    </div>
  )
}

export default function TraceDetail() {
  const { traceId } = useParams<{ traceId: string }>()
  const navigate = useNavigate()
  const [data, setData] = useState<Awaited<ReturnType<typeof traceService.detail>> | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [activeTab, setActiveTab] = useState<'nodes'>('nodes')
  const [exportLoading, setExportLoading] = useState(false)

  useEffect(() => {
    if (!traceId) return
    setLoading(true)
    traceService.detail(traceId)
      .then(d => setData(d))
      .catch(e => setError(String(e)))
      .finally(() => setLoading(false))
  }, [traceId])

  // 节点统计（useMemo 必须在条件返回前调用）
  const nodeStats = useMemo(() => {
    const nodes = data?.nodes || []
    const total = nodes.length
    const successCount = nodes.filter(n => n.status === 'success').length
    const errorCount   = nodes.filter(n => n.status === 'error').length
    const avgDuration  = total > 0
      ? Math.round(nodes.reduce((sum, n) => sum + (n.durationMs || 0), 0) / total)
      : 0
    const topSlowestId = nodes.reduce<string>((maxId, n) => {
      if (!maxId) return n.nodeId
      const maxNode = nodes.find(x => x.nodeId === maxId)
      return (n.durationMs || 0) > (maxNode?.durationMs || 0) ? n.nodeId : maxId
    }, '')
    return { total, successCount, errorCount, avgDuration, topSlowestId }
  }, [data])

  // 关键路径与并行分析
  const childrenMap = useMemo(() => buildChildrenMap(data?.nodes || []), [data])
  const criticalPath = useMemo(() => computeCriticalPath(data?.nodes || [], childrenMap), [data, childrenMap])

  // Cache Hit Rate 统计
  const cacheStats = useMemo(() => {
    const cacheNodes = data?.nodes.filter(n => n.nodeType === 'CACHE') || []
    const hits = cacheNodes.filter(n => {
      try { return (JSON.parse(n.metadata || '{}') as Record<string, unknown>)?.hit === true } catch { return false }
    }).length
    return { hits, total: cacheNodes.length }
  }, [data])

  if (loading) {
    return (
      <div className="flex items-center justify-center h-48">
        <Loader />
      </div>
    )
  }
  if (error || !data) {
    return <p className="p-6 text-red-500 text-sm">{error || '未找到链路数据'}</p>
  }

  const traceStartMs  = new Date(data.startTime).getTime()
  const totalMs       = data.durationMs || 1
  const sb = statusBadge[data.status] || statusBadge.running
  const StatusIcon = sb.icon

  return (
    <div className="flex flex-col gap-5 h-full">

      {/* 返回 */}
      <button
        onClick={() => navigate('/traces')}
        className="flex items-center gap-2 text-sm text-gray-500 hover:text-gray-900 transition-colors w-fit"
      >
        <ArrowLeft className="w-4 h-4" />
        返回链路列表
      </button>

      {/* 头部信息卡 */}
      <div className="card card-body">
        <div className="flex items-start justify-between gap-4 mb-4">
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2 mb-1.5">
              <Activity className="w-4 h-4 text-indigo-500 flex-shrink-0" />
              <code className="text-sm font-mono text-indigo-600 bg-indigo-50 px-2 py-0.5 rounded">
                {data.traceId}
              </code>
            </div>
            <p className="text-gray-700 text-sm leading-relaxed">
              {data.queryText || '(无查询文本)'}
            </p>
          </div>
          <span className={cn('inline-flex items-center gap-1.5 px-3 py-1 rounded-full text-sm font-medium flex-shrink-0', sb.cls)}>
            <StatusIcon className="w-3.5 h-3.5" />
            {sb.label}
          </span>
        </div>

        <div className="grid grid-cols-2 md:grid-cols-4 gap-4 text-sm pt-4 border-t border-gray-100">
          <div>
            <p className="text-xs text-gray-400 mb-0.5">入口</p>
            <p className="text-gray-700 truncate">{data.entryPoint || '-'}</p>
          </div>
          <div>
            <p className="text-xs text-gray-400 mb-0.5">总耗时</p>
            <p className="font-semibold text-amber-600 tabular-nums">{formatDuration(data.durationMs)}</p>
          </div>
          <div>
            <p className="text-xs text-gray-400 mb-0.5">Token（↑输入 / ↓输出）</p>
            <p className="tabular-nums">
              <span className="text-blue-600">{data.totalInputTokens}</span>
              <span className="text-gray-300 mx-1">/</span>
              <span className="text-violet-600">{data.totalOutputTokens}</span>
            </p>
          </div>
          <div>
            <p className="text-xs text-gray-400 mb-0.5">开始时间</p>
            <p className="text-gray-600 text-xs">{new Date(data.startTime).toLocaleString('zh-CN')}</p>
          </div>
        </div>

        {/* LLM Token 消耗时序图（LLM 节点 > 1 时显示） */}
        {data.nodes.filter(n => n.nodeType === 'LLM').length > 1 && (
          <div className="mt-3 pt-3 border-t border-gray-100">
            <p className="text-[10px] text-gray-400 mb-1.5">LLM Token 消耗时序</p>
            <div className="relative h-6 bg-gray-50 rounded overflow-hidden">
              {(() => {
                const llmNodes = data.nodes
                  .filter(n => n.nodeType === 'LLM')
                  .sort((a, b) => new Date(a.startTime).getTime() - new Date(b.startTime).getTime())
                const maxTokens = Math.max(...llmNodes.map(n => (n.inputTokens || 0) + (n.outputTokens || 0)), 1)
                return llmNodes.map(n => {
                  const nodeStart = new Date(n.startTime).getTime() - traceStartMs
                  const left = totalMs > 0 ? (nodeStart / totalMs) * 100 : 0
                  const tokens = (n.inputTokens || 0) + (n.outputTokens || 0)
                  const height = Math.max(20, (tokens / maxTokens) * 100)
                  return (
                    <div
                      key={n.nodeId}
                      className="absolute bottom-0 bg-violet-400 opacity-70 rounded-sm"
                      style={{ left: `${left}%`, width: '2%', height: `${height}%`, minWidth: 4 }}
                      title={`${n.nodeName}: ↑${n.inputTokens} ↓${n.outputTokens}`}
                    />
                  )
                })
              })()}
            </div>
          </div>
        )}

      </div>

      {/* Tab 导航 */}
      <div className="flex items-center gap-1 border-b border-gray-200">
        <button
          onClick={() => setActiveTab('nodes')}
          className={cn(
            'flex items-center gap-1.5 px-4 py-2 text-sm font-medium border-b-2 -mb-px transition-colors',
            activeTab === 'nodes'
              ? 'border-indigo-500 text-indigo-600'
              : 'border-transparent text-gray-500 hover:text-gray-700',
          )}
        >
          <Activity className="w-4 h-4" />
          节点树
        </button>
        <button
          onClick={async () => {
            if (exportLoading || !traceId) return
            setExportLoading(true)
            try { await traceService.export(traceId) } catch { /* silent */ }
            finally { setExportLoading(false) }
          }}
          className="flex items-center gap-1.5 px-4 py-2 text-sm font-medium text-gray-500 hover:text-gray-700 transition-colors ml-auto"
        >
          {exportLoading
            ? <Loader2 className="w-4 h-4 animate-spin" />
            : <Download className="w-4 h-4" />}
          导出 JSON
        </button>
      </div>

      {/* 节点树 Tab */}
      {activeTab === 'nodes' && (
      <div className="card flex-1 min-h-0 flex flex-col overflow-hidden">
        {/* 表头 */}
        <div className="flex items-center gap-2 px-3 py-2.5 border-b border-gray-100 flex-shrink-0">
          <div className="w-4" />
          <div className="w-24 text-xs font-semibold text-gray-500">类型</div>
          <div className="flex-1 text-xs font-semibold text-gray-500">节点名</div>
          <div className="w-4" />
          <div className="w-16 text-xs font-semibold text-gray-500 text-right">耗时</div>
          <div className="w-36 text-xs font-semibold text-gray-500">
            时间轴（{formatDuration(totalMs)}）
          </div>
          <div className="w-36 text-xs font-semibold text-gray-500 text-right">Token / 成本</div>
        </div>

        {/* 图例 */}
        <div className="flex items-center gap-4 px-4 py-1.5 bg-gray-50 border-b border-gray-100 flex-shrink-0 flex-wrap">
          {Object.entries(nodeTypeConfig).map(([type, cfg]) => {
            const Icon = cfg.icon
            return (
              <span key={type} className={cn('inline-flex items-center gap-1 text-xs', cfg.textClass)}>
                <Icon className="w-3 h-3" />
                {cfg.label}
              </span>
            )
          })}
          {/* 节点统计摘要 + Cache Hit Rate */}
          {nodeStats.total > 0 && (
            <div className="ml-auto flex items-center gap-3 text-xs text-gray-400 flex-wrap">
              <span>共 <span className="font-medium text-gray-600">{nodeStats.total}</span> 节点</span>
              {nodeStats.successCount > 0 && (
                <span className="text-emerald-600">✓ {nodeStats.successCount}</span>
              )}
              {nodeStats.errorCount > 0 && (
                <span className="text-red-500">✗ {nodeStats.errorCount}</span>
              )}
              <span>均值 <span className="font-medium text-amber-600 tabular-nums">{formatDuration(nodeStats.avgDuration)}</span></span>
              {cacheStats.total > 0 && (
                <span className="text-cyan-600">
                  缓存命中 {cacheStats.hits}/{cacheStats.total} ({Math.round(cacheStats.hits / cacheStats.total * 100)}%)
                </span>
              )}
            </div>
          )}
        </div>

        {/* 节点列表 */}
        <div className="overflow-auto flex-1 py-1">
          {data.nodes.length === 0 ? (
            <p className="text-center py-8 text-sm text-gray-400">暂无节点数据</p>
          ) : (
            data.nodes.map(node => (
              <NodeRow
                key={node.nodeId}
                node={node}
                traceStartMs={traceStartMs}
                totalMs={totalMs}
                isTopSlowest={node.nodeId === nodeStats.topSlowestId}
                isCritical={criticalPath.has(node.nodeId)}
                selfTime={computeSelfTime(node, childrenMap)}
                offsetMs={Math.max(0, new Date(node.startTime).getTime() - traceStartMs)}
              />
            ))
          )}
        </div>
      </div>
      )}
    </div>
  )
}
