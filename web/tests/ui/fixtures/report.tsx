import { useState, type ComponentProps } from 'react'
import { createRoot } from 'react-dom/client'
import ReportDetailModal from '../../../src/pages/reports/components/ReportDetailModal'
import '../../../src/assets/styles/index.css'

type Report = NonNullable<ComponentProps<typeof ReportDetailModal>['report']>
declare global { interface Window { renderReportFixture: (report: Report) => void } }
export function Fixture({ report }: { report: Report }) {
  const [open, setOpen] = useState(false)
  return <main>
    <button onClick={() => setOpen(true)}>打开报告测试</button>
    <ReportDetailModal report={open ? report : null} onClose={() => setOpen(false)} />
  </main>
}
const root = createRoot(document.getElementById('root')!)
// Only test-supplied data; this harness has no product route or API fallback.
window.renderReportFixture = report => root.render(<Fixture report={report} />)
