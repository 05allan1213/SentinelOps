# P15 planexecute 薄 Executor builder

- Status: `PASS`
- Started from: `7560a9dd164cf1c17d40dd51ca0c32f785cd6f66`；`main`；开工时工作树干净
- Spec references: 上位 Spec `3.2`、`3.4`、Task 3C；执行 Plan `P15`、`2.1～2.7`
- Actual files: `internal/ai/agent/plan_pipeline/executor_adk.go`、`executor_test.go`；本证据文件
- Red test and expected failure: 先加入 `TestExecutorBuilder`、`TestExecutorSessionKeys`、`TestPlanTopology`；精确过滤命令因 P15 builder config、构造函数和 Session 输入函数尚不存在而预期编译失败，退出 1
- Local commands: P15 精确门禁与 Case 枚举、目标包未过滤回归、`go vet`、scoped `staticcheck`、`goimports`、`git diff --check`、官方拓扑/Session key/Tool 构造/Gate 源码扫描
- Results: `PASS`
- Key assertions: 新 builder 只用 `adk.NewChatModelAgent`；输入逐消息等于官方 `planexecute.NewExecutor` golden；只消费官方 Session key allowlist；OutputKey、20 次上限与空 ReturnDirectly 保持；Tool 只由 P13 `GetManyRequired` 与调用方官方 AgentTool 输入组成；P14 RuntimeHandler 必填且最外层；总拓扑仍由 `planexecute.New` 创建；durable Gate 仍关闭
- Deviations from recommended route: 推荐新增 `executor_adk.go` 路线已采用；为保护 P17～P19 尚未迁移的现有专业 Worker 和 legacy 可达路径，本单元没有替换旧 `NewExecutor`，只新增 durable builder contract，后续专业 Agent 组装才能消费；官方 default formatter 未导出，因此仅在 GenModelInput 内按官方 golden 组合现有 `ExecutionContext` 与导出的 `ExecutorPrompt`，没有复制 Prompt、Session key 或执行循环
- Raw artifact references: 无
- Unfinished items: P15 无未完成实现；P16 共享检索、P17～P19 专业 Agent/AgentTool 迁移、P20 durable Worker、P22 Approval/Interrupt、P23～P26 Effect、P27 Retry/Failover、P28 完整 Budget、前端、镜像、在线供应商、Hosted CI、发布与 P43 全量门禁均 `NOT RUN`

## Boundary Audit

- 目标：保留 Eino v0.9.15 官方 `planexecute.NewPlanner` / `NewReplanner` / `New` 总拓扑；为官方 `ExecutorConfig` 缺少 `Handlers` 的事实缺口增加一个最薄 `adk.NewChatModelAgent` builder。builder 只读取官方 `planexecute` Session key，以官方 `ExecutorPrompt` 组装 allowlist 输入，固定官方 `ExecutedStepSessionKey`、当前 20 次上限与空 `ReturnDirectly`，并将 P14 唯一 `RuntimeHandler` 置于第一个用户 Handler。
- 明确非目标：不迁移专业 Agent 或共享检索，不创建 AgentTool inventory、不删除 legacy `planexecute.NewExecutor` 调用路径，不运行 durable 业务 Agent，不实现 Retry/Failover、Approval/Effect、API/Worker/SSE、前端、Compose、镜像或发布能力，不启用 durable Agent Gate。
- 兼容契约：legacy `BuildPlanAgent` 继续保持当前可达行为；durable builder 只接收由 P13 `GetManyRequired` 解析的 Registry 名称和调用方用官方 `adk.NewAgentTool` 构造的 Agent Tool；P17～P19 只能组装专业 Agent 后消费同一 builder，不能复制 Executor 或 ReAct Loop。
- 安全不变量：builder 不直接实例化已注册 Tool，不复制四个 Session key 字符串、不读取任意 SessionValues、不创建 Runner/Loop；`RuntimeHandler` 必须非空且保持最外层；durable Gate 继续关闭；源码不得出现项目版 Execute-Replan Loop。
- 预计文件：新增 `internal/ai/agent/plan_pipeline/executor_adk.go`、`executor_test.go` 和本证据文件；除非 Red/编译证据证明必要，不修改 Planner、Replanner、总拓扑、配置或依赖。
- 验证方式：先运行计划精确过滤命令保存 Red；实现后运行同一命令、目标包未过滤测试、`go vet`、`staticcheck`、`goimports`、源码禁止项扫描、Gate 扫描与 Git 差异检查。
- 回滚方式：通过本单元独立本地提交的普通反向提交移除薄 builder 与 contract test；不修改 remote、不改写历史、不 push。

## Build-or-Reuse

