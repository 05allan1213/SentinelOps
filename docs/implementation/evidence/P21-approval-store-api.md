# P21 Approval Store、CAS 决策与 API

- Status: `PASS`
- Started from: `82061161bd18293bd4c74516871c7e47c76908e0`；`main`；开工时工作树干净
- Spec references: 上位 Spec `6.5～6.7`、`7.6`、Task 4；执行 Plan `P21`、`2.1～2.7`
- Results: P21 命名行为、真实 MySQL 事务/CAS/并发/故障注入、直接受影响包回归、race、`go vet`、`staticcheck`、格式和安全负向扫描均 `PASS`
- Raw artifact references: 无；命令与脱敏摘要直接记录于本文件，未记录 Secret、完整 DSN、Authorization、Cookie、Token 或模型输入原文
- Unfinished items: P22 StatefulInterrupt、preparing upsert、Checkpoint 绑定、pending 发布、expiry scan、Resume 及后续单元均 `NOT RUN`；P43 全量门禁 `NOT RUN`

## Boundary Audit

- 目标：在既有 `workflow.GORMStore` 原位增加 Approval pending 查询、详情和精确命名的 `DecideApprovalAndWakeRun`；使用独立 version CAS 原子提交 pending→approved/rejected/expired、Run waiting_approval→pending 与唯一 Approval Event；在既有 ops v1 controller 上增加 pending list/detail/approve/reject，并落实 approver/admin、禁止自批、proposal hash 与稳定 409。
- 明确非目标：不实现 P22 StatefulInterrupt、preparing upsert、Checkpoint 绑定、pending 发布、expiry scan 或 Resume；不实现 P23～P26 Effect Ledger、Tool/Effect 执行或 Mutation 切流；不新增 Approval Store、Scheduler、Queue、Worker loop、Runtime 或 Registry；不修改 Migration、前端、Compose、镜像、配置、依赖或远程状态。
- 兼容契约：复用 P03 `agent_approvals` 全字段与唯一约束、P05 typed server Identity/RBAC、P06 稳定 `ApprovalID`/Redactor、P08 Run/Event 事务与数据库序号、P20 ops/API bootstrap；保留现有 ops v1 path 和 controller 构造入口。
- 安全不变量：只有 pending 可决策；approved/rejected/expired/invalidated 不可逆；相同决定幂等且不追加 Event；冲突决定和过期稳定 409；preparing 对 list/detail/decision 均不可见；客户端 proposal_hash 必须精确匹配；approver/admin 只能决定他人提案，L2 永不自批，auth-disabled fail-closed；决策 API 不调用 Tool/Effect；Approval、Run 与 Event 任一写失败必须全部回滚。
- 预计文件：`internal/ai/workflow/approvals.go`、`internal/dao/mysql/model.go`、`api/ops/v1/approvals.go`、`internal/controller/ops/approvals.go`、既有 RBAC middleware/tests、本单元测试与本证据。
- 验证方式：先运行计划命名过滤测试记录预期 `FAIL`；实现后执行同一过滤测试、直接受影响包回归、race、`go vet`、`goimports -l`、差异与禁止 Tool/Effect/第二 Store 负向扫描；不运行全仓、Compose、E2E、Eval 或 P43 门禁。
- 回滚方式：通过本单元独立本地提交的普通反向提交恢复；不运行 Migration Down，不修改 remote，不 push。

## Build-or-Reuse

- 现有代码能力：P03 已创建完整 `agent_approvals` Schema；P05 已提供 server Identity、`PermissionDecideProposal` 和自批拒绝；P06 已提供稳定 Approval ID 与统一 Redactor；P08 已提供同一 `GORMStore`、Run transition matrix、Event envelope 和数据库 seq；P20 已提供 ops/API bootstrap。全部原位复用。
- 官方能力：事务、行锁和 CAS 继续使用锁定的 GORM `Transaction`、`clause.Locking` 与条件更新；HTTP DTO/路由继续使用 GoFrame typed controller，不引入新依赖或 Web 层。
- 剩余业务缺口：当前没有 Approval GORM model/DAO、pending 查询、原子决策 primitive 或真实审批 API；既有 middleware 只识别旧 singular approval 示例路径，尚未覆盖实际 plural approvals 路由。
- 最薄 Adapter：在 `workflow.GORMStore` 增加单个 approvals 文件并返回既有 DAO model；ops controller 按请求获取同一个 Store，做 DTO/HTTP 状态映射；仅补 plural 路由的粗 RBAC，不创建 service、Store 接口层、Scheduler 或 Effect 执行器。

## Red evidence

~~~bash
go test ./internal/ai/workflow ./internal/controller/ops ./internal/ai/policy \
  -run 'Test(Approval|Decision|SelfApproval|Preparing)' -count=1
~~~

