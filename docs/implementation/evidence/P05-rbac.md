# P05 服务端身份、RBAC 与 auth-disabled 只读

- Status: `PASS`
- Started from: `9676b049649059d5974f5454f1747b728aa90873`；`main`；开工时工作树干净
- Spec references: 上位 Spec `6.5`、Task 2；执行 Plan `P05`、`2.1～2.7`
- Results: P05 新增行为、直接受影响包回归、`go vet`、格式与安全负向扫描均 `PASS`
- Raw artifact references: 无；命令与摘要直接记录于本文件，未记录 Secret、DSN、Token 或模型输入
- Unfinished items: P06 及后续单元未实施；P43 全量门禁未执行

## Boundary Audit

- 目标：让 JWT 或 auth-disabled fallback 产生唯一的 typed server Identity；落实 viewer/operator/approver/admin 矩阵、服务端 Scope 复核和 HTTP 粗授权；Chat 与 RAG Eval 不再信任客户端 `user_id`；auth disabled 只允许 viewer 范围内读取和创建只读 Run，拒绝业务 Mutation、审批和管理写入。
- 明确非目标：不实现 P06 canonical JSON/Redactor，不实现 durable Runtime、Approval Store、Effect Ledger、Mutation Catalog 或 RuntimeHandler，不启用任何 Agent Mutation，不新增用户/Policy/Gate 管理 API，不执行 P43 全量门禁。
- 兼容契约：保留现有登录响应和旧 JWT/数据库角色输入，但将旧 `user` 规范化为 `viewer`、旧 `admin` 保持 `admin`；保留现有 API path 与请求字段以兼容旧客户端，客户端 `user_id` 仅被忽略而不再作为身份来源；外部 ingest 在认证开启时继续使用独立 API Key，不混入用户 JWT 身份，但认证关闭时同样遵守全局只读并拒绝写入。
- 安全不变量：缺失或未知身份/角色 fail-closed；认证关闭只注入固定 viewer Identity；任何客户端字段不能覆盖服务端身份；HTTP 中间件与 Service/resource 边界双重校验；L2 永不自批；预授权 L1 同时受角色、配置和静态上限约束；P05 不让 L1/L2 Agent endpoint 获得新可达性。
- 预计文件：`utility/middleware/jwt.go` 及测试；`utility/auth/jwt.go` 及测试；新增 `internal/ai/policy`；`internal/bootstrap/api.go`；Chat、workflow、RAG Eval 与直接受影响的 controller/service；本证据。
- 验证方式：先运行计划命名的 Red tests；再运行 P05 精确过滤测试、受影响包既有测试、`go vet` 和负向扫描，确认每个预期 Case 被实际执行，且不运行全仓/P43 门禁。
- 回滚方式：通过本单元独立本地提交的普通反向提交恢复；不执行数据库 Down，不修改 remote，不 push。

## Build-or-Reuse

- 现有代码能力：复用现有 GoFrame Router/Middleware、JWT 签发与解析、controller/service 分层、P03 已存在的 `workflow_runs.user_id` Schema 和 P04 单一 bootstrap/config 路径；不建设第二套 Web/Auth/Config/Store。
- 官方能力：typed Identity 使用 Go 标准库 `context.Context`；HTTP 接线复用 GoFrame `Request.SetCtx` 和 Router middleware；JWT 继续使用锁定的 `golang-jwt/jwt/v5`，不引入新鉴权框架。
- 剩余业务缺口：原 JWT 只写字符串 ctx var，auth disabled 直接匿名放行；旧 role 未规范化；路由没有统一权限矩阵；Chat/RAG Eval 可回退客户端 `user_id`；服务层没有资源 Scope 复核。
- 最薄 Adapter：在 `internal/ai/policy` 定义 Identity/Scope/Permission 与纯授权函数，由 JWT middleware 注入并由统一 HTTP authorization middleware、现有 service/store 原位调用；不创建策略服务、第二套 RBAC 存储或通用 Policy Engine。

## Actual files

