import { RiskDimensions } from '@/types/agent'

interface Props {
  data: RiskDimensions
}

const labels = ['攻击成本', '影响深度', '修复难度', '探测频率', '受众范围']

export default function RiskRadar({ data }: Props) {
  const values = [data.attack_cost, data.impact_depth, data.fix_difficulty, data.detect_frequency, data.audience_scope]
  const cx = 80, cy = 80, r = 60
  const angleStep = (Math.PI * 2) / 5

  const getPoint = (i: number, scale: number) => ({
    x: cx + Math.sin(i * angleStep) * r * scale,
    y: cy - Math.cos(i * angleStep) * r * scale
  })

  const dataPoints = values.map((v, i) => getPoint(i, v / 10))
  const pathD = dataPoints.map((p, i) => `${i === 0 ? 'M' : 'L'} ${p.x} ${p.y}`).join(' ') + ' Z'

  return (
    <svg width={160} height={160} className="mx-auto">
      {[0.2, 0.4, 0.6, 0.8, 1].map(scale => (
        <polygon key={scale} points={[0,1,2,3,4].map(i => {
          const p = getPoint(i, scale)
          return `${p.x},${p.y}`
        }).join(' ')} fill="none" stroke="#30363D" strokeWidth="1" />
      ))}
      {[0,1,2,3,4].map(i => {
        const p = getPoint(i, 1)
        return <line key={i} x1={cx} y1={cy} x2={p.x} y2={p.y} stroke="#30363D" strokeWidth="1" />
      })}
      <polygon points={pathD.replace(/[MLZ]/g, '').trim().replace(/ +/g, ',')} fill="#F85149" fillOpacity="0.3" stroke="#F85149" strokeWidth="2" />
      {dataPoints.map((p, i) => (
        <g key={i}>
          <circle cx={p.x} cy={p.y} r="3" fill="#F85149" />
          <text x={getPoint(i, 1.2).x} y={getPoint(i, 1.2).y} fill="#8B949E" fontSize="9" textAnchor="middle" dominantBaseline="middle">{labels[i]}</text>
        </g>
      ))}
    </svg>
  )
}
