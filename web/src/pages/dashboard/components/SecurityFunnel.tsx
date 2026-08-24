interface Props {
  collected: number
  todayCount: number
  critical: number
}

export default function SecurityFunnel({ collected, todayCount, critical }: Props) {
  const stages = [
    { label: '全部事件', value: collected, color: '#3b82f6' },
    { label: '今日新增', value: todayCount, color: '#8b5cf6' },
    { label: '高危事件', value: critical, color: '#ef4444' },
  ]

  return (
    <div className="card card-body">
      <div className="text-sm font-semibold text-gray-900 mb-4">安全事件漏斗</div>
      <div className="flex items-center justify-between">
        {stages.map((stage, i) => (
          <div key={stage.label} className="flex items-center">
            <div className="text-center">
              <div className="text-3xl font-mono font-bold" style={{ color: stage.color }}>
                {stage.value.toLocaleString()}
              </div>
              <div className="text-xs text-gray-500 mt-1">{stage.label}</div>
            </div>
            {i < stages.length - 1 && (
              <div className="mx-6 flex items-center">
                <div className="w-12 h-px bg-gray-200" />
                <div className="w-0 h-0 border-t-4 border-b-4 border-l-6 border-transparent border-l-gray-300" />
              </div>
            )}
          </div>
        ))}
    </div>
    </div>
  )
}

