# P11 Runtime Snapshot、兼容哈希与 typed Context

- Status: `PASS`
- Started from: `02c48afe1f2bbedebebbff253736e9b41647911c`；`main`；开工时工作树干净
- Spec references: 上位 Spec `3.3`、`3.4`、`6.9`、`7.3`、Task 3B；执行 Plan `P11`、`2.1～2.7`
- Actual files: `internal/ai/runtime/snapshot.go`、`compatibility.go`、`context.go`、`p11_runtime_context_test.go`；`internal/ai/workflow/types.go`、`run.go`、`p08_run_store_test.go`、`p11_runtime_snapshot_test.go`；`internal/dao/mysql/model.go`；`internal/config/config.go` 及直接配置测试；`manifest/config/config.yaml`、`config.docker.yaml`；本证据文件。被忽略的有效 `manifest/config/config.local.yaml` 同步了 pricing revision，但按既有 Secret/本地配置契约不暂存。
- Unfinished items: P11 无未完成实现；P12 Resume / Replay / parked selector 与 cancel、P13 及后续单元、durable Agent、共享/生产数据库、镜像、在线供应商、Hosted CI、发布动作和 P43 全量门禁均 `NOT RUN`

## Boundary Audit

- 目标：定义并冻结 `Runtime Snapshot v1`，用统一 canonical JSON 计算精确兼容哈希并原子写入 P03 已有 `workflow_runs` 列；每个 Attempt 只从 MySQL Run 真值重建带 Run、Identity、Scope、Budget handle、P09 lease、Trace 与 deadline 的 typed Runtime Context；限制 Eino SessionValues 并提供不做隐式 FString 插值的显式 `GenModelInput`。
- 明确非目标：不实现 P12 Resume / Replay / parked selector 或 cancel，不创建 Runner/Agent/Runtime Store，不实现 P13 Registry、P14 RuntimeHandler 或 durable Budget 记账，不启用 durable Agent，不修改 Migration、Compose、前端或发布配置。
- 兼容契约：P08 唯一 `CreateRunWithSessionLock` 和 P09/P10 唯一 lease/checkpoint 身份原位扩展；legacy Run 保持隔离；v1 兼容只接受哈希精确相等；模型身份始终包含 provider-qualified Catalog Ref、Provider、Driver、厂商 Model ID、Profile、Route Options 和 pricing revision。
- 安全不变量：Snapshot、兼容哈希、SessionValues 与 Checkpoint 不含解析后的 Secret、API Key、DB/Redis/MCP handle、函数、channel、Tool/Model 实例或可变 Budget 对象；typed Runtime handle 不序列化；新 Attempt 保留 run/snapshot/budget identity，只更新 attempt/lease/trace。
- 预计文件：`internal/ai/runtime/context.go`、`snapshot.go`、`compatibility.go`、P11 测试，`internal/ai/workflow/run.go`、`types.go`、`internal/dao/mysql/model.go`，模型 pricing revision 配置与本证据文件。
- 验证方式：先运行 P11 精确过滤测试保存预期 Red；实现后运行执行 Plan 的精确门禁、直接受影响包既有测试、`go vet`、`staticcheck`、`goimports` 和 Secret/重复实现/SessionValues 禁止项扫描。
- 回滚方式：通过本单元独立本地提交的普通反向提交恢复；不执行 Schema Down，不修改 remote，不 push。

## Build-or-Reuse

- 现有 SentinelOps 能力：复用 P06 `policy.CanonicalJSON`、P08 `CreateRunWithSessionLock` 与 P03 已有 Snapshot 列、P09 `LeaseToken`、P10 context accessor、P04 `SecretRef` 和唯一 Provider → Model Catalog → Routing；不增加 Store、模型配置中心或 Secret Resolver。
- 锁定版 Eino/Eino-ext 能力：Session state 继续使用 Eino `WithSessionValues` 和 `planexecute` 四个官方 Session Key；ChatModelAgent 继续使用官方 `GenModelInput` 扩展点，P11 只提供 literal 输入函数供后续 Agent builder 显式接线，不包装 Agent/Runner。
- 剩余业务缺口：当前 Run 只写 runtime version/hash，P03 的 Agent/model/tool/MCP/Skill/prompt/policy/config/feature/budget 列尚未映射或写入；Context Snapshot 只有 History；runtime 包没有 immutable Snapshot、精确兼容校验、typed Context 或 SessionValues 安全入口；模型定价没有显式 revision。
- 最薄实现及删除条件：增加不可变 Frozen Snapshot、workflow 持久化 DTO、一次 Attempt builder、严格 allowlist 和 literal `GenModelInput`；官方未来若提供覆盖项目 MySQL Snapshot/lease/identity 语义的等价 typed Context，保留 contract test 后替换项目胶水。

## Red evidence

先加入计划要求的 P11 测试，再执行：

