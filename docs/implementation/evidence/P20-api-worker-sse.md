# P20 API 单点建 Run、Worker 与纯事件 SSE

- Status: `PASS`
- Started from: `d0c71b9d1f8172939e41ca7a9fc6be418b09158f`；`main`；开工时工作树干净，P19 本地提交与证据为 `PASS`
- Spec references: 上位 Spec `3.3`、`5`、`6.1`、`7.2`、Task 3E；执行 Plan `P20`、`2.1～2.7`
- Result: API 只创建 durable Run 或读取 durable Event；独立 Worker 是唯一 Agent 执行者，并且只运行 `event_analysis_agent` 与 P13 Catalog 中冻结的 L0 Tool。SSE 断开、重连和终态游标探测都不会调用模型或 Tool。生产 `accept_new_runs` 在 P42 前默认且强制关闭。
- Actual files: `api/chat/chat.go`、`api/chat/v2/run.go`；`internal/controller/chat/{chat.go,durable.go,chat_test.go,durable_test.go,timeout_test.go}`；`internal/service/chat/{chat.go,p20_durable.go,chat_test.go,p20_durable_test.go}`；`internal/ai/runtime/{context.go,profile.go,runner.go,worker.go,p20_api_worker_sse_test.go}`；`internal/ai/workflow/{claim.go,run.go}`；`internal/bootstrap/{api.go,worker.go,config.go,bootstrap_test.go}`；`internal/config/config.go`；`manifest/config/{config.yaml,config.docker.yaml}`；本证据文件。
- Results: P20 局部门禁 `PASS`。P21～P43、完整 crash/fault 矩阵、全仓测试、真实模型、在线供应商、Hosted CI、发布与推送均 `NOT RUN`。
- Rollback: 只通过本单元独立本地提交的普通反向提交恢复；不修改历史、remote 或开发/生产 Compose project。

## Boundary Audit

- 目标：新增 `/chat/v2` Run 创建和 Event SSE；把旧 `/chat/v1` 收缩为创建兼容适配器；让独立 Worker 通过 P09 claim/heartbeat/generation fence、P11 frozen Context、P12 Resume/Replay/parked、P14 Handler 和官方 Eino Runner 执行首个 L0-only durable Agent；成功、失败、取消全部通过 P08 唯一完成 primitive 提交终态。
- 明确非目标：不实现 Approval、Effect、Mutation、Retry/Failover provider 策略、完整 Budget、Revision/context governance、Evidence、MCP/Skill、Trace flush barrier、前端或 P43 crash/fault 矩阵；不运行 L1/L2 Agent，不开放生产流量，不新增 Scheduler/Queue/Store/Runner 协议。
- 兼容契约：保留 `/chat/v1` 路径和 SSE transport，但其行为只返回新 Run 身份；复用 P08 Event envelope、持久化 seq、Session active lock 和 `CompleteRunAndCommitSession`；复用 P09 `ClaimNextRun`/heartbeat/lease generation；复用 P10～P12 Checkpoint 与 recovery contract；不改变 Event catalog 常量或 Schema。
- 安全不变量：一次请求只调用一次 `CreateRunWithSessionLock`，Controller/Service 不执行 Agent、不持有执行 goroutine；SSE 只调用 `ListEventsAfter` 并验证 Run owner/scope；Worker 只接受 `event_analysis_agent` 和 frozen L0 Catalog，`query_database` 继续 fail-closed；retry 不重置 attempt、Budget 或 runtime snapshot；parked 保持 Session 占用；终态释放只在 P08 完成事务发生；生产配置和 bootstrap 都拒绝 `accept_new_runs=true`。
- 预计与实际边界：实际修改均落在 P20 预期 API/Controller/Service/Runtime/Bootstrap/Config 以及既有 P08/P09 Store primitive 的原位扩展。`utility/sse` 已完整提供 transport，未修改；P03/P04 Compose 已有隔离依赖和 API/Worker role，未复制或修改夹具。
- 验证方式：保存预期 Red；执行计划过滤测试、受影响包全量测试、过滤 race、精确 API/Worker MySQL 集成、Compose config/健康等待/清理、`go vet`、`staticcheck`、`goimports`、diff 检查，以及执行路径/重复创建/进程内 seq/生产 Gate 负向扫描。

## Build-or-Reuse

