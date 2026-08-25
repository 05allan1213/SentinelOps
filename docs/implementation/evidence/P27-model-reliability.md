# P27 Eino Retry/Failover、breaker 与 limiter

- Status: `PASS`
- Started from: `66074c308d7ed7a910efad863d320393eefd8986`；`main`；开工时工作树干净
- Spec references: 上位 Spec `3.4`、`3.5`、`6.8`；执行 Plan `P27`
- Results: `PASS`
- Unfinished items: 无 P27 未完成项；P28 及后续单元未开始

## Boundary Audit

- 目标：让现有 Executor 与六个 durable 专业 `ChatModelAgent` 使用 Eino v0.9.15 官方 `ModelRetryConfig` / `ModelFailoverConfig`；按 Profile 构造有序 provider-qualified candidates；让 breaker 与 limiter 跨请求共享并按 Catalog Ref 隔离；在每次物理 endpoint call 处复用唯一 `RuntimeHandler`。
- 明确非目标：不修改 Planner/Replanner，不实现 MCP/Skill Agent，不实现 P28 durable Budget 扩展，不新增模型 Gateway、Agent loop、Store、队列或项目 Retry/Failover loop，不执行在线供应商调用、全仓集成或 P43 门禁。
- 兼容契约：首候选保持既有 `default/reasoning` 行为；旧测试/兼容 builder 显式传入 Model 时保持直连；Provider/Catalog/Driver/Secret Resolver 继续复用 P04；Snapshot、breaker、limiter 和 metadata 使用 Catalog Ref，不比较供应商名。
- 安全不变量：未知 Driver/能力/Endpoint 启动即失败；认证、参数、Policy、预算和取消错误不可重试；每个候选 Route Options 独立，未配置的 `enable_thinking` 不下发；流读取失败必须按失败 settle 并计入 breaker；Eino failover proxy 不再次进入逻辑层 `WrapModel`。
- 预计与实际修改范围：`internal/ai/{models,breaker,limiter,runtime}`、现有 Executor/专业 Agent builders、`internal/config`、`manifest/config/config*.yaml`、相关测试与本证据文件。
- 回滚方式：反向提交本单元独立本地 Commit；无 Schema、remote、容器或部署状态变更。

## Build-or-Reuse

| 现有 SentinelOps 能力 | Eino / Eino-ext 官方能力 | 剩余业务缺口 | 最薄 Adapter |
| --- | --- | --- | --- |
| P04 Provider -> Catalog -> Routing、有限 Driver 与唯一 Secret Resolver | Eino v0.9.15 `ModelRetryConfig`、`ModelFailoverConfig` 会完整消费 stream 后判断，且 Failover 包裹 Retry | Profile 只有单 Route，无法表达候选顺序和独立 Options | 仅把 Chat Route 扩为有序 candidates，继续用既有 `Config.Resolve` 和 OpenAI-compatible adapter |
| P14 唯一无状态 `RuntimeHandler`、durable reserve/settle 与 frozen model snapshot | `ChatModelAgentMiddleware.WrapModel` 是官方模型挂载点；官方 failover proxy 类型为 `FailoverProxyModel` | Handler 原先位于逻辑模型层，无法区分 Retry/Failover 的每次物理调用 | 为每个真实候选绑定同一 Handler，并让逻辑 failover proxy 原样通过；不建第二套 Handler/预算实现 |
| 现有进程 limiter 与 Catalog Ref 模型身份 | Eino 不提供项目级共享 breaker/供应商配额真值 | limiter 原先全局单桶，且没有跨请求 candidate health | 保留唯一 limiter 包并改为按 Catalog Ref 的共享 token bucket；新增只保存候选健康状态的最小 breaker registry |
| 现有 Executor 与六个 durable 专业 Agent builder | 官方 `ChatModelAgentConfig` 原生组合 Retry/Failover | 各 builder 尚未消费同一可靠性配置 | `BuildReliability` + `ConfigureChatModelAgent` 是唯一组合入口；P33/P34 只允许后续复用，不预建 Agent |

## Red

首次执行：

