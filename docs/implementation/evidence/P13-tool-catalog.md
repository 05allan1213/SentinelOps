# P13 Strict Registry、Mutation Catalog 与 inventory

- Status: `PASS`
- Started from: `cf0ea4ce981dbb328fa453f80add3cf1b7dcc6ea`；`main`；开工时工作树干净
- Spec references: 上位 Spec `3.1`、`3.4`、`6.3`、`6.4`、Task 3C；执行 Plan `P13`、`2.1～2.7`
- Actual files: `internal/ai/tools/registry.go`、`registry_test.go`、`init.go`、`ops/notify_tools.go`、`ops/trigger_soar.go`、`ops/trigger_soar_test.go`；`internal/ai/policy/catalog.go`、`catalog_test.go`、`redact.go`；`internal/ai/prompt/agents/ops.go`；`internal/bootstrap/worker.go`；`manifest/agent/tool-inventory-v1.yaml`；本证据文件
- Red test and expected failure: 先加入 strict Registry、Catalog 固定矩阵、inventory least-privilege、`query_database` 隔离和 mutation-disabled 测试；精确门禁因 P13 Catalog、strict resolver、inventory API 和 manifest 尚不存在而预期编译失败，退出 1
- Local commands: P13 精确过滤测试与 Case 枚举、`tools/...` / `policy` / `bootstrap` 直接回归、filtered race、`go vet`、scoped `staticcheck`、`goimports`、`git diff --check` 和 Registry/Catalog/query_database/trigger_ops/Gate 禁止项扫描
- Results: `PASS`
- Key assertions: Registry 单实例真值；启动严格校验名称、数量、重名、`ToolInfo.Name`、Catalog 与 canonical schema hash；固定 L0/L1/L2 Catalog 和 deterministic Effect steps；未知 Tool 与 L1/L2 在 endpoint 前 fail-closed；专业 durable inventory 最小权限；`query_database` 默认不可注册；`trigger_ops` 只规划 Proposal
- Deviations from recommended route: 推荐文件均采用；为使固定 L0 语义与真实 endpoint 一致，原位收缩 `trigger_ops` 并移除 Worker 注入，同时同步 ChatOps Prompt；为 Catalog 固定矩阵补齐已有 `webhook_out` action 的薄 Eino Tool Schema，但不加入专业 Agent inventory；scoped staticcheck 暴露的既有 `redact.go` S1008 以等价机械修改修复
- Raw artifact references: 无
- Unfinished items: P13 无未完成实现；P14 RuntimeHandler、P15 Executor、P17～P19 Agent 迁移、admin/debug query builder、Approval/Effect、MCP/Skill、前端、镜像、在线供应商、Hosted CI、发布动作和 P43 全量门禁均 `NOT RUN`

## Boundary Audit

- 目标：在现有 `internal/ai/tools` Registry 原位增加 durable 必用的严格解析；在 `internal/ai/policy` 建立不持有 Tool 实例的服务端 Catalog 侧表；固化专业 Agent durable inventory；在 RuntimeHandler 尚未实现前，使未知 Tool 与 L1/L2 的执行判定 fail-closed。
- 明确非目标：不实现 P14 RuntimeHandler、预算、Trace、Scope/deadline middleware，不迁移专业 Agent、不启用 durable 业务 Agent、不实现 Approval/Effect Ledger、admin/debug builder、通用 SQL 解析/改写、MCP/Skill 接线、前端、Schema、Compose、镜像或发布。
- 兼容契约：保留 `GetMany` 供 legacy 路径；Catalog 不持有 Tool 实例或 endpoint；普通 admin HTTP CRUD 继续走 Service/Audit；旧专业 Runnable Graph 本单元不切流，durable inventory 只冻结 P17～P19 将消费的 least-privilege 合同；Scheduler/Ingest/Controller 的旧 ops engine 路径不删除。
- 安全不变量：durable Tool 名称缺失、重名、`ToolInfo.Name` 或 schema hash 漂移均阻止启动；未知 Tool fail-closed；L1/L2 在 endpoint 前返回结构化 `POLICY_MUTATION_DISABLED`；`query_database` 不在默认 Registry 或普通 durable inventory；EventAnalysis/Risk/Solve 只含 L0；L0 `trigger_ops` 不触碰旧执行函数或任何叶子 endpoint。
- 预计文件：`internal/ai/tools/registry.go`、`init.go` 及测试；`internal/ai/policy/catalog.go` 及测试；`manifest/agent/tool-inventory-v1.yaml`；本证据文件。实现审计确认还需原位收缩既有 `trigger_ops`、移除已失效的 Worker 注入、同步对应安全指令，并为已有 webhook action 补薄 Tool Schema。
- 验证方式：先运行 P13 精确过滤测试保存预期 Red；实现后运行执行 Plan 精确门禁、直接受影响包回归、race、`go vet`、`staticcheck`、`goimports`、差异与禁止项扫描。
- 回滚方式：通过本单元独立本地提交的普通反向提交恢复；不修改 Schema、remote 或历史，不 push。

