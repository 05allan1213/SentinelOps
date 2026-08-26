# P42 双层 Gate、Shadow、Cutover 与回滚证据

## Boundary Audit

- 目标：实现九项 canonical Gate 的 `frozen AND current static cap AND current dynamic switch` deny-only 语义，并覆盖 durable Run 创建、按版本认领/恢复、Approval/Effect、MCP、Skill、admin query 与 Langfuse Attempt 导出边界；提供可执行的发布/回滚 Compose 与 Runbook。
- 非目标：不执行 P43；不执行真实切流、灰度、共享/生产数据库迁移、Contract Migration、legacy Graph/Ops 删除、远端 push/rebase 或 Hosted CI。
- 兼容契约：继续复用唯一 `workflow.GORMStore`、`runtime.Worker`、`runtime.RuntimeHandler`、官方 Eino `adk.Runner`/`adk.WithCallbacks`、现有 MCP `SessionOwner`、Skill read-only Backend、Effect Ledger 与 MySQL Trace barrier；历史 Run 只从自身 frozen snapshot 重建兼容身份。
- 安全不变量：缺失、未知或非法动态值 fail-closed；当前 Gate 只能关闭历史 Run 能力；shadow 下 L1/L2 Mutation 为零；批准不能越过当前 Gate；不匹配 `runtime_version` 的 Worker 不得 claim；关闭 MCP/Skill/Langfuse/admin query 时不得先建立外部资源或调用 endpoint；rollback 不 Down Schema、不改写 Checkpoint/Effect 证据。
- 预计修改：`internal/ai/runtime` Gate evaluator、snapshot/profile/handler/runner/worker；`internal/ai/workflow` claim；settings API/service/DAO/seed；MCP/Skill 与 Langfuse 接线；静态配置、测试 Compose/release overlay；P42 Runbook、测试与本证据。
- 验证：先运行计划指定的红测；实现后运行过滤测试、直接受影响包回归、受影响包 `go vet`、P42 Go 文件 `goimports -l`、`git diff --check`、base/release Compose `config`、隔离 MySQL `sentinelops-p42` 验证与安全负向扫描。
- 回滚：发布时先关闭 `accept_new_runs/l1_writes/l2_writes`，safe-point drain 后只用匹配 `runtime_version` 的 Worker 收敛；应用可回退但不 Down Schema。本单元代码回滚只允许以后续独立提交完成，不改写历史。

## Build-or-Reuse

| 能力 | 复用结论 | P42 最薄缺口 |
| --- | --- | --- |
| Runtime/恢复 | 复用唯一 `runtime.Worker`、`DurableExecutor`、官方 `adk.Runner` 与 `workflow.GORMStore` | 增加唯一 Gate evaluator、exact runtime-version claim，并让恢复 hash 来自 Run frozen snapshot |
| Approval/Effect | 复用 P21-P25 Approval CAS、Effect Ledger、原 endpoint callback | 在 Resume 与每个 endpoint 前重新求 effective L1/L2，关闭时 safe-not-sent |
| MCP/Skill | 复用官方 MCP client/`SessionOwner`、官方 Skill middleware/read-only Backend | 在创建 Session/Backend 前读取当前 Gate |
| Langfuse | 复用官方 `callbacks/langfuse/v2.CallbackHandler` 与现有 Attempt barrier | 每个 Attempt 条件创建并通过官方 `adk.WithCallbacks` 挂载；不创建 callback bus |
| Settings | 复用现有 `settings` 表、DAO 与 admin RBAC | 同一事务更新完整九项向量并追加不可覆盖审计记录 |
| 发布 | 复用 base Compose 与既有 schema v6 | 增加显式 current/compatibility image overlay 和严格步骤 Runbook；Contract 审计若无清理对象则 no-op |

## Red / PASS 记录

### Red

