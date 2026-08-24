import { useState } from 'react'
import { FileText, Shield, ChevronRight, CheckCircle } from 'lucide-react'

interface Props {
  criticalCount: number
  pendingReports: number
}

function getStored(key: string): number {
  return parseInt(localStorage.getItem(key) || '0', 10)
}

export default function ActionQueue({ pendingReports }: Props) {
  const [dismissedReports, setDismissedReports] = useState(() => getStored('dismissed_report_count'))

  const showReports = pendingReports > dismissedReports

  const handleReportClick = () => {
    localStorage.setItem('dismissed_report_count', String(pendingReports))
    setDismissedReports(pendingReports)
  }

  return (
    <div className="card">
      <div className="card-header flex items-center gap-2">
        <Shield className="w-4 h-4 text-primary-500" />
        待办任务
        <span className="ml-auto text-xs font-normal text-gray-400">
          {showReports ? '1 项' : '全部完成'}
        </span>
      </div>
      {!showReports ? (
        <div className="flex items-center gap-3 px-5 py-4 text-sm text-gray-500">
          <CheckCircle className="w-4 h-4 text-success-500 flex-shrink-0" />
          暂无待办事项，系统运行正常
        </div>
      ) : (
        <div className="divide-y divide-gray-100">
          <a
            href="/reports"
            onClick={handleReportClick}
            className="flex items-center gap-3 p-4 hover:bg-gray-50 transition-colors"
          >
            <div className="w-8 h-8 rounded-lg flex items-center justify-center flex-shrink-0 bg-amber-100">
              <FileText className="w-4 h-4 text-amber-600" />
            </div>
            <div className="flex-1 min-w-0">
              <p className="text-sm font-medium text-gray-900 truncate">{pendingReports} 份报告待审阅</p>
              <p className="text-xs text-gray-400">点击查看后将从待办中移除</p>
            </div>
            <span className="text-xs font-medium px-3 py-1.5 rounded whitespace-nowrap flex-shrink-0 bg-amber-100 text-amber-700">
              查看报告
            </span>
            <ChevronRight className="w-4 h-4 text-gray-400 flex-shrink-0" />
          </a>
        </div>
      )}
    </div>
  )
}


