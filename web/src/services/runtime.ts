import api from './api'
import { ApiResponse } from '@/types'
import type {
  ApprovalsRes,
  AttemptsRes,
  CapabilitiesRes,
  CheckpointsRes,
  EffectsParams,
  EffectsRes,
  EvalParams,
  EvalRes,
  EvidenceRes,
  ExpandEvidenceRes,
  GetContextRes,
  GetOperationRes,
  GetRunRes,
  ListRunsParams,
  ListRunsRes,
  OperationAcceptedRes,
  RecoverRunRequest,
  ReleaseRes,
  RetentionRes,
  SafetyRes,
  TimelineParams,
  TimelineRes,
  TracesRes,
  WorkerHealthRes,
  PageParams,
} from '@/types/runtime'

export type RuntimeQueryParams = Record<string, string | number | boolean | string[] | undefined>

export interface RuntimeRequestOptions {
  signal?: AbortSignal
}

export interface RuntimeIncludeOptions {
  include?: 'quote' | 'history'
  signal?: AbortSignal
}

/** datetime-local uses the browser timezone; the API requires an explicit RFC3339 offset. */
export function runtimeFilterTimestamp(value: string): string {
  if (!value || /(?:Z|[+-]\d{2}:\d{2})$/i.test(value)) return value
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toISOString()
}

export function runtimeLocalTimestamp(value: string): string {
  if (!value || !/(?:Z|[+-]\d{2}:\d{2})$/i.test(value)) return value
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`
}

/** Deterministic parameter normalization: empty filters are dropped, explicit false/0 values are kept. */
export function normalizeRuntimeParams(params: object = {}): RuntimeQueryParams {
  const source = params as Record<string, unknown>
  const normalized: RuntimeQueryParams = {}
  Object.keys(source)
    .sort()
    .forEach(key => {
      const value = source[key]
      if (value === undefined || value === null || value === '') return
      if (Array.isArray(value)) {
        if (value.length === 0) return
        normalized[key] = value.map(String)
        return
      }
      if (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean') {
        normalized[key] = (key === 'from' || key === 'to') && typeof value === 'string' ? runtimeFilterTimestamp(value) : value
      }
    })
  return normalized
}

const get = async <T>(url: string, params: RuntimeQueryParams = {}, options: RuntimeRequestOptions = {}): Promise<T> => {
  const res = await api.get<ApiResponse<T>>(url, { params, signal: options.signal })
  return res.data.data
}

/** Sole Runtime service. Mirrors the frozen /runtime/v1 routes; no cache, no optimistic state. */
export const runtimeService = {
  listRuns: (params: ListRunsParams = {}, options?: RuntimeRequestOptions) =>
    get<ListRunsRes>('/runtime/v1/runs', normalizeRuntimeParams(params), options),

  getRun: (runId: string, options?: RuntimeRequestOptions) =>
    get<GetRunRes>(`/runtime/v1/runs/${runId}`, {}, options),

  getTimeline: (runId: string, params: TimelineParams = {}, options?: RuntimeRequestOptions) =>
    get<TimelineRes>(`/runtime/v1/runs/${runId}/timeline`, normalizeRuntimeParams(params), options),

  getAttempts: (runId: string, params: PageParams = {}, options?: RuntimeRequestOptions) =>
    get<AttemptsRes>(`/runtime/v1/runs/${runId}/attempts`, normalizeRuntimeParams(params), options),

  getCheckpoints: (runId: string, params: PageParams = {}, options?: RuntimeRequestOptions) =>
    get<CheckpointsRes>(`/runtime/v1/runs/${runId}/checkpoints`, normalizeRuntimeParams(params), options),

  getApprovals: (runId: string, params: PageParams = {}, options?: RuntimeRequestOptions) =>
    get<ApprovalsRes>(`/runtime/v1/runs/${runId}/approvals`, normalizeRuntimeParams(params), options),

  getEffects: (runId: string, params: EffectsParams = {}, options?: RuntimeRequestOptions) =>
    get<EffectsRes>(`/runtime/v1/runs/${runId}/effects`, normalizeRuntimeParams(params), options),

  getEvidence: (runId: string, params: PageParams = {}, options?: RuntimeRequestOptions) =>
    get<EvidenceRes>(`/runtime/v1/runs/${runId}/evidence`, normalizeRuntimeParams(params), options),

  expandEvidence: (runId: string, evidenceId: string, options?: RuntimeIncludeOptions) =>
    get<ExpandEvidenceRes>(
      `/runtime/v1/runs/${runId}/evidence/${evidenceId}`,
      normalizeRuntimeParams({ include: options?.include }),
      { signal: options?.signal },
    ),

  getContext: (runId: string, options?: RuntimeIncludeOptions) =>
    get<GetContextRes>(
      `/runtime/v1/runs/${runId}/context`,
      normalizeRuntimeParams({ include: options?.include }),
      { signal: options?.signal },
    ),

  getTraces: (runId: string, params: PageParams = {}, options?: RuntimeRequestOptions) =>
    get<TracesRes>(`/runtime/v1/runs/${runId}/traces`, normalizeRuntimeParams(params), options),

  getOperation: (operationId: string, options?: RuntimeRequestOptions) =>
    get<GetOperationRes>(`/runtime/v1/operations/${operationId}`, {}, options),

  recoverRun: async (runId: string, body: RecoverRunRequest): Promise<OperationAcceptedRes> => {
    const res = await api.post<ApiResponse<OperationAcceptedRes>>(`/runtime/v1/runs/${runId}/recovery`, body)
    return res.data.data
  },

  getCapabilities: (params: PageParams = {}, options?: RuntimeRequestOptions) =>
    get<CapabilitiesRes>('/runtime/v1/capabilities', normalizeRuntimeParams(params), options),

  getSafety: (options?: RuntimeRequestOptions) =>
    get<SafetyRes>('/runtime/v1/safety', {}, options),

  getWorkerHealth: (params: PageParams = {}, options?: RuntimeRequestOptions) =>
    get<WorkerHealthRes>('/runtime/v1/worker-health', normalizeRuntimeParams(params), options),

  getEval: (params: EvalParams = {}, options?: RuntimeRequestOptions) =>
    get<EvalRes>('/runtime/v1/eval', normalizeRuntimeParams(params), options),

  getRelease: (options?: RuntimeRequestOptions) =>
    get<ReleaseRes>('/runtime/v1/release', {}, options),

  getRetention: (options?: RuntimeRequestOptions) =>
    get<RetentionRes>('/runtime/v1/retention', {}, options),
}

export default runtimeService
