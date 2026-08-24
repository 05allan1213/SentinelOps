import { ShieldAlert, Server, Terminal } from 'lucide-react'

interface Log {
  agent: string
  status: string
  message: string
  data?: Record<string, unknown>
}

interface Props {
  selectedNode: string | null
  logs: Log[]
}

export default function IntelligenceSidebar({ selectedNode, logs }: Props) {
  if (!selectedNode) {
    return (
      <div className="w-80 bg-gray-900 rounded-lg border border-gray-700 p-4 flex items-center justify-center text-gray-500 text-sm">
        点击左侧节点查看详情
      </div>
    )
  }

  const nodeLogs = logs.filter(l => l.agent === selectedNode)

  return (
    <div className="w-80 bg-gray-900 rounded-lg border border-gray-700 overflow-hidden flex flex-col">
      <div className="p-3 border-b border-gray-700 font-medium text-white">{selectedNode}</div>

      <div className="flex-1 overflow-auto p-3 space-y-4">
        {/* 证据链 */}
        <div>
          <h4 className="text-xs text-gray-500 mb-2 flex items-center gap-1">
            <ShieldAlert className="w-3 h-3" /> 证据链
          </h4>
          <div className="bg-gray-800 rounded p-2 text-xs text-gray-300 space-y-1">
            {nodeLogs.map((log, i) => (
              <p key={i}>{log.message}</p>
            ))}
          </div>
        </div>

        {/* 资产快照 */}
        <div>
          <h4 className="text-xs text-gray-500 mb-2 flex items-center gap-1">
            <Server className="w-3 h-3" /> 关联资产
          </h4>
          <div className="bg-gray-800 rounded p-2 text-xs text-gray-400">
            暂无关联资产
          </div>
        </div>

        {/* 处置建议 */}
        <div>
          <h4 className="text-xs text-gray-500 mb-2 flex items-center gap-1">
            <Terminal className="w-3 h-3" /> 处置建议
          </h4>
          <div className="space-y-2">
            <button className="w-full text-left px-3 py-2 bg-red-500/10 border border-red-500/30 rounded text-xs text-red-400 hover:bg-red-500/20">
              阻止攻击者IP
            </button>
            <button className="w-full text-left px-3 py-2 bg-blue-500/10 border border-blue-500/30 rounded text-xs text-blue-400 hover:bg-blue-500/20">
              更新补丁包
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
