import { useState } from 'react'
import { createRoot } from 'react-dom/client'
import EventDetailModal from '../../../src/pages/events/components/EventDetailModal'
import ChunkDrawer from '../../../src/pages/knowledge/components/ChunkDrawer'
import TraceDetailModal from '../../../src/pages/rag-eval/components/TraceDetailModal'
import EventPickerModal from '../../../src/pages/event-analysis/components/EventPickerModal'
import ReportModal from '../../../src/pages/event-analysis/components/ReportModal'
import ReportGenerateModal from '../../../src/components/ReportGenerateModal'
import TermMappingPage from '../../../src/pages/term-mapping'
import { useAnalyzeStore } from '../../../src/stores/analyzeStore'
import '../../../src/assets/styles/index.css'

// Isolated test inputs, no product route, no network fallback or successful API response.
const event = { id: 'test-event', subscription_id: 1, title: '事件详情测试', severity: 'high' as const, status: 'new' as const, source: 'test', source_url: '', event_time: '', created_at: '', description: 'long test input '.repeat(200) }
const pendingRisk = { count: 1, maxCVSS: 1, avgRisk: 1, events: [{ id: 1, event_id: 'test-event', title: 'pending test input', desc: '', cve_id: '', cvss: 1, severity: 'low', vendor: '', product: '', source: 'test', source_url: '', recommendationComplete: false }] }
export function Fixture() {
  const [open, setOpen] = useState('')
  const close = () => setOpen('')
  return <main>
    {['事件详情测试', '文档分块详情', '链路详情', '选择要分析的事件', '安全事件分析报告', '术语规则'].map(name => <button key={name} onClick={() => setOpen(name)}>{name}</button>)}
    <button onClick={() => useAnalyzeStore.setState({ riskData: pendingRisk, reportGenerating: true })}>打开生成进度</button>
    {open === '事件详情测试' && <EventDetailModal event={event} onClose={close} />}
    {open === '文档分块详情' && <ChunkDrawer docID="test-doc" docName={'long document name '.repeat(30)} onClose={close} />}
    {open === '链路详情' && <TraceDetailModal detail={null} loading onClose={close} />}
    {open === '选择要分析的事件' && <EventPickerModal visible onClose={close} onConfirm={close} selectedIds={[]} />}
    {open === '安全事件分析报告' && <ReportModal visible data={pendingRisk} logs={[]} analysisText="" onClose={close} />}
    {open === '术语规则' && <TermMappingPage />}
    <ReportGenerateModal />
  </main>
}
createRoot(document.getElementById('root')!).render(<Fixture />)
