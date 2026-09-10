# Task E-01 brief (coordinator)

Task card: read `output/final-implementation-plan-2026-08-31.md` lines 1533-1576 (section `### Task E-01: Add typed Runtime service and complete TanStack Query key surface`) in full; it is the authoritative scope.

## Environment

- Work in `/home/monody/project/.worktrees/sentinelops-e` (branch `feat/phase-e-20260910`, base `f461a3c`). Do NOT edit `/home/monody/project/SentinelOps`.
- `web/node_modules` is installed. Baseline before your work: `npm run test:unit` = 10 files / 247 tests PASS.
- Commands run from `web/`: `npm run test:unit`, `npm run lint`, `npm run build`. Node 24.19.0.

## Scope

Create exactly these files:

- `web/src/types/runtime.ts`
- `web/src/services/runtime.ts`
- `web/src/hooks/useRuntimeQueries.ts`
- `web/tests/unit/runtime-service.test.ts`
- `web/tests/unit/runtime-query-keys.test.ts`

Do not modify any other product file. No new dependencies, no second Axios client, no second QueryClient, no Zustand server-state slice, no Go code, no routes/pages (those are E-03+), no F work.

## Interfaces (frozen)

- `runtimeQueryKeys`: `runs(params)`, `run(runId)`, `timeline(runId,params)`, `events(runId,afterSeq)`, `attempts(runId,params)`, `checkpoints(runId,params)`, `approvals(runId,params)`, `effects(runId,params)`, `evidence(runId,params)`, `context(runId,include?)`, `traces(runId,params)`, `operation(operationId)`, `capabilities()`, `safety()`, `workerHealth()`, `eval(params)`, `release()`, `retention()`. Every key must include every parameter value.
- `runtimeService` methods: `listRuns`, `getRun`, `getTimeline`, `getAttempts`, `getCheckpoints`, `getApprovals`, `getEffects`, `getEvidence`, `expandEvidence`, `getContext`, `getTraces`, `getOperation`, `recoverRun`, `getCapabilities`, `getSafety`, `getWorkerHealth`, `getEval`, `getRelease`, `getRetention`.
- Hooks: `useRuntimeRuns`, `useRuntimeRun`, `useRuntimeTimeline`, `useRuntimeAttempts`, `useRuntimeCheckpoints`, `useRuntimeApprovals`, `useRuntimeEffects`, `useRuntimeEvidence`, `useRuntimeContext`, `useRuntimeTraces`, `useRuntimeOperation`, `useRuntimeCapabilities`, `useRuntimeSafety`, `useRuntimeWorkerHealth`, `useRuntimeEval`, `useRuntimeRelease`, `useRuntimeRetention`.
- Routes: browser URLs are `/api/runtime/v1/...`; the existing `api` instance from `web/src/services/api.ts` already has `baseURL: '/api'`, so use paths like `/runtime/v1/runs`. Read the actual route table in the plan (`Frozen Public Contract → Routes and method names`) and mirror it exactly.
- DTO field names must be mirrored from the Go source of truth `api/runtime/v1/runtime.go` (953 lines): read it and copy the exact JSON tags/field names and optionality (`omitempty`, pointer fields -> `null | undefined`). Enums are the frozen string unions. Helper types live in `utility`/Go structs; mirror only what the Runtime routes return.

## Coordinator instructions

1. TDD: write the two test files first, run `npm run test:unit -- runtime-service runtime-query-keys`, record the RED output verbatim in your report (expected failure: module not found / missing exports). Then implement, then rerun to GREEN. Automated module-resolution failures are acceptable RED evidence for new modules.
2. Service layer: unwrap the existing `{message,data}` envelope the same way sibling services do (inspect `web/src/services/ops.ts`, `chat.ts`, `rageval.ts`). Keep `ApiRequestError` behavior; do not swallow availability payloads; never convert missing/`null`/`unavailable`/`not_run` facts into zero-valued success defaults.
3. `recoverRun` is the only mutation; it POSTs `/runtime/v1/runs/{run_id}/recovery`. No optimistic status changes anywhere.
4. AbortSignal support through Axios config where available; hooks should accept an `enabled` option and `queryFn` should pass `signal` for at least the list/detail/timeline queries.
5. Query keys must normalize equivalent params (drop empty/undefined, keep explicit values, stable ordering) so equal requests share a cache entry and different filters/ids never collide.
6. Verify: focused tests RED->GREEN; then full `npm run test:unit` (must stay >= 247 + your new tests, all PASS); `npm run lint` (expect 0 errors; 68 pre-existing warnings are baseline — your new files must not add warnings); `npm run build` (existing large-chunk warning is baseline). If any check fails, fix before committing.
7. Commit only your five files with the exact message `feat(web-runtime): add typed runtime service and query hooks`. Do not push. Another agent may commit docs concurrently in this worktree; if `index.lock` exists, wait briefly and retry rather than removing it.
8. Write `.superpowers/sdd/final-implementation-plan-2026-08-31/task-E-01-report.md` with status, files, RED/GREEN evidence, exact commands and PASS/FAIL/NOT RUN table, and any deferred item.

## Stop conditions

Stop and report if the frozen Go DTO/route contract is missing or contradicts the plan, or if the work seems to require changing backend code, routes, or the shared Axios client's behavior for other callers.
