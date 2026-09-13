import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AlertTriangle, CheckCircle, ChevronRight, Clock3, Shield, TriangleAlert } from 'lucide-react'
import toast from 'react-hot-toast'
import ConfirmDialog from '@/components/common/ConfirmDialog'
import { getApiErrorStatus } from '@/services/api'
import { opsQueryKeys, opsService, type ApprovalItem, type UnknownEffectItem } from '@/services/ops'
import { useAuthStore } from '@/stores/authStore'

interface Props { pendingReports?: number }
type DecisionKind = 'approve' | 'reject'
type Resolution = 'executed' | 'not_executed' | 'still_unknown'

const resolutionLabels: Record<Resolution, string> = {
  executed: '确认已执行',
  not_executed: '确认未执行',
  still_unknown: '仍然未知',
}

function proposalValue(proposal: unknown, key: string): string {
  if (!proposal || typeof proposal !== 'object') return ''
  const value = (proposal as Record<string, unknown>)[key]
  return typeof value === 'string' || typeof value === 'number' ? String(value) : ''
}

function prettyProposal(proposal: unknown): string {
  try { return JSON.stringify(proposal, null, 2) } catch { return '已脱敏参数不可展示' }
}

function conflictLabel(error: unknown): string {
  if (getApiErrorStatus(error) !== 409) return '操作失败，请稍后重试'
  const message = error instanceof Error ? error.message.toLowerCase() : ''
  if (message.includes('expired') || message.includes('过期')) return '审批已过期，未执行决定'
  if (message.includes('hash') || message.includes('invalidated') || message.includes('失效')) return '提案已失效，未执行决定'
  return '审批已被其他人处理，未执行重复决定'
}

function ApprovalCard({ approval, canDecide, onDecide, conflict }: { approval: ApprovalItem; canDecide: boolean; onDecide: (approval: ApprovalItem, decision: DecisionKind) => void; conflict?: string }) {
  const target = proposalValue(approval.proposal, 'target') || proposalValue(approval.proposal, 'resource') || '未标注目标'
  return (
    <article className="border-b border-slate-100 p-4 last:border-b-0" data-testid={`approval-${approval.id}`}>
      <div className="flex items-start gap-3">
        <div className="mt-0.5 rounded-lg bg-amber-50 p-2 text-amber-600"><AlertTriangle className="h-4 w-4" /></div>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="text-sm font-semibold text-slate-800">{approval.tool_name}</h3>
            <span className="rounded bg-red-50 px-1.5 py-0.5 text-[11px] font-medium text-red-600">风险 {approval.risk_level}</span>
            <span className="text-xs text-slate-400">v{approval.tool_revision}</span>
          </div>
          <dl className="mt-2 grid grid-cols-1 gap-1 text-xs text-slate-500 sm:grid-cols-2">
            <div><dt className="inline text-slate-400">目标：</dt><dd className="inline font-medium text-slate-700">{target}</dd></div>
            <div><dt className="inline text-slate-400">到期：</dt><dd className="inline">{approval.expires_at || '未设置'}</dd></div>
          </dl>
          <details className="mt-2 rounded-lg bg-slate-50 px-3 py-2">
            <summary className="cursor-pointer text-xs font-medium text-slate-600">查看脱敏参数与 Proposal Hash</summary>
            <pre className="mt-2 max-h-36 overflow-auto whitespace-pre-wrap break-all text-[11px] text-slate-500">{prettyProposal(approval.proposal)}</pre>
          </details>
          <p className="mt-2 break-all font-mono text-[10px] text-slate-400">{approval.proposal_hash}</p>
          {conflict && <p className="mt-2 rounded-md bg-amber-50 px-2 py-1.5 text-xs font-medium text-amber-700">{conflict}</p>}
          {canDecide && !conflict && (
            <div className="mt-3 flex gap-2">
              <button type="button" onClick={() => onDecide(approval, 'approve')} className="btn-success px-3 py-1.5 text-xs">批准</button>
              <button type="button" onClick={() => onDecide(approval, 'reject')} className="btn-default px-3 py-1.5 text-xs text-red-600">拒绝</button>
            </div>
          )}
          {!canDecide && !conflict && <p className="mt-3 text-xs text-slate-400">当前角色仅可查看，决定入口由后端 RBAC 保护。</p>}
        </div>
      </div>
    </article>
  )
}

