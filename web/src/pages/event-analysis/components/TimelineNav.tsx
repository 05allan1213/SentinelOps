import { cn } from '@/utils'

interface Log { agent: string; status: string }
interface Props { logs: Log[]; selected: string | null; onSelect: (a: string) => void }

const agents = ['数据采集Agent', '提取Agent', '去重Agent', '风险评估Agent']

export default function TimelineNav({ logs, selected, onSelect }: Props) {
  const done = new Set(logs.filter(l => l.status === 'success').map(l => l.agent))
  const running = new Set(logs.filter(l => l.status === 'running').map(l => l.agent))

  return (
    <div className="w-44 border-r border-gray-200 bg-white p-3">
      <div className="text-[10px] text-gray-500 mb-3 uppercase tracking-wider">Investigation Trace</div>
      <div className="relative pl-4">
        <div className="absolute left-1 top-1 bottom-1 w-px bg-gray-100" />
        {agents.map((agent) => {
          const isDone = done.has(agent)
          const isRunning = running.has(agent) && !isDone
          return (
            <div key={agent} className="relative pb-3 cursor-pointer" onClick={() => isDone && onSelect(agent)}>
              <div className={cn(
                'absolute -left-2.5 w-2 h-2 rounded-full border',
                isDone ? 'bg-blue-600 border-[#58A6FF]' : isRunning ? 'bg-transparent border-[#58A6FF] animate-pulse' : 'bg-white border-gray-200'
              )} />
              <span className={cn('text-xs', isDone ? 'text-gray-900' : 'text-gray-500', selected === agent && 'text-blue-600')}>
                {agent.replace('Agent', '')}
              </span>
            </div>
          )
        })}
      </div>
    </div>
  )
}
