# P22 StatefulInterrupt 与两阶段 Approval 发布

- Status: `PASS`
- Started from: `1cce569e5ffcdc33fb2c6ffb32c4948f99f62244`；`main`；开工时工作树干净
- Spec references: 上位 Spec `6.3`、`6.6～6.7`、`7.5～7.6`、Task 4；执行 Plan `P22`、`2.1～2.7`
- Results: P22 命名行为、真实 Eino Runner/CheckPointStore/ResumeWithParams、MySQL 两阶段事务/故障注入/expiry、直接受影响包回归、race、`go vet`、`staticcheck`、格式和唯一实现扫描均 `PASS`
- Raw artifact references: 无；命令与脱敏摘要直接记录于本文件，未记录 Secret、完整 DSN、Authorization、Cookie、Token 或模型输入原文
- Unfinished items: P23 Effect Ledger、P24～P42 及 P43 全量门禁均 `NOT RUN`；生产 Mutation 写 Gate 仍关闭

## Boundary Audit

- 目标：在唯一 `RuntimeHandler` 的四类 Tool wrapper 中复用 Eino `tool.StatefulInterrupt` / `tool.GetResumeContext`；在唯一 `workflow.GORMStore` 原位增加 generation-fenced preparing、exact Checkpoint fingerprint 发布、Resume 授权、invalidated/parked 与过期批处理；由 P20 既有 Worker poll loop 消费到期审批；Runner 只用官方 `ResumeWithParams`。
- 明确非目标：不实现 P23～P26 Effect Ledger、Mutation endpoint 真实业务执行或 durable_v1 写切流；不实现 P27 failover、P28 扩展预算、P36 retention 或 P42 动态 Gate；不新增 Approval Scheduler、Queue、Store、Tool Gateway、Runtime、Runner、Resume context、Checkpoint Codec 或跨 Agent Interrupt 协议；不修改 Migration、前端、Compose 定义、依赖、remote 或远程状态。
- 兼容契约：复用 P03 `agent_approvals` / `workflow_checkpoints` Schema、P05 server Identity/RBAC、P06 canonical Proposal/Approval identity/Redactor、P08 Run/Event 事务、P09 generation fence、P10 opaque Eino Checkpoint、P12 Resume/Replay/parked selector、P13 Catalog、P14 唯一 Handler、P19 AgentTool CompositeInterrupt、P20 Worker loop、P21 `DecideApprovalAndWakeRun`。
- 安全不变量：preparing 不可审批；旧 generation 不得创建、刷新或发布 Approval；Checkpoint Set 失败或 ID/SHA/write generation 任一不匹配时不得 pending；Resume 必须是官方显式 target，ResumeParams 只送回决定且最终授权重新读取 MySQL；rejected/expired/invalidated 不再弹窗；已决策记录不可逆；不兼容恢复 parked 且 Mutation endpoint 调用为 0；L2 不得自批；生产默认 Handler 和冻结写 Gate 继续 fail-closed。
- 预计文件：`internal/ai/runtime/handler.go`、`interrupt.go`、`runner.go`、`worker.go`；`internal/ai/workflow/approvals.go`、`approval_lifecycle.go`、`eino_checkpoint_store.go`、`run.go`；对应 P22 测试与本证据。
- 验证方式：先运行计划命名过滤测试记录预期 `FAIL`；实现后执行计划原文 race 命令、直接受影响包既有测试、`go vet`、`staticcheck`、`goimports -l`、差异与禁止重复抽象扫描；MySQL 只使用唯一 `sentinelops-p22` throwaway project，finally 执行 `down -v --remove-orphans`；不运行全仓、镜像、E2E、Eval 或 P43 门禁。
- 回滚方式：通过本单元独立本地提交的普通反向提交恢复；不解析或 Down Checkpoint/Schema，不修改 remote，不 push。

## Build-or-Reuse