- API/认证/路由：`api/chat/v1/chat.go`、`api/rageval/v1/rageval.go`、`utility/auth/jwt.go`、`utility/middleware/jwt.go`、`utility/middleware/authorize.go`、`internal/bootstrap/api.go`、`internal/service/auth/auth.go`。
- typed RBAC：`internal/ai/policy/authorize.go`、`internal/ai/policy/authorize_test.go`。
- 服务端身份与资源 Scope：`internal/controller/chat/chat.go`、`internal/controller/rageval/rageval.go`、`internal/ai/trace/tracer.go`、`internal/ai/workflow/store.go`、`internal/dao/mysql/model.go`、`internal/dao/mysql/trace_dao.go`、`internal/service/chat/chat.go`、`internal/service/chat/rollback.go`、`internal/service/rageval/service.go`、`internal/service/trace/service.go`。
- 业务写入口复核：`internal/controller/ops/soar.go`、`internal/controller/settings/settings.go`、`internal/controller/term_mapping/term_mapping.go`、`internal/service/event/event.go`、`internal/service/knowledge/knowledge.go`、`internal/service/report/report.go`、`internal/service/settings/settings.go`、`internal/service/subscription/subscription.go`、`internal/service/term_mapping/service.go`。
- 既有 Agent Mutation fail-closed 接线：`internal/ai/tools/intelligence/save_intelligence.go`、`internal/ai/tools/ops/notify_tools.go`、`internal/ai/tools/ops/trigger_soar.go`、`internal/ai/tools/report/create_report.go`。
- 测试：`utility/middleware/jwt_test.go`、`internal/ai/trace/rbac_test.go`、`internal/ai/workflow/rbac_test.go`、`internal/ai/tools/rbac_test.go`、`internal/service/rbac_test.go`、`internal/service/rageval/rbac_test.go`、`internal/service/trace/rbac_test.go`。
- 证据：`docs/implementation/evidence/P05-rbac.md`。

## Red evidence

~~~bash
go test ./utility/middleware ./internal/ai/policy ./internal/service \
  -run 'Test(JWT|Role|AuthDisabled|ClientUserID|ServiceLayer)' -count=1
~~~

先加入命名测试后、生产 `internal/ai/policy` 类型尚不存在时运行最小过滤测试，结果为预期 `FAIL`：编译器报告测试引用的 Identity/Role/Permission/Authorize 等 policy 类型或符号缺失。该失败证明测试不是在已有实现上直接变绿；随后才加入 typed Identity、RBAC 和接线实现。

## Local commands and results

### P05 行为门禁

~~~bash
goimports -w $(git diff --name-only -- '*.go') $(git ls-files --others --exclude-standard -- '*.go')

go test ./utility/middleware ./utility/auth ./internal/ai/policy ./internal/ai/trace \
  ./internal/controller/... ./internal/service/... ./internal/ai/tools/... \
  ./internal/ai/workflow \
  -run 'Test(JWT|Role|AuthDisabled|ClientUserID|ServiceLayer)' -count=1

go test ./utility/middleware ./internal/ai/policy ./internal/ai/trace \
  ./internal/service ./internal/service/rageval ./internal/service/trace \
  ./internal/ai/tools ./internal/ai/workflow \
  -run 'Test(JWT|Role|AuthDisabled|ClientUserID|ServiceLayer)' -count=1 -v
~~~

- `PASS`：精确过滤命令所有目标包通过。
- `PASS`：`-v` 证据明确执行 `TestJWTContextIsAuthoritative`、`TestClientUserIDCannotOverrideJWT`、`TestRoleMatrix`、`TestRoleMatrixHTTP`、`TestAuthDisabledInjectsViewer`、`TestAuthDisabledRejectsBusinessWrites`、多个 `TestServiceLayerRechecksScope`，以及完整角色/权限与真实 HTTP 正反向子矩阵；不存在 0 Case 冒充通过。

### 直接受影响回归与静态检查

~~~bash
go test ./utility/middleware ./utility/auth ./internal/ai/policy ./internal/ai/trace \
  ./internal/controller/... ./internal/service/... ./internal/ai/tools/... \
  ./internal/ai/workflow -count=1

go vet ./utility/middleware ./utility/auth ./internal/ai/policy ./internal/ai/trace \
  ./internal/controller/... ./internal/service/... ./internal/ai/tools/... \
  ./internal/ai/workflow

go test ./internal/dao/mysql -run '^$' -count=1
go vet ./internal/dao/mysql
~~~

- `PASS`：受影响包未过滤测试全部通过。
- `PASS`：受影响包与 `internal/dao/mysql` 的 `go vet` 通过。
- `PASS`（仅编译）：`internal/dao/mysql` 在不执行测试 Case 时成功编译；输出包含 `[no tests to run]`，未将其计作行为测试 PASS。
- `NOT RUN`：`internal/dao/mysql` 未过滤 migration 集成测试要求 `SENTINELOPS_TEST_DSN` 与一次性 P03 数据库。本单元没有启动 P03 throwaway Compose；一次探索性未过滤执行曾因缺少该 DSN 退出，未把环境缺失误记为 P05 行为失败或 PASS。
- 说明：Go 1.27 下 Sonic 输出回退 `encoding/json` 的兼容警告，命令退出码仍为 0；本单元没有改变依赖或序列化架构。

