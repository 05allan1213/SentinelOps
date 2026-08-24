# P16 共享检索抽取、迁移清单与 scripted harness

- Status: `PASS`
- Started from: `8a5653a5b2571cac4d189788b14def8543d956e4`；`main`；开工时工作树干净
- Spec references: 上位 Spec Task 3D、Task 6、Definition of Done Eino-first；执行 Plan P16、`2.1～2.7`
- Actual files: `internal/ai/agent/base/builder.go`、`retrieval.go`、`retrieval_test.go`、`testdata/shared_retrieval.golden.json`；`internal/ai/agent/migration_manifest_test.go`、`scripted_harness_test.go`；`internal/testutil/agent/harness.go`、`harness_test.go`；`manifest/agent/migration-contract-v1.yaml`；本证据文件
- Red test and expected failure: 先加入 manifest、共享检索 golden 和 scripted harness contract test；精确过滤命令因 manifest 不存在、`RetrieveDocuments` / `RetrievalOptions` / 依赖类型不存在、scripted Model/Tool API 不存在而预期失败，退出 1
- Local commands: P16 精确过滤门禁与 Case 枚举、受影响包未过滤回归、`go vet`、scoped `staticcheck`、`goimports`、`git diff --check`、依赖/禁止项/唯一实现源码扫描
- Results: `PASS`
- Key assertions: 六个专业 Agent 的 Name、Description、Instruction SHA-256/输入、legacy/durable Tool inventory、空 ReturnDirectly 与 max iterations 已冻结；旧 Graph 两种检索模式调用同一 `RetrieveDocuments`；固定输入 golden 覆盖 Normalize、Rewrite/Split、Retriever、Dedup、Rerank 和 metadata 保留；harness 直接实现 Eino 公共 Model/Tool/Stream/Interrupt 类型；Embedder 仍唯一使用 OpenAI ACL、`routing.embedding.default` 和 2048 维约束
- Deviations from recommended route: 未修改 `internal/ai/retrieval`，因为现有唯一 Retriever、Milvus、Redis Cache 和 `MultiRetrieve` 已完整复用，重复落点会违反唯一实现门禁；为使 Plan 原样过滤命令实际执行 harness Case，在 `internal/ai/agent` 增加公共接口 contract test，同时保留 `internal/testutil/agent` 自测；当前源码没有独立 EvidenceRef decoration，P16 只保留 `schema.Document` metadata，Scope/EvidenceRef 仍由 P30 实现
- Raw artifact references: 无
- Unfinished items: P16 无未完成实现；P17～P19 专业 Agent/AgentTool 迁移及后续单元均 `NOT RUN`

## Boundary Audit

- 目标：冻结六个专业 Agent 的 Name、Description、Instruction、Prompt 输入、Tool inventory、ReturnDirectly 和 max iterations；将旧 `BuildReactAgentGraph` 内的 Normalize、Rewrite/Split、Retriever、Rerank 文档汇聚抽成新旧共用的唯一检索阶段；提供只实现 Eino 公共接口的 scripted ChatModel/Tool 测试 harness。
- 明确非目标：不创建或迁移 `ChatModelAgent` / `AgentTool`，不开放 durable Agent Gate，不修改 Prompt 或专业 Agent 业务行为，不删除旧 Graph，不实现 P17-P19 的 Agent 迁移，不增加 P30-P31 的 Evidence Scope/Citation 能力。
- 兼容契约：旧 Graph 的固定输入经过 Normalize、Rewrite/Split、Retriever、Rerank 后得到相同文档；六个专业 Agent 除已批准的 least-privilege delta 外，Prompt 和 Tool contract 不漂移。
- 安全不变量：只保留一套 Embedder/Retriever/Rerank/Cache；Embedding 继续由 `routing.embedding.default` 的 provider-qualified Catalog Ref 构造 OpenAI ACL Adapter 并校验 2048 维；不引入 `components/embedding/dashscope`；不把旧 Runnable 包成 Agent。
- 预计修改：`internal/ai/agent/base`、`internal/testutil/agent`、`manifest/agent/migration-contract-v1.yaml`、当前证据文件；仅在测试中读取专业 Agent Prompt 和清单。
- 验证方式：先记录缺少 manifest、共享检索入口和 harness 的 Red；随后运行 P16 指定过滤测试、受影响包既有测试、`go vet`、依赖/源码负向扫描和唯一实现扫描。
- 回滚方式：本单元形成一个独立本地提交；回滚该提交即可恢复抽取前 Graph，且不涉及 Schema、外部数据、remote 或线上状态。

