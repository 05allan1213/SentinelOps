import { createPortal } from 'react-dom'
import { X, Loader2, ChevronRight, ChevronDown, ThumbsUp, ThumbsDown } from 'lucide-react'
import { useState } from 'react'
import { cn } from '@/utils'
import type { TraceDetail, TraceNodeItem } from '@/services/rageval'

interface Props {
  detail: TraceDetail | null
  loading: boolean
  onClose: () => void
}

function fmtMs(ms: number) {
  if (ms <= 0) return '—'
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(1)}s`
}

const NODE_TYPE_COLOR: Record<string, string> = {
  LLM:       'bg-violet-100 text-violet-700',
  TOOL:      'bg-amber-100 text-amber-700',
  RETRIEVER: 'bg-blue-100 text-blue-700',
  EMBEDDING: 'bg-cyan-100 text-cyan-700',
  CACHE:     'bg-emerald-100 text-emerald-700',
  AGENT:     'bg-indigo-100 text-indigo-700',
  LAMBDA:    'bg-gray-100 text-gray-600',
}

function NodeRow({ node, depth = 0 }: { node: TraceNodeItem; depth?: number }) {
  const [expanded, setExpanded] = useState(true)
  const hasChildren = node.children && node.children.length > 0
  const isRetriever = node.node_type === 'RETRIEVER'

  return (
    <div>
      <div
        className={cn(
          'flex items-start gap-2 px-3 py-2 rounded-lg hover:bg-gray-50 transition-colors',
          node.status === 'error' && 'bg-red-50/50',
        )}
        style={{ marginLeft: depth * 16 }}
      >
        {/* 展开/折叠 */}
        <button
          className="mt-0.5 flex-shrink-0 w-4 h-4 flex items-center justify-center text-gray-400"
          onClick={() => hasChildren && setExpanded(v => !v)}
        >
          {hasChildren
            ? expanded ? <ChevronDown className="w-3.5 h-3.5" /> : <ChevronRight className="w-3.5 h-3.5" />
            : <span className="w-1.5 h-1.5 rounded-full bg-gray-300 inline-block" />
          }
        </button>

        {/* 节点类型标签 */}
        <span className={cn(
          'flex-shrink-0 text-[10px] font-semibold px-1.5 py-0.5 rounded mt-0.5',
          NODE_TYPE_COLOR[node.node_type] ?? 'bg-gray-100 text-gray-600',
        )}>
          {node.node_type}
        </span>

        {/* 节点名称 */}
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 flex-wrap">
            <span className="text-xs font-medium text-gray-700 truncate">{node.node_name || node.node_type}</span>
            {node.model_name && (
              <span className="text-[10px] text-gray-400">{node.model_name}</span>
            )}
            <span className={cn(
              'text-[10px] px-1.5 py-0.5 rounded-full font-medium',
              node.status === 'success' ? 'bg-emerald-50 text-emerald-600' : 'bg-red-50 text-red-500',
            )}>
              {node.status}
            </span>
            <span className="text-[10px] text-gray-400 tabular-nums">{fmtMs(node.duration_ms)}</span>
            {node.input_tokens ? (
              <span className="text-[10px] text-gray-400">↑{node.input_tokens} ↓{node.output_tokens}</span>
            ) : null}
            {node.cache_hit && (
              <span className="text-[10px] text-emerald-600 font-medium">缓存命中</span>
            )}
          </div>

          {/* RETRIEVER 专属指标 */}
          {isRetriever && (
            <div className="mt-1 flex flex-wrap gap-3 text-[10px] text-gray-500">
              <span>召回数: <strong className="text-gray-700">{node.doc_count ?? node.final_top_k}</strong></span>
              {node.max_vector_score ? (
                <span>最高相似度: <strong className="text-gray-700">{(node.max_vector_score * 100).toFixed(1)}%</strong></span>
              ) : null}
              {node.avg_vector_score ? (
                <span>平均相似度: <strong className="text-gray-700">{(node.avg_vector_score * 100).toFixed(1)}%</strong></span>
              ) : null}
              {node.rerank_used && node.avg_rerank_score ? (
                <span>Rerank均分: <strong className="text-gray-700">{(node.avg_rerank_score * 100).toFixed(1)}%</strong></span>
              ) : null}
              {node.final_top_k === 0 && (
                <span className="text-red-500 font-medium">无召回文档</span>
              )}
            </div>
          )}

          {node.error_message && (
            <p className="mt-1 text-[10px] text-red-500 truncate">{node.error_message}</p>
          )}
        </div>
      </div>

      {/* 子节点 */}
      {hasChildren && expanded && node.children!.map(child => (
        <NodeRow key={child.node_id} node={child} depth={depth + 1} />
      ))}
    </div>
  )
}

export default function TraceDetailModal({ detail, loading, onClose }: Props) {
  return createPortal(
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      <div className="absolute inset-0 bg-black/40 backdrop-blur-sm" onClick={onClose} />
      <div className="relative bg-white rounded-2xl shadow-2xl w-full max-w-2xl max-h-[85vh] flex flex-col">

        {/* 头部 */}
        <div className="flex items-center justify-between px-5 py-4 border-b border-gray-100 flex-shrink-0">
          <div>
            <p className="text-sm font-semibold text-gray-900">链路详情</p>
            {detail && (
              <p className="text-xs text-gray-400 mt-0.5 font-mono">{detail.trace_id}</p>
            )}
          </div>
          <button onClick={onClose} className="p-1.5 rounded-lg hover:bg-gray-100 transition-colors">
            <X className="w-4 h-4 text-gray-500" />
          </button>
        </div>

        {loading ? (
          <div className="flex-1 flex items-center justify-center py-16">
            <Loader2 className="w-6 h-6 animate-spin text-gray-300" />
          </div>
        ) : detail ? (
          <div className="flex-1 overflow-y-auto">
            {/* 基本信息 */}
            <div className="px-5 py-4 border-b border-gray-50 grid grid-cols-2 gap-3 text-xs">
              <div>
                <p className="text-gray-400">链路名称</p>
                <p className="font-medium text-gray-700 mt-0.5">{detail.trace_name || '—'}</p>
              </div>
              <div>
                <p className="text-gray-400">状态</p>
                <p className={cn('font-medium mt-0.5', detail.status === 'success' ? 'text-emerald-600' : 'text-red-500')}>
                  {detail.status}
                </p>
              </div>
              <div>
                <p className="text-gray-400">总耗时</p>
                <p className="font-medium text-gray-700 mt-0.5">{fmtMs(detail.duration_ms)}</p>
              </div>
              <div>
                <p className="text-gray-400">Token 消耗</p>
                <p className="font-medium text-gray-700 mt-0.5">
                  ↑{detail.total_input_tokens} ↓{detail.total_output_tokens}
                  {detail.estimated_cost_usd > 0 && (
                    <span className="text-gray-400 ml-1">(${detail.estimated_cost_usd.toFixed(4)})</span>
                  )}
                </p>
              </div>
              {detail.query_text && (
                <div className="col-span-2">
                  <p className="text-gray-400">查询内容</p>
                  <p className="font-medium text-gray-700 mt-0.5 line-clamp-2">{detail.query_text}</p>
                </div>
              )}
              {detail.feedback_vote !== 0 && (
                <div>
                  <p className="text-gray-400">用户反馈</p>
                  <div className="flex items-center gap-1 mt-0.5">
                    {detail.feedback_vote === 1
                      ? <><ThumbsUp className="w-3.5 h-3.5 text-emerald-500" /><span className="text-emerald-600 font-medium">有帮助</span></>
                      : <><ThumbsDown className="w-3.5 h-3.5 text-red-400" /><span className="text-red-500 font-medium">没帮助</span></>
                    }
                  </div>
                </div>
              )}
            </div>

            {/* 节点树 */}
            <div className="px-3 py-3">
              <p className="text-xs font-semibold text-gray-500 px-2 mb-2">执行节点</p>
              {detail.nodes.length === 0 ? (
                <p className="text-xs text-gray-400 px-2">暂无节点数据</p>
              ) : (
                detail.nodes.map(node => (
                  <NodeRow key={node.node_id} node={node} depth={0} />
                ))
              )}
            </div>
          </div>
        ) : null}
      </div>
    </div>,
    document.body,
  )
}
