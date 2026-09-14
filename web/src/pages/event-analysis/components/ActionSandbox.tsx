import { Terminal, Shield, Copy, Eye } from 'lucide-react'
import toast from 'react-hot-toast'

interface Props { selected: string | null }

export default function ActionSandbox({ selected }: Props) {
  const copy = (t: string) => { navigator.clipboard.writeText(t); toast.success('已复制') }

  return (
    <div className="w-56 border-l border-gray-200 bg-white p-3">
      <div className="flex items-center gap-1.5 text-[10px] text-gray-500 mb-3 uppercase tracking-wider"><Eye className="w-3 h-3" /> Proposal Preview</div>

      {!selected ? (
        <div className="text-xs text-gray-500">等待分析结果；此处仅预览，不执行 Effect。</div>
      ) : (
        <div className="space-y-2">
          <div className="p-2 bg-red-600/10 border border-[#F85149]/30 rounded">
            <div className="flex items-center gap-1 text-red-600 text-xs font-medium mb-1">
              <Shield className="w-3 h-3" /> 封禁IP
            </div>
            <code className="text-[10px] text-gray-500 block">iptables -A INPUT -s x.x.x.x -j DROP</code>
            <div className="text-[10px] text-gray-500 mt-1">影响: 1节点</div>
          </div>

          <div className="p-2 bg-blue-600/10 border border-[#58A6FF]/30 rounded">
            <div className="flex items-center gap-1 text-blue-600 text-xs font-medium mb-1">
              <Terminal className="w-3 h-3" /> 应用补丁
            </div>
            <code className="text-[10px] text-gray-500 block">apt update && apt upgrade</code>
          </div>

          <button onClick={() => copy('CVE-2024-38077')} className="w-full p-1.5 bg-gray-100 rounded text-[10px] text-gray-500 flex items-center justify-center gap-1 hover:bg-gray-100/80">
            <Copy className="w-3 h-3" /> 复制CVE
          </button>
          <p className="text-[10px] text-gray-500">执行需要后端 Approval、Effect 结果和审计证据。</p>
        </div>
      )}
    </div>
  )
}
