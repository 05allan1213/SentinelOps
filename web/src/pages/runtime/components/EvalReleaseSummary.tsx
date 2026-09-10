import { BarChart3, Database, Rocket } from 'lucide-react'
import { useRuntimeEval, useRuntimeRelease, useRuntimeRetention } from '@/hooks/useRuntimeQueries'
import RuntimeQualityState from './RuntimeQualityState'

const dash = (value: unknown) => (value === null || value === undefined || value === '' ? '—' : String(value))

const formatTime = (value: string | null | undefined) => {
  if (!value) return '—'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN', { hour12: false })
}

function Section({ title, icon, children, testId }: { title: string; icon: React.ReactNode; children: React.ReactNode; testId: string }) {
  return (
    <section data-testid={testId} className="rounded-xl border border-gray-200 bg-white p-4">
      <h3 className="flex items-center gap-2 text-sm font-medium text-gray-900">{icon}{title}</h3>
      <div className="mt-3">{children}</div>
    </section>
  )
}

/** Read-only Eval/Release/Retention facts on the Worker Health route; no control plane, no fake green. */
export default function EvalReleaseSummary() {
  const evalQuery = useRuntimeEval({ page: 1, page_size: 20 })
  const releaseQuery = useRuntimeRelease()
  const retentionQuery = useRuntimeRetention()

  const evalItem = evalQuery.data?.item
  const releaseItem = releaseQuery.data?.item
  const retentionItem = retentionQuery.data?.item

  return (
    <div className="grid gap-4 lg:grid-cols-3">
      <Section title="Agent Eval" icon={<BarChart3 className="w-4 h-4 text-indigo-500" />} testId="runtime-eval-section">
        <RuntimeQualityState availability={evalQuery.data?.availability} dataQuality={evalQuery.data?.data_quality} reasonCode={evalQuery.data?.reason_code} notRun={evalQuery.data?.not_run} />
        <dl className="mt-2 grid grid-cols-2 gap-2 text-xs">
          <div><dt className="text-gray-500">Suite</dt><dd className="text-gray-800">{dash(evalItem?.suite)}</dd></div>
          <div><dt className="text-gray-500">Case / Pass / Fail</dt><dd className="tabular-nums text-gray-800">{dash(evalItem?.case_count)} / {dash(evalItem?.passed)} / {dash(evalItem?.failed)}</dd></div>
          <div><dt className="text-gray-500">Baseline / Runtime 版本</dt><dd className="font-mono text-gray-800">{dash(evalItem?.baseline_version)} / {dash(evalItem?.runtime_version)}</dd></div>
          <div><dt className="text-gray-500">Regression</dt><dd className="text-gray-800">{evalItem?.regression === null || evalItem?.regression === undefined ? '—' : evalItem.regression ? '是' : '否'}</dd></div>
          <div className="col-span-2"><dt className="text-gray-500">Gate / Judge</dt><dd className="text-gray-800">{dash(evalItem?.deterministic_gate_result)} / {dash(evalItem?.llm_judge_result)}</dd></div>
        </dl>
        <p className="mt-2 text-[11px] text-gray-400">Agent Eval 与现有 RAG Eval 页面是不同事实源；无版本化结果时显示 not_run。</p>
      </Section>

      <Section title="Release / Gray / Rollback" icon={<Rocket className="w-4 h-4 text-indigo-500" />} testId="runtime-release-section">
        <RuntimeQualityState availability={releaseQuery.data?.availability} dataQuality={releaseQuery.data?.data_quality} reasonCode={releaseQuery.data?.reason_code} notRun={releaseQuery.data?.not_run} />
        <dl className="mt-2 grid grid-cols-2 gap-2 text-xs">
          <div><dt className="text-gray-500">Runtime 版本</dt><dd className="font-mono text-gray-800">{dash(releaseItem?.runtime_version)}</dd></div>
          <div><dt className="text-gray-500">观测 Worker 版本</dt><dd className="font-mono text-gray-800 break-all">{releaseItem?.observed_worker_versions?.join(', ') || '—'}</dd></div>
          <div><dt className="text-gray-500">Gray 状态</dt><dd data-testid="runtime-release-gray" className="text-gray-800">{dash(releaseItem?.gray_state)}</dd></div>
          <div><dt className="text-gray-500">Rollback 状态</dt><dd data-testid="runtime-release-rollback" className="text-gray-800">{dash(releaseItem?.rollback_state)}</dd></div>
          <div className="col-span-2"><dt className="text-gray-500">Gate vector</dt><dd className="text-gray-800 break-words">{releaseItem?.gate_vector ? Object.entries(releaseItem.gate_vector).map(([key, value]) => `${key}:${value ? 'on' : 'off'}`).join(', ') : '—'}</dd></div>
        </dl>
        <p className="mt-2 text-[11px] text-gray-400">没有持久化 gray/rollback 事实时保持 unavailable/not_observed，不显示发布控制按钮。</p>
      </Section>

      <Section title="Retention" icon={<Database className="w-4 h-4 text-indigo-500" />} testId="runtime-retention-section">
        <RuntimeQualityState availability={retentionQuery.data?.availability} dataQuality={retentionQuery.data?.data_quality} reasonCode={retentionQuery.data?.reason_code} notRun={retentionQuery.data?.not_run} />
        <dl className="mt-2 grid grid-cols-2 gap-2 text-xs">
          <div><dt className="text-gray-500">Payload 保留天数</dt><dd className="tabular-nums text-gray-800">{dash(retentionItem?.payload_days)}</dd></div>
          <div><dt className="text-gray-500">Audit 保留天数</dt><dd className="tabular-nums text-gray-800">{dash(retentionItem?.audit_days)}</dd></div>
          <div><dt className="text-gray-500">策略有效</dt><dd className="text-gray-800">{retentionItem ? (retentionItem.policy_valid ? '是' : '否') : '—'}</dd></div>
          <div><dt className="text-gray-500">保护中的活跃对象</dt><dd className="tabular-nums text-gray-800">{dash(retentionItem?.protected_active_count)}</dd></div>
          <div className="col-span-2"><dt className="text-gray-500">最近清理</dt><dd className="tabular-nums text-gray-800">{formatTime(retentionItem?.last_cleanup)}</dd></div>
        </dl>
        <p className="mt-2 text-[11px] text-gray-400">缺失清理证据时显示 unavailable，不推断清理成功。</p>
      </Section>
    </div>
  )
}