| 现有 SentinelOps 能力 | 锁定版 Eino / Eino-ext 能力 | 剩余业务缺口 | 最薄实现及删除条件 |
| --- | --- | --- | --- |
| P14 唯一、无状态 `RuntimeHandler` 四类 Tool wrapper；P13 Catalog/Policy；P06 Proposal hash | Eino v0.9.15 `tool.StatefulInterrupt`、`tool.GetInterruptState`、`tool.GetResumeContext` | Handler 尚未为 L1/L2 创建 preparing 或区分初次调用/显式 Resume | 原位扩展同一 Handler；状态只保存规范提案、原 arguments、hash 与 Catalog effect steps；P23 接入 Effect 后删除 P22 的最终 Mutation fail-closed 占位，不创建 Wrapper 类 |
| P10 `workflow.GORMStore` 已实现 generation-fenced opaque `CheckPointStore.Set`；P21 已实现 Approval CAS 决策 | Eino v0.9.15 Runner 在 Interrupt 时先保存 Checkpoint，并公开 `InterruptContexts` | 缺 preparing upsert、exact fingerprint pending 发布、Resume 再授权与 orphan/invalidated 生命周期 | 在同一 GORMStore 增加薄事务 primitive；只读取 Checkpoint 元数据和 hash，禁止解析 blob；若现有 primitive 完整覆盖则删除重复代码 |
| P12 只调用官方 Runner；P19 AgentTool 依赖官方 CompositeInterrupt | Eino v0.9.15 `Runner.ResumeWithParams`、空 Targets 重新 Interrupt、AgentTool CompositeInterrupt | 缺数据库决定到显式 target 的接线与 Runner Interrupt Event 发布 | 只把持久化 interrupt ID 映射成官方 Targets；不定义 Runner/Resume 接口或地址协议 |
| P20 唯一 durable poll loop；P21 `DecideApprovalAndWakeRun` | 无需框架调度器 | 缺 pending 到期批量扫描 | 在同一 poll loop 每轮执行有界排序扫描并调用 P21 CAS primitive；竞争终态按稳定冲突跳过，不建 Scheduler/Queue/Store |

Build-or-Reuse 判定：`PASS`。官方覆盖 Interrupt/Resume/CompositeInterrupt 生命周期，现有项目覆盖 Store/lease/Checkpoint/Worker/Catalog；P22 只补 MySQL 两阶段业务语义与薄接线，没有 fork Eino 或建立同构平台。

## Red evidence

~~~bash
go test ./internal/ai/runtime ./internal/ai/workflow \
  -run 'Test(TwoPhaseApproval|StatefulInterrupt|CheckpointFingerprint|ResumeAuthorization|CompositeInterrupt)' \
  -count=1
~~~

- `FAIL`（预期 Red）：workflow 测试先引用 P22 API 后编译失败，报告 `PrepareApproval`、`LoadCheckpointFingerprint`、`PublishApprovalAndWait`、`LoadApprovalResumeTarget`、`AuthorizeApprovalResume` 等尚不存在；runtime 当时显示 `[no tests to run]`，明确不把空过滤计作证据。失败来自缺少 P22 行为，不是 MySQL、依赖或既有测试失败。

## Actual files

- 唯一 Handler HITL：`internal/ai/runtime/handler.go`、`internal/ai/runtime/interrupt.go`。
- 官方 Runner/Resume/Interrupt Event 接线：`internal/ai/runtime/runner.go`；恢复 parked 原因：`internal/ai/runtime/recovery.go`。
- frozen Policy/Gate 只读访问：`internal/ai/runtime/snapshot.go`。
- 既有 Worker poll loop expiry 接线：`internal/ai/runtime/worker.go`。
- 唯一 GORMStore Approval 生命周期：`internal/ai/workflow/approval_lifecycle.go`；终态 orphan 原子失效：`internal/ai/workflow/run.go`。
- 测试：`internal/ai/runtime/p22_hitl_test.go`、`internal/ai/workflow/p22_approval_lifecycle_test.go`。
- 证据：`docs/implementation/evidence/P22-hitl.md`。

## Local commands and results

以下命令均在仓库根执行。真实 MySQL 只使用 `sentinelops-p22` throwaway project；DSN、测试口令和动态端口只经进程环境传入，未写入证据或代码；未启动测试栈中的 API/Worker/Milvus/Redis。

### 隔离夹具与工具链前置条件

~~~bash
docker compose -p sentinelops-p22 -f manifest/docker/docker-compose.test.yml up -d --wait mysql
<pinned-goose> -version
~~~

- `PASS`：隔离 MySQL healthy；既有 `sentinelops-dev` 容器在测试前后保持原状态。
- `FAIL → PASS`：首次从本机 cache 构建 goose 时 `-version` 报 `(devel)`，在任何 Migration/行为断言前即按工具链前置条件失败处理；使用同一 `github.com/pressly/goose/v3@v3.27.3` 源码并注入官方 version ldflag 重建后，`goose -version` 精确报告 `v3.27.3`。未升级依赖或修改仓库工具链。

### P22 精确 race 门禁

