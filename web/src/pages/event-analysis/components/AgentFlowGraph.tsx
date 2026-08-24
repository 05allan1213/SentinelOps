import { useEffect, useRef, useState } from 'react'
import { cn } from '@/utils'
import { Database, Filter, Shield, Cpu, CheckCircle, Lightbulb } from 'lucide-react'
import { AgentLog } from '@/types/agent'

interface Props {
  logs: AgentLog[]
  isProcessing: boolean
}

const agents = [
  { id: '数据采集Agent', icon: Database,  color: '#3b82f6', x: 8,  y: 52, label: '数据采集' },
  { id: '提取Agent',    icon: Filter,     color: '#8b5cf6', x: 26, y: 26, label: '智能提取' },
  { id: '去重Agent',    icon: Cpu,        color: '#10b981', x: 50, y: 52, label: '去重过滤' },
  { id: '风险评估Agent', icon: Shield,    color: '#ef4444', x: 74, y: 26, label: '风险评估' },
  { id: '解决方案Agent', icon: Lightbulb, color: '#f59e0b', x: 92, y: 52, label: '解决方案' },
]

const connections = [
  { from: 0, to: 1 },
  { from: 1, to: 2 },
  { from: 2, to: 3 },
  { from: 3, to: 4 },
]

function getBezier(from: { x: number; y: number }, to: { x: number; y: number }, w: number, h: number) {
  const x1 = (from.x / 100) * w
  const y1 = (from.y / 100) * h
  const x2 = (to.x / 100) * w
  const y2 = (to.y / 100) * h
  const mx = (x1 + x2) / 2
  const cy = Math.min(y1, y2) - 28
  return { x1, y1, x2, y2, cx1: mx, cy1: cy, cx2: mx, cy2: cy }
}

function bezierPoint(t: number, p: ReturnType<typeof getBezier>) {
  const u = 1 - t
  const x = u * u * u * p.x1 + 3 * u * u * t * p.cx1 + 3 * u * t * t * p.cx2 + t * t * t * p.x2
  const y = u * u * u * p.y1 + 3 * u * u * t * p.cy1 + 3 * u * t * t * p.cy2 + t * t * t * p.y2
  return { x, y }
}

function hexToRgba(hex: string, alpha: number) {
  const r = parseInt(hex.slice(1, 3), 16)
  const g = parseInt(hex.slice(3, 5), 16)
  const b = parseInt(hex.slice(5, 7), 16)
  return `rgba(${r},${g},${b},${alpha})`
}

function drawArrowHead(
  ctx: CanvasRenderingContext2D,
  p: ReturnType<typeof getBezier>,
  t: number, color: string, size: number
) {
  const dt = 0.01
  const p1 = bezierPoint(Math.max(0, t - dt), p)
  const p2 = bezierPoint(Math.min(1, t + dt), p)
  const angle = Math.atan2(p2.y - p1.y, p2.x - p1.x)
  const pt = bezierPoint(t, p)
  ctx.save()
  ctx.translate(pt.x, pt.y)
  ctx.rotate(angle)
  ctx.beginPath()
  ctx.moveTo(size, 0)
  ctx.lineTo(-size, -size * 0.6)
  ctx.lineTo(-size, size * 0.6)
  ctx.closePath()
  ctx.fillStyle = color
  ctx.fill()
  ctx.restore()
}

