# P07 Legacy Cutover、Session Revision 0 与 Session 占用

- Status: `PASS`
- Started from: `588ef7c5afe525285c23ed8ee1a1dff753876d20`；`main`；开工时工作树干净
- Spec references: 上位 Spec `6.1`、`6.9`、`7.2`、`7.8`、Task 2；执行 Plan `P07`、`2.1～2.7`
- Unfinished items: P07 无未完成实现；真实 legacy drain/终态化、共享数据库、发布授权与 P43 全量门禁均 `NOT RUN`；P08 及后续单元未实施

## Boundary Audit

- 目标：显式隔离 `legacy`、NULL runtime contract 与 `durable_v1`；提供只读分类/统计和显式 allowlist 的一次性审计终态化；以 MySQL 唯一约束并发建立历史 Session Revision 0；锁定所有非终态继续占用 `active_session_key` 的契约。
- 明确非目标：不实现新 Run 创建、状态机通用迁移、Event catalog、claim/lease/heartbeat、Eino CheckPointStore、Resume/Replay、Worker loop、真实 legacy drain、共享数据库 cutover 或 P08 及后续单元。
- 兼容契约：保留既有 legacy `CreateRun`/`FinishRun` 路径；旧终态 Run 只读保留；旧 Checkpoint 不转换为 Eino blob；Revision 0 仅使用 MySQL `user_preferences`，因当前 Schema 没有可靠的 MySQL 会话消息真值，History 明确为空。
- 安全不变量：`legacy` 或 runtime contract 任一字段为空都不满足 durable eligibility；Redis、进程内 SessionMemory、Trace 与旧输出不参与 Revision 0 或 immutable input 反推；cutover 只处理明确 allowlist，写审计 Event、`legacy_cutover_not_resumable`、终态和 Session 释放必须同事务；未获 P43 部署授权不接触真实数据。
- 预计文件：`internal/ai/workflow/legacy.go`、`session_revision.go` 及测试，`internal/dao/mysql/model.go` 及约束测试，`cmd/workflow-cutover` 一次性管理命令，本证据文件，仓库外执行 Plan 台账。
- 验证方式：先运行计划过滤命令保存预期 Red；实现后运行同一局部门禁、直接受影响包既有测试、`go vet`、命令包测试/构建、格式与禁止项扫描；MySQL 仅使用一次性测试库。
- 回滚方式：通过本单元独立本地提交的普通反向提交恢复；不执行数据库 Down，不修改 remote，不 push；测试库由测试清理。

## Build-or-Reuse

- 现有代码能力：原位复用唯一 `workflow.GORMStore`、P03 的 `workflow_runs` / `workflow_events` / `workflow_checkpoints` / `session_state_revisions` Expand Schema、nullable `active_session_key` 唯一索引和 MySQL 测试夹具；复用 `user_preferences` 作为当前唯一可证明的 durable Session 状态来源。
- Eino / Eino-ext 官方能力：锁定版 Eino 的 CheckPointStore/Runner 负责未来 opaque checkpoint 生命周期，不提供 SentinelOps 的 legacy 分类、MySQL cutover、Session Revision 0 或 active Session 唯一约束；本单元不包装、不复制 Eino Checkpoint，也不新增 Agent Runtime。
- 剩余业务缺口：现有 GORM 模型未映射 P07 消费的 Expand 列；没有 canonical durable eligibility predicate、legacy 统计/审计终态化或并发 Revision 0 bootstrap；旧 `success` 状态和新 `succeeded` 需要在 cutover 分类中同时视为终态，但通用状态迁移留给 P08。
- 最薄实现：在现有 workflow 包增加纯分类/查询和同一 GORM Store 的管理事务，在现有 MySQL model 映射必要列；一次性 CLI 只解析 SecretRef、默认只读预览并要求显式 allowlist/确认，不创建第二套 Store、Runtime、Queue、Checkpoint codec 或 Session Lock 表。

## Red evidence

先加入计划命名的 legacy、Revision 0、active Session 和 cutover 测试，再执行：

```text
$ go test ./internal/ai/workflow ./internal/dao/mysql \
    -run 'Test(Legacy|RevisionZero|ActiveSession|Cutover)' -count=1
# SentinelOps/internal/ai/workflow [SentinelOps/internal/ai/workflow.test]
internal/ai/workflow/legacy_test.go:21:39: unknown field RuntimeMode ...
internal/ai/workflow/legacy_test.go:21:52: undefined: RuntimeModeLegacy
internal/ai/workflow/legacy_test.go:21:97: unknown field ImmutableInputJSON ...
# SentinelOps/internal/dao/mysql [SentinelOps/internal/dao/mysql.test]
internal/dao/mysql/p07_active_session_test.go:20:4: unknown field RuntimeMode ...
internal/dao/mysql/p07_active_session_test.go:21:4: unknown field ActiveSessionKey ...
FAIL SentinelOps/internal/ai/workflow [build failed]
FAIL SentinelOps/internal/dao/mysql [build failed]
```

