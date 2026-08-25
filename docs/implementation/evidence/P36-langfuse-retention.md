# P36 Langfuse 官方 Handler 与数据保留

- Status: `PASS (P36 local gates; broader runtime package attempt has unrelated FAIL)`
- Started from: `8a26a099ac0782dcd9c2f2e00b20a5227415e8b3` (`main`，工作树干净)
- Spec references: Implementation Plan P36；上位 Spec 3.1、3.4、3.5、6.3、Task 9

## Boundary Audit

- 目标：把锁定版 Eino-ext 官方 Langfuse v2 Handler 作为默认关闭的第二个 Eino Global Handler 接入 P35 Attempt Context，完整管理 `StartTrace` / `EndTrace` / `Flush` / `Shutdown`；使用 P06 Redactor 和官方属性上限；在现有 Worker poll loop 中复用 `workflow.GORMStore` 的 generation-fenced lease 执行有界 retention，按 Run 终态时间物理清除 30 天敏感 payload 与 180 天审计元数据；保留期变更只走现有 settings/RBAC 路径并追加可查询审计。
- 明确非目标：不新增 Callback Bus、OTel Exporter、Span 转换层、Trace/Retention Store、Queue、Scheduler、Migration、Eval Runner、前端或 P37+ 能力；Langfuse 不参与 Resume/Replay/parked、Run 真值或 Eval 唯一真值。
- 兼容契约：MySQL Trace 继续使用 `aitrace.NewCallbackHandler` 和 P35 barrier；Langfuse 只直接注册官方 `callbacks/langfuse/v2.CallbackHandler`；retention SQL 留在现有 TraceDAO / `workflow.GORMStore`，runtime 只编排；动态配置继续使用现有 `settings` 表和 `PermissionManageUsersPolicyGates`。
- 安全不变量：静态配置和动态 Gate 必须同时允许才可构造网络 exporter；MaskFunc 必须复用 P06 Redactor；不记录或回显 Key；非终态、parked、waiting_approval、retryable_failed、reconciling 或仍需 Resume 的 Checkpoint 不得删除；审计变更必须包含操作者、旧值、新值、理由和时间；长期聚合不得保留原始 Prompt/Completion/Tool payload。
- 预计修改：`go.mod` / `go.sum`；`internal/ai/trace/langfuse.go` 与 P36 测试；`internal/ai/runtime/retention.go`、Worker 接线与测试；现有 `workflow.GORMStore`、TraceDAO、settings DAO/service/API 的最小 retention 方法；bootstrap/config/manifest 接线；本证据。
- 验证方式：先运行 P36 精确过滤取得 Red；实现后执行 Plan 指定测试、Langfuse MVS/why、受影响包测试、`go vet`、`goimports`、Secret/重复基础设施扫描和暂存差异审计。
- 回滚方式：通过本单元独立本地提交的普通反向提交恢复；不修改 remote、不执行 Contract Migration、不运行 P37+。

## Build-or-Reuse

| 现有 SentinelOps 能力 | 锁定版 Eino/Eino-ext 能力 | 剩余业务缺口 | 最薄实现及删除条件 |
| --- | --- | --- | --- |
| P35 MySQL Handler/Attempt barrier、P06 Redactor、P09 generation-fenced lease、P20 Worker poll loop、TraceDAO、`workflow.GORMStore`、settings/RBAC | 官方 Langfuse v2 `NewHandler`、`StartTrace`、`EndTrace`、`Flush`、`Shutdown`、OTLP/HTTP exporter、MaskFunc、属性预算和 Eino Callback | 可选 Handler 的双 Gate/Secret 生命周期、Attempt metadata 接线、退出 deadline；现有 DAO 尚无物理清理与审计 settings 原子写 | 只保存官方 Handler 生命周期句柄并直接加入同一 Callback 链；retention runtime 只调用现有 DAO/Store 方法，lease 复用 `workflow_runs` 的 owner/until/generation 字段；若官方或现有能力覆盖剩余胶水则删除项目代码，不新增同构总线/Store/Exporter。 |

