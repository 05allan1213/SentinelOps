import { RefreshCw, ShieldCheck } from 'lucide-react'
import { useRuntimeSafety } from '@/hooks/useRuntimeQueries'
import { cn } from '@/utils'
import SafetySummary from './components/SafetySummary'

export default function RuntimeSafetyPage() {
  const query = useRuntimeSafety()

  return (
    <div className="flex flex-col gap-4 pb-8 min-w-0 max-w-[1440px]">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex items-center gap-3">
          <div className="p-2 rounded-lg bg-indigo-50"><ShieldCheck className="w-5 h-5 text-indigo-600" /></div>
          <div>
            <h1 className="text-2xl font-semibold text-gray-900 tracking-tight">Safety</h1>
            <p className="text-sm text-gray-500 mt-0.5">Static / Dynamic / Effective Gate、Shadow Mode 与策略修订；只读展示服务端事实</p>
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

      {query.isError && !query.data && (
        <div data-testid="runtime-safety-error" className="rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
          Safety 数据加载失败，可重试。
        </div>
      )}

      <SafetySummary data={query.data} loading={query.isLoading && !query.data} />
    </div>
  )
}