```text
$ go test ./internal/ai/runtime ./internal/ai/workflow -run 'Test(RuntimeSnapshot|Compatibility|TypedContext|SessionValues|Attempt)' -count=1
internal/ai/runtime/p11_runtime_context_test.go:23:16: undefined: FreezeRuntimeSnapshot
internal/ai/runtime/p11_runtime_context_test.go:41:16: undefined: RuntimeSnapshotInput
internal/ai/workflow/p11_runtime_snapshot_test.go:18:8: input.RuntimeSnapshot undefined
internal/ai/workflow/p11_runtime_snapshot_test.go:18:26: undefined: RuntimeSnapshotFields
internal/ai/workflow/p11_runtime_snapshot_test.go:44:10: stored.AgentRevision undefined
FAIL SentinelOps/internal/ai/runtime [build failed]
FAIL SentinelOps/internal/ai/workflow [build failed]
```

- `PASS`（预期 Red）：命令退出 1，两个目标包因 P11 Snapshot、workflow 持久化字段、typed Context 与 SessionValues API 尚不存在而编译失败；不是 MySQL/凭据环境错误、`[no tests to run]` 或已有实现直接变绿。

## Implementation result

- `FreezeRuntimeSnapshot` 复用 P06 `policy.CanonicalJSON`，按 `sentinelops/runtime-compatibility/v1` domain 冻结 Snapshot v1。Snapshot 覆盖 Go/Eino/app、Agent、Prompt、Policy/config、provider-qualified model identity、candidate order、Route Options、pricing revision/费率、Tool revision/schema、MCP catalog、Skill 内容和九个完整 Feature Gate；切片、map 与 `EnableThinking` 指针均先复制再规范化，调用方后续修改不影响冻结值。
- 模型快照只从当前唯一 `Config.Resolve` 的 Provider → Model Catalog → Routing 构造，保存 Catalog Ref、Provider、有限 Driver、厂商 Model ID、Profile、候选顺序/Options 与 pricing revision。两个 Provider 使用相同厂商 Model ID 时 identity 仍不同；结构中没有 `SecretRef`、Endpoint、API Key 或解析后的 Secret。
- `CreateRunWithSessionLock` 不再接受可彼此漂移的裸 runtime version/hash，而是接收同一 Frozen Snapshot 拆出的 `RuntimeSnapshotFields`，在原事务中写入 P03 已有 Agent/model/tool/MCP/Skill/prompt/policy/config/feature/budget 列。没有修改 Migration、Store 数量或 claim 条件。
- `context_snapshot_json` 现在是版本化 envelope：服务端认证后的 Identity/Role/Scope、最新 MySQL Session Revision History、Budget limits 和 deadline。`BuildAttemptContext` 只从 `ClaimedRun` 数据库真值重建，校验 durable/running、P09 owner/generation、Run owner、Budget 三段 JSON、Snapshot 精确 Hash，再由唯一 Budget service factory 重建非序列化 handle并创建新 Trace ID/deadline context；P09 `LeaseTokenFromContext` 仍是唯一租约 accessor。
- `RequireExactCompatibility` 的 v1 规则只有完整 SHA-256 精确相等。Snapshot 任一受保护字段变化都会产生不同 Hash；存储列重建后的 Hash 不相等直接返回 `ErrRuntimeIncompatible`，P12 才负责对应 parked 状态转换。
- `SafeSessionValues` 只接受 `planexecute` 四个官方 Session Key 的官方类型，以及 Run ID/runtime version/compatibility hash 三个非空 immutable string。未知键、DB/函数/channel/Secret 名称和自定义 map/slice 均 fail-closed；运行时 Budget/Trace/lease/Identity handle 不进入 SessionValues 或 Checkpoint。
- `LiteralGenModelInput` 直接使用 Eino `ChatModelAgent.GenModelInput` 扩展点，只按字面量追加 System instruction 与输入消息，完全不读取 SessionValues，因此 JSON 花括号和任意 SessionValues 不会触发默认 FString 隐式插值。P11 尚未创建 durable ChatModelAgent；后续 builder 必须显式接入该函数。
- `Pricing.Revision` 成为 Catalog 必需字段；跟踪的 local/Docker 配置和当前被忽略的完整 local 配置均使用核对日期 `2026-08-24`，直接配置/模型/Embedding 测试夹具同步。没有读取、打印或暂存本地 Secret。

## Local commands and results

MySQL Case 只连接本机 `dev-mysql`，并由测试逐个创建/清理 `sentinelops_p03_*` 一次性数据库；凭据只在进程环境中解析和注入，未进入命令输出、证据或工作树。goose 使用已核验的 `/tmp/sentinelops-p07-bin/goose v3.27.3`。

### P11 计划局部门禁

```text
$ go test ./internal/ai/runtime ./internal/ai/workflow \
    -run 'Test(RuntimeSnapshot|Compatibility|TypedContext|SessionValues|Attempt)' -count=1
ok SentinelOps/internal/ai/runtime 0.220s
ok SentinelOps/internal/ai/workflow 4.683s

$ go test ./internal/ai/runtime ./internal/ai/workflow \
    -list 'Test(RuntimeSnapshot|Compatibility|TypedContext|SessionValues|Attempt)'
TestRuntimeSnapshotCompatibilityStableAndSensitive
TestRuntimeSnapshotProviderQualifiedIdentityAndSecretsExcluded
TestTypedContextRebuildsAttemptFromDatabaseTruth
TestAttemptPreservesRunSnapshotAndRotatesAttemptTrace
TestSessionValuesAllowlistRejectsHandlesSecretsAndMutableValues
TestSessionValuesDoNotImplicitlyFormatLiteralInstruction
TestRuntimeSnapshotPersistenceAndContextEnvelope
ok SentinelOps/internal/ai/runtime
ok SentinelOps/internal/ai/workflow
```

