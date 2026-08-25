# P29 Immutable Input、Session Revision 与上下文治理

- Status: PASS（局部门禁通过；MySQL 集成项 NOT RUN）
- Started from: `454d966 feat(runtime): persist shared run budgets`
- Spec references: Implementation Plan P29；上位 Spec 6.9、Task 5 上下文治理
- Boundary audit:
  - In scope: immutable Run input 解码、Revision N+1 payload、durable Attempt History 一次注入、Redis best-effort projection、旧 chat 内联二次摘要移除、Eino 官方 Summarization/Reduction clear-only 配置。
  - Out of scope: P30 Evidence、P35 Trace barrier、P36 retention、Schema migration、第二套 Store/Runner/Memory。
  - Invariants: Query 只含当前任务；History 只来自已提交 Revision；完成只调用 P08 `CompleteRunAndCommitSession`；Redis 投影失败不回滚 MySQL；Resume 不重复注入 History；不使用本机 offload 文件。
- Build-or-Reuse:

  | 现有能力 | 官方能力 | 剩余缺口 | 最薄实现 |
  | --- | --- | --- | --- |
  | P07/P08 `session_state_revisions`、P08 完成 primitive、P11 typed Attempt Context | Eino ADK `summarization.New`、`reduction.New`、`ChatModelAgent` | Revision payload 组装、Snapshot History 到 Agent Message 的边界、投影失败隔离 | `BuildSessionRevisionPayload`、`HistoryMessagesFromRevision`、显式 ContextGovernance handlers；不新建 Store/Loop |
- Actual files:
  - `internal/ai/runtime/input.go`, `context.go`, `runner.go`, `worker.go`。
  - `internal/ai/workflow/session_revision.go`。
  - `internal/ai/agent/plan_pipeline/agent_worker.go`, `executor.go`, `executor_adk.go`。
  - `internal/ai/agent/base/context_middleware.go`, `specialist.go`。
  - durable specialist orchestration files；`internal/ai/agent/chat_pipeline/lambda_func.go`。
  - P29 contract tests in runtime/workflow/chat_pipeline/plan_pipeline。
- Red test and expected failure: 新增 P29 用例首次运行时因 `BuildSessionRevisionPayload`、`BuildImmutableRunInput`、`HistoryMessagesFromRevision` 和 durable History helper 尚未实现而编译失败；chat lambda 静态门禁命中 `summarizeOldHistory`。
- Local commands:
  - `go test ./internal/ai/runtime ./internal/ai/workflow ./internal/ai/agent/chat_pipeline ./internal/ai/agent/... -run 'Test(ImmutableInput|SessionRevision|HistoryOnce|RetrieverQuery|Summarization|Reduction)' -count=1`
  - `go test -race ./internal/ai/runtime ./internal/ai/workflow ./internal/ai/agent/chat_pipeline ./internal/ai/agent/plan_pipeline -run 'Test(ImmutableInput|SessionRevision|HistoryOnce|Summarization)' -count=1`
  - `go vet ./internal/ai/runtime ./internal/ai/workflow ./internal/ai/agent/chat_pipeline ./internal/ai/agent/...`
  - `go test ./internal/ai/runtime ./internal/ai/workflow ./internal/ai/agent/chat_pipeline ./internal/ai/agent/... -count=1`（已有 MySQL 集成用例因未设置 `SENTINELOPS_TEST_DSN`，NOT RUN）
- Results: PASS / NOT RUN
- Key assertions:
  - immutable input 使用 `DisallowUnknownFields`，History/handle/secret 不能进入 Query payload。
  - Revision payload 保留 preference、summary、provenance，并只追加一次当前 user/assistant turn；revision 必须按 N→N+1 递增。
  - durable AgentTool 从 typed Attempt 的 MySQL Revision Snapshot 注入 History；Resume 已有相同前缀时不重复注入；legacy 兼容路径仍保留既有业务 Memory 行为。
  - Retriever 入口只读取当前 Query；chat lambda 不再调用模型进行第二次摘要。
  - durable ContextGovernance 显式接入 Eino Summarization 与 `SkipTruncation=true` 的 clear-only Reduction；未配置本机 Backend/offload。
  - MySQL completion 成功后 projection callback 即使失败也不改变 completion 结果。
- Deviations from recommended route: 为保持 P15 兼容 Executor contract，ContextGovernance 采用 durable 显式开关；旧兼容构建默认仍只有 RuntimeHandler。
- Raw artifact references: none
- Unfinished items:
- `SENTINELOPS_TEST_DSN` 未提供，P08/P09/P10/P12/P14/P20-P26 既有 MySQL 集成门禁未执行；留给具备隔离测试库的环境复跑。
- P35 Trace barrier、P30 Evidence 等后续单元未实施。
