import Button from '@/components/common/Button'
import { Component, Suspense, type ReactNode } from 'react'

export class RouteErrorBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false }
  static getDerivedStateFromError() { return { failed: true } }
  render() {
    if (this.state.failed) return (
      <section role="alert" className="rounded-lg border border-red-200 bg-red-50 p-5 text-red-900">
        <h1 className="text-lg font-semibold">页面暂时无法显示</h1>
        <p className="mt-2 text-sm">页面加载失败。请刷新重试，或通过侧边导航查看其他页面。</p>
        <Button onClick={() => window.location.reload()} className="mt-4">刷新重试</Button>
      </section>
    )
    return this.props.children
  }
}

export default function RouteContent({ children }: { children: ReactNode }) {
  return <RouteErrorBoundary><Suspense fallback={<p role="status" className="p-5 text-sm text-gray-600">正在加载页面…</p>}>{children}</Suspense></RouteErrorBoundary>
}
