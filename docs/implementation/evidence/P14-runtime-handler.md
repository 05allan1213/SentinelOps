# P14 唯一 RuntimeHandler safety foundation

- Status: `PASS`
- Started from: `2966cb0922fe2ee9481d50ec0bf5fa3d44d38810`；`main`；开工时工作树干净
- Spec references: 上位 Spec `3.4`、`6.3`、`6.8`、`6.9`、Task 3C；执行 Plan `P14`、`2.1～2.7`
- Actual files: `internal/ai/runtime/handler.go`、`handler_test.go`、`budget.go`、`snapshot.go`；`internal/ai/workflow/budget.go`、`p14_budget_test.go`；本证据文件
- Red test and expected failure: 先加入五类 Handler endpoint、动态 Policy/Scope/deadline、provider-qualified metadata、128 并发隔离和 MySQL Budget hard-stop/crash/CAS 测试；精确过滤命令因 P14 Handler、metadata 与 durable Budget API 尚不存在而预期编译失败，退出 1
- Local commands: P14 精确 race 门禁与 Case 枚举、`runtime` / `workflow` 两包未过滤回归、`go vet`、scoped `staticcheck`、`goimports`、`git diff --check`、fixture 清理和 Handler/Proxy/Store/Provider/Gate/Effect 禁止项扫描
- Results: `PASS`
- Key assertions: 唯一 stateless Handler 显式覆盖 Model 与四类 Tool wrapper；真实 Eino Agent 中第一个用户 Handler 为最外层；未知/L1/L2/Scope/deadline/缺失 typed Context 均在 endpoint 前失败；Model/L0 Tool 与 duration/deadline 由同一 MySQL Store pre-call reserve；crash/换租约不重置额度；provider-qualified 完整 Model Snapshot identity 不含 Secret；动态 L0 仍受 per-call Policy 包裹且不创建 Effect
- Deviations from recommended route: 推荐 middleware 与既有 Store 路线均采用；额外原位修改 `snapshot.go`，因为 P11 的旧 identity 未包含 Profile、Route Options 与 Pricing，不能满足 P14 对实际 Model Snapshot identity 的精确计量；未增加 Migration、Model Proxy、Tool instance wrapper 或第二套 Store
- Raw artifact references: 无
- Unfinished items: P14 无未完成实现；P15 builder、P20 Worker 终态分类、P22 Approval/Interrupt、P23～P26 Effect、P27 Retry/Failover、P28 全维 Budget、专业 Agent 接线、前端、镜像、在线供应商、Hosted CI、发布和 P43 全量门禁均 `NOT RUN`

## Boundary Audit

