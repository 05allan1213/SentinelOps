# P10 Eino opaque CheckPointStore Adapter

- Status: `PASS`
- Started from: `125371cb3883d85ecad7492f0bfb7c2237e5320e`；`main`；开工时工作树干净
- Spec references: 上位 Spec `7.2`、`7.5`、Task 3B；执行 Plan `P10`、`2.1～2.7`
- Actual files: `internal/ai/workflow/eino_checkpoint_store.go`、`p10_eino_checkpoint_store_test.go`，`internal/dao/mysql/model.go`、`migrations_test.go`，本证据文件
- Unfinished items: P10 无未完成实现；P11 Runtime Snapshot/typed Runtime Context、P12 Resume/Replay/PARKED selector、共享/生产数据库、镜像、在线供应商、Hosted CI、发布动作和 P43 全量门禁均 `NOT RUN`

## Boundary Audit

- 目标：在唯一 `workflow.GORMStore` 上直接实现 Eino `CheckPointStore` / `CheckPointDeleter`；使用 P09 唯一 `LeaseToken` 对 opaque bytes 做 generation-fenced upsert，并持久化 payload SHA、Run runtime identity 与 generation。
- 明确非目标：不实现 P11 Runtime Snapshot/typed Runtime Context，不实现 P12 Resume/Replay/PARKED selector，不启用 durable Agent，不修改 Migration、Agent、Approval、Effect、Budget、API、Worker poll loop、Compose 或后续单元。
- 兼容契约：旧 `workflow_checkpoints` JSON 行继续只供 legacy 读取且 `eino_checkpoint_id IS NULL`；Eino `Get` 保持官方 `([]byte, bool, error)`，项目不解码、不镜像 checkpoint blob；稳定 checkpoint ID 只从 run ID 派生并通过官方 `adk.WithCheckPointID` 使用。
- 安全不变量：`Set` 只接受 context 中与 checkpoint ID 同 Run 的当前 owner/generation，复用 P09 同一数据库行锁 fence；stale Worker 对 checkpoint 真值影响为 0；payload SHA、runtime version/hash 与 generation 和 blob 同事务提交；`Delete` 只允许已进入终态的 Run 生命周期清理。
- 预计文件：`internal/ai/workflow/eino_checkpoint_store.go`、P10 测试、`internal/dao/mysql/model.go` 的既有 Schema 映射、本证据文件和仓库外执行 Plan 台账。
- 验证方式：先加入计划列出的官方签名、空/缺失、opaque round-trip、元数据/幂等、stale generation、legacy 隔离、Delete 生命周期、官方 Runner 稳定 ID 与跨进程注册类型测试并保存预期 Red；实现后运行 P10 精确门禁、直接受影响回归、`go vet`、`staticcheck`、`goimports` 和禁止重复实现扫描。
- 回滚方式：通过本单元独立本地提交的普通反向提交恢复；不执行 Schema Down，不修改 remote，不 push。

## Build-or-Reuse

- 现有 SentinelOps 能力：复用唯一 `workflow.GORMStore`、P03 已扩展的 nullable Eino checkpoint 列/唯一索引、P07 durable eligibility、P09 唯一 `LeaseToken` 与 `withFencedRunTransaction`；legacy `SaveCheckpoint` / `LatestCheckpoint` 保持原路径。
- 锁定版 Eino/Eino-ext 能力：直接实现 Eino v0.9.15 官方 `adk.CheckPointStore` / `adk.CheckPointDeleter`，稳定 ID 使用官方 `adk.WithCheckPointID`；序列化完全由 Eino gob 与 `schema.Register[T]()` 管理，不包装 Runner、不复制 codec。
- 剩余业务缺口：现有 `GORMStore` 尚未实现 Eino 接口，P03 的 Eino 列尚未映射/读写，P09 token 尚无 context accessor，旧 Worker 仍缺少 checkpoint generation fence，且没有终态 Delete 契约或跨进程恢复证明。
- 最薄实现及删除条件：只增加 workflow 内的 context accessor、稳定 ID/官方 option helper 和三个 Eino Store 方法，并补齐既有 model 字段；若 Eino 将来提供可直接复用且能原子执行 SentinelOps MySQL generation fence 的 Store，再用同一 contract test 保护后替换。

## Red evidence

先加入计划要求的 P10 测试，再执行：

