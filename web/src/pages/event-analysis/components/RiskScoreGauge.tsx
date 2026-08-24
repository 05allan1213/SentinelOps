interface Props {
  score: number
}

export default function RiskScoreGauge({ score }: Props) {
  const color = score >= 80 ? 'text-red-500' : score >= 60 ? 'text-orange-500' : score >= 40 ? 'text-yellow-500' : 'text-green-500'

  return (
    <div className="bg-gray-800 rounded-lg p-4 text-center">
      <div className="text-sm text-gray-400 mb-2">风险评分</div>
      <div className={`text-4xl font-bold ${color}`}>{score}</div>
      <div className="text-xs text-gray-500 mt-1">/ 100</div>
    </div>
  )
}