- 目标：建立唯一、无请求可变字段且并发安全的 Eino `RuntimeHandler`，显式覆盖 `WrapModel` 与四类 Tool endpoint；每次调用从 P11 typed `AttemptContext` 读取 Run、服务端 Identity/Scope、lease、deadline、Trace、provider-qualified Model Snapshot 和 durable Budget handle；在 endpoint 前以同一 `workflow.GORMStore` generation-fenced CAS 持久化 Model/L0 Tool 调用与 Run duration/deadline 预算，正常返回后 settle，崩溃时保留未结算 reservation；L1/L2 和未知 Tool 在 endpoint 前 fail-closed。
- 明确非目标：不实现 P15 Executor builder，不迁移或运行专业 Agent，不启用 durable Agent Gate，不实现 P22 Approval/Interrupt、不实现 P23～P26 Effect Ledger、不实现 P27 Retry/Failover/breaker、不补 P28 Token/Cost/MCP/RAG/Skill/并发/output 全维预算，不改 Migration、API/Worker/SSE、前端、Compose、镜像或发布配置。
- 兼容契约：继续使用 P03 的 `workflow_runs` 三个 Budget JSON 列、P08 Event catalog/seq 与 P09 lease generation；继续使用 P11 `AttemptContext`、Frozen Runtime Snapshot 和现有 Trace ID；P13 Catalog/Registry 是 Tool 风险与实例唯一真值；legacy Agent/Tool 路径不切流、不删除。
- 安全不变量：五个 endpoint 均必须先验证 typed Context、服务端 Identity/Scope、当前 lease、deadline、Trace 与 Budget；Model metadata 必须精确匹配 Frozen Snapshot 的 Catalog Ref/Provider/Driver/vendor Model ID/Profile/snapshot identity，且不包含 Secret；相同 reservation identity 幂等且不重复增加额度，未结算 reservation 在换租约后仍占用额度；未知 Tool 和 L1/L2 永不触碰 endpoint；Handler 不保存 currentRun/currentUser/currentBudget/currentLease/currentApproval；无进程内额度真值、Model Proxy、逐 Tool wrapper 或第二套 Store。
- 预计文件：`internal/ai/runtime/handler.go`、`handler_test.go`、`budget.go`；`internal/ai/workflow/budget.go`、`p14_budget_test.go`；本证据文件。Schema 已由 P03 完整预留，本单元不得新增 Migration。
- 验证方式：先运行执行 Plan 精确过滤命令保存预期 Red；实现后运行同一 race 门禁，枚举实际 Case，运行 runtime/workflow 直接回归、`go vet`、scoped `staticcheck`、`goimports`、差异检查，以及 Handler 字段/Model Proxy/逐 Tool wrapper/Gate/Provider 硬编码扫描。
- 回滚方式：通过本单元独立本地提交的普通反向提交恢复；Budget JSON 为同表 Expand 列内的版本化内容，不执行 Down、不修改 remote 或历史、不 push。

## Build-or-Reuse

| 现有 SentinelOps 能力 | 锁定版 Eino/Eino-ext 能力 | 剩余业务缺口 | 最薄实现及删除条件 |
| --- | --- | --- | --- |
| P03 已有 Run Budget JSON 列；P08 `workflow.GORMStore`、Event catalog/事务 seq；P09 generation fence；P11 typed `AttemptContext`、Frozen provider-qualified Model Snapshot、deadline/Trace/lease；P13 strict Registry 与服务端 Catalog；现有 `internal/ai/limiter` 和 MySQL Trace Callback | Eino v0.9.15 `adk.BaseChatModelAgentMiddleware`、`ChatModelAgentMiddleware`、`WrapModel` 与 invokable/streamable/enhanced 四类 Tool wrapper；官方 Handler 第一项为用户 wrapper 最外层，动态 Tool 在 call time 进入 wrapper；官方 Retry/Failover/Event Sender 顺序由框架维护 | SentinelOps 特有的服务端 Identity/Scope、Run deadline、generation-fenced durable call reservation、Catalog mutation-disabled、provider-qualified metadata 与 crash continuity | 一个直接实现官方 middleware 的 stateless Handler；同一 `GORMStore` 上增加最小 base reserve/settle primitive，并通过 P11 Budget handle 注入；不代理 Model、不包装 Tool 实例、不创建项目版 Middleware/Store。P28 只能扩展同一 JSON schema/identity/primitive；若 Eino 未来原生提供等价 MySQL durable business budget，保留 contract test 后收缩项目胶水 |

- Gate 判定：`PASS`。Eino 已完整拥有 middleware 生命周期和五个挂载点，但不拥有 SentinelOps 的 MySQL Run、lease、Scope、Catalog 或 crash-preserved Budget 语义；现有项目已拥有全部真值与列，只缺最薄的 durable reserve/settle 和统一调用接线。

## Red evidence

先加入计划要求的 Handler 五端点、动态 Policy/Scope/deadline、provider-qualified metadata、128 并发隔离，以及 MySQL base budget hard stop/crash reservation/CAS 测试，再执行：

```text
$ go test ./internal/ai/runtime ./internal/ai/workflow \
    -run 'Test(RuntimeHandler|HandlerConcurrentIsolation|DynamicToolPolicy|BaseBudget|BudgetCrashReservation)' -count=1
internal/ai/workflow/p14_budget_test.go:19:56: undefined: BaseBudgetLimits
internal/ai/workflow/p14_budget_test.go:24:28: store.ReserveBaseBudget undefined
internal/ai/runtime/handler_test.go:23:13: undefined: NewRuntimeHandler
internal/ai/runtime/handler_test.go:26:14: undefined: WithModelInvocation
FAIL SentinelOps/internal/ai/runtime [build failed]
FAIL SentinelOps/internal/ai/workflow [build failed]
```

