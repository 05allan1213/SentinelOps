# P31 Citation、Injection 防护与派生索引

- Status: PASS（局部门禁通过；P24 MySQL 真实重认领项 NOT RUN）
- Started from: `59e1f0b feat(rag): enforce evidence scope and cache isolation`
- Spec references: Implementation Plan P31；上位 Spec 6.4、Task 6、7.7、7.9

## Boundary Audit

- Goal: validate citations against the current Run's effective Evidence, keep retrieved content untrusted, prevent evidence text from becoming System/Developer instructions or direct Tool/Effect input, and prove the existing `milvus_index` derived Effect can be reclaimed without duplicate business records.
- In scope: `internal/ai/evidence/{validator.go,prompt.go}`, shared RAG prompt assembly, retriever Trace metadata, P13 Catalog/P24 DAG contract tests, and this evidence record.
- Out of scope: a second Retriever/Embedder/Reranker/Milvus client, a second Effect Catalog/DAG/Executor/Worker, MQ/Outbox, new schema/migration, MCP/Skill/Trace retention, and later implementation units.
- Compatibility contracts: preserve `EvidenceRef` and stable Evidence ID from P30; preserve Eino prompt/Agent interfaces, the shared Retriever, existing `workflow.GORMStore`, P13 Catalog, P24 Indexer and Effect DAG.
- Safety invariants: only current Run Evidence may be cited; source version/hash/scope/index version must match; invalid or expired refs fail closed; no-evidence output is explicitly inference/insufficient-information; evidence is rendered as untrusted User data and cannot authorize a Tool/Approval/Effect; Trace stores IDs and bounded summaries, never full retrieved content; derived indexing reuses the existing `milvus_index` step and ledger identity.
- Expected validation: targeted Evidence/Prompt/UntrustedEvidence/MilvusDerivedEffect tests, affected package `go vet`, focused compile tests, source scans for no second MQ/Outbox/Worker/Effect path, staged diff checks, and one local commit.
- Rollback: revert the single P31 commit; no shared service, database, provider, Compose project, or remote is changed.

## Build-or-Reuse

| Existing SentinelOps capability | Locked Eino capability | Remaining project-specific gap | Thinnest sufficient adapter |
| --- | --- | --- | --- |
| P30 `EvidenceRef`/Scope and one Retriever; P13 Catalog; P24 `effects.Executor`/`workflow.GORMStore`/Indexer; existing Eino prompt templates and callbacks | `schema.Message` User role, Eino ChatTemplate and Retriever/Tool callbacks | Citation syntax/validation, untrusted evidence framing, no-evidence classification, and Trace evidence summary contract | Add pure `internal/ai/evidence` validator/prompt helpers, use User-message evidence formatting in existing graphs, and narrow existing Trace retriever metadata; consume existing Effect contracts only |

## Verification ledger

- Red test: `go test ./internal/ai/evidence ./internal/ai/indexer ./internal/ai/effects ./internal/ai/agent/... -run 'Test(Citation|PromptInjection|UntrustedEvidence|MilvusDerivedEffect)' -count=1` -> FAIL as expected before implementation (missing validator/prompt APIs; derived fixture used an invalid proposal hash).
- Local commands:
  - `go test ./internal/ai/evidence ./internal/ai/indexer ./internal/ai/effects ./internal/ai/agent/... -run 'Test(Citation|PromptInjection|UntrustedEvidence|MilvusDerivedEffect)' -count=1` -> PASS
  - `go test ./internal/ai/evidence ./internal/ai/trace ./internal/ai/agent/base ./internal/ai/agent/chat_pipeline ./internal/ai/agent/event_analysis_pipeline ./internal/ai/agent/intelligence_pipeline ./internal/ai/agent/report_pipeline ./internal/ai/agent/risk_pipeline ./internal/ai/agent/solve_pipeline ./internal/ai/agent/ops_pipeline ./internal/ai/effects ./internal/ai/indexer -count=1` -> PASS
  - `go test -race ./internal/ai/evidence ./internal/ai/trace ./internal/ai/intent -run 'Test(Citation|PromptInjection|UntrustedEvidence|EvidenceTrace|FinalizeCollected)' -count=1` -> PASS
  - `go test ./... -run '^$' -count=1` -> PASS（全仓编译型 contract）
  - `go vet ./internal/ai/evidence ./internal/ai/intent ./internal/ai/trace ./internal/ai/agent/base ./internal/ai/agent/chat_pipeline ./internal/ai/agent/event_analysis_pipeline ./internal/ai/agent/intelligence_pipeline ./internal/ai/agent/report_pipeline ./internal/ai/agent/risk_pipeline ./internal/ai/agent/solve_pipeline ./internal/ai/agent/ops_pipeline ./internal/ai/effects ./internal/ai/indexer ./internal/ai/workflow` -> PASS
  - `rg` source scan for second Retriever/Indexer/Worker/MQ/Outbox and DashScope Embedding path -> PASS（未新增路径）
  - `SENTINELOPS_TEST_DSN=... go test ./internal/ai/workflow -run 'Test(DerivedEffect|ExternalEffect|UnknownWindow)' -count=1` -> NOT RUN（当前环境未提供 P03 throwaway MySQL DSN）
  - `git diff --check` -> PASS
- Key assertions:
  - `ValidateCitation` requires current Run ID, exact Evidence ID, source version, content hash, access scope and optional indexed version; Scope and 24h freshness are fail-closed. `ValidateAnswer` marks uncited output as inference/insufficient information, and `FinalizeCollectedAnswer` is called by the existing Intent Executor before returning final content.
  - Durable Runtime attempts receive the same collector from the existing typed Attempt Context; final output is validated before the existing Session Revision commit, and the existing `GORMStore.AppendRunEvent` emits `evidence.retrieved` / `evidence.cited` with IDs only.
  - Existing shared Eino graphs move `{documents}` out of System into a separate User data message. Prompt text explicitly treats instructions, Secret requests, overrides and Tool suggestions inside Evidence as data only; the User message has no ToolCalls.
  - Retriever Trace output now stores only Evidence ID, source ID, score and a 160-rune summary; it no longer stores the retrieved content or metadata blob.
  - `save_intelligence` remains the sole P13 Catalog entry with `primary -> milvus_index`; P31 tests call the existing `buildDAG` and assert stable parent/child identity. No second Effect path or recovery worker was added.
- Unfinished / not run:
  - Real MySQL test for primary-commit/Milvus-before-index crash and derived re-claim is covered by existing P24 workflow tests but was NOT RUN because `SENTINELOPS_TEST_DSN` was not supplied. No provider, Milvus, MQ, Outbox or Docker service was invoked.
  - P32 and later units remain intentionally out of scope.
