# P38 Compose 下真实 Playwright 审批链

- Status: `PASS`
- Started from: `9502871d68b7113df4ecc5b02b81f32b7fc71503` (`main`，P38 开始前的干净提交)
- Completed: 2026-08-26
- Spec references: P38；真实浏览器→API→MySQL→Worker→Checkpoint/Approval→Resume→Effect→UI 刷新；单一 `sentinelops-e2e` Compose project
- Commit: 由实施计划台账记录本单元本地提交 SHA；未 push

## Boundary Audit

- 目标：在唯一 `sentinelops-e2e` 隔离 Compose project 中，用真实浏览器和生产 API/Worker 路径证明 pending Approval、CAS 决策、StatefulInterrupt Resume、Primary/Derived Effect、unknown Effect 对账和 SSE replay/reconnect。
- 明确非目标：不修改生产 Compose、Runtime/Store/API 契约，不引入第二套 Worker/Runtime/Store/Registry/模型网关，不执行 P39-P43 Eval、CI、发布或全量浏览器矩阵。
- 兼容与安全不变量：浏览器不写数据库；viewer 决策返回 403；相同决定幂等且不追加 Event；相反决定和 proposal hash mismatch 返回 409；`unknown`/`parked`/`reconciling` 不显示成功；unknown 只能由 admin 提交证据化处置；测试开关只在 `app.environment=test` 且显式设置 `SENTINELOPS_E2E_ENABLE_MUTATION_GATES=true` 时生效，生产默认保持 fail-closed。
- 隔离契约：Compose 合并配置无固定 `container_name`、无 external/shared network、无开发/生产宿主机数据卷；MySQL/Redis/Milvus 使用 `sentinelops_e2e_*` 专属卷，只有 nginx 暴露 `127.0.0.1:18080`。Worker 使用只读 Docker socket 和专属 nginx blocklist 卷完成真实 mutation fixture。
- 清理契约：验证结束后只执行 `docker compose -p sentinelops-e2e ... down -v --remove-orphans`，不触碰其它 Compose project。

## Root Cause And Fixes

首次干净卷执行的失败不是 Approval API 放行了错误请求，而是后端按 P26 正确拒绝了未开启 Mutation Gate 的测试运行：`approval.invalidated(reason=write_gate_closed)` → `run.parked(park_reason=approval_invalidated)`。同时 provider double 的 Replanner 在 Effect 成功后再次选择 `block_ip`，嵌套 `AgentTool` 使用了错误的 `query` 字段，StatefulInterrupt 还过早结算了预算 reservation；这些问题共同使真实链无法到达 terminal success。

本单元的修复保持生产边界不变：

- `RuntimeHandler` 在 `StatefulInterrupt` 时保留 reservation，Resume 后由同一物理调用结算；stream、enhanced tool 和普通 tool 路径一致。
- 四类 Tool wrapper 的 endpoint-level Interrupt 和两类 stream receive-level Interrupt 都有回归断言；中断不会先 settle reservation。
- Runtime Snapshot 只在测试环境且显式 opt-in 时打开 L1/L2 Gate；增加负向测试证明默认值仍关闭。
- provider double 识别已完成的子 Agent 结果，优先返回 `respond`；记录既有 tool call，避免重复 `block_ip`/`webhook_out`，并把官方 `AgentTool` 参数改为 `request`。
- 预算 truth 使用相对容差比较浮点成本，避免 Resume 后的序列化误差被误判为不一致。
- Playwright 抽出公共登录、Run、Approval、SSE、unknown helper；移除审批确认后的重复 locator 操作，覆盖 CAS、hash mismatch、reject、viewer RBAC、SSE replay 和 unknown admin resolution。
- 测试 Compose 增加显式环境、nginx blocklist volume、只读 Docker socket 和 provider double 接线；不修改生产配置。

## Build-or-Reuse

| 现有能力 | 剩余缺口 | 最薄实现 |
| --- | --- | --- |
| P20 API/Worker/SSE、P21-P25 Approval/Effect/Reconciliation、P37 ActionQueue、P03/P04 Compose 基础 | 真实 provider HTTP double、浏览器串联和测试 mutation fixture | 复用现有 Runtime/Store/Worker/ActionQueue，只增加 provider double、Compose 覆盖和 Playwright helper/spec |

## Red Evidence

