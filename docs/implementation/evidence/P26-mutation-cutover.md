# P26 Mutation 切流与旧 Ops 直写关闭

- Status: `PASS`
- Started from: `f50c7236c0e7d51870b2bec4fb9611bd403c4ea9`；`main`；开工时工作树干净
- Spec references: 上位 Spec `3.1`～`3.4`、`6.3`～`6.4`、Task 4“旧 Ops 改造”；执行 Plan `P26`、`2.1`～`2.7`
- Actual files: `internal/ai/runtime/{context.go,profile.go,handler.go,p20_api_worker_sse_test.go,p22_hitl_test.go,p26_mutation_route_test.go,p26_nested_agent_tool_test.go}`；`internal/ai/agent` 下六个专业 Agent orchestration、Ops legacy Gate/run/tests、Plan durable builder/Worker/Executor/tests 与 migration manifest test；`internal/ai/{effects,policy,prompt/agents,tools,ops}` 的 P26 接线与测试；`internal/{bootstrap,controller,service}` 的 durable 默认路由、legacy 入口关闭与人工 CRUD 测试；`manifest/agent/{migration-contract-v1.yaml,tool-inventory-v1.yaml}`；本证据文件
- Results: `PASS`
- Unfinished items: 无 P26 未完成项；P27 及后续单元均未开始

## Boundary Audit

- 目标：把新建 `durable_v1` Run 的 Report、Intelligence、Ops 叶子 Mutation 接到同一个 `RuntimeHandler -> Approval -> effects.Executor -> 原 Tool endpoint`；把旧 Ops 异步直写入口置于显式且默认关闭的兼容边界，并保证 `durable_v1` 即使拿到兼容许可也不能进入；保持普通人工 HTTP CRUD 的 Service 事务路径。
- 明确非目标：不物理删除 legacy Graph/Ops 实现，不开放生产 L1/L2 Gate，不实现 P27 Retry/Failover、P28 预算扩展、P29 上下文治理、P37 前端或 P42 动态 Gate；不新增 Tool/Action/Effect Registry、Store、Runtime、Worker loop、MQ、Outbox 或动作实现。
- 兼容契约：复用 P13 Catalog 与 strict Registry、P14 唯一无状态 Handler、P19 官方 AgentTool/Plan 拓扑、P20 API/Worker/Runner、P21-P22 Approval、P23-P25 Effect/对账以及现有 `ops/actions`、Service/DAO、Indexer 和 protected asset 检查；legacy Artifact 保留到 P42/P43 发布检查点。
- 安全不变量：所有 Agent Mutation 必须先中断审批；批准后仍重新校验 exact Checkpoint、Proposal/Policy/runtime/Gate/lease；外层 AgentTool 不创建 Effect，只有内层叶子 Tool 创建一条 Primary Ledger；生产 L1/L2 frozen Gate 继续为 false；普通人工 CRUD 不创建 Approval/Effect；旧直写对 `durable_v1` 永远拒绝。
- 验证方式：P26 命名 Red；计划原文 race 门禁；直接受影响包未过滤测试与补充 race；`go vet`、`goimports -l`、manifest/hash、源码唯一性和禁止直写扫描；现有 Compose test fixture 的隔离 MySQL。
- 回滚方式：反向提交本单元独立本地 Commit；不 Down Schema、不删除 legacy Artifact、不修改 remote、不 push。

## Build-or-Reuse