```text
$ go test ./internal/ai/workflow -run 'TestEinoCheckpoint|TestCheckpointCrossProcess' -count=1
internal/ai/workflow/p10_eino_checkpoint_store_test.go:33:30: cannot use (*GORMStore)(nil) ... missing method Get
internal/ai/workflow/p10_eino_checkpoint_store_test.go:34:32: cannot use (*GORMStore)(nil) ... missing method Delete
internal/ai/workflow/p10_eino_checkpoint_store_test.go:43:32: store.Get undefined
internal/ai/workflow/p10_eino_checkpoint_store_test.go:47:18: store.Set undefined
internal/ai/workflow/p10_eino_checkpoint_store_test.go:79:37: row.EinoCheckpointID undefined
FAIL SentinelOps/internal/ai/workflow [build failed]
```

- `PASS`（预期 Red）：命令退出 1，目标包因 `GORMStore` 尚未实现 Eino 官方接口且 checkpoint model 尚无 P03 已存在列的映射而编译失败；不是 MySQL/凭据环境错误、`[no tests to run]` 或已有实现直接变绿。

## Implementation result

- `workflow.GORMStore` 直接满足 Eino v0.9.15 `adk.CheckPointStore` / `adk.CheckPointDeleter`，没有 Adapter interface、runtime Store 或第二套 DAO。`Get` 只读取非 NULL `eino_checkpoint_id` + `checkpoint_blob`，以官方 `(payload, exists, error)` 区分不存在与已提交空 payload；legacy JSON 行不会进入 Eino 读路径。
- `ContextWithLeaseToken` / `LeaseTokenFromContext` 只传递 P09 唯一 `LeaseToken`。`Set` 校验 checkpoint ID 必须等于 `sentinelops/run/<run_id>`，再复用 `withFencedRunTransaction` 同时检查 durable contract、running、owner、generation 与未过期 lease；缺 token、跨 Run ID 和 stale generation 都在写入前明确失败。
- `Set` 在同一事务 generation-fenced upsert 原始 bytes、SHA-256、Run 的 runtime version/compatibility hash、lease generation 和 MySQL `CURRENT_TIMESTAMP(3)`；重复同 ID/同 payload 只有一行且成功。项目没有解码 Eino blob、业务 JSON 镜像或自定义 codec；`snapshot_json={}` 仅满足 P03 保留给 legacy 的 NOT NULL 兼容列。
- `EinoCheckpointID` 只从 Run ID 确定性派生，`EinoCheckpointOption` 直接返回官方 `adk.WithCheckPointID`。测试通过真实官方 Runner 写入 StatefulInterrupt checkpoint，再由新的测试进程从 MySQL 读取并 Resume，证明 `schema.Register[p10RegisteredState]()` 的项目测试类型可跨进程解码且没有随机 checkpoint ID。
- `Delete` 从稳定 ID 取得 Run，数据库行锁确认 durable Run 已终态后才物理清理；running 删除明确返回 `ErrCheckpointDeleteDenied`，终态重复删除幂等。非终态/仍需 Resume 的 checkpoint 不会按普通调用误删。
- `mysql.WorkflowCheckpoint` 只映射 P03 已存在的 nullable Eino 列，没有修改 Migration。生产 model 扩展暴露出“升级前 Schema 快照”夹具误用生产 model；改为冻结的 `p03WorkflowCheckpoint` 后，测试仍真实覆盖从旧 Schema goose Up，不提前创建 Expand 列。

## Local commands and results

MySQL Case 只连接本机 `dev-mysql` 中由测试逐个创建并清理的 `sentinelops_p03_*` 一次性数据库；凭据只在进程环境中解析和注入，未进入命令输出、证据或工作树。goose 使用已核验的 `/tmp/sentinelops-p07-bin/goose v3.27.3`。

### P10 计划局部门禁

```text
$ go test ./internal/ai/workflow -run 'TestEinoCheckpoint|TestCheckpointCrossProcess' -count=1
ok SentinelOps/internal/ai/workflow 14.979s

$ go test ./internal/ai/workflow -list 'TestEinoCheckpoint|TestCheckpointCrossProcess'
TestEinoCheckpointStoreOfficialContract
TestEinoCheckpointGetDistinguishesMissingAndEmpty
TestEinoCheckpointSetRoundTripsOpaqueMetadataAndIsIdempotent
TestEinoCheckpointStaleGenerationCannotOverwrite
TestEinoCheckpointRejectsCrossRunTokenAndLegacyRows
TestEinoCheckpointDeleteRequiresTerminalRun
TestCheckpointCrossProcessRegisteredState
TestCheckpointCrossProcessHelper
ok SentinelOps/internal/ai/workflow 0.119s
```

- `PASS`：8 个顶层 Case 被发现；helper 在父进程按设计 skip，并由 `TestCheckpointCrossProcessRegisteredState` 启动的新进程真实执行。官方签名、missing/empty、opaque bytes/SHA/runtime/generation、幂等、缺 token/跨 Run/stale、legacy 隔离、终态 Delete、稳定 ID/官方 option 和跨进程 Resume 均实际通过，无 0 tests。

