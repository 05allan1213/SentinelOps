import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import Layout from './components/layout/Layout'
import Dashboard from './pages/dashboard'
import Subscriptions from './pages/subscriptions'
import Events from './pages/events'
import EventAnalysis from './pages/event-analysis'
import Reports from './pages/reports'
import Chat from './pages/chat'
import Settings from './pages/settings'
import TermMapping from './pages/term-mapping'
import Traces from './pages/traces'
import TraceDetail from './pages/traces/detail'
import Knowledge from './pages/knowledge'
import RagEval from './pages/rag-eval'
import Ingest from './pages/ingest'
import Ops from './pages/ops'
import Login from './pages/login'
import { useAuthStore } from './stores/authStore'

function RequireAuth({ children }: { children: React.ReactNode }) {
  const token = useAuthStore((s) => s.token) ?? localStorage.getItem('token')
  return token ? <>{children}</> : <Navigate to="/login" replace />
}

function App() {
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
          {/* /cost-monitor 重定向到 /traces?tab=overview */}
          <Route path="cost-monitor" element={<Navigate to="/traces?tab=overview" replace />} />
        </Route>
      </Routes>
    </BrowserRouter>
  )
}

export default App
