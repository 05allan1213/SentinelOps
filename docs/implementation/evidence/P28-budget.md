# P28 Durable Budget reserve/settle

- Status: `PASS`
- Started from: `b4121b42509777e5990ad46832f0c61176065aa1`; `main`; 开工时工作树干净
- Spec references: 上位 Spec `6.8`；执行 Plan `P28`
- Results: `PASS`（代码与无外部依赖局部验证）；MySQL 集成门禁 `NOT RUN`
- Unfinished items: 无 P28 代码项；P14/P28 MySQL 集成证据因缺少 `SENTINELOPS_TEST_DSN` 保留 `NOT RUN`

## Boundary Audit

- 目标：在 P14 已有 `workflow.GORMStore` reservation identity 和 CAS primitive 上补齐跨 Retry/Failover/Resume/Replay 的调用、control-plane、Token/Cost、MCP、RAG、并发与结果限制；每次调用前 durable reserve，调用后同一 identity settle。
- 明确非目标：不新建 Budget Store/Service、Runtime、模型 Gateway、RAG/MCP/Skill 生产调用链；不修改后续 P30/P32-P34 的真实接入；不迁移 P14 reservation identity；不执行在线供应商、共享数据库或 P43 全量验证。
- 兼容契约：保留 `sentinelops/run-base-budget/v1`、P14 Model/L0 Tool kind、已有 `ReserveBaseBudget`/`SettleBaseBudget` identity 语义；P14 旧 limits JSON 继续有效，新增维度为可选字段。
- 安全不变量：确定性 hard limit 在 endpoint 前由同一行锁 CAS 拒绝；同名跨 Provider 使用完整 Catalog Ref/snapshot identity；Secret 不进入 metadata；unknown usage 保留保守上界并阻止后续调用；pending reservation crash 后保留。
- 预计修改范围：`internal/ai/workflow/budget.go`、`internal/ai/runtime/budget.go`、`internal/ai/runtime/handler.go`、相关 P28 contract tests 与本证据文件。
- 验证方式：P28 精确 race 门禁、受影响包未过滤测试、`go vet`、`goimports`、`git diff --check`；MySQL 需要显式 `SENTINELOPS_TEST_DSN`，缺失则记录 `NOT RUN`。
- 回滚方式：反向本单元独立本地 commit；不修改 migration、remote 或部署状态。

## Build-or-Reuse

| 现有 SentinelOps 能力 | Eino / Eino-ext 官方能力 | 剩余业务缺口 | 最薄 Adapter |
| --- | --- | --- | --- |
| P14 `workflow.GORMStore` 的唯一 durable reserve/settle、P11 typed Runtime Context、P27 physical candidate metadata | Eino `schema.TokenUsage` 是模型实际 usage 来源；官方 planexecute 保留 Planner/Executor/Replanner 拓扑 | P14 只有 Model/L0 Tool 调用次数和时限，缺少维度矩阵、保守 token reserve、成本与 unknown quality | 扩展同一 JSON envelope、kind/identity 和 Handler wrapper；以 estimate/usage settlement 更新同一 reservation，不复制 Store |
| P27 Frozen Model Snapshot 的 Catalog Ref、Provider、Model ID、pricing identity | Eino Retry/Failover 已逐次进入绑定 Handler 的 physical endpoint | 成本需按 provider-qualified snapshot 查价，不能按裸 Model ID 合并 | 从已有 `ModelInvocation` 传递 Catalog Ref/snapshot identity 和 pricing fields，保持每候选 reservation 独立 |

## Implementation

- 扩展同一 `sentinelops/run-base-budget/v1` envelope，保留 P14 Model/L0 Tool reservation identity，并增加 Planner/Executor/Replanner、Retry/Failover、MCP/RAG/Skill kind、各类 limits、保守 estimate、实际 usage、usage quality 与 provider-qualified pricing metadata。
- `workflow.GORMStore` 继续是唯一 durable primitive：reserve 在同一 fenced workflow row CAS 内执行，settle 更新同一 reservation；pending reservation 在 crash/lease handoff 后保留，unknown usage 设置 durable `usage_unknown` 并阻止下一次 reserve。
- Runtime Handler 的 Model/Tool wrapper 对每次 physical call 传递 input estimate；Model 读取 Eino `schema.TokenUsage`，Tool 记录字符/字节，缺失 usage 进入 unknown；cached input 属于 input、reasoning 属于 output，成本按 frozen Catalog Ref/snapshot pricing 结算且不重复累计。
- 提供同一 primitive 的 `ControlPlaneBudget.ReserveControlPlane` contract，后续 RAG/MCP/Skill 只消费既有 kind/identity。

## Verification

PASS:

```bash
go test -race ./internal/ai/runtime -run 'Test(Budget|UsageUnknown|ControlPlane)' -count=1
go test -race ./internal/ai/workflow -run 'Test(BudgetDimensions|SettlementActual|ControlPlaneReservation)' -count=1
go test ./internal/ai/trace -run 'Test(UsageFromModel|ParseUsage)' -count=1
go test ./internal/... -run '^$'
go vet ./internal/ai/runtime ./internal/ai/workflow ./internal/ai/trace
git diff --check
```

以上命令均 `PASS`；race detector 无报告，全仓内部包编译通过，vet 与 diff check 无输出。

NOT RUN:

```bash
go test -race ./internal/ai/runtime ./internal/ai/workflow -run 'Test(RuntimeHandler|HandlerConcurrentIsolation|DynamicToolPolicy|BaseBudget|BudgetCrashReservation)' -count=1
go test ./internal/ai/runtime -count=1
```

两条命令均命中既有 MySQL 集成用例；本地未注入指向 P03 throwaway MySQL 的 `SENTINELOPS_TEST_DSN`，测试在 fixture guard 处退出。未修改测试绕过，也未将其写成 PASS。在线供应商、共享数据库、镜像、E2E、Chaos、Hosted CI、发布与 P43 均不属于 P28 局部门禁，未执行。

## Local Gate

- 确定性新增维度在同一 reserve CAS 前检查：`PASS`（contract tests）。
- Token/Cost 无 usage 不宣称零越界：`PASS`（unknown quality 与阻断 contract）。
- 跨 Provider Catalog Ref/snapshot 隔离：`PASS`（ModelInvocation 与 pricing metadata 继承 frozen snapshot）。
- P14 reservation identity/JSON 兼容：`PASS`（未迁移 schema、kind 或 identity；编译与既有 contract 保持）。
- 第二套 Budget Store/Service/local truth table：`PASS`（源码审计未发现）。
