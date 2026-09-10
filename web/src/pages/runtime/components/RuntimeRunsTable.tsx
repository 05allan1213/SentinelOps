import { Bot } from 'lucide-react'
import { cn } from '@/utils'
import RuntimeStatusBadge from './RuntimeStatusBadge'
import { isLegacyRun } from './runtimeStatus'
import type { ListRunsRes, RunSummaryDTO } from '@/types/runtime'

interface Props {
  data: ListRunsRes
  onSelectRun: (runId: string) => void
  loading: boolean
}

const COLUMNS = [
  { key: 'run_id', label: 'Run', width: 'w-[220px]' },
  { key: 'session_id', label: 'Session', width: 'w-[160px]' },
  { key: 'agent', label: 'Agent', width: 'w-[140px]' },
  { key: 'status', label: '状态 / 阶段', width: 'w-[190px]' },
  { key: 'attempt', label: 'Attempt', width: 'w-[90px]' },
  { key: 'worker', label: 'Worker / Lease', width: 'w-[200px]' },
  { key: 'recovery', label: 'Recovery', width: 'w-[120px]' },
  { key: 'budget', label: 'Budget', width: 'w-[170px]' },
  { key: 'time', label: '开始时间', width: 'w-[180px]' },
  { key: 'duration', label: '耗时', width: 'w-[110px]' },
]

const formatTime = (value: string | null | undefined) => {
  if (!value) return '—'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN', { hour12: false })
}

const formatDuration = (value: number | null | undefined) => (
  typeof value === 'number' && Number.isFinite(value) ? `${Math.max(0, Math.round(value))} ms` : '—'
)

const budgetSummary = (run: RunSummaryDTO) => {
  const model = run.budget?.model_calls
  const tool = run.budget?.tool_calls
  if (model === null || model === undefined || tool === null || tool === undefined) {
    return { text: '—', title: '预算计数不可用' }
  }
  return { text: `M${model} / T${tool}`, title: '模型调用 / 工具调用' }
}

export default function RuntimeRunsTable({ data, onSelectRun, loading }: Props) {
  const items = data.items ?? []

  return (
    <div
      data-testid="runtime-runs-table"
      className="min-w-0 w-full overflow-x-auto rounded-xl border border-gray-200 bg-white"
    >
      <table className="w-full min-w-[1180px] border-collapse text-left">
        <caption className="sr-only">Agent Runtime 运行列表</caption>
        <thead>
          <tr className="border-b border-gray-200 bg-gray-50/70">
            {COLUMNS.map(column => (
              <th key={column.key} scope="col" className={cn('px-3 py-2 text-xs font-medium text-gray-500 whitespace-nowrap', column.width)}>
                {column.label}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {loading && items.length === 0 && Array.from({ length: 5 }).map((_, index) => (
            <tr key={`skeleton-${index}`} data-testid="runtime-runs-skeleton" className="border-b border-gray-100">
              {COLUMNS.map(column => (
                <td key={column.key} className="px-3 py-3"><div className="h-4 rounded bg-gray-100 animate-pulse" /></td>
              ))}
            </tr>
          ))}
          {!loading && items.length === 0 && (
            <tr>
              <td colSpan={COLUMNS.length} className="px-3 py-10 text-center text-sm text-gray-500">
                暂无数据
              </td>
            </tr>
          )}
          {items.map(run => {
            const budget = budgetSummary(run)
            const legacy = isLegacyRun(run)
            return (
              <tr
                key={run.run_id}
                data-testid="runtime-run-row"
                data-run-id={run.run_id}
                tabIndex={0}
                onClick={() => onSelectRun(run.run_id)}
                onKeyDown={event => { if (event.key === 'Enter') onSelectRun(run.run_id) }}
                className="border-b border-gray-100 last:border-b-0 cursor-pointer hover:bg-indigo-50/40 focus:outline-none focus:bg-indigo-50/40 transition-colors duration-150"
              >
                <td className="px-3 py-2.5 font-mono text-xs text-gray-900">
                  <span className="inline-flex items-center gap-2 min-w-0">
                    <span className="truncate max-w-[190px]" title={run.run_id}>{run.run_id}</span>
                    {legacy && (
                      <span data-testid="runtime-legacy-chip" className="shrink-0 rounded border border-gray-300 bg-gray-100 px-1.5 py-0.5 text-[11px] text-gray-600">
                        仅历史记录
                      </span>
                    )}
                  </span>
                </td>
                <td className="px-3 py-2.5 font-mono text-xs text-gray-600"><span className="block truncate max-w-[150px]" title={run.session_id}>{run.session_id || '—'}</span></td>
                <td className="px-3 py-2.5 text-sm text-gray-700">
                  <span className="inline-flex items-center gap-1.5 min-w-0">
                    <Bot className="w-3.5 h-3.5 text-gray-400 shrink-0" />
                    <span className="truncate max-w-[110px]" title={run.agent}>{run.agent || '—'}</span>
                  </span>
                </td>
                <td className="px-3 py-2.5"><RuntimeStatusBadge status={run.status} phase={run.current_phase} /></td>
                <td className="px-3 py-2.5 text-sm text-gray-700 tabular-nums">{run.attempt}</td>
                <td className="px-3 py-2.5 text-xs text-gray-600">
                  <span className="block truncate max-w-[180px] font-mono" title={run.worker_id}>{run.worker_id || '—'}</span>
                  <span className="text-gray-400">gen {run.lease_generation} · {run.lease_state || '—'}</span>
                </td>
                <td className="px-3 py-2.5 text-xs text-gray-600">{run.recovery_mode || '—'}</td>
                <td className="px-3 py-2.5 text-xs text-gray-700 tabular-nums" title={budget.title}>{budget.text}</td>
                <td className="px-3 py-2.5 text-xs text-gray-600 tabular-nums">{formatTime(run.started_at)}</td>
                <td className="px-3 py-2.5 text-xs text-gray-700 tabular-nums">{formatDuration(run.duration_ms)}</td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}
