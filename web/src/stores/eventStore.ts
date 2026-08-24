import { create } from 'zustand'

interface AgentLog {
  agent: string
  status: 'running' | 'success' | 'error'
  message: string
  timestamp: string
  data?: Record<string, unknown>
}

interface EventStore {
  agentLogs: AgentLog[]
  isProcessing: boolean
  addLog: (log: AgentLog) => void
  clearLogs: () => void
  setProcessing: (v: boolean) => void
  // 将所有 running 状态的日志标记为终止（用于用户手动停止分析）
  terminateRunningLogs: (message?: string) => void
}

export const useEventStore = create<EventStore>((set) => ({
  agentLogs: [],
  isProcessing: false,
  addLog: (log) => set((s) => ({ agentLogs: [...s.agentLogs, log] })),
  clearLogs: () => set({ agentLogs: [] }),
  setProcessing: (v) => set({ isProcessing: v }),
  terminateRunningLogs: (message = '已被用户终止') =>
    set((s) => ({
      agentLogs: s.agentLogs.map((log) =>
        log.status === 'running'
          ? { ...log, status: 'error' as const, message: `${log.message}（${message}）` }
          : log
      ),
    })),
}))
