# P00 冻结事实基线与证据协议

- Status: `PASS`
- Started from: `main` / `a8b92d8838451f46fab1e56e126095c3937264d4`，前一提交 `2436ef1ebc3201797c56f56347842d3876c8dc43`，共 3 个提交，起始工作树干净
- Spec references: 上位 Spec `Task 0`、`Task 11`；执行 Plan `P00`
- Actual files: `.gitignore`、`docs/implementation/evidence/README.md`、`docs/implementation/evidence/P00-baseline.md`
- Red test and expected failure: P00 是文档/事实冻结单元，不新增行为测试；按计划在 Go 1.27.0 下用 `internal/ai/indexer` 复现 Sonic v1.14.1 编译失败，预期错误为 `undefined: GoMapIterator`
- Local commands: 见“执行记录”
- Results: P00 局部门禁 `PASS`；Sonic 基线编译 `FAIL`（预期且已明确标注）；Schema 副本、五类运行时 smoke、四类在线 preflight 为 `NOT RUN`
- Key assertions: 起点与漂移可复核；已知失败和未运行项分列；`config.local.yaml` 被忽略且未跟踪；证据模板不含 Secret；业务代码零变化
- Deviations from recommended route: 计划编制基线后新增了仅包含 `AGENTS.md` 的本地提交，实际起点由 `2436ef1` 漂移为 `a8b92d8`；Eino v0.9.15 没有名为 `RuntimeHandler` 的公开符号，实际官方契约是 `ChatModelAgentMiddleware`
- Raw artifact references: `.artifacts/implementation/P00/`
- Unfinished items: P00 无未完成项；依赖升级、运行时实现、完整 Eval 和外部环境验证仍属于 P01 及后续单元

## Boundary Audit

- 目标：冻结当前 Git、工具链、依赖、镜像、测试、关键入口和 Eino v0.9.15 官方契约，建立后续统一证据格式。
- 明确非目标：不修改业务行为，不升级依赖，不生成 Migration，不访问共享/真实数据库，不调用在线模型，不运行全仓测试或完整 Eval，不执行 P01。
- 兼容契约：保留当前 Provider → Model Catalog → Routing、完整本地配置替代、Driver 对应 Endpoint 校验、显式 Retrieval WarmUp、cached/reasoning Usage/Cost 行为。
- 安全不变量：不读取或输出 Key、完整 DSN、Token、模型输入或真实业务数据；不修改 remote、不 push、不重建 Git 历史。
- 预计文件：仅本页、证据协议 README 和 `.gitignore` 的 P00 原始证据忽略项。
- 验证方式：执行 P00 列出的只读命令、Go 1.27 Sonic 最小编译复现、Go 1.24.4 关键包局部测试、cached diff 审核。
- 回滚方式：本单元只新增文档与忽略规则，可通过独立本地提交的普通反向提交恢复；不涉及数据或运行时状态。

## Build-or-Reuse

- 现有代码能力：已有配置 contract、供应商协议 preflight、Usage/Cost 测试和业务入口源码。
- Eino / Eino-ext 官方能力：直接引用下载到 module cache 的 Eino v0.9.15 公共 API 源码，不复制框架实现。
- 剩余业务缺口：P00 只记录事实，不产生新的运行时能力。
- 最薄 Adapter：不适用；本单元没有新增包、接口、Registry、Runtime 或跨层抽象。

## 执行环境与 Git 起点

采集时间为 `2026-08-24T15:38:35+08:00`，工作目录为 `/home/monody/project/SentinelOps`。

| 项目 | 结果 | 状态 |
| --- | --- | --- |
| Branch | `main` | `PASS` |
| HEAD | `a8b92d8838451f46fab1e56e126095c3937264d4` | `PASS` |
| Parent | `2436ef1ebc3201797c56f56347842d3876c8dc43` | `PASS` |
| Commit count | 3 | `PASS` |
| Worktree at start | clean | `PASS` |
| Remote | `origin` fetch/push 均为 `https://github.com/05allan1213/SentinelOps.git` | `PASS` |
| Go | `go1.27.0 linux/amd64`；项目声明 `go 1.24.0` / `toolchain go1.24.4` | `PASS` |
| Node | `v24.19.0` | `PASS` |
| npm | `12.0.2` | `PASS` |
| Docker Engine | Client/Server `29.7.1`，API `1.55` | `PASS` |
| Docker Compose | `v5.4.0` | `PASS` |