## Build-or-Reuse

| 现有 SentinelOps 能力 | 锁定版 Eino/Eino-ext 能力 | 剩余业务缺口 | 最薄实现及删除条件 |
| --- | --- | --- | --- |
| 全局 `internal/ai/tools` 单实例 map、兼容 `Get` / `GetMany` / `All`；P06 `RiskLevel`、canonical JSON/hash、RBAC；现有专业 Tool 白名单与 ops action Registry | Eino v0.9.15 `tool.BaseTool.Info(context.Context)`、`schema.ToolInfo.Name`、`ParamsOneOf.ToJSONSchema()` 和既有 Tool 实例；官方不拥有 SentinelOps 风险、Effect、RBAC 或专业 Agent inventory | 严格批量解析、注册重名检测、schema hash 校验、服务端风险/Effect 元数据、least-privilege inventory 与 mutation-disabled 决策 | 原位扩展唯一 Registry；Catalog 只保存静态 metadata 和 inventory name，不保存实例/endpoint；直接从官方 ToolInfo 计算 canonical schema hash，不新建 Schema/Tool Gateway/数据库表。若 Eino 未来直接提供所需严格解析与业务 metadata hook，保留 contract test 后收缩对应胶水 |

- Gate 判定：`PASS`。Eino 已提供唯一 Tool schema 真值但不提供 SentinelOps 的风险/Effect/Policy 元数据；现有 Registry 已持有唯一实例但会静默跳过缺失并覆盖重名。P13 只补项目特有 fail-fast 胶水，不创建第二套 Registry、Schema 系统、Action Registry 或运行时 Wrapper。

## Red evidence

先加入计划要求的 P13 行为测试，再执行：

```text
$ go test ./internal/ai/tools ./internal/ai/policy \
    -run 'Test(GetManyRequired|Catalog|Inventory|MutationDisabled|QueryDatabase)' -count=1
internal/ai/policy/catalog_test.go:36:13: undefined: CatalogEntries
internal/ai/policy/catalog_test.go:69:15: undefined: LookupCatalog
internal/ai/policy/catalog_test.go:75:10: undefined: RequireExecutable
internal/ai/tools/registry_test.go:23:18: undefined: policy.RequiredDurableToolNames
internal/ai/tools/registry_test.go:24:14: undefined: GetManyRequired
FAIL SentinelOps/internal/ai/tools [build failed]
FAIL SentinelOps/internal/ai/policy [build failed]
```

- `PASS`（预期 Red）：命令退出 1，两个目标包因 P13 strict Registry、Catalog、inventory 与 mutation-disabled API 尚不存在而编译失败；不是环境错误、`[no tests to run]` 或已有实现直接变绿。

## Implementation result

