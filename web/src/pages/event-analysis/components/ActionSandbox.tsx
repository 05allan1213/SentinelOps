import { Terminal, Shield, Copy } from 'lucide-react'
import toast from 'react-hot-toast'

interface Props { selected: string | null }

export default function ActionSandbox({ selected }: Props) {
  const copy = (t: string) => { navigator.clipboard.writeText(t); toast.success('已复制') }

  return (
    <div className="w-56 border-l border-[#30363D] bg-[#0D1117] p-3">
      <div className="text-[10px] text-[#8B949E] mb-3 uppercase tracking-wider">Proposed Actions</div>

      {!selected ? (
        <div className="text-xs text-[#8B949E]">等待分析结果</div>
      ) : (
        <div className="space-y-2">
          <div className="p-2 bg-[#F85149]/10 border border-[#F85149]/30 rounded">
            <div className="flex items-center gap-1 text-[#F85149] text-xs font-medium mb-1">
              <Shield className="w-3 h-3" /> 封禁IP
            </div>
            <code className="text-[10px] text-[#8B949E] block">iptables -A INPUT -s x.x.x.x -j DROP</code>
            <div className="text-[10px] text-[#8B949E] mt-1">影响: 1节点</div>
          </div>

          <div className="p-2 bg-[#58A6FF]/10 border border-[#58A6FF]/30 rounded">
            <div className="flex items-center gap-1 text-[#58A6FF] text-xs font-medium mb-1">
              <Terminal className="w-3 h-3" /> 应用补丁
            </div>
            <code className="text-[10px] text-[#8B949E] block">apt update && apt upgrade</code>
          </div>

          <button onClick={() => copy('CVE-2024-38077')} className="w-full p-1.5 bg-[#30363D] rounded text-[10px] text-[#8B949E] flex items-center justify-center gap-1 hover:bg-[#30363D]/80">
            <Copy className="w-3 h-3" /> 复制CVE
          </button>
        </div>
      )}
    </div>
  )
}