| 现有 SentinelOps 能力 | 锁定版 Eino / Eino-ext 能力 | 剩余业务缺口 | 最薄实现及删除条件 |
| --- | --- | --- | --- |
| P13 Catalog/Registry 与完整专业 Agent inventory；P14/P22-P25 唯一 `RuntimeHandler`、Approval、Effect Executor；P19 官方 AgentTool；P20 唯一 durable Worker/Runner；现有 `ops/actions`、Tool endpoint、Service/DAO、Indexer | Eino v0.9.15 `ChatModelAgentMiddleware`、`NewAgentTool`、`CompositeInterrupt`、`planexecute.New`、官方 Runner/Resume 已负责调用与恢复传播；Eino 不负责项目 legacy cutover 或 MySQL Effect 真值 | 生产 durable resolver/snapshot 仍只启用只读 EventAnalysis，Mutation 专业 Agent 使用无 Store Handler；旧 ingest/scheduler/Ops API 仍可 fire-and-forget 直写 | 给现有 Plan/专业 Agent builder 注入同一个 P22 Handler 并由现有 Worker 解析；扩展同一 frozen snapshot 到实际组合 inventory；旧入口只增加默认关闭且拒绝 `durable_v1` 的薄兼容边界，不复制 endpoint、Registry、Worker 或 Effect 实现 |

## Red

首次执行：

```bash
go test ./internal/ai/runtime ./internal/ai/ops/engine ./internal/ai/effects ./internal/ai/ops/actions ./internal/service/event \
  -run 'Test(MutationRoute|NoDirectWrite|HumanCRUD|ProtectedAsset|NestedLedger)' -count=1
```

结果为预期 `FAIL`：编译器确认 `BuildDurableRuntimeSnapshot`、`IsDurableV1Context` 和显式 legacy Ops Gate/API 尚不存在。失败来自 P26 contract 缺失，不是空过滤、外部服务或环境问题。完成实现后同一扩展目标命令 `PASS`。

新增 MySQL 人工 CRUD Case 的首次探索运行在插入 fixture 时 `FAIL`：`events.metadata`/`raw_payload` 的 JSON 列拒绝空字符串，尚未进入 `UpdateStatus`。只把测试 fixture 改为合法 `{}`，随后同一命令 `PASS`；生产代码未因该夹具错误修改。

## Implementation

- `BuildDurableRuntimeSnapshot` 冻结完整专业 Tool inventory 与六个 framework AgentTool；API 和 Worker 共用它，新 Run 默认选择官方 `plan_agent`，生产 `agent_runtime.l1_writes/l2_writes` 仍固定为 `false`。
- Worker 为现有 `workflow.GORMStore` 创建一个 `NewHITLRuntimeHandler`，同一实例注入 Plan Executor、外层 AgentTool 和内层专业 ChatModelAgent。没有第二套 Registry、Store、Worker loop 或 Effect adapter。
- Report、Intelligence、event update、block、三类通知和 webhook 原 endpoint 在业务写入前执行 `effects.RequireMutationRoute`；transactional Primary callback 与 external/derived callback 都携带稳定 Effect metadata。
- durable Ops 的 `trigger_ops` 只规范化 Proposal，随后使用 exact arguments 调用叶子 Tool 并由 Handler 中断。`webhook_out` 加入 durable Ops inventory；legacy inventory 保持不变。Ops Prompt hash 更新为 `f0bd06aa6b567a0ff5816b5f978d32085d115c038a42435553ebdfb16c5bd8e3`。
- ingest 不再自动启动旧 Ops goroutine，Worker bootstrap 不再启动补偿扫描。保留 Artifact 只经唯一 `ops_pipeline.RequireLegacyOpsWrites`；nil 默认关闭，typed `durable_v1` 永远拒绝，Gate 通过后才注入 legacy mutation context。
- 普通 HTTP CRUD 未接入 Agent Runtime，仍执行 RBAC -> Service -> DAO/Transaction/Audit 路径。

## Verification

隔离环境：

```bash
docker compose -p sentinelops-p26 -f manifest/docker/docker-compose.test.yml up -d --wait mysql
docker compose -p sentinelops-p26 -f manifest/docker/docker-compose.test.yml port mysql 3306
# 127.0.0.1:<dynamic-port>
```

真实 MySQL 行为测试：