- `PASS`（预期 Red）：命令退出 1，两个目标包因 P14 唯一 Handler、调用 metadata 和 durable base reserve/settle API 尚不存在而编译失败；不是环境错误、`[no tests to run]` 或已有实现直接变绿。

## Implementation result

- `RuntimeHandler` 只嵌入 Eino v0.9.15 `adk.BaseChatModelAgentMiddleware`，显式实现 `WrapModel` 与 invokable/streamable/enhanced 四类 Tool wrapper。struct 不保存任何 Run/User/Budget/Lease/Approval 可变请求状态；所有调用事实均从 typed Context 读取。
- 五类入口共享同一验证链：P11 `AttemptContext`、服务端 `Identity` / `Scope`、P09 lease token 与 generation、Run/Attempt、Trace、deadline、实现 durable call reservation 的 Budget handle。缺失或不一致均 fail-closed；五类 endpoint 的缺失 typed Context 负向测试计数均为 0。
- Tool 每次调用重新执行 P13 `RequireExecutable` 和 Frozen Tool Snapshot revision/schema 校验；未知 Tool、L1/L2、未冻结动态 Tool 均在 endpoint 前拒绝。真实 Eino `BeforeAgent` 注入的未知 ToolCall 被统一 wrapper 拒绝且 endpoint 计数为 0；冻结后动态 `trigger_ops` 正向通过统一 L0 reserve/settle，并断言 Catalog 没有 Effect type/steps。P14 没有 Approval/Effect 创建路径。
- `RuntimeHandlerFirst` 始终将唯一 Handler 放在用户 Handler 列表第一项。除切片顺序断言外，真实 `adk.NewChatModelAgent` + `Runner` contract test 让内层 probe 读取 RuntimeHandler 注入的 metadata：只有 RuntimeHandler 为用户 wrapper 最外层时才可通过；未改 Eino Retry/Failover/Event Sender 内部顺序。
- Model 调用必须显式携带稳定 reservation identity、Catalog Ref、Provider、Driver、vendor Model ID、Profile 与完整 candidate snapshot identity。Handler 逐字段匹配 Frozen Runtime Snapshot；同名跨 Provider 候选不会合并，篡改身份和常见 Secret 形态均在 endpoint 前拒绝。
- `ModelSnapshot.Identity()` 改为对完整 canonical candidate（含 Route Options 与 Pricing）加 domain-separated SHA-256；Frozen Snapshot 新增只返回深拷贝的 Model/Tool accessor，Handler 无法修改快照内部真值。
- `DurableBudget` 只是在 P11 `BudgetHandleFactory` 与现有 `workflow.GORMStore` 间重建 immutable Run handle，不复制额度到进程内。`ReserveBaseBudget` / `SettleBaseBudget` 在已有 `workflow_runs` Budget JSON 列和 P08 event/seq 事务中使用 P09 generation fence；没有新表、Migration、Store 或业务 Service。
- reserve 在 endpoint 前永久占用 Model/L0 Tool 次数；deadline 和 Run duration 使用 MySQL 时间，并取 durable Context Snapshot deadline 与本 Attempt 更早 deadline 的最小值。相同 identity/metadata 幂等读取但不能在 deadline/duration 后继续调用，冲突 metadata fail-closed，pending reservation 经硬崩溃和 lease handoff 仍占用额度；usage 与 reservation JSON 数量不一致也 fail-closed，不能通过篡改 usage 重置额度。
- 同步 endpoint 返回后 settle；流式 Model/Tool 只在完整消费、流错误或提前关闭后 settle。reserve/settle 使用保留 Context value、脱离调用取消且最长 5 秒的持久化 Context，避免 endpoint 返回/取消使已开始的 durable 记账无界丢失。
- 预算耗尽持久化 `budget.exhausted` 并阻止 endpoint。将其分类为 Worker error 并驱动 Run terminal transition 属于 P20，本单元没有越界实现。