function UnknownEffectCard({ effect, canResolve, onResolve }: { effect: UnknownEffectItem; canResolve: boolean; onResolve: (effect: UnknownEffectItem, resolution: Resolution) => void }) {
  return (
    <article className="border-b border-slate-100 p-4 last:border-b-0" data-testid={`effect-${effect.id}`}>
      <div className="flex items-start gap-3">
        <div className="mt-0.5 rounded-lg bg-orange-50 p-2 text-orange-600"><TriangleAlert className="h-4 w-4" /></div>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="text-sm font-semibold text-slate-800">{effect.tool_name}</h3>
            <span className="rounded bg-orange-50 px-1.5 py-0.5 text-[11px] font-medium text-orange-700">结果未知</span>
          </div>
          <p className="mt-1 text-xs text-slate-500">Effect {effect.effect_step} · Run {effect.run_id} · 第 {effect.reconciliation_attempts} 次对账</p>
          {canResolve ? (
            <div className="mt-3 flex flex-wrap gap-2">
              {(Object.keys(resolutionLabels) as Resolution[]).map(resolution => (
                <button key={resolution} type="button" onClick={() => onResolve(effect, resolution)} className="btn-default px-2.5 py-1.5 text-xs">{resolutionLabels[resolution]}</button>
              ))}
            </div>
          ) : <p className="mt-3 text-xs text-slate-400">仅 admin 可提交对账决定；此处不会直接执行 Effect。</p>}
        </div>
      </div>
    </article>
  )
}