## Build-or-Reuse

| 检查项 | 结论 |
| --- | --- |
| 现有代码能力 | `base.BuildReactAgentGraph` 已包含完整检索阶段；`retrieval.GetRetriever`、`MultiRetrieve`、现有 Rerank client、Redis SemanticCache 和 Milvus client 均可原位复用。 |
| Eino / Eino-ext 官方能力 | 继续使用 Eino `retriever.Retriever`、`model.ToolCallingChatModel`、`tool.InvokableTool` / `StreamableTool`、`schema.Document` / `StreamReader`；OpenAI ACL Embedder 已由 Eino-ext 提供。 |
| 剩余业务缺口 | 旧 Graph 内检索编排不可供后续 `GenModelInput` 直接复用；缺少版本化 Agent contract 和可确定性表达 Model/Tool/stream/error/iteration/Interrupt 的测试 double。 |
| 最薄 Adapter | 抽取一个无状态共享检索函数并让旧 Graph 调用；测试 harness 仅保存 scripted step/call，直接实现官方接口，不复制 Eino `internal/mock`。 |

## Red evidence

先写三组目标测试并执行：

```text
$ go test ./internal/ai/agent/base ./internal/ai/retrieval \
    ./internal/ai/agent/... ./internal/testutil/agent \
    -run 'Test(MigrationManifest|SharedRetrieval|ScriptedHarness)' -count=1
internal/testutil/agent/harness_test.go: undefined: NewScriptedChatModel / ModelStep / NewScriptedTool / ToolStep
internal/ai/agent/base/retrieval_test.go: undefined: retrieveDocuments / RetrievalOptions / retrievalDependencies
internal/ai/agent/migration_manifest_test.go: open ../../../manifest/agent/migration-contract-v1.yaml: no such file or directory
FAIL
```

- `PASS`（预期 Red）：命令退出 1，三个目标缺口均由目标 Case 直接证明；不是工具错误或空过滤结果。
- 首次实现后，golden 因测试夹具尾部多一个空行失败；只修正 fixture 尾部。中断断言最初误用锁定版不存在的 `tool.IsInterruptError`，改为对公开 `adk.InterruptSignal` 执行 `errors.As`；生产行为未为测试假设调整。

## Implementation result

- `RetrieveDocuments` 是专业 Agent 唯一共享检索入口。它按原行为执行 Normalize；非 Split 模式仅在存在 History 时 Rewrite 后调用现有 Eino Retriever；Split 模式执行 RewriteAndSplit 或 SplitQuestions、现有 `MultiRetrieve` 去重和现有 Rerank client，TopN 仍为 3。
- `BuildReactAgentGraph` 的两种兼容模式均调用该入口；没有复制 Retriever、Reranker、Embedder、Cache、Milvus client 或模型配置。文档对象和现有 metadata 原样进入 Prompt 汇聚，P16 不提前定义 P30 的 EvidenceRef。
- migration manifest 覆盖 EventAnalysis、Report、Risk、Solve、Intelligence、Ops 六个专业 Agent。Instruction 用符号名和当前内容 SHA-256 冻结；durable inventory 与 P13 `tool-inventory-v1.yaml` 逐项相等；只记录批准的 EventAnalysis `save_intelligence`/持久化指令移除和普通 durable inventory 禁用 `query_database` 两项 delta。
- `ScriptedChatModel`、`ScriptedTool` 直接实现锁定版 Eino 的 `model.ToolCallingChatModel`、`tool.InvokableTool` 和 `tool.StreamableTool`；使用官方 `schema.Pipe` / `StreamReader` 与 `tool.CompositeInterrupt`。step 和 call 状态受 mutex 保护，不注册生产 Provider，也不导入或复制 Eino `internal/mock`。
- 本单元未调用 `adk.NewChatModelAgent` / `NewAgentTool`，未包装 Runnable，未开放 durable Gate，未修改任何 Prompt、业务 Tool、配置、依赖、Schema 或外部状态。

