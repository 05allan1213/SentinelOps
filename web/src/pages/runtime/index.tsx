import { useCallback, useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'
import { Activity, AlertTriangle, Inbox, RefreshCw, ServerCrash } from 'lucide-react'
import Pagination from '@/components/common/Pagination'
import StatCard from '@/components/common/StatCard'
import { useRuntimeRuns } from '@/hooks/useRuntimeQueries'
import { cn } from '@/utils'
import RuntimeRunFilters, { type RuntimeRunFilterState } from './components/RuntimeRunFilters'
import RuntimeRunsTable from './components/RuntimeRunsTable'
import type { ListRunsParams, RuntimeStatus, SortDirection } from '@/types/runtime'

const DEFAULT_PAGE_SIZE = 20

function parseFilters(searchParams: URLSearchParams): RuntimeRunFilterState {
  const read = (key: string) => searchParams.get(key) ?? ''
  return {
    status: read('status'),
    session_id: read('session_id'),
    agent: read('agent'),
    from: read('from'),
    to: read('to'),
    scope: read('scope'),
    include_legacy: read('include_legacy') === 'true',
    sort: read('sort'),
    direction: read('direction'),
  }
}

function toQueryParams(searchParams: URLSearchParams): ListRunsParams {
  const page = Number(searchParams.get('page') ?? '1')
  const pageSize = Number(searchParams.get('page_size') ?? String(DEFAULT_PAGE_SIZE))
  const filters = parseFilters(searchParams)
  return {
    status: filters.status as RuntimeStatus | '',
    session_id: filters.session_id,
    agent: filters.agent,
    from: filters.from,
    to: filters.to,
    scope: filters.scope,
    ...(filters.include_legacy ? { include_legacy: true } : {}),
    sort: filters.sort,
    direction: filters.direction as SortDirection | '',
    page: Number.isFinite(page) && page >= 1 ? page : 1,
    page_size: Number.isFinite(pageSize) && pageSize >= 1 ? pageSize : DEFAULT_PAGE_SIZE,
  }
}

export default function RuntimeRunsPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const params = useMemo(() => toQueryParams(searchParams), [searchParams])
  const query = useRuntimeRuns(params)
  const filters = useMemo(() => parseFilters(searchParams), [searchParams])

  // keepPreviousData in the shared hook keeps the last successful page visible on failure; rows are never fabricated.
  const data = query.data
  const page = params.page ?? 1
  const pageSize = params.page_size ?? DEFAULT_PAGE_SIZE
  const total = data?.page?.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  const update = useCallback((mutate: (next: URLSearchParams) => void) => {
    const next = new URLSearchParams(searchParams)
    mutate(next)
    setSearchParams(next, { replace: true })
  }, [searchParams, setSearchParams])

  const onFilterChange = useCallback((patch: Partial<RuntimeRunFilterState>) => {
    update(next => {
      Object.entries(patch).forEach(([key, value]) => {
        if (value === '' || value === false) next.delete(key)
        else next.set(key, String(value))
      })
      next.delete('page')
    })
  }, [update])

  const onReset = useCallback(() => setSearchParams(new URLSearchParams(), { replace: true }), [setSearchParams])

  const reason = data?.reason_code
  const unavailable = data?.availability === 'unavailable'
  const partial = data?.availability === 'partial'
  const failed = query.isError
  const showUnavailablePanel = unavailable && !failed

  return (
    <div className="flex flex-col gap-5 pb-8 min-w-0 max-w-[1440px]">
      <header className="flex items-start justify-between gap-4">
        <div className="flex items-center gap-3">
          <div className="p-2 rounded-lg bg-indigo-50">
            <Activity className="w-5 h-5 text-indigo-600" />
          </div>
          <div>
            <h1 className="text-2xl font-semibold text-gray-900 tracking-tight">Agent Runtime</h1>
            <p className="text-sm text-gray-500 mt-0.5">Durable Run 列表、状态与恢复入口；默认只显示 durable_v1，历史记录需显式筛选</p>
          </div>
        </div>
        <button
          type="button"
          onClick={() => void query.refetch()}
          className="inline-flex items-center gap-1.5 h-9 px-3 rounded-lg border border-gray-200 bg-white text-sm text-gray-600 hover:bg-gray-50 transition-colors duration-150"
        >
          <RefreshCw className={cn('w-3.5 h-3.5', query.isFetching && 'animate-spin')} />
          刷新
        </button>
      </header>

      <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
        <StatCard label="匹配 Run 总数" value={data?.page?.total ?? 0} Icon={Activity} tone="indigo" />
        <StatCard label="当前页" value={`${page} / ${totalPages}`} Icon={Inbox} tone="gray" sub={`每页 ${pageSize} 条`} />
        <StatCard
          label="数据可用性"
          value={data?.availability ?? '—'}
          Icon={partial ? AlertTriangle : ServerCrash}
          tone={partial ? 'amber' : data?.availability === 'available' ? 'blue' : 'gray'}
          sub={reason ? `reason: ${reason}` : undefined}
        />
        <StatCard
          label="本页已搁置"
          value={(data?.items ?? []).filter(run => run.status === 'parked').length}
          Icon={ServerCrash}
          tone="gray"
          sub="Recovery 仅 admin 可提交"
        />
      </div>

      <RuntimeRunFilters filters={filters} onChange={onFilterChange} onReset={onReset} disabled={query.isFetching} />

      {failed && (
        <div data-testid="runtime-runs-error" className="flex items-center justify-between gap-3 rounded-xl border border-red-200 bg-red-50 px-4 py-3">
          <p className="text-sm text-red-700">Run 列表加载失败{data ? '，下方保留上次成功加载的数据' : ''}。</p>
          <button
            type="button"
            onClick={() => void query.refetch()}
            className="inline-flex items-center gap-1.5 h-8 px-3 rounded-lg border border-red-300 bg-white text-sm text-red-700 hover:bg-red-50 transition-colors duration-150"
          >
            <RefreshCw className="w-3.5 h-3.5" />
            重试
          </button>
        </div>
      )}

      {partial && (
        <div data-testid="runtime-runs-partial" className="rounded-xl border border-amber-200 bg-amber-50 px-4 py-2.5 text-sm text-amber-800">
          数据部分可用{reason ? `（reason: ${reason}）` : ''}，缺失字段显示为 “—”，不会用默认成功值填充。
        </div>
      )}

      {showUnavailablePanel ? (
        <div data-testid="runtime-runs-unavailable" className="rounded-xl border border-gray-200 bg-gray-50 px-4 py-10 text-center">
          <p className="text-sm font-medium text-gray-700">Runtime 数据当前不可用</p>
          <p className="mt-1 text-xs text-gray-500">{reason ? `reason_code: ${reason}` : '服务端未提供可用数据'}</p>
        </div>
      ) : (
        <RuntimeRunsTable data={data ?? { items: [], availability: 'unavailable', data_quality: 'unknown' }} loading={query.isLoading || (query.isFetching && !data)} />
      )}

      <div className="flex items-center justify-between gap-4">
        <p className="text-xs text-gray-500">
          {filters.include_legacy ? '包含历史记录（只读）' : '默认仅 durable_v1 Run'}；每页 {pageSize} 条
        </p>
        <Pagination
          page={page}
          totalPages={totalPages}
          total={total}
          pageSize={pageSize}
          onChange={nextPage => update(next => {
            if (nextPage <= 1) next.delete('page')
            else next.set('page', String(nextPage))
          })}
          onPageSizeChange={size => update(next => {
            next.set('page_size', String(size))
            next.delete('page')
          })}
        />
      </div>

      <p className="sr-only" data-testid="runtime-runs-filters">{JSON.stringify(filters)}</p>
    </div>
  )
}
