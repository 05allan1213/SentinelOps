# H-03 Implementation Report

## Scope

Implemented only H-03 factual Eval/Release read models. H-01/H-02 code and
routes were preserved; D/E/F were not run.

## Source audit

- `internal/service/rageval.GetDashboard` is the existing RAG source. It
  aggregates `agent_trace_runs` and RETRIEVER nodes and applies
  `mysql.EvidenceTraceQualityPredicate`, so incomplete traces are excluded
  from its metric aggregate.
- No versioned Agent Eval suite, baseline, artifact store, deterministic case
  runner, or LLM judge result source exists in this checkout. Runtime traces
  are therefore never promoted to Agent Eval evidence.
- `internal/ai/runtime.CurrentRuntimeVersion` is the current canonical Runtime
  identity source; `GateEvaluator.CurrentState` is the existing static/dynamic
  effective Gate source.
- `runtime_worker_snapshots` is the only persisted Worker observation source.
  The current schema has no durable gray/rollout/rollback execution source.

## Changes

- Added `RuntimeService.GetEval(ctx, EvalFilter)` and
  `RuntimeService.GetRelease(ctx)` in `internal/service/runtime/eval_release.go`.
- Normalized the two Eval namespaces to `agent_eval` and `rag_eval`.
- Explicit `rag_eval` requests reuse the existing RAG dashboard aggregate;
  its result is labeled `rag_eval`, and deterministic Gate/LLM judge fields are
  `not_applicable`.
- Agent Eval requests return a valid zero-valued DTO with
  `availability=unavailable`, `data_quality=unknown`,
  `reason_code=eval_not_executed`, and `not_run=true` when the versioned source
  is absent.
- Release exposes current Runtime version, current effective Gate vector, and
  sorted unique versions read from persisted Worker snapshots. Missing
  gray/rollback evidence keeps both fields nullable and returns
  `availability=unavailable`, `reason_code=not_observed`, `not_run=true`.
- Wired the existing Runtime controller methods to the service while retaining
  the frozen routes, `{message,data}` envelope, and read-only boundary.
- Added focused unit tests plus an optional disposable-MySQL service-path test
  for observed Worker versions.

## Verification

PASS — `go test ./internal/service/runtime ./internal/controller/runtime -count=1`

PASS — focused H-03 service/controller tests (`TestEval*`, `TestRelease*`,
`TestIncompleteTraceCannotPassRelease`, `TestRuntimeControllerH03ViewsUseServiceReadModels`).

PASS — `go test ./internal/service/rageval -count=1`

PASS — `go test ./internal/dao/mysql -run 'TestTraceIncompleteAggregateContract|TestRuntimeWorkerSnapshotQueryRequiresStore' -count=1`

PASS — `go vet ./internal/service/runtime ./internal/controller/runtime`

NOT RUN — `TestReleaseServiceProjectsPersistedWorkerVersions` was skipped because
`SENTINELOPS_TEST_DSN` was not set in this session. No real provider, full Agent
Eval, rollout, rollback, image, D, E, or F evidence was run.

## Read-only and truthfulness checks

- No Agent Eval truth table, fixture pass, fake release dashboard, gray/rollback
  control, or mutation endpoint was added.
- Worker versions are never filled from `CurrentRuntimeVersion`; they only come
  from non-empty persisted snapshot values.
- Release remains `not_run` even if current version or Gate facts are present,
  because no durable rollout execution fact exists.
