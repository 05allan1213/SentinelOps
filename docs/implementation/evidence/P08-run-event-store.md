# P08 原子 Run/Event/Session Store primitive

- Status: `PASS`
- Started from: `236618f49cd2715fade6ae256bb45d5c5fdb6fe2`；`main`；开工时工作树干净
- Spec references: 上位 Spec `6.1～6.2`、`7.2～7.4`、`7.8`；执行 Plan `P08`、`2.1～2.7`
- Unfinished items: P08 无未完成实现；P09 及后续单元、完整 Agent 执行、共享数据库、发布动作和 P43 全量门禁均 `NOT RUN`

## Boundary Audit

- 目标：在现有 `workflow.GORMStore` 原位增加 `CreateRunWithSessionLock`、`TransitionRunWithEvent`、`CompleteRunAndCommitSession`，统一完整 Run 状态矩阵、数据库事件序号、版本化 Event envelope/catalog，以及首次 durable Run 和成功完成所需的 Session Revision 原子事务。
- 明确非目标：不实现 P09 claim/lease/generation、P10 Eino CheckPointStore、P11 typed Runtime Context、P12 恢复选择、P20 API/Worker/SSE、P21～P25 Approval/Effect primitive、P29 上下文内容规则或 P35 Trace barrier；不运行 durable Agent，不修改 Migration、Compose、remote 或后续单元。
- 兼容契约：保留旧 `Store`、`CreateRun`、`AppendEvent`、`FinishRun` 给 legacy 路径，但禁止它们终结 `durable_v1`；新 Run 统一使用 `succeeded`，旧 `success` 只读兼容；SSE 读取接口继续读取同一 `workflow_events` 表。
- 安全不变量：Run CAS、`last_event_seq` 和 Event insert 同一 MySQL 事务；同 Session 只允许一个非终态 Run；Revision 0、最新 Revision Snapshot、Run、Session 占用与 `run.created` 同事务；成功 Revision N+1、终态 Event、Run 终态与解锁同事务；失败/取消不写 Revision；Event 先经 P06 统一 Redactor 且拒绝超大正文；parked 不存在通用解锁路径。
- 预计文件：`internal/ai/workflow/run.go`、`events.go`、`store.go`、`types.go` 及 P08 测试，`internal/dao/mysql/model.go`，本证据文件，仓库外执行 Plan 台账。
- 验证方式：先运行 Plan 的过滤命令保存预期 Red；实现后运行同一命令、直接受影响包既有测试、`go vet`、race、源码唯一性/禁止项扫描；MySQL 只使用本机容器中由测试创建并清理的一次性数据库。
- 回滚方式：通过本单元独立本地提交的普通反向提交恢复；不执行数据库 Down，不修改 remote，不 push。

## Build-or-Reuse

- 现有 SentinelOps 能力：原位复用唯一 `workflow.GORMStore`、P03 的 Run/Event/Revision Schema、P06 `policy.Redactor`/Canonical JSON、P07 `bootstrapRevisionZero`、`RunOccupiesSession` 与 nullable `active_session_key` 唯一索引；保留旧 Store 仅作 legacy 兼容。
- 锁定版 Eino/Eino-ext 能力：Eino Runner/CheckPointStore 管理未来 opaque checkpoint 和执行生命周期，但不提供 SentinelOps 的 MySQL Run 状态机、Session 唯一占用、业务 Event catalog 或 Session Revision 完成事务；本单元不包装或复制 Eino API。
- 剩余业务缺口：当前 Run 创建、Event 追加和完成是三条非原子 legacy 路径；序号依赖调用方内存；没有 durable 输入/Snapshot 固化、完整状态矩阵、Event envelope/catalog 或唯一完成 primitive。
- 最薄实现及删除条件：只在现有 workflow 包和 GORM model 增加 typed input、纯状态校验及三个事务方法；不增加包、Runtime Store、DAO 接口、Queue、Registry 或循环。若后续官方能力能原子覆盖同一业务 Schema 和安全契约，届时以 contract test 保护后替换薄实现。

## Red evidence

先加入计划命名的 Create/Transition/Matrix/Catalog/Sequence/Complete 测试，再执行：

```text
$ go test ./internal/ai/workflow \
    -run 'Test(CreateRun|TransitionRun|RunTransitionMatrix|VersionedEventCatalog|EventSequence|CompleteRun)' \
    -count=1
internal/ai/workflow/p08_run_store_test.go:24:20: store.CreateRunWithSessionLock undefined
internal/ai/workflow/p08_run_store_test.go:45:104: undefined: EventRunCreated
internal/ai/workflow/p08_run_store_test.go:61:21: undefined: ErrSessionRunActive
internal/ai/workflow/p08_run_store_test.go:147:15: store.TransitionRunWithEvent undefined
internal/ai/workflow/p08_run_store_test.go:427:46: undefined: CreateRunInput
FAIL SentinelOps/internal/ai/workflow [build failed]
```

