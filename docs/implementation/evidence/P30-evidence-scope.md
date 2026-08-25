# P30 Evidence Scope、过滤与缓存隔离

- Status: PASS（局部门禁通过；MySQL 集成项 NOT RUN）
- Started from: `940c842 feat(memory): make session revisions durable context truth`
- Spec references: Implementation Plan P30；上位 Spec 7.9、Task 6 Evidence RAG
- Boundary audit:
  - In scope: 完整 `EvidenceRef`/stable evidence ID、Scope predicate、唯一 Retriever 的 documents 过滤、Redis semantic cache namespace、P03 Evidence 字段到 MySQL model/Milvus metadata 的映射。
  - Out of scope: P31 citation/injection、P32/P33 MCP、P35 Trace、第二套 Embedder/Retriever/Milvus/Reranker/Cache、任何 migration/DDL。
  - Invariants: disabled/deleted/index incomplete/ACL 不匹配文档返回 0；MySQL/ACL 真值源失败 fail-closed；用户、角色、知识库、索引版本和 Policy 不跨缓存命中；事件分区不套用 knowledge ACL；retrieval subflow 仍 L0-only。
- Build-or-Reuse:

  | 现有能力 | 官方能力 | 剩余缺口 | 最薄实现 |
  | --- | --- | --- | --- |
  | P16 唯一 `internal/ai/retrieval.Retriever`、现有 Redis `SemanticCache`、P03 migration 字段 | Eino `schema.Document`、Milvus metadata JSON、现有 OpenAI ACL Embedder | Evidence identity、Scope predicate、fail-closed 状态交集和缓存 namespace | `internal/ai/evidence` contract；Retriever 原位调用 `FilterDocuments`；cache 仅复制 prefix，不新建 client/store |
- Actual files:
  - `internal/ai/evidence/types.go`, `scope.go`, `types_test.go`。
  - `internal/ai/retrieval/filter.go`, `retriever.go`, `filter_test.go`。
  - `internal/ai/budgetctx/budget.go`、`internal/ai/runtime/budget.go`、`internal/ai/runtime/context.go`（仅适配 P28 既有 reservation，不新增 Store）。
  - `internal/ai/cache/semantic.go`, `semantic_test.go`。
  - `internal/dao/mysql/model.go`, `internal/ai/agent/knowledge_index_pipeline/orchestration.go`, `internal/service/knowledge/knowledge.go`。
  - `internal/ai/tools/system/query_internal_docs.go`（严格过滤错误直接返回）。
- Red test and expected failure: P30 contract 首次新增时，`EvidenceRef`、Scope namespace 和 strict filtering API 尚不存在；实现后局部测试通过。
- Local commands:
  - `go test ./internal/ai/evidence ./internal/ai/retrieval ./internal/ai/cache -run 'Test(EvidenceScope|EvidenceRef|StableEvidence|Filter|SemanticCache)' -count=1` -> PASS
  - `go vet ./internal/ai/budgetctx ./internal/ai/evidence ./internal/ai/retrieval ./internal/ai/cache ./internal/ai/runtime ./internal/ai/agent/knowledge_index_pipeline ./internal/service/knowledge ./internal/ai/tools/system` -> PASS
  - `go test ./... -run '^$' -count=1` -> PASS（全仓编译型 contract）
  - `go test ./internal/dao/mysql ./internal/ai/retrieval -count=1` -> NOT RUN（`SENTINELOPS_TEST_DSN` 未提供；MySQL 集成门禁要求 P03 throwaway DSN）
- Key assertions:
  - `EvidenceRef` 精确覆盖 evidence/source/base/document/chunk/version/hash/scope/retrieved/quote-preview/vector/rerank 字段；反序列化缺字段、非法 hash 或 stable ID 不一致均拒绝。
  - stable evidence ID 只由来源、版本和内容 hash 决定，不含时间或分数。
  - documents Retriever 缺少 request Scope 立即返回 `evidence unavailable`；MySQL 文档状态、chunk 状态、ACL、版本/hash/scope 与 Milvus metadata 必须交集匹配。
  - `SearchDocs` 不再在过滤为空时回退原始 Milvus 结果；`query_internal_docs` 对授权依赖错误 fail-closed。
  - semantic cache prefix 由 user、role、knowledge base、access scope、indexed version、Policy hash 组成；每个维度变化均隔离命中。
  - durable Attempt Context 通过 `budgetctx` 适配 P28 `BaseBudgetKindRAG`：Retriever pre-call 预留候选文档/上下文字符上界，返回后以真实文档数/字符 settle；没有本地 RAG 计数器。
  - 新建/注册文档把 P03 `content_hash/source_version/access_scope/indexed_version` 写入 MySQL，并原位注入 Milvus metadata；启停/重建递增 indexed version。
  - 生产源码不存在 `components/embedding/dashscope`；Retriever/Embedder/Reranker/Milvus/Cache 均无第二套初始化路径。
- Deviations from recommended route: 现有 `FilterDisabledDocs` 保留兼容签名，但内部改为 strict fail-closed；新增调用方使用 `FilterDisabledDocsStrict`，避免旧 API 返回值无法表达授权依赖错误。
- Unfinished items:
  - `SENTINELOPS_TEST_DSN` 未提供，真实 MySQL 状态/ACL、Milvus metadata 等价性集成测试 NOT RUN。
  - P31 Citation、Injection 和派生索引未实施。