```bash
go test ./internal/config ./internal/ai/models ./internal/ai/breaker ./internal/ai/limiter \
  -run 'Test(ChatRoutingCandidates|ConfigRejectsInvalidChatCandidate|ResolveChatCandidates|Breaker|Limiter)' \
  -count=1
```

结果为预期 `FAIL`：`ChatRoute`、候选解析、breaker registry 与按 Catalog Ref 的 limiter registry 尚不存在。失败来自 P27 contract 缺失，不是空过滤或外部依赖。实现后对应包未过滤测试与 Plan 精确 race 门禁均 `PASS`。

## Implementation

- `routing.chat.default/reasoning` 现在保存有序 candidates；配置校验拒绝空列表、重复 Catalog Ref、未知 Driver、错误 capability 和缺失 Endpoint。每候选独立构造 `ExtraFields`，未配置的 `enable_thinking` 不会继承。
- `BuildReliability` 只组合 Eino 官方 Retry/Failover。候选失败携带非敏感顺序，使 Eino 的 last-success candidate 失败后从下一个有序候选继续；没有项目 failover loop 或二次 Retry。
- Retry 分类允许 408/409/429/5xx、网络和普通瞬态错误；拒绝 context/stream cancellation、400/401/403、Policy 以及 P14 Budget 错误。stream 在 EOF 前失败会按失败记录并触发官方 Retry/Failover。
- breaker 是进程共享、按 Catalog Ref 隔离的连续失败状态；limiter 复用唯一进程 registry，同配置重复构建不会重置 token bucket。相同厂商 Model ID 的不同 Provider 仍保持独立候选、健康状态、限流与 metadata。
- frozen runtime snapshot 保留全部 Chat candidates、顺序、Catalog Ref、Driver、Model ID、Route Options 与 pricing identity。每次物理调用生成独立 reservation identity，并在 endpoint 前验证候选与 frozen snapshot 完全匹配。
- Executor 与六个 durable 专业 Agent 使用唯一可靠性入口；测试/legacy 调用显式传 Model 时保持直连。Planner/Replanner 源码未修改。
- `config.yaml`、`config.docker.yaml` 与被忽略的完整 `config.local.yaml` 已迁移；测试只验证结构、引用和完整性，不读取或输出解析后的 Secret。

## Verification

Plan 原文精确 race 门禁：

```bash
go test -race ./internal/ai/models ./internal/ai/breaker ./internal/ai/limiter ./internal/ai/runtime \
  -run 'Test(Retry|Failover|Breaker|StreamFailure|Limiter|NoDoubleRetry)' -count=1
```

结果：四个 package 均 `PASS`，无 `[no tests to run]`，race detector 无报告。

直接受影响包未过滤回归：

```bash
go test ./internal/config ./internal/ai/models ./internal/ai/breaker ./internal/ai/limiter \
  ./internal/ai/agent/base ./internal/ai/agent/event_analysis_pipeline \
  ./internal/ai/agent/intelligence_pipeline ./internal/ai/agent/ops_pipeline \
  ./internal/ai/agent/plan_pipeline ./internal/ai/agent/report_pipeline \
  ./internal/ai/agent/risk_pipeline ./internal/ai/agent/solve_pipeline ./internal/bootstrap -count=1
```

结果：全部 `PASS`。

运行时快照与 P27 物理调用回归：

```bash
go test ./internal/ai/runtime \
  -run 'Test(RuntimeSnapshot|DurableSnapshot|MutationRouteSnapshot|Retry|Failover|StreamFailure|NoDoubleRetry)' \
  -count=1
```

结果：`PASS`。覆盖 candidate order、frozen identity、跨 Provider 同 Model ID、独立 reservation、stream 失败 settle 和 failover proxy 不二次包装。

静态与编译门禁：

```bash
go vet ./internal/config ./internal/ai/models ./internal/ai/breaker ./internal/ai/limiter \
  ./internal/ai/runtime ./internal/ai/agent/base ./internal/ai/agent/event_analysis_pipeline \
  ./internal/ai/agent/intelligence_pipeline ./internal/ai/agent/ops_pipeline \
  ./internal/ai/agent/plan_pipeline ./internal/ai/agent/report_pipeline \
  ./internal/ai/agent/risk_pipeline ./internal/ai/agent/solve_pipeline ./internal/bootstrap
go test ./internal/... -run '^$'
git diff --check
```

