// Agent 分析模块统一颜色常量
export const COLORS = {
  // 主题色
  primary: '#00F0E0',      // 青绿色 - 主要操作、活跃状态
  primaryDark: '#00D9FF',  // 深青色 - 渐变辅助

  // 状态色
  critical: '#F43F5E',     // 严重 - CVSS ≥9
  high: '#F97316',         // 高危
  medium: '#EAB308',       // 中危
  low: '#3B82F6',          // 低危
  success: '#22C55E',      // 成功/完成
  warning: '#F59E0B',      // 警告/解决方案

  // Agent 颜色
  agents: {
    dataCollection: '#00F0E0',
    extraction: '#A855F7',
    deduplication: '#22C55E',
    riskAssessment: '#F43F5E',
    solution: '#F59E0B',
  },

  // 背景色（深色模式）
  bg: {
    primary: '#010409',
    secondary: '#0D1117',
    tertiary: '#161B22',
    overlay: 'rgba(13, 17, 23, 0.8)',  // 统一透明度
  },

  // 边框色（深色模式）
  border: {
    default: 'rgba(48, 54, 61, 0.6)',  // 统一透明度
    light: 'rgba(48, 54, 61, 0.3)',
  },

  // 文字色（深色模式）
  text: {
    primary: '#E6EDF3',
    secondary: '#C9D1D9',
    tertiary: '#8B949E',    // 提升对比度
    disabled: '#6B7280',
  },
}
