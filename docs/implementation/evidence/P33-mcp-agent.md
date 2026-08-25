# P33 mcp_agent、Tool Search 与 AgentTool

- Status: `PASS`（P33 局部门禁通过；外部依赖项按实况 `NOT RUN`）
- Spec references: Implementation Plan P33；上位 Spec 4.4、6.8、Task 7

## Boundary Audit

- 目标：新增独立 `mcp_agent`，使用 Eino 官方 `ChatModelAgent` 与 `toolsearch` middleware；远端 MCP Tool 只在该 Agent 内动态可见；外层 Executor 只暴露一个官方 `adk.NewAgentTool`。
- 明确非目标：不实现 MCP 协议、Transport、Session、Schema converter、reconnect client、Skill、第二套模型 Retry/Failover/breaker/limiter、第二套 Budget Store、写 Tool、前端、Compose、镜像或 P34+。
- 兼容契约：复用 P32 `SessionOwner`/officialmcp Tool、P27 `BuildReliability`/`ConfigureChatModelAgent`、P14 `RuntimeHandler`、P13 Catalog、P11 Runtime Snapshot/`MCPCatalogHash` 和官方 ADK `AgentTool`/Tool Search API；不改变 Planner/Executor 的既有 Session key 与 planexecute 拓扑。
- 安全不变量：`UseModelToolSearch` 默认 false；true 必须有已验证 provider contract；外层 Tool inventory 只出现 `mcp_agent`；MCP 动态 Tool 仅接受官方 annotations 明确 `readOnlyHint=true` 且非 destructive；动态调用继续经过 Scope、Catalog、Budget、Trace；模型只由 Profile/candidates 选择，不比较 Provider 名或覆盖 Route Options；Catalog/hash 不匹配 fail-closed。
- 预计修改：`internal/ai/agent/mcp_pipeline/agent.go` 及测试、`internal/ai/agent/plan_pipeline/{executor.go,p33_mcp_agent_test.go}`、`internal/ai/policy/catalog.go`、`internal/ai/runtime/{handler.go,dynamic_catalog.go}` 及测试、本证据。
- 验证方式：先写 P33 Red 测试；实现后运行 P33 精确测试、受影响 Agent/Runtime/Policy 测试与 `go vet`/`goimports`，再做负向源码扫描、暂存差异审计和一个本地提交；外部 MCP/在线供应商/共享数据库/Compose/P43 按实况记录 `NOT RUN`。
- 回滚方式：通过本单元独立本地提交的普通反向提交恢复；不修改 remote、数据库、运行服务或上位 Spec/Plan。

## Build-or-Reuse

| 现有 SentinelOps 能力 | 锁定版 Eino/Eino-ext 官方能力 | 剩余业务缺口 | 最薄实现及删除条件 |
| --- | --- | --- | --- |
| P32 `SessionOwner.Tools`/官方 MCP Tool；P27 `BuildReliability` 与 `ConfigureChatModelAgent`；P14 `RuntimeHandler`；P13 Catalog；P11 Runtime Snapshot/`MCPCatalogHash`；P19 Executor/AgentTool | Eino v0.9.15 `adk.NewChatModelAgent`、`adk.NewAgentTool`、`toolsearch.New`、`ChatModelAgentMiddleware`、`ModelRetryConfig`/`ModelFailoverConfig` | MCP 动态 Tool 的只读过滤、动态 Catalog 上下文与 schema/hash fail-closed、独立 Agent 及外层单 Tool 接线 | 只增加 `mcp_pipeline` 的薄组装、官方 middleware 和动态 Catalog metadata；不复制 Agent Loop、Tool Search、MCP 协议、模型可靠性、Budget 或 Registry；官方/现有能力足够时删除本 Adapter。 |

## Red evidence

先写入 `internal/ai/agent/mcp_pipeline/p33_mcp_agent_test.go` 后执行：

```text
$ go test ./internal/ai/agent/mcp_pipeline -run 'Test(MCPAgent|ToolSearch)' -count=1
FAIL SentinelOps/internal/ai/agent/mcp_pipeline [build failed]
undefined: BuildMCPAgent / Config / newAgentConfig
```

状态：`FAIL`（预期 Red）。失败来自 P33 生产包尚未存在，不是 0 tests、环境跳过或已有实现直接变绿。

## Implementation result

