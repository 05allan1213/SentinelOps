import { Search, RotateCcw } from 'lucide-react'
import CustomSelect from '@/components/common/CustomSelect'
import { cn } from '@/utils'
import type { RuntimeStatus } from '@/types/runtime'

export interface RuntimeRunFilterState {
  status: string
  session_id: string
  agent: string
  from: string
  to: string
  scope: string
  include_legacy: boolean
  sort: string
  direction: string
}

const STATUS_OPTIONS: { value: string; label: string }[] = [
  { value: '', label: '全部状态' },
  { value: 'pending', label: '待执行' },
  { value: 'running', label: '运行中' },
  { value: 'waiting_approval', label: '等待审批' },
  { value: 'retryable_failed', label: '可重试失败' },
  { value: 'parked', label: '已搁置' },
  { value: 'reconciling', label: '对账中' },
  { value: 'succeeded', label: '成功' },
  { value: 'failed', label: '失败' },
  { value: 'canceled', label: '已取消' },
]

const SORT_OPTIONS: { value: string; label: string }[] = [
  { value: '', label: '默认排序' },
  { value: 'created_at', label: '创建时间' },
  { value: 'started_at', label: '开始时间' },
  { value: 'finished_at', label: '结束时间' },
  { value: 'updated_at', label: '更新时间' },
  { value: 'status', label: '状态' },
  { value: 'attempt', label: 'Attempt' },
]

const DIRECTION_OPTIONS = [
  { value: '', label: '方向' },
  { value: 'desc', label: '降序' },
  { value: 'asc', label: '升序' },
]

interface Props {
  filters: RuntimeRunFilterState
  onChange: (patch: Partial<RuntimeRunFilterState>) => void
  onReset: () => void
  disabled?: boolean
}

const inputClass = 'h-9 px-3 rounded-lg border border-gray-200 bg-white text-sm text-gray-800 placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-indigo-500/20 focus:border-indigo-400 transition-colors duration-150'

export default function RuntimeRunFilters({ filters, onChange, onReset, disabled }: Props) {
  return (
    <section
      aria-label="Run 筛选"
      className={cn('flex flex-wrap items-center gap-2 rounded-xl border border-gray-200 bg-white p-3', disabled && 'opacity-70')}
    >
      <div className="flex items-center gap-2">
        <Search className="w-4 h-4 text-gray-400" />
        <CustomSelect
          value={filters.status}
          onChange={value => onChange({ status: value as RuntimeStatus | '' })}
          options={STATUS_OPTIONS}
          placeholder="状态"
          className="w-[150px]"
        />
      </div>
      <input
        aria-label="Session ID"
        className={cn(inputClass, 'w-[190px]')}
        placeholder="Session ID"
        value={filters.session_id}
        onChange={event => onChange({ session_id: event.target.value })}
      />
      <input
        aria-label="Agent"
        className={cn(inputClass, 'w-[150px]')}
        placeholder="Agent"
        value={filters.agent}
        onChange={event => onChange({ agent: event.target.value })}
      />
      <input
        aria-label="Scope"
        className={cn(inputClass, 'w-[130px]')}
        placeholder="Scope"
        value={filters.scope}
        onChange={event => onChange({ scope: event.target.value })}
      />
      <input
        aria-label="开始时间"
        type="datetime-local"
        className={cn(inputClass, 'w-[190px]')}
        value={filters.from}
        onChange={event => onChange({ from: event.target.value })}
      />
      <input
        aria-label="结束时间"
        type="datetime-local"
        className={cn(inputClass, 'w-[190px]')}
        value={filters.to}
        onChange={event => onChange({ to: event.target.value })}
      />
      <CustomSelect
        value={filters.sort}
        onChange={value => onChange({ sort: value })}
        options={SORT_OPTIONS}
        placeholder="排序"
        className="w-[130px]"
      />
      <CustomSelect
        value={filters.direction}
        onChange={value => onChange({ direction: value })}
        options={DIRECTION_OPTIONS}
        placeholder="方向"
        className="w-[100px]"
      />
      <label className="inline-flex items-center gap-2 h-9 px-3 rounded-lg border border-gray-200 bg-white text-sm text-gray-700 cursor-pointer select-none">
        <input
          type="checkbox"
          className="w-4 h-4 accent-indigo-600"
          checked={filters.include_legacy}
          onChange={event => onChange({ include_legacy: event.target.checked })}
        />
        包含历史记录
      </label>
      <button
        type="button"
        onClick={onReset}
        className="inline-flex items-center gap-1.5 h-9 px-3 rounded-lg border border-gray-200 bg-white text-sm text-gray-600 hover:bg-gray-50 transition-colors duration-150"
      >
        <RotateCcw className="w-3.5 h-3.5" />
        重置
      </button>
    </section>
  )
}
