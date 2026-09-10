import { Component, useCallback, useMemo, useState, type ReactNode } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { AlertTriangle, ArrowLeft, RefreshCw, RotateCcw, ShieldAlert, Wifi, WifiOff } from 'lucide-react'
import { useRuntimeRun, useRuntimeTimeline } from '@/hooks/useRuntimeQueries'
import { useRuntimeEventTail } from '@/hooks/useRuntimeEventTail'
import { useAuthStore } from '@/stores/authStore'
import { cn } from '@/utils'
import AttemptsPanel from './components/AttemptsPanel'
import CheckpointPanel from './components/CheckpointPanel'
import ContextPanel from './components/ContextPanel'
import EffectsPanel from './components/EffectsPanel'
import EvidenceInspector from './components/EvidenceInspector'
import OperationProgress from './components/OperationProgress'
import RecoveryDialog from './components/RecoveryDialog'
import RunDetailTabs from './components/RunDetailTabs'
import RunOverview from './components/RunOverview'
import RunTimeline from './components/RunTimeline'
import RuntimeQualityState from './components/RuntimeQualityState'
import RuntimeStatusBadge from './components/RuntimeStatusBadge'
import TracePanel from './components/TracePanel'
import { isRuntimeDetailTab, type RuntimeDetailTab } from './components/detailTabs'
import type { RecoveryAction } from '@/types/runtime'

const TERMINAL = new Set(['succeeded', 'failed', 'canceled', 'parked'])
const RECOVERY_LABEL: Record<RecoveryAction, string> = { resume: 'Resume', replay: 'Replay', cancel: 'Cancel', restore: 'Restore' }
const operationStorageKey = (runId: string) => `runtime_operation_${runId}`

class PanelBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false }
  static getDerivedStateFromError() { return { failed: true } }
  render() {
    if (this.state.failed) {
      return (
        <div data-testid="runtime-panel-error" className="rounded-xl border border-red-200 bg-red-50 px-4 py-6 text-sm text-red-700">
          该面板渲染失败，Run 身份与其余面板保持可用。
        </div>
      )
    }
    return this.props.children
  }
}

const PLACEHOLDER_LABEL: Record<Exclude<RuntimeDetailTab, 'overview' | 'timeline'>, string> = {
  attempts: 'Attempts',
  effects: 'Effects',
  evidence: 'Evidence',
  context: 'Context',
  trace: 'Trace',
}

