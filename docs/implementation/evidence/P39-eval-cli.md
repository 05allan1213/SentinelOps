# P39 Eval 官方复核与最薄 Runtime CLI

- Status: `PASS`
- Started from: `b88e88ca6a3da58482019371d60930b9b3865a6b`（`main`，开工前工作树干净）
- Completed: 2026-08-26
- Spec references: 实施计划 P39；上位 Spec Task 11.1；Build-or-Reuse Gate
- Commit: 由仓库外实施计划台账记录本单元本地提交 SHA；未 push

## Boundary Audit

- 目标：实施日重新核对 Eino/Eino-ext 官方 Eval 能力；在没有稳定官方 Eval Runner 时，提供只负责 Case 校验、生产 API 提交、MySQL 真值等待、确定性比较和 JSON 报告的最薄 CLI。
- 明确非目标：不实现 Agent Loop、Eval Runner、Checkpoint Store、Tool Registry、Callback、持久化框架、第二套 Trace/Dashboard/反馈表；不创建 Dataset/baseline（P40）、CI/故障注入（P41）、发布控制（P42）或执行完整 Eval（P43）。
- 兼容契约：Run 只经 `POST /api/chat/v2/runs` 创建；执行仍由既有 Worker 完成；终态和 Tool/Recovery/Evidence 事实来自 `workflow_runs`/`workflow_events`，Approval/Effect 来自既有 Ledger，Latency/Token/Cost 来自既有 TraceDAO；`rageval` Dashboard 保持原实现。
- 安全不变量：Case 必须声明至少一个合法终态，未知字段、冲突 Expected/Forbidden Tool、非法 Recovery mode 和明显 Secret 输入（含结构化 JSON 敏感键）被拒绝，合法 SecretRef 不按明文误报；CLI 只接受 SecretRef，不输出 Authorization、DSN、模型输入或错误响应正文；Run 身份不一致、缺失/非 complete Trace、非法或未实际 cited 的 Evidence、禁止 Tool、重复 Effect 或持久化 Secret 泄漏均 fail-closed。
- 预计与实际范围：新增 `internal/ai/eval` 和 `cmd/agenteval`，原位增加 TraceDAO 的 workflow Run 关联读取及 metric-reuse contract test；不修改生产 Agent 拓扑、Worker、Schema、Migration、Dashboard 或模型配置。
- 验证方式：P39 contract tests、CLI tests、受影响包未过滤回归、`go vet`、`goimports`、CLI validate smoke、禁止重复 Runtime/Runner/Store/Trace/Dashboard/写数据库的静态扫描。
- 回滚方式：通过本单元独立本地提交的普通反向提交恢复；没有 Schema、数据、容器、remote 或部署状态变更。

## Official Capability Review

复核日期：2026-08-26。

| 查询 | 结果 | 判定 |
| --- | --- | --- |
| `git ls-remote --tags --refs https://github.com/cloudwego/eino.git` | 最高稳定标签 `v0.9.15`，commit `ebd616c8291e957684ea6ca99dd54225d04e0438` | 与仓库锁定版本一致 |
| `git ls-remote --heads https://github.com/cloudwego/eino-ext.git main` | `main` commit `6752ff8da9b1ea85c8e91b27b0a64fef099f22f9` | 作为无稳定标签模块的当前源码复核点 |
| GitHub recursive tree API 扫描 Eino `v0.9.15` | 没有 Eval/Evaluation/Evaluator 路径 | 无稳定 Eval 包或 Runner |
| GitHub recursive tree API 扫描 Eino-ext 上述 commit | 只命中 `devops/internal/apihandler/types/evaluation.go`；文件属于 `internal` 且仅声明 `package types`；另有 `skills/eino-agent/reference/runner-and-events.md` | 不存在可导入的稳定 Eval Runner |

结论：官方能力 Gate 为 `PASS`，满足计划的条件分支，保留项目最薄 Runtime Eval CLI；没有复制 Eino Runner。

## Build-or-Reuse

| 现有 SentinelOps 能力 | Eino / Eino-ext 官方能力 | 剩余业务缺口 | 最薄 Adapter |
| --- | --- | --- | --- |
| P20 生产 API/Worker/SSE、P08 Workflow/Event 真值、P21-P25 Approval/Effect Ledger、P35 Trace barrier/TraceDAO、既有 `rageval`、P06 Redactor、P04 SecretRef Resolver | Eino `v0.9.15` 提供生产 Agent/Runner/Callback 生命周期；实施日仍无稳定 Eval Runner | 缺少安全 Case 格式、生产 Run 提交、终态轮询、跨既有真值的确定性断言和无敏感正文的 CLI 报告 | `HTTPRuntimeAdapter` 只调用生产 Run API；`MySQLTruthReader` 只读既有表和 TraceDAO；`EvaluateCase` 做确定性比较；`agenteval` 只负责编排与 JSON 输出 |