- `PASS`（预期 Red）：命令退出 1，目标包因 P08 三个 durable primitive、Event catalog/envelope 和错误契约不存在而编译失败；不是环境错误、`[no tests to run]` 或已有实现直接变绿。

## Implementation result

- `run.go` 在唯一 `GORMStore` 上实现三个 P08 primitive。创建事务复用 P07 `bootstrapRevisionZero`，锁定并读取最新 MySQL Revision，将其原样冻结为 Context Snapshot，再写 `durable_v1` Run、`active_session_key`、`last_event_seq=1` 和 `run.created`；唯一索引冲突稳定返回 `ErrSessionRunActive`。
- `TransitionRunWithEvent` 先锁定 durable Run，再以期望旧状态 CAS，通过 `last_event_seq = last_event_seq + 1` 由 MySQL 分配 seq，并在同一事务插入 Event。完整矩阵覆盖九个 durable 状态；waiting/retry/parked/reconciling 的恢复边使用显式 intent，`parked` 无通用解锁捷径。
- 通用 Transition 明确拒绝 `succeeded/failed/canceled`。`CompleteRunAndCommitSession` 是唯一 durable 终态入口：成功只允许 `running -> succeeded` 并核对冻结 Revision、插入 N+1；失败/取消接受全部六个非终态来源且不写 Revision；两类完成都原子写终态 Event、Run 终态、时间/质量和 `active_session_key=NULL`。
- `events.go` 唯一定义 Spec `7.4` 的 32 个 canonical Event、`sentinelops/workflow-event/v1` envelope 和 payload version 1。P06 `Redactor` 在写库前统一处理结构化 Event 与完成输出；单文本 4096 bytes、总 envelope 16 KiB 的 fail-closed 上限迫使正文改用摘要、引用或 `trace_id`。
- 现有 `Store` 保持 legacy 兼容；`AppendEvent`/`FinishRun` 对 `durable_v1` 返回 `ErrDurablePrimitiveRequired`，从源码层阻断“先成功、后补 Session”的旧路径。`ListEventsAfter` 继续从同一 `workflow_events` 表读取。
- `mysql.WorkflowRun`/`WorkflowEvent` 只映射 P08 已存在的 Expand 列；没有修改 Migration、Schema、依赖、配置、Compose 或 `internal/ai/runtime`。

## Local commands and results

MySQL Case 只连接本机 `dev-mysql` 中由测试逐个创建/清理的 `sentinelops_p03_*` 一次性数据库；凭据只在进程环境中注入，未写入证据。goose 使用 P07 已核验的 `/tmp/sentinelops-p07-bin/goose v3.27.3`，未进入工作树。

### P08 计划局部门禁

```text
$ go test ./internal/ai/workflow \
    -run 'Test(CreateRun|TransitionRun|RunTransitionMatrix|VersionedEventCatalog|EventSequence|CompleteRun)' \
    -count=1
ok SentinelOps/internal/ai/workflow 40.972s

$ go test ./internal/ai/workflow \
    -list 'Test(CreateRun|TransitionRun|RunTransitionMatrix|VersionedEventCatalog|EventSequence|CompleteRun)'
20 个顶层 P08 Case；无 0 tests
```

- `PASS`：Create 原子成功、Event insert 故障整体回滚、顺序/并发同 Session 只有一个成功、历史 Revision 0 与最新 Revision Snapshot 固化均通过。
- `PASS`：Transition CAS/Event insert 故障回滚、数据库 seq 连续分配、九状态全部允许边和其余拒绝边、错误 intent/park_reason 负向 Case 均通过。
- `PASS`：Catalog 精确锁定 32 个 canonical name；envelope schema/version、安全 Trace ID、统一脱敏与超大 Prompt 拒绝均通过。
- `PASS`：成功完成原子写 Revision N+1/Event/终态/解锁；失败与取消从 pending、running、waiting_approval、retryable_failed、parked、reconciling 的 12 个子 Case 均只写 `run.failed` canonical Event 并解锁，不写成功 Revision。
- `PASS`：legacy `AppendEvent`/`FinishRun` 和通用 `TransitionRunWithEvent` 均不能终结 durable Run；完成 Revision insert 故障时 Revision/Event/Run/Session 全部回滚。

