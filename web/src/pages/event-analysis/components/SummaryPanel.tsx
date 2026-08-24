interface Props {
  total: number
  processed: number
}

export default function SummaryPanel({ total, processed }: Props) {
  if (total === 0) return null

  return (
    <div className="bg-green-900/30 border border-green-700 rounded-lg p-4 mt-4">
      <div className="text-green-400 font-semibold">处理完成</div>
      <div className="text-sm text-gray-300 mt-1">
        共处理 {total} 个事件，更新 {processed} 个风险评分
      </div>
    </div>
  )
}
