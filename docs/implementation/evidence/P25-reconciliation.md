# P25 unknown Effect Reconciliation 与人工处置

- Status: PASS
- Started from: `2e4c64f0fcaef1b8a156d59bfc0b7160808db7e6` (`main`, local worktree)
- Spec references: `6.1`, `6.2`, `6.5`, `6.6`, `7.4`, `7.7`; implementation Plan P25
- Actual files: `internal/ai/workflow/{reconciliation.go,lease.go,run.go,p25_reconciliation_test.go}`, `internal/ai/effects/reconcile.go`, `internal/ai/runtime/{worker.go,p25_reconciliation_test.go}`, `internal/bootstrap/{worker.go,p25_reconciliation_test.go}`, `internal/ai/ops/actions/{action.go,system.go}`, `internal/ai/indexer/indexer.go`, `internal/controller/ops/{reconciliation.go,p25_reconciliation_test.go,soar.go}`, `api/ops/v1/reconciliation.go`, and this evidence file
- Red tests: PASS. The initial workflow tests failed at compile time on missing reconciliation primitives. The Worker integration tests added during the completion audit then failed at compile time on missing `effects.TargetState`, `effects.ReconciliationTarget`, and `WorkerConfig.QueryEffectTargetState` before the existing poll-loop wiring was implemented.

## Build-or-Reuse

- Existing capabilities: the P20 `runtime.Worker` owns the sole durable poll loop and heartbeat; `workflow.GORMStore` owns Run/Effect truth and generation/CAS; `actions` owns the sole action Registry; Indexer plus MySQL `indexed_at` owns Milvus target evidence; P08 owns terminal Run/Session completion.
- Remaining gap: the existing Worker did not scan/claim unknown Effects or invoke the target-state capabilities, so the previously added `effects.Reconciler` was unreachable in production.
- Thin adapter: `Worker.RunOnce` now reuses the same loop, Run lease, frozen identity, heartbeat, and Store to process one eligible claim. A single injected target-state callback delegates to the existing actions Registry, the existing Indexer query, or a provider-idempotent same-key retry. It does not introduce a queue, scheduler, Store, Registry, Tool-name dispatch table, or second Worker loop.

## Local Verification

- `SENTINELOPS_TEST_DSN=<redacted> SENTINELOPS_GOOSE_BIN=/tmp/sentinelops-p25-tools/goose go test ./internal/ai/effects ./internal/ai/workflow ./internal/controller/ops -run 'Test(Reconciliation|UnknownEffect|AdminResolution|ParkReason)' -count=1` -> PASS. P25 test names were aligned with the Plan filter so it covers expired external Effects, automatic claim filters, all resolutions, stale generation, derived Primary response, admin acceptance, and non-Effect park reasons.
- `SENTINELOPS_TEST_DSN=<redacted> SENTINELOPS_GOOSE_BIN=/tmp/sentinelops-p25-tools/goose go test -race ./internal/ai/workflow ./internal/ai/runtime ./internal/bootstrap -run 'Test(Reconciliation|UnknownEffect|AdminResolution|ParkReason)' -count=1` -> PASS.
- `go test ./internal/ai/effects ./internal/controller/ops ./internal/ai/ops/actions ./internal/ai/indexer ./internal/bootstrap -count=1` -> PASS.
- `SENTINELOPS_TEST_DSN=<redacted> SENTINELOPS_GOOSE_BIN=/tmp/sentinelops-p25-tools/goose go test ./internal/ai/runtime -run 'Test(LeaseBackoff|Worker|TwoPhaseApprovalWorkerLoop|Reconciliation)' -count=1` -> PASS.
- `SENTINELOPS_TEST_DSN=<redacted> SENTINELOPS_GOOSE_BIN=/tmp/sentinelops-p25-tools/goose go test ./internal/ai/workflow -run 'Test(LeaseReapFencesExpiredOwner|StaleGenerationCannotWriteTruth|ClaimPredicateExcludesIncompleteAndUnavailableRuns)$' -count=1` -> PASS.
- `go test ./internal/ai/effects ./internal/ai/workflow ./internal/ai/runtime ./internal/controller/ops ./internal/ai/ops/actions ./internal/ai/indexer ./internal/bootstrap -run '^$' -count=1` -> PASS.
- `go vet ./internal/ai/effects ./internal/ai/workflow ./internal/ai/runtime ./internal/controller/ops ./internal/ai/ops/actions ./internal/ai/indexer ./internal/bootstrap` -> PASS.
- `git diff --check` -> PASS.
- Source scan of `internal/controller/ops/reconciliation.go` for Effect/Tool imports or `.Execute(` -> PASS; the admin API does not call an external endpoint. Source scan confirms the only added ticker is a per-claim lease heartbeat inside the existing Worker, not a scheduler.
- Full workflow/runtime suites, real Milvus/Nginx/provider calls, Docker image builds, P43 full validation, hosted CI, and deployment -> NOT RUN; they are outside the P25 local gate. The provider-idempotent path was verified only against a loopback `httptest` endpoint with the same Effect key.

## Results

- An expired external `running` Effect atomically becomes `unknown`; the Run becomes `parked` with `park_reason=effect_unknown`, generation advances, canonical Events are written, and the Session remains held. The generic reaper invokes this path before ordinary lease clearing and honors one total batch limit.
- Automatic claims are limited to `effect_unknown` plus `reconcilable` or `provider_idempotent`, honor `next_reconcile_at`, and fence Run status/generation, Effect version/generation, lease owner, and reconciliation attempt. A reconciliation claim does not consume the normal Run attempt.
- The existing Worker automatically scans, claims, queries, and resolves in its sole `RunOnce` loop. Target queries run under the frozen Run identity and the same heartbeat-protected lease. `provider_idempotent` reuses the existing action Registry and same idempotency key; `milvus_index` receives the persisted Primary response and checks existing MySQL metadata.
- `Known && Applied` resolves to `succeeded` Effect plus `pending` Run with a P24-decodable persisted response. `Known && !Applied` resolves to `pending` Effect plus `pending` Run. Unknown or query error returns to `unknown` plus `parked` with bounded backoff.
- `non_reconcilable_external`, `transactional_db`, `runtime_incompatible`, and `checkpoint_missing` never enter automatic reconciliation. Admin can submit a definitive decision or accept uncertainty; acceptance keeps the Effect `unknown`, records redacted evidence/operator, and delegates Run cancellation and Session release to the sole P08 `CompleteRunAndCommitSession` primitive.

## Boundary Audit

- Non-goals: P26 Mutation cutover, P27 reliability, P42/P43 release gates, UI changes, migrations, dependency changes, new infrastructure, or full-suite/release validation.
- No real provider or external side effect was invoked. MySQL tests used disposable P03-derived schemas and pinned goose v3.27.3; credentials are redacted. The local MySQL container is test infrastructure only.
- Rollback: revert the single P25 commit; no migration, remote state, or deployed artifact is involved.
- Unfinished items: none within P25. P26 and later units remain intentionally out of scope.