export default function ActionQueue({ pendingReports = 0 }: Props) {
  const role = useAuthStore(s => s.role)
  const canDecide = role === 'approver' || role === 'admin'
  const canResolve = role === 'admin'
  const queryClient = useQueryClient()
  const approvals = useQuery({ queryKey: opsQueryKeys.approvals(), queryFn: () => opsService.listApprovals(20), refetchInterval: 3000 })
  // unknown Effect 队列是 admin-only 接口：非 admin 请求会被拒绝(403)，
  // 若照常发起查询会让整块待办区进入错误态，审批人将看不到可审批的 Approval。
  const unknownEffects = useQuery({
    queryKey: opsQueryKeys.unknownEffects(),
    queryFn: () => opsService.listUnknownEffects(20),
    enabled: canResolve,
    refetchInterval: canResolve ? 3000 : false,
  })
  const [decision, setDecision] = useState<{ approval: ApprovalItem; kind: DecisionKind } | null>(null)
  const [resolution, setResolution] = useState<{ effect: UnknownEffectItem; kind: Resolution } | null>(null)
  const [reason, setReason] = useState('')
  const [conflicts, setConflicts] = useState<Record<string, string>>({})

  const decisionMutation = useMutation({
    mutationFn: async ({ approval, kind, reason }: { approval: ApprovalItem; kind: DecisionKind; reason: string }) => {
      const input = { proposal_hash: approval.proposal_hash, version: approval.version, reason }
      return kind === 'approve' ? opsService.approveApproval(approval.id, input) : opsService.rejectApproval(approval.id, input)
    },
    onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['ops', 'approvals'] }); toast.success('审批决定已提交') },
    onError: (error, variables) => {
      setConflicts(current => ({ ...current, [variables.approval.id]: conflictLabel(error) }))
      if (getApiErrorStatus(error) !== 409) toast.error(conflictLabel(error))
    },
  })

  const resolutionMutation = useMutation({
    mutationFn: ({ effect, kind, reason }: { effect: UnknownEffectItem; kind: Resolution; reason: string }) => opsService.resolveEffect(effect.id, { resolution: kind, evidence: { reason } }),
    onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['ops', 'effects', 'unknown'] }); toast.success('对账决定已提交') },
    onError: error => toast.error(conflictLabel(error)),
  })

  const submitDecision = () => {
    if (!decision || !reason.trim() || decisionMutation.isPending) return
    decisionMutation.mutate({ approval: decision.approval, kind: decision.kind, reason: reason.trim() })
    setDecision(null); setReason('')
  }
  const submitResolution = () => {
    if (!resolution || !reason.trim() || resolutionMutation.isPending) return
    resolutionMutation.mutate({ effect: resolution.effect, kind: resolution.kind, reason: reason.trim() })
    setResolution(null); setReason('')
  }

  const total = (approvals.data?.length || 0) + (unknownEffects.data?.length || 0) + (pendingReports > 0 ? 1 : 0)
  return (
    <div className="rounded-2xl border border-slate-200 bg-white shadow-sm" data-testid="action-queue">
      <div className="flex items-center gap-2 border-b border-slate-100 px-5 py-4"><Shield className="h-4 w-4 text-indigo-500" /><h2 className="text-sm font-semibold text-slate-800">Approval / Effect 待办</h2><span className="ml-auto text-xs text-slate-400">{total ? `${total} 项` : '全部完成'}</span></div>
      {approvals.isLoading ? <div className="px-5 py-8 text-center text-sm text-slate-400">加载待办状态…</div> : approvals.isError ? <div className="flex items-center gap-3 px-5 py-8 text-sm text-amber-700"><TriangleAlert className="h-4 w-4" />待办状态暂不可用，未确认成功或已完成。</div> : total === 0 ? <div className="flex items-center gap-3 px-5 py-8 text-sm text-slate-500"><CheckCircle className="h-4 w-4 text-emerald-500" />暂无待办事项，系统运行正常</div> : <>
        {(approvals.data || []).map(item => <ApprovalCard key={item.id} approval={item} canDecide={canDecide} onDecide={(approval, kind) => { setDecision({ approval, kind }); setReason('') }} conflict={conflicts[item.id]} />)}
        {canResolve && unknownEffects.isError && <div className="flex items-center gap-3 border-t border-slate-100 px-5 py-3 text-xs text-amber-700"><TriangleAlert className="h-3.5 w-3.5" />unknown Effect 待办暂不可用，未确认成功或已完成。</div>}
        {(unknownEffects.data || []).map(item => <UnknownEffectCard key={item.id} effect={item} canResolve={canResolve} onResolve={(effect, kind) => { setResolution({ effect, kind }); setReason('') }} />)}
        {pendingReports > 0 && <a href="/reports" className="flex items-center gap-3 border-t border-slate-100 p-4 text-sm hover:bg-slate-50"><Clock3 className="h-4 w-4 text-amber-500" /><span className="flex-1">{pendingReports} 份报告待审阅</span><ChevronRight className="h-4 w-4 text-slate-400" /></a>}
      </>}
      <ConfirmDialog open={!!decision} title={decision?.kind === 'approve' ? '确认批准 Proposal' : '确认拒绝 Proposal'} description="决定会携带当前 proposal_hash 与 version 提交；后端无成功响应前不会改变界面状态。" confirmLabel={decision?.kind === 'approve' ? '确认批准' : '确认拒绝'} danger={decision?.kind !== 'approve'} reason={reason} onReasonChange={setReason} reasonRequired reasonLabel="审批理由" onConfirm={submitDecision} onClose={() => { setDecision(null); setReason('') }} />
      <ConfirmDialog open={!!resolution} title={resolution ? resolutionLabels[resolution.kind] : '提交对账决定'} description="此操作只提交 admin 对 unknown Effect 的决定与证据，不会直接调用外部 Effect。" confirmLabel="提交对账决定" danger={resolution?.kind === 'still_unknown'} reason={reason} onReasonChange={setReason} reasonRequired reasonLabel="对账证据" reasonPlaceholder="说明核查来源或仍无法确认的原因" onConfirm={submitResolution} onClose={() => { setResolution(null); setReason('') }} />
    </div>
  )
}