- `PASS`（预期 Red）：命令退出 1，两个目标包均因 P07 所需 runtime model、隔离谓词、Revision 0 和 cutover 符号不存在而编译失败；不是环境错误、`[no tests to run]` 或已有实现直接变绿。

## Implementation result

- `internal/ai/workflow/legacy.go` 在唯一 `GORMStore` 上提供 legacy/durable 分类、P09 必须复用且不可放宽的完整 runtime contract 谓词、legacy terminal/non-terminal/cutover/durable 统计、只读候选查询和显式 allowlist 审计终态化。
- cutover 对选中 Run 加行锁，从数据库 Event 最大 seq 与 `last_event_seq` 的较大值分配审计 seq；`failed`、`legacy_cutover_not_resumable`、`run.failed` version 1 payload、finished/duration、`last_event_seq` 与 `active_session_key=NULL` 同事务提交。审计 Event 注入失败时全部回滚；未选中的 legacy Run 保持旧状态，可继续旧 Runtime drain。
- `session_revision.go` 以 `(session_id, revision)` 唯一约束和 insert-if-absent 并发建立唯一 Revision 0；公开入口沿用 P05 Scope 校验，P08 可在其创建 Run 的同一事务中复用包内 primitive。内容只读取 `mysql.user_preferences`；当前没有可靠 MySQL 会话消息真值，因此明确记录空 History 和来源，不读取 Redis、进程缓存、Trace 或旧输出。
- MySQL model 只映射 P07 消费的 Expand 列，并将 Event seq/Revision 对齐为 unsigned；既有 legacy `CreateRun` 仍由数据库默认写 `runtime_mode=legacy`。P03 的“从旧 Schema Up”测试改用冻结旧模型，避免后续生产 model 演进篡改历史 Schema fixture。
- `cmd/workflow-cutover` 复用 P04 `SecretRef`/唯一 Resolver、P03 Schema 检查和现有 `GORMStore`；默认只读 preview。写操作同时要求 `--execute`、精确确认短语和逐个 `--run-id` allowlist，且最多 1000 个；没有 bulk-all、Redis 或旧 Checkpoint 转换入口。

## Local commands and results

所有 MySQL 测试只连接本机 `dev-mysql` 上由测试创建/清理的 `sentinelops_p03_*` 一次性数据库；DSN 由进程内环境传入且未写入证据。`goose v3.27.3` 安装在 `/tmp/sentinelops-p07-bin/goose`，未进入工作树。

### P07 计划局部门禁

```text
$ go test ./internal/ai/workflow ./internal/dao/mysql \
    -run 'Test(Legacy|RevisionZero|ActiveSession|Cutover)' -count=1 -v
PASS: workflow 9 个 P07 Case
PASS: mysql 的 active Session 唯一约束 Case
PASS: 既有 TestLegacyApplicationModelsReadExpandSchema 回归
ok SentinelOps/internal/ai/workflow
ok SentinelOps/internal/dao/mysql
```

- `PASS`：legacy 与三个 NULL runtime contract fixture 均不满足 durable eligibility，只有完整 `durable_v1` 命中。
- `PASS`：16 个并发 bootstrap 全部返回同一条 Revision 0；有/无 MySQL preference、空 durable History、跨用户拒绝以及无 Redis/进程内/Trace source 均通过。
- `PASS`：waiting_approval、retryable_failed、parked、reconciling 连同 pending/running 均保留占用；每个状态下第二个相同 `active_session_key` 都由 MySQL 唯一索引拒绝，置 NULL 终态后才可复用。
- `PASS`：选中 legacy Run 终态、审计 Event、统计和 Session 释放通过；未选中 parked legacy Run 保持不变；旧 Checkpoint 的 `eino_checkpoint_id`/blob 仍为 NULL；durable Run 被 cutover 拒绝；MySQL trigger 强制审计 Event 失败时 Run/Session/seq 全部回滚。

### 直接受影响回归、静态检查与 race