### 直接受影响回归

```text
$ go test ./internal/ai/workflow ./internal/dao/mysql -count=1
ok SentinelOps/internal/ai/workflow 193.325s
FAIL SentinelOps/internal/dao/mysql 45.177s
```

- 首轮如实为 `FAIL`：`TestMigrationsUpFromCurrentSchemaSnapshot` 用已扩展的生产 `WorkflowCheckpoint` AutoMigrate 升级前快照，导致 goose 00002 再加 `eino_checkpoint_id` 时得到 duplicate column。失败不是生产 Migration 或 P10 Store 行为；将夹具替换为与同文件 Run/Event/Session 一致的冻结 `p03WorkflowCheckpoint` 后复测。

```text
$ go test ./internal/ai/workflow ./internal/dao/mysql \
    -run 'Test(EinoCheckpoint|CheckpointCrossProcess|MigrationsUpFromCurrentSchemaSnapshot)' -count=1
ok SentinelOps/internal/ai/workflow 28.303s
ok SentinelOps/internal/dao/mysql 7.065s

$ go test ./internal/dao/mysql -count=1
ok SentinelOps/internal/dao/mysql 34.290s
```

- `PASS`：P10 精确行为、升级前 Schema → goose Up contract 和 MySQL DAO 全包均通过；workflow 全包已在同一最终生产实现上通过。夹具修复未修改 DDL、Migration 或 Schema contract。

### 静态、格式与源码唯一性

```text
$ go vet ./internal/ai/workflow ./internal/dao/mysql
PASS

$ staticcheck ./internal/ai/workflow ./internal/dao/mysql
PASS

$ goimports -l <P10 Go files>
无输出

$ git diff --check
PASS

$ <P10 禁止项与唯一性 rg 检查>
P10_STATIC_AND_UNIQUENESS=PASS
```

- `PASS`：P10 生产/测试代码无 deprecated `compose.RegisterSerializableType`、`schema.RegisterName`、自定义 gob/JSON codec 或随机 ID；新测试类型只出现一次 `schema.Register[T]()`。`internal/ai/runtime` 无 GORM、checkpoint blob、Store/DAO 定义；唯一 `GORMStore` 恰有官方 `Get` / `Set` / `Delete` 三个实现，稳定 option 恰调用一次官方 `adk.WithCheckPointID`。
- 前端、镜像、完整 `go test -race ./...`、在线供应商、共享数据库、Hosted CI、发布和 P43 全量门禁均未运行，不属于 P10 局部门禁，均不据此宣称整体通过。

## Key assertions

- Eino payload 对项目代码始终是 opaque bytes；SHA、runtime identity 与 generation 是同事务旁路校验元数据，不存在业务 JSON 镜像或自定义 Eino codec。
- P09 owner/generation 仍是唯一 Worker 写身份；context 不创建第二种租约类型。缺失、跨 Run、过期或 stale token 的 Set 影响行数为 0 并返回稳定错误。
- legacy `eino_checkpoint_id IS NULL` 行保持只读隔离；空 Eino payload 通过数据库非 NULL 和 `exists=true` 与缺失明确区分。
- checkpoint ID 只由 run ID 派生并通过官方 Runner option 使用；跨进程 Resume 不依赖上一进程 handle、内存 Store 或随机 ID。
- active Run 的 checkpoint 不可 Delete；只有终态生命周期清理成功且幂等。P36 的 30 天 retention 尚未提前实现。

## Deviations from recommended route

- 推荐文件落点保持为唯一 workflow Store；为满足 P03 `snapshot_json NOT NULL` 的 legacy 兼容 Schema，Eino 行写固定空对象 `{}`，但绝不把 opaque payload 解码或镜像进去。
- 跨进程测试所需项目类型放在 P10 test 中并使用 `schema.Register[T]()`；P11 尚未创建的生产 Runtime Snapshot 类型没有被提前设计或占位注册。
- 生产 checkpoint model 映射 P03 已有列后，修正了直接受影响的旧 Schema contract fixture，使其使用冻结 `p03WorkflowCheckpoint`；这是测试夹具对既有注释契约的最小对齐，不改变 Migration。

## Final scope statement

P10 局部门禁为 `PASS`。本结论只覆盖唯一 workflow Store 上的 Eino opaque CheckPointStore/Deleter、generation-fenced metadata upsert、稳定 ID/官方 option、legacy 隔离、终态 Delete 和跨进程注册类型恢复；不代表 Runtime Snapshot、Resume/Replay/PARKED、Approval、Effect、durable Agent、整个二次开发或 P43 全量门禁完成。
