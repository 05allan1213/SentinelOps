import { useState, useEffect, useRef } from 'react'
import { FileText, Loader2 } from 'lucide-react'
import { useAnalyzeStore } from '@/stores/analyzeStore'
import { useEventStore } from '@/stores/eventStore'
import { reportService } from '@/services/report'
import ReportModal, { buildMarkdown } from '@/pages/event-analysis/components/ReportModal'
import type { ReportPayload } from '@/types'
import toast from 'react-hot-toast'

/**
 * 报告生成进度弹窗。
 * 挂载在顶层 Layout，与当前路由无关，切换页面后继续运行。
 */
export default function ReportGenerateModal() {
  const {
    riskData, analysisText,
    reportGenerating, reportSaving,
    setReportGenerating, setReportSaving,
  } = useAnalyzeStore()
  const { agentLogs } = useEventStore()

  const [showReport, setShowReport] = useState(false)
  const isSavingRef = useRef(false)

  // ── 进度计算 ─────────────────────────────────────────────────────────────────
  const targetEvents = riskData?.events || []
  const doneCount = targetEvents.filter(e => e.recommendationComplete).length
  const totalCount = targetEvents.length
  const progressPct = totalCount > 0 ? Math.round((doneCount / totalCount) * 100) : 100

  // 监控进度，全部完成后自动触发保存
  useEffect(() => {
    if (!reportGenerating) {
      isSavingRef.current = false
      return
    }
    if (isSavingRef.current) return
    if (totalCount === 0 || doneCount >= totalCount) {
      isSavingRef.current = true
      doSaveReport()
    }
  }, [reportGenerating, doneCount, totalCount])

  const doSaveReport = async () => {
    if (!riskData) return
    setReportSaving(true)
    try {
      const now = new Date()
      const dateStr = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`
      const timeStr = `${String(now.getHours()).padStart(2, '0')}:${String(now.getMinutes()).padStart(2, '0')}`
      const title = `安全事件分析报告 ${dateStr} ${timeStr}`
      const markdown = buildMarkdown(riskData, agentLogs)
      const payload: ReportPayload = {
        format: 'sentinel-report-v1',
        meta: {
          report_id: title,
          generated_at: now.toISOString(),
          event_count: riskData.count,
          critical_count: riskData.critical ?? 0,
          high_count: riskData.highRisk ?? 0,
          max_cvss: riskData.maxCVSS,
        },
        risk_data: riskData,
        analysis_text: analysisText,
        agent_logs: agentLogs,
        markdown,
      }
      await reportService.save(title, JSON.stringify(payload))
      toast.success('报告已保存到报告库')
    } catch {
      toast.error('报告保存失败，请重试')
    } finally {
      setReportSaving(false)
      setReportGenerating(false)
      setShowReport(true)
    }
  }

  const handleForceGenerate = () => {
    if (isSavingRef.current) return
    isSavingRef.current = true
    doSaveReport()
  }

  if (!reportGenerating && !showReport) return null

  return (
    <>
      {/* 进度弹窗：fixed 定位，覆盖全屏，切换页面后依然可见 */}
      {reportGenerating && riskData && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/30">
          <div className="w-[440px] rounded-2xl bg-white border border-gray-200 shadow-xl overflow-hidden">
            {/* 头部 */}
            <div className="px-6 py-4 border-b border-gray-100 flex items-center gap-2.5">
              <FileText className="w-5 h-5 text-primary-500" />
              <span className="text-base font-semibold text-gray-900">安全分析报告生成</span>
            </div>

            {/* 主体 */}
            <div className="px-6 py-5">
              {/* 线性进度条 */}
              <div className="mb-5">
                <div className="flex items-center justify-between mb-2">
                  <div className="text-sm font-medium text-gray-900">
                    {reportSaving
                      ? '正在保存报告到报告库...'
                      : progressPct >= 100
                        ? '所有修复方案已生成完毕'
                        : 'AI 修复方案生成中'
                    }
                  </div>
                  <div className="flex items-center gap-1.5">
                    {reportSaving
                      ? <Loader2 className="w-4 h-4 animate-spin text-primary-500" />
                      : <span className="text-sm font-bold text-gray-700">{progressPct}%</span>
                    }
                  </div>
                </div>
                <div className="w-full h-2 bg-gray-100 rounded-full overflow-hidden">
                  <div
                    className="h-full rounded-full transition-all duration-500 ease-out"
                    style={{
                      width: `${progressPct}%`,
                      backgroundColor: progressPct >= 100 ? '#10b981' : '#3b82f6',
                    }}
                  />
                </div>
                {totalCount > 0 && (
                  <div className="text-xs text-gray-400 mt-1.5">
                    {doneCount} / {totalCount} 个事件修复方案已完成
                  </div>
                )}
              </div>

              {/* 事件进度列表 */}
              {targetEvents.length > 0 && (
                <div className="rounded-xl border border-gray-100 bg-gray-50 p-3 space-y-1.5 max-h-44 overflow-y-auto mb-4">
                  {targetEvents.map(ev => (
                    <div key={ev.event_id} className="flex items-center gap-2 text-xs">
                      {ev.recommendationComplete
                        ? <span className="text-emerald-500 shrink-0 font-bold">✓</span>
                        : <Loader2 className="w-3 h-3 animate-spin text-gray-300 shrink-0" />
                      }
                      <span className={`truncate flex-1 ${ev.recommendationComplete ? 'text-gray-600' : 'text-gray-400'}`}>
                        {ev.title}
                      </span>
                      <span className={`shrink-0 ${ev.recommendationComplete ? 'text-emerald-500' : 'text-gray-300'}`}>
                        {ev.recommendationComplete ? '完成' : '生成中'}
                      </span>
                    </div>
                  ))}
                </div>
              )}
            </div>

            {/* 底部按钮 */}
            {!reportSaving && (
              <div className="px-6 pb-5 flex gap-2">
                {progressPct < 100 && (
                  <button onClick={handleForceGenerate} className="btn-default flex-1 text-sm">
                    跳过等待，立即生成
                  </button>
                )}
                <button onClick={() => setReportGenerating(false)} className="btn-default flex-1 text-sm">
                  取消
                </button>
              </div>
            )}
          </div>
        </div>
      )}

      {/* 报告预览弹窗 */}
      <ReportModal
        visible={showReport}
        onClose={() => setShowReport(false)}
        data={riskData}
        logs={agentLogs}
        analysisText={analysisText}
      />
    </>
  )
}
