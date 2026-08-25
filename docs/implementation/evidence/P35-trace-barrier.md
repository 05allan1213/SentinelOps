# P35 Attempt Trace barrier、关联与脱敏

- Status: `PASS`
- Started from: `8cb85cd` (`main`，工作树干净)
- Spec references: Implementation Plan P35；上位 Spec 3.1、3.5、6.2、6.9、Task 9

## Boundary Audit

- 目标：原位扩展现有 MySQL Trace Callback/Store，使每个 durable Attempt 使用独立 `trace_id` 并携带 `run_id`、`attempt`、`lease_generation`、`runtime_version`；将 P11 provider-qualified Model Snapshot 和 cached/reasoning usage 接入节点、Run 聚合与兼容 API；把现有异步写纳入 Attempt barrier，在 P08 唯一终态事务前 flush，并以 canonical `trace.flushed` / `trace.incomplete` 和 `trace_quality` 记录结果；所有 Trace 输出复用 P06 Redactor。
- 明确非目标：不新增 Trace 表、Migration、Queue、Backend、Callback Bus、OTel/Langfuse、retention、Eval Runner、第二套 Runtime/Store/完成事务；不执行 P36 或后续单元。
- 兼容契约：复用 `aitrace.NewCallbackHandler`、`agent_trace_runs` / `agent_trace_nodes`、P11 Frozen Runtime Snapshot、P20 Worker、P08 `CompleteRunAndCommitSession`、P06 Redactor 和现有 Trace/API cached/reasoning 字段；旧请求级 `StartRun`/`FinishRun` 继续可用。
- 安全不变量：Secret 解析值不得进入 Query、Prompt、Tool、metadata、错误、日志或导出；stale generation 只能留下诊断 Trace，不能越过 P09 fence 改写 Run；不完整 Trace 不得成为 Eval/发布证据；终态 Event 不早于 barrier。
- 预计修改：`internal/ai/trace/{callback,context,span,store,tracer,types}.go`、`internal/ai/runtime/{handler,runner,worker}.go`、`internal/ai/workflow/run.go`、Trace DAO/API 接线和 P35 测试；不改 Schema/Migration。
- 验证方式：先运行 P35 精确过滤得到预期 Red；实现后执行 Plan 指定 race gate、受影响包测试、`go vet`、`goimports`、禁止项扫描和暂存差异审计。
- 回滚方式：通过本单元独立本地提交的普通反向提交恢复；不修改 remote、数据库 Schema、运行服务或 P36+。

## Build-or-Reuse

| 现有 SentinelOps 能力 | 锁定版 Eino/Eino-ext 能力 | 剩余业务缺口 | 最薄实现及删除条件 |
| --- | --- | --- | --- |
| 现有 MySQL Trace Callback/Store、P11 Model Snapshot、P20 Worker、P08 完成 primitive、P06 Redactor | Eino v0.9.15 全局 Callback lifecycle 和现有 Context 传播 | 异步写不可跟踪、durable Attempt 未启动 ActiveTrace、终态前无 flush、节点缺 durable/model 关联、部分输出未统一脱敏 | 只在现有 Callback/Store 增加 Attempt tracker/barrier 和 metadata 接线，并在 Worker 唯一完成调用前 flush；若现有能力已完整覆盖则删除胶水，不创建同构 Store/Queue/Bus。 |

## Red evidence

首次按 Plan 精确过滤运行：

```text
go test ./internal/ai/trace ./internal/ai/runtime ./internal/ai/workflow \
  -run 'Test(TraceAttempt|TraceFlush|TraceIncomplete|TraceRedaction|StaleTrace)' -count=1
```

结果为预期编译失败（`FAIL`）：`AttemptMetadata`、`ModelMetadata`、`newActiveTrace`、`TraceBarrier`、barrier/quality 常量及 `completionTraceEvents` 尚未定义。该失败命中 P35 新增断言，不是 `[no tests to run]`。

## Verification ledger

- 精确 Plan race gate：

  ```text
  go test -race ./internal/ai/trace ./internal/ai/runtime ./internal/ai/workflow \
    -run 'Test(TraceAttempt|TraceFlush|TraceIncomplete|TraceRedaction|StaleTrace)' -count=1
  ```

  `PASS`；trace、runtime、workflow 均通过。

- P35 目标与兼容回归：`PASS`。
  - Trace 包覆盖 Attempt identity/model snapshot pricing/redaction/barrier timeout；runtime 覆盖 flush-before-completion、incomplete quality、stale generation、heartbeat lease loss 后仍 flush；workflow 覆盖 `trace.flushed` / `trace.incomplete` 先于 terminal Event，以及 legacy 无 Trace ID 不新增事件。
  - 使用隔离 Compose 项目 `sentinelops-p35` 和 pinned goose v3.27.3 执行 `CompleteRunAndCommitSessionSucceededAtomic`、失败终态、P35 Trace/Attempt 集成用例；P08 原有 `LastEventSeq == 3` 契约通过。
  - `go test ./internal/ai/trace ./internal/service/trace ./api/trace/v1 -count=1`：`PASS`。
  - `go test -race ./internal/ai/trace ./internal/ai/runtime ./internal/ai/workflow -run '^$' -count=1`：`PASS`（全包编译 race gate）。
  - `go vet ./internal/ai/trace ./internal/ai/runtime ./internal/ai/workflow ./internal/dao/mysql ./internal/service/trace ./api/trace/v1`：`PASS`。
  - `goimports -l` 对全部 P35 Go 文件为空，`git diff --check`：`PASS`。

- 禁止项/架构扫描：`PASS`。
  - 仍只有既有 `agent_trace_runs`、`agent_trace_nodes` 和 `NewCallbackHandler`；未新增 Trace 表、Migration、Queue、Backend、Callback Bus、Runtime 或完成路径。
  - 生产 Trace 写均经过 `ActiveTrace.submit`；唯一 `context.Background()` 命中 `GetConfig` 配置读取，不是不可跟踪写入。
  - 未引入 `trace_incomplete` 下划线事件；仅使用 canonical `trace.incomplete`。

- broad affected-package suites：`NOT RUN` / `FAIL`（不计入 P35 目标门禁）。未设置 DSN 的探索性全包命令先在既有 fixture guard 退出；隔离 DSN 全包运行还命中既有 P26 `TestNestedLedgerAgentToolLeafCreatesOnePrimary` budget reservation conflict，且 `internal/dao/mysql` migration snapshot 用例命中既有 `00005_evidence_rag.sql` duplicate `content_hash`。这些失败不触及 P35 目标测试，未修改测试绕过。

## Unfinished / NOT RUN

- P36+、Langfuse/retention、外部供应商、Hosted CI、完整 Eval、发布与 P43：`NOT RUN`，不属于 P35 单元。
- 真实 Provider、真实 Effect、浏览器/E2E、镜像发布：`NOT RUN`。