- `GetManyRequired` 在唯一全局 Registry 的同一读锁内原子检查请求重名、缺失、注册次数、返回数量、`ToolInfo.Name`、Catalog audience 与 schema hash；`GetMany` 保持 legacy 静默兼容。`init()` 完成静态注册后立即校验全 Catalog 和默认 Registry，任何漂移都会 panic 阻止启动。
- `ToolSchemaHash` 直接使用 Eino v0.9.15 `ParamsOneOf.ToJSONSchema()`，经 P06 canonical JSON 后计算 SHA-256；Catalog 固定 canonical name、L0/L1/L2、revision、schema hash、Policy、Effect type 和 deterministic primary/derived steps，不保存 Tool 实例、endpoint 或动作函数。
- 首版矩阵覆盖领域/Evidence/外部只读 Tool，L1 `create_report` / `save_intelligence` / `update_event_status`，L2 `block_ip` / 三类通知 / `webhook_out`。`save_intelligence` 固定 `primary → milvus_index`，`block_ip` 固定 `primary → nginx_reload`；未知名称必须先更新 Catalog 才可能进入 durable builder。
- 六个专业 Agent inventory 同时以服务端内存真值和 `sentinelops.agent.tool-inventory/v1` manifest 固化，并由测试逐项比对防漂移。EventAnalysis 移除 `save_intelligence`，EventAnalysis/Risk/Solve 均为 L0-only；Report/Intelligence/Ops 可见的 Mutation 仍只能得到 `POLICY_MUTATION_DISABLED`。
- `query_database` 构造器保留供未来独立 admin/debug builder，但从默认 Registry 和所有普通 durable inventory 移除。专用 Catalog Policy 固定 admin、默认关闭 Gate、SELECT-only 和 `events/reports/subscriptions` allowlist；本单元未实现 builder、SQL parser/rewriter 或 Scope 注入，因此当前无法注册或解析。
- `trigger_ops` 从异步调用旧 engine 收缩为只读规划：先读取事件，再对模型给出的每个叶子 Mutation 名称查 Catalog、拒绝未知/L0/重名项、canonicalize 无重名 key 的 JSON object，最后返回服务端 revision/schema/risk/Effect steps；不调用 action、DAO mutation、通知、Nginx 或旧 TriggerFunc。Worker 删除该函数注入，ChatOps Prompt 同步为“只规划、不声称执行”。旧 engine 仍由 Scheduler/Ingest/Controller 原路径拥有，未在 P13 删除或迁移。
- 既有 `webhook_out` action 只补一个不接受 auth token 的 Eino Tool Schema，纳入 L2 Catalog 和严格启动校验，但不进入任何专业 Agent inventory；Secret 仍须后续 Effect 执行时通过 P04 Resolver 即时解析。
- `RequireExecutable` 对未知 Tool 返回可 `errors.Is` 的 fail-closed error，对 L1/L2 返回带 `code/tool_name/risk_level` 的 `ToolPolicyError{Code: POLICY_MUTATION_DISABLED}`；测试的 endpoint counter 保持 0。P14 只允许把该决策接入唯一 RuntimeHandler，不得再建 Gateway/Wrapper。

## Local commands and results

### P13 计划局部门禁

```text
$ go test ./internal/ai/tools ./internal/ai/tools/ops ./internal/ai/policy \
    -run 'Test(GetManyRequired|Catalog|Inventory|MutationDisabled|QueryDatabase)' -count=1
ok SentinelOps/internal/ai/tools
ok SentinelOps/internal/ai/tools/ops
ok SentinelOps/internal/ai/policy

$ go test ./internal/ai/tools ./internal/ai/tools/ops ./internal/ai/policy \
    -list 'Test(GetManyRequired|Catalog|Inventory|MutationDisabled|QueryDatabase)'
TestGetManyRequiredFailsFast
TestQueryDatabaseIsNotRegisteredForDurableUse
TestInventoryTriggerOpsPlanningOnly
TestCatalogFixedRiskMatrixAndMetadata
TestCatalogUnknownFailsClosedAndMutationDisabledBeforeEndpoint
TestInventoryMatchesManifestAndLeastPrivilege
```

- `PASS`：6 个顶层 Case 被发现并实际执行，无 0 tests；子 Case 另覆盖缺失、请求/注册重名、返回数、`ToolInfo.Name`、schema drift、未知 Tool、L0/Mutation 矩阵、Proposal 重名与 JSON 重名 key。

### Race 与直接受影响回归

```text
$ go test -race ./internal/ai/tools ./internal/ai/tools/ops ./internal/ai/policy \
    -run 'Test(GetManyRequired|Catalog|Inventory|MutationDisabled|QueryDatabase)' -count=1
ok SentinelOps/internal/ai/tools
ok SentinelOps/internal/ai/tools/ops
ok SentinelOps/internal/ai/policy

$ go test ./internal/ai/tools/... ./internal/ai/policy ./internal/bootstrap -count=1
ok SentinelOps/internal/ai/tools
?  SentinelOps/internal/ai/tools/event [no test files]
?  SentinelOps/internal/ai/tools/intelligence [no test files]
ok SentinelOps/internal/ai/tools/ops
?  SentinelOps/internal/ai/tools/report [no test files]
?  SentinelOps/internal/ai/tools/system [no test files]
ok SentinelOps/internal/ai/policy
ok SentinelOps/internal/bootstrap
```

- `PASS`：strict Registry 共享状态在 race detector 下通过；所有 Tool 子包、Policy 与移除 Worker 注入后的 bootstrap 编译/回归通过。`[no test files]` 只出现在没有测试的叶子包，不用于证明 P13 行为；P13 的 6 个 Case 已由精确门禁实际执行。

### 静态、格式与源码唯一性

