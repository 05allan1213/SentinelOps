import { useState, useEffect, useRef } from 'react'
import { FileText, BrainCircuit, RotateCcw } from 'lucide-react'
import { useLocation } from 'react-router-dom'
import toast from 'react-hot-toast'
import { useEventStore } from '@/stores/eventStore'
import { useAnalyzeStore } from '@/stores/analyzeStore'
import AgentFlowGraph from './components/AgentFlowGraph'
import ThinkingConsole from './components/ThinkingConsole'
import StatsBar from './components/StatsBar'
import StartButton from './components/StartButton'
import ResultPanel from './components/ResultPanel'
import AnalysisModeSelect, { type AnalysisMode } from './components/AnalysisModeSelect'
import EventPickerModal from './components/EventPickerModal'
import ReportModal, { buildMarkdown } from './components/ReportModal'
import { eventService } from '@/services/event'
import { reportService } from '@/services/report'

export default function EventAnalysis() {
  const { clearLogs, isProcessing, setProcessing, agentLogs, addLog, terminateRunningLogs } = useEventStore()
  const { riskData, analysisText, savedAt, setResult, clearResult, updateEventSolution, reportGenerating, reportSaving, setReportGenerating, setReportSaving } = useAnalyzeStore()
  const location = useLocation()
  const abortControllerRef = useRef<AbortController | null>(null)
  const stageTimersRef = useRef<ReturnType<typeof setTimeout>[]>([])
  const [analysisMode, setAnalysisMode] = useState<AnalysisMode>(() => {
    return location.state?.mode === 'specific' ? 'specific' : 'latest'
  })
  const [selectedEventIds, setSelectedEventIds] = useState<string[]>(() => {
    return location.state?.preSelectedIds ?? []
  })
  const [showEventPicker, setShowEventPicker] = useState(false)

  // 从事件列表页跳转时，若携带了预选事件则自动读取（仅首次挂载）
  useEffect(() => {
    if (location.state?.mode === 'specific' && Array.isArray(location.state?.preSelectedIds) && location.state.preSelectedIds.length > 0) {
      setAnalysisMode('specific')
      setSelectedEventIds(location.state.preSelectedIds)
    }
  }, [])

  // ── 模式与选择 ────────────────────────────────────────────────────────────────
  const handleModeChange = (mode: AnalysisMode) => {
    setAnalysisMode(mode)
    if (mode === 'specific') {
      setShowEventPicker(true)
    }
  }

  const handleSolutionUpdate = (eventId: string, content: string, complete: boolean) => {
    if (!complete) return
    updateEventSolution(eventId, content)
  }

  const handleSaveReport = async () => {
    if (!riskData) return
    setReportSaving(true)
    try {
      const now = new Date()
      const dateStr = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`
      const timeStr = `${String(now.getHours()).padStart(2, '0')}:${String(now.getMinutes()).padStart(2, '0')}`
      const reportId = `REPORT-${dateStr.replace(/-/g, '')}-${timeStr.replace(':', '')}`
      const md = buildMarkdown(riskData, agentLogs)
      const payload = JSON.stringify({
        format: 'sentinel-report-v1',
        meta: {
          report_id: reportId,
          generated_at: now.toISOString(),
          event_count: riskData.count,
          critical_count: riskData.critical ?? 0,
          high_count: riskData.highRisk ?? 0,
          max_cvss: riskData.maxCVSS,
        },
        risk_data: riskData,
        analysis_text: analysisText,
        agent_logs: agentLogs,
        markdown: md,
      })
      await reportService.save(`安全事件分析报告 ${dateStr} ${timeStr}`, payload, 'custom')
      toast.success('报告已保存到报告库')
      setReportGenerating(false)
    } catch (err) {
      console.error('[handleSaveReport] 保存失败:', err)
      toast.error('报告保存失败，请重试')
    } finally {
      setReportSaving(false)
    }
  }

  // ── 分析主流程 ────────────────────────────────────────────────────────────────
  const startAnalysis = async () => {
    if (analysisMode === 'specific' && selectedEventIds.length === 0) {
      setShowEventPicker(true)
      return
    }
    clearLogs()
    setProcessing(true)
    clearResult()

    // 清空旧定时器
    stageTimersRef.current.forEach(t => clearTimeout(t))
    stageTimersRef.current = []

    try {
      const mapEvent = (e: { id: string; title: string; severity: string; source?: string; source_url?: string; cve_id?: string; cvss_score?: number }, idx: number) => ({
        id: idx + 1,
        event_id: e.id,
        title: e.title,
        desc: '',
        cve_id: e.cve_id || '',
        cvss: e.cvss_score || (e.severity === 'critical' ? 9.0 : e.severity === 'high' ? 7.0 : 5.0),
        severity: e.severity,
        vendor: '',
        product: '',
        source: e.source || '',
        source_url: e.source_url || '',
      })

      let severeEvents: ReturnType<typeof mapEvent>[]
      if (analysisMode === 'specific') {
        const allRes = await eventService.list({ size: 100 })
        severeEvents = allRes.list
          .filter(e => selectedEventIds.includes(e.id))
          .map((e, idx) => mapEvent(e, idx))
      } else {
        // latest：取最近 10 条，按时间倒序
        const latestRes = await eventService.list({ size: 10 })
        severeEvents = latestRes.list.map((e, idx) => mapEvent(e, idx))
      }

      const dispCritical = severeEvents.filter(e => e.severity === 'critical').length
      const dispHigh     = severeEvents.filter(e => e.severity === 'high').length
      const dispTotal    = severeEvents.length
      const dispMaxCVSS  = dispCritical > 0 ? 9.0 : dispHigh > 0 ? 7.0 : 5.0

      const buildSpecificQuery = () => {
        if (severeEvents.length === 0) return `分析以下事件 ID：${selectedEventIds.join(', ')}`
        const titles = severeEvents.map(e => e.title).join('；')
        return `分析以下安全事件（共 ${severeEvents.length} 条）：${titles}。请评估这些事件的威胁态势并给出处置建议。`
      }
      const query = analysisMode === 'specific'
        ? buildSpecificQuery()
        : `分析最新 ${dispTotal} 条安全事件，重点关注高危漏洞`

      const agentSequence = ['数据采集Agent', '提取Agent', '去重Agent', '风险评估Agent', '解决方案Agent']
      const stageStartMessages: Record<string, string> = {
        '提取Agent':    `正在解析事件特征：CVE 编号、CVSS 评分、漏洞类型...`,
        '去重Agent':    `正在对比事件指纹哈希，过滤重复数据...`,
        '风险评估Agent': `正在评估威胁等级：发现 ${dispCritical} 个严重漏洞，${dispHigh} 个高危漏洞`,
        '解决方案Agent': `正在综合 CVSS ${dispMaxCVSS} 分析生成修复建议...`,
      }
      const stageDoneMessages: Record<string, string> = {
        '数据采集Agent': `采集完成，共获取 ${dispTotal} 条安全事件`,
        '提取Agent':    `特征提取完成，识别到 ${dispCritical + dispHigh} 条高危事件`,
        '去重Agent':    `去重完成，有效事件 ${dispTotal} 条`,
        '风险评估Agent': `风险评估完成，最高 CVSS ${dispMaxCVSS}，需立即处置 ${dispCritical} 条`,
        '解决方案Agent': `分析完成，AI 研判结论已生成`,
      }

      let currentStage = 0
      let finalized = false

      // 立即显示第一阶段（数据采集 running）
      addLog({ agent: '数据采集Agent', status: 'running', message: `正在采集安全事件数据，共 ${dispTotal} 条...`, timestamp: new Date().toISOString() })

      // 定时推进后续阶段：每 1.5s 前进一步，保证过程可见
      // 每次 setTimeout 独立触发 React setState，不会被批量合并
      for (let nextStage = 1; nextStage < agentSequence.length; nextStage++) {
        const prevStage = nextStage - 1
        const ns = nextStage
        const timer = setTimeout(() => {
          if (finalized) return
          addLog({ agent: agentSequence[prevStage], status: 'success', message: stageDoneMessages[agentSequence[prevStage]] || '处理完成', timestamp: new Date().toISOString() })
          currentStage = ns
          addLog({ agent: agentSequence[ns], status: 'running', message: stageStartMessages[agentSequence[ns]] || '处理中...', timestamp: new Date().toISOString() })
        }, ns * 1500)
        stageTimersRef.current.push(timer)
      }

      const finalize = (text: string) => {
        if (finalized) return
        finalized = true
        stageTimersRef.current.forEach(t => clearTimeout(t))
        stageTimersRef.current = []
        // 补全未完成阶段的完成日志
        for (let s = currentStage; s < agentSequence.length - 1; s++) {
          addLog({ agent: agentSequence[s], status: 'success', message: stageDoneMessages[agentSequence[s]] || '处理完成', timestamp: new Date().toISOString() })
        }
        addLog({ agent: '解决方案Agent', status: 'success', message: stageDoneMessages['解决方案Agent'], timestamp: new Date().toISOString() })
        setProcessing(false)
        setResult({
          riskData: { maxCVSS: dispMaxCVSS, count: dispTotal, avgRisk: 0, critical: dispCritical, highRisk: dispHigh, events: severeEvents },
          analysisText: text,
        })
      }

      const controller = new AbortController()
      abortControllerRef.current = controller

      const response = await fetch('/api/event/v1/pipeline/stream', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ query }),
        signal: controller.signal,
      })
      if (!response.ok) throw new Error(`HTTP ${response.status}`)
      const reader = response.body?.getReader()
      if (!reader) throw new Error('No reader')
      const decoder = new TextDecoder()
      let buffer = ''
      let fullText = ''

      while (true) {
        const { done, value } = await reader.read()
        if (done) break
        buffer += decoder.decode(value, { stream: true })
        const lines = buffer.split('\n')
        buffer = lines.pop() || ''
        for (const line of lines) {
          if (!line.startsWith('data: ')) continue
          const raw = line.slice(6)
          if (raw === '[DONE]') { finalize(fullText); return }
          try {
            const data = JSON.parse(raw)
            if (data.type === 'content' && data.content) {
              fullText += data.content
            } else if (data.type === 'done') {
              finalize(fullText); return
            } else if (data.type === 'error') {
              finalized = true
              stageTimersRef.current.forEach(t => clearTimeout(t))
              stageTimersRef.current = []
              addLog({ agent: agentSequence[Math.min(currentStage, agentSequence.length - 1)], status: 'error', message: data.content || '分析失败', timestamp: new Date().toISOString() })
              setProcessing(false)
              return
            }
          } catch { /* 忽略解析错误 */ }
        }
      }
      finalize(fullText)
    } catch (err) {
      stageTimersRef.current.forEach(t => clearTimeout(t))
      stageTimersRef.current = []
      if (err instanceof Error && err.name !== 'AbortError') {
        console.error('[EventAnalysis] 分析失败:', err)
      }
      setProcessing(false)
    } finally {
      abortControllerRef.current = null
    }
  }

  const stopAnalysis = () => {
    abortControllerRef.current?.abort()
    abortControllerRef.current = null
    stageTimersRef.current.forEach(t => clearTimeout(t))
    stageTimersRef.current = []
    setProcessing(false)
    terminateRunningLogs()
  }

  return (    <div className="relative flex flex-col bg-white -m-8 min-h-full">
      {/* 顶部操作栏 */}
      <div className="px-6 py-3 border-b border-gray-200 flex items-center justify-between shrink-0">
        <div className="flex items-center gap-2.5">
          <BrainCircuit className="w-5 h-5 text-primary-500" />
          <span className="text-base font-semibold text-gray-900">智能体研判</span>
          {savedAt && !isProcessing && (
            <span className="text-xs text-gray-400">
              上次分析：{new Date(savedAt).toLocaleString('zh-CN')}
            </span>
          )}
        </div>
        <div className="flex items-center gap-2">
          {riskData && !isProcessing && (
            <>
              <button
                onClick={() => { clearResult(); clearLogs() }}
                className="btn-default text-xs text-gray-500 flex items-center gap-1.5"
                title="清除当前研判结果，重新分析"
              >
                <RotateCcw className="w-3.5 h-3.5" />
                清除结果
              </button>
              <button
                onClick={() => setReportGenerating(true)}
                className="btn-primary flex items-center gap-1.5"
              >
                <FileText className="w-4 h-4" />
                生成报告
              </button>
            </>
          )}
          <AnalysisModeSelect
            value={analysisMode}
            onChange={handleModeChange}
            disabled={isProcessing}
            selectedCount={analysisMode === 'specific' ? selectedEventIds.length : undefined}
          />
          <StartButton isProcessing={isProcessing} onStart={startAnalysis} onStop={stopAnalysis} hasData={!!riskData} />
        </div>
      </div>

      {/* 数据统计横条 */}
      <div className="px-4 py-2 border-b border-gray-100 bg-gray-50 shrink-0">
        <StatsBar data={riskData} isProcessing={isProcessing} />
      </div>

      {/* 主内容区：左右两栏 */}
      <div className="flex">
        <div className="w-[960px] shrink-0 flex flex-col border-r border-gray-200">
          <div className="h-[260px] shrink-0 border-b border-gray-100">
            <AgentFlowGraph logs={agentLogs} isProcessing={isProcessing} />
          </div>
          <div className="p-3">
            <ThinkingConsole logs={agentLogs} isProcessing={isProcessing} />
          </div>
        </div>
        <div className="flex-1">
          <ResultPanel data={riskData} isProcessing={isProcessing} onSolutionUpdate={handleSolutionUpdate} />
        </div>
      </div>

      {/* 事件选择弹窗 */}
      <EventPickerModal
        visible={showEventPicker}
        onClose={() => setShowEventPicker(false)}
        onConfirm={(ids) => setSelectedEventIds(ids)}
        selectedIds={selectedEventIds}
      />

      {/* 报告预览与保存弹窗 */}
      <ReportModal
        visible={reportGenerating}
        onClose={() => setReportGenerating(false)}
        data={riskData}
        logs={agentLogs}
        analysisText={analysisText}
        onSave={handleSaveReport}
        saving={reportSaving}
      />
    </div>
  )
}