## Local commands and results

### P14 计划精确门禁与 Case 枚举

```text
$ go test -race ./internal/ai/runtime ./internal/ai/workflow \
    -run 'Test(RuntimeHandler|HandlerConcurrentIsolation|DynamicToolPolicy|BaseBudget|BudgetCrashReservation)' -count=1
ok SentinelOps/internal/ai/runtime 1.311s
ok SentinelOps/internal/ai/workflow 10.100s

$ go test ./internal/ai/runtime ./internal/ai/workflow \
    -list 'Test(RuntimeHandler|HandlerConcurrentIsolation|DynamicToolPolicy|BaseBudget|BudgetCrashReservation)'
TestRuntimeHandlerCoversAllEndpoints
TestRuntimeHandlerRejectsPolicyScopeDeadlineBeforeEndpoint
TestHandlerConcurrentIsolation
TestDynamicToolPolicyAndRuntimeHandlerOrder
TestRuntimeHandlerFirstIsOutermostInEinoAgent
TestRuntimeHandlerModelMetadataUsesProviderQualifiedSnapshot
TestRuntimeHandlerModelStreamSettlesAfterConsumption
TestBaseBudgetPreCallHardStopAndSettlement
TestBudgetCrashReservationPreservedAcrossLeaseHandoff
TestBaseBudgetDeadlineDurationAndReservationIdentity
TestBaseBudgetConcurrentCAS
TestBaseBudgetRejectsTamperedDurableTruth
```

- `PASS`：12 个顶层 Case 被发现并由精确 race 命令实际执行，无 `[no tests to run]`。覆盖五 wrapper 正向与共同 fail-closed、真实 Eino wrapper 顺序、真实 `BeforeAgent` 动态 Tool、128 个隔离 Run、128 路 MySQL CAS、预算 hard stop、crash/lease handoff、identity conflict、durable truth tamper 和流式 settle。

### 直接受影响包回归

```text
$ go test ./internal/ai/runtime ./internal/ai/workflow -count=1
ok SentinelOps/internal/ai/runtime 0.432s
ok SentinelOps/internal/ai/workflow 15.579s
```

- `PASS`：两包全部未过滤测试通过，包括 P08～P13 既有 Store/lease/checkpoint/snapshot/recovery contract，证明完整 Model identity 与 Budget 扩展未破坏直接依赖回归。

### 静态、格式与源码唯一性

```text
$ go vet ./internal/ai/runtime ./internal/ai/workflow
PASS

$ staticcheck ./internal/ai/runtime ./internal/ai/workflow
PASS

$ goimports -l <P14 Go files>
无输出

$ git diff --check
PASS

$ docker compose -p sentinelops-p14 \
    -f manifest/docker/docker-compose.test.yml ps --all
无 service；fixture 已执行 down -v --remove-orphans

$ <Handler 字段 / Model Proxy / Tool wrapper / Store / Provider / Gate / Effect 禁止项扫描>
REQUEST_GLOBAL_HANDLER_FIELDS_ABSENT=PASS
MODEL_PROXY_ABSENT=PASS
PER_TOOL_POLICY_WRAPPER_ABSENT=PASS
SECOND_STORE_OR_MIGRATION_ABSENT=PASS
PROVIDER_NAME_BUSINESS_BRANCH_ABSENT=PASS
APPROVAL_EFFECT_PATH_ABSENT=PASS
manifest/config/config.yaml: agent_runtime.enabled=false
manifest/config/config.docker.yaml: agent_runtime.enabled=false
```