与计划编制值相比，`a8b92d8 docs: add AGENTS.md` 是第三个提交。P00 只记录该漂移，没有回退历史、修改 remote 或 push。

## 依赖、Lockfile 与镜像

`go list -m -f '{{if not .Indirect}}{{.Path}} {{.Version}}{{end}}' all` 退出 0，当前 direct module 如下：

```text
github.com/bytedance/sonic v1.14.1
github.com/cloudwego/eino v0.7.13
github.com/cloudwego/eino-ext/components/document/loader/file v0.0.0-20251022075257-f53d64495d2f
github.com/cloudwego/eino-ext/components/indexer/milvus v0.0.0-20251011073417-75b93b87b8a9
github.com/cloudwego/eino-ext/components/model/openai v0.1.13
github.com/cloudwego/eino-ext/components/tool/duckduckgo/v2 v2.0.0-20250826125654-37d4a5029810
github.com/cloudwego/eino-ext/libs/acl/openai v0.1.17
github.com/dslipak/pdf v0.0.2
github.com/fumiama/go-docx v0.0.0-20250506085032-0c30fd09304b
github.com/gogf/gf/v2 v2.7.1
github.com/golang-jwt/jwt/v5 v5.3.1
github.com/google/uuid v1.6.0
github.com/milvus-io/milvus-sdk-go/v2 v2.4.2
github.com/mmcdole/gofeed v1.3.0
github.com/pdfcpu/pdfcpu v0.11.1
github.com/redis/go-redis/v9 v9.18.0
golang.org/x/crypto v0.43.0
golang.org/x/time v0.0.0-20201208040808-7e3f01d25324
gopkg.in/yaml.v3 v3.0.1
gorm.io/driver/mysql v1.6.0
gorm.io/gorm v1.31.0
```

`go list -m all` 退出 0，完整输出保存在 `.artifacts/implementation/P00/go-list-m-all.txt`。关键 MVS 事实为 Eino `v0.7.13`、Sonic `v1.14.1`、GoFrame `v2.7.1`、GORM `v1.31.0`、go-redis `v9.18.0`。

前端同时跟踪 `package-lock.json` 与 `pnpm-lock.yaml`。`npm --prefix web outdated` 列出 22 个可升级项并退出 1；这是“存在可升级项”的预期 npm 语义，不是命令执行故障。完整输出保存在 `.artifacts/implementation/P00/npm-outdated.txt`；关键当前/最新版本包括 React `18.3.1` / `19.2.8`、Router `6.30.3` / `7.18.2`、Vite `6.4.1` / `8.2.2`、TypeScript `5.6.3` / `7.0.2`、Tailwind `3.4.19` / `4.3.3`。

Dockerfile 与 Compose 当前镜像：

```text
Dockerfile.backend: golang:1.24-alpine（builder 与 runtime）
Dockerfile.frontend: nginx:1.27-alpine
Compose: mysql:8.0
Compose: redis:7.4-alpine
Compose: milvusdb/milvus:v2.5.10
Compose: zilliz/attu:v2.6
Compose: quay.io/coreos/etcd:v3.5.18
Compose: minio/minio:RELEASE.2023-03-20T20-16-18Z
Compose build images: sentinelops-backend、sentinelops-frontend
```

两份 Compose 的 `config --images` 均退出 0；未执行 `up`，没有触碰任何现有栈。

## 现有测试清单与局部结果

`rg --files -g '*_test.go' -g '!manifest/docker/volumes/**'` 共找到 19 个文件：

```text
api/trace/v1/trace_test.go
main_test.go
utility/stringutil/truncate_test.go
internal/ai/ops/engine/engine_test.go
internal/ai/ops/actions/nginx_helper_test.go
internal/ai/rerank/rerank_test.go
internal/ai/rerank/provider_online_test.go
internal/ai/models/open_ai_test.go
internal/ai/models/provider_online_test.go
internal/ai/trace/usage_cost_test.go
internal/config/config_test.go
internal/service/chat/chat_test.go
internal/ai/embedder/dense_test.go
internal/ai/embedder/provider_online_test.go
internal/service/pipeline/pipeline_test.go
internal/ai/agent/ops_pipeline/run_test.go
internal/ai/agent/event_analysis_pipeline/orchestration_test.go
internal/controller/chat/timeout_test.go
internal/controller/chat/chat_test.go
```

Go 1.27.0 的最小 Sonic 复现：