### 安全负向扫描

~~~bash
git diff --check

if rg -n --glob '*.go' --glob '!**/*_test.go' -e 'req\.UserID' .; then
  exit 1
fi

rg -n -U -e 'agent_runtime:\n\s+enabled:\s+false' \
  manifest/config/config.yaml \
  manifest/config/config.local.yaml \
  manifest/config/config.docker.yaml

if rg -n -U -e 'agent_runtime:\n\s+enabled:\s+true' \
  manifest/config/config.yaml \
  manifest/config/config.local.yaml \
  manifest/config/config.docker.yaml; then
  exit 1
fi
~~~

- `PASS`：`git diff --check` 无输出。
- `PASS`：生产 Go 源码不存在 `req.UserID` 身份读取；`rg` 返回 1 是预期空结果。
- `PASS`：三条实际配置路径的 `agent_runtime.enabled` 均为 `false`，不存在 `true`；P05 未启用 durable Agent Runtime 或后续 Mutation 架构。

## Key assertions

- JWT middleware 只从校验通过的 HS256 claims 构造 typed server Identity；未知角色和伪造 Scope fail-closed。旧 `user` 映射为 `viewer`，只有 `admin` 默认获得全局 Scope。
- auth disabled 注入固定 `auth-disabled-viewer`，允许授权范围读取与 Chat/Event read-only Run，拒绝业务 Mutation、Approval、用户/Policy/Gate/Settings/Ops playbook 管理写入；独立 ingest API-key 写入口也在 auth disabled 时拒绝。
- Chat、RAG Eval、workflow run、Trace tags 与 feedback 只消费服务端 Identity；兼容 DTO 中的客户端 `user_id` 不再参与授权或资源归属。
- workflow run 持久化 `user_id`，event/checkpoint/finish/reconnect 先校验 Identity 再校验 owner Scope；缺失、legacy 空 owner 或不存在 run 均 fail-closed。
- Trace 与 RAG Eval 查询通过服务端 `server_user_id` tag 限定 Scope；legacy 无 tag 记录对非 admin 不可见。批量删除混入越权或不存在 trace ID 时整批拒绝，不产生部分成功。
- feedback 必须属于服务端用户拥有的 session，查询、取消与更新均同时限定 session/message/user。
- HTTP middleware 提供粗授权，controller/service/store/Agent Tool 在副作用前复核；auth-disabled/viewer 的负向测试不触达 DAO 或外部 Effect。
- 完整矩阵覆盖 viewer/operator/approver/admin、预授权 L1 静态上限、他人提案决定、自批 L2 永久拒绝和 admin-only 用户/Policy/Gate 管理。

## Deviations from recommended route

- 计划示例局部命令未列出直接受影响的 Trace、Agent Tool 与 workflow 包；实际过滤与回归范围扩展到 `internal/ai/trace`、`internal/ai/tools/...`、`internal/ai/workflow`，并用单独 `-v` 命令证明命名 Case 确实执行。
- Scope 复核复用了 P03 已创建但 legacy model 尚未映射的 `workflow_runs.user_id`，因此只补 GORM model 字段，不新增或修改 Migration；P03 Schema contract 已包含该列和索引。
- Trace 表没有独立 owner 列且 P05 不拥有 Schema，故复用现有 JSON tags 持久化服务端 `server_user_id`，并在 DAO/RAG Eval 原位加过滤；未创建第二套 Trace 或 P06 canonical JSON。
- 计划负向扫描中的配置简称在仓库根不存在；实际有效路径是 `manifest/config/config.yaml`、`manifest/config/config.local.yaml`、`manifest/config/config.docker.yaml`，已全部核对。
- 为覆盖所有现存直接业务写入口，实际修改文件多于“预期文件”示例；这些改动只是在副作用前接入同一个 P05 Authorize，不改变业务实现、依赖或架构。

## Final scope statement

P05 局部门禁为 `PASS`。本结论只覆盖服务端 Identity、RBAC、Scope 与 auth-disabled 只读契约；不代表整个二次开发完成。P06 及后续实现、全仓 race/E2E/Eval/故障矩阵、Hosted CI、共享数据库、发布与生产动作均 `NOT RUN`。