结果：全部 `PASS`；`go vet` 与 `git diff --check` 无输出，全部修改过的 Go 文件经 `goimports` 处理。

源码审计：

```bash
rg -n 'failoverModel|type .*Failover.*Model|for .*failover|for .*retry' internal --glob '*.go' --glob '!**/*_test.go'
rg -n 'ModelRetryConfig|ModelFailoverConfig|BuildReliability|ConfigureChatModelAgent' internal/ai --glob '*.go' --glob '!**/*_test.go'
rg -n 'aliyun_bailian|dashscope' internal/ai --glob '*.go' --glob '!**/*_test.go'
git diff -- internal/ai/agent/plan_pipeline/planner.go internal/ai/agent/plan_pipeline/replan.go
```

结果：`PASS`。无生产自研 failover/retry loop、无具体 Provider 名业务分支；官方配置只在 `internal/ai/models/reliability.go` 构造，Agent 只消费唯一入口；Planner/Replanner diff 为空；P33/P34 的 MCP/Skill Agent 未实现。

## Key Assertions

- Retry success、Retry exhausted 后 Failover success、全部 exhausted、mid-stream failure Retry 均真实通过 Eino `ChatModelAgent`；一次首候选 exhausted 加一次备选成功只有 3 次物理调用，不存在双层乘法 Retry。
- 认证、参数、结构化 Policy 与 Budget 错误只调用首候选 1 次，不 Retry、不 Failover。
- 旧 Executor 兼容 builder 显式传入 `Model` 时，`ModelRetryConfig` 与 `ModelFailoverConfig` 均为 `nil`，不会叠加 Eino ADK Retry/Failover。
- 两次独立 reliability build 取得同一 Catalog Ref 的同一 breaker health；相同 limiter settings 重复配置不重置已消费 token。
- 同一 Profile 两个 Provider 使用相同厂商 Model ID 时，候选顺序、Route Options、Snapshot、breaker、limiter 和 runtime metadata 均按 Catalog Ref 隔离。
- stream 读取失败同时触发 breaker failure 与 durable failed settlement；EOF 才记录成功。

## Deviations And Not Run

- 与推荐路线无架构偏差。为适配 Eino last-success 行为，候选错误增加非敏感 order wrapper；它只辅助官方 `GetFailoverModel` 选择下一个候选，不执行循环或 Retry。
- P27 只保证同一 durable attempt 内每次物理调用 reservation identity 不同；跨 crash/Resume/Replay 的完整预算连续性属于 P28，`NOT RUN`，本单元不作相关完成声明。
- 一次未过滤的 `go test` 混合命令中，`internal/ai/runtime` 因未设置 `SENTINELOPS_TEST_DSN` 返回命令级 `FAIL`；这些既有 MySQL 集成 Case 在本环境为 `NOT RUN`。P27 指定的无外部依赖精确 runtime/race 门禁均已 `PASS`。
- `NOT RUN`：在线模型供应商、共享数据库、完整 runtime/MySQL suite、全仓未过滤测试、应用镜像、前端、E2E、Eval、Chaos、Hosted CI、发布与 P43 全量门禁；均不属于 P27 局部门禁。

## Raw Artifact References

- Config/Candidates：`internal/config/{config.go,p27_reliability_test.go}`、`manifest/config/config*.yaml`
- Official reliability：`internal/ai/models/{open_ai.go,reliability.go,p27_candidates_test.go,reliability_test.go}`
- Shared health/rate：`internal/ai/breaker`、`internal/ai/limiter`
- Physical call boundary：`internal/ai/runtime/{model_reliability.go,p27_model_reliability_test.go,handler.go,context.go,snapshot.go,profile.go}`
- Agent wiring：`internal/ai/agent/base/specialist.go`、`internal/ai/agent/plan_pipeline/{executor.go,executor_adk.go,executor_test.go}`、六个专业 Agent `orchestration.go`