```text
$ go test ./internal/ai/indexer -run '^$' -count=1
# github.com/bytedance/sonic/internal/rt
.../sonic@v1.14.1/internal/rt/stubs.go:33:22: undefined: GoMapIterator
.../sonic@v1.14.1/internal/rt/stubs.go:36:54: undefined: GoMapIterator
FAIL SentinelOps/internal/ai/indexer [build failed]
```

命令退出 1，结果为 `FAIL`，且与 Spec 中的已知基线一致。完整输出保存在 `.artifacts/implementation/P00/sonic-go1.27.txt`。因此 P01 可以继续以“必须升级 Sonic”作为已复现事实，但 P00 不实施升级。

Go 1.24.4 的关键入口局部测试：

```bash
GOTOOLCHAIN=go1.24.4 go test ./internal/config ./internal/ai/models ./internal/ai/embedder ./internal/ai/rerank ./internal/ai/trace ./internal/controller/chat ./internal/ai/agent/ops_pipeline -count=1
```

7 个目标包均实际执行测试并退出 0，结果为 `PASS`。该命令覆盖完整本地配置替代、Provider/Catalog/Routing 与 Driver Endpoint 校验、Thinking 参数、Embedding 2048 维、Rerank 协议、cached/reasoning Usage/Cost、DeepThinking timeout 和 Ops 状态转换；它不是全仓验证。

## 当前关键契约位置

| 契约 | 源码 | 测试 | 状态 |
| --- | --- | --- | --- |
| `config.local.yaml` 完整替代且不合并 | `internal/config/config.go:84` | `internal/config/config_test.go:56` | `PASS` |
| Provider → Model Catalog → Routing | `internal/config/config.go:136`、`internal/config/config.go:241` | `internal/config/config_test.go:96`、`internal/config/config_test.go:155` | `PASS` |
| 按实际 Driver 校验 Endpoint | `internal/config/config.go:161` | `internal/config/config_test.go:123`、`internal/config/config_test.go:188` | `PASS` |
| 显式 Retrieval WarmUp | `main.go:75`、`internal/ai/retrieval/factory.go:31`、`internal/ai/retrieval/factory.go:69` | 本单元只做源码定位 | `PASS`（位置） |
| cached/reasoning Usage 与成本不重复累计 | `internal/ai/trace/usage.go:9`、`internal/ai/trace/store.go:325` | `internal/ai/trace/usage_cost_test.go:10`、`:35`、`:47` | `PASS` |

`git check-ignore -v manifest/config/config.local.yaml` 命中 `.gitignore`，`git ls-files manifest/config/config.local.yaml` 无输出：该路径被忽略且未跟踪。本单元未读取该文件内容。

## Schema 基线

当前代码在 `internal/dao/mysql/database.go:53` 使用 GORM `AutoMigrate`，覆盖 21 个模型，并在 `:63` 额外创建 `idx_protected_asset_type_value`。模型声明位于 `internal/dao/mysql/model.go`。

脱敏 schema-only 副本或等价 contract fixture：`NOT RUN`。本次没有得到明确授权的一次性测试库或 Schema 副本，因此没有连接数据库、读取 DSN、导出 Schema 或复制任何业务数据。这一 `NOT RUN` 符合 P00 的安全边界，不能当作 Schema contract 已通过。

## Eino v0.9.15 官方源码契约

使用 `go mod download -json github.com/cloudwego/eino@v0.9.15` 只下载并检查目标版本源码，未修改 `go.mod` / `go.sum`。模块校验和为 `h1:F+7uXeZYbJm30a/kaC93Mj6H9K2zx4thQaQP695NggE=`。以下路径均相对于 `$(go env GOMODCACHE)/github.com/cloudwego/eino@v0.9.15/`：