export default function AgentFlowGraph({ logs, isProcessing }: Props) {
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const [elapsed, setElapsed] = useState(0)

  const getStatus = (id: string) => {
    const agentLogs = logs.filter(l => l.agent === id)
    if (agentLogs.some(l => l.status === 'success')) return 'completed'
    if (agentLogs.some(l => l.status === 'running')) return 'running'
    return 'pending'
  }

  useEffect(() => {
    if (!isProcessing) return
    const t = setInterval(() => setElapsed(e => e + 1), 1000)
    return () => clearInterval(t)
  }, [isProcessing])

  useEffect(() => {
    if (!isProcessing && logs.length === 0) setElapsed(0)
  }, [isProcessing, logs.length])

  useEffect(() => {
    const canvas = canvasRef.current
    if (!canvas) return
    const ctx = canvas.getContext('2d')
    if (!ctx) return

    const inactiveLineColor = 'rgba(148,163,184,0.4)'

    const resize = () => {
      canvas.width = canvas.offsetWidth * 2
      canvas.height = canvas.offsetHeight * 2
      ctx.scale(2, 2)
    }
    resize()

    const particles: { progress: number; conn: number; speed: number; size: number }[] = []
    let animationId: number
    let tick = 0

    const animate = () => {
      ctx.clearRect(0, 0, canvas.width, canvas.height)
      const w = canvas.offsetWidth
      const h = canvas.offsetHeight
      tick++

      // 绘制贝塞尔连接线
      connections.forEach((conn) => {
        const from = agents[conn.from]
        const to = agents[conn.to]
        const bp = getBezier(from, to, w, h)
        const fromStatus = getStatus(from.id)
        const toStatus = getStatus(to.id)
        const isActive = fromStatus === 'completed' || fromStatus === 'running'

        // 底层虚线
        ctx.beginPath()
        ctx.setLineDash([6, 5])
        ctx.moveTo(bp.x1, bp.y1)
        ctx.bezierCurveTo(bp.cx1, bp.cy1, bp.cx2, bp.cy2, bp.x2, bp.y2)
        ctx.strokeStyle = isActive ? hexToRgba(from.color, 0.25) : inactiveLineColor
        ctx.lineWidth = 2
        ctx.stroke()
        ctx.setLineDash([])

        // 激活时的流动虚线
        if (isActive) {
          ctx.beginPath()
          ctx.setLineDash([10, 8])
          ctx.lineDashOffset = -tick * 0.7
          ctx.moveTo(bp.x1, bp.y1)
          ctx.bezierCurveTo(bp.cx1, bp.cy1, bp.cx2, bp.cy2, bp.x2, bp.y2)
          const grad = ctx.createLinearGradient(bp.x1, bp.y1, bp.x2, bp.y2)
          grad.addColorStop(0, hexToRgba(from.color, 0.7))
          grad.addColorStop(1, hexToRgba(to.color, 0.5))
          ctx.strokeStyle = grad
          ctx.lineWidth = 2.5
          ctx.stroke()
          ctx.setLineDash([])
          ctx.lineDashOffset = 0

          // 中点方向箭头
          drawArrowHead(ctx, bp, 0.5, hexToRgba(from.color, 0.7), 5)
          if (toStatus === 'running' || toStatus === 'completed') {
            drawArrowHead(ctx, bp, 0.75, hexToRgba(to.color, 0.5), 4)
          }
        }
      })

      // 生成粒子
      if (isProcessing && tick % 4 === 0) {
        connections.forEach((conn, idx) => {
          const status = getStatus(agents[conn.from].id)
          if (status === 'running' || status === 'completed') {
            if (Math.random() > 0.6) {
              particles.push({ progress: 0, conn: idx, speed: 0.007 + Math.random() * 0.007, size: 2 + Math.random() * 1.5 })
            }
          }
        })
      }

      // 更新和绘制粒子
      for (let i = particles.length - 1; i >= 0; i--) {
        const p = particles[i]
        p.progress += p.speed
        if (p.progress >= 1) { particles.splice(i, 1); continue }

        const conn = connections[p.conn]
        const from = agents[conn.from]
        const bp = getBezier(from, agents[conn.to], w, h)
        const pt = bezierPoint(p.progress, bp)

        // 粒子拖尾
        for (let t = 1; t <= 4; t++) {
          const tp = Math.max(0, p.progress - t * 0.02)
          const tpt = bezierPoint(tp, bp)
          ctx.fillStyle = hexToRgba(from.color, (1 - t / 4) * 0.25)
          ctx.beginPath()
          ctx.arc(tpt.x, tpt.y, p.size * (1 - t / 4 * 0.5), 0, Math.PI * 2)
          ctx.fill()
        }

        // 粒子核心
        ctx.fillStyle = hexToRgba(from.color, 0.9)
        ctx.beginPath()
        ctx.arc(pt.x, pt.y, p.size, 0, Math.PI * 2)
        ctx.fill()
      }

      animationId = requestAnimationFrame(animate)
    }

    animate()
    window.addEventListener('resize', resize)
    return () => {
      cancelAnimationFrame(animationId)
      window.removeEventListener('resize', resize)
    }
  }, [logs, isProcessing])

  const formatTime = (s: number) => `${Math.floor(s / 60).toString().padStart(2, '0')}:${(s % 60).toString().padStart(2, '0')}`

  return (
    <div className="relative w-full h-full min-h-[360px] overflow-visible">
      {/* 粒子画布 */}
      <canvas ref={canvasRef} className="absolute inset-0 w-full h-full" />

      {/* Agent 节点 */}
      {agents.map((agent) => {
        const status = getStatus(agent.id)
        const Icon = agent.icon
        return (
          <div
            key={agent.id}
            className="absolute transform -translate-x-1/2 -translate-y-1/2 flex flex-col items-center"
            style={{ left: `${agent.x}%`, top: `${agent.y}%` }}
          >
            {/* 节点主体 */}
            <div
              className={cn(
                'relative rounded-2xl flex items-center justify-center transition-all duration-300 border-2 shadow-sm',
                status === 'running' && 'scale-110 shadow-md',
              )}
              style={{
                width: '72px',
                height: '72px',
                borderColor: status !== 'pending' ? agent.color : hexToRgba(agent.color, 0.35),
                backgroundColor: status === 'running'
                  ? hexToRgba(agent.color, 0.08)
                  : status === 'completed'
                  ? hexToRgba(agent.color, 0.06)
                  : hexToRgba(agent.color, 0.05),
              }}
            >
              {status === 'completed' ? (
                <CheckCircle className="w-7 h-7 relative z-10" style={{ color: agent.color }} />
              ) : (
                <Icon
                  className="w-7 h-7 relative z-10 transition-colors"
                  style={{ color: status === 'running' ? agent.color : hexToRgba(agent.color, 0.6) }}
                />
              )}

              {/* 运行中脉冲环 */}
              {status === 'running' && (
                <div
                  className="absolute inset-0 rounded-2xl animate-ping opacity-20"
                  style={{ backgroundColor: agent.color }}
                />
              )}
            </div>

            {/* 标签 */}
            <div className="mt-3 text-center">
              <div className={cn(
                'text-sm font-medium transition-colors',
                status === 'pending' ? 'text-gray-500' : 'text-gray-800'
              )}>
                {agent.label}
              </div>
              {status === 'running' && (
                <div className="text-xs mt-1 text-gray-400">处理中...</div>
              )}
            </div>
          </div>
        )
      })}

      {/* 底部状态条 */}
      {(isProcessing || logs.length > 0) && (
        <div className="absolute left-1/2 bottom-4 transform -translate-x-1/2 flex items-center gap-2">
          {isProcessing ? (
            <div className="flex items-center gap-2 px-3 py-1.5 rounded-full bg-primary-50 border border-primary-200">
              <span className="w-2 h-2 rounded-full bg-primary-500 animate-pulse" />
              <span className="text-xs text-primary-600 font-medium">
                分析中 · {formatTime(elapsed)}
              </span>
            </div>
          ) : (
            <div className="flex items-center gap-2 px-3 py-1.5 rounded-full bg-success-50 border border-success-200">
              <CheckCircle className="w-3.5 h-3.5 text-success-500" />
              <span className="text-xs text-success-600 font-medium">
                分析完成 · {formatTime(elapsed)}
              </span>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
