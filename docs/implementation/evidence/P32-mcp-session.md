# P32 Official MCP 安全配置与 Session 生命周期

- Status: PASS（P32 局部门禁通过；外部依赖项按实况 NOT RUN）
- Started from: `16ad458 feat(rag): validate citations and derived indexing`
- Spec references: Implementation Plan P32；上位 Spec 4.4、6.8、Task 7

## Boundary Audit

- 目标：只使用锁定的 Eino officialmcp 协议、官方 SDK Transport/Session 和 `session.Session`；补齐每 Server 的配置、连接前 SSRF/stdio 安全校验、Secret 即时解析、初次连接 backoff、required/optional 生命周期、结果/分页/mapper policy、schema hash 以及 P28 MCP reservation/audit 接口。
- 明确非目标：不实现 `mcp_agent`、Tool Search、AgentTool 外层接线（P33）；不实现 Skill、通用 MCP Gateway、JSON-RPC、Transport、Schema converter、reconnect client、第二套 Budget Store/本地计数器、前端、Compose、镜像或 P43。
- 兼容契约：官方 `officialmcp@v0.1.1` 的 `GetTools`、`Config`、`ToolNameMapper`、`DescriptionPolicy`、`ResultPolicy`、`ListToolsMode`、`ToolCallResultHandlerV2` 与官方 `session.Session` 保持原样；P04 唯一 `SecretResolver`、P06 `Redactor`、P28 reservation kind/identity 和 P11 `MCPCatalogHash` 语义不迁移。
- 安全不变量：空 `allowed_tools` 拒绝所有工具；HTTPS、DNS 解析和每一跳 redirect 均做 SSRF 检查；metadata/link-local 永拒，localhost/private/port 逐 Server 显式 allowlist；stdio 只接受绝对命令、无 shell、固定 cwd 和环境 allowlist；Secret 只保存引用、建立连接前即时解析并清零；open 后 tools/list 失败立即 Close；首连失败可 backoff 重试，不能被 `sync.Once` 永久缓存；结果只由官方 `ResultPolicy.MaxChars` 裁剪，byte limit 只拒绝/计量。
- 预计修改：`go.mod`、`go.sum`、`internal/config/config.go`、`internal/ai/tools/mcp/{config.go,security.go,session_owner.go,budget_tool.go,p32_mcp_test.go}`、`internal/ai/runtime/{profile.go,mcp_budget.go,p32_mcp_budget_test.go}`、`manifest/config/config.yaml`、`manifest/config/config.docker.yaml`、本证据。
- 验证方式：先执行 Red 测试；实现后执行 P32 精确测试、受影响 `internal/config`/`internal/ai/runtime` contract、`go vet`、`go list -m`/`go mod why`/`go mod graph`、Secret/第二套 MCP 协议/预算 Store 负向扫描、暂存差异检查和一个本地提交。外部 MCP、在线供应商、共享数据库、Compose/P43 均按真实执行结果记录 `NOT RUN`。
- 回滚方式：通过本单元独立本地提交的普通反向提交恢复；不修改 remote、数据库、运行服务或上位 Spec/Plan。

## Build-or-Reuse

| 现有 SentinelOps 能力 | 锁定版 Eino/Eino-ext 官方能力 | 剩余业务缺口 | 最薄 Adapter |
| --- | --- | --- | --- |
| P04 `SecretRef`/唯一 Resolver；P06 `Redactor`；P11 `MCPCatalogHash`；P28 `BudgetCallKindMCP` 和同一 durable reserve/settle primitive；现有 YAML `secret_refs.mcp_header` | `officialmcp@v0.1.1` `Config`/`GetTools`、官方 SDK `mcp.Transport`/`ClientSession`、officialmcp `session.Session` 的连接级单次透明重连、官方 mapper/description/result/pagination/handler policy | 每 Server 的 SSRF/stdio/allowlist、Secret header 生命周期、初次失败 backoff、required/optional 状态、跨 Server namespace/schema hash 与项目审计/预算接线 | 只做 `internal/ai/tools/mcp` 的配置和校验、`SessionOwner` 生命周期与官方 Config 构造；调用官方 Session/Tool/Policy，不复制协议、transport、mapper、truncator 或 reconnect client；预算只通过回调携带 P28 kind/identity，不新建 Store。 |

## Red evidence

先添加 `p32_mcp_test.go` 后执行：

```text
$ go test ./internal/ai/tools/mcp -run 'TestMCP' -count=1
FAIL SentinelOps/internal/ai/tools/mcp [setup failed]
internal/ai/tools/mcp/p32_mcp_test.go:14:2: no required module provides package github.com/modelcontextprotocol/go-sdk/mcp
```

状态：`FAIL`（预期 Red）。失败来自 P32 尚未加入 officialmcp/SDK 真实导入和生产 API，不是 0 tests、环境跳过或已有实现直接变绿。

## Implementation result

