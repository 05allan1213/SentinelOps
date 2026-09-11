import RuntimeQueryError from './RuntimeQueryError'
import RuntimeResourcePagination from './RuntimeResourcePagination'
import { useState } from 'react'
import { useRuntimeAttempts } from '@/hooks/useRuntimeQueries'
import { cn } from '@/utils'
import RuntimeQualityState from './RuntimeQualityState'
import type { AttemptDTO } from '@/types/runtime'

interface Props {
  runId: string
}

const dash = (value: unknown) => (value === null || value === undefined || value === '' ? '—' : String(value))

const MODE_LABEL: Record<string, string> = { fresh: '首次执行', resume: '恢复', replay: '重放' }

function AttemptRow({ attempt }: { attempt: AttemptDTO }) {
  return (
    <li data-testid="runtime-attempt-row" data-attempt={attempt.attempt} className="rounded-lg border border-gray-200 bg-white p-3">
      <div className="flex flex-wrap items-center gap-3">
        <span className="font-mono text-xs text-gray-900">#{attempt.attempt}</span>
        <span className="rounded border border-indigo-200 bg-indigo-50 px-1.5 py-0.5 text-xs text-indigo-700">{MODE_LABEL[attempt.mode] ?? dash(attempt.mode)}</span>
        <span className="text-xs text-gray-600">{dash(attempt.status)} · {dash(attempt.current_phase)}</span>
        <RuntimeQualityState availability={attempt.availability} dataQuality={attempt.usage_quality} reasonCode={attempt.reason_code} />
      </div>
      <dl className="mt-2 grid grid-cols-2 gap-2 text-xs lg:grid-cols-4">
        <div><dt className="text-gray-500">Worker（脱敏）</dt><dd className="font-mono text-gray-800 break-all">{dash(attempt.worker_id)}</dd></div>
        <div><dt className="text-gray-500">Generation</dt><dd className="tabular-nums text-gray-800">{dash(attempt.lease_generation)}</dd></div>
        <div><dt className="text-gray-500">Trace</dt><dd className="font-mono text-gray-800 break-all">{dash(attempt.trace_id)}</dd></div>
        <div><dt className="text-gray-500">Operation</dt><dd className="font-mono text-gray-800 break-all">{dash(attempt.operation_id)}</dd></div>
        <div><dt className="text-gray-500">Retry / Failover</dt><dd className="tabular-nums text-gray-800">{attempt.retry_count} / {attempt.failover_count}</dd></div>
        <div><dt className="text-gray-500">Run 兼容性哈希</dt><dd className="font-mono text-gray-800 break-all">{dash(attempt.run_compatibility_hash)}</dd></div>
        <div><dt className="text-gray-500">Checkpoint 兼容性哈希</dt><dd className="font-mono text-gray-800 break-all">{dash(attempt.checkpoint_compatibility_hash)}</dd></div>
        <div><dt className="text-gray-500">执行 Worker 指纹</dt><dd className="font-mono text-gray-800 break-all">{dash(attempt.executing_worker_fingerprint)}</dd></div>
      </dl>
      {(attempt.failure_code || attempt.failure_message) && (
        <p className="mt-2 rounded-lg border border-red-100 bg-red-50 px-2 py-1 text-xs text-red-700">
          {dash(attempt.failure_code)}：{dash(attempt.failure_message)}
        </p>
      )}
    </li>
  )
}

export default function AttemptsPanel({ runId }: Props) {
  const [page, setPage] = useState(1)
  const query = useRuntimeAttempts(runId, { page, page_size: 20 })
  const data = query.data
  const items = data?.items ?? []

  return (
    <section data-testid="runtime-attempts-panel" className={cn('rounded-xl border border-gray-200 bg-white p-4', query.isFetching && 'opacity-90')}>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h3 className="text-sm font-medium text-gray-900">Attempts（服务端投影）</h3>
        <RuntimeQualityState availability={data?.availability} dataQuality={data?.data_quality} reasonCode={data?.reason_code} notRun={data?.not_run} />
      </div>
      <RuntimeQueryError query={query} />
      {query.isError && !data ? null : query.isLoading && !data ? (
        <p data-testid="runtime-attempts-loading" className="mt-3 text-sm text-gray-500">加载中…</p>
      ) : items.length === 0 ? (
        <p data-testid="runtime-attempts-empty" className="mt-3 text-sm text-gray-500">
          {data?.availability === 'unavailable' ? 'Attempt 投影当前不可用' : '暂无 Attempt 记录'}
        </p>
      ) : (
        <ul className="mt-3 flex flex-col gap-2">
          {items.map(attempt => <AttemptRow key={attempt.attempt_id} attempt={attempt} />)}
        </ul>
      )}
      <RuntimeResourcePagination label="Attempts" page={page} meta={data?.page} loading={query.isFetching} onChange={setPage} />
    </section>
  )
}