## Red Evidence

首次执行：

```bash
go test ./internal/ai/eval -run 'Test(Eval|ProductionRuntimeAdapter|MetricReuse)' -count=1
```

结果为预期 `FAIL`：Case、生产 Runtime adapter、Observation/Compare 等契约尚不存在，编译报 `undefined`；失败命中新建 P39 测试，不是空过滤。

最终契约审计继续以负向断言验证并修复：生产 GoFrame API 的 `data` 包装、Trace 最新 Attempt 和 Run/Trace complete 双重真值；未知 Case 字段；Run 身份错配；Event-only Tool 无明确成功证据；Trace/Event Tool 保守合并；Evidence ID 形状、sticky invalid 与 cited/retrieved 区分；结构化 Case 及 Run/Event/Approval/Effect/Trace 持久化 Secret 扫描并豁免合法 SecretRef；重复 Effect 全局失败；Effect parking 不覆盖 Recovery mode；Authorization 生命周期清理。对应回归均为 `PASS`。

## Implementation

- `EvalCase` 使用 `sentinelops/eval-case/v1` 的严格 YAML/JSON 薄格式；未知字段、重复/冲突 Tool 条件、非法状态或 Recovery mode、明显 Secret 均在提交生产 Run 前拒绝；不创建 P40 Dataset、baseline、阈值或批准流程。
- `HTTPRuntimeAdapter` 只调用生产 `/api/chat/v2/runs`，解析 GoFrame `data` 响应；缺省 Session ID 使用长度受限的随机身份；拒绝带凭据、非法协议、query 或 fragment 的 base URL，不读取错误响应正文，并在结束时清除暂存 Header。
- `MySQLTruthReader` 轮询既有 Workflow 可评估终态，只读 Run、Event、Approval、Effect；按 Run owner 的现有 Trace scope 读取最新 Attempt，且只有 Workflow 与 Trace 均明确为 `complete` 才可成为成功证据。TraceDAO 支持绑定该 reader 的只读 DB 连接，注入式/隔离测试不会退回全局连接。
- Tool 指标以 Trace 节点和 Workflow Event 的保守并集为准：任一真值明确失败即失败；没有 Trace 节点且 Event 又缺少明确成功证据时不计成功；Event-only Tool 不会被部分 Trace 投影隐藏。Evidence 只有 canonical ID 有效且出现 `evidence.cited` 时才满足引用期望。
- 指标精确覆盖 Execution Success、Expected/Forbidden Tool、RBAC denial、Approval、Effect、Evidence、Resume/Replay/parked recovery、Tool Call Success Rate、Replan Count、Latency、cached/reasoning 拆分 Token 和 Cost；重复 Effect 与 Run/Event/Approval/Effect/Trace 中的 Secret 泄漏无条件使 Case 失败；报告不包含 Query、Prompt、响应正文或 Secret。
- `cmd/agenteval validate` 只校验 Case；`run` 使用 `--dsn-ref` 和可选 `--authorization-ref`，在一个有界 timeout 内依次提交 Case、等待真值并输出 JSON。任一 Case 未通过时进程返回非零。

## Actual Files

- `internal/ai/eval/types.go`
- `internal/ai/eval/evaluate.go`
- `internal/ai/eval/http_runtime.go`
- `internal/ai/eval/mysql_truth.go`
- `internal/ai/eval/eval_test.go`
- `internal/ai/eval/http_runtime_test.go`
- `internal/ai/eval/mysql_truth_test.go`
- `cmd/agenteval/main.go`
- `cmd/agenteval/main_test.go`
- `internal/dao/mysql/trace_dao.go`
- `internal/dao/mysql/trace_dao_p39_test.go`
- `internal/service/rageval/p39_metric_reuse_test.go`
- `docs/implementation/evidence/P39-eval-cli.md`

## Verification Ledger

