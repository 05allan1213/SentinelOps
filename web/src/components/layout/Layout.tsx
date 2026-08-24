import { Outlet } from 'react-router-dom'
import Sidebar from './Sidebar'
import { useAppStore } from '@/stores/app'
import ReportGenerateModal from '@/components/ReportGenerateModal'

export default function Layout() {
  const { sidebarCollapsed, sidebarWidth } = useAppStore()

  return (
    <div className="h-screen overflow-hidden bg-[#F3F4F6]">
      <Sidebar />
      <div
        className="flex flex-col h-full transition-[margin] duration-300"
        style={{ marginLeft: sidebarCollapsed ? '72px' : `${sidebarWidth}px` }}
      >
        <main className="flex-1 min-h-0 relative overflow-y-auto p-8">
          <Outlet />
        </main>
      </div>
      {/* 报告生成进度弹窗：fixed 定位，切换页面后持续显示 */}
      <ReportGenerateModal />
    </div>
  )
}