- `FAIL`（预期 Red）：先加入命名测试后运行，workflow 编译器报告 `ApprovalStatusPreparing`、`DecideApprovalInput`、`mysql.AgentApproval` 和 `DecideApprovalAndWakeRun` 不存在；controller 编译器报告 Approval 稳定错误、DTO/HTTP 映射和 DAO model 不存在。policy 的既有 RBAC 加强测试通过。该失败证明 P21 Store/API 行为尚未实现，不是环境、空过滤或 0 Case 失败。

## Actual files

- Approval DAO model：`internal/dao/mysql/model.go`；复用 P03 表，不修改 Migration。
- 唯一 Store primitive 与查询：`internal/ai/workflow/approvals.go`。
- API DTO/路由：`api/ops/v1/approvals.go`。
- ops controller 接线：`internal/controller/ops/approvals.go`、`internal/controller/ops/soar.go`。
- RBAC 粗路由与安全回归：`utility/middleware/authorize.go`、`utility/middleware/jwt_test.go`、`internal/ai/policy/authorize_test.go`。
- 测试：`internal/ai/workflow/p21_approval_test.go`、`internal/controller/ops/approvals_test.go`。
- 证据：`docs/implementation/evidence/P21-approval-store-api.md`。

## Local commands and results

以下命令均在仓库根执行。真实 MySQL 只使用 `sentinelops-p21` throwaway project；DSN 与动态端口只经进程环境传入，未写入证据；goose 使用既有锁定的 v3.27.3 binary。

### 隔离夹具与计划命名门禁

~~~bash
docker compose -p sentinelops-p21 -f manifest/docker/docker-compose.test.yml config --services
docker compose -p sentinelops-p21 -f manifest/docker/docker-compose.test.yml up -d --wait mysql

SENTINELOPS_GOOSE_BIN=<goose-v3.27.3> SENTINELOPS_TEST_DSN=<redacted> \
  go test ./internal/ai/workflow ./internal/controller/ops ./internal/ai/policy \
  -run 'Test(Approval|Decision|SelfApproval|Preparing)' -count=1 -v
~~~

- `PASS`：20 个顶层匹配 Case 全部真实执行，无 `[no tests to run]`。workflow 覆盖稳定 `approval_id`、`run_id+proposal_hash` 唯一约束、pending version CAS、approved/rejected/expired、相同决定幂等、冲突决定/过期、Run 唤醒、唯一 Event、preparing 隐藏、pending list/detail、viewer/operator/approver/admin、L2 自批、auth-disabled、proposal hash、持久化 identity、终态不可逆、Reason 脱敏、事务故障回滚与并发竞争；controller 覆盖 DTO、version/hash 转发、稳定 HTTP 403/404/409 和无 Effect 执行面；policy 覆盖全部角色自批拒绝。
- `PASS`：故障注入在 Event 插入处返回 MySQL 1644 后，Approval 仍为 pending/version 1、Run 仍为 waiting_approval/原 seq，证明 Approval、Run 与 Event 没有部分提交。
- `PASS`：并发 approved/rejected 竞争恰有一个成功、一个 `APPROVAL_ALREADY_DECIDED`，只产生一个 `approval.decided`。

### 直接受影响包回归与 Schema/model contract

~~~bash
SENTINELOPS_GOOSE_BIN=<goose-v3.27.3> SENTINELOPS_TEST_DSN=<redacted> \
  go test ./internal/ai/workflow -count=1

SENTINELOPS_GOOSE_BIN=<goose-v3.27.3> SENTINELOPS_TEST_DSN=<redacted> \
  go test ./internal/dao/mysql \
  -run 'Test(ActiveSession|RuntimeSchemaContract|LegacyApplication)' -count=1

go test ./api/ops/v1 ./internal/controller/ops ./internal/ai/policy \
  ./utility/middleware -count=1
go test ./internal/bootstrap -count=1
~~~

- `PASS`：workflow 包 70 个既有/新增顶层测试全部通过；P03 Approval 全字段/索引 Schema contract、legacy model 读取和 active Session contract 通过。
- `PASS`：controller/policy/middleware 全部测试通过，plural `/ops/v1/approvals/{id}/approve|reject` 对 operator/auth-disabled 拒绝、approver 放行；bootstrap 回归通过。
- `PASS`（仅编译）：`api/ops/v1` 无测试文件但成功编译，未把它计作行为测试。

### Race、静态检查、格式与边界扫描

~~~bash
SENTINELOPS_GOOSE_BIN=<goose-v3.27.3> SENTINELOPS_TEST_DSN=<redacted> \
  go test -race ./internal/ai/workflow ./internal/controller/ops ./internal/ai/policy \
  -run 'Test(Approval|Decision|SelfApproval|Preparing)' -count=1

go vet ./api/ops/v1 ./internal/controller/ops ./internal/ai/policy \
  ./internal/ai/workflow ./internal/dao/mysql ./utility/middleware