```bash
SENTINELOPS_GOOSE_BIN=<goose-v3.27.3> SENTINELOPS_TEST_DSN=<redacted> \
  go test ./internal/ai/runtime ./internal/service/event \
  -run 'Test(MutationRouteEveryLeafInterruptsBeforeEndpoint|HumanCRUDChangesDataWithoutApprovalOrEffect)$' -count=1
```

结果：两个 package 均 `PASS`。

Plan 原文 race 门禁：

```bash
SENTINELOPS_GOOSE_BIN=<goose-v3.27.3> SENTINELOPS_TEST_DSN=<redacted> \
  go test -race ./internal/ai/tools/... ./internal/ai/ops/... ./internal/ai/effects ./internal/service/... \
  -run 'Test(MutationRoute|NoDirectWrite|HumanCRUD|ProtectedAsset|NestedLedger)' -count=1
```

结果：所有有命中 Case 的 package `PASS`，race detector 无报告；无匹配 Case 的 package 明确显示 `[no tests to run]`，不计作行为覆盖。

补充 durable wiring/nested race 门禁：

```bash
SENTINELOPS_GOOSE_BIN=<goose-v3.27.3> SENTINELOPS_TEST_DSN=<redacted> \
  go test -race ./internal/ai/runtime ./internal/bootstrap ./internal/ai/agent/plan_pipeline \
  ./internal/ai/agent/ops_pipeline \
  -run 'Test(MutationRoute|NoDirectWrite|HumanCRUD|ProtectedAsset|NestedLedger)' -count=1
```

结果：四个 package 均 `PASS`，race detector 无报告；`ops_pipeline` 直接证明 typed `durable_v1` Context 即使配 open legacy Gate 仍在 Gate evaluator 前拒绝。

直接受影响包未过滤回归：

```bash
SENTINELOPS_GOOSE_BIN=<goose-v3.27.3> SENTINELOPS_TEST_DSN=<redacted> \
  go test ./internal/ai/runtime ./internal/ai/agent/... ./internal/ai/policy ./internal/ai/ops/... \
  ./internal/ai/effects ./internal/ai/tools/... ./internal/bootstrap ./internal/controller/chat \
  ./internal/controller/ops ./internal/service/chat ./internal/service/event \
  ./internal/service/ingest ./internal/service/scheduler -count=1
```

结果：所有含测试的 package `PASS`；runtime 的 P22-P25 审批恢复、transactional/external Effect 回归真实执行，证明新增 route guard 接受批准后的两类 Effect callback；无测试文件的 ingest/scheduler 明确保留静态接线审计。

静态门禁：

```bash
go vet ./internal/ai/runtime ./internal/ai/agent/... ./internal/ai/policy ./internal/ai/ops/... \
  ./internal/ai/effects ./internal/ai/tools/... ./internal/bootstrap ./internal/controller/chat \
  ./internal/controller/ops ./internal/service/chat ./internal/service/event \
  ./internal/service/ingest ./internal/service/scheduler
go test ./internal/ai/agent -run 'TestMigration' -count=1
git diff --check
goimports -l <all-modified-go-files>
```

结果：全部 `PASS`；`go vet`、`git diff --check`、`goimports -l` 无输出。

源码审计：

```bash
rg -n 'context\.Background|go func|New.*Tool\(' internal/ai/ops internal/ai/tools internal/ai/agent
rg -n 'WithLegacyMutationContext|RequireLegacyOpsWrites|AllowLegacyOpsWrites' --glob='*.go' .
rg -n '^func (New(CreateReport|SaveIntelligence|UpdateEventStatus|BlockIP|NotifyDingTalk|NotifyWeCom|NotifyEmail|WebhookOut)Tool|\(.*\) Execute\()' internal/ai/tools internal/ai/ops/actions
rg -n 'New(Default)?Registry|NewGORMStore|NewHITLRuntimeHandler|NewExecutor\(|NewDurableWorker|NewWorker|type .*Registry|type .*Store|type .*Worker' internal/ai internal/bootstrap internal/service --glob='*.go'
```