| 现有 SentinelOps 能力 | 锁定版 Eino/Eino-ext 能力 | 剩余业务缺口 | 最薄实现及删除条件 |
| --- | --- | --- | --- |
| P13 唯一 Tool Registry 与 `GetManyRequired`；P14 唯一 stateless `RuntimeHandler` 及 `RuntimeHandlerFirst`；现有 legacy `NewExecutor` / `BuildPlanAgent` 仍由官方 Plan-Execute 组件运行 | Eino v0.9.15 `planexecute.New` / `NewPlanner` / `NewReplanner` / `ExecutorPrompt`、四个官方 Session key、`adk.NewChatModelAgent` / `NewAgentTool`；官方 `ExecutorConfig` 只有 Model、ToolsConfig、MaxIterations、GenInputFn，没有 Handlers | durable Executor 必须挂 P14 Handler，同时保持官方 Session、Prompt、OutputKey、iteration 和 Tool 语义；P15 尚不能越界迁移 P17～P19 专业 Agent | 一个只组装 `adk.ChatModelAgentConfig` 的 builder：Registry 名称交给 `GetManyRequired`，Agent Tool 由调用方提供，Handler 通过 `RuntimeHandlerFirst` 注入；不实现 Run/Loop。官方 `ExecutorConfig` 暴露等价 Handler 接口后删除 builder 并保留 contract test |

- Gate 判定：`PASS`。Eino 已拥有完整 Agent 与 Plan-Execute-Replan Loop，SentinelOps 已拥有 Tool/Handler 真值；唯一缺口是官方 `ExecutorConfig` 无法传入 Handler，允许最薄配置胶水，不允许 fork 或复制 Loop。

## Red evidence

先加入计划要求的 builder、官方 Session key/allowlist golden 与官方总拓扑 contract test，再执行：

```text
$ go test ./internal/ai/agent/plan_pipeline \
    -run 'Test(ExecutorBuilder|ExecutorSessionKeys|PlanTopology)' -count=1
internal/ai/agent/plan_pipeline/executor_test.go:26:14: undefined: newExecutorAgentConfig
internal/ai/agent/plan_pipeline/executor_test.go:26:43: undefined: ExecutorBuilderConfig
internal/ai/agent/plan_pipeline/executor_test.go:51:16: undefined: NewExecutorBuilder
internal/ai/agent/plan_pipeline/executor_test.go:70:41: undefined: executorGenModelInput
FAIL SentinelOps/internal/ai/agent/plan_pipeline [build failed]
```

- `PASS`（预期 Red）：命令退出 1，目标包只因 P15 薄 builder 的可观察配置与输入函数不存在而编译失败；不是环境错误、`[no tests to run]` 或已有实现直接变绿。

首次实现后的同一命令实际执行 Case，`TestExecutorBuilder` 因测试错误地假定 `NewChatModelAgent` 会在构造期调用 fake Model 的 `BindTools` 而失败；Eino v0.9.15 在调用期用 Model option 传 Tool，构造期不调用该方法。只将断言修正为读取 builder 产出的官方 `ToolsConfig.Tools[*].Info().Name`，生产实现未因该测试假设调整。

首次局部静态检查报告新增错误字符串以大写 `Executor` 开头的 `ST1005`；只将错误前缀机械改为小写，未改变控制流、接口或安全契约，并以同一目标包测试、`go vet` 与 `staticcheck` 复验。

## Implementation result

- `NewExecutorBuilder` 只创建官方 `adk.ChatModelAgentConfig` 并调用 `adk.NewChatModelAgent`。它不创建 Runner，不实现 Plan、Executor 或 Replan 循环；`BuildPlanAgent` 中总拓扑仍由官方 `planexecute.New` 创建。
- `ExecutorBuilderConfig` 只接收现有 Model、P13 Registry 名称、调用方已用官方 `adk.NewAgentTool` 形成的 Agent Tool 和 P14 Handler。Registry 名称统一交给 `GetManyRequired` fail-fast；builder 不调用任何业务 Tool 构造器。
- `RuntimeHandlerFirst` 拒绝空 Handler，并保证唯一 P14 Handler 是第一个、最外层用户 wrapper；附加 Handler 只能排在其后。本单元未增加另一种 Handler、Policy wrapper、Model Proxy 或 Tool Registry。
- `executorGenModelInput` 只读取 `planexecute.UserInputSessionKey`、`PlanSessionKey` 与 `ExecutedStepsSessionKey`，输出固定写入 `ExecutedStepSessionKey`；额外 SessionValues 和已有 `ExecutedStep` 不作为本轮输入。四个 key 均直接引用官方常量，没有复制字符串。
- 输入格式继续使用官方导出的 `planexecute.ExecutorPrompt`，固定测试将两条消息逐字段与官方 `planexecute.NewExecutor` 的真实输出比较；Plan JSON、原始输入、已完成步骤和 FirstStep golden 完全一致。
- identity 保持 `executor` / `an executor agent`，`MaxIterations=20`，`ReturnDirectly` 为空；两个生效配置均保持 `agent_runtime.enabled=false`。本单元没有让任何 durable Agent 调用 Runner。

## Local commands and results

### P15 精确门禁与 Case 枚举