- 新增 `internal/ai/agent/mcp_pipeline/agent.go`：使用官方 Eino `adk.NewChatModelAgent` 与 `toolsearch.New`；默认 `UseModelToolSearch=false`；开启 model-native Tool Search 必须提供已验证的 provider Catalog Ref、revision 和 64 位 contract hash，否则启动 fail-closed。
- P32 `SessionOwner.Tools` 作为唯一动态 Tool 来源；MCP Tool 只接受 officialmcp annotations 的 `readOnlyHint=true`、非 destructive 且名称不含明显写操作语义；重复名称、空 Tool、缺 Schema 均拒绝。外层通过 `adk.NewAgentTool` 只暴露一个 `mcp_agent`。
- 新增 `internal/ai/runtime/dynamic_catalog.go` 并扩展 `RuntimeHandler.prepareToolCall`：动态 MCP Tool 复用现有 identity/Scope/Trace/RuntimeHandler，预算分类使用既有 `BudgetCallKindMCP`，不创建本地计数器；动态 Catalog 只保存非敏感名称、revision、schema hash 与聚合 hash，context 读取返回副本。
- `internal/ai/agent/plan_pipeline/executor.go` 原位追加唯一 `mcp_agent` AgentTool；`internal/ai/policy/catalog.go` 登记为 L0、无 Effect framework Tool；Planner/Executor/Replanner 的官方 `planexecute` 拓扑和既有 Worker Tool 未复制或改写。
- Session-owned nested Agent 在运行结束后关闭所有 P32 `SessionOwner`；构建失败也立即 Close，保持 official Session 生命周期。

## Verification ledger

- `go test ./internal/ai/agent/mcp_pipeline ./internal/ai/tools/mcp ./internal/ai/agent/plan_pipeline -run 'Test(MCPAgent|ToolSearch|CatalogHash|MCPReadOnly)' -count=1` -> `PASS`（P32 `tools/mcp` 与 `plan_pipeline` 对应筛选项无匹配用例，Go 输出 `no tests to run`，不是跳过 P33 用例）。
- `go test ./internal/ai/agent/mcp_pipeline ./internal/ai/agent/plan_pipeline ./internal/ai/policy -count=1` -> `PASS`。
- `go test ./internal/ai/runtime -run 'Test(P33DynamicCatalog|MCPBudgetHook)' -count=1` -> `PASS`。
- `go test -race ./internal/ai/agent/mcp_pipeline ./internal/ai/agent/plan_pipeline ./internal/ai/policy ./internal/ai/runtime -run 'Test(P33|MCPAgent|ToolSearch|ExecutorOuterInventory|MCPBudgetHook)' -count=1` -> `PASS`。
- `go vet ./internal/ai/agent/mcp_pipeline ./internal/ai/agent/plan_pipeline ./internal/ai/policy ./internal/ai/runtime` -> `PASS`。
- `goimports -l` 覆盖本单元所有 Go 文件 -> `PASS`（无输出）。
- `go test ./... -run '^$' -count=1` -> `PASS`（全仓编译型 contract）。
- `git diff --check` -> `PASS`。
- 生产源码负向扫描 -> `PASS`：未发现自研 Tool Search/Agent Loop、Agent 专用 Retry/Failover/breaker/limiter/Budget Store、Provider 名分支或 MCP 协议实现。

## Key assertions

- 外层 Executor inventory 仅以单一 `mcp_agent` 形式承载远端 MCP Tool；真实远端 Tool 不展开到 Planner/Executor Context。
- 动态 Tool 每次调用仍经过现有 `RuntimeHandler` 的 identity、Scope、Trace、reservation/settle；远端声明安全不能绕过项目只读拒绝。
- `mcp_agent` 使用 Profile/candidates 构建唯一 P27 Reliability；未创建第二套模型配置，也不覆写 Route Options 或比较 Provider 名称。
- Catalog hash 与动态 Tool schema hash 进入本次 Agent context；Snapshot 兼容性仍由 P11 `MCPCatalogHash`/P12 recovery primitive 负责，未新增 parked Store 或恢复协议。

## Unfinished / NOT RUN

- 受影响 `internal/ai/runtime` 全包测试中依赖 `SENTINELOPS_TEST_DSN` 的 P12/P20/P22-P26 集成用例未执行：测试在环境缺少 DSN 时直接报告 `SENTINELOPS_TEST_DSN is required`，按规则记为 `NOT RUN`，不属于 P33 局部门禁。
- 外部 MCP Server、在线供应商、共享 MySQL、Compose、镜像、Hosted CI、Eval 与 P43：`NOT RUN`，不属于 P33 局部门禁。
