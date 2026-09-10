import { AlertTriangle, ShieldCheck } from 'lucide-react'
import RuntimeQualityState from './RuntimeQualityState'
import type { SafetyRes } from '@/types/runtime'

interface Props {
  data: SafetyRes | undefined
  loading: boolean
}

const dash = (value: unknown) => (value === null || value === undefined || value === '' ? '—' : String(value))

export default function SafetySummary({ data, loading }: Props) {
  const safety = data?.item

  if (loading && !safety) {
    return <p data-testid="runtime-safety-loading" className="rounded-xl border border-gray-200 bg-white px-4 py-8 text-center text-sm text-gray-500">加载中…</p>
  }
  if (!safety) {
    return (
      <p data-testid="runtime-safety-unavailable" className="rounded-xl border border-gray-200 bg-gray-50 px-4 py-8 text-center text-sm text-gray-600">
        Safety 数据当前不可用{data?.reason_code ? `（reason_code: ${data.reason_code}）` : ''}
      </p>
    )
  }

  const capList = (value: string[] | undefined) => (value && value.length > 0 ? value.join(', ') : '—')

  return (
    <div data-testid="runtime-safety-summary" className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-3">
        {safety.shadow_mode ? (
          <span data-testid="runtime-safety-shadow" className="inline-flex items-center gap-2 rounded-lg border border-amber-300 bg-amber-50 px-3 py-1.5 text-sm text-amber-900">
            <AlertTriangle className="w-4 h-4" />
            Shadow Mode 开启：L1/L2 有效写入关闭，配置值不等于执行结果
          </span>
        ) : (
          <span data-testid="runtime-safety-shadow" className="inline-flex items-center gap-2 rounded-lg border border-emerald-200 bg-emerald-50 px-3 py-1.5 text-sm text-emerald-800">
            <ShieldCheck className="w-4 h-4" />
            Shadow Mode 关闭
          </span>
        )}
        <RuntimeQualityState availability={data.availability} dataQuality={data.data_quality} reasonCode={data.reason_code} notRun={data.not_run} />
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        <section className="rounded-xl border border-gray-200 bg-white p-4">
          <h3 className="text-sm font-medium text-gray-900">Gate 生效状态</h3>
          <dl className="mt-3 grid grid-cols-2 gap-3 text-xs">
            <div><dt className="text-gray-500">Static caps</dt><dd className="text-gray-800 break-words">{capList(safety.static_caps)}</dd></div>
            <div><dt className="text-gray-500">Dynamic caps</dt><dd className="text-gray-800 break-words">{capList(safety.dynamic_caps)}</dd></div>
            <div className="col-span-2"><dt className="text-gray-500">Current effective</dt><dd className="text-gray-800 break-words">{capList(safety.current_effective)}</dd></div>
            <div><dt className="text-gray-500">L1 写入（有效）</dt><dd data-testid="runtime-safety-l1" className="text-gray-800">{safety.current_effective?.includes('l1_write') ? '允许' : '阻止'}</dd></div>
            <div><dt className="text-gray-500">L2 写入（有效）</dt><dd data-testid="runtime-safety-l2" className="text-gray-800">{safety.current_effective?.includes('l2_write') ? '允许' : '阻止'}</dd></div>
          </dl>
        </section>

        <section className="rounded-xl border border-gray-200 bg-white p-4">
          <h3 className="text-sm font-medium text-gray-900">策略与审计</h3>
          <dl className="mt-3 grid grid-cols-2 gap-3 text-xs">
            <div><dt className="text-gray-500">Policy hash</dt><dd className="font-mono text-gray-800 break-all">{dash(safety.policy_hash)}</dd></div>
            <div><dt className="text-gray-500">Catalog revision</dt><dd className="font-mono text-gray-800 break-all">{dash(safety.catalog_revision)}</dd></div>
            <div><dt className="text-gray-500">Gate 审计</dt><dd data-testid="runtime-safety-audit" className="text-gray-800">{safety.gate_audit_available ? '可用' : '不可用'}</dd></div>
            <div><dt className="text-gray-500">写入入口</dt><dd className="text-gray-800">仅既有 settings admin API</dd></div>
          </dl>
        </section>
      </div>
      <p className="text-xs text-gray-500">本页面只读；Gate 变更继续走既有系统设置页与 admin API，Runtime 不提供旁路开关。</p>
    </div>
  )
}