~~~bash
SENTINELOPS_GOOSE_BIN=<goose-v3.27.3> SENTINELOPS_TEST_DSN=<redacted> \
  go test -race ./internal/ai/runtime ./internal/ai/workflow \
  -run 'Test(TwoPhaseApproval|StatefulInterrupt|CheckpointFingerprint|ResumeAuthorization|CompositeInterrupt)' \
  -count=1 -v
~~~

- `PASS`：20 个顶层匹配 Case 全部真实执行，无 `[no tests to run]`，两个包均 `ok` 且 race detector 无报告。
- `PASS`：真实 Eino ChatModelAgent/Runner 证明 StatefulInterrupt 后先由 generation-fenced `CheckPointStore.Set` 持久化 opaque payload，再发出 Interrupt Event，Runtime 才发布 pending；注入 `Set` 失败时仅保留不可见 preparing，pending/Checkpoint 均为 0。
- `PASS`：四类 Tool wrapper 均在原 endpoint 前中断，生产默认 Handler fail-closed；嵌套 AgentTool CompositeInterrupt 保留 root/sub；Mutation endpoint 调用为 0。
- `PASS`：preparing insert/refresh 受 generation fence；同稳定 ID 的不兼容 preparing invalidated+parked；pending 发布锁定 exact checkpoint ID/SHA/write generation，并与 `agent.interrupted`、`approval.requested`、Run waiting_approval 同事务。分别拒绝两个 Event 插入时，Approval、Run seq/status 和已插 Event 全部回滚。
- `PASS`：Checkpoint 前 orphan 返回 nil ResumeParams 走 immutable Replay；Checkpoint 后、发布前 orphan 构造空 Targets 走官方 ResumeWithParams 并重新 Interrupt，未解析 opaque blob 或猜测 address。
- `PASS`：approved Resume 要求显式 target、MySQL approved、exact fingerprint、proposal/Policy/runtime/Gate；稳定 ID payload 覆盖和关闭 Gate 均 parked，已 approved 审计状态保持不可变；客户端伪造 `approved` 不能覆盖 MySQL rejected/expired，且同 Proposal 不再弹窗。
- `PASS`：L2 自批仍被 P21 RBAC 拒绝；Worker 同一 RunOnce poll loop 有界扫描 due pending 并复用 `DecideApprovalAndWakeRun` 原子 expired+唤醒；Run 终态把 preparing→invalidated、追加 `approval.invalidated` 后再追加终态 Event，同一事务提交。

### 直接受影响包回归

~~~bash
SENTINELOPS_GOOSE_BIN=<goose-v3.27.3> SENTINELOPS_TEST_DSN=<redacted> \
  go test ./internal/ai/runtime ./internal/ai/workflow -count=1
~~~

- `PASS`：runtime 与 workflow 的全部既有/新增包测试通过，覆盖 P08 完成事务、P09 lease fence、P10 CheckPointStore、P12 recovery、P14 Handler、P20 Worker 和 P21 Approval 兼容契约。

### 静态检查、格式与复用边界

~~~bash
go vet ./internal/ai/runtime ./internal/ai/workflow
staticcheck ./internal/ai/runtime ./internal/ai/workflow
goimports -l <P22 Go files>
git diff --check

rg <Approval Store/Queue/Scheduler/Runner duplicate patterns> internal --glob '*.go'
rg -n '\.ResumeWithParams\(' internal/ai --glob '*.go' --glob '!**/*_test.go'
rg -n 'type RuntimeHandler struct|type GORMStore struct' internal/ai --glob '*.go' --glob '!**/*_test.go'
rg -n 'NewHITLRuntimeHandler|NewRuntimeHandler\(' internal/ai --glob '*.go'
~~~

- `PASS`：最终 `go vet`、`staticcheck`、`goimports -l` 与 `git diff --check` 均无输出。
- `PASS`：没有新增 Approval Store/Queue/Scheduler/Runner；真实 `ResumeWithParams` 调用仍只有官方 Runner 接线一处；项目仍各只有一个 `RuntimeHandler` 和 `workflow.GORMStore` 类型。
- `PASS`：`NewHITLRuntimeHandler` 只出现在 Handler 定义和 P22 测试；所有生产 Agent builder 继续使用无 Store 的 `NewRuntimeHandler()`，因此生产写路径保持 `POLICY_MUTATION_DISABLED`。

### 清理

~~~bash
docker compose -p sentinelops-p22 -f manifest/docker/docker-compose.test.yml \
  down -v --remove-orphans
docker compose -p sentinelops-p22 -f manifest/docker/docker-compose.test.yml ps -a
~~~

