# P23 transactional_db Primary Effect

- Status: PASS
- Started from: `4890869e7484dc8680cccbb9f45f75b8df58117a` (`main`, worktree clean, `origin/main` behind local HEAD by three commits)
- Spec references: `3.1`, `3.2`, `3.3`, `6.2`, `6.3`, `6.4`, `6.6`, `7.4`, `7.7`, Task 4 Effect execution order, package boundary `9`; implementation Plan P23
- Actual files: `internal/ai/effects`, `internal/ai/workflow/effects.go`, `internal/ai/runtime/{handler.go,interrupt.go,p23_transactional_effect_test.go}`, `internal/dao/mysql/{database.go,model.go}`, the three existing mutation Tool packages and their P23 tests, and `internal/ai/workflow/p23_effect_test.go`
- Red test and expected failure: PASS. The first test-first run failed at compile time because `TransitionEffectWithEvent`, `AgentEffect`, and the P23 Effect status definitions did not exist. No implementation was counted as passing before that failure.
- Local commands:
  - `SENTINELOPS_TEST_DSN='root:sentinelops-p03-only@tcp(127.0.0.1:56931)/sentinelops_p03?parseTime=true' SENTINELOPS_GOOSE_BIN=/tmp/sentinelops-p23-tools/goose go test ./internal/ai/effects ./internal/ai/workflow ./internal/ai/tools/report ./internal/ai/tools/intelligence ./internal/ai/tools/event ./internal/ai/tools/ops ./internal/ai/runtime -run 'Test(TransactionalEffect|EffectIdentity|EffectRollback|ReuseSucceeded)' -count=1` -> PASS
  - `SENTINELOPS_TEST_DSN='root:sentinelops-p03-only@tcp(127.0.0.1:56931)/sentinelops_p03?parseTime=true' SENTINELOPS_GOOSE_BIN=/tmp/sentinelops-p23-tools/goose go test ./internal/ai/effects ./internal/ai/workflow ./internal/ai/tools/report ./internal/ai/tools/intelligence ./internal/ai/tools/event ./internal/ai/tools/ops ./internal/ai/runtime -count=1` -> PASS
  - `go vet ./internal/ai/effects ./internal/ai/workflow ./internal/ai/tools/report ./internal/ai/tools/intelligence ./internal/ai/tools/event ./internal/ai/tools/ops ./internal/ai/runtime ./internal/dao/mysql` -> PASS
  - `git diff --check` -> PASS
- Results: PASS for the P23 filtered gate, directly affected package tests, and targeted vet. The one-time `sentinelops-p23` Compose MySQL was isolated to `manifest/docker/docker-compose.test.yml`; no dev/prod stack or external API was used.
- Key assertions: exact approved Resume revalidates Approval/Checkpoint-derived identity, uses checkpoint `ArgumentsJSON` for the original synchronous endpoint, creates one stable `policy.EffectKey` Primary row, performs the existing DAO/domain write inside the same GORM transaction, records `effect.started`/`effect.succeeded`, creates deterministic derived `pending` rows, redacts request/response before persistence, reuses succeeded results without invoking the endpoint, and rolls back domain/ledger/events together without `unknown`.
- Deviations from recommended route: target repository is the Plan's calibrated `/home/monody/project/SentinelOps`; the removed historical `/home/monody/project/Fo-Sentinel-Agent` path is not used
- Raw artifact references: none
- Unfinished items: none within P23. The staged-diff audit and local commit are the final handoff steps; P24/P25 external execution/reconciliation and P26 cutover remain intentionally out of scope.

## Boundary Audit

- Goal: give the Catalog's existing `transactional_db` L1 mutations (`create_report`, `save_intelligence`, `update_event_status`) a stable Primary Effect, transaction-scoped domain write, structured Events, deterministic derived pending rows, and succeeded-result reuse.
- Non-goals: external Effect execution or reconciliation (P24/P25), global Mutation cutover and legacy direct-write removal (P26), production Gate/cutover (P42), new Tool names or schemas, MQ/Outbox, a second Worker/Store/Registry/Action adapter, and full-suite/P43 verification.
- Compatibility contracts: preserve the existing Tool Registry and endpoint callbacks, existing Service/DAO behavior for ordinary HTTP/legacy callers, the P22 exact Approval/Checkpoint authorization, and `policy.EffectKey(run_id, proposal_hash, effect_step)` identity independent of attempt/generation/tool_call_id.
- Safety invariants: lease/generation, approved Approval, frozen runtime/policy identity and effective Gate are revalidated before the domain write; ledger, domain write, Effect Events, and deterministic derived pending rows commit or roll back together; `transactional_db` never becomes `unknown`; succeeded replay never invokes the endpoint again; persisted request/response and Events are redacted.
- Expected packages/files: existing `internal/ai/workflow`, `internal/ai/runtime`, `internal/ai/policy`, `internal/dao/mysql`, the three existing mutation endpoint packages, one new thin `internal/ai/effects` package, and this evidence file.
- Verification: P23 filtered local gate with non-empty matching tests; directly affected Runtime/DAO/Ops tests; targeted `go vet`; source scans proving a single Effect Store path and no new Action Registry/Queue/MQ.
- Rollback: revert the single P23 local commit; no Migration, remote, shared database, Compose stack, or external side effect is changed by this unit.

## Build-or-Reuse

| Existing SentinelOps capability | Locked Eino capability | Remaining project-specific gap | Thinnest sufficient adapter |
| --- | --- | --- | --- |
| `workflow.GORMStore`, fenced Run transaction helpers, structured Event catalog, GORM/MySQL transactions, migrated `agent_effects`, `policy.EffectKey`, RuntimeHandler, Tool Registry, original Tool endpoints and existing `ops/actions` Registry | ChatModelAgent middleware supplies the original endpoint callback and official StatefulInterrupt/Resume lifecycle; Eino v0.9.15 has no business Effect Ledger or MySQL domain transaction primitive | Stable Primary ledger lifecycle, Approval/Gate/lease CAS, transaction-bound DAO calls, derived pending rows, persisted result reuse | One `effects.Executor` delegating to one exact `GORMStore.TransitionEffectWithEvent`; it accepts the Handler-provided endpoint callback and owns no Registry, Tool switch, queue, or business implementation |
