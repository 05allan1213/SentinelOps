import { create } from 'zustand'
import { persist } from 'zustand/middleware'

export interface RiskEvent {
  id: number
  event_id: string
  title: string
  desc: string
  cve_id: string
  cvss: number
  severity: string
  vendor: string
  product: string
  source: string
  source_url: string
  recommendation?: string
  recommendationComplete?: boolean
}

export interface RiskData {
  maxCVSS: number
  count: number
  avgRisk: number
  critical?: number
  highRisk?: number
  events?: RiskEvent[]
}

interface AnalyzeResult {
  riskData: RiskData | null
  analysisText: string
  savedAt: string | null
}

interface AnalyzeStore extends AnalyzeResult {
  // 持久化字段操作
  setResult: (data: Omit<AnalyzeResult, 'savedAt'>) => void
  updateEventSolution: (eventId: string, content: string) => void
  clearResult: () => void
  // 报告生成 UI 状态（不持久化，页面刷新重置）
  reportGenerating: boolean
  reportSaving: boolean
  setReportGenerating: (v: boolean) => void
  setReportSaving: (v: boolean) => void
}

export const useAnalyzeStore = create<AnalyzeStore>()(
  persist(
    (set) => ({
      riskData: null,
      analysisText: '',
      savedAt: null,
      reportGenerating: false,
      reportSaving: false,

      setResult: (data) =>
        set({ ...data, savedAt: new Date().toISOString() }),

      updateEventSolution: (eventId, content) =>
        set((s) => ({
          riskData: s.riskData
            ? {
                ...s.riskData,
                events: s.riskData.events?.map((e) =>
                  e.event_id === eventId
                    ? { ...e, recommendation: content, recommendationComplete: true }
                    : e,
                ),
              }
            : null,
        })),

      clearResult: () =>
        set({ riskData: null, analysisText: '', savedAt: null }),

      setReportGenerating: (v) => set({ reportGenerating: v }),
      setReportSaving: (v) => set({ reportSaving: v }),
    }),
    {
      name: 'analyze-result-v1',
      version: 1,
      // 只持久化分析结果，不持久化 UI 状态
      partialize: (state) => ({
        riskData: state.riskData,
        analysisText: state.analysisText,
        savedAt: state.savedAt,
      }),
    },
  ),
)
