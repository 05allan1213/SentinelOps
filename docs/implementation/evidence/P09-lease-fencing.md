# P09 Claim、lease、heartbeat 与 generation fencing

- Status: `PASS`
- Started from: `c715a043efcab7e2ca9f7e66086792ceb6596336`；`main`；开工时工作树干净
- Spec references: 上位 Spec `3.3`、`6.1～6.2`、`7.2～7.5`、Task 3A；执行 Plan `P09`、`2.1～2.7`
- Actual files: `internal/ai/workflow/claim.go`、`lease.go`、`run.go`、`p09_lease_test.go`、直接受影响的 `p08_run_store_test.go`，`internal/ai/runtime/worker.go`、`worker_test.go`，`internal/dao/mysql/model.go`，本证据文件
- Unfinished items: P09 无未完成实现；P10 及后续单元、共享/生产数据库、镜像、在线供应商、Hosted CI、发布动作和 P43 全量门禁均 `NOT RUN`

## Boundary Audit

- 目标：在现有 `workflow.GORMStore` 原位增加 MySQL 原子 claim、lease heartbeat/reap、单调 generation 与统一事务 fence；多 Worker 竞争或租约接管后，只有当前 owner/generation 能改变 durable 真值。
- 明确非目标：不实现 P10 Eino CheckPointStore、P11 typed Runtime Context、P12 Resume/Replay、P14 Budget 业务、P20 poll loop/API/SSE、P21 Approval、P23 Effect、Agent 执行、第二套 Store/Queue/Runtime 或后续单元。
- 兼容契约：复用 P07 `applyDurableRuntimeContract`，不放宽 legacy/NULL contract 排除；复用 P08 Run/Event/Session 事务和唯一完成 primitive；API 决策不构造 Worker lease，后续继续使用独立 CAS 路径。
- 安全不变量：claim、generation/attempt、Run 状态、`run.claimed` Event 与 seq 同事务；heartbeat 和所有 fenced write 同时校验 run/status/owner/generation/未过期 lease；接管或 reap 后旧 token 明确返回 `ErrLeaseLost` 且受影响行数为 0；无进程内 channel/atomic/Redis 作为调度真值。
- 预计文件：`internal/ai/workflow/claim.go`、`lease.go`、P09 测试，`internal/ai/runtime/worker.go` 雏形及测试，直接受影响的 P08 test/model，本证据文件，仓库外执行 Plan 台账。
- 验证方式：先执行 Plan 的精确过滤命令保存预期 Red；实现后执行同一 race 门禁、直接受影响包未过滤测试、`go vet`、`staticcheck`、源码禁止项扫描；MySQL 仅使用由测试创建并清理的一次性数据库。
- 回滚方式：通过本单元独立本地提交的普通反向提交恢复；不执行 Schema Down，不修改 remote，不 push。

## Build-or-Reuse

- 现有 SentinelOps 能力：复用唯一 `workflow.GORMStore`、P03 已存在的 claim/reap 索引和 lease 列、P07 canonical durable eligibility predicate、P08 数据库 Event seq/事务 helper 和 Run 完成 primitive。
- 锁定版 Eino/Eino-ext 能力：Eino Runner/Checkpoint 生命周期不提供 SentinelOps MySQL durable queue、lease owner/generation、legacy contract predicate 或领域写 fence；本单元不包装或复制 Eino Runner/Store。
- 剩余业务缺口：现有代码尚无 claim、heartbeat、reap 或最小 fenced token；P08 transition/complete 只有 status CAS，旧 Worker 接管后仍可能凭 stale generation 写 Run/Event/终态。
- 最薄实现及删除条件：只在同一 workflow Store 增加 lease token、三个 lease primitive 和一个私有事务 fence；`runtime.Worker` 只保存 owner/时长并委派该 Store，不启动循环或持有调度真值。若官方能力未来能原子覆盖同一 MySQL Schema 与业务 fence，再以 contract test 保护后替换。

## Red evidence

先加入计划命名的 Claim/Lease/Heartbeat/Generation/Stale 测试，再执行：

