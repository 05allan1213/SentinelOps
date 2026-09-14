import { useSearchParams } from 'react-router-dom'
import Pagination from '@/components/common/Pagination'
import PaginationBar from '@/components/common/PaginationBar'
import StatePanel from '@/components/common/StatePanel'
import Button from '@/components/common/Button'
import { RefreshCw, ServerCog } from 'lucide-react'
import { useRuntimeWorkerHealth } from '@/hooks/useRuntimeQueries'
import { cn } from '@/utils'
import EvalReleaseSummary from './components/EvalReleaseSummary'
import WorkerHealthTable from './components/WorkerHealthTable'

export default function RuntimeWorkerHealthPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const readPositive = (key: string, fallback: number) => {
    const value = Number(searchParams.get(key) ?? fallback)
    return Number.isSafeInteger(value) && value > 0 ? value : fallback
  }
  const page = readPositive('page', 1)
  const pageSize = Math.min(100, readPositive('page_size', 50))
  const changePage = (nextPage: number, size = pageSize) => {
    const next = new URLSearchParams(searchParams)
    next.set('page', String(nextPage))
    next.set('page_size', String(size))
    setSearchParams(next)
  }
  const query = useRuntimeWorkerHealth({ page, page_size: pageSize })
  const data = query.data ?? { items: [], availability: 'unavailable' as const, data_quality: 'unknown' as const }

  return (
    <div className="flex flex-col gap-4 pb-8 min-w-0 max-w-[1440px]">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex items-center gap-3">
          <div className="p-2 rounded-lg bg-indigo-50"><ServerCog className="w-5 h-5 text-indigo-600" /></div>
          <div>
            <h1 className="text-2xl font-semibold text-gray-900 tracking-tight">Worker Health</h1>
            <p className="text-sm text-gray-500 mt-0.5">持久化 Worker 观测、心跳与 Runtime 兼容性；Eval / Release / Retention 只读事实同样在此展示</p>
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

      {query.isError && <StatePanel kind="error" title="Worker Health 加载失败"
        description={query.data ? `当前请求第 ${page} 页；下方保留上次成功加载的第 ${query.data.page?.page ?? '—'} 页数据。` : '请重试。'}
        action={<Button onClick={() => void query.refetch()}>重试</Button>} />}
      {query.isPlaceholderData && <StatePanel kind="loading" title={`正在加载第 ${page} 页`} description={`下方仍是上次成功加载的第 ${query.data?.page?.page ?? '—'} 页数据。`} />}
      {!query.isFetching && !query.isError && data.page && page > Math.max(1, Math.ceil(data.page.total / pageSize)) &&
        <StatePanel kind="empty" title="请求页超出范围" action={<Button onClick={() => changePage(1)}>返回第一页</Button>} />}

      <WorkerHealthTable data={data} loading={query.isLoading || (query.isFetching && !query.data)} />

      {data.page && <PaginationBar>
        <Pagination aria-label="Worker Health 分页" page={page} pageSize={pageSize}
          total={data.page.total} totalPages={Math.max(1, Math.ceil(data.page.total / pageSize))}
          isFetching={query.isFetching} onPageChange={changePage} onPageSizeChange={size => changePage(1, size)} />
      </PaginationBar>}

      <EvalReleaseSummary />
    </div>
  )
}
