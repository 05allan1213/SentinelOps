# B0 Runtime Contract Implementation Progress

> 更新时间：2026-08-31（Asia/Shanghai）
> 仓库：`/home/monody/project/SentinelOps`
> 目标：只实施最终实施计划中的 B0-01、B0-02、B0-03；不进入 C。

## 范围与基线

- 已读取 `grill-truth.md` 与 `output/final-implementation-plan-2026-08-31.md`。
- 实施基线 HEAD：`1db892af5189b653de2c1b2a4e5527e99a16b0b3`。
- B0 产品代码 HEAD：`1059861fe05df149ffbde2cfead482821bc53c27`；进度记录另以文档提交保存。
- 分支为 `main`，本轮未 push。
- 工作树只保留用户原有未跟踪文件 `grill-truth.md`；该文件未被本轮提交触碰。

## Task 结果

### B0-01 — Runtime v1 DTO/枚举/接口契约

- Commit：`14d5a85ca7eacbe6081e5545b848479490cda87a`
  `feat(runtime-api): freeze runtime v1 DTO contracts`
- 文件：
  - `api/runtime/v1/runtime.go`
  - `api/runtime/v1/runtime_test.go`
  - `api/runtime/runtime.go`
- 影响：冻结 `/runtime/v1` 的 20 个请求/响应路由、`IRuntimeV1`、闭集枚举、分页/时间/筛选校验、资源元数据与脱敏 DTO；SSE 使用空 marker response。
- Review：独立 review 发现 nullable aggregate、状态常量复用、SSE envelope、UTC normalization 四项问题，修订后 `APPROVED`。

### B0-02 — Scope/RBAC/资源归属/内容展开权限

- Commit：`883457ac286909e5b86af3b7b6fe42be8f1f5214`
  `feat(runtime-auth): freeze scoped runtime permissions`
- 文件：
  - `internal/ai/policy/authorize.go`
  - `internal/service/runtime/contract.go`
  - `internal/service/runtime/contract_test.go`
  - `utility/middleware/authorize.go`
  - `utility/middleware/jwt_test.go`
- 影响：新增 Runtime 内容查看与 admin Recovery 权限；服务层重验 owner/Scope/auth-disabled；闭集 content kind 与稳定错误码；授权路径与 GoFrame 的 `RawPath`、`X-Url-Path` 和连续斜杠归一化保持一致，关闭路径降权绕过。
- Review：独立安全 review 针对 `X-Url-Path` 与 duplicate-slash 两次提出高危问题，修订后 `APPROVED`。
- 权限口径：`grill-truth.md` 的高优先级 Scope/资源级原则与 Task 接口/测试共同表明：非 auth-disabled 身份可展开自己拥有的 Run 内容，不能展开他人内容；admin 可全局查看/恢复；auth-disabled 仅可读自己的 metadata。Task Acceptance 中“expand raw content”的简写按“他人资源内容”解释，已保留在本记录中，未扩大为 admin-only。

### B0-03 — 状态/阶段/可用性映射与错误语义

- Commit：`1059861fe05df149ffbde2cfead482821bc53c27`
  `feat(runtime-api): freeze phase and error semantics`
- 文件：
  - `api/runtime/v1/runtime.go`（请求校验 sentinel/error bridge）
  - `api/runtime/v1/runtime_test.go`
  - `internal/service/runtime/contract.go`
  - `internal/service/runtime/contract_test.go`
  - `internal/service/runtime/event_mapper.go`
  - `internal/service/runtime/event_mapper_test.go`
  - `internal/controller/runtime/error_test.go`
- 影响：冻结 canonical status/legacy `success` 兼容规则、服务器端 Current Phase precedence、`ProjectionState` 与 `ResourceMeta` 映射、400/403/404/409/422/500 稳定错误、Event schema/version/allowlist/Redactor 与损坏事件局部降级；未新增 C 阶段 schema 或执行链路。
- Review：独立 review 针对请求 400、生产 Event allowlist、损坏关联字段、未知状态 fail-closed、agent/budget 语义和格式问题多轮修订，最终 `APPROVED`。

## 验证证据

| 检查 | 结果 | 证据/说明 |
| --- | --- | --- |
| `go test ./api/runtime/v1 ./internal/service/runtime ./internal/controller/runtime` | PASS | B0 DTO、服务 mapper、controller error tests |
| `go test ./api/...` | PASS | API 包回归 |
| `go test ./internal/ai/policy ./utility/middleware` | PASS | 既有角色矩阵与 middleware 回归 |
| `go vet ./api/runtime/... ./internal/service/runtime ./internal/controller/runtime ./utility/middleware ./internal/ai/policy` | PASS | 静态检查 |
| `/home/monody/go/bin/goimports -l`（全部本轮 Go 文件） | PASS | 无输出 |
| `git diff --check` | PASS | 三个实现提交及最终工作树 |
| workflow 纯事件/状态测试 | PASS | `TestVersionedEventCatalogIsComplete` 与 Trace phase 纯测试 |
| `go test ./internal/ai/workflow ./internal/service/runtime -run 'Event|Status|Phase'` | NOT RUN | `SENTINELOPS_TEST_DSN` 未设置；workflow 命中用例在测试前置处明确要求 phase03 throwaway MySQL；service mapper PASS |
| `go test ./...` | NOT RUN | 数据库绑定包因同一缺失 DSN 前置条件退出；非数据库包已通过，未将环境前置失败冒充产品 FAIL |

## 边界审计

- 未修改 `migrations/`、`workflow_attempts`、Operation/Worker snapshot、Recovery Worker 执行链路、前端或应用镜像配置。
- 未注册 Runtime controller/routes；B0 只冻结公共契约与服务层纯语义，后续 B1/C 仍未执行。
- 未运行 Hosted CI、Provider、真实 API/Worker、应用镜像或 P43；这些不属于本阶段证据。
- 后续单元 C、B1、H、D、E、F：`NOT RUN`。

## 阶段结论

阶段 B0 的三个 Task 均已完成实现、独立 review、窄范围本地提交和本地可运行验证；数据库依赖项保持 `NOT RUN`。协调器在此停止，不进入 C。