结果：`PASS`。生产 goroutine 命中只剩 `engine.startRun` 与 legacy `RunWithQuery`，二者的公开调用路径先经过唯一 legacy Gate；durable Worker 不启动 ingest 自动 Ops 或补偿扫描。`WithLegacyMutationContext` 唯一生产调用紧跟 `RequireLegacyOpsWrites`，其余命中均为测试。每个 Tool constructor/action endpoint 仍只有一份，protected asset/Nginx 逻辑仍在原 `ops/actions`；只存在既有 Registry、`workflow.GORMStore`、runtime Worker 和 effects Executor。

清理：

```bash
docker compose -p sentinelops-p26 -f manifest/docker/docker-compose.test.yml down -v --remove-orphans
docker compose -p sentinelops-p26 -f manifest/docker/docker-compose.test.yml ps -a
```

结果：`PASS`；容器、网络与匿名卷已移除，`ps -a` 为空。

## Key Assertions

- 八个 durable 叶子 Mutation 逐项使用真实 Approval Store：endpoint 调用数为 0，每个 Run 恰有一条 `preparing` Approval，审批前 Effect 为 0。
- 实际嵌套 `report_agent` AgentTool 传播 CompositeInterrupt；批准恢复后报告写入 1 次，整个 Run 的 Effect 总数为 1 且 Primary 为 1，外层 AgentTool 不拥有 Ledger。
- 普通管理员 `eventsvc.UpdateStatus` 真实改变 MySQL 数据，同时 `agent_approvals=0`、`agent_effects=0`。
- P23 transactional report 与 P24 external webhook 未过滤回归均通过，新增 route enforcement 没有阻断批准后的稳定 Effect metadata callback。
- protected asset 负向测试继续命中唯一原实现；旧 Ops nil Gate 关闭、显式测试 Gate 可达、typed durable context 即使配 open Gate 仍拒绝。
- 新 frozen snapshot 覆盖全部 Mutation 与 framework AgentTool，生产 L1/L2 effective Gate 仍为 false；新 API/Worker 默认只解析 `plan_agent`。

## Deviations And Not Run

- 与推荐路线无架构偏差。为跨 package 验证人工 CRUD，测试内复用了 P12 一次性数据库模式；它只存在于 `_test.go`，没有新增生产 Store/fixture abstraction。
- `NOT RUN`：全仓测试、应用镜像构建、前端、完整 E2E、在线模型/通知供应商、Eval、Chaos、Hosted CI、共享数据库、发布与 P43 全量门禁；均不属于 P26 局部门禁。
- `NOT RUN`：P27 Retry/Failover/breaker/limiter 以及任何后续单元。

## Raw Artifact References

- Runtime/Effect：`internal/ai/runtime/{profile.go,handler.go,p26_mutation_route_test.go,p26_nested_agent_tool_test.go}`、`internal/ai/effects/{route.go,executor.go,p26_nested_ledger_test.go}`
- Durable topology：`internal/bootstrap/worker.go`、`internal/ai/agent/plan_pipeline/{durable.go,agent_worker.go,executor.go}`、六个专业 Agent `orchestration.go`
- Ops cutover：`internal/ai/agent/ops_pipeline/{legacy_gate.go,run.go}`、`internal/ai/ops/engine/engine.go`、`internal/service/{ingest/service.go,scheduler/soar_scan.go}`
- Mutation endpoints/actions：`internal/ai/tools/{report,intelligence,ops}`、`internal/ai/ops/actions`
- Human CRUD：`internal/service/event/{event.go,p26_human_crud_test.go}`
- Contract：`internal/ai/policy/catalog.go`、`internal/ai/prompt/agents/ops.go`、`manifest/agent/{migration-contract-v1.yaml,tool-inventory-v1.yaml}`