staticcheck ./api/ops/v1 ./internal/controller/ops ./internal/ai/policy \
  ./internal/ai/workflow ./internal/dao/mysql ./utility/middleware
goimports -l <P21 Go files>
git diff --check

rg -n -e 'StatefulInterrupt' -e 'PublishApprovalAndWait' -e 'ResumeWithParams' \
  -e 'internal/ai/effects' -e 'internal/ai/tools' -e '\.Execute\(' \
  internal/ai/workflow/approvals.go internal/controller/ops/approvals.go api/ops/v1/approvals.go
rg -n -e 'type .*Approval.*Store' -e 'NewApprovalStore' \
  -e 'ApprovalScheduler' -e 'ApprovalQueue' internal api
~~~

- `PASS`：P21 命名测试 race 通过；`go vet`、最终 `staticcheck`、`goimports -l` 和 `git diff --check` 均无输出。
- `PASS`：禁止后续能力与重复抽象扫描均为预期空结果（`rg` 退出 1）；P21 API/Store 不引用 StatefulInterrupt、Checkpoint 发布、Resume、Tool/Effect 执行，也没有第二个 Approval Store、Scheduler 或 Queue。

### 清理

~~~bash
docker compose -p sentinelops-p21 -f manifest/docker/docker-compose.test.yml \
  down -v --remove-orphans
docker compose -p sentinelops-p21 -f manifest/docker/docker-compose.test.yml ps -a
~~~

- `PASS`：只清理 `sentinelops-p21`；最终 `ps -a` 无 service。未启动、停止或修改开发/生产 Compose project。

## Diagnostics and failure handling

- 实现前 Red 为预期 `FAIL`，见上文。
- 首次 `staticcheck` 报告 `internal/ai/workflow/approvals.go` 的参数错误文本以大写开头（ST1005），状态为 `FAIL`；只将该文本改为小写后，相同 `staticcheck`、`go vet`、格式和差异门禁均 `PASS`，未改变审批语义。
- Go 1.27 下 Sonic 输出回退 `encoding/json` 的兼容 warning，所有相关命令退出码为 0；本单元未修改依赖或序列化架构。
- GORM 在唯一约束、preparing 不可见和 Event 故障注入负向 Case 中输出预期数据库错误日志；对应测试断言均 `PASS`，未将预期拒绝误记为失败。

## Key assertions

- Approval 使用 P06 `fo/approval/v1` 稳定 identity，并在读取/决策时重新校验；P03 的 `run_id + proposal_hash` 唯一约束继续作为数据库真值，不以 tool_call_id、attempt 或 lease 生成身份。
- list/detail 只返回 approver/admin 可决定的他人 pending Approval；preparing 对外统一不可见。API 始终回传 `proposal_hash`，approve/reject 必须携带精确 hash 与 expected version。
- `DecideApprovalAndWakeRun` 位于唯一 `workflow.GORMStore`，不接受 Worker lease；行锁和 `status=pending AND version=?` CAS 后，在同一 GORM Transaction 内提交 Approval 终态、Run waiting_approval→pending、数据库 Event seq 与唯一 `approval.decided`/`approval.expired`。
- approved/rejected/expired/invalidated 不可逆；相同终态决定只读取原结果，不更新 version、不追加 Event；冲突决定、过期、version/hash/identity 冲突均稳定映射为 HTTP 409。
- viewer/operator/auth-disabled 和所有 L2 自批在 Store 写前 fail-closed；controller 的粗路由同时拒绝实际 plural 决策路径。Decision reason 经 P06 统一 Redactor 后才持久化。
- P21 没有创建或发布 pending Approval，没有 expiry scan/Resume，也没有调用 Tool/Effect。Approval 发布生命周期仍不可用，必须由 P22 在同一 primitive/Store 上接线。

## Deviations from recommended route

- 未新增 Migration：P03 已完整创建 `agent_approvals` 字段和唯一/查询索引；P21 只补与既有表精确对应的 GORM model，并复跑 Schema contract。
- 未新增独立 Service 或 Approval Store：ops controller 按请求复用 `mysql.DB` 与 `workflow.NewGORMStore`；测试只用最小 repository seam 注入，不形成生产平行实现。
- 为落实实际 API 路径的 P05 粗 RBAC，除计划示例包外补测 `utility/middleware`；这只让现有 singular approval 匹配同时覆盖 plural approvals，不改变角色矩阵。
- P21 计划未要求 Compose，但 workflow 事务测试按既有 P03 fixture 必须连接真实 MySQL；因此只启动隔离的 `sentinelops-p21` MySQL，门禁后完整清理，未使用 dev/prod 栈。

## Final scope statement

P21 局部门禁为 `PASS`。本结论只覆盖 Approval 持久化读取、CAS 决策、原子 Run 唤醒/Event 与 pending 审批 API；不代表 P22 HITL/发布/Resume、Effect、整个二次开发或 P43 全量门禁完成。