## Local commands and results

### P16 精确门禁与 Case 枚举

```text
$ go test ./internal/ai/agent/base ./internal/ai/retrieval ./internal/ai/agent/... \
    -run 'Test(MigrationManifest|SharedRetrieval|ScriptedHarness)' -count=1
ok SentinelOps/internal/ai/agent/base
ok SentinelOps/internal/ai/agent

$ go test ./internal/ai/agent/base ./internal/ai/retrieval ./internal/ai/agent/... \
    -list 'Test(MigrationManifest|SharedRetrieval|ScriptedHarness)'
TestSharedRetrievalMatchesLegacyGolden
TestSharedRetrievalPreservesDocumentMetadata
TestMigrationManifestFreezesSpecialistContracts
TestMigrationManifestDurableInventoryMatchesCatalog
TestScriptedHarnessImplementsEinoContracts
```

- `PASS`：五个 P16 Case 均被发现并由精确过滤命令实际执行；其他包显示 `[no tests to run]` 不用于支持本结论。
- Go 1.27 对 Sonic 打印已有支持范围 warning 并回退 `encoding/json`；命令退出 0，本单元没有修改依赖或工具链。

### 直接受影响包回归与静态检查

```text
$ go test ./internal/ai/agent/base ./internal/ai/retrieval \
    ./internal/ai/agent/... ./internal/testutil/agent -count=1
PASS

$ go vet ./internal/ai/agent/base ./internal/ai/retrieval \
    ./internal/ai/agent/... ./internal/testutil/agent
PASS

$ staticcheck ./internal/ai/agent/base ./internal/ai/agent \
    ./internal/ai/retrieval ./internal/testutil/agent
PASS

$ goimports -l <P16 Go files>
无输出

$ git diff --check
PASS
```

- `PASS`：目标实现包、契约测试包和 harness 的全部未过滤测试、vet、scoped staticcheck、格式与差异检查通过。
- 额外对全部 `internal/ai/agent/...` 运行 staticcheck 返回 1，报告 6 个既有 `U1000`：`chat_pipeline` 2 个、Event/Report/Risk/Solve 的 `model.go` 各 1 个；文件均不在 P16 修改范围。此项为非 P16 门禁的既有 `FAIL`，未越界清理；上述新增/修改包 scoped staticcheck 为 `PASS`。

### 唯一实现、依赖和禁止项

```text
$ rg -n 'components/embedding/dashscope' go.mod go.sum internal manifest
无输出；退出 1

$ rg -n 'type RunnableAgent|func NewRunnableAgent|type ResumeBridge|type InterruptBridge|type EventBridge' \
    internal/ai/agent internal/testutil
无输出；退出 1

$ rg -n 'NewChatModelAgent|NewAgentTool' \
    internal/ai/agent/base internal/testutil manifest/agent/migration-contract-v1.yaml
无输出；退出 1

$ <唯一实现计数断言：Retriever / NewDenseEmbedder / SemanticCache / Rerank 各 1>
PASS

$ go list -m github.com/cloudwego/eino github.com/cloudwego/eino-ext/libs/acl/openai
github.com/cloudwego/eino v0.9.15
github.com/cloudwego/eino-ext/libs/acl/openai v0.1.17
```

- `PASS`：三个禁止项 `rg` 的退出 1 均是预期空结果，不是工具错误；源码正向扫描仍命中 `routing.embedding.default`、`openaiacl.NewEmbeddingClient` 和 `milvus.EmbeddingDim`。
- `PASS`：Retriever、Embedder constructor、SemanticCache 和 Rerank 实现各只有一处；P16 只新增共享编排入口和测试 double，没有第二套生产 RAG。
- 完整 `go test -race ./...`、前端、Compose、镜像、E2E/Eval、在线供应商、Hosted CI、发布和 P43 全量门禁均 `NOT RUN`；它们不属于 P16 局部门禁，也不据此宣称整体二改通过。

## Final scope statement

P16 局部门禁为 `PASS`。本结论只覆盖专业 Agent 契约冻结、旧 Graph 共用检索抽取和 deterministic scripted harness；P17 的 `ChatModelAgent` 迁移、P18-P19 及任何后续单元均未执行。
