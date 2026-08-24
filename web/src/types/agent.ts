// Agent 输出标准接口
export interface AgentOutput {
  agent_id: string
  agent_name: string
  status: 'pending' | 'running' | 'completed' | 'failed'
  confidence: number
  timestamp: string
  result: AgentResult
}

export interface AgentResult {
  summary: string
  key_evidence?: string
  impact_assets?: string[]
  ioc_list?: IOCItem[]
  cvss_detail?: CVSSDetail
  action_payload?: ActionPayload
}

export interface IOCItem {
  type: 'ip' | 'domain' | 'hash' | 'url'
  value: string
  source: string
  threat_level: 'high' | 'medium' | 'low'
}

export interface CVSSDetail {
  score: number
  vector: string
  attack_vector: string
  attack_complexity: string
  privileges_required: string
  user_interaction: string
}

export interface ActionPayload {
  rule_type: 'block_ip' | 'block_domain' | 'quarantine' | 'alert'
  value: string
  auto_executable: boolean
}

// 风险雷达五维数据
export interface RiskDimensions {
  attack_cost: number
  impact_depth: number
  fix_difficulty: number
  detect_frequency: number
  audience_scope: number
}

// Agent 日志（兼容现有结构）
export interface AgentLog {
  agent: string
  status: string
  message: string
  timestamp?: string
  data?: AgentLogData
}

export interface AgentLogData {
  count?: number
  sources?: string[]
  severity?: Record<string, number>
  maxCVSS?: number
  avgRisk?: number
  critical?: number
  highRisk?: number
  events?: AgentEvent[]
  // 解决方案Agent数据
  event_id?: number
  solution?: string
  tool_calls?: string[]
}

export interface AgentEvent {
  id: number
  title: string
  desc: string
  cve_id: string
  cvss: number
  severity: string
  vendor: string
  product: string
  source_url: string
  recommendation?: string
  similar_events?: Array<{ title: string; similarity: number }>
}
