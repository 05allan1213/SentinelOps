import RuntimeQueryError from './RuntimeQueryError'
import RuntimeResourcePagination from './RuntimeResourcePagination'
import { useState } from 'react'
import { Link } from 'react-router-dom'
import { ExternalLink, Route } from 'lucide-react'
import { useRuntimeTraces } from '@/hooks/useRuntimeQueries'
import { cn } from '@/utils'
import RuntimeQualityState from './RuntimeQualityState'
import type { TraceAggregateDTO } from '@/types/runtime'

interface Props {
  runId: string
}

const dash = (value: unknown) => (value === null || value === undefined || value === '' ? '—' : String(value))

const QUALITY_TONE: Record<string, string> = {
  complete: 'text-emerald-700',
  reconstructed: 'text-blue-700',
  partial: 'text-amber-700',
  unknown: 'text-gray-500',
}

function TraceRow({ trace }: { trace: TraceAggregateDTO }) {
  return (
    <li data-testid="runtime-trace-row" data-trace-id={trace.trace_id} data-raw-available={String(trace.raw_available)} className="rounded-lg border border-gray-200 bg-white p-3">
      <div className="flex flex-wrap items-center gap-2">
        <Route className="w-3.5 h-3.5 text-gray-400" />
        <span className="font-mono text-xs text-gray-900 break-all">{trace.trace_id}</span>
        <span className="text-xs text-gray-600">attempt {trace.attempt}</span>
        <span className="text-xs text-gray-600">{dash(trace.status)}</span>
        <span className={cn('text-xs', QUALITY_TONE[trace.trace_quality] ?? 'text-gray-500')}>quality: {trace.trace_quality}</span>
        {trace.raw_available && trace.detail_url ? (
          <Link data-testid="runtime-trace-link" to={trace.detail_url} className="inline-flex items-center gap-1 text-xs text-indigo-700 hover:text-indigo-900">
            <ExternalLink className="w-3 h-3" />
            查看原始 Trace
          </Link>
        ) : (
          <span data-testid="runtime-trace-unavailable" className="text-xs text-gray-500">原始节点不可用{trace.reason_code ? `（${trace.reason_code}）` : ''}</span>
        )}
      </div>
      <dl className="mt-2 grid grid-cols-2 gap-2 text-xs lg:grid-cols-5">
        <div><dt className="text-gray-500">耗时</dt><dd className="tabular-nums text-gray-800">{trace.duration_ms} ms</dd></div>
        <div><dt className="text-gray-500">Tokens（入 / 出）</dt><dd className="tabular-nums text-gray-800">{trace.input_tokens} / {trace.output_tokens}</dd></div>
        <div><dt className="text-gray-500">成本 (CNY)</dt><dd className="tabular-nums text-gray-800">{trace.cost_cny}</dd></div>
        <div><dt className="text-gray-500">节点数</dt><dd className="tabular-nums text-gray-800">{trace.node_count}</dd></div>
        <div><dt className="text-gray-500">原始可用</dt><dd className="text-gray-800">{trace.raw_available ? '是' : '否'}</dd></div>
      </dl>
      <div className="mt-2">
        <RuntimeQualityState availability={trace.availability} dataQuality={trace.data_quality} reasonCode={trace.reason_code} />
      </div>
    </li>
  )
}

export default function TracePanel({ runId }: Props) {
  const [page, setPage] = useState(1)
  const query = useRuntimeTraces(runId, { page, page_size: 50 })
  const data = query.data
  const items = data?.items ?? []

  return (
    <section data-testid="runtime-trace-panel" className={cn('rounded-xl border border-gray-200 bg-white p-4', query.isFetching && 'opacity-90')}>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h3 className="text-sm font-medium text-gray-900">Run → Attempt → Trace</h3>
        <RuntimeQualityState availability={data?.availability} dataQuality={data?.data_quality} reasonCode={data?.reason_code} notRun={data?.not_run} />
      </div>
      <RuntimeQueryError query={query} />
      {query.isError && !data ? null : query.isLoading && !data ? (
        <p data-testid="runtime-trace-loading" className="mt-3 text-sm text-gray-500">加载中…</p>
      ) : items.length === 0 ? (
        <p data-testid="runtime-trace-empty" className="mt-3 text-sm text-gray-500">
          {data?.availability === 'unavailable' ? 'Trace 聚合当前不可用' : '暂无 Trace 记录'}
        </p>
      ) : (
        <ul className="mt-3 flex flex-col gap-2">
          {items.map(trace => <TraceRow key={trace.trace_id} trace={trace} />)}
        </ul>
      )}
      <p className="mt-3 text-[11px] text-gray-400">不完整 Trace 仅作为诊断证据，不作为发布/验收结论；原始节点沿用既有 Trace 页面权限。</p>
      <RuntimeResourcePagination label="Trace" page={page} meta={data?.page} loading={query.isFetching} onChange={setPage} />
    </section>
  )
}
