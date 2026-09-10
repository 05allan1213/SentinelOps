import { Bot, Clock, Gauge, KeyRound, Layers, ShieldCheck, UserRound } from 'lucide-react'
import { cn } from '@/utils'
import RuntimeQualityState from './RuntimeQualityState'
import RuntimeStatusBadge from './RuntimeStatusBadge'
import type { RunDetailDTO } from '@/types/runtime'

interface Props {
  run: RunDetailDTO
}

const dash = (value: unknown) => (value === null || value === undefined || value === '' ? '—' : String(value))

const formatTime = (value: string | null | undefined) => {
  if (!value) return '—'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN', { hour12: false })
}

const bool = (value: boolean | undefined, trueLabel = '匹配', falseLabel = '不匹配') => (value ? trueLabel : falseLabel)

function Field({ label, value, mono, testId }: { label: string; value: React.ReactNode; mono?: boolean; testId?: string }) {
  return (
    <div className="min-w-0">
      <dt className="text-xs text-gray-500">{label}</dt>
      <dd
        data-testid={testId}
        className={cn('mt-0.5 text-sm text-gray-900 break-words', mono && 'font-mono text-xs')}
        title={typeof value === 'string' ? value : undefined}
      >
        {value}
      </dd>
    </div>
  )
}

function Panel({ title, icon, children, testId }: { title: string; icon: React.ReactNode; children: React.ReactNode; testId: string }) {
  return (
    <section data-testid={testId} className="rounded-xl border border-gray-200 bg-white p-4">
      <h3 className="flex items-center gap-2 text-sm font-medium text-gray-900">
        {icon}
        {title}
      </h3>
      <div className="mt-3">{children}</div>
    </section>
  )
}

