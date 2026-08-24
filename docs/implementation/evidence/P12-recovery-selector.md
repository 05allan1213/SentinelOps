# P12 Resume / Replay / parked 选择与 cancel

- Status: `PASS`
- Started from: `f52b78d02b857d37c8e0ae2af61a53b147085044`；`main`；开工时工作树干净
- Spec references: 上位 Spec `3.3`、`3.4`、`6.1`、`6.2`、Task 3B；执行 Plan `P12`、`2.1～2.7`
- Actual files: `internal/ai/runtime/recovery.go`、`runner.go`、`cancel.go`、`p12_recovery_test.go`；`internal/ai/workflow/recovery.go`、`p12_recovery_test.go`；本证据文件
- Red test and expected failure: 先加入 P12 selector / Runner / cancel / Store 行为测试；精确门禁因 `RecoveryFacts`、`LoadRecoveryFacts`、`SelectRecovery`、Runner/cancel 接线等 P12 符号尚不存在而预期编译失败，退出 1
- Local commands: P12 精确测试与 Case 枚举、filtered race、`runtime` / `workflow` 两个直接受影响包全量测试、`go vet`、`staticcheck`、`goimports`、`git diff --check` 和源码唯一性/禁止项扫描
- Results: `PASS`
- Key assertions: 数据库真值唯一决定 Resume / Replay / parked；Runner 只调用 Eino 官方 API；safe-point 交接要求当前 generation 的 fenced checkpoint；`effect_unknown`、损坏依赖和 lost lease 均 fail-closed
- Deviations from recommended route: 文件落点与推荐路线一致；为证明合法 SHA 但非法 Eino gob 也会 parked，runtime 测试增加了复用同一 migrations 的一次性 MySQL 库；无生产 Schema、依赖或后续单元改动
- Raw artifact references: 无
- Unfinished items: P12 无未完成实现；P13 及后续单元、durable 业务 Agent、SSE/API/Worker poll loop、前端、镜像、在线供应商、Hosted CI、发布动作和 P43 全量门禁均 `NOT RUN`

## Boundary Audit

- 目标：在 P10 唯一 generation-fenced Eino CheckPointStore 和 P11 immutable Runtime Snapshot / typed Attempt Context 上，实现确定性的 Resume / Replay / parked selector、官方 Eino Runner 薄调用入口、结构化 Run 恢复 Event，以及 recursive safe-point / lost-lease cancel。
- 明确非目标：不实现 P13 Registry、P14 RuntimeHandler、P15 Executor builder、Approval/Effect 业务 primitive、API/Worker poll loop、SSE、durable 业务 Agent、前端、Compose、镜像或发布；不解析 Eino opaque checkpoint，不自研 Runner/Resume 协议或 Agent Loop。
- 兼容契约：同一 `run_id` 保持不变；P09 claim 产生的新 attempt / lease generation 与 P11 新 trace_id 用于本次调用；Budget、Context、immutable input 和 frozen Runtime Snapshot 不重置；v1 只接受精确 compatibility hash；恢复选择不接受 Controller、SSE 或客户端覆盖。
- 安全不变量：只有完整、SHA-256 正确、元数据与 Run 一致且不来自未来 generation 的 opaque checkpoint 才可 Resume；只有从未提交 checkpoint 且无已发布 Approval / Effect 恢复依赖时才可 Replay；损坏依赖 fail-closed parked；`effect_unknown` 不由恢复 selector 解锁；lost lease 立即 recursive cancel，所有后续 Store 写继续由 P09/P10 fence 拒绝。
- 预计文件：`internal/ai/runtime/runner.go`、`recovery.go`、`cancel.go` 与 P12 测试；`internal/ai/workflow/recovery.go` 与 P12 Store 测试；本证据文件。
- 验证方式：先运行 P12 精确过滤测试保存预期 Red；实现后运行执行 Plan 精确门禁、两个直接受影响包既有测试、`go vet`、`staticcheck`、`goimports`、差异与禁止项扫描。
- 回滚方式：通过本单元独立本地提交的普通反向提交恢复；不执行 Migration Down，不修改 remote，不 push。

