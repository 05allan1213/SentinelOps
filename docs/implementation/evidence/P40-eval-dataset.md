# P40 40+ Dataset、基线与阈值比较

- Status: `PASS`（P40 局部门禁）；代表性生产 Runtime `NOT RUN`
- Started from: `b0e4c2740f21789d2d19f661c3948b7575184b71`（`main`，P39 提交后）
- Completed: 2026-08-26
- Spec references: 实施计划 P40；上位 Spec Task 11.1～11.3
- Commit: 由仓库外实施计划台账回填本单元本地提交完整 SHA；未 push

## Boundary

- 只实现版本化 Dataset、严格覆盖校验、代表 Case 选择、批准基线格式、阈值比较和 Eval 报告聚合；生产执行仍只经过 P39 的 API/Worker Runtime Adapter 与 MySQL Truth Reader。
- 未实现 P41 GitHub Actions、集成/故障注入、P42 发布控制或 P43 全量 Eval；未修改 P41～P43 台账行。
- Dataset 固定八类、每类至少 5 Case、总数至少 40、成功/拒绝/恢复三种 outcome、每类恰好一个 representative；完整 Dataset 的 `repeat` 固定为 3。
- Snapshot 固定 provider-qualified Catalog Ref、candidate order、Provider/Driver revision、Routing Profile、Route Options、pricing currency/unit/rates/revision、模型 Snapshot identity，以及 Prompt/Tool/Skill/MCP/KB snapshot；Secret 不进入 Dataset、baseline 或报告。

## Red evidence

首次执行受影响包完整测试时，新增断言按预期暴露未收敛问题：

```text
go test ./internal/ai/eval ./cmd/agenteval -count=1 -v
FAIL: generated Dataset fixture indentation; manifest pricing fields absent; rerank Route Options and identity did not match runtime snapshot
```

修正 fixture、补齐实际 pricing/identity、固定 candidate order 和 representative 后，新增及受影响包测试均通过。

## Actual files

- `internal/ai/eval/dataset.go`
- `internal/ai/eval/dataset_test.go`
- `internal/ai/eval/types.go`
- `internal/ai/eval/evaluate.go`
- `internal/ai/eval/mysql_truth.go`
- `internal/ai/eval/mysql_truth_test.go`
- `cmd/agenteval/main.go`
- `cmd/agenteval/main_test.go`
- `manifest/eval/cases/01-routing-conversation.yaml` 至 `08-prompt-injection-security.yaml`
- `manifest/eval/baselines/pending.yaml`
- 删除伪造的 `manifest/eval/baselines/approved.yaml`
- `docs/implementation/evidence/P40-eval-dataset.md`

## Verification ledger

| Gate | Command / evidence | Result |
| --- | --- | --- |
| P40 focused contract tests | `go test ./internal/ai/eval -run 'Test(DatasetSchema|DatasetCoverage|Thresholds|BaselineApproval)' -count=1 -v` | `PASS`：4 个过滤测试均实际执行；覆盖 schema、40+/八类/outcome/representative、阈值和安全不变量、独立批准 baseline |
| Budget stop negative test | `TestMetricReuseDoesNotCountBudgetStopsAsActualOverrun` | `PASS`：`budget.exhausted` / `budget.usage_unknown` 仅表示停止/不确定 usage，不被计为实际预算越界；实测 latency/token/cost 超限仍 fail-closed |
| Affected package regression | `go test ./internal/ai/eval ./cmd/agenteval -count=1` | `PASS`：两包测试实际执行，无 `[no tests to run]` |
| Static analysis | `go vet ./internal/ai/eval ./cmd/agenteval` | `PASS`：无输出 |
| Formatting / whitespace | `gofmt -l`（全部 P40 Go 文件）；`git diff --check` | `PASS`：无输出 |
| Dataset CLI validation | `go run ./cmd/agenteval validate --cases manifest/eval/cases` | `PASS`：`repeat=3`、`cases=40`、`passed=40`；8 类 snapshot identity 全部按 `runtime.ModelSnapshot.Identity()` 校验 |
| Baseline write protection | `manifest/eval/baselines/pending.yaml` 使用 `status: pending`，CLI `compare` 只读已批准 baseline；功能运行没有 baseline 写路径 | `PASS`：pending template 不可作为批准 baseline 比较，批准字段需独立 approver/admin |
| Representative production Runtime | `go run ./cmd/agenteval run --cases manifest/eval/cases --sample-per-category 1` | `NOT RUN`：未提供生产 API base URL、Eval DSN/Authorization SecretRef，当前无 API/Worker 栈；不以 scripted unit double 或 provider preflight 冒充生产 Eval |

## Key assertions

- Dataset loader 严格拒绝未知字段、重复 ID、错误 schema/version/repeat、缺失类别/outcome/representative、未声明预算/外部依赖/契约、未限定的 Route Options、Secret 和不匹配的模型 Snapshot identity。
- 八类固定为 `routing_conversation`、`evidence_rag`、`mcp`、`skill`、`plan_replan`、`hitl_rbac`、`resume_replay_budget`、`prompt_injection_security`；required contract 覆盖 query_database 拒绝、三类 L0 read-only、mutation-disabled、Primary+Derived key、Approval fingerprint、legacy claim exclusion 和跨 Provider 同 Model ID。
- 同厂商 Model ID 的不同 Provider 保留不同 Catalog Ref、candidate order 和 Snapshot identity；比较不按厂商 Model ID 合并身份。
- `CompareBaseline` 只接受独立批准的 baseline，要求 Dataset/schema/version、完整 snapshot 集合和报告计数一致；任务成功率回归按百分点比较，P95/Token/Cost 按相对回归比较。
- 报告只保存 eval/case/run ID、状态、确定性指标和 snapshot identity，不保存 Query、Prompt、响应正文、DSN、Authorization 或模型 Secret。

## Unfinished / NOT RUN

- 代表性生产 Runtime/API/Worker/MySQL、真实供应商 Case、40+ Case × 3、Hosted CI、故障矩阵、P42 发布和 P43 全量门禁：`NOT RUN`，需外部环境/授权，按计划留给后续单元。
- `manifest/eval/baselines/pending.yaml` 不是批准基线；待生产代表 Case 真值和独立 approver/admin 批准后，才可生成不可变 approved baseline。

## Raw artifact references

- 无大体积原始 Artifact；本证据只记录脱敏命令、计数和状态。
- 未保存 Secret、完整 DSN、Authorization、Cookie、模型输入、响应正文或供应商原始返回。
