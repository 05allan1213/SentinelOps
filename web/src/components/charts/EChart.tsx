// 统一图表入口：按需注册 ECharts 模块，避免把整包 echarts 打进构建产物。
// 现有图表只使用 line / bar / pie 三类 series，以及 grid、tooltip、legend、
// graphic 组件与 canvas/svg 渲染器；新增图表类型时必须在此登记。
//
// 这里直接使用 echarts 官方 API（不经过 echarts-for-react 的 CJS core 入口），
// 保持与原封装相同的可见行为：容器尺寸由调用方给定、option 变化时重新
// setOption、容器尺寸变化时 resize、卸载时 dispose。
import { useEffect, useRef, type CSSProperties } from 'react'
import * as echarts from 'echarts/core'
import { BarChart, LineChart, PieChart } from 'echarts/charts'
import {
  AxisPointerComponent,
  GraphicComponent,
  GridComponent,
  LegendComponent,
  TooltipComponent,
} from 'echarts/components'
import { CanvasRenderer, SVGRenderer } from 'echarts/renderers'

echarts.use([
  LineChart,
  BarChart,
  PieChart,
  GridComponent,
  TooltipComponent,
  LegendComponent,
  GraphicComponent,
  AxisPointerComponent,
  CanvasRenderer,
  SVGRenderer,
])

interface Props {
  // 调用方以普通对象字面量构造 option（与迁移前 echarts-for-react 的宽松类型一致）。
  option: unknown
  style?: CSSProperties
  opts?: { renderer?: 'canvas' | 'svg' }
  notMerge?: boolean
}

export default function ReactECharts({ option, style, opts, notMerge }: Props) {
  const hostRef = useRef<HTMLDivElement>(null)
  const chartRef = useRef<echarts.EChartsType | null>(null)
  const renderer = opts?.renderer ?? 'canvas'

  useEffect(() => {
    const host = hostRef.current
    if (!host) return
    const chart = echarts.init(host, undefined, { renderer })
    chartRef.current = chart
    const resize = () => chart.resize()
    const observer = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(resize)
    observer?.observe(host)
    window.addEventListener('resize', resize)
    return () => {
      observer?.disconnect()
      window.removeEventListener('resize', resize)
      chart.dispose()
      chartRef.current = null
    }
  }, [renderer])

  useEffect(() => {
    chartRef.current?.setOption(option as echarts.EChartsCoreOption, { notMerge: !!notMerge })
  }, [option, notMerge])

  return <div ref={hostRef} className="echarts-for-react" style={{ width: '100%', height: '100%', ...style }} />
}