## Red evidence

首次按 Plan 精确过滤运行：

```text
go test ./internal/ai/trace ./internal/ai/runtime ./internal/bootstrap \
  -run 'Test(Langfuse|Retention|RetentionAudit|PhysicalDelete|CleanupLease)' -count=1
```

结果为预期编译失败（`FAIL`）：`NewLangfuseRuntime`、`LangfuseOptions`、retention coordinator、generation-fenced retention lease 和 cleanup result 尚未定义；`internal/bootstrap` 为 `[no tests to run]`。失败命中 P36 新增断言，未触及 P37+。

## Verification ledger

| Gate | Command / evidence | Result |
| --- | --- | --- |
| 精确 P36 单元测试 | `SENTINELOPS_TEST_DSN=<redacted> SENTINELOPS_GOOSE_BIN=/tmp/tmp.7MW3iun2mx/goose go test ./internal/ai/trace ./internal/ai/runtime ./internal/bootstrap ./internal/ai/workflow ./internal/dao/mysql ./internal/service/settings -run 'Test(Langfuse|Retention|RetentionAudit|PhysicalDelete|CleanupLease)' -count=1` | `PASS`：受影响 P36 测试通过；使用隔离 Compose MySQL 项目 `sentinelops-p36` 与 pinned goose `v3.27.3`。 |
| Langfuse MVS | `go list -m github.com/cloudwego/eino-ext/callbacks/langfuse/v2` | `PASS`：`v2.0.0-20260820123736-6752ff8da9b1`。 |
| Langfuse 依赖路径 | `go mod why -m github.com/cloudwego/eino-ext/callbacks/langfuse/v2` | `PASS`：仅由 `SentinelOps/internal/ai/trace` 引入。 |
| 格式与静态检查 | `goimports`（P36 修改文件）；`git diff --check`; `go vet ./internal/ai/trace ./internal/ai/runtime ./internal/bootstrap ./internal/ai/workflow ./internal/dao/mysql ./internal/service/settings ./internal/controller/settings` | `PASS`。 |
| Bootstrap deadline | `TestLangfuseShutdownUsesConfiguredDeadline` | `PASS`：shutdown context 使用配置的 deadline。 |
| retention-only Worker 边界 | `TestRetentionOnlyWorkerDoesNotClaimRuns` | `PASS`：Runtime Gate 关闭时仍复用同一 Worker loop 执行 retention，不 claim durable Run。 |
| 隔离环境 | `docker compose -p sentinelops-p36 -f manifest/docker/docker-compose.test.yml up -d mysql` | `PASS`：MySQL healthy；未使用共享数据库。 |
| 受影响包完整测试尝试 | `go test ./internal/ai/trace ./internal/ai/runtime ./internal/bootstrap ./internal/ai/workflow ./internal/dao/mysql ./internal/service/settings ./internal/controller/settings -count=1` | `FAIL`：既有 `internal/ai/runtime` 的 `TestNestedLedgerAgentToolLeafCreatesOnePrimary` 报 `budget reservation identity conflict`；该断言不涉及 P36 文件，未改写测试或将其标为 PASS。 |

受影响包的完整测试尝试包含既有 P26 集成用例；其中 `TestNestedLedgerAgentToolLeafCreatesOnePrimary` 因既有预算 reservation identity conflict 失败，未命中 P36 文件/断言，故不能记为 P36 全包 PASS；P36 精确过滤和 P36 新增 MySQL 集成均已 PASS。

## Unfinished / NOT RUN

- P37+、真实 Langfuse 服务、真实供应商、Hosted CI、完整 Eval、发布与 P43：`NOT RUN`，不属于 P36 单元。
- 真实 Langfuse 网络导出：`NOT RUN`；官方 Handler 使用 InMemory exporter 完成生命周期、metadata、usage、redaction 和属性预算验证。
- 全仓测试、完整 Eval、镜像构建、部署和 push：`NOT RUN`。