## Build-or-Reuse

| 现有 SentinelOps 能力 | 锁定版 Eino/Eino-ext 能力 | 剩余业务缺口 | 最薄实现及删除条件 |
| --- | --- | --- | --- |
| P08 `TransitionRunWithEvent` / Event catalog、P09 lease generation fence、P10 `GORMStore` 上的 opaque `CheckPointStore`、P11 exact hash / typed Context / SessionValues allowlist | Eino v0.9.15 `Runner`、`RunnerConfig.CheckPointStore`、`Query`、`Resume`、`ResumeWithParams`、`WithCheckPointID`、`WithCancel`、`CancelAfterChatModel`、`CancelAfterToolCalls`、`CancelImmediate`、`WithRecursive` | 数据库真值到三分支选择、checkpoint 元数据完整性与依赖校验、fenced 恢复 Event、immutable Query 接线、安全交接确认 | 组合官方 `Runner` 的具体函数与现有 `GORMStore`；不定义项目 Runner 接口、不解析 checkpoint；若官方未来直接覆盖 MySQL dependency / park_reason / fenced Event 语义，则保留 contract test 后删除对应胶水 |

- Gate 判定：`PASS`。官方 Eino 完整覆盖执行、Resume 与 cancel 生命周期，但不拥有 SentinelOps 的 MySQL Run/Approval/Effect 真值、compatibility hash、park_reason 和 generation-fenced Event；现有 Store 覆盖持久化与 fencing，但尚无确定性恢复选择和官方 Runner 调用接线。剩余实现是业务胶水，不需要 fork Eino、第二套 Store/Runner/Loop 或新依赖。

## Red evidence

先加入计划要求的 P12 行为测试，再执行：

```text
$ go test ./internal/ai/runtime ./internal/ai/workflow \
    -run 'Test(RecoverySelector|Resume|Replay|Parked|RecursiveCancel)' -count=1
internal/ai/runtime/p12_recovery_test.go:20:23: undefined: workflow.RecoveryFacts
internal/ai/runtime/p12_recovery_test.go:31:44: undefined: workflow.RecoveryModeResume
internal/ai/workflow/p12_recovery_test.go:19:22: store.LoadRecoveryFacts undefined
internal/ai/workflow/p12_recovery_test.go:57:18: store.RecordRecoverySelection undefined
FAIL SentinelOps/internal/ai/runtime [build failed]
FAIL SentinelOps/internal/ai/workflow [build failed]
```

- `PASS`（预期 Red）：命令退出 1，两个目标包因 P12 的恢复事实、selector、Runner/cancel 和 fenced Store API 尚不存在而编译失败；不是 MySQL/凭据错误、`[no tests to run]` 或已有实现直接变绿。

## Implementation result

