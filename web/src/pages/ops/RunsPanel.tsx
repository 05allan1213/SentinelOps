import React, { useState, useEffect, useRef } from 'react'
import { RefreshCw, ChevronDown, ChevronRight, Trash2, Loader2, CheckCircle2, XCircle, Clock, Zap, X } from 'lucide-react'
import { opsService, type OpsRun } from '@/services/ops'
import { cn } from '@/utils'
import toast from 'react-hot-toast'

const severityColor: Record<string, string> = {
  critical: 'bg-red-50 text-red-700 border-red-200',
  high: 'bg-orange-50 text-orange-700 border-orange-200',
  medium: 'bg-amber-50 text-amber-700 border-amber-200',
  low: 'bg-blue-50 text-blue-700 border-blue-200',
}
const severityLabel: Record<string, string> = {
  critical: '严重', high: '高危', medium: '中危', low: '低危',
}
const actionLabel: Record<string, string> = {
  ops_agent: '运维 Agent',
  update_event_status: '更新事件状态',
  block_ip: '封禁 IP',
  ai_analyze: 'AI 深度分析',
  notify_dingtalk: '钉钉通知',
  notify_wecom: '企业微信通知',
  notify_email: '邮件通知',
  webhook_out: 'Webhook 推送',
  ai_decide: 'AI 决策',
}
const fmtDuration = (ms: number) => ms < 1000 ? `${ms}ms` : `${(ms / 1000).toFixed(1)}s`
const isActive = (s: string) => s === 'running' || s === 'pending'