- 单独保存的实现前 Red 日志：`NOT RUN`。本次工作在已有未提交 P42 实现上接续，没有通过回退生产代码伪造 Red 结果；以下最终局部门禁是本证据的权威执行记录。

### 精确 P42 门禁

~~~bash
go test ./internal/ai/runtime ./internal/service/settings ./internal/ai/effects \
  -run 'Test(EffectiveGate|FrozenSnapshot|ShadowMode|RollbackCompatibility)' -count=1

go test ./internal/dao/mysql -run 'TestEffectiveGateAudit' -count=1

go test ./internal/ai/workflow \
  -run 'Test(ShadowMode|RollbackCompatibility)' -count=1

go test ./internal/bootstrap \
  -run 'TestRollbackCompatibilityUsesFrozenGateAndCurrentSkillContent' -count=1
~~~

- 结果：`PASS`。
- 环境：隔离 Compose project `sentinelops-p42`、MySQL `8.0.43`、pinned goose `v3.27.3`；`SENTINELOPS_TEST_DSN` 只指向该 throwaway MySQL，未写入证据。
- 覆盖：九项 truth table、缺失/非法/旧别名 fail-closed、frozen=false 不可重开、shadow 零 Effect/业务 Mutation、`accept_new_runs=false` 不阻止已有 Run 由匹配版本 Worker 收敛、Runtime Gate 关闭时 reconciliation 不 claim、Run/Effect reconciliation exact `runtime_version`、Skill Gate 关闭时不建立 Backend 且不改写 frozen hash，Gate 重开后当前 Worker Skill 内容漂移仍被 compatibility hash 检出、settings 原子更新/回滚/审计与理由脱敏。
- 清理：执行 `docker compose -p sentinelops-p42 ... down -v --remove-orphans`；容器、网络和卷均删除，随后 `docker compose ls --format json` 返回 `[]`。

### 直接受影响包回归

~~~bash
go test ./api/settings/v1 ./internal/controller/settings ./internal/service/settings \
  ./internal/ai/effects ./internal/service/chat ./internal/ai/trace \
  ./internal/ai/agent/chat_pipeline ./internal/ai/agent/mcp_pipeline \
  ./internal/ai/agent/skill_pipeline ./internal/ai/tools/system \
  ./internal/bootstrap ./internal/config -count=1
~~~

- 结果：`PASS`。API/controller 包无测试文件；行为测试位于 service/runtime 等直接消费包。

~~~bash
go vet ./api/settings/v1 ./internal/controller/settings ./internal/service/settings \
  ./internal/ai/effects ./internal/service/chat ./internal/ai/trace \
  ./internal/ai/agent/chat_pipeline ./internal/ai/agent/mcp_pipeline \
  ./internal/ai/agent/skill_pipeline ./internal/ai/runtime \
  ./internal/ai/tools/system ./internal/ai/workflow \
  ./internal/bootstrap ./internal/config ./internal/dao/mysql
~~~

- 结果：`PASS`，无输出。

### Compose / release contract

- base Compose `config`：`PASS`。
- base + test Compose `config`：`PASS`；`runtime-gates` 必须在 E2E API/Worker 前完成。
- base + release Compose `config`：`PASS`；current API/Worker 使用同一 current image/runtime/config 三元组，compatibility Worker 使用独立 compatibility 三元组，两份完整配置均只读挂载到 `/app/manifest/config/config.local.yaml`。
- release triplet JSON 断言：`PASS`（最终复跑显式提供不可变 digest 与绝对配置路径）。
- 首次 JSON 断言未启用 `compatibility` profile，Compose 输出中没有该 profile service；加入 `--profile compatibility` 后按实际输出结构复核为 `PASS`，属于断言命令修正。

### 安全负向扫描