| 契约 | 源码位置 | 结论 |
| --- | --- | --- |
| `NewAgentTool` | `adk/agent_tool.go:93` | 入参是 `adk.Agent`，不是 `compose.Runnable` |
| `ExecutorConfig` | `adk/prebuilt/planexecute/plan_execute.go:485` | 字段为 `Model`、`ToolsConfig`、`MaxIterations`、`GenInputFn` |
| Model Handler / Retry / Failover 顺序 | `adk/chatmodel.go:323` | failover wrapper 在 retry wrapper 外层；Handlers 第一项最外层 |
| Tool wrapper 顺序 | `adk/chatmodel.go:356`、`:533` | `ToolsConfig.ToolCallMiddlewares` 位于 event sender 内、Agent/Handler wrappers 外 |
| Handler 五个 wrapper 入口 | `adk/handler.go:183`、`:195`、`:207`、`:219`、`:231` | 四类 Tool wrapper 加 `WrapModel` 均属于 `ChatModelAgentMiddleware` |
| `GenModelInput` | `adk/chatmodel.go:165` | 默认实现只在 instruction 非空且 SessionValues 非空时用 `schema.FString` 格式化 |
| `schema.Register[T]()` | `schema/serialization.go:114` | 用于会持久化到 graph/ADK checkpoint 的顶层或 interface concrete type，建议在声明文件的 `init()` 调用 |
| Tool Search 两种模式 | `adk/middlewares/dynamictool/toolsearch/toolsearch.go:36`、`:101` | 默认 middleware 过滤工具；`UseModelToolSearch=true` 委托模型原生 Tool Search |

对 Eino v0.9.15 全部 `*.go` 执行 `rg -n 'RuntimeHandler'` 得到 0 个匹配。`RuntimeHandler` 是后续 P13 的项目命名，不是 Eino 的公开类型；其应实现的实际官方边界是 `ChatModelAgentMiddleware`。这是一项命名事实偏差，不改变已冻结的五入口语义，也未命中停止条件。

## Smoke 与在线预检

| 项目 | 结果 | 原因或边界 |
| --- | --- | --- |
| 标准模式运行时 smoke | `NOT RUN` | 无本单元专属隔离运行栈与模型凭证；仅记录 `internal/controller/chat/chat.go:178` 入口并运行离线 contract |
| DeepThinking 运行时 smoke | `NOT RUN` | 同上；离线 timeout 测试 `PASS`，不冒充真实模型 smoke |
| RAG 运行时 smoke | `NOT RUN` | 未启动或触碰 Milvus/Redis；仅记录显式 WarmUp 与 RAG 图入口 |
| Ops 查询运行时 smoke | `NOT RUN` | 未连接数据库或调用模型；仅运行 `ops_pipeline` 局部状态测试 |
| SSE 重连运行时 smoke | `NOT RUN` | 无隔离 HTTP/浏览器环境；仅记录 `utility/sse/sse.go`、`internal/controller/chat/chat.go:37` 和 `web/src/utils/sse.ts` 的现有实现 |
| 文本生成 preflight | `NOT RUN` | `SENTINELOPS_ONLINE_TEST` 未显式设为 `1` |
| Tool Calling preflight | `NOT RUN` | `SENTINELOPS_ONLINE_TEST` 未显式设为 `1` |
| 2048 维 Embedding preflight | `NOT RUN` | `SENTINELOPS_ONLINE_TEST` 未显式设为 `1` |
| Rerank preflight | `NOT RUN` | `SENTINELOPS_ONLINE_TEST` 未显式设为 `1` |

四类供应商测试分别位于 `internal/ai/models/provider_online_test.go`、`internal/ai/embedder/provider_online_test.go` 和 `internal/ai/rerank/provider_online_test.go`。本次未读取、探测或输出任何 Key；即使将来 preflight 通过，也不能替代 40+ Case 三轮完整 Eval。

## 执行记录

以下命令均在 `/home/monody/project/SentinelOps` 执行：

```bash
git status --short --branch
git rev-parse HEAD
git rev-parse HEAD^
git rev-list --count HEAD
git remote -v
go version
node --version
npm --version
docker version
docker compose version
go list -m -f '{{if not .Indirect}}{{.Path}} {{.Version}}{{end}}' all
go list -m all
npm --prefix web outdated
rg -n '^FROM ' manifest/docker/Dockerfile.*
docker compose -f manifest/docker/docker-compose.yml config --images
docker compose -f manifest/docker/docker-compose.dev.yml config --images
rg --files -g '*_test.go' -g '!manifest/docker/volumes/**'
git check-ignore -v manifest/config/config.local.yaml
git ls-files manifest/config/config.local.yaml
go test ./internal/ai/indexer -run '^$' -count=1
GOTOOLCHAIN=go1.24.4 go test ./internal/config ./internal/ai/models ./internal/ai/embedder ./internal/ai/rerank ./internal/ai/trace ./internal/controller/chat ./internal/ai/agent/ops_pipeline -count=1
go mod download -json github.com/cloudwego/eino@v0.9.15
```

P00 没有运行完整 `go test ./...`、完整前端 E2E、Compose `up`、数据库 Migration、完整 Eval 或故障矩阵。