| Gate | Command / evidence | Result |
| --- | --- | --- |
| Plan 精确 contract gate | `go test ./internal/ai/eval ./internal/service/rageval ./internal/dao/mysql -run 'Test(Eval|ProductionRuntimeAdapter|MetricReuse)' -count=1` | `PASS`：三个 package 均实际执行匹配测试，无 `[no tests to run]` |
| 受影响包未过滤回归 | `go test ./internal/ai/eval ./internal/service/rageval ./cmd/agenteval -count=1` | `PASS` |
| Trace evidence contract | `go test ./internal/dao/mysql -run 'Test(TraceIncompleteAggregateContract|MetricReuseWorkflowTraceLookupUsesScopedLatestAttempt)' -count=1` | `PASS` |
| Static analysis | `go vet ./internal/ai/eval ./internal/service/rageval ./cmd/agenteval ./internal/dao/mysql` | `PASS`，无输出 |
| Formatting | `goimports -l` 对全部 P39 Go 文件 | `PASS`，无输出 |
| CLI validate smoke | `go run ./cmd/agenteval validate --cases /dev/stdin`，输入一个非敏感合成 Case | `PASS`：`cases=1, passed=1`；Go 1.27 下 Sonic 输出已知 fallback warning，命令退出 0 |
| 禁止 Runtime/Runner/Store/Callback/Schema 复制 | 在 `internal/ai/eval`、`cmd/agenteval` 扫描 Agent/Runtime/Eino ADK/Callback 构造、`NewRunner`、`NewGORMStore`、`AutoMigrate`、`CREATE TABLE` | `PASS`：`rg` exit 1，为预期空结果 |
| 禁止第二 Dashboard/Trace VO/Feedback | 在上述新生产路径扫描对应 type/function | `PASS`：`rg` exit 1，为预期空结果 |
| 禁止 Eval 写数据库 | 在上述新生产路径扫描 GORM `Create/Save/Update/Delete/Exec/Transaction` | `PASS`：`rg` exit 1，为预期空结果 |
| 生产提交唯一性 | 在上述新生产路径扫描 `/api/chat/v2/runs` | `PASS`：仅 `internal/ai/eval/http_runtime.go` 一处 |
| 真实 API/Worker/MySQL/Provider Eval | 环境检查仅报告 Secret/URL 是否配置，不读取其值 | `NOT RUN`：`SENTINELOPS_TEST_DSN`、Eval API URL 和 Authorization Ref 均未提供；不以单元 double 冒充外部运行 |

## Key Assertions

- Eval 提交不会直接调用 Agent、Runner、Checkpoint、Tool Registry 或 Callback；生产执行仍只属于 API/Worker 链。
- Trace 缺失、`unknown` 或 `incomplete` 时，即使 Run 状态是 `succeeded`，Execution Success 仍为 false，Case 失败。
- 未知 Case 字段不会被 YAML/JSON 解码器静默忽略；Run API 返回身份与 MySQL 真值身份必须精确一致。
- Event-only Tool Result 没有显式成功证据时按失败处理；Trace/Event 投影任一侧明确失败即失败，Event-only 禁止 Tool 不会被部分 Trace 覆盖。
- 显式非法 Evidence 一旦出现，后续 Evidence 事件不能把它重新标成 valid；仅 retrieved、未 cited 的 Evidence 不满足引用期望。
- 重复 Effect 与任一已读取持久化真值中的明显 Secret 泄漏都是全局安全失败，不依赖 Case 是否显式声明 Forbidden。
- 普通 `effect_unknown` parking 不会伪装成 Recovery `parked as designed`；只有带 canonical recovery `mode=parked` 的事件才投影该模式。
- API 错误正文、Authorization、DSN、Case Query 和模型输出均不进入报告或证据。

## Deviations And NOT RUN

- 未修改 `internal/service/rageval/service.go`：现有 Dashboard/Trace 指标已覆盖 latency/token/cost/RAG，P39 只增加 contract test 证明继续复用；这是比计划推荐路径更小的原位复用。
- 未增加 `manifest/eval/cases`、baseline 或阈值比较：它们属于 P40，`NOT RUN`。
- 未运行真实 API/Worker/MySQL/Provider Case、40+ Case 三轮 Eval、Hosted CI、故障矩阵、Shadow/Cutover、发布或 P43 全量门禁：分别属于外部条件或 P40-P43，全部 `NOT RUN`。
- 未运行全仓测试、完整浏览器矩阵或镜像构建；P39 只声明上述局部门禁 `PASS`，不声明整个项目完成。

## Raw Artifact References

- 无大体积原始 Artifact；精确命令与短结果已记录于本文件。
- 未保存 Secret、完整 DSN、Authorization、Cookie、Token、模型输入或供应商响应正文。