首次真实 Compose 构建尝试在迁移镜像阶段因访问 `proxy.golang.org` 超时；复用已成功构建的本地迁移镜像后，唯一 `sentinelops-e2e` project 通过 `up -d --no-build --wait` 启动。随后首次干净卷 Playwright 运行出现 `1 FAIL, 1 PASS, 1 SKIP`；失败链的数据库/Event 证据为 `approval.invalidated(write_gate_closed)` 与 `run.parked(park_reason=approval_invalidated)`。该结果证明问题位于测试环境 Gate/恢复编排，而不是绕过 API 制造的假成功。

修复后在同一唯一 project、同一真实 MySQL/Redis/Milvus/API/Worker/nginx/provider double 栈上执行 4 个 Case：

```text
4 passed (29.5s)
```

## Actual Files

- `internal/ai/runtime/handler.go`
- `internal/ai/runtime/handler_test.go`
- `internal/ai/runtime/p20_api_worker_sse_test.go`
- `internal/ai/runtime/profile.go`
- `internal/ai/workflow/budget.go`
- `manifest/docker/Dockerfile.backend.e2e`
- `manifest/docker/docker-compose.test.yml`
- `manifest/test/provider-double/main.go`
- `web/tests/e2e/p38-helpers.ts`
- `web/tests/e2e/approval-flow.spec.ts`
- `web/tests/e2e/unknown-effect.spec.ts`
- `docs/implementation/evidence/P38-playwright.md`

## Verification Ledger

Exact targeted Go commands (all exited 0):

```bash
go test ./internal/ai/runtime -run '^(Test(DurableSnapshotContainsCatalogToolsAndFrozenWriteGates|TestOnlyMutationGatesRequireExplicitOptIn|WorkerRunsAfterAPIContextIsGone|WorkerRetryableFailureUsesBoundedBackoffWithoutResettingAttempt|WorkerFatalFailureUsesOnlyCompletePrimitive|WorkerCanceledFailureUsesOnlyCompletePrimitive|WorkerParkedFailureUsesFencedTransition|WorkerLeaseLossCancelsExecutionAndSkipsTruthWrites|WorkerPollLoopContinuesAfterLeaseLoss)|TestRuntimeHandler|TestMutationDisabledAcrossAllToolEndpoints|TestMutationDisabledInNestedBeforeAgentTool|TestStatefulInterruptProductionHandlerAndClosedGateFailClosed|TestRetryPhysicalCallsUseDistinctReservationsAndCatalogRefs|TestFailoverCandidateMustMatchFrozenSnapshot|TestStreamFailureSettlesPhysicalCallAsFailed|TestNoDoubleRetryLeavesEinoFailoverProxyUnwrapped|TestBudgetModelUsageActualIncludesCachedAndReasoningOnce|TestUsageUnknownWhenModelUsageMissing)$' -count=1
go test ./internal/ai/workflow -run '^(Test(RunTransitionMatrix|VersionedEventCatalogIsComplete|CompleteRunDoesNotAllowLegacySuccessSpelling|ControlPlaneReservationKindsShareBasePrimitive|BudgetDimensionsUseSharedReservationPrimitive|SettlementActualDoesNotDoubleCountDetails|TraceIncompleteEventPrecedesRunCompleted|TraceFlushedEventPrecedesRunCompleted|TraceFlushedEventRequiresAttemptTraceID))$' -count=1
go test ./internal/ai/tools ./internal/ai/agent/plan_pipeline ./internal/ai/agent/ops_pipeline -count=1
go vet ./internal/ai/runtime ./internal/ai/workflow ./internal/ai/tools ./internal/ai/agent/plan_pipeline ./internal/ai/agent/ops_pipeline
```