| 现有 SentinelOps 能力 | 锁定版 Eino/Eino-ext 能力 | 剩余业务缺口 | 最薄实现及删除条件 |
| --- | --- | --- | --- |
| P08 `CreateRunWithSessionLock`、Event envelope/seq、Session lock、`CompleteRunAndCommitSession`；P09 claim/heartbeat/generation fence；P10 CheckPointStore；P11 frozen snapshot/typed Context；P12 recovery selector；P13 strict Catalog；P14 RuntimeHandler；P17 `event_analysis_agent`；P04 API/Worker bootstrap role 与 P03 隔离 Compose | Eino v0.9.15 `adk.Runner`、`Query`/`Resume`/`ResumeWithParams`、`WithCancel`、recursive safe-point cancellation、opaque Checkpoint；现有 `utility/sse.Client` | HTTP Run/Event DTO、API 单点创建、纯 Event tail、独立 durable poll loop、Agent Event 投影、retry/terminal 分类、停机时 generation-fenced safe-point handoff，以及 P42 前 accept-new-runs Gate | 一个 `DurableService`、一个 V2 Controller、既有 `Worker` 的 poll loop、一个只组合既有 primitive 与官方 Runner 的 `DurableExecutor`、一个 P20 L0 frozen profile；不创建项目版 Runner/Queue/Scheduler/Event bus。后续单元只能原位扩展这些入口，不能增加平行执行路径 |

- Gate 判定：`PASS`。官方 Eino 已拥有 Agent loop、恢复与 cancel 生命周期，项目已有全部 durable 真值；P20 只补 SentinelOps 特有的 API/Worker/SSE 和错误分类胶水。

## Red evidence

先加入 API 单点创建、SSE 纯读取、独立 Worker、retry/terminal/parked、Session lock、safe-point 与 focused integration 测试，再执行计划过滤命令。实现前按预期编译失败：

```text
undefined: NewRunAPI
undefined: RunAPIConfig
unknown field ClaimNext in WorkerConfig
worker.RunOnce undefined
```

- `PASS`（预期 Red）：目标类型和 Worker 执行入口尚不存在，命令退出 1；不是环境错误、`[no tests to run]` 或已有实现直接变绿。

## Implementation result

- `DurableService.CreateRun` 服务端生成 Run ID、identity/context/runtime snapshot、Budget、deadline 和 immutable `agent/query`，只绑定一次 `CreateRunWithSessionLock`。默认 Agent 固定为 `event_analysis_agent`，其他 Agent 在 Store 前拒绝。
- `/chat/v2/runs` 只返回 `run_id/session_id/status`；`/chat/v2/runs/{run_id}/events?after_seq=` 只 replay/tail `workflow_events`。旧 `/chat/v1` 仅发送包含新 Run 身份的兼容 `run.created`，不再调用 `ExecuteIntent`、`ExecuteDeepThink` 或任何 Agent。
- SSE 使用持久化 Event ID 作为 cursor；重连只读 Store。已确认终态 cursor 不永久等待，retryable `run.failed` 继续 tail，terminal failed/completed/parked 停止。owner/scope 拒绝映射为 403，同 Session active Run 映射为 409，关闭 Gate 映射为 503。
- `Worker.Run` 复用同一 `ClaimNextRun` poll loop；无进程内 Queue 或第二 Scheduler。每次 claim 期间 heartbeat 当前 generation；lease loss 取消执行且不写过期真值。retry 路径为 `running -> retryable_failed -> pending -> running`，`available_at` 指数退避有界，lease 被释放但 attempt、Budget 和 frozen runtime snapshot 不重置。
- `DurableExecutor` 从 immutable input 和 frozen Context 重建服务端 identity/scope，通过 P12 `StartRecovery` 进入官方 Eino Runner，并把 Agent plan/tool call/tool result 投影为 P08 canonical durable Event。只有 bootstrap resolver 中的 `event_analysis_agent` 可达。
- 成功、不可重试失败和取消只调用 `CompleteRunAndCommitSession`；parked 使用 P08 fenced transition 并保持 active Session key；P12 已完成 parked 的路径用 `RunTransitioned` 避免第二次终态更新。成功 Revision 与 terminal Event/Session 解锁在同一 P08 事务提交。
- Worker 停机或 lifecycle 取消时使用官方 recursive safe-point cancel，等待 `CancelHandle`，再通过 generation-fenced Checkpoint Store 确认 Checkpoint 后交接。`context.WithoutCancel` 只存在于 Worker lifecycle 桥接，不存在于 Controller/Service，也不让 API request 驱动执行。
- `BuildP20L0Snapshot` 从服务端生效配置冻结唯一 chat route 与 P13 L0 Catalog；明确排除 `query_database`，并冻结 `accept_new_runs`、L1/L2、MCP、Skill、Langfuse 等 Gate。local/Docker 默认均为 `enabled: false`、`accept_new_runs: false`；bootstrap 只允许 development/test 显式开启，在 production 启动前 fail-fast。
- 旧 `ExecuteIntent`/`ExecuteDeepThink` 声明暂时保留以维持包兼容，但生产树无调用点；P20 endpoint 不可达旧 Controller/Service Agent 执行链。

