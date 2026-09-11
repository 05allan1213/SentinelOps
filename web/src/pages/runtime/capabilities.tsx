import { Boxes, RefreshCw } from 'lucide-react'
import { useRuntimeCapabilities } from '@/hooks/useRuntimeQueries'
import { cn } from '@/utils'
import CapabilityTable from './components/CapabilityTable'

export default function RuntimeCapabilitiesPage() {
  const query = useRuntimeCapabilities({ page: 1, page_size: 100 })
  const data = query.data ?? { items: [], availability: 'unavailable' as const, data_quality: 'unknown' as const }

  return (
    <div className="flex flex-col gap-4 pb-8 min-w-0 max-w-[1440px]">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex items-center gap-3">
          <div className="p-2 rounded-lg bg-indigo-50"><Boxes className="w-5 h-5 text-indigo-600" /></div>
          <div>
            <h1 className="text-2xl font-semibold text-gray-900 tracking-tight">Capabilities</h1>
            <p className="text-sm text-gray-500 mt-0.5">只读 Local / MCP / Skill / AgentTool 目录；configured 与 observed 严格分列，不提供开关或编辑器</p>
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
        <div data-testid="runtime-capabilities-error" className="rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
          Capability 目录加载失败，可重试。
        </div>
      )}

      <CapabilityTable data={data} loading={query.isLoading || (query.isFetching && !query.data)} />
    </div>
  )
}
