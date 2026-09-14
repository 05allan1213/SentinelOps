import { lazy, useLayoutEffect } from 'react'
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import Layout from './components/layout/Layout'
const Dashboard = lazy(() => import('./pages/dashboard'))
const Subscriptions = lazy(() => import('./pages/subscriptions'))
const Events = lazy(() => import('./pages/events'))
const EventAnalysis = lazy(() => import('./pages/event-analysis'))
const Reports = lazy(() => import('./pages/reports'))
const Chat = lazy(() => import('./pages/chat'))
const Settings = lazy(() => import('./pages/settings'))
const TermMapping = lazy(() => import('./pages/term-mapping'))
const Traces = lazy(() => import('./pages/traces'))
const TraceDetail = lazy(() => import('./pages/traces/detail'))
const Knowledge = lazy(() => import('./pages/knowledge'))
const RagEval = lazy(() => import('./pages/rag-eval'))
const Ingest = lazy(() => import('./pages/ingest'))
const Ops = lazy(() => import('./pages/ops'))
const RuntimeRuns = lazy(() => import('./pages/runtime'))
const RuntimeRunDetail = lazy(() => import('./pages/runtime/detail'))
const RuntimeCapabilities = lazy(() => import('./pages/runtime/capabilities'))
const RuntimeSafety = lazy(() => import('./pages/runtime/safety'))
const RuntimeWorkerHealth = lazy(() => import('./pages/runtime/worker-health'))
import Login from './pages/login'
import { useAuthStore } from './stores/authStore'

function RequireAuth({ children }: { children: React.ReactNode }) {
  const token = useAuthStore((s) => s.token) ?? localStorage.getItem('token')
  return token ? <>{children}</> : <Navigate to="/login" replace />
}

function App() {
  useLayoutEffect(() => { document.documentElement.classList.remove('dark') }, [])
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route path="/" element={<RequireAuth><Layout /></RequireAuth>}>
          <Route index element={<Navigate to="/dashboard" replace />} />
          <Route path="dashboard" element={<Dashboard />} />
          <Route path="subscriptions" element={<Subscriptions />} />
          <Route path="events" element={<Events />} />
          <Route path="events/analysis" element={<EventAnalysis />} />
          <Route path="reports" element={<Reports />} />
          <Route path="chat" element={<Chat />} />
          <Route path="settings" element={<Settings />} />
          <Route path="term-mapping" element={<TermMapping />} />
          <Route path="traces" element={<Traces />} />
          <Route path="traces/:traceId" element={<TraceDetail />} />
          <Route path="knowledge" element={<Knowledge />} />
          {/* 旧子路由重定向到统一知识库页面 */}
          <Route path="knowledge/:baseId/docs" element={<Navigate to="/knowledge?tab=bases" replace />} />
          <Route path="knowledge/:baseId/docs/:docId/chunks" element={<Navigate to="/knowledge?tab=bases" replace />} />
          <Route path="rag-eval" element={<RagEval />} />
          <Route path="ingest" element={<Ingest />} />
          <Route path="ops" element={<Ops />} />
          <Route path="runtime/runs" element={<RuntimeRuns />} />
          <Route path="runtime/runs/:runId" element={<RuntimeRunDetail />} />
          <Route path="runtime/capabilities" element={<RuntimeCapabilities />} />
          <Route path="runtime/safety" element={<RuntimeSafety />} />
          <Route path="runtime/worker-health" element={<RuntimeWorkerHealth />} />
          {/* /cost-monitor 重定向到 /traces?tab=overview */}
          <Route path="cost-monitor" element={<Navigate to="/traces?tab=overview" replace />} />
        </Route>
      </Routes>
    </BrowserRouter>
  )
}

export default App