## Local commands and results

以下命令均在仓库根执行。DSN 与动态端口只通过进程环境传入，未写入证据；隔离数据库名必须使用 P03 throwaway 前缀，goose 固定为 v3.27.3。

### Compose 与计划过滤门禁

```text
$ docker compose -p sentinelops-p20 -f manifest/docker/docker-compose.test.yml config
PASS

$ docker compose -p sentinelops-p20 -f manifest/docker/docker-compose.test.yml \
    up -d --wait mysql redis standalone
mysql, redis, etcd, minio, standalone: healthy

$ SENTINELOPS_GOOSE_BIN=<goose-v3.27.3> SENTINELOPS_TEST_DSN=<redacted> \
    go test ./internal/controller/chat ./internal/service/chat ./internal/ai/runtime ./internal/ai/workflow \
    -run 'Test(CreateRun|SSE|Worker|L0|SessionRun|Recovery)' -count=1
ok  SentinelOps/internal/controller/chat
ok  SentinelOps/internal/service/chat
ok  SentinelOps/internal/ai/runtime
ok  SentinelOps/internal/ai/workflow
```

- `PASS`：过滤枚举发现并实际执行 32 个顶层 Case，无 `[no tests to run]`。覆盖单点 Create、Gate/L0、SSE replay/reconnect/retry/terminal/scope、API 生命周期独立、retry/parked/canceled/fatal、lease loss/poll continuation、MySQL retry reclaim、safe-point Checkpoint、Session lock 和 P12 recovery 回归。

### 受影响包、race、静态检查与精确集成

```text
$ SENTINELOPS_GOOSE_BIN=<goose-v3.27.3> SENTINELOPS_TEST_DSN=<redacted> \
    go test ./api/chat/... ./internal/controller/chat ./internal/service/chat \
    ./internal/ai/runtime ./internal/ai/workflow ./internal/bootstrap ./internal/config -count=1
PASS

$ SENTINELOPS_GOOSE_BIN=<goose-v3.27.3> SENTINELOPS_TEST_DSN=<redacted> \
    go test -race ./internal/controller/chat ./internal/service/chat ./internal/ai/runtime ./internal/ai/workflow \
    -run 'Test(CreateRun|SSE|Worker|L0|SessionRun|Recovery)' -count=1
PASS

$ go vet ./api/chat/... ./internal/controller/chat ./internal/service/chat \
    ./internal/ai/runtime ./internal/ai/workflow ./internal/bootstrap ./internal/config
PASS

$ staticcheck ./api/chat/... ./internal/controller/chat ./internal/service/chat \
    ./internal/ai/runtime ./internal/ai/workflow ./internal/bootstrap ./internal/config
PASS

$ SENTINELOPS_GOOSE_BIN=<goose-v3.27.3> SENTINELOPS_TEST_DSN=<redacted> \
    go test ./internal/ai/runtime -run '^TestAPIWorkerFocusedIntegration$' -count=1
ok  SentinelOps/internal/ai/runtime
```

- `PASS`：focused integration 在真实隔离 MySQL 上完成 Create→claim→official Runner→Agent Event projection→唯一 completion/Revision/Session release；模型 double 只调用一次，Event replay 调用计数不变，跨 owner 读取为 forbidden，终态后同 Session 可创建下一 Run。

### 格式、差异与安全负向扫描

