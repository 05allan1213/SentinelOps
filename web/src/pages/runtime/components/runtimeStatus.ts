import type { CurrentPhase, RunSummaryDTO, RuntimeStatus } from '@/types/runtime'

export type RuntimeStatusTone = 'neutral' | 'info' | 'warning' | 'attention' | 'success' | 'danger' | 'unknown'

export interface RuntimeStatusMeta {
  label: string
  tone: RuntimeStatusTone
  className: string
}

/** Only `succeeded` is rendered as success; unknown/parked/reconciling never inherit it. */
export const RUNTIME_STATUS_META: Record<RuntimeStatus, RuntimeStatusMeta> = {
  pending: { label: '待执行', tone: 'neutral', className: 'bg-gray-100 text-gray-700 border-gray-200' },
  running: { label: '运行中', tone: 'info', className: 'bg-blue-50 text-blue-700 border-blue-200' },
  waiting_approval: { label: '等待审批', tone: 'warning', className: 'bg-amber-50 text-amber-700 border-amber-200' },
  retryable_failed: { label: '可重试失败', tone: 'warning', className: 'bg-amber-50 text-amber-800 border-amber-300' },
  parked: { label: '已搁置', tone: 'attention', className: 'bg-amber-100 text-amber-900 border-amber-300' },
  reconciling: { label: '对账中', tone: 'attention', className: 'bg-violet-50 text-violet-700 border-violet-200' },
  succeeded: { label: '成功', tone: 'success', className: 'bg-emerald-50 text-emerald-700 border-emerald-200' },
  failed: { label: '失败', tone: 'danger', className: 'bg-red-50 text-red-700 border-red-200' },
  canceled: { label: '已取消', tone: 'neutral', className: 'bg-gray-100 text-gray-500 border-gray-200' },
}

const UNKNOWN_META: RuntimeStatusMeta = {
  label: '未知',
  tone: 'unknown',
  className: 'bg-white text-gray-500 border-dashed border-gray-300',
}

export const PHASE_LABELS: Record<CurrentPhase, string> = {
  planning: '规划中',
  executing: '执行中',
  waiting_approval: '等待审批',
  recovering: '恢复中',
  reconciling: '对账中',
  completed: '已完成',
  failed: '失败',
  unknown: '未知',
}

export function runtimeStatusMeta(status: string | undefined): RuntimeStatusMeta {
  if (status && status in RUNTIME_STATUS_META) return RUNTIME_STATUS_META[status as RuntimeStatus]
  return UNKNOWN_META
}

export function runtimePhaseLabel(phase: string | undefined): string {
  if (phase && phase in PHASE_LABELS) return PHASE_LABELS[phase as CurrentPhase]
  return '未知'
}

/** Legacy/compatibility rows are read-only history and never expose Recovery controls. */
export function isLegacyRun(run: RunSummaryDTO): boolean {
  return run.runtime_mode !== '' && run.runtime_mode !== 'durable_v1'
}