| Gate | Command / evidence | Result |
| --- | --- | --- |
| Compose isolation | `docker compose -p sentinelops-e2e -f manifest/docker/docker-compose.yml -f manifest/docker/docker-compose.test.yml config --format json` | `PASS`：无固定容器名、external network、宿主数据 bind mount；卷均为 `sentinelops_e2e_*`；仅 nginx 暴露 `127.0.0.1:18080` |
| Real stack | `docker compose -p sentinelops-e2e -f manifest/docker/docker-compose.yml -f manifest/docker/docker-compose.test.yml up -d --no-build --wait`（迁移镜像已在前置构建阶段成功生成） | `PASS`：MySQL/Redis/Milvus/API/Worker/nginx/provider double healthy/started |
| Provider double | `go build ./manifest/test/provider-double` | `PASS` |
| Go affected unit tests | exact commands in the code block above | `PASS`；覆盖显式测试 Gate、四类 Tool wrapper Interrupt reservation、RuntimeHandler/Worker、预算 truth、Registry/Agent topology |
| Go package compile | `go test ./internal/ai/runtime ./internal/ai/workflow -run '^$' -count=1` | `PASS`：两包编译完成；`[no tests to run]` 只记编译，不冒充测试执行 |
| Go DSN-backed package suites | `go test ./internal/ai/runtime ./internal/ai/workflow -count=1` | `NOT RUN`：未向宿主测试进程提供 `SENTINELOPS_TEST_DSN`；真实数据库行为由上方 Compose + Playwright + 最终 MySQL 汇总证明 |
| Go vet | `go vet ./internal/ai/runtime ./internal/ai/workflow ./internal/ai/tools ./internal/ai/agent/plan_pipeline ./internal/ai/agent/ops_pipeline` | `PASS` |
| Test-only Gate contract | `TestTestOnlyMutationGatesRequireExplicitOptIn` | `PASS`：默认关闭，显式测试 opt-in 才打开 |
| Frontend lint | `cd web && npm run lint` | `PASS`：0 error，67 条既有 warning |
| Frontend build | `cd web && npm run build` | `PASS` |
| Real browser chain | `SENTINELOPS_E2E_BASE_URL=http://127.0.0.1:18080 npx playwright test tests/e2e/approval-flow.spec.ts tests/e2e/unknown-effect.spec.ts --project=chromium --workers=1` | `PASS`：4/4，29.5s |
| Final diff | `git diff --check` | `PASS` |

## Database Evidence

以下来自最终干净测试卷的只读汇总查询；只保留 ID、状态和计数，不保留 DSN、Authorization、Cookie、Secret、模型输入或 Effect request 明文。

| Scenario | Run status / last seq | Approval | Effects | Event assertions |
| --- | --- | --- | --- | --- |
| approval success | `succeeded / 36`（2 个 Run 均如此） | `approved / version 2` | `primary succeeded` + `nginx_reload succeeded` | `approval.decided=1`、`run.resumed=1`、`effect.started=2`、`effect.succeeded=2`、`run.completed=1`；terminal replay 只返回 1 个 Event |
| explicit reject | `failed / 23` | `rejected / version 2` | 无 Effect 行 | `approval.decided=1`、`effect.started=0`、`run.failed=1` |
| unknown external Effect | `canceled / 30` | `approved / version 2` | `primary unknown`，`resolution=accepted_unknown`，无 `effect.succeeded` | `approval.decided=1`、`run.resumed=1`、`effect.started=1`、`effect.unknown=1`、`run.parked=1`、`effect.resolved=2`（`still_unknown` 后 `accepted_unknown`）、terminal `run.failed(to_status=canceled)` |

保留 unknown Effect 为 `unknown` 是设计要求：admin 只接受不确定性并记录脱敏证据，绝不把外部副作用伪装成 succeeded；Run 通过 P08 完成 primitive 终止并释放 Session。

## Key Assertions

- 成功链真实经过 pending Approval → admin approve → Worker Resume → `block_ip` Primary Effect → `nginx_reload` Derived Effect → succeeded Run；两次 `effect.started` 和两次 `effect.succeeded` 均只出现一次。
- 同一 Approval 的重复 approve 返回成功但不追加第二个 `approval.decided`；相反 reject 返回 409；错误 proposal hash 返回 409；viewer approve 和 viewer `accept-unknown` 均返回 403。
- reject 不启动 Effect；unknown 在 admin 处置前保持 `parked`，处置后仅产生 `effect.resolved(accepted_unknown)` 与 canceled Run，不产生 `effect.succeeded`。
- SSE 从 terminal 边界重连只 replay terminal Event，不触发新的模型调用、Tool call 或 Effect。

## Unfinished / NOT RUN

- P39-P43：`PENDING / NOT RUN`，包括 Eval Dataset、Hosted CI、完整故障矩阵、真实供应商、发布、灰度和回滚。
- 完整 `go test -race ./...`、完整浏览器矩阵和 P43 全量门禁：按计划留到 P43，不据 P38 局部结果宣称整体完成。

## Rollback

通过本单元独立本地提交的普通反向提交恢复；验证只使用 throwaway `sentinelops-e2e` project，未修改 remote、共享数据库或开发/生产栈。
