# H-02 Implementation Report

## Scope

Implemented only H-02 (persisted Worker Health and retention facts). H-03/D/E/F were not run.

## Changed files

- `internal/service/runtime/health.go`: read-only Worker snapshot projection, heartbeat classification, malformed-row partial metadata, Redactor use, validated retention projection and active-session protection count.
- `internal/controller/runtime/runtime.go`: route H-02 calls into the existing RuntimeService while preserving nil-service unavailable/not_run behavior.
- `internal/dao/mysql/runtime_query.go`: persisted snapshot and durable run-status query methods.
- `internal/service/runtime/health_test.go`: focused H-02 service tests.
- `internal/dao/mysql/runtime_health_test.go`: DAO nil-store contract tests.

## Verification

Command:

```text
SENTINELOPS_TEST_DSN='root:sentinel_test_pw@tcp(127.0.0.1:13307)/sentinelops_phase03?parseTime=true&multiStatements=true' go test ./internal/service/runtime ./internal/dao/mysql ./internal/controller/runtime -run 'Test(WorkerHealth|Retention|RuntimeControllerHViews|RuntimeWorkerSnapshotQuery|RuntimeRunStatusQuery)'
```

Output:

```text
ok   SentinelOps/internal/service/runtime 0.122s
ok   SentinelOps/internal/dao/mysql 1.431s
ok   SentinelOps/internal/controller/runtime 0.105s
```

Additional focused package checks:

```text
go test ./internal/service/runtime
ok   SentinelOps/internal/service/runtime 0.112s

go test ./internal/dao/mysql -run 'TestRuntimeWorkerSnapshotQueryRequiresStore|TestRuntimeRunStatusQueryRequiresStore'
ok   SentinelOps/internal/dao/mysql 0.106s
```

## Self-audit

- Worker health source is exclusively `runtime_worker_snapshots`; no API process or registry inference.
- Missing snapshots return empty items and `availability=unavailable`, `reason_code=not_observed`; aggregate counts remain absent.
- Missing `observed_mcp_json`/`observed_skill_json` is explicitly `unavailable/not_observed`; zero counts are never marked complete measurements.
- Heartbeat age uses `ClassifyObservedWorker`; persisted running/draining/idle states are retained when fresh, stale overrides persisted state.
- Malformed rows/components produce partial metadata while valid rows remain present; `last_error` is passed through the shared Redactor.
- Retention uses `settingssvc.GetRetention` and `RetentionPolicy.Validate`; active protection counts use `workflow.RunOccupiesSession`. Cleanup history has no durable source and is reported `availability=unavailable`, `reason_code=not_observed` with nullable `last_cleanup`.
- NOT RUN: H-03 Eval/Release, all D/E/F phases, hosted/provider/production evidence.

## Fix round 2 evidence

- Empty/malformed `worker_id` rows now force `status=unavailable` even when a heartbeat/status column is present; the row remains in the observation list with malformed metadata.
- Missing observed MCP/Skill JSON is fail-closed as `unavailable/not_observed`, so zero counts are not presented as complete measurements.
- Added `TestWorkerHealthServiceProjectsPersistedMySQLRows`, which opens the disposable MySQL DSN, persists valid/stale/malformed snapshots, invokes `RuntimeService.GetWorkerHealth`, and verifies stale/active projection, aggregate count metadata, malformed-row preservation, generation/version, and redacted error.

Latest focused command output:

```text
SENTINELOPS_TEST_DSN='root:sentinel_test_pw@tcp(127.0.0.1:13307)/sentinelops_phase03?parseTime=true&multiStatements=true' go test ./internal/service/runtime ./internal/dao/mysql ./internal/controller/runtime -run 'Test(WorkerHealth|Retention|RuntimeControllerHViews|RuntimeWorkerSnapshotQuery|RuntimeRunStatusQuery)' && go vet ./internal/service/runtime ./internal/dao/mysql ./internal/controller/runtime
ok   SentinelOps/internal/service/runtime 0.243s
ok   SentinelOps/internal/dao/mysql 3.109s
ok   SentinelOps/internal/controller/runtime 0.112s
```