- `workflow.RecoveryFacts` 在当前 generation 的 running lease 行锁内读取 immutable input、Run runtime identity、Eino checkpoint 元数据、已发布 Approval 和任意 Effect 依赖；不接受 Controller、SSE 或客户端 recovery mode。Checkpoint 只校验确定性 ID/key/row ID、非 NULL opaque bytes、SHA-256、runtime version/hash、committed/expiry 与 generation，不解析 bytes。
- `SelectRecovery` 是纯函数：当前 hash 不精确相等一律 `runtime_incompatible` parked；元数据完整的 checkpoint 才 Resume；只有从未提交 checkpoint、无已发布 Approval/Effect 且 immutable query 与 frozen `query_text` 一致时才 Replay；缺失依赖或损坏 checkpoint 分别 `checkpoint_missing` / `checkpoint_corrupt` parked；已有 `effect_unknown` 永不解锁。
- `NewDurableRunner` 直接把 P10 `GORMStore` 作为官方 `adk.CheckPointStore` 注入 `adk.RunnerConfig`。`InvokeRecoveryRunner` 只调用 `Query`、`Resume` 或 `ResumeWithParams`，统一携带 `WithCheckPointID`、P11 allowlisted SessionValues 和 `WithCancel`；没有项目 Runner interface、Runnable bridge、Agent Loop 或 checkpoint codec。
- `StartRecovery` 先读取/选择并 fenced 写 `run.resumed`、`run.replayed` 或 `run.parked`，Event 携带 `run_id` 关联、attempt、lease_generation、trace_id 和 runtime_version。合法元数据但 Eino gob 反序列化失败时，先保留 Resume 尝试 Event，再原子转为 `checkpoint_corrupt` parked。
- `RestoreRuntimeCompatible` 只允许当前 hash 为 64 位小写 SHA-256 且与 Run 精确相等、park_reason 为 `runtime_incompatible`、恢复依赖仍满足 Resume/Replay 的当前 lease CAS 回 pending；同时清除旧 lease，使后续 claim 必须产生新 attempt/generation。`checkpoint_missing/corrupt` 和 `effect_unknown` 没有该解锁入口。
- `RequestDrain` 对模型边界固定 `CancelAfterChatModel`，对 Tool 边界固定 `CancelAfterToolCalls`，两者都带有界 `WithAgentCancelTimeout` 和 `WithRecursive`；`RequestLostLeaseCancel` 固定 `CancelImmediate + WithRecursive`。`ConfirmSafePointHandoff` 只有在 cancel 成功且 P10 checkpoint 属于当前有效 generation 时通过，否则不允许交接。
- 测试使用 Eino 公共 Agent/ChatModel/AgentTool/Runner 类型：覆盖 `ResumeWithParams`、immutable Query Replay、两种 safe-point mode、lost-lease immediate cancel、recursive AgentTool checkpoint 跨 Runner 从嵌套子 Agent 恢复，以及 MySQL 上的真实 fenced Event/park/unpark/handoff；测试 double 只存在于 `_test.go`。

## Local commands and results

MySQL Case 只连接本机 `dev-mysql`，每个 Case 创建并清理 `sentinelops_p03_*` 一次性数据库；DSN 在进程内从容器环境构造且未输出，session `loc=UTC` 与 MySQL `CURRENT_TIMESTAMP` 时区一致。Migration 使用已核验的 `/tmp/sentinelops-p07-bin/goose v3.27.3`。

### P12 计划局部门禁

```text
$ go test ./internal/ai/runtime ./internal/ai/workflow \
    -run 'Test(RecoverySelector|Resume|Replay|Parked|RecursiveCancel)' -count=1
ok SentinelOps/internal/ai/runtime 6.880s
ok SentinelOps/internal/ai/workflow 21.282s

$ go test ./internal/ai/runtime ./internal/ai/workflow \
    -list 'Test(RecoverySelector|Resume|Replay|Parked|RecursiveCancel)'
TestRecoverySelectorChoosesResumeReplayOrParked
TestResumeAndReplayUseOfficialRunnerAndSafeSessionIdentity
TestParkedOnOpaqueCheckpointDecodeFailure
TestRecursiveCancelUsesSafePointsAndImmediateOnLostLease
TestRecursiveCancelPropagatesThroughAgentTool
TestRecoverySelectorFactsComeOnlyFromDurableTruth
TestRecoverySelectorPersistsFencedResumeReplayAndParkedEvents
TestRecoverySelectorRuntimeRestoreRequiresExactHashAndValidDependency
TestRecursiveCancelHandoffRequiresCurrentGenerationCheckpoint
ok SentinelOps/internal/ai/runtime
ok SentinelOps/internal/ai/workflow
```

- `PASS`：9 个顶层 Case 均被发现并实际执行，无 0 tests。覆盖三分支、opaque decode failure、Query/Resume/ResumeWithParams、Event、exact restore、safe-point/lost-lease cancel、fenced handoff 和嵌套 AgentTool recursive resume。

### Race 与直接受影响回归

```text
$ go test -race ./internal/ai/runtime ./internal/ai/workflow \
    -run 'Test(RecoverySelector|Resume|Replay|Parked|RecursiveCancel)' -count=1
ok SentinelOps/internal/ai/runtime 7.860s
ok SentinelOps/internal/ai/workflow 21.033s

$ go test ./internal/ai/runtime ./internal/ai/workflow -count=1
ok SentinelOps/internal/ai/runtime 2.813s
ok SentinelOps/internal/ai/workflow 97.729s
```