- `PASS`：生产 Handler/Budget 文件没有 `currentRun/currentUser/currentBudget/currentLease/currentApproval`、Model Proxy、Tool 实例 wrapper、Provider 名称硬分支、Approval/Effect 或新 Store/Schema 定义。出现的 `Provider` 比较只用于 Frozen Snapshot/metadata 全字段相等校验；`NewDurableBudget` 只持有既有 `*workflow.GORMStore`。
- 隔离测试使用 Compose project `sentinelops-p14`、动态 MySQL host port、P03 throwaway database 前缀和固定 goose v3.27.3；DSN 未写入证据或命令输出。每轮门禁后均执行 `down -v --remove-orphans`，最终 `ps --all` 为空；现有开发栈未触碰。
- Go 1.27 的测试枚举打印已有 Sonic 性能路径 warning 并回退标准 `encoding/json`；测试退出码与断言仍为 `PASS`，本单元未改依赖或工具链。
- 完整 `go test -race ./...`、前端、镜像、完整 E2E/Eval/故障矩阵、在线供应商、共享数据库、Hosted CI、发布和 P43 全量门禁均 `NOT RUN`；它们不属于 P14 局部门禁，也不据此声称整体二改通过。

## Key assertions

- Handler 只有一个类型与一个挂载顺序 helper；动态 Tool 不能绕过 per-call Catalog/Snapshot/Scope/Budget 检查。五类 endpoint 的所有安全失败都发生在原 endpoint 之前。
- Model/L0 Tool 次数与 duration/durable deadline 的唯一真值在 `workflow_runs` 现有 JSON 列；更早的 per-attempt deadline 也在同一 MySQL 事务以数据库时间执行 hard stop。进程重启、Resume/Replay 或 lease generation 变化不会清零 pending/settled reservation，也不能用相同 identity 改写调用 metadata 或在时限后绕过拒绝。
- Model metadata 使用完整 candidate snapshot identity，并保留 Catalog Ref/Provider/Driver/Model ID/Profile 用于审计；不按 Provider 名分支、不只按 vendor Model ID 计量、不持久化或传播可识别的 Secret。
- Eino 官方 Handler 第一项最外层规则由真实 Agent/Runner contract test 锁定；项目没有改写框架 Retry/Failover/Event Sender 组合。P27/P28 只能让每个 physical call 通过同一 Handler/identity/reservation primitive 扩展计量，不能另建 Proxy 或 Budget Store。
- L1/L2 当前只返回 P13 `POLICY_MUTATION_DISABLED`，endpoint 调用为 0；`trigger_ops` 是不带 Effect metadata 的 L0 Proposal Tool。Approval/Effect/Mutation 单一路径留给 P22～P26。
- local 与 Docker 生效配置均保持 `agent_runtime.enabled: false`；P14 只建立首次 durable Agent 运行前的 safety foundation，没有启动或迁移业务 Agent。

## Deviations from recommended route

- 推荐的 Eino middleware、typed Context、既有 `GORMStore` 与已有 Budget JSON 列路线全部采用；没有替代官方 Agent loop、Middleware、Tool、Store 或 Catalog。
- 计划预期文件之外只原位修改 `internal/ai/runtime/snapshot.go`。旧 `ModelSnapshot.Identity()` 只拼接 Catalog Ref/Provider/Driver/Model ID，无法区分同一候选的 Profile、Route Options 或 Pricing revision；P14 改为完整 canonical candidate hash，并提供深拷贝 accessor 供 Handler 做逐调用匹配。
- P14 只落地 Model/L0 Tool 次数与 Run duration/deadline。Token/Cost、Retry/Failover physical identity 构建、Planner/Replanner、MCP/RAG/Skill、并发和 output limits 必须由 P27/P28 原位扩展同一 reservation schema/identity；本单元不提前实现。
- Budget exhausted 只发出既有 catalog 中的 `budget.exhausted` 并返回 hard-stop error；P20 才负责 Worker error 分类和 Run 终态，P14 未跨单元补齐。

## Final scope statement

P14 局部门禁为 `PASS`。本结论只覆盖唯一 RuntimeHandler、五类 endpoint safety、provider-qualified Model metadata 和可崩溃保留的最小 MySQL Budget foundation；不代表 P15、durable 业务 Agent、Approval/Effect、P28 全维预算、整个二次开发或 P43 全量门禁完成。不得据此开始 P15。
