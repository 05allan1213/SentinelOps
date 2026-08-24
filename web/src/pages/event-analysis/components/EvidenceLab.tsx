import { useState } from 'react'
import { cn } from '@/utils'
import { AgentLog } from '@/types/agent'
import DataBlock from '@/components/common/DataBlock'

interface Props {
  logs: AgentLog[]
  selected: string | null
}

type Tab = 'overview' | 'ioc' | 'cvss'

export default function EvidenceLab({ logs, selected }: Props) {
  const [tab, setTab] = useState<Tab>('overview')

  const agentLogs = logs.filter(l => l.agent === selected)
  const completeLog = agentLogs.find(l => l.status === 'success')
  const data = completeLog?.data

  if (!selected || agentLogs.length === 0) {
    return (
      <div className="flex-1 flex items-center justify-center bg-[#010409] text-[#8B949E] text-sm">
        选择左侧节点查看详情
      </div>
    )
  }

  return (
    <div className="flex-1 flex flex-col bg-[#010409] overflow-hidden">
      {/* Tabs */}
      <div className="flex border-b border-[#30363D] px-3">
        {(['overview', 'ioc', 'cvss'] as Tab[]).map(t => (
          <button
            key={t}
            onClick={() => setTab(t)}
            className={cn(
              'px-3 py-2 text-xs border-b-2 -mb-px transition-colors',
              tab === t ? 'border-[#58A6FF] text-[#E6EDF3]' : 'border-transparent text-[#8B949E] hover:text-[#E6EDF3]'
            )}
          >
            {t === 'overview' ? '概览' : t === 'ioc' ? 'IOC提取' : 'CVSS详情'}
          </button>
        ))}
      </div>

      {/* Content */}
      <div className="flex-1 p-3 overflow-auto">
        {tab === 'overview' && <OverviewTab agent={selected} data={data} />}
        {tab === 'ioc' && <IOCTab data={data} />}
        {tab === 'cvss' && <CVSSTab data={data} />}
      </div>
    </div>
  )
}

function OverviewTab({ agent, data }: { agent: string; data?: AgentLog['data'] }) {
  if (!data) return <div className="text-[#8B949E] text-sm">暂无数据</div>

  if (agent === '数据采集Agent') {
    return (
      <div className="space-y-3">
        <div className="grid grid-cols-2 gap-2">
          <DataBlock label="采集事件数" value={String(data.count || 0)} status="medium" />
          <DataBlock label="数据源" value={(data.sources || []).join(', ')} />
        </div>
      </div>
    )
  }

  if (agent === '提取Agent' && data.severity) {
    return (
      <div className="grid grid-cols-4 gap-2">
        {Object.entries(data.severity).map(([k, v]) => (
          <DataBlock key={k} label={k} value={String(v)} status={k as 'critical' | 'high' | 'medium' | 'low'} />
        ))}
      </div>
    )
  }

  if (agent === '风险评估Agent') {
    return (
      <div className="space-y-3">
        <div className="grid grid-cols-4 gap-2">
          <DataBlock label="最高CVSS" value={data.maxCVSS?.toFixed(1) || '-'} status="critical" />
          <DataBlock label="严重" value={String(data.critical || 0)} status="critical" />
          <DataBlock label="高危" value={String(data.highRisk || 0)} status="high" />
          <DataBlock label="平均分" value={String(data.avgRisk || 0)} />
        </div>
        {data.events && data.events.length > 0 && (
          <div className="mt-4">
            <div className="text-xs text-[#8B949E] mb-2">高危事件列表</div>
            <div className="space-y-2 max-h-[300px] overflow-auto">
              {data.events.slice(0, 5).map(e => (
                <div key={e.id} className="p-2 bg-[#0D1117] border border-[#30363D] rounded">
                  <div className="flex items-center gap-2 mb-1">
                    <span className="text-xs font-mono text-[#58A6FF]">{e.cve_id}</span>
                    <span className={cn('text-[10px] px-1 rounded', e.severity === 'critical' ? 'bg-[#F85149]/20 text-[#F85149]' : 'bg-[#F0883E]/20 text-[#F0883E]')}>
                      CVSS {e.cvss}
                    </span>
                  </div>
                  <div className="text-xs text-[#E6EDF3] truncate">{e.title}</div>
                </div>
              ))}
            </div>
          </div>
        )}
      </div>
    )
  }

  return <div className="text-[#8B949E] text-sm">暂无数据</div>
}

function IOCTab({ data }: { data?: AgentLog['data'] }) {
  if (!data?.events) return <div className="text-[#8B949E] text-sm">暂无IOC数据</div>

  const iocs = data.events.flatMap(e => [
    { type: 'CVE', value: e.cve_id, source: e.vendor },
  ]).filter(i => i.value)

  return (
    <div className="space-y-2">
      {iocs.map((ioc, i) => (
        <DataBlock key={i} label={ioc.type} value={ioc.value} onSearch={() => window.open(`https://nvd.nist.gov/vuln/detail/${ioc.value}`, '_blank')} />
      ))}
    </div>
  )
}

function CVSSTab({ data }: { data?: AgentLog['data'] }) {
  if (!data?.events?.[0]) return <div className="text-[#8B949E] text-sm">暂无CVSS数据</div>

  const e = data.events[0]
  return (
    <div className="space-y-3">
      <div className="text-center p-4 bg-[#0D1117] border border-[#30363D] rounded">
        <div className="text-4xl font-mono font-bold text-[#F85149]">{e.cvss}</div>
        <div className="text-xs text-[#8B949E] mt-1">CVSS 3.1 评分</div>
      </div>
      <div className="grid grid-cols-2 gap-2">
        <DataBlock label="厂商" value={e.vendor || '-'} />
        <DataBlock label="产品" value={e.product || '-'} />
      </div>
    </div>
  )
}