```text
$ go test -race ./internal/ai/workflow ./internal/ai/runtime \
    -run 'Test(Claim|Lease|Heartbeat|Generation|Stale)' -count=1
internal/ai/runtime/worker_test.go:9:17: undefined: NewWorker
internal/ai/runtime/worker_test.go:9:32: undefined: WorkerConfig
internal/ai/workflow/p09_lease_test.go:34:28: store.ClaimNextRun undefined
internal/ai/workflow/p09_lease_test.go:34:63: undefined: ClaimInput
internal/ai/workflow/p09_lease_test.go:45:14: undefined: ClaimedRun
internal/ai/workflow/p09_lease_test.go:68:43: run.LeaseOwner undefined
FAIL SentinelOps/internal/ai/workflow [build failed]
FAIL SentinelOps/internal/ai/runtime [build failed]
```

- `PASS`（预期 Red）：命令退出 1，两个目标包均因 P09 所需 claim/lease/heartbeat/generation、Worker 雏形和模型映射不存在而编译失败；不是环境错误、`[no tests to run]` 或已有实现直接变绿。

## Implementation result

- `ClaimNextRun` 复用 P07 `applyDurableRuntimeContract`，以 MySQL 8 `FOR UPDATE SKIP LOCKED` 按 priority/available_at/id 原子选择 pending 或 lease 已失效的 running Run；legacy、NULL/空 runtime contract、未来 `available_at`、有效 lease 和超过 `max_attempts` 的行均不命中。
- claim 在同一事务内写 running、owner、MySQL 计算的 lease 时间、heartbeat、单调 generation、attempt、数据库 Event seq 和 version 1 `run.claimed`。Event trigger 故障时上述字段全部回滚；两个 Worker 并发只产生一个 owner/generation。
- `HeartbeatLease` 同时校验完整 runtime contract、running、owner、generation 与未过期 lease；`ReapExpiredLeases` 使用 `SKIP LOCKED` 和 1～1000 有界批次清空过期 owner/时间并先递增 generation，旧 token 随即失效。所有租约判断与续期都使用 MySQL `CURRENT_TIMESTAMP(3)`，不信任 Worker 本机时钟。
- 唯一 `LeaseToken` 只含 run/owner/generation。私有 `withFencedRunTransaction` 在同一 MySQL 行锁事务中校验完整 contract、expected status、owner、generation 和 lease；P08 `TransitionRunWithEvent` 与 `CompleteRunAndCommitSession` 已强制携带该 token，并以同一条件更新 Run/seq。原先两个无 fence 私有 helper 已删除，避免形成绕过路径。
- stale generation 的 transition/Event/终态写返回 `ErrLeaseLost`；同一事务 fence 在回调执行前拒绝 stale token，测试确认 Budget、Checkpoint、Approval、Effect 行均保持原值且回调执行次数为 0。P10/P14/P21/P23 后续 primitive 只需在同一 workflow 包复用该 fence，不在本单元提前实现其业务 DAO。
- `runtime.Worker` 只是委派唯一 `workflow.GORMStore` 的 P09 雏形，保存 owner、lease duration 与有上限的指数空轮询 backoff；没有 poll loop、channel、goroutine、GORM、第二套 Store/Queue/Scheduler 或 Agent 执行。P20 才负责接入 durable poll loop。
- `mysql.WorkflowRun` 只映射 P03 已存在的 available/priority/attempt/lease 列；未修改 Migration、Schema、依赖、配置或 Compose。P08 既有测试改用显式有效 token，原有状态机、Event/Session 原子性断言未弱化。

## Local commands and results

MySQL Case 只连接本机 `dev-mysql` 中由测试逐个创建并清理的 `sentinelops_p03_*` 一次性数据库；凭据只在进程环境中解析和注入，未进入命令输出、证据或工作树。goose 使用已核验的 `/tmp/sentinelops-p07-bin/goose v3.27.3`。

### P09 计划局部门禁

```text
$ go test -race ./internal/ai/workflow ./internal/ai/runtime \
    -run 'Test(Claim|Lease|Heartbeat|Generation|Stale)' -count=1
ok SentinelOps/internal/ai/workflow 26.472s
ok SentinelOps/internal/ai/runtime 1.241s

$ go test ./internal/ai/workflow ./internal/ai/runtime \
    -list 'Test(Claim|Lease|Heartbeat|Generation|Stale)'
workflow: 7 个顶层 P09 Case
runtime: TestLeaseBackoffIsBounded
```