```text
$ goimports -l <P20 Go files>
无输出

$ git diff --check
PASS

$ <Controller/Service WithoutCancel、legacy 调用点、重复 Create、进程内 Event seq、API Agent 执行路径、生产 Gate 扫描>
CONTROLLER_SERVICE_WITHOUT_CANCEL=PASS
LEGACY_EXECUTION_CALL_SITES=PASS
LEGACY_DECLARATIONS_ONLY=PASS
SINGLE_DURABLE_CREATE_BINDING=PASS
PROCESS_LOCAL_EVENT_SEQUENCE=PASS
API_DURABLE_AGENT_EXECUTION_PATH=PASS
PRODUCTION_GATE_DEFAULTS=PASS
WORKER_SAFEPOINT_WITHOUT_CANCEL_OCCURRENCES=1
WORKER_SAFEPOINT_LIFECYCLE=PASS

$ docker compose -p sentinelops-p20 -f manifest/docker/docker-compose.test.yml down -v --remove-orphans
PASS

$ docker compose -p sentinelops-p20 -f manifest/docker/docker-compose.test.yml ps -a
无 service
```

- `PASS`：Controller/Service 没有 detached context、Runner、Agent resolver 或执行 goroutine；生产 API 只有一个 Store create binding，不维护进程内 Event 序号。Worker 的一个 `WithoutCancel` 只保留 frozen values 并另传可取消 lifecycle，必须配合官方 safe-point 与 fenced Checkpoint 确认，不能由 API/SSE 使用。

## Diagnostics and failure handling

- 实现前 Red 为预期 `FAIL`，见上文。
- 一次早期组合验证在 MySQL 容器已 started 但尚未 healthy 时出现 `driver: bad connection`，状态真实记录为 fixture-readiness `FAIL`。该 project 已按 finally 清理；增加显式 health wait 后相同门禁 `PASS`。
- 在已执行 `down` 的空栈上探索性重跑过滤测试时，因未设置 `SENTINELOPS_TEST_DSN` fail-fast，未连接数据库、未进入实现断言；状态为 fixture precondition `FAIL`。随后只在 `sentinelops-p20` 重建隔离栈。
- 首次重建后的重跑误用了 PATH 中不存在的默认 goose，测试在 Migration 前以“需要 v3.27.3” fail-fast；状态为 toolchain precondition `FAIL`。显式使用既有锁定 v3.27.3 binary 后相同命令 `PASS`。
- 一次过宽源码扫描把保留的 `ExecuteIntent`/`ExecuteDeepThink` 函数声明识别成“API 执行路径”，状态为 scan-definition `FAIL`；收窄到 `api/chat`、Controller 和 P20 durable Service 的实际可达生产文件后为零命中，且独立 call-site 扫描确认两个 legacy 函数没有调用点。
- 所有失败均先保留诊断，再只清理 `sentinelops-p20`；没有启动、停止或修改开发/生产 Compose project，没有记录 Secret、完整 DSN、Authorization、Cookie、Token 或模型输入原文。

## Deviations from recommended route

- 未修改 `manifest/docker/docker-compose.test.yml`。P03 已提供隔离 MySQL/Redis/Milvus 依赖，P04 已提供 API/Worker role；P20 直接复用这些 service 和 project isolation contract，避免复制第二套夹具。集成测试由 host Go test 驱动真实隔离 MySQL，API/Worker process separation 由 role bootstrap contract 和独立 Worker test 覆盖。
- 未修改 `utility/sse`。既有 Client 已提供 Event ID/type/data transport；P20 仅在 Controller 中实现 durable cursor replay/tail 和 terminal/retry 判定。
- 计划要求删除 Controller 执行路径已完成；旧 Service 的两个导出函数声明暂时保留但无调用点，避免无关兼容破坏。它们不在 `/chat/v1`、`/chat/v2`、bootstrap 或 Worker 可达图内。
- Worker 使用一次 `context.WithoutCancel` 不是旧 Controller detached execution，而是安全停机所需的 lifecycle 分离：Agent context 保留 frozen values，独立 lifecycle 触发官方 Eino cancel，且必须确认 generation-fenced Checkpoint 后才 handoff。

## Final scope statement

P20 局部门禁为 `PASS`。本结论只覆盖 API 单点建 Run、独立 L0-only Worker、纯 durable Event SSE、错误分类/退避、唯一终态事务、安全停机与 P42 前生产 Gate；不代表 P21～P43、完整二次开发或全量验证完成。不得据此开始 P21。
