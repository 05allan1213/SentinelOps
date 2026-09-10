import { useRuntimeCheckpoints } from '@/hooks/useRuntimeQueries'
import { cn } from '@/utils'
import RuntimeQualityState from './RuntimeQualityState'
import type { CheckpointDTO } from '@/types/runtime'

interface Props {
  runId: string
}

const STATE_META: Record<string, { label: string; className: string }> = {
  valid: { label: '有效', className: 'border-emerald-200 bg-emerald-50 text-emerald-700' },
  missing: { label: '缺失', className: 'border-amber-200 bg-amber-50 text-amber-800' },
  corrupt: { label: '损坏', className: 'border-red-200 bg-red-50 text-red-700' },
  expired: { label: '过期', className: 'border-amber-200 bg-amber-50 text-amber-800' },
  incompatible: { label: '不兼容', className: 'border-violet-200 bg-violet-50 text-violet-700' },
}

const dash = (value: unknown) => (value === null || value === undefined || value === '' ? '—' : String(value))

const formatTime = (value: string | null | undefined) => {
  if (!value) return '—'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN', { hour12: false })
}

function CheckpointRow({ checkpoint }: { checkpoint: CheckpointDTO }) {
  const state = STATE_META[checkpoint.state] ?? { label: dash(checkpoint.state), className: 'border-dashed border-gray-300 bg-white text-gray-500' }
  return (
    <li data-testid="runtime-checkpoint-row" data-state={checkpoint.state} className="rounded-lg border border-gray-200 bg-white p-3">
      <div className="flex flex-wrap items-center gap-3">
        <span className="font-mono text-xs text-gray-900 break-all">{checkpoint.checkpoint_id}</span>
        <span data-testid="runtime-checkpoint-state" className={cn('rounded border px-1.5 py-0.5 text-xs', state.className)}>{state.label}</span>
        <RuntimeQualityState availability={checkpoint.availability} dataQuality={checkpoint.data_quality} reasonCode={checkpoint.reason_code} />
      </div>
      <dl className="mt-2 grid grid-cols-2 gap-2 text-xs lg:grid-cols-4">
        <div><dt className="text-gray-500">Key</dt><dd className="font-mono text-gray-800 break-all">{dash(checkpoint.checkpoint_key)}</dd></div>
        <div><dt className="text-gray-500">Payload SHA256</dt><dd className="font-mono text-gray-800 break-all">{dash(checkpoint.payload_sha256)}</dd></div>
        <div><dt className="text-gray-500">Runtime 版本</dt><dd className="font-mono text-gray-800 break-all">{dash(checkpoint.runtime_version)}</dd></div>
        <div><dt className="text-gray-500">兼容性哈希</dt><dd className="font-mono text-gray-800 break-all">{dash(checkpoint.runtime_compatibility_hash)}</dd></div>
        <div><dt className="text-gray-500">Generation</dt><dd className="tabular-nums text-gray-800">{dash(checkpoint.lease_generation)}</dd></div>
        <div><dt className="text-gray-500">提交时间</dt><dd className="tabular-nums text-gray-800">{formatTime(checkpoint.committed_at)}</dd></div>
        <div><dt className="text-gray-500">过期时间</dt><dd className="tabular-nums text-gray-800">{formatTime(checkpoint.expires_at)}</dd></div>
        <div><dt className="text-gray-500">创建时间</dt><dd className="tabular-nums text-gray-800">{formatTime(checkpoint.created_at)}</dd></div>
      </dl>
      <p className="mt-2 text-[11px] text-gray-400">仅展示元数据；Checkpoint 不透明字节不会进入浏览器。</p>
    </li>
  )
}

export default function CheckpointPanel({ runId }: Props) {
  const query = useRuntimeCheckpoints(runId, { page: 1, page_size: 20 })
  const data = query.data
  const items = data?.items ?? []

  return (
    <section data-testid="runtime-checkpoints-panel" className={cn('rounded-xl border border-gray-200 bg-white p-4', query.isFetching && 'opacity-90')}>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h3 className="text-sm font-medium text-gray-900">Checkpoint 元数据</h3>
        <RuntimeQualityState availability={data?.availability} dataQuality={data?.data_quality} reasonCode={data?.reason_code} notRun={data?.not_run} />
      </div>
      {query.isLoading && !data ? (
        <p data-testid="runtime-checkpoints-loading" className="mt-3 text-sm text-gray-500">加载中…</p>
      ) : items.length === 0 ? (
        <p data-testid="runtime-checkpoints-empty" className="mt-3 text-sm text-gray-500">
          {data?.availability === 'unavailable' ? 'Checkpoint 查询当前不可用' : '暂无 Checkpoint 记录'}
        </p>
      ) : (
        <ul className="mt-3 flex flex-col gap-2">
          {items.map(checkpoint => <CheckpointRow key={checkpoint.checkpoint_id} checkpoint={checkpoint} />)}
        </ul>
      )}
    </section>
  )
}