- `go.mod`/`go.sum` 锁定并真实导入 `github.com/cloudwego/eino-ext/components/tool/mcp/officialmcp@v0.1.1` 与官方 `github.com/modelcontextprotocol/go-sdk@v1.6.1`；`go mod why` 的唯一项目读者为 `internal/ai/tools/mcp`，未引入父 MCP 模块或第三方 Adapter。
- `internal/config/config.go`、两个受版本控制的配置模板增加 `mcp.enabled`、每 Server Transport/SecretRef/allowlist/policy/lifecycle 字段，默认关闭；`config.local.yaml` 仅保留被忽略的完整替代空配置，不纳入提交。
- `internal/ai/tools/mcp/config.go` 将配置转换为不可变运行时 policy：空 `allowed_tools` 在进入 officialmcp 前直接返回空工具集；非空列表原样作为官方 `ToolNameList`；namespace 使用稳定的 `ServerName__raw_tool` mapper；描述、结果、分页、metadata 和 `ErrorAsError` 全部由官方 policy 驱动；`MCPToolSchemaHash`/`MCPToolCatalogHash` 覆盖 Server/Tool identity、公开名称、Schema、annotations、description，并将配置 policy hash 写入 P11 `MCPCatalogHash`。
- `internal/ai/tools/mcp/security.go` 在连接前及每个 redirect hop 做 HTTPS、DNS、metadata/link-local、private/localhost、CIDR 和端口校验；HTTP transport 使用固定解析结果拨号以降低 DNS rebinding；拒绝 URL userinfo。stdio 只接受绝对可执行文件、绝对 cwd、无 shell 的 `exec.Cmd`、NUL-free args 和显式环境 allowlist。
- `internal/ai/tools/mcp/session_owner.go` 只持有官方 SDK Session/Close：初次建立失败按 backoff 重试，required/optional 错误语义明确；`tools/list` 失败立即 Close；成功后的 connection-level 单次透明重连交给官方 `officialmcp/session.Session`。stdio 分支直接构造官方 SDK `CommandTransport`，因为 `officialmcp/session` 的 stdio helper 会追加整个进程环境，不满足 P32 allowlist；该分支未复制 MCP 协议、schema converter 或重连逻辑。
- `internal/ai/tools/mcp/budget_tool.go` 与 `internal/ai/runtime/mcp_budget.go` 只通过 P28 `BudgetCallKindMCP` 的既有 reserve/settle primitive 接入调用次数、并发、字符/字节上界和 timeout；字符裁剪唯一来自官方 `ResultPolicy.MaxChars`，额外 byte limit 只拒绝/计量；结果错误摘要经过 P06 `Redactor`。

## Verification ledger

- Red test：先添加 `p32_mcp_test.go` 后执行 `go test ./internal/ai/tools/mcp -run 'TestMCP' -count=1`，因尚未加入官方 SDK 真实依赖而按预期 FAIL（缺少 `github.com/modelcontextprotocol/go-sdk/mcp`），不是 0 tests 或环境跳过。
- `go test ./internal/ai/tools/mcp -count=1` -> PASS
- `go test ./internal/ai/tools/mcp -run 'Test(MCP|SSRF|Stdio|Allowlist|SessionLifecycle|ResultPolicy)' -count=1` -> PASS
- `go test -race ./internal/ai/tools/mcp -run 'Test(MCP|SSRF|Stdio|Allowlist|SessionLifecycle|ResultPolicy)' -count=1` -> PASS
- `go test ./internal/ai/runtime -run '^TestMCPBudgetHook' -count=1` 与 race 版本 -> PASS
- `go test ./internal/config -count=1` -> PASS
- `go vet ./internal/ai/tools/mcp ./internal/ai/runtime ./internal/config` -> PASS
- `go test ./... -run '^$' -count=1` -> PASS（全仓编译型 contract）
- `go mod tidy -diff`、`git diff --check` -> PASS
- `go list -m github.com/cloudwego/eino-ext/components/tool/mcp/officialmcp` -> `v0.1.1`
- `go mod why -m github.com/cloudwego/eino-ext/components/tool/mcp/officialmcp` -> 唯一项目读者 `SentinelOps/internal/ai/tools/mcp`
- `go mod graph` -> officialmcp 精确 `v0.1.1`，官方 SDK 精确 `v1.6.1`，无父 MCP/第三方 Adapter 路线
- 负向源码/依赖扫描 -> PASS：未发现 JSON-RPC、Schema converter、reconnect client、MCP 专用 Store/本地计数器或父 MCP 模块；Secret literal 扫描仅命中既有非 P32 模板占位和测试 fixture。
- `docker compose -f manifest/docker/docker-compose.yml config` -> PASS（只解析配置，未启动服务）

## Key assertions

- 空 allowlist、HTTPS/DNS/redirect/metadata/private IP/port、stdio 命令/cwd/env、Secret 即时解析和清零、初次连接 retry、tools/list 失败 Close、official mapper/description/result/pagination、schema/catalog hash、P28 reservation 和官方 SDK in-memory Server 均有测试覆盖。
- Header/Token 配置与 Snapshot 只保存 SecretRef；解析后的 Header 不进入配置、Event、Trace 或 catalog hash；stdio 环境值不参与 catalog hash；MCP feature gate 读取配置值，默认模板为 `mcp.enabled: false`。
- 项目未实现 MCP 协议、Transport、Schema converter、reconnect client 或第二套 Budget Store；P33 的 `mcp_agent`/Tool Search/AgentTool 仍未接线。

## Unfinished / NOT RUN

- P33 及后续单元：`NOT RUN`。
- 外部 MCP Server、在线供应商、共享 MySQL（`SENTINELOPS_TEST_DSN` 未提供）、Compose 服务启动、镜像、Playwright、Hosted CI、发布和 P43：`NOT RUN`。