```text
$ go test ./internal/ai/workflow ./internal/dao/mysql \
    ./cmd/workflow-cutover -count=1
ok SentinelOps/internal/ai/workflow
ok SentinelOps/internal/dao/mysql
ok SentinelOps/cmd/workflow-cutover

$ go vet ./internal/ai/workflow ./internal/dao/mysql \
    ./cmd/workflow-cutover
PASS

$ go test -race ./internal/ai/workflow ./internal/dao/mysql \
    -run 'Test(Legacy|RevisionZero|ActiveSession|Cutover)' -count=1
ok SentinelOps/internal/ai/workflow
ok SentinelOps/internal/dao/mysql
```

- `PASS`：两个直接受影响包的未过滤既有测试及命令包测试全部通过；P03 空库/旧 Schema/Schema contract 回归随 `internal/dao/mysql` 未过滤测试真实执行。
- `PASS`：`go vet` 无输出；最终当前工作树上的并发 Revision 0、cutover 和 active Session Case 通过 race detector。

### 一次性命令 smoke 与禁止项扫描

```text
$ go run ./cmd/workflow-cutover --dsn-ref=env:P07_CLI_DSN --limit=10
{
  "mode": "preview",
  "stats": {
    "legacy_terminal": 0,
    "legacy_non_terminal": 0,
    "cutover_terminalized": 0,
    "durable_eligible": 0
  },
  "candidates": []
}

$ rg ... internal/ai/workflow/legacy.go \
    internal/ai/workflow/session_revision.go cmd/workflow-cutover
P07_FORBIDDEN_PATHS=PASS
```

- `PASS`：命令在独立 `sentinelops_p07_cli_smoke` 数据库完成 Schema 检查与只读 preview，finally 已删除该数据库；没有执行 `--execute` 或接触现有应用数据。
- `PASS`：生产路径不存在 Redis/cache/SessionMemory、Checkpoint blob/Eino ID 转换、AutoMigrate/DDL；源码扫描只发现既有 `workflow.Store`/`GORMStore`，没有第二套 Store/Runtime/Queue/Session Lock/Event catalog。
- `PASS`：`go.mod`、`go.sum`、migrations、配置、Compose、现有本地/生产数据和 remote 均未修改；`git diff --check` 无输出。

## Key assertions

- `runtime_mode=legacy` 或 immutable input/runtime version/compatibility hash 任一 NULL/空白时，canonical durable predicate 不命中；P09 只能追加 claim 条件，不得放宽。
- cutover 是显式 allowlist、最大 1000 行、默认只读；不存在“终止不可接受仍交给新 Worker”的路径。未选中旧 Run 继续旧 Runtime，真实选择与执行留给 P42 Runbook/P43 授权。
- terminalization 不补造 immutable input，不写 Eino Checkpoint blob，不读取 Redis/Trace；审计失败或 selection 混入 durable/终态/不存在 Run 时事务不改变任何目标。
- Revision 0 的 JSON 明确包含 `fo/session-state/v1`、revision 0、空 summary、空 History 与 provenance；MySQL preference 存在时精确保留，并发只产生一行。
- `RunOccupiesSession` 对未知状态 fail-closed；只有旧 `success`、新 `succeeded`、failed、canceled 被视为终态。P07 只冻结占用分类，正常获取/迁移/释放事务仍由 P08 唯一实现。

## Deviations from recommended route

- 推荐文件落点基本保持；一次性管理入口落在现有项目惯例可构建的 `cmd/workflow-cutover`，不修改主应用角色解析或 bootstrap 生命周期。
- 当前 Schema 没有可靠的 MySQL 会话消息表；按 Spec 允许的“可靠历史为空”规则，Revision 0 不把 Redis、Trace 或旧输出升级为 truth，只迁移 MySQL preference 并明确写空 History/provenance。
- P07 为审计 Event 消费 Spec 已锁定的字符串 `run.failed`，没有导出 Event 常量或建立 catalog；完整 envelope/catalog 仍由 P08 唯一定义。
- 生产 GORM model 增加 P07 必需列后，P03 的旧 Schema fixture 必须冻结为测试专用旧模型；这是维持既有 migration contract 的直接回归修复，不修改 migration 或 Schema。

## Final scope statement

P07 局部门禁为 `PASS`。本结论只覆盖 legacy/durable 隔离、一次性可审计 cutover、Revision 0 bootstrap 与 active Session 占用契约；不代表 durable Worker 或整个二次开发完成。真实 legacy drain/终态化、共享数据库 Migration/cutover、发布、P08+ 和 P43 全量门禁均 `NOT RUN`。
