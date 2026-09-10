import { cn } from '@/utils'
import RuntimeQualityState from './RuntimeQualityState'
import type { CapabilitiesRes, CapabilityDTO } from '@/types/runtime'

interface Props {
  data: CapabilitiesRes
  loading: boolean
}

const dash = (value: unknown) => (value === null || value === undefined || value === '' ? '—' : String(value))

const formatTime = (value: string | null | undefined) => {
  if (!value) return '—'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN', { hour12: false })
}

const STATE_TONE: Record<string, string> = {
  enabled: 'border-emerald-200 bg-emerald-50 text-emerald-700',
  allowed: 'border-emerald-200 bg-emerald-50 text-emerald-700',
  loaded: 'border-emerald-200 bg-emerald-50 text-emerald-700',
  disabled: 'border-gray-300 bg-gray-100 text-gray-600',
  denied: 'border-gray-300 bg-gray-100 text-gray-600',
  not_observed: 'border-dashed border-gray-300 bg-white text-gray-500',
  unavailable: 'border-dashed border-gray-300 bg-white text-gray-500',
}

function StateCell({ value, testId }: { value: string; testId: string }) {
  const tone = STATE_TONE[value] ?? 'border-gray-200 bg-white text-gray-700'
  return <span data-testid={testId} data-state={value || 'unknown'} className={cn('inline-flex items-center rounded border px-1.5 py-0.5 text-xs', tone)}>{value || '—'}</span>
}

export default function CapabilityTable({ data, loading }: Props) {
  const items: CapabilityDTO[] = data.items ?? []

  return (
    <div className="min-w-0">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-xs text-gray-500">configured_state 来自目录/配置；observed_worker_state 只来自持久化 Worker 观测，未观测显示 unavailable。</p>
        <RuntimeQualityState availability={data.availability} dataQuality={data.data_quality} reasonCode={data.reason_code} notRun={data.not_run} />
      </div>
      {items.length === 0 && !loading ? (
        <p data-testid="runtime-capabilities-empty" className="mt-3 rounded-xl border border-gray-200 bg-white px-4 py-8 text-center text-sm text-gray-500">
          {data.availability === 'unavailable' ? 'Capability 数据当前不可用' : '暂无 Capability 记录'}
        </p>
      ) : (
        <div data-testid="runtime-capabilities-table" className="mt-3 min-w-0 w-full overflow-x-auto rounded-xl border border-gray-200 bg-white">
          <table className="w-full min-w-[1180px] border-collapse text-left">
            <caption className="sr-only">Runtime Capability 目录</caption>
            <thead>
              <tr className="border-b border-gray-200 bg-gray-50/70 text-xs text-gray-500">
                <th scope="col" className="px-3 py-2">Name</th>
                <th scope="col" className="px-3 py-2">Source</th>
                <th scope="col" className="px-3 py-2">Configured</th>
                <th scope="col" className="px-3 py-2">Observed（Worker）</th>
                <th scope="col" className="px-3 py-2">Risk</th>
                <th scope="col" className="px-3 py-2">Revision</th>
                <th scope="col" className="px-3 py-2">Schema hash</th>
                <th scope="col" className="px-3 py-2">Required gate / role</th>
                <th scope="col" className="px-3 py-2">Allowed agents</th>
                <th scope="col" className="px-3 py-2">最后观测</th>
              </tr>
            </thead>
            <tbody>
              {loading && items.length === 0 && (
                <tr><td colSpan={10} className="px-3 py-8 text-center text-sm text-gray-500">加载中…</td></tr>
              )}
              {items.map(item => (
                <tr key={`${item.source}-${item.name}`} data-testid="runtime-capability-row" className="border-b border-gray-100 last:border-b-0 text-xs text-gray-700">
                  <td className="px-3 py-2 font-medium text-gray-900">
                    <span className="block truncate max-w-[180px]" title={item.name}>{item.name}</span>
                    <span className="text-[11px] text-gray-500">{item.effect_type || '—'}</span>
                  </td>
                  <td className="px-3 py-2">{dash(item.source)}</td>
                  <td className="px-3 py-2"><StateCell value={item.configured_state} testId="runtime-capability-configured" /></td>
                  <td className="px-3 py-2"><StateCell value={item.observed_worker_state} testId="runtime-capability-observed" /></td>
                  <td className="px-3 py-2">{dash(item.risk)}</td>
                  <td className="px-3 py-2 font-mono">{dash(item.revision)}</td>
                  <td className="px-3 py-2 font-mono break-all max-w-[160px]">{dash(item.schema_hash)}</td>
                  <td className="px-3 py-2">{dash(item.required_gate)} / {dash(item.required_role)}</td>
                  <td className="px-3 py-2">{item.allowed_agents?.length ? item.allowed_agents.join(', ') : '—'}</td>
                  <td className="px-3 py-2 tabular-nums">{formatTime(item.last_observed_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
