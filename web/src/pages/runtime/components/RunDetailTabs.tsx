import { cn } from '@/utils'
import { RUNTIME_DETAIL_TABS, type RuntimeDetailTab } from './detailTabs'

interface Props {
  active: RuntimeDetailTab
  onChange: (tab: RuntimeDetailTab) => void
}

export default function RunDetailTabs({ active, onChange }: Props) {
  return (
    <div role="tablist" aria-label="Run 详情" className="flex flex-wrap items-center gap-1 border-b border-gray-200">
      {RUNTIME_DETAIL_TABS.map(tab => {
        const selected = tab.id === active
        return (
          <button
            key={tab.id}
            role="tab"
            type="button"
            aria-selected={selected}
            data-testid="runtime-detail-tab"
            data-tab={tab.id}
            onClick={() => onChange(tab.id)}
            className={cn(
              'h-9 px-3 text-sm rounded-t-lg border-b-2 -mb-px transition-colors duration-150 focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500/30',
              selected
                ? 'border-indigo-500 text-indigo-700 bg-indigo-50/60 font-medium'
                : 'border-transparent text-gray-600 hover:text-gray-900 hover:bg-gray-50',
            )}
          >
            {tab.label}
          </button>
        )
      })}
    </div>
  )
}