```text
$ go vet ./internal/ai/tools/... ./internal/ai/policy ./internal/bootstrap ./internal/ai/prompt/agents
PASS

$ staticcheck ./internal/ai/tools ./internal/ai/tools/ops \
    ./internal/ai/policy ./internal/bootstrap ./internal/ai/prompt/agents
PASS

$ goimports -l <P13 Go files>
无输出

$ git diff --check
PASS

$ <Catalog/Registry/query_database/trigger_ops/Gate 禁止项扫描>
QUERY_DATABASE_DURABLE_INVENTORY_ABSENT=PASS
QUERY_DATABASE_DEFAULT_REGISTRATION_ABSENT=PASS
CATALOG_HAS_NO_TOOL_INSTANCE_TYPE=PASS
REGISTRY_INSTANCE_MAPS=1
TRIGGER_OPS_EXECUTOR_REFERENCE_ABSENT=PASS
agent_runtime.enabled=false
```

- `PASS`：Catalog 没有 Eino Tool 实例类型或 endpoint 字段；`internal/ai/tools` 只有一个 `map[string]tool.BaseTool` 实例真值；默认配置仍关闭 durable Agent；P13 未修改依赖、Migration、数据库模型、HTTP、Compose 或镜像。
- scoped staticcheck 初次发现既有 `internal/ai/policy/redact.go` S1008；按仓库机械修复规则等价收缩为布尔返回后重跑 `PASS`。一次探索性的全 `tools/...` staticcheck 另发现未改动 `intelligence/web_search.go` 的既有 ST1005；该非 P13 gate 诊断为 `FAIL`，源码保持不动，最终对所有直接受影响包的 scoped staticcheck 为 `PASS`。
- Sonic 在 Go 1.27 上打印已有的性能路径 warning 并回退标准 `encoding/json`；所有命令退出码和断言仍为 PASS。
- 前端、镜像、完整 `go test -race ./...`、完整 E2E/Eval/故障矩阵、在线供应商、共享数据库、Hosted CI、发布和 P43 全量门禁均 `NOT RUN`，不属于 P13 局部门禁，也不据此宣称整体改造通过。

## Key assertions

- durable builder 只能按 inventory 名称从现有 Registry 取得同一 Tool 实例；Catalog 是纯 metadata 侧表，manifest 只是经测试锁定的版本化合同，不是第二个实例 Registry。
- 任一默认 Tool 的参数 Schema、实际名称、注册次数或 Catalog entry 漂移都会在包初始化时阻止启动；运行期未知动态 Tool 也只能 fail-closed，不能信任 Prompt、Tool description 或 MCP/Skill 自报风险。
- L0 没有 Effect metadata；L1/L2 必须有稳定 `primary`，derived step 由服务端静态定义。Mutation-disabled 决策不创建 Approval/Effect，也不触碰原 endpoint。
- `query_database` 当前既不默认注册，也不在 durable manifest；仅保留未来 admin/debug 专用 Policy metadata，不能被普通用户 Scope 或通用 SQL 替代领域 Tool。
- `trigger_ops` 只产生确定 Proposal 数据，不产生业务 Effect；其输入不能临时命名未知动作，Catalog 元数据由服务端覆盖而非模型提供。原 ops engine 业务执行仍是 legacy 路径，未伪装成 L0 Tool。
- durable Agent Feature Gate 仍关闭；P13 只建立安全基础，不调用 Runner 执行业务 Agent。

## Deviations from recommended route

- 推荐的 Registry、Catalog 与 manifest 落点全部采用。额外修改 `trigger_soar.go`、Worker 注入和 ChatOps Prompt，是因为只改 Catalog 会把实际异步写 endpoint 错标成 L0，违反固定风险矩阵；该收缩不迁移或删除旧 ops engine。
- `webhook_out` 已有 action 但没有 Eino ToolInfo，无法形成可校验的 schema hash；因此只补最薄 InvokableTool Schema 并默认注册供 strict startup 校验，不加入 Agent inventory、不实现新的动作或 Effect 路径。
- `query_database` 采用“保留构造器、默认不注册”的更严格关闭态；admin/debug builder 留给后续明确单元，本单元不提前扩张 SQL 能力。
- scoped staticcheck 的 `redact.go` 机械修复不改变行为、接口或安全契约；未修复更宽扫描发现的无关 `web_search.go` 既有风格告警。

## Final scope statement

P13 局部门禁为 `PASS`。本结论只覆盖 strict Registry、服务端 Mutation Catalog、专业 durable inventory、`query_database` 隔离、L1/L2 mutation-disabled 决策与只读 `trigger_ops` 规划；不代表 P14、durable 业务 Agent、Approval/Effect、整个二次开发或 P43 全量门禁完成。
