# P34 Eino Skill Backend、校验与 skill_agent

- Status: `PASS`（P34 局部门禁通过；外部依赖项按实况 `NOT RUN`）
- Spec references: Implementation Plan P34；上位 Spec 4.4、6.8、Task 8

## Boundary Audit

- 目标：使用 Eino 官方 `skill.NewBackendFromFilesystem`、只读 local backend 和 `skill.NewMiddleware` 加载版本化只读 SOP；启动时 `List` 后逐个 `Get` 并执行项目 Policy 校验；新增只暴露 L0 Tool 的 `skill_agent`。
- 明确非目标：不实现 Skill Parser、Registry、Skill Tool、Fork 协议、Agent Loop、第二套模型配置/Retry/Failover/breaker/limiter/Budget Store、动态远程 Skill、写文件、命令执行、前端、P35 Trace 或后续单元能力。
- 兼容契约：复用 P27 `BuildReliability`/`ConfigureChatModelAgent`、P14 `RuntimeHandler`、P13 Tool Catalog、P11 Runtime Snapshot、官方 `skill.NewMiddleware` 和 `adk.NewAgentTool`；Skill Backend 不保存 Provider、Model ID 或 Secret。
- 安全不变量：只允许绝对 BaseDir 边界内的版本化 `SKILL.md`；官方 Parser 负责 frontmatter YAML 语义；项目 validator 只追加 duplicate name、路径边界、允许字段、空 description、大小上限和禁止 context/agent/model override；local backend 的 Write/Edit/Execute/ExecuteStreaming 均 fail-closed；Skill Agent 的业务 Tool 只能通过 `GetManyRequired` 获取且每项必须为 L0。
- 预计修改：`go.mod`/`go.sum`、`internal/ai/agent/skill_pipeline/{agent.go,validator.go,p34_skill_agent_test.go}`、`internal/ai/runtime/profile.go` 的 Skill Snapshot 接线、`internal/ai/policy/catalog.go`、`internal/ai/agent/plan_pipeline/executor.go` 及测试、`internal/config/config.go`、两份配置和 Compose、三份 manifest SOP、本证据。
- 验证方式：先写 P34 contract/安全负向测试；实现后运行 P34 精确测试、受影响 Runtime/Plan/Policy 测试、`go vet`、`goimports`、全仓编译型 contract、负向源码扫描和暂存差异审计；外部供应商、共享数据库、Compose、镜像、Hosted CI、Eval 与 P43 按实况记录 `NOT RUN`。
- 回滚方式：通过本单元独立本地提交的普通反向提交恢复；不修改 remote、数据库、运行服务或上位 Spec/Plan。

## Build-or-Reuse

| 现有 SentinelOps 能力 | 锁定版 Eino/Eino-ext 官方能力 | 剩余业务缺口 | 最薄实现及删除条件 |
| --- | --- | --- | --- |
| P27 Reliability、P14 RuntimeHandler、P13 L0 Catalog/Registry、P11 SkillSnapshot 字段、P19 AgentTool | Eino v0.9.15 `skill.NewBackendFromFilesystem`、`skill.NewMiddleware`、ADK `ChatModelAgent`/`AgentTool`；Eino-ext local backend `v0.2.6` | local backend 默认具备写/执行方法、项目 Skill Policy、启动期逐个 Get、Skill Agent 接线 | 只增加 read-only filesystem adapter、validator 和官方 middleware 组装；官方/现有能力足够时删除 Adapter，不复制 Parser/Registry/Tool/Fork/Runtime。 |

## Red evidence

先写入 `internal/ai/agent/skill_pipeline/p34_skill_agent_test.go` 后执行：

```text
$ go test ./internal/ai/agent/skill_pipeline -run 'Test(Skill|ReadOnlyBackend)' -count=1
FAIL SentinelOps/internal/ai/agent/skill_pipeline [build failed]
undefined: ValidateBackend / ValidationPolicy / NewReadOnlyLocalBackend / BuildSkillAgent / Config / AgentName
```

状态：`FAIL`（预期 Red）。失败来自 P34 生产包尚未存在，不是 0 tests、环境跳过或已有实现直接变绿。

## Verification ledger

- go test ./internal/ai/agent/skill_pipeline ./internal/ai/agent/plan_pipeline -run 'Test(Skill|SkillAgent|SkillSnapshot|ReadOnlyBackend)' -count=1 -> PASS。
- go test -race ./internal/ai/agent/skill_pipeline ./internal/ai/agent/plan_pipeline -run 'Test(Skill|SkillAgent|SkillSnapshot|ReadOnlyBackend)' -count=1 -> PASS。
- go vet ./internal/ai/agent/skill_pipeline ./internal/ai/agent/plan_pipeline ./internal/ai/runtime ./internal/ai/policy ./internal/bootstrap ./internal/config -> PASS。
- goimports -l 覆盖本单元全部 Go 文件 -> PASS（无输出）。
- 受影响包完整测试 -> P34 相关包 PASS；runtime 中依赖 SENTINELOPS_TEST_DSN 的既有 P12/P20/P22-P26 集成用例按环境记为 NOT RUN。
- go test ./... -run '^$' -count=1 -> PASS（全仓编译型 contract）。
- go list -m github.com/cloudwego/eino-ext/adk/backend/local -> github.com/cloudwego/eino-ext/adk/backend/local v0.2.6；go mod why 真实导入方为 SentinelOps/internal/ai/agent/skill_pipeline -> PASS。
- 官方能力/负向源码扫描 -> PASS：使用官方 Backend、Middleware、AgentTool、GetManyRequired 和统一 Reliability；未发现第二套 Agent Loop、Skill Tool、Registry、Provider 分支或命令执行协议。
- SOP executable/secret pattern scan -> PASS；三份 Skill 均为只读 Markdown。
- docker compose -f manifest/docker/docker-compose.yml config --quiet -> PASS；组合既有 test Compose 时因存量 duplicate security_opt 被拒绝，按环境/存量问题记为 NOT RUN。
- git diff --cached --check -> PASS。

## Unfinished / NOT RUN

- 外部供应商、外部 MCP Server、共享 MySQL、Hosted CI、镜像构建、Eval 与 P43：NOT RUN，不属于 P34 局部门禁。