- `PASS`：7 个顶层 Case 均被发现并实际执行，无 0 tests。覆盖 canonical 稳定性、计划列出的每个 compatibility 维度、Provider 碰撞、Secret 排除、MySQL 列与 Context envelope 持久化、Identity/Scope/Budget/lease/Trace/deadline 重建、新 Attempt 不变/变化边界、SessionValues 负向用例和 literal Prompt。

### 直接受影响回归

```text
$ go test ./internal/ai/workflow ./internal/dao/mysql -count=1
ok SentinelOps/internal/ai/workflow 157.015s
ok SentinelOps/internal/dao/mysql 10.853s

$ go test ./internal/ai/runtime ./internal/config ./internal/bootstrap \
    ./internal/ai/models ./internal/ai/embedder -count=1
ok SentinelOps/internal/ai/runtime 0.233s
ok SentinelOps/internal/config 0.228s
ok SentinelOps/internal/bootstrap 0.354s
ok SentinelOps/internal/ai/models 0.301s
ok SentinelOps/internal/ai/embedder 0.403s
```

- `PASS`：P08 原子 Run/Event/Session、P09 lease/generation、P10 opaque Checkpoint/跨进程恢复、Migration/DAO、配置完整替代加载、启动校验和当前 Chat/Embedding 构造回归均通过。MySQL JSON 的规范化空格不被误判为 Snapshot 语义变化。

### 静态、格式与源码唯一性

```text
$ go vet ./internal/ai/runtime ./internal/ai/workflow ./internal/dao/mysql \
    ./internal/config ./internal/bootstrap ./internal/ai/models ./internal/ai/embedder
PASS

$ staticcheck <同一包集合>
PASS

$ goimports -l <P11 Go files>
无输出

$ git diff --check
PASS

$ <P11 Secret、handle、Store、deprecated Register、Migration 与 pricing revision 扫描>
P11_STATIC_AND_UNIQUENESS=PASS
```

- `PASS`：P11 生产 runtime 源码不存在 API Key、SecretRef、Secret Resolver、GORM/Redis、`context.Background` 或 `isolateCtx`；`AttemptContext`、literal `GenModelInput` 和全仓 `GORMStore` 均各只有一个；runtime/workflow 不存在 `schema.RegisterName` 或 deprecated `compose.RegisterSerializableType`；Migration 差异为 0；跟踪配置 6 个和本地有效配置 3 个 model 均有 pricing revision。
- 前端、镜像、完整 `go test -race ./...`、在线供应商、共享数据库、Hosted CI、发布和 P43 全量门禁均未运行，不属于 P11 局部门禁，均不据此宣称整体通过。

## Key assertions

- v1 compatibility 只接受从同一规范化 Snapshot 重建出的精确 Hash；不按模型厂商 ID、字段子集或当前动态配置猜测兼容。
- Frozen Snapshot 和 workflow 持久化列不含 Secret；运行时 Budget/Trace/DB 等 handle 只存在 typed Go Context，不进入 Snapshot、SessionValues 或 Eino Checkpoint。
- 每次 Attempt 沿用 run_id、完整 Snapshot 和持久化 Budget state，使用数据库中的新 attempt、P09 generation/owner，并新建 trace_id；不复用上一进程内存对象。
- 当前仍未运行任何 durable 业务 Agent，也没有实现恢复选择、Runner 入口或 parked 状态转换。

## Deviations from recommended route

- 推荐的三个 runtime 文件落点保持不变；为避免 runtime 导入 Store 或 workflow 反向依赖 runtime，在 `workflow/types.go` 增加纯持久化 DTO，Frozen Snapshot 通过 `WorkflowFields` 拆列，唯一 `GORMStore` 仍只负责事务与形状校验。
- P11 新类型没有直接放入 Eino SessionValues/Checkpoint，因此没有为了满足形式而调用 `schema.Register[T]()`；SessionValues 只保留 Eino 官方状态和三个 immutable string。后续若新增确需 Resume 的项目类型，仍按 P10 契约使用 `schema.Register[T]()`。
- 为使 pricing revision 真正来自 Catalog 而非调用点硬编码，P11 在现有 `Pricing` 原位增加必需字段并同步 local/Docker 有效配置；没有创建第二套 pricing 或模型配置。

## Final scope statement

P11 局部门禁为 `PASS`。本结论只覆盖 Runtime Snapshot v1、精确 compatibility hash、现有 workflow 列持久化、MySQL typed Attempt Context 重建、SessionValues allowlist、literal `GenModelInput` 与 pricing revision；不代表 P12 恢复选择、durable Agent、Approval/Effect、整个二次开发或 P43 全量门禁完成。