```text
$ go test ./internal/ai/agent/plan_pipeline \
    -run 'Test(ExecutorBuilder|ExecutorSessionKeys|PlanTopology)' -count=1
ok SentinelOps/internal/ai/agent/plan_pipeline 0.121s

$ go test ./internal/ai/agent/plan_pipeline \
    -list 'Test(ExecutorBuilder|ExecutorSessionKeys|PlanTopology)'
TestExecutorBuilder
TestExecutorSessionKeys
TestPlanTopology
ok SentinelOps/internal/ai/agent/plan_pipeline 0.216s
```

- `PASS`：三个计划指定的顶层 Case 均被发现并由精确过滤命令实际执行，无 `[no tests to run]`。覆盖 Registry + AgentTool ToolInfo、Handler 必填/顺序、输出配置、官方 golden、Session allowlist、官方总拓扑和 durable Gate。
- Case 枚举时 Go 1.27 打印已有 Sonic 支持范围 warning 并回退标准 `encoding/json`；命令退出码和断言仍为 `PASS`，本单元未修改依赖或工具链。

### 直接受影响包回归与静态检查

```text
$ go test ./internal/ai/agent/plan_pipeline -count=1
ok SentinelOps/internal/ai/agent/plan_pipeline 0.115s

$ go vet ./internal/ai/agent/plan_pipeline
PASS

$ staticcheck ./internal/ai/agent/plan_pipeline
PASS

$ goimports -l \
    internal/ai/agent/plan_pipeline/executor_adk.go \
    internal/ai/agent/plan_pipeline/executor_test.go
无输出

$ git diff --check
PASS
```

- `PASS`：目标包全部未过滤测试、vet、staticcheck、格式与差异检查通过。未修改依赖，当前 `go.mod` 仍为 Go `1.27.0`、Eino `v0.9.15`。

### 官方复用、禁止项与 Gate 扫描

```text
$ rg -n <Runner/Loop/direct Tool constructor/copied Session key patterns> \
    internal/ai/agent/plan_pipeline/executor_adk.go
无输出；退出 1

$ rg -n <GetManyRequired/official Session key/RuntimeHandlerFirst patterns> \
    internal/ai/agent/plan_pipeline/executor_adk.go
命中 GetManyRequired、RuntimeHandlerFirst、UserInputSessionKey、PlanSessionKey、
ExecutedStepSessionKey、ExecutedStepsSessionKey

$ rg -n 'planexecute\.New\(ctx, &planexecute\.Config' \
    internal/ai/agent/plan_pipeline/plan_pipeline.go
161: agent, err := planexecute.New(ctx, &planexecute.Config{

$ rg -n -A1 '^agent_runtime:' \
    manifest/config/config.yaml manifest/config/config.docker.yaml
两份配置下一行均为 enabled: false
```

- `PASS`：禁止项 `rg` 退出 1 是预期空结果，不是工具错误。新增 builder 没有 `planexecute.NewExecutor`、`adk.NewRunner`、`LoopAgent`、无限循环、业务 Tool 构造器、`adk.NewAgentTool` 或四个 Session key 字符串字面量；只消费 Registry 与调用方 AgentTool。
- `PASS`：现有 `plan_pipeline.go` 仍使用官方 `planexecute.New`，本单元未改 Planner/Replanner/legacy Worker；双配置 durable Gate 未开启。
- 完整 `go test -race ./...`、前端、Compose、镜像、完整 E2E/Eval/故障矩阵、在线供应商、共享数据库、Hosted CI、发布和 P43 全量门禁均 `NOT RUN`；它们不属于 P15 局部门禁，也不据此声称整体二改通过。

## Key assertions

- 官方 `ExecutorConfig` 缺少 Handlers，而 `ChatModelAgentConfig` 提供 Handlers；P15 只补该已证实缺口。若官方 Executor Config 后续暴露等价能力，删除 `executor_adk.go` 并让本 contract test 对官方构造继续成立。
- P13 Registry 是已注册 Tool 实例的唯一真值；专业 Agent Tool 的真实 `adk.Agent`、inventory、CompositeInterrupt/cancel 与 parity 属于 P17～P19，本单元只锁定 builder 接口，不提前构造或迁移它们。
- P14 RuntimeHandler 仍是唯一安全 wrapper，并固定在用户 Handler 最外层；P27/P28 只能原位扩展官方模型可靠性与同一 durable Budget，不得在 builder 增加 Retry/Failover/计量循环。
- Session 输入 map 只有 `input`、`plan`、`executed_steps`、`step` 四个官方 Prompt 变量；任意额外 SessionValues 不会隐式进入 Prompt。OutputKey 使用官方 `ExecutedStepSessionKey`。
- legacy `NewExecutor` 暂不替换是单元边界要求：它仍服务未迁移旧路径；P19 才能在专业 Agent 变成真实 ChatModelAgent/AgentTool 后接入本 builder，P20 前 durable Gate 保持关闭。

## Final scope statement

P15 局部门禁为 `PASS`。本结论只覆盖可挂 P14 Handler、严格 Tool 来源和官方 Session/Prompt/Output/拓扑 parity 的薄 Executor builder；不代表专业 Agent 已迁移、durable 业务 Agent 已运行、P16 或后续单元、整个二次开发或 P43 全量门禁完成。
