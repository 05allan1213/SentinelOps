import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { runtimeService, normalizeRuntimeParams } from '@/services/runtime'
import type {
  EffectsParams,
  EvalParams,
  ListRunsParams,
  PageParams,
  TimelineParams,
} from '@/types/runtime'

export interface RuntimeHookOptions {
  enabled?: boolean
  refetchInterval?: number | false
}

const enabledOr = (options: RuntimeHookOptions | undefined, fallback: boolean) => options?.enabled ?? fallback

/**
 * Frozen Runtime query key surface. Every key embeds the full normalized parameter set so
 * equivalent requests share one cache entry and different filters never collide.
 */
export const runtimeQueryKeys = {
  runs: (params: ListRunsParams = {}) => ['runtime', 'runs', normalizeRuntimeParams(params)] as const,
  run: (runId: string) => ['runtime', 'run', runId] as const,
  timeline: (runId: string, params: TimelineParams = {}) => ['runtime', 'timeline', runId, normalizeRuntimeParams(params)] as const,
  events: (runId: string, afterSeq: number = 0) => ['runtime', 'events', runId, afterSeq] as const,
  attempts: (runId: string, params: PageParams = {}) => ['runtime', 'attempts', runId, normalizeRuntimeParams(params)] as const,
  checkpoints: (runId: string, params: PageParams = {}) => ['runtime', 'checkpoints', runId, normalizeRuntimeParams(params)] as const,
  approvals: (runId: string, params: PageParams = {}) => ['runtime', 'approvals', runId, normalizeRuntimeParams(params)] as const,
  effects: (runId: string, params: EffectsParams = {}) => ['runtime', 'effects', runId, normalizeRuntimeParams(params)] as const,
  evidence: (runId: string, params: PageParams = {}) => ['runtime', 'evidence', runId, normalizeRuntimeParams(params)] as const,
  context: (runId: string, include?: 'history') => ['runtime', 'context', runId, include ?? 'metadata'] as const,
  traces: (runId: string, params: PageParams = {}) => ['runtime', 'traces', runId, normalizeRuntimeParams(params)] as const,
  operation: (operationId: string) => ['runtime', 'operation', operationId] as const,
  capabilities: (params: PageParams = {}) => ['runtime', 'capabilities', normalizeRuntimeParams(params)] as const,
  safety: () => ['runtime', 'safety'] as const,
  workerHealth: (params: PageParams = {}) => ['runtime', 'worker-health', normalizeRuntimeParams(params)] as const,
  eval: (params: EvalParams = {}) => ['runtime', 'eval', normalizeRuntimeParams(params)] as const,
  release: () => ['runtime', 'release'] as const,
  retention: () => ['runtime', 'retention'] as const,
}

export function useRuntimeRuns(params: ListRunsParams = {}, options?: RuntimeHookOptions) {
  return useQuery({
    queryKey: runtimeQueryKeys.runs(params),
    queryFn: ({ signal }) => runtimeService.listRuns(params, { signal }),
    enabled: enabledOr(options, true),
    refetchInterval: options?.refetchInterval,
    placeholderData: keepPreviousData,
  })
}

export function useRuntimeRun(runId: string, options?: RuntimeHookOptions) {
  return useQuery({
    queryKey: runtimeQueryKeys.run(runId),
    queryFn: ({ signal }) => runtimeService.getRun(runId, { signal }),
    enabled: enabledOr(options, Boolean(runId)),
    refetchInterval: options?.refetchInterval,
    placeholderData: keepPreviousData,
  })
}

export function useRuntimeTimeline(runId: string, params: TimelineParams = {}, options?: RuntimeHookOptions) {
  return useQuery({
    queryKey: runtimeQueryKeys.timeline(runId, params),
    queryFn: ({ signal }) => runtimeService.getTimeline(runId, params, { signal }),
    enabled: enabledOr(options, Boolean(runId)),
    refetchInterval: options?.refetchInterval,
    placeholderData: keepPreviousData,
  })
}

export function useRuntimeAttempts(runId: string, params: PageParams = {}, options?: RuntimeHookOptions) {
  return useQuery({
    queryKey: runtimeQueryKeys.attempts(runId, params),
    queryFn: ({ signal }) => runtimeService.getAttempts(runId, params, { signal }),
    enabled: enabledOr(options, Boolean(runId)),
    refetchInterval: options?.refetchInterval,
    placeholderData: keepPreviousData,
  })
}

export function useRuntimeCheckpoints(runId: string, params: PageParams = {}, options?: RuntimeHookOptions) {
  return useQuery({
    queryKey: runtimeQueryKeys.checkpoints(runId, params),
    queryFn: ({ signal }) => runtimeService.getCheckpoints(runId, params, { signal }),
    enabled: enabledOr(options, Boolean(runId)),
    refetchInterval: options?.refetchInterval,
    placeholderData: keepPreviousData,
  })
}