### 直接受影响回归、race 与静态检查

```text
$ go test ./internal/ai/workflow ./internal/dao/mysql -count=1
ok SentinelOps/internal/ai/workflow 156.252s
ok SentinelOps/internal/dao/mysql 22.650s

$ go test -race ./internal/ai/workflow \
    -run 'Test(CreateRun|TransitionRun|RunTransitionMatrix|VersionedEventCatalog|EventSequence|CompleteRun)' \
    -count=1
ok SentinelOps/internal/ai/workflow 32.760s

$ go test ./internal/ai/workflow \
    -run 'TestLegacyFinishRunCannotTerminateDurableRun' -count=1
ok SentinelOps/internal/ai/workflow 4.469s

$ go vet ./internal/ai/workflow ./internal/dao/mysql
PASS

$ staticcheck ./internal/ai/workflow ./internal/dao/mysql
PASS
```

- `PASS`：workflow/mysql 未过滤测试通过，P03 空库/旧 Schema/Schema contract 与 P07 legacy cutover/Revision 0/active Session 回归真实执行；race detector 无报告；`go vet` 与 `staticcheck` 无输出。
- 首次额外 `staticcheck` 命中 `run.go` 一处错误字符串首字母大写的 `ST1005`；只将 `Session Revision` 改为 `session revision`，未改变错误类型、事务、接口或业务语义，复跑后 `PASS`。

### 源码唯一性与禁止项扫描

```text
$ rg ... internal/ai/runtime
RUNTIME_GORM_HITS=NONE

$ rg ... internal/ai/workflow/events.go internal/ai/workflow/run.go
ATOMIC_SEQ_HITS=NONE
VOLATILE_TRUTH_HITS=NONE

$ rg -n 'func \(s \*GORMStore\) CompleteRunAndCommitSession' internal/ai/workflow
internal/ai/workflow/run.go:227:func (s *GORMStore) CompleteRunAndCommitSession(...)

$ git diff --check
PASS
```

- `PASS`：`internal/ai/runtime` 无 GORM Store；P08 生产代码无进程内 atomic 序号、Redis/cache/SessionMemory 或 `context.Background` 真值；全仓只有一个 `CompleteRunAndCommitSession` 定义。
- `PASS`：未修改 migration、依赖、配置、Compose、remote 或后续单元文件；未运行镜像、前端、在线供应商、共享数据库或 P43 全量验证，均不属于 P08 局部门禁。

## Key assertions

- `active_session_key` 的获取与释放都属于 MySQL 事务；waiting_approval、retryable_failed、parked、reconciling 不释放，只有成功/失败/取消的唯一完成 primitive 释放。
- Event seq 只由锁定 Run 行后的数据库表达式分配；状态 CAS、seq 和 Event insert 任一步失败均全部回滚。
- Revision 0/最新 Snapshot 只来自 MySQL；成功 Revision N+1 必须建立在 Run 创建时冻结的 Revision 上，发现漂移即 CAS 失败。Redis/进程缓存不参与真值。
- `success` 只服务 legacy 读取；durable 状态矩阵只接受 `succeeded`。终态不可返回运行态，generic `parked -> pending/reconciling` 被拒绝。
- Event 表不保存完整 Prompt/Completion/Tool 正文或 Secret；Event 与完成输出先经同一 P06 Redactor，超界数据必须由调用方改成摘要、引用或 Trace ID。

## Deviations from recommended route

- 推荐文件中的 `types.go` 已由 P07 完整定义状态常量，本单元没有为形式一致重复移动或重写；P08 新 typed input、状态矩阵和事务落在 `run.go`，Event catalog/envelope 落在 `events.go`。
- Spec 的 canonical catalog 没有 `run.canceled`；failed 与 canceled 完成均使用唯一既定的 `run.failed`，payload 的 `to_status` 精确区分终态，未创建第二个未冻结事件名。
- `CompleteRunAndCommitSession` 已接受 P29 将构建的 opaque Revision JSON，当前只验证 JSON 与原子性，不提前实现 P29 的上下文内容规则；Trace quality 已可原子写入，但 P35 barrier 尚未实施。

## Final scope statement

P08 局部门禁为 `PASS`。本结论只覆盖现有 `workflow.GORMStore` 上的原子 Run/Event/Session 基础 primitive、完整状态矩阵和版本化 Event catalog；不代表 Worker、Resume/Replay、Approval/Effect、durable Agent 或整个二次开发完成。P09+ 和 P43 全量门禁均 `NOT RUN`。