- `SENTINELOPS_E2E_ENABLE_MUTATION_GATES` 生产/测试旁路不存在：`PASS`。
- 生产代码不再读取 `observability.langfuse.enabled` 旧别名，且不存在全局 Langfuse Runtime：`PASS`。
- canonical Gate 集合精确为九项；旧 Langfuse 别名仅出现在拒绝测试：`PASS`。
- migrations 无 `00007`、无 P42 Contract DDL：`PASS`。
- legacy Run/Checkpoint/Ops 源码与 Schema 无删除：`PASS`。
- 两次早期扫描失败属于扫描定义错误：一次 glob 顺序重新包含 `_test.go`，一次 `[a-z_]+` 把 `l1_writes/l2_writes` 截断为 `agent_runtime.l`；修正扫描后均 `PASS`，不属于实现失败。
- 一次隔离 MySQL 脚本因清理函数包含被执行策略拒绝的 `rm -f` 而在启动测试前被拒绝；改为固定临时目录、精确 `unlink`/`rmdir` 后完整门禁 `PASS`，且最终 Compose project、临时 goose 与临时 Compose 文件均已清理。

### 宽回归诊断

- 带隔离 MySQL 的宽 affected-package 回归仍有两个已知基线失败：`TestNestedLedgerAgentToolLeafCreatesOnePrimary` 报 `budget reservation identity conflict`；`TestMigrationsUpFromCurrentSchemaSnapshot` 的旧 Schema fixture 已含后续 `content_hash`，迁移时报 duplicate column。P42 未删除测试、未弱化断言，也未越界修复 P26/旧 fixture。
- 未注入 `SENTINELOPS_TEST_DSN` 的 `go test ./internal/ai/runtime ./internal/dao/mysql -count=1` 也按夹具前置条件 `FAIL`；该命令不是 P42 门禁，不能替代上述隔离 MySQL 结果。

## Contract Migration 审计

- 结论：`PASS (no-op)`。当前仍存在真实 reader/审计/回滚用途，因此没有安全清理对象，不创建空 `00007`，不执行 Contract Migration。
- `internal/ai/workflow/legacy.go`：`LegacyCutoverStats`、`ListLegacyNonTerminalRunIDs`、`TerminalizeLegacyRuns` 仍读 legacy Run；durable claim 仍显式排除 `runtime_mode=legacy`。
- `internal/dao/mysql/model.go`：`WorkflowRun` 保留 legacy/audit 字段；`WorkflowCheckpoint` 保留 `checkpoint_key`、`snapshot_json`；`OpsRun`、`OpsRunStep` 仍建模。
- `internal/ai/workflow/store.go`：`LatestCheckpoint` 仍读取 legacy JSON checkpoint 字段。
- `internal/ai/workflow/eino_checkpoint_store.go`：durable opaque checkpoint 行仍有意填充 legacy compatibility 字段。
- `internal/dao/mysql/soar.go` 与 `internal/controller/ops/soar.go`：Ops 读、统计和人工 CRUD（`ListRuns`、`GetRun`、`GetRunSteps`、`GetOpsStats`、`ClearRuns`、`DeleteRun`）仍保留。
- 未发现 P42 migration diff、`00007` 或上述对象删除。物理删除只可在 P43 全部门禁、回滚演练和观察期条件满足后另行实施。

## 发布/回滚 Artifact

- `manifest/docker/docker-compose.release.yml`：显式 current/compatibility image、exact runtime version 与完整只读配置三元组。
- `docs/runbooks/P42-release-and-rollback.md`：严格包含发布 ①～⑫与回滚 ①～⑥；每步均列进入条件、动作、观测、停止条件和恢复判定。
- P42 只验证机制和文档可执行性；真实 Gate 打开、灰度、切流、legacy 删除、Contract Migration 和回滚演练均未执行。

## NOT RUN

- P43 全量验证、完整 `go test -race ./...`、完整前端 E2E、40+ Case 三轮 Eval、真实 provider、Hosted CI、真实灰度/切流/回滚、共享或生产数据库迁移、legacy 删除：`NOT RUN`（超出 P42 边界）。