export function useRuntimeApprovals(runId: string, params: PageParams = {}, options?: RuntimeHookOptions) {
  return useQuery({
    queryKey: runtimeQueryKeys.approvals(runId, params),
    queryFn: ({ signal }) => runtimeService.getApprovals(runId, params, { signal }),
    enabled: enabledOr(options, Boolean(runId)),
    refetchInterval: options?.refetchInterval,
    placeholderData: keepPreviousData,
  })
}

export function useRuntimeEffects(runId: string, params: EffectsParams = {}, options?: RuntimeHookOptions) {
  return useQuery({
    queryKey: runtimeQueryKeys.effects(runId, params),
    queryFn: ({ signal }) => runtimeService.getEffects(runId, params, { signal }),
    enabled: enabledOr(options, Boolean(runId)),
    refetchInterval: options?.refetchInterval,
    placeholderData: keepPreviousData,
  })
}

export function useRuntimeEvidence(runId: string, params: PageParams = {}, options?: RuntimeHookOptions) {
  return useQuery({
    queryKey: runtimeQueryKeys.evidence(runId, params),
    queryFn: ({ signal }) => runtimeService.getEvidence(runId, params, { signal }),
    enabled: enabledOr(options, Boolean(runId)),
    refetchInterval: options?.refetchInterval,
    placeholderData: keepPreviousData,
  })
}

export function useRuntimeContext(runId: string, include?: 'history', options?: RuntimeHookOptions) {
  return useQuery({
    queryKey: runtimeQueryKeys.context(runId, include),
    queryFn: ({ signal }) => runtimeService.getContext(runId, { include, signal }),
    enabled: enabledOr(options, Boolean(runId)),
    refetchInterval: options?.refetchInterval,
    placeholderData: keepPreviousData,
  })
}

export function useRuntimeTraces(runId: string, params: PageParams = {}, options?: RuntimeHookOptions) {
  return useQuery({
    queryKey: runtimeQueryKeys.traces(runId, params),
    queryFn: ({ signal }) => runtimeService.getTraces(runId, params, { signal }),
    enabled: enabledOr(options, Boolean(runId)),
    refetchInterval: options?.refetchInterval,
    placeholderData: keepPreviousData,
  })
}

export function useRuntimeOperation(operationId: string, options?: RuntimeHookOptions) {
  return useQuery({
    queryKey: runtimeQueryKeys.operation(operationId),
    queryFn: ({ signal }) => runtimeService.getOperation(operationId, { signal }),
    enabled: enabledOr(options, Boolean(operationId)),
    refetchInterval: options?.refetchInterval,
  })
}

/** Polls the Operation endpoint only while the server reports a non-terminal operation. */
export function useRuntimeOperationPolling(operationId: string, options?: RuntimeHookOptions) {
  return useQuery({
    queryKey: runtimeQueryKeys.operation(operationId),
    queryFn: ({ signal }) => runtimeService.getOperation(operationId, { signal }),
    enabled: enabledOr(options, Boolean(operationId)),
    refetchInterval: query => (query.state.data?.item?.terminal ? false : 2000),
  })
}

export function useRuntimeCapabilities(params: PageParams = {}, options?: RuntimeHookOptions) {
  return useQuery({
    queryKey: runtimeQueryKeys.capabilities(params),
    queryFn: ({ signal }) => runtimeService.getCapabilities(params, { signal }),
    enabled: enabledOr(options, true),
    refetchInterval: options?.refetchInterval,
  })
}

export function useRuntimeSafety(options?: RuntimeHookOptions) {
  return useQuery({
    queryKey: runtimeQueryKeys.safety(),
    queryFn: ({ signal }) => runtimeService.getSafety({ signal }),
    enabled: enabledOr(options, true),
    refetchInterval: options?.refetchInterval,
  })
}

export function useRuntimeWorkerHealth(params: PageParams = {}, options?: RuntimeHookOptions) {
  return useQuery({
    queryKey: runtimeQueryKeys.workerHealth(params),
    queryFn: ({ signal }) => runtimeService.getWorkerHealth(params, { signal }),
    enabled: enabledOr(options, true),
    refetchInterval: options?.refetchInterval,
  })
}

export function useRuntimeEval(params: EvalParams = {}, options?: RuntimeHookOptions) {
  return useQuery({
    queryKey: runtimeQueryKeys.eval(params),
    queryFn: ({ signal }) => runtimeService.getEval(params, { signal }),
    enabled: enabledOr(options, true),
    refetchInterval: options?.refetchInterval,
  })
}

export function useRuntimeRelease(options?: RuntimeHookOptions) {
  return useQuery({
    queryKey: runtimeQueryKeys.release(),
    queryFn: ({ signal }) => runtimeService.getRelease({ signal }),
    enabled: enabledOr(options, true),
    refetchInterval: options?.refetchInterval,
  })
}

export function useRuntimeRetention(options?: RuntimeHookOptions) {
  return useQuery({
    queryKey: runtimeQueryKeys.retention(),
    queryFn: ({ signal }) => runtimeService.getRetention({ signal }),
    enabled: enabledOr(options, true),
    refetchInterval: options?.refetchInterval,
  })
}