- `PASS`：P12 并发 cancel / AgentTool / Store 测试在 race detector 下通过；两个直接受影响包的全部 P07～P12 既有测试与新增测试通过。
- 环境诊断记录：第一次全包回归误用 `loc=Local`，MySQL 服务器为 UTC，导致 P09 `available_at` 相对 `CURRENT_TIMESTAMP` 快 8 小时并出现一个 `FAIL`；核对 host/MySQL 时钟后改为 `loc=UTC`，单独复核该 P09 Case 及上述最终全包均 `PASS`。没有为此修改 P09 源码或测试。

### 静态、格式与源码唯一性

```text
$ go vet ./internal/ai/runtime ./internal/ai/workflow
PASS

$ staticcheck ./internal/ai/runtime ./internal/ai/workflow
PASS

$ goimports -l <P12 Go files>
无输出

$ git diff --check
PASS

$ <P12 Runner/Store/checkpoint codec/client import/依赖/Schema 禁止项扫描>
P12_STATIC_AND_UNIQUENESS=PASS
GORM_STORE_COUNT=1
```

- `PASS`：生产源码没有项目 Runner interface、第二个 `GORMStore`、checkpoint gob/JSON 解析、Controller/SSE/API import；`go.mod`、`go.sum`、migrations 和 DAO model 无差异；P12 没有新增依赖、Schema、HTTP 路径或后续单元实现。
- Sonic 在 Go 1.27 上打印其已有的性能路径 warning 并回退标准 `encoding/json`；测试退出码和断言均为 PASS，此 warning 不改变 P12 结果。
- 前端、镜像、完整 `go test -race ./...`、完整 E2E/Eval/故障矩阵、在线供应商、共享数据库、Hosted CI、发布和 P43 全量门禁均未运行，不属于 P12 局部门禁，均不据此宣称整体通过。

## Key assertions

- 恢复选择只看 MySQL immutable Run / checkpoint / Approval / Effect 真值和当前部署 Runtime hash；没有 Controller/SSE/client mode 参数，也不读取前一进程 handle。
- Resume / Replay 沿用同一 run_id、Budget/Context/Runtime Snapshot identity；新 claim 的 attempt、lease_generation 和 P11 trace_id 写入 Event/Runner Context，不在 P12 重置持久化状态。
- Checkpoint bytes 对项目保持 opaque；元数据欺骗、SHA/identity/generation 不一致或官方 Runner 解码失败全部 fail-closed parked。
- `runtime_incompatible` 只有 exact hash + 有效恢复依赖才能回到可重新 claim 的 pending；`checkpoint_missing/corrupt` 默认留给后续 admin 证据化处置或取消，`effect_unknown` 只能走 Effect reconciliation。
- safe-point `Wait` 本身不代表可交接；只有当前有效 generation 的 P10 fenced Set 被 `RequireCommittedCheckpoint` 验证后才允许 handoff。lost lease 使用 immediate recursive cancel，P09/P10 继续拒绝旧 token 写入。
- 当前只运行测试 Agent；生产 durable 业务 Agent 仍未启用，符合 Task 3C 完成前禁用 Runner 业务调用的边界。

## Deviations from recommended route

- 推荐文件全部采用。为避免 runtime 建立 Store abstraction，恢复数据库事实和原子 Event 留在同一 `workflow.GORMStore`；runtime 直接接收具体 Store 和官方 `*adk.Runner`，没有新增项目接口。
- P12 测试额外使用一次性 MySQL 数据库证明“元数据有效但 opaque Eino gob 损坏”路径；它复用 P03 migrations 与既有隔离/清理原则，不创建 Compose、Migration 或生产测试后门。
- 全包回归首次 DSN 时区注入错误已如实记录并纠正为数据库 UTC；源码无偏差。

## Final scope statement

P12 局部门禁为 `PASS`。本结论只覆盖 Resume / Replay / parked selector、官方 Eino Runner 调用、恢复 Event、runtime-compatible 解锁与 recursive cancel/fenced handoff；不代表 P13、durable 业务 Agent、Approval/Effect、整个二次开发或 P43 全量门禁完成。
