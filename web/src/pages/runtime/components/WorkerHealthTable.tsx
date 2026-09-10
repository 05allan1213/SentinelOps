import { cn } from '@/utils'
import RuntimeQualityState from './RuntimeQualityState'
import type { WorkerHealthRes } from '@/types/runtime'

interface Props {
  data: WorkerHealthRes
  loading: boolean
}

const dash = (value: unknown) => (value === null || value === undefined || value === '' ? '—' : String(value))

const formatTime = (value: string | null | undefined) => {
  if (!value) return '—'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN', { hour12: false })
}

const STATUS_TONE: Record<string, string> = {
  active: 'border-emerald-200 bg-emerald-50 text-emerald-700',
  idle: 'border-gray-300 bg-gray-100 text-gray-600',
  stale: 'border-amber-200 bg-amber-50 text-amber-800',
}

export default function WorkerHealthTable({ data, loading }: Props) {
  const items = data.items ?? []
  const aggregate = data.aggregate

  return (
    <div className="min-w-0">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-xs text-gray-500">只展示持久化 Worker 观测；API 进程状态不会伪装成 Worker 健康。</p>
        <RuntimeQualityState availability={data.availability} dataQuality={data.data_quality} reasonCode={data.reason_code} notRun={data.not_run} />
      </div>

      <dl data-testid="runtime-worker-aggregate" className="mt-3 grid grid-cols-2 gap-3 lg:grid-cols-4">
        {([['total', 'Worker 总数'], ['active', '活跃'], ['idle', '空闲'], ['stale', '过期']] as const).map(([key, label]) => (
          <div key={key} className="rounded-xl border border-gray-200 bg-white p-3">
            <dt className="text-xs text-gray-500">{label}</dt>
            <dd data-testid={`runtime-worker-aggregate-${key}`} className="mt-1 text-xl font-semibold tabular-nums text-gray-900">
              {aggregate?.[key] === null || aggregate?.[key] === undefined ? '—' : aggregate[key]}
            </dd>
          </div>
        ))}
      </dl>

      {items.length === 0 && !loading ? (
        <p data-testid="runtime-worker-empty" className="mt-3 rounded-xl border border-gray-200 bg-white px-4 py-8 text-center text-sm text-gray-500">
          {data.availability === 'unavailable' ? 'Worker 观测当前不可用（reason_code: not_observed）' : '暂无 Worker 观测记录'}
        </p>
      ) : (
        <div data-testid="runtime-worker-table" className="mt-3 min-w-0 w-full overflow-x-auto rounded-xl border border-gray-200 bg-white">
          <table className="w-full min-w-[980px] border-collapse text-left">
            <caption className="sr-only">持久化 Worker 观测</caption>
            <thead>
              <tr className="border-b border-gray-200 bg-gray-50/70 text-xs text-gray-500">
                <th scope="col" className="px-3 py-2">Worker</th>
                <th scope="col" className="px-3 py-2">状态</th>
                <th scope="col" className="px-3 py-2">最后心跳</th>
                <th scope="col" className="px-3 py-2">Runtime / 兼容性</th>
                <th scope="col" className="px-3 py-2">Active Run / Generation</th>
                <th scope="col" className="px-3 py-2">观测 MCP / Skill</th>
                <th scope="col" className="px-3 py-2">Last error</th>
              </tr>
            </thead>
            <tbody>
              {loading && items.length === 0 && (
                <tr><td colSpan={7} className="px-3 py-8 text-center text-sm text-gray-500">加载中…</td></tr>
              )}
              {items.map(worker => (
                <tr key={worker.worker_id} data-testid="runtime-worker-row" data-worker-id={worker.worker_id} className="border-b border-gray-100 last:border-b-0 text-xs text-gray-700">
                  <td className="px-3 py-2 font-mono text-gray-900 break-all">{dash(worker.worker_id)}</td>
                  <td className="px-3 py-2">
                    <span data-testid="runtime-worker-status" data-status={worker.status} className={cn('rounded border px-1.5 py-0.5', STATUS_TONE[worker.status] ?? 'border-dashed border-gray-300 bg-white text-gray-500')}>
                      {dash(worker.status)}
                    </span>
                  </td>
                  <td className="px-3 py-2 tabular-nums">{formatTime(worker.heartbeat_at)}</td>
                  <td className="px-3 py-2 font-mono break-all">{dash(worker.runtime_version)} / {dash(worker.runtime_compatibility_hash)}</td>
                  <td className="px-3 py-2 font-mono break-all">{dash(worker.active_run_id)} / {worker.active_generation ?? '—'}</td>
                  <td className="px-3 py-2 tabular-nums">{worker.observed_mcp_count} / {worker.observed_skill_count}</td>
                  <td className="px-3 py-2 text-red-700 break-words">{dash(worker.last_error)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
