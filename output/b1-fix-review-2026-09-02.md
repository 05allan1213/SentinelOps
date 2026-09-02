# B1-02 契约修复 Review（2026-09-02）

> 仓库：`/home/monody/project/SentinelOps`
> 范围：只修复上一轮 review 认定的 B1-01/B1-02 契约缺口；不扩展 Route/IRuntimeV1，不加 migration，不进入 D/E/F。

## 修复内容

### 1. Worker/Attempt compatibility

- `workflow.AttemptFingerprint(runID, attempt, generation)` 使用 `sentinelops/runtime-attempt/v1` 域分隔 SHA-256，AttemptFingerprint 不再用 `executing_worker_fingerprint` 冒充。
- `ClaimInput` 与 `ClaimNextRecoveryOperation` 携带 `ExecutingWorkerFingerprint`；真实 Worker 从 `WorkerObservation.RuntimeCompatibilityHash` 提供当前 Frozen Snapshot 指纹。
- 首次 Claim/Recovery claim 在同一 row-lock 事务内校验指纹等于 Run 的 `runtime_compatibility_hash`，不等即拒绝认领；`beginAttemptTx` 首次创建 Attempt 时把指纹写入现有 `workflow_attempts.executing_worker_fingerprint`。
- `BuildCompatibilityDTO` 的 `ExecutingWorkerFingerprint` 只取 Attempt 持久化值，`WorkerMatch` 只与该值对应的当前 `RuntimeWorkerSnapshot.RuntimeCompatibilityHash` 比较；旧 NULL 行显示 `partial/worker_fingerprint_not_observed`，不会伪装成 exact restore。

### 2. Context `include=history`

- `ContextDTO` 新增 `history`、`history_truncated`、`redaction_applied`，以及最小消息类型 `RuntimeHistoryMessageDTO`。
- `BuildContextDTO` 同时解析真实 `fo/session-state/v1` 对象（含 `history[]`）与旧测试裸数组。
- 有权限的 `include=history` 只返回最近 50 条已脱敏消息；超过 50 条置 `history_truncated=true`；content 按 8000 runes 截断；任一 Redactor 变更置 `redaction_applied=true`；`HistoryCount` 始终为原始条数；malformed 消息只降级局部 metadata。
- controller `GetContext` 直接调用 `RuntimeService.GetContext(ctx, runID, includeHistory)`，不再用 GetRun summary 代替。

### 3. Budget / Agent quality

- Budget JSON invalid 时所有 nullable 计数保持 `nil`，只返回 `partial/unknown/invalid_budget_json`。
- `BuildRunSummary` 使用 DAO 已提供的 `RuntimeAgentQuality`：`missing` → `partial/unknown/agent_missing`，`partial` → `partial/unknown/malformed_agent_input`；`complete` 不降级。

### 4. 工具与文档

- 删除 staticcheck 报出的未使用函数 `newDurableAPIService`、`latestApprovalEventSeq`。
- 修正 controller test 中不可能为 nil 的断言；`GetContext` controller 重写后不再需要 S1016 struct literal。
- 对触及文件执行 `goimports`；冻结计划中的 ContextDTO 描述与 B1-02 卡片同步更新。

## 本地提交（完整 SHA）

- `f1ce886`：separate attempt and worker fingerprints
- `8542022`：add bounded redacted history expansion
- `cd66487`：keep invalid budget counters nil
- `34e0485`：remove dead helpers and record B1 fix review
- 全程未 push。

## 验证证据

已 PASS（使用 disposable `sentinelops_phase03` DSN，凭据未写入本文件）：

- `go test ./internal/ai/workflow -run 'TestAttemptFingerprint'`
- `SENTINELOPS_TEST_DSN=... go test ./internal/ai/workflow -run 'TestAttempt(PersistsExecuting|ClaimRejectsMismatched)|TestC06RecoveryFirstClaim' -count=1 -v`
- `SENTINELOPS_TEST_DSN=... go test ./internal/service/runtime -run 'TestGetContextRealRunHistoryCountIsNonZero' -count=1 -v`
- `go test ./internal/service/runtime -run 'Test(Context|Compatibility|RunDetail|BudgetInvalid|RunSummary)'`
- `SENTINELOPS_TEST_DSN=... go test ./internal/ai/workflow -run 'Test(Attempt|C06)' -count=1 -timeout 30m`
- `SENTINELOPS_TEST_DSN=... go test ./api/runtime/... ./internal/service/runtime ./internal/controller/runtime ./internal/dao/mysql ./internal/ai/runtime -count=1`
- `go test -race ./internal/service/runtime ./internal/controller/runtime`、`go vet ./...`、`staticcheck`（目标包）、`git diff --check` 均 PASS。
- 最终全量复查：`SENTINELOPS_TEST_DSN=... go test -p 1 ./api/runtime/... ./internal/service/runtime ./internal/controller/runtime ./internal/dao/mysql ./internal/ai/workflow ./internal/ai/runtime -count=1 -timeout 30m` PASS；其中 `internal/ai/workflow` 787.037s，`internal/ai/runtime` 61.064s，`internal/dao/mysql` 41.276s。
- Final review（2026-09-02）：无 Critical/Important 缺陷；四组契约修复均与 frozen 计划一致，边界（旧 NULL 行、默认无 history、越权 403、invalid JSON、malformed agent）由新增测试覆盖。

## 未执行

D/E/F、Hosted CI、真实 Provider/API/Worker 运行、rollout/rollback、镜像合同与 P43 均 NOT RUN；本地 PASS 不代表生产就绪。
