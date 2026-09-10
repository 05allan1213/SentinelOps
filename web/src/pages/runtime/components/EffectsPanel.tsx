import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { AlertTriangle, GitBranch } from 'lucide-react'
import { useRuntimeEffects } from '@/hooks/useRuntimeQueries'
import { opsService, type EffectResolutionInput } from '@/services/ops'
import { useAuthStore } from '@/stores/authStore'
import { cn } from '@/utils'
import RuntimeQualityState from './RuntimeQualityState'
import type { EffectDTO } from '@/types/runtime'

interface Props {
  runId: string
}

const STATUS_TONE: Record<string, string> = {
  succeeded: 'border-emerald-200 bg-emerald-50 text-emerald-700',
  failed: 'border-red-200 bg-red-50 text-red-700',
  pending: 'border-gray-300 bg-gray-100 text-gray-600',
  running: 'border-blue-200 bg-blue-50 text-blue-700',
  unknown: 'border-amber-300 bg-amber-100 text-amber-900',
  reconciling: 'border-violet-200 bg-violet-50 text-violet-700',
}

const dash = (value: unknown) => (value === null || value === undefined || value === '' ? '—' : String(value))

function EffectRow({ effect, runId }: { effect: EffectDTO; runId: string }) {
  const role = useAuthStore(state => state.role)
  const queryClient = useQueryClient()
  const [open, setOpen] = useState(false)
  const [resolution, setResolution] = useState<EffectResolutionInput['resolution']>('executed')
  const [reason, setReason] = useState('')
  const [externalReference, setExternalReference] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)

  const controllable = (effect.status === 'unknown' || effect.status === 'reconciling') && role === 'admin'
  const refresh = () => queryClient.invalidateQueries({ queryKey: ['runtime', 'effects', runId] })

  const submitResolve = async () => {
    setBusy(true); setError(null)
    try {
      await opsService.resolveEffect(effect.id, {
        resolution,
        evidence: { reason },
        ...(externalReference ? { external_reference: externalReference } : {}),
      })
      setNotice('对账结论已提交，等待服务端刷新')
      await refresh()
    } catch (submitError) {
      setError(submitError instanceof Error ? submitError.message : '提交失败')
    } finally {
      setBusy(false)
    }
  }

  const submitAcceptUnknown = async () => {
    if (!reason.trim()) { setError('接受未知需要填写理由'); return }
    setBusy(true); setError(null)
    try {
      await opsService.acceptUnknownEffect(effect.id, runId, reason.trim())
      setNotice('已接受未知并提交取消请求')
      await refresh()
    } catch (submitError) {
      setError(submitError instanceof Error ? submitError.message : '提交失败')
    } finally {
      setBusy(false)
    }
  }

  return (
    <li data-testid="runtime-effect-row" data-effect-id={effect.id} data-role={effect.effect_role} data-status={effect.status} className="rounded-lg border border-gray-200 bg-white p-3">
      <div className="flex flex-wrap items-center gap-2">
        <GitBranch className="w-3.5 h-3.5 text-gray-400" />
        <span className="font-mono text-xs text-gray-900">{effect.effect_step || dash(effect.id)}</span>
        <span className={cn('rounded border px-1.5 py-0.5 text-xs', STATUS_TONE[effect.status] ?? 'border-dashed border-gray-300 bg-white text-gray-500')}>{dash(effect.status)}</span>
        <span className="rounded border border-gray-200 bg-gray-50 px-1.5 py-0.5 text-xs text-gray-600">{effect.effect_role === 'primary' ? 'Primary' : 'Derived'}</span>
        <span className="text-xs text-gray-600">{dash(effect.effect_type)} · {dash(effect.tool_name)}@{dash(effect.tool_revision)}</span>
        {effect.parent_effect_id && <span className="text-xs text-gray-500">parent: <span className="font-mono">{effect.parent_effect_id}</span></span>}
      </div>
      <dl className="mt-2 grid grid-cols-2 gap-2 text-xs lg:grid-cols-4">
        <div><dt className="text-gray-500">Idempotency digest</dt><dd className="font-mono text-gray-800 break-all">{dash(effect.idempotency_key_digest)}</dd></div>
        <div><dt className="text-gray-500">Proposal hash</dt><dd className="font-mono text-gray-800 break-all">{dash(effect.proposal_hash)}</dd></div>
        <div><dt className="text-gray-500">Target hash</dt><dd className="font-mono text-gray-800 break-all">{dash(effect.target_hash)}</dd></div>
        <div><dt className="text-gray-500">Tool schema hash</dt><dd className="font-mono text-gray-800 break-all">{dash(effect.tool_schema_hash)}</dd></div>
        <div><dt className="text-gray-500">Generation / Attempt</dt><dd className="tabular-nums text-gray-800">{effect.lease_generation} / {effect.attempt}</dd></div>
        <div><dt className="text-gray-500">External reference</dt><dd className="font-mono text-gray-800 break-all">{dash(effect.external_reference)}</dd></div>
        <div><dt className="text-gray-500">对账次数 / 结论</dt><dd className="text-gray-800">{effect.reconciliation_attempts} · {dash(effect.resolution)}</dd></div>
        <div><dt className="text-gray-500">处理人</dt><dd className="text-gray-800">{dash(effect.resolved_by)}</dd></div>
      </dl>

      {(effect.history ?? []).length > 0 && (
        <ul data-testid="runtime-effect-history" className="mt-2 space-y-1 border-t border-gray-100 pt-2 text-xs">
          {(effect.history ?? []).map(entry => (
            <li key={`${entry.seq}-${entry.event_type}`} className="flex flex-wrap items-center gap-2 text-gray-600">
              <span className="font-mono text-gray-500">#{entry.seq}</span>
              <span>{entry.event_type}</span>
              <span>{dash(entry.status)}</span>
              <span>{dash(entry.actor_id)}</span>
              <span className="break-words">{dash(entry.reason)}</span>
              <span className="font-mono">{dash(entry.evidence_reference)}</span>
            </li>
          ))}
        </ul>
      )}

      {(effect.status === 'unknown' || effect.status === 'reconciling') && (
        <div className="mt-2 border-t border-gray-100 pt-2">
          {controllable ? (
            <>
              <button type="button" onClick={() => setOpen(value => !value)} className="inline-flex items-center gap-1.5 text-xs text-amber-800 hover:text-amber-900">
                <AlertTriangle className="w-3.5 h-3.5" />
                {open ? '收起受控对账' : '受控对账（仅 admin，不执行外部 Effect）'}
              </button>
              {open && (
                <div data-testid="runtime-effect-reconcile" className="mt-2 grid gap-2 lg:grid-cols-[160px_1fr_200px_auto]">
                  <select
                    aria-label="对账结论"
                    value={resolution}
                    onChange={event => setResolution(event.target.value as EffectResolutionInput['resolution'])}
                    className="h-9 rounded-lg border border-gray-200 px-2 text-sm"
                  >
                    <option value="executed">已执行</option>
                    <option value="not_executed">未执行</option>
                    <option value="still_unknown">仍未知</option>
                  </select>
                  <input aria-label="对账理由" value={reason} onChange={event => setReason(event.target.value)} placeholder="理由 / 证据说明（写入审计）" className="h-9 rounded-lg border border-gray-200 px-3 text-sm" />
                  <input aria-label="外部引用" value={externalReference} onChange={event => setExternalReference(event.target.value)} placeholder="external_reference（可选）" className="h-9 rounded-lg border border-gray-200 px-3 font-mono text-xs" />
                  <div className="flex gap-2">
                    <button type="button" disabled={busy} onClick={() => void submitResolve()} className="h-9 rounded-lg bg-indigo-600 px-3 text-sm text-white disabled:bg-gray-300">提交对账</button>
                    <button type="button" disabled={busy} onClick={() => void submitAcceptUnknown()} className="h-9 rounded-lg border border-red-300 px-3 text-sm text-red-700 disabled:border-gray-200 disabled:text-gray-400">接受未知</button>
                  </div>
                </div>
              )}
            </>
          ) : (
            <p data-testid="runtime-effect-reconcile-readonly" className="text-xs text-gray-500">该 Effect 处于 {effect.status}；受控对账仅 admin 可执行。</p>
          )}
          {error && <p data-testid="runtime-effect-error" className="mt-1 text-xs text-red-700">{error}</p>}
          {notice && <p data-testid="runtime-effect-notice" className="mt-1 text-xs text-emerald-700">{notice}</p>}
        </div>
      )}
    </li>
  )
}

