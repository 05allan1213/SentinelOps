import RouteContent from './RouteContent'
import { Outlet, useLocation } from 'react-router-dom'
import Sidebar from './Sidebar'
import { useAppStore } from '@/stores/app'
import ReportGenerateModal from '@/components/ReportGenerateModal'

export default function Layout() {
  const sidebarCollapsed = useAppStore(s => s.sidebarCollapsed)
  const sidebarWidth = useAppStore(s => s.sidebarWidth)
  const location = useLocation()

  return (
    <div className="h-screen overflow-hidden bg-[#F5F5F7]">
      <a href="#main-content" className="sr-only focus:not-sr-only focus:fixed focus:left-4 focus:top-4 focus:z-[100] focus:rounded focus:bg-white focus:px-4 focus:py-2 focus:text-indigo-800">跳到主要内容</a>
      <Sidebar />
      <div
        className="flex flex-col h-full transition-[margin] duration-200 motion-reduce:transition-none"
        style={{ marginLeft: sidebarCollapsed ? '72px' : `${sidebarWidth}px` }}
      >
        <main id="main-content" tabIndex={-1} data-surface={location.pathname.startsWith('/runtime/') ? 'runtime' : location.pathname === '/chat' ? 'chat' : undefined} className="flex-1 min-w-0 min-h-0 relative overflow-y-auto p-8">
          <div className="mx-auto min-h-full w-full max-w-[1440px]">
            <RouteContent key={location.pathname}><Outlet /></RouteContent>
          </div>
        </main>
      </div>
      {/* 报告生成进度弹窗：fixed 定位，切换页面后持续显示 */}
      <ReportGenerateModal />
    </div>
  )
}