- `PASS`：只清理 `sentinelops-p22` container/network/volume；最终 `ps -a` 无 service。清理前后 `sentinelops-dev` 的 MySQL、Redis、Milvus/etcd/minio/attu 状态未变化。

## Diagnostics and failure handling

- 实现前 Red 和首次 goose version 前置条件为预期/受控 `FAIL`，见上文。
- 新增测试第一次格式化时因测试块缺一个 `}` 而编译失败，同时 `docker compose port` 对随机映射返回 `0` 导致夹具连接失败；补齐括号并改为读取容器实际 HostPort 后，相同命令 `PASS`。未改 Compose 定义或固定端口。
- 伪造 Resume 负向测试第一次误用了 P11 占位 Budget，先报“不支持 durable reservation”；改为与 `DurableExecutor` 相同的 `NewDurableBudget(store)` 后，真实 ResumeWithParams 测试 `PASS`，未修改生产授权语义。
- 首次最终 `staticcheck` 报 5 个新增错误文本首字母大写（ST1005）；只改为小写后，`staticcheck`、`go vet`、格式和差异门禁全部 `PASS`。
- Go 1.27 下 Sonic 输出回退 `encoding/json` warning；所有最终测试命令退出码为 0，本单元未修改依赖或序列化架构。
- GORM 在 insert-if-absent 和 Event 故障注入 Case 输出预期 `record not found`/MySQL 1644 日志；对应负向断言均 `PASS`，未将预期拒绝误记为失败。
- 清理命令第一次因附带删除 `/tmp` 临时工具目录而被执行策略整体拒绝，Compose 未执行；去掉该动作后精确 `down -v --remove-orphans` `PASS`。临时工具不属于仓库或运行栈。

## Key assertions

- Handler 保存的唯一依赖是不可变 `*workflow.GORMStore`；Run/User/Budget/Lease/Approval/Resume 状态全部来自每次调用的 typed Context 与 MySQL，不进入共享字段。
- Approval identity 继续使用 P06 canonical Proposal hash 和稳定 `approval_id`；State 只保存 checkpoint-safe 的规范提案、原 arguments、hash、版本与 effect step plan，并用 `schema.Register` 交给官方 gob/checkpoint 生命周期。
- Runtime 不解析 `checkpoint_blob`；只验证提交状态、稳定 ID、payload SHA-256、写入 generation、runtime version/hash。Resume Worker 可持有更高 Run generation，但批准时绑定的 Checkpoint fingerprint 必须原样未变。
- preparing 对 API 不可见；preparing/pending 不兼容可转 invalidated，approved/rejected 审计决定保持不可变但不得继续授权；所有不兼容路径同时追加 `approval.invalidated` 与 `run.parked`。
- P22 批准后仍返回 `POLICY_MUTATION_DISABLED`，绝不调用原 Mutation endpoint。只有 P23 Effect Executor 完成后，才能替换该占位并进入 Effect Ledger。

## Deviations from recommended route

- 计划示例列出 `workflow/approvals.go`、`checkpoints.go`；P21/P10 已分别拥有这些唯一职责，因此 P22 新增 `approval_lifecycle.go` 并调用既有 `approvals.go`/`eino_checkpoint_store.go`，没有复制 DAO 或 Codec。
- 为完成真实接线，除计划示例文件外最小修改了既有 `runner.go`、`worker.go`、`recovery.go`、`snapshot.go` 和 `run.go`；分别只负责 Interrupt Event 发布/Resume、同一 poll loop expiry、park reason、frozen Gate 读取与终态 orphan 事务，没有新增层或循环。
- 上位 Spec 要求 approved/rejected 历史决定不可变，因此 fingerprint/Gate 不兼容时只把 preparing/pending 状态改成 invalidated；已经 approved/rejected 的记录保留审计状态，同时 Run parked 并拒绝授权。
- `NewHITLRuntimeHandler` 仅作为 P22 test profile 的不可变依赖构造入口；生产 builder 未接线，符合本单元“生产写 Gate 继续关闭”的结果要求。
- 未使用 SKIP LOCKED：P22 只要求现有 poll loop 的有界扫描；到期竞争由 P21 version/status CAS 返回稳定冲突并跳过，未新增 claim/lease/Scheduler 语义。

## Final scope statement

P22 局部门禁为 `PASS`。本结论只覆盖 StatefulInterrupt、preparing→Checkpoint→pending 两阶段发布、exact fingerprint Resume、invalidated/expiry/orphan 与 Worker 接线；P23 Effect Ledger、真实 Mutation 执行、后续单元和 P43 全量门禁均 `NOT RUN`，不代表整个二次开发完成。
