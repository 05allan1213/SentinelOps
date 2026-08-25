import api from './api'
import { ApiResponse } from '@/types'

export interface PlaybookStep {
  id?: string
  step_order: number
  name: string
  action_type: string
  action_params: string
  on_success: number
  on_failure: number
  parallel_group: string
  max_retries: number
  retry_interval: number
  risk_level: string
  continue_on_error: boolean
}

export interface Playbook {
  id: string
  name: string
  description: string
  trigger_rules: unknown
  exec_mode: string
  enabled: boolean
  cooldown_sec: number
  steps?: PlaybookStep[]
  created_at: string
  success_rate?: number  // -1 = 无记录，0-1 = 成功率
}

export interface OpsRun {
  id: string
  playbook_id: string
  playbook_name?: string
  event_id: string
  event_title?: string
  event_severity?: string
  plan_summary?: string
  status: string
  error_msg?: string
  duration_ms: number
  started_at: string
  finished_at?: string
  steps?: RunStep[]
}

export interface RunStep {
  id: string
  step_order: number
  action_type: string
  status: string
  output?: string
  error_msg?: string
  retry_count: number
  duration_ms: number
  started_at: string
}

export interface ProtectedAsset {
  id: number
  asset_type: string
  value: string
  reason: string
  created_at: string
}

export interface OpsStats {
  total_runs: number
  success_runs: number
  failed_runs: number
}

export interface ApprovalItem {
  id: string
  run_id: string
  tool_name: string
  tool_revision: string
  risk_level: string
  proposal: unknown
  proposal_hash: string
  requested_by: string
  decided_by?: string
  status: string
  version: number
  decision_reason?: string
  published_at?: string
  expires_at?: string
  decided_at?: string
}

export interface UnknownEffectItem {
  id: string
  run_id: string
  tool_name: string
  effect_step: string
  effect_type: string
  status: string
  version: number
  evidence?: unknown
  reconciliation_attempts: number
}

export interface ApprovalDecisionInput {
  proposal_hash: string
  version: number
  reason: string
}

export interface EffectResolutionInput {
  resolution: 'executed' | 'not_executed' | 'still_unknown'
  evidence?: unknown
  response?: unknown
  external_reference?: string
}

export const opsQueryKeys = {
  approvals: (limit = 20) => ['ops', 'approvals', limit] as const,
  unknownEffects: (limit = 20) => ['ops', 'effects', 'unknown', limit] as const,
  runs: (limit = 20) => ['ops', 'runs', limit] as const,
}

export const opsService = {
  // 响应剧本
  async listPlaybooks(): Promise<Playbook[]> {
    const res = await api.get<ApiResponse<{ items: Playbook[] }>>('/ops/v1/playbooks')
    return res.data.data?.items || []
  },
  async getPlaybook(id: string): Promise<Playbook> {
    const res = await api.get<ApiResponse<{ item: Playbook }>>(`/ops/v1/playbooks/${id}`)
    return res.data.data!.item
  },
  async createPlaybook(data: Partial<Playbook>): Promise<string> {
    const res = await api.post<ApiResponse<{ id: string }>>('/ops/v1/playbooks', data)
    return res.data.data!.id
  },
  async updatePlaybook(id: string, data: Partial<Playbook>): Promise<void> {
    await api.put(`/ops/v1/playbooks/${id}`, data)
  },
  async deletePlaybook(id: string): Promise<void> {
    await api.delete(`/ops/v1/playbooks/${id}`)
  },
  async testPlaybook(id: string, eventId: string): Promise<string> {
    const res = await api.post<ApiResponse<{ run_id: string }>>(`/ops/v1/playbooks/${id}/test`, { event_id: eventId })
    return res.data.data?.run_id || ''
  },

  // 运行记录
  async listRuns(limit = 20): Promise<OpsRun[]> {
    const res = await api.get<ApiResponse<{ items: OpsRun[] }>>('/ops/v1/runs', { params: { limit } })
    return res.data.data?.items || []
  },
  async getRun(id: string): Promise<OpsRun> {
    const res = await api.get<ApiResponse<{ item: OpsRun }>>(`/ops/v1/runs/${id}`)
    return res.data.data!.item
  },

  // 受保护资产
  async listProtectedAssets(): Promise<ProtectedAsset[]> {
    const res = await api.get<ApiResponse<{ items: ProtectedAsset[] }>>('/ops/v1/protected_assets')
    return res.data.data?.items || []
  },
  async createProtectedAsset(data: Omit<ProtectedAsset, 'id' | 'created_at'>): Promise<void> {
    await api.post('/ops/v1/protected_assets', data)
  },
  async deleteProtectedAsset(id: number): Promise<void> {
    await api.delete(`/ops/v1/protected_assets/${id}`)
  },

  // 运行统计
  async getStats(): Promise<OpsStats> {
    const res = await api.get<ApiResponse<OpsStats>>('/ops/v1/stats')
    return res.data.data || { total_runs: 0, success_runs: 0, failed_runs: 0 }
  },

  // 按事件触发
  async triggerForEvent(eventId: string): Promise<void> {
    await api.post<ApiResponse<void>>('/ops/v1/playbooks/trigger_for_event', { event_id: eventId })
  },

  // DirectRunForEvent：直接触发 AI 运维，返回 run_id
  async directRun(eventId: string): Promise<string> {
    const res = await api.post<ApiResponse<{ run_id: string }>>('/ops/v1/runs/direct', { event_id: eventId })
    return res.data.data?.run_id || ''
  },

  // 清空运行记录
  async clearRuns(): Promise<void> {
    await api.delete('/ops/v1/runs')
  },

  // 删除运行记录
  async deleteRun(id: string): Promise<void> {
    await api.delete(`/ops/v1/runs/${id}`)
  },

  // Approval / HITL
  async listApprovals(limit = 20): Promise<ApprovalItem[]> {
    const res = await api.get<ApiResponse<{ items: ApprovalItem[] }>>('/ops/v1/approvals', { params: { limit } })
    return (res.data.data?.items || []).filter(item => item.status === 'pending')
  },
  async getApproval(id: string): Promise<ApprovalItem> {
    const res = await api.get<ApiResponse<{ item: ApprovalItem }>>(`/ops/v1/approvals/${id}`)
    return res.data.data!.item
  },
  async approveApproval(id: string, input: ApprovalDecisionInput): Promise<ApprovalItem> {
    const res = await api.post<ApiResponse<{ item: ApprovalItem }>>(`/ops/v1/approvals/${id}/approve`, input)
    return res.data.data!.item
  },
  async rejectApproval(id: string, input: ApprovalDecisionInput): Promise<ApprovalItem> {
    const res = await api.post<ApiResponse<{ item: ApprovalItem }>>(`/ops/v1/approvals/${id}/reject`, input)
    return res.data.data!.item
  },

  // Unknown Effect 对账，只提交 admin 决定，不直接执行外部 Effect
  async listUnknownEffects(limit = 20): Promise<UnknownEffectItem[]> {
    const res = await api.get<ApiResponse<{ items: UnknownEffectItem[] }>>('/ops/v1/effects/unknown', { params: { limit } })
    return (res.data.data?.items || []).filter(item => item.status === 'unknown' || item.status === 'reconciling')
  },
  async resolveEffect(id: string, input: EffectResolutionInput): Promise<void> {
    await api.post(`/ops/v1/effects/${id}/resolve`, input)
  },
  async acceptUnknownEffect(id: string, runId: string, reason: string, evidence?: unknown): Promise<void> {
    await api.post(`/ops/v1/effects/${id}/accept-unknown`, { run_id: runId, reason, evidence })
  },
}