function RuntimeRunDetailPage({ runId }: { runId: string }) {
  const [searchParams, setSearchParams] = useSearchParams()
  const navigate = useNavigate()
  const role = useAuthStore(state => state.role)
  const [recoveryAction, setRecoveryAction] = useState<RecoveryAction | null>(null)
  const [operationOverride, setOperationOverride] = useState('')
  const storedOperationId = typeof sessionStorage === 'undefined' ? '' : sessionStorage.getItem(operationStorageKey(runId)) ?? ''
  const operationId = operationOverride || storedOperationId
  const tabParam = searchParams.get('tab')
  const tab: RuntimeDetailTab = isRuntimeDetailTab(tabParam) ? tabParam : 'overview'

  const runQuery = useRuntimeRun(runId)
  const timelineQuery = useRuntimeTimeline(runId, { page: 1, page_size: 50 })
  const run = runQuery.data?.item
  const status = run?.summary?.status ?? ''
  const tail = useRuntimeEventTail({ runId, enabled: Boolean(runId) && status !== '' && !TERMINAL.has(status) })

  const acceptedOperation = useCallback((id: string) => {
    setOperationOverride(id)
    setRecoveryAction(null)
    if (typeof sessionStorage !== 'undefined') sessionStorage.setItem(operationStorageKey(runId), id)
  }, [runId])

  const onTabChange = useCallback((next: RuntimeDetailTab) => {
    const params = new URLSearchParams(searchParams)
    if (next === 'overview') params.delete('tab')
    else params.set('tab', next)
    setSearchParams(params, { replace: true })
  }, [searchParams, setSearchParams])

  const headerQuality = useMemo(() => (
    <RuntimeQualityState
      availability={runQuery.data?.availability}
      dataQuality={runQuery.data?.data_quality}
      reasonCode={runQuery.data?.reason_code}
      notRun={runQuery.data?.not_run}
    />
  ), [runQuery.data])

  return (
    <div className="flex flex-col gap-4 pb-8 min-w-0 max-w-[1440px]">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <button
            type="button"
            onClick={() => navigate('/runtime/runs')}
            className="inline-flex items-center gap-1.5 text-xs text-gray-500 hover:text-gray-800 transition-colors duration-150"
          >
            <ArrowLeft className="w-3.5 h-3.5" />
            返回 Runs
          </button>
          <h1 className="mt-1 text-xl font-semibold text-gray-900 tracking-tight break-all">Run {runId || '—'}</h1>
          <div className="mt-2 flex flex-wrap items-center gap-3">
            <RuntimeStatusBadge status={run?.summary?.status} phase={run?.summary?.current_phase} />
            {headerQuality}
            <span
              data-testid="runtime-tail-state"
              data-connected={String(tail.connected)}
              className={cn('inline-flex items-center gap-1.5 text-xs', tail.connected ? 'text-blue-600' : 'text-gray-500')}
            >
              {tail.connected ? <Wifi className="w-3.5 h-3.5" /> : <WifiOff className="w-3.5 h-3.5" />}
              {tail.connected ? `事件流已连接（after_seq ${tail.afterSeq}）` : '事件流未连接'}
            </span>
          </div>
        </div>
        <button
          type="button"
          onClick={() => { void runQuery.refetch(); void timelineQuery.refetch() }}
          className="inline-flex items-center gap-1.5 h-9 px-3 rounded-lg border border-gray-200 bg-white text-sm text-gray-600 hover:bg-gray-50 transition-colors duration-150"
        >
          <RefreshCw className={cn('w-3.5 h-3.5', (runQuery.isFetching || timelineQuery.isFetching) && 'animate-spin')} />
          刷新
        </button>
      </header>

      {runQuery.isError && (
        <div data-testid="runtime-detail-error" className="flex items-center justify-between gap-3 rounded-xl border border-red-200 bg-red-50 px-4 py-3">
          <p className="flex items-center gap-2 text-sm text-red-700"><AlertTriangle className="w-4 h-4" />Run 详情加载失败。</p>
          <button
            type="button"
            onClick={() => void runQuery.refetch()}
            className="inline-flex items-center gap-1.5 h-8 px-3 rounded-lg border border-red-300 bg-white text-sm text-red-700 hover:bg-red-50 transition-colors duration-150"
          >
            重试
          </button>
        </div>
      )}

      <PanelBoundary>
        <RunDetailTabs active={tab} onChange={onTabChange} />
      </PanelBoundary>

      {tab === 'overview' && (
        <PanelBoundary>
          {runQuery.isLoading && !run ? (
            <div data-testid="runtime-detail-loading" className="rounded-xl border border-gray-200 bg-white px-4 py-10 text-center text-sm text-gray-500">加载中…</div>
          ) : runQuery.data?.availability === 'unavailable' && !run ? (
            <div data-testid="runtime-detail-unavailable" className="rounded-xl border border-gray-200 bg-gray-50 px-4 py-10 text-center text-sm text-gray-600">
              Run 数据当前不可用{runQuery.data.reason_code ? `（reason_code: ${runQuery.data.reason_code}）` : ''}
            </div>
          ) : run ? (
            <RunOverview run={run} />
          ) : (
            <div data-testid="runtime-detail-empty" className="rounded-xl border border-gray-200 bg-white px-4 py-10 text-center text-sm text-gray-500">未找到该 Run</div>
          )}
        </PanelBoundary>
      )}

      {tab === 'timeline' && (
        <PanelBoundary>
          {timelineQuery.isLoading && !timelineQuery.data ? (
            <div data-testid="runtime-timeline-loading-panel" className="rounded-xl border border-gray-200 bg-white px-4 py-10 text-center text-sm text-gray-500">加载中…</div>
          ) : (
            <RunTimeline data={timelineQuery.data ?? { items: [], availability: 'available', data_quality: 'complete' }} loading={timelineQuery.isFetching} />
          )}
        </PanelBoundary>
      )}

      {tab !== 'overview' && tab !== 'timeline' && (
        <PanelBoundary>
          {tab === 'attempts' && run ? (
            <div className="flex flex-col gap-4">
              <AttemptsPanel runId={runId} />
              <CheckpointPanel runId={runId} />
              <section data-testid="runtime-recovery-actions" className="rounded-xl border border-gray-200 bg-white p-4">
                <h3 className="flex items-center gap-2 text-sm font-medium text-gray-900">
                  <ShieldAlert className="w-4 h-4 text-amber-500" />
                  Recovery（仅 admin，服务端合法性矩阵）
                </h3>
                <p className="mt-1 text-xs text-gray-500">按钮只来自服务端 <code>allowed_recovery_actions</code>；前端不能新增动作。</p>
                <div className="mt-3 flex flex-wrap items-center gap-2">
                  {(run.allowed_recovery_actions ?? []).length === 0 && (
                    <span data-testid="runtime-recovery-none" className="text-xs text-gray-500">该 Run 当前没有可用的恢复动作</span>
                  )}
                  {(run.allowed_recovery_actions ?? []).map(action => {
                    const legacy = run.summary.runtime_mode !== '' && run.summary.runtime_mode !== 'durable_v1'
                    const terminal = TERMINAL.has(run.summary.status)
                    const restoreBlocked = action === 'restore' && !run.compatibility?.exact_restore_allowed
                    const disabledReason = role !== 'admin' ? '仅 admin 可执行'
                      : legacy ? '历史记录不可恢复'
                        : terminal && action !== 'restore' ? 'Run 已处于终态'
                          : restoreBlocked ? '兼容性不允许 Restore'
                            : ''
                    return (
                      <span key={action} className="inline-flex items-center gap-2">
                        <button
                          type="button"
                          data-testid="runtime-recovery-button"
                          data-action={action}
                          disabled={Boolean(disabledReason)}
                          onClick={() => setRecoveryAction(action)}
                          className={cn(
                            'inline-flex items-center gap-1.5 h-9 px-3 rounded-lg border text-sm transition-colors duration-150',
                            disabledReason
                              ? 'border-gray-200 bg-gray-100 text-gray-400 cursor-not-allowed'
                              : action === 'cancel' || action === 'restore'
                                ? 'border-red-300 bg-white text-red-700 hover:bg-red-50'
                                : 'border-indigo-300 bg-white text-indigo-700 hover:bg-indigo-50',
                          )}
                        >
                          <RotateCcw className="w-3.5 h-3.5" />
                          {RECOVERY_LABEL[action]}
                        </button>
                        {disabledReason && <span data-testid="runtime-recovery-disabled-reason" className="text-xs text-gray-500">{disabledReason}</span>}
                      </span>
                    )
                  })}
                </div>
                <p className="mt-2 text-[11px] text-gray-400">提交后返回 202 + operation_id；运行结果只以服务端 Operation/Run 事实为准，前端不做乐观成功。</p>
              </section>
              {operationId && <OperationProgress operationId={operationId} onTerminal={() => { void runQuery.refetch() }} />}
              {recoveryAction && (
                <RecoveryDialog
                  key={recoveryAction}
                  run={run}
                  action={recoveryAction}
                  open
                  onClose={() => setRecoveryAction(null)}
                  onAccepted={acceptedOperation}
                />
              )}
            </div>
          ) : tab === 'effects' ? (
            <EffectsPanel runId={runId} />
          ) : tab === 'evidence' ? (
            <EvidenceInspector runId={runId} />
          ) : tab === 'context' ? (
            <ContextPanel runId={runId} />
          ) : tab === 'trace' ? (
            <TracePanel runId={runId} />
          ) : (
            <div data-testid="runtime-tab-placeholder" data-tab={tab} className="rounded-xl border border-dashed border-gray-300 bg-white px-4 py-10 text-center text-sm text-gray-500">
              {PLACEHOLDER_LABEL[tab]} 面板尚未接入
            </div>
          )}
        </PanelBoundary>
      )}
    </div>
  )
}

/**
 * Route wrapper: keying by Run ID resets every Run-scoped state (tail cursor/terminal flag,
 * accepted operation, dialog) when the URL moves between two Run detail pages.
 */
export default function RuntimeRunDetailRoute() {
  const { runId = '' } = useParams()
  return <RuntimeRunDetailPage key={runId} runId={runId} />
}