export default function EffectsPanel({ runId }: Props) {
  const query = useRuntimeEffects(runId, { page: 1, page_size: 50 })
  const data = query.data
  const items = data?.items ?? []
  const primary = items.filter(effect => effect.effect_role === 'primary')
  const derived = items.filter(effect => effect.effect_role !== 'primary')

  return (
    <section data-testid="runtime-effects-panel" className={cn('rounded-xl border border-gray-200 bg-white p-4', query.isFetching && 'opacity-90')}>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h3 className="text-sm font-medium text-gray-900">Effect Ledger（Primary / Derived）</h3>
        <RuntimeQualityState availability={data?.availability} dataQuality={data?.data_quality} reasonCode={data?.reason_code} notRun={data?.not_run} />
      </div>
      {query.isLoading && !data ? (
        <p data-testid="runtime-effects-loading" className="mt-3 text-sm text-gray-500">加载中…</p>
      ) : items.length === 0 ? (
        <p data-testid="runtime-effects-empty" className="mt-3 text-sm text-gray-500">
          {data?.availability === 'unavailable' ? 'Effect Ledger 当前不可用' : '暂无 Effect 记录'}
        </p>
      ) : (
        <div className="mt-3 flex flex-col gap-3">
          <div>
            <p className="text-xs font-medium text-gray-500">Primary</p>
            {primary.length === 0 ? <p className="text-xs text-gray-400">—</p> : (
              <ul className="mt-1 flex flex-col gap-2">{primary.map(effect => <EffectRow key={effect.id} effect={effect} runId={runId} />)}</ul>
            )}
          </div>
          <div>
            <p className="text-xs font-medium text-gray-500">Derived</p>
            {derived.length === 0 ? <p className="text-xs text-gray-400">—</p> : (
              <ul className="mt-1 flex flex-col gap-2">{derived.map(effect => <EffectRow key={effect.id} effect={effect} runId={runId} />)}</ul>
            )}
          </div>
        </div>
      )}
    </section>
  )
}