export default function RunsPanel() {
  const [runs, setRuns] = useState<OpsRun[]>([])
  const [loading, setLoading] = useState(false)
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [details, setDetails] = useState<Record<string, OpsRun>>({})
  const [now, setNow] = useState(Date.now())

  const pollingRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const tickRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const expandedRef = useRef(expanded)

  const load = async () => {
    setLoading(true)
    try { setRuns(await opsService.listRuns(50)) } catch { /* ignore */ }
    finally { setLoading(false) }
  }

  const loadDetail = (id: string) =>
    opsService.getRun(id).then(r => setDetails(p => ({ ...p, [id]: r }))).catch(() => {})

  useEffect(() => { load() }, [])

  // 有活跃任务时自动轮询 + 实时计时
  useEffect(() => {
    const active = runs.filter(r => isActive(r.status))
    if (active.length > 0) {
      if (!pollingRef.current) {
        pollingRef.current = setInterval(async () => {
          const list = await opsService.listRuns(50).catch(() => runs)
          setRuns(list)
          list.forEach(r => { if (expandedRef.current.has(r.id)) loadDetail(r.id) })
        }, 2000)
      }
      if (!tickRef.current) {
        tickRef.current = setInterval(() => setNow(Date.now()), 1000)
      }
    } else {
      if (pollingRef.current) { clearInterval(pollingRef.current); pollingRef.current = null }
      if (tickRef.current) { clearInterval(tickRef.current); tickRef.current = null }
      // 任务刚完成：对所有已展开的任务做最终一次 detail 刷新
      expandedRef.current.forEach(id => loadDetail(id))
    }
    return () => {
      if (runs.filter(r => isActive(r.status)).length === 0) {
        if (pollingRef.current) { clearInterval(pollingRef.current); pollingRef.current = null }
        if (tickRef.current) { clearInterval(tickRef.current); tickRef.current = null }
      }
    }
  }, [runs.map(r => r.status).join(',')])

  const toggle = (id: string) => {
    setExpanded(prev => {
      const next = new Set(prev)
      if (next.has(id)) { next.delete(id) } else { next.add(id); if (!details[id]) loadDetail(id) }
      expandedRef.current = next
      return next
    })
  }

  const handleClear = async () => {
    if (!confirm('确认清空所有运维任务记录？')) return
    try { await opsService.clearRuns(); setRuns([]); setExpanded(new Set()); setDetails({}); toast.success('已清空') }
    catch { toast.error('清空失败') }
  }

  const handleDelete = async (id: string, e: React.MouseEvent) => {
    e.stopPropagation()
    try {
      await opsService.deleteRun(id)
      setRuns(prev => prev.filter(r => r.id !== id))
      setExpanded(prev => { const next = new Set(prev); next.delete(id); return next })
      setDetails(prev => { const next = { ...prev }; delete next[id]; return next })
    } catch { toast.error('删除失败') }
  }

  const [collapsed, setCollapsed] = useState(false)
  const activeRuns = runs.filter(r => isActive(r.status))

  return (
    <div className="bg-white rounded-xl border border-slate-200 shadow-sm overflow-hidden">
      <div className="flex items-center justify-between px-4 py-3 border-b border-slate-100 bg-slate-50">
        <div className="flex items-center gap-2 cursor-pointer" onClick={() => setCollapsed(p => !p)}>
          <Zap className="w-4 h-4 text-violet-500" />
          <span className="text-sm font-semibold text-gray-800">运维任务</span>
          {runs.length > 0 && <span className="text-xs text-gray-400 bg-slate-100 px-2 py-0.5 rounded-full">{runs.length} 条</span>}
          {activeRuns.length > 0 && (
            <span className="flex items-center gap-1 text-xs text-blue-600 bg-blue-50 px-2 py-0.5 rounded-full border border-blue-100">
              <Loader2 className="w-3 h-3 animate-spin" />{activeRuns.length} 个执行中
            </span>
          )}
          {collapsed ? <ChevronRight className="w-3.5 h-3.5 text-gray-400" /> : <ChevronDown className="w-3.5 h-3.5 text-gray-400" />}
        </div>
        <div className="flex items-center gap-2">
          {runs.length > 0 && (
            <button onClick={handleClear} className="flex items-center gap-1 text-xs text-gray-400 hover:text-red-500 transition-colors">
              <Trash2 className="w-3.5 h-3.5" /> 清空
            </button>
          )}
          <button onClick={load} className="text-gray-400 hover:text-indigo-500 transition-colors">
            <RefreshCw className={cn('w-4 h-4', loading && 'animate-spin')} />
          </button>
        </div>
      </div>

      {!collapsed && (runs.length === 0 ? (
        <div className="py-16 text-center text-sm text-gray-400">
          <Clock className="w-8 h-8 mx-auto mb-2 opacity-30" />
          暂无运维任务，在安全事件列表点击 ⚡ 触发 AI 运维
        </div>
      ) : (
        <div className="divide-y divide-slate-100">
          {runs.map(r => {
            const detail = details[r.id]
            const isOpen = expanded.has(r.id)
            const active = isActive(r.status)
            return (
              <React.Fragment key={r.id}>
                <div
                  className={cn('flex items-center gap-3 px-4 py-3 cursor-pointer hover:bg-slate-50/60 transition-colors')}
                  onClick={() => toggle(r.id)}
                >
                  <span className="text-gray-300 flex-shrink-0">
                    {isOpen ? <ChevronDown className="w-3.5 h-3.5" /> : <ChevronRight className="w-3.5 h-3.5" />}
                  </span>
                  <span className="flex-shrink-0">
                    {active ? <Loader2 className="w-4 h-4 text-blue-500 animate-spin" />
                      : r.status === 'success' ? <CheckCircle2 className="w-4 h-4 text-emerald-500" />
                      : <XCircle className="w-4 h-4 text-red-400" />}
                  </span>
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="text-sm font-medium text-gray-800 truncate">{r.event_title || r.event_id}</span>
                      {r.event_severity && (
                        <span className={cn('inline-flex items-center h-4 px-1.5 rounded text-[10px] font-semibold border flex-shrink-0',
                          severityColor[r.event_severity] || 'bg-gray-50 text-gray-500 border-gray-200')}>
                          {severityLabel[r.event_severity] || r.event_severity}
                        </span>
                      )}
                      {active && <span className="text-[10px] text-blue-500 bg-blue-50 px-1.5 py-0.5 rounded-full border border-blue-100 flex-shrink-0">执行中</span>}
                    </div>
                    {detail?.plan_summary && (
                      <p className="text-xs text-gray-400 mt-0.5 truncate">{detail.plan_summary}</p>
                    )}
                  </div>
                  <div className="text-right flex-shrink-0 flex items-center gap-2">
                    <div>
                      <p className="text-xs text-gray-500">{isActive(r.status) ? fmtDuration(now - new Date(r.started_at).getTime()) : fmtDuration(r.duration_ms)}</p>
                      <p className="text-[11px] text-gray-400">{r.started_at}</p>
                    </div>
                    <button
                      onClick={e => handleDelete(r.id, e)}
                      className="p-1 rounded text-gray-300 hover:text-red-400 hover:bg-red-50 transition-colors"
                      title="删除此任务"
                    >
                      <X className="w-3.5 h-3.5" />
                    </button>
                  </div>
                </div>

                {isOpen && (
                  <div className="px-4 pb-4 bg-slate-50/40">
                    {!detail ? (
                      <div className="py-3 flex items-center gap-2 text-xs text-gray-400 pl-6">
                        <Loader2 className="w-3.5 h-3.5 animate-spin" />加载中...
                      </div>
                    ) : !detail.steps?.length ? (
                      <div className="py-3 flex items-center gap-2 text-xs text-gray-400 pl-6">
                        <Loader2 className="w-3.5 h-3.5 animate-spin" />AI 智能运维中...
                      </div>
                    ) : (
                      <div className="mt-2 pl-6">
                        {(() => {
                          const opsAgent = detail.steps.find(s => s.action_type === 'ops_agent')
                          const subSteps = detail.steps.filter(s => s.action_type !== 'ops_agent')
                          const visibleSteps = subSteps.filter(s => {
                            const out = s.output ? (() => { try { return JSON.parse(s.output!) } catch { return {} } })() : {}
                            return out.skipped !== 'true'
                          })
                          const total = visibleSteps.length
                          const done = visibleSteps.filter(s => s.status === 'success' || s.status === 'failed').length
                          const pct = total > 0 ? Math.round((done / total) * 100) : 0

                          const renderSubStep = (s: typeof subSteps[0], isLast: boolean) => {
                            const out = s.output ? (() => { try { return JSON.parse(s.output!) } catch { return {} } })() : {}
                            const skipped = out.skipped === 'true'
                            if (skipped) return null
                            return (
                              <div key={s.id} className="flex gap-3">
                                <div className="flex flex-col items-center flex-shrink-0 pt-2">
                                  <div className={cn('w-2 h-2 rounded-full flex-shrink-0',
                                    skipped ? 'bg-gray-300' :
                                    s.status === 'success' ? 'bg-emerald-400' :
                                    s.status === 'failed' ? 'bg-red-400' : 'bg-blue-400 animate-pulse')} />
                                  {!isLast && <div className="w-px flex-1 bg-slate-200 mt-1" />}
                                </div>
                                <div className={cn('flex-1 pb-3', isLast && 'pb-1')}>
                                  <div className="flex items-center gap-2 pt-1 flex-wrap">
                                    <span className="text-sm font-medium text-gray-700">{actionLabel[s.action_type] || s.action_type}</span>
                                    <span className={cn('text-xs px-1.5 py-0.5 rounded font-semibold',
                                      skipped ? 'bg-gray-50 text-gray-400' :
                                      s.status === 'success' ? 'bg-emerald-50 text-emerald-600' :
                                      s.status === 'failed' ? 'bg-red-50 text-red-600' : 'bg-blue-50 text-blue-600')}>
                                      {skipped ? '已跳过' : s.status === 'success' ? '成功' : s.status === 'failed' ? '失败' : '执行中'}
                                    </span>
                                  </div>
                                  {s.action_type === 'notify_email' && out.sent === 'true' && (
                                    <p className="text-xs text-gray-400 mt-0.5">已发送至 {out.to}</p>
                                  )}
                                  {s.action_type === 'block_ip' && out.ip && (
                                    <p className="text-xs text-orange-600 mt-0.5">
                                      {out.blocked === 'true' ? `IP ${out.ip} 已加入封禁名单` : `IP ${out.ip} 在保护名单中，已跳过`}
                                    </p>
                                  )}
                                  {s.action_type === 'update_event_status' && out.status && (
                                    <p className="text-xs text-gray-400 mt-0.5">状态已更新为「{out.status}」</p>
                                  )}
                                  {skipped && out.message && (
                                    <p className="text-xs text-gray-400 mt-0.5">{out.message}</p>
                                  )}
                                  {s.error_msg && !skipped && (
                                    <p className="text-xs text-red-500 mt-0.5">{s.error_msg}</p>
                                  )}
                                </div>
                              </div>
                            )
                          }

                          return (
                            <div className="flex gap-3">
                              <div className="flex flex-col items-center flex-shrink-0 pt-2">
                                <div className={cn('w-2 h-2 rounded-full flex-shrink-0',
                                  opsAgent?.status === 'success' ? 'bg-emerald-400' :
                                  opsAgent?.status === 'failed' ? 'bg-red-400' : 'bg-blue-400 animate-pulse')} />
                              </div>
                              <div className="flex-1 pb-1">
                                <div className="flex items-center gap-2 pt-1 flex-wrap">
                                  <span className="text-sm font-medium text-gray-700">运维 Agent</span>
                                  <span className={cn('text-xs px-1.5 py-0.5 rounded font-semibold',
                                    opsAgent?.status === 'success' ? 'bg-emerald-50 text-emerald-600' :
                                    opsAgent?.status === 'failed' ? 'bg-red-50 text-red-600' : 'bg-blue-50 text-blue-600')}>
                                    {opsAgent?.status === 'success' ? '成功' : opsAgent?.status === 'failed' ? '失败' : '执行中'}
                                  </span>
                                  <span className="text-xs text-gray-400">{isActive(r.status) ? fmtDuration(now - new Date(r.started_at).getTime()) : fmtDuration(r.duration_ms)}</span>
                                </div>
                                {/* 进度条 */}
                                {total > 0 && (
                                  <div className="mt-2 mb-2">
                                    <div className="flex items-center justify-between mb-1">
                                      <span className="text-xs text-gray-400">{done}/{total} 步骤完成</span>
                                      <span className="text-xs text-gray-400">{pct}%</span>
                                    </div>
                                    <div className="h-1 bg-slate-200 rounded-full overflow-hidden">
                                      <div className={cn('h-full rounded-full transition-all duration-500',
                                        r.status === 'failed' ? 'bg-red-400' : 'bg-emerald-400'
                                      )} style={{ width: `${pct}%` }} />
                                    </div>
                                  </div>
                                )}
                                {/* 子步骤 */}
                                <div className="mt-1 pl-3 border-l border-slate-200 flex flex-col gap-0">
                                  {subSteps.map((s, idx) => renderSubStep(s, idx === subSteps.length - 1))}
                                </div>
                              </div>
                            </div>
                          )
                        })()}
                      </div>
                    )}
                  </div>
                )}
              </React.Fragment>
            )
          })}
        </div>
      ))}
    </div>
  )
}