export default function RunOverview({ run }: Props) {
  const summary = run.summary
  const budget = run.budget ?? summary?.budget
  const compatibility = run.compatibility
  const gates = run.gate_summary
  const context = run.context_summary
  const attempt = run.current_attempt

  return (
    <div className="flex flex-col gap-4" data-testid="runtime-run-overview">
      <div className="flex flex-wrap items-center gap-3">
        <RuntimeStatusBadge status={summary?.status} phase={summary?.current_phase} />
        <RuntimeQualityState availability={run.availability} dataQuality={run.data_quality} reasonCode={run.reason_code} notRun={run.not_run} />
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        <Panel title="身份与编排" icon={<Bot className="w-4 h-4 text-indigo-500" />} testId="runtime-overview-identity">
          <dl className="grid grid-cols-2 gap-3">
            <Field label="Run ID" value={dash(summary?.run_id)} mono />
            <Field label="Session" value={dash(summary?.session_id)} mono />
            <Field label="Agent" value={dash(summary?.agent)} />
            <Field label="Workflow Key" value={dash(summary?.workflow_key)} mono />
            <Field label="Runtime Mode" value={dash(summary?.runtime_mode)} />
            <Field label="Runtime Version" value={dash(summary?.runtime_version)} mono />
            <Field label="Attempt" value={dash(summary?.attempt)} testId="runtime-overview-attempt" />
            <Field label="Recovery Mode" value={dash(summary?.recovery_mode)} />
            <Field label="开始时间" value={formatTime(summary?.started_at)} />
            <Field label="结束时间" value={formatTime(summary?.finished_at)} />
          </dl>
        </Panel>

        <Panel title="Worker 与租约" icon={<Layers className="w-4 h-4 text-indigo-500" />} testId="runtime-overview-lease">
          <dl className="grid grid-cols-2 gap-3">
            <Field label="Worker（服务端脱敏）" value={dash(summary?.worker_id)} mono testId="runtime-overview-worker" />
            <Field label="Lease 状态" value={dash(summary?.lease_state)} />
            <Field label="Generation" value={dash(summary?.lease_generation)} testId="runtime-overview-generation" />
            <Field label="最后心跳" value={formatTime(summary?.heartbeat_at)} />
            <Field label="Park Reason" value={dash(summary?.park_reason)} />
            <Field
              label="当前 Attempt 执行 Worker"
              value={attempt ? `${dash(attempt.worker_id)} · gen ${dash(attempt.lease_generation)}` : '—'}
              mono
            />
          </dl>
        </Panel>

        <Panel title="Runtime 兼容性" icon={<ShieldCheck className="w-4 h-4 text-indigo-500" />} testId="runtime-overview-compatibility">
          <dl className="grid grid-cols-2 gap-3">
            <Field label="Run fingerprint" value={dash(compatibility?.run_fingerprint)} mono />
            <Field label="Run 匹配" value={bool(compatibility?.run_match)} testId="runtime-overview-run-match" />
            <Field label="Checkpoint fingerprint" value={dash(compatibility?.checkpoint_fingerprint)} mono />
            <Field label="Checkpoint 匹配" value={bool(compatibility?.checkpoint_match)} />
            <Field label="Attempt fingerprint" value={dash(compatibility?.attempt_fingerprint)} mono />
            <Field label="Executing worker fingerprint" value={dash(compatibility?.executing_worker_fingerprint)} mono />
            <Field label="Worker 匹配" value={bool(compatibility?.worker_match)} />
            <Field label="Exact Restore 允许" value={compatibility?.exact_restore_allowed ? '允许' : '不允许'} testId="runtime-overview-restore" />
          </dl>
          <div className="mt-3">
            <RuntimeQualityState availability={compatibility?.availability} dataQuality={compatibility?.data_quality} reasonCode={compatibility?.reason_code} />
          </div>
        </Panel>

        <Panel title="Gate 与策略" icon={<KeyRound className="w-4 h-4 text-indigo-500" />} testId="runtime-overview-gates">
          <dl className="grid grid-cols-2 gap-3">
            <Field label="Shadow Mode" value={gates?.shadow_mode ? '开启（写入关闭）' : '关闭'} testId="runtime-overview-shadow" />
            <Field label="L1 写入" value={gates?.l1_write_allowed ? '允许' : '阻止'} />
            <Field label="L2 写入" value={gates?.l2_write_allowed ? '允许' : '阻止'} />
            <Field label="Static / Dynamic / Effective" value={`${gates?.static_caps?.length ?? 0} / ${gates?.dynamic_caps?.length ?? 0} / ${gates?.effective_caps?.length ?? 0}`} />
            <Field label="Policy Hash" value={dash(gates?.policy_hash)} mono />
            <Field label="Catalog Revision" value={dash(gates?.catalog_revision)} mono />
            <Field label="Gate 审计可用" value={gates?.audit_available ? '可用' : '不可用'} />
            <Field label="可恢复操作（服务端）" value={(run.allowed_recovery_actions ?? []).join(', ') || '—'} testId="runtime-overview-recovery-actions" />
          </dl>
        </Panel>

        <Panel title="Budget 与用量" icon={<Gauge className="w-4 h-4 text-indigo-500" />} testId="runtime-overview-budget">
          <dl className="grid grid-cols-3 gap-3">
            <Field label="模型调用" value={dash(budget?.model_calls)} testId="runtime-overview-model-calls" />
            <Field label="工具调用" value={dash(budget?.tool_calls)} />
            <Field label="迭代" value={dash(budget?.iterations)} />
            <Field label="输入 Tokens" value={dash(budget?.input_tokens)} />
            <Field label="输出 Tokens" value={dash(budget?.output_tokens)} />
            <Field label="成本 (CNY)" value={dash(budget?.cost_cny)} />
            <Field label="已用时长" value={dash(budget?.elapsed_ms)} />
            <Field label="预留模型调用" value={dash(budget?.reserved_model_calls)} />
            <Field label="预留工具调用" value={dash(budget?.reserved_tool_calls)} />
            <Field label="MCP / RAG 调用" value={`${dash(budget?.mcp_calls)} / ${dash(budget?.rag_calls)}`} />
            <Field label="预算耗尽" value={budget?.exhausted ? '是' : '否'} />
            <Field label="耗尽原因" value={dash(budget?.exhausted_reason)} />
          </dl>
          <div className="mt-3 flex flex-wrap items-center gap-2 text-xs text-gray-500">
            <Clock className="w-3.5 h-3.5" />
            usage quality: {summary?.usage_quality ?? '—'} · trace quality: {summary?.trace_quality ?? '—'}
          </div>
        </Panel>

        <Panel title="Context 概要" icon={<UserRound className="w-4 h-4 text-indigo-500" />} testId="runtime-overview-context">
          <dl className="grid grid-cols-2 gap-3">
            <Field label="身份" value={`${dash(context?.identity?.username || context?.identity?.user_id)} (${dash(context?.identity?.role)})`} />
            <Field label="Scope" value={dash(context?.identity?.scope)} mono />
            <Field label="Session Revision（使用 / 提交）" value={`${dash(context?.session_revision_used)} / ${dash(context?.session_revision_committed)}`} />
            <Field label="History 条数" value={dash(context?.history_count)} />
            <Field label="Summary Hash" value={dash(context?.summary_hash)} mono />
            <Field label="Budget Limits Hash" value={dash(context?.budget_limits_hash)} mono />
            <Field label="Policy / Config Hash" value={`${dash(context?.policy_hash)} / ${dash(context?.config_hash)}`} mono />
            <Field label="Deadline" value={formatTime(context?.deadline_at)} />
          </dl>
          <div className="mt-3">
            <RuntimeQualityState availability={context?.availability} dataQuality={context?.data_quality} reasonCode={context?.reason_code} />
          </div>
        </Panel>
      </div>
    </div>
  )
}