- `PASS`：并发 claim、claim Event 故障回滚、完整 predicate、当前/错误 owner/generation heartbeat、lease expiry 接管、reap、stale truth writes 与 bounded backoff 均实际执行；无 0 tests。
- `PASS`：race detector 无报告；claim/heartbeat/reap/fence 使用 MySQL 时间和行级条件，stale 写返回明确 `ErrLeaseLost`。

### 直接受影响回归

```text
$ go test ./internal/ai/workflow ./internal/ai/runtime ./internal/dao/mysql -count=1
ok SentinelOps/internal/ai/workflow 62.574s
ok SentinelOps/internal/ai/runtime 0.331s
ok SentinelOps/internal/dao/mysql 9.932s
```

- `PASS`：workflow 未过滤测试覆盖 P03 Migration/P07 legacy 与 Session/P08 Run-Event-Session 事务以及 P09 lease；runtime 和 MySQL Schema/model contract 回归通过。

### 静态、格式与源码唯一性

```text
$ go vet ./internal/ai/workflow ./internal/ai/runtime ./internal/dao/mysql
PASS

$ staticcheck ./internal/ai/workflow ./internal/ai/runtime ./internal/dao/mysql
PASS

$ goimports -l <P09 Go files>
无输出

$ git diff --check
PASS

$ <禁止项与唯一性 rg 检查>
P09_STATIC_AND_UNIQUENESS=PASS
```

- `PASS`：生产 claim/lease/Worker 路径无 channel、`sync/atomic`、`context.Background`、Redis、SessionMemory、AutoMigrate；`internal/ai/runtime` 无 GORM 或 Store 接口；全仓只有一个 `LeaseToken`、一个 `ClaimNextRun` 和一个事务 fence 定义。
- 首轮 `staticcheck` 如实发现 P09 接线后 P08 的 `lockDurableRun`、`updateDurableRunAndAllocateSeq` 变成 `U1000`；删除这两个无 fence 私有 helper 后复跑为 `PASS`，未改变公开契约或扩大范围。
- 镜像、前端、完整 `go test -race ./...`、在线供应商、共享数据库、Hosted CI、发布和 P43 全量门禁均未运行，不属于 P09 局部门禁，均不据此宣称整体通过。

## Key assertions

- owner/generation 是 MySQL Run 行上的唯一租约身份；claim/reap 每次接管均使 generation 单调增加，旧 generation 的 heartbeat 和所有 durable truth write 受影响行数为 0。
- claim 的 Run 状态、owner、lease、generation、attempt、last_event_seq 与 `run.claimed` Event 是同一事务；Event insert 失败不存在半提交。
- P07 durable eligibility predicate 未复制或放宽；legacy、缺 immutable input、缺 runtime version/hash、未到 available_at 的 Run 不可 claim。
- P08 状态 CAS 仍保留，但 Worker transition/complete 还必须通过 owner/generation/lease fence；status 正确不能替代执行权。
- Worker 雏形没有进程内调度真值；MySQL lease 和 Event 表仍是唯一 durable truth。

## Deviations from recommended route

- 推荐文件落点保持为 workflow `claim.go`/`lease.go` 和 runtime `worker.go`；为冻结“最小 token 只有 run/owner/generation”，没有在 P11 前把 token 注入 context，也没有提前建立 typed RuntimeContext。
- `ReapExpiredLeases` 在清空过期 owner 时先递增 generation；后续重新 claim 再递增一次。Spec 只要求 generation 单调且 stale 立即失权，不要求每次接管恰好加一；该做法使 reap 与新 owner 之间也没有旧 generation 写入窗口。
- P09 没有创建 API decision primitive 或伪造 lease；Approval 等 API 独立 version CAS 仍由其唯一所有者 P21 接入。当前单元只收紧 Worker 写 primitive，不提前修改 Approval Schema/业务。
- `runtime.Worker` 按计划仅为雏形，不启动 P20 才拥有的轮询生命周期；bounded backoff 作为纯配置计算提供，不创建 timer/channel 调度真值。

## Final scope statement

P09 局部门禁为 `PASS`。本结论只覆盖 MySQL 原子 claim、lease heartbeat/reap、generation fence、显式 stale write 拒绝和无调度真值的 Worker 雏形；不代表 Checkpoint/Resume/Replay、Budget/Approval/Effect 业务 primitive、durable Agent、API/Worker/SSE 或整个二次开发完成。P10+ 与 P43 全量门禁均 `NOT RUN`。
