# P01 后端与 Eino 精确升级

- Status: `PASS`
- Started from: `main` / `2d6c0e44c1c07dc8f287d7032151eb9b9d66744a`，起始工作树干净
- Resumed from: `main` / `f943832bab474e5fb6060571001e10e6bae3f7d2`；用户明确授权两项 ADR 后恢复执行，恢复时工作树干净
- Spec references: 上位 Spec `4.1`、`4.2`、`Task 1A`；执行 Plan `P01`
- Actual files: `go.mod`、`go.sum`、`manifest/docker/Dockerfile.backend`、`README.md`、`internal/ai/compat/eino_contract_test.go`、`docs/implementation/evidence/P01-backend-upgrade.md`
- Red test and expected failure: 新增 Eino v0.9.15 公共 API 编译契约；旧 Eino v0.7.13 因缺少 `adk/middlewares/dynamictool/toolsearch` 按预期编译失败
- Local commands: 见“恢复执行与局部门禁”
- Results: Red contract `FAIL`（预期）；P01 必需局部门禁 `PASS`；在线供应商测试与 P43 全量门禁 `NOT RUN`
- Key assertions: canonical `go 1.27.0`、Eino v0.9.15、Sonic v1.15.2 及当前实际使用依赖均精确；无 `replace`、DashScope Embedder、Eino v0.10 alpha 或未授权预发布依赖；Chat/Embedding/Rerank/Routing 既有离线行为通过；后端 throwaway 镜像内为 Go 1.27.0
- Deviations from recommended route: 首次执行命中两个互斥门禁，用户授权 ADR 后恢复且不改写既有证据提交；`internal/ai/embedder/dense.go` 无需 API 适配；直接从含现有 `.runtime` 的工作目录发送 Docker context 被权限拒绝，改用只含 Git 跟踪文件与本单元显式覆盖的 throwaway context 验证镜像
- Raw artifact references: 无；关键短输出已内联；throwaway context 与本地测试镜像验证后已清理，未读取或保留 Secret
- Unfinished items: P01 无未完成项；P02 及后续单元未开始

## Boundary Audit

- 目标：将 Go、Eino 和当前实际使用的后端依赖精确升级到锁定版本，并用编译契约固定 Eino v0.9.15 的公共 API。
- 明确非目标：不执行 P02，不迁移专业 Agent，不实现 Runtime、HITL、MCP、Skill、Langfuse 或后续单元能力，不运行完整全仓验证或完整 Eval。
- 兼容契约：保持现有 Provider -> Model Catalog -> Routing、OpenAI-compatible Chat、OpenAI ACL Embedding、2048 维校验和 Rerank 行为；只因官方签名变化做最小适配。
- 安全不变量：不读取或记录 Secret；不引入 `components/embedding/dashscope`、`replace`、预发布 Eino 或仅用于占位锁版的生产 blank import。
- 预计修改：`go.mod`、`go.sum`、`manifest/docker/Dockerfile.backend`、`README.md`、`internal/ai/compat/eino_contract_test.go`，以及真实编译错误要求的现有调用点。
- 验证方式：执行 P01 指定的局部测试、`go vet`、MVS/依赖图/负向扫描，并复核实际测试 Case 被执行。
- 回滚方式：通过本单元独立本地提交的普通反向提交恢复；不涉及数据库、远程或运行时外部状态。

## Build-or-Reuse

- 现有代码能力：保留现有 Eino Graph/ReAct/ADK PlanExecute 调用、Tool Registry、模型 Adapter 和 Embedder，不创建平行实现。
- Eino / Eino-ext 官方能力：直接编译锁定 ADK Runner、Checkpoint、ChatModelAgent middleware、AgentTool、Retry/Failover、Skill 和 Tool Search 公共 API，不复制其实现。
- 剩余业务缺口：本单元只有依赖版本与官方 API 兼容性缺口；实际 Runtime、安全策略和生命周期属于后续单元。
- 最薄 Adapter：只允许升级后真实编译错误所需的现有调用点适配；未真实使用的独立 Eino-ext 模块留到归属单元首次引入。

## 执行与停止证据

### Red contract

临时 contract test 导入锁定版本要求的 Tool Search 公共包，并仅运行目标 Case：

```text
$ go test ./internal/ai/compat -run '^TestEinoPublicContracts$' -count=1
FAIL SentinelOps/internal/ai/compat [setup failed]
internal/ai/compat/eino_contract_test.go:8:2: no required module provides package github.com/cloudwego/eino/adk/middlewares/dynamictool/toolsearch
```

该命令退出 1，结果为预期 `FAIL`，不是 `[no tests to run]`。临时测试在确认停止后撤回，避免旧依赖基线长期处于不可编译状态。

### 锁定矩阵尝试

以下目标版本均能够从配置的 Go module proxy 解析，升级命令本身退出 0：Eino v0.9.15、三个实际使用的 Eino-ext pseudo-version、GoFrame v2.10.2、GORM v1.31.2、go-redis v9.22.0、Sonic v1.15.2、x/crypto v0.55.0、x/time v0.15.0；OpenAI model v0.1.13 与 OpenAI ACL v0.1.17 保持不变。

`go mod tidy` 随后选入：

```text
github.com/emirpasic/gods/v2 v2.0.0-alpha
```

来源链可复核为：

```text
SentinelOps
github.com/gogf/gf/v2/frame/g
github.com/gogf/gf/v2/os/gres
github.com/gogf/gf/v2/container/gtree
github.com/emirpasic/gods/v2/trees/avltree
```

`go mod graph` 同时证明直接边：

```text
github.com/gogf/gf/v2@v2.10.2 github.com/emirpasic/gods/v2@v2.0.0-alpha
```

在仓库外的临时最小 module 中执行 `go list -m -versions github.com/emirpasic/gods/v2`，唯一输出版本也是 `v2.0.0-alpha`。GoFrame v2.10.2 下载模块自身的 `go.mod` 明确直接要求该版本。因此不能通过正常 MVS 选择稳定 v2 版本；使用 `replace` 又被 P01 明确禁止。

### Go toolchain 指令

在仓库外的临时最小 module 中依次执行：

```bash
go mod init example.invalid/p01
go mod edit -go=1.27.0 -toolchain=go1.27.0
go mod tidy
go get toolchain@go1.27.0
```

`go mod edit` 后 `go.mod` 同时包含 `go 1.27.0` 和 `toolchain go1.27.0`；`go mod tidy` 会删除同版本冗余 toolchain 指令，`go get toolchain@go1.27.0` 也不会恢复它。目标仓库中强行重新加入该行后，普通 `go test` 会要求再次运行 `go mod tidy`。因此“tidy 后干净可构建”与“保留同版本 toolchain 行”无法同时成立。

### 停止与清理

- 命中执行 Plan `2.4` 的停止条件：锁定依赖与 Spec 的关键版本门禁不一致。
- 未使用 `replace`、未选择不同 GoFrame 版本、未放宽预发布扫描，也未绕过 `go mod tidy`。
- 已用补丁恢复 `go.mod`、`go.sum` 和临时 contract test；现有依赖与业务源码保持 P00 基线。
- 未修改 Dockerfile、README、模型 Routing、Agent 或任何后续单元文件；未运行全仓测试、在线供应商测试、完整 Eval、Compose 或 P02。

## ADR 决议

用户已明确授权继续 P01，以下两项自恢复执行起成为 P01 的验收口径，并已同步上位 Spec 与执行 Plan：

1. Go 声明采用 Go 1.27 的 canonical 形式 `go 1.27.0`，允许省略被 `go mod tidy` 判定为冗余的同版本 `toolchain` 行；Docker 仍精确使用 Go 1.27.0。
2. 允许 GoFrame v2.10.2 自身唯一锁定的 `gods/v2 v2.0.0-alpha` 作为受控传递依赖例外；其他 alpha / beta / rc 和 Eino v0.10 alpha 仍禁止。

## 恢复执行与局部门禁

### 精确版本与 MVS

`go mod tidy` 后 `go mod edit -json` 返回 `Go: 1.27.0`、`Toolchain: null`、`Replace: null`，符合已授权 canonical 形式。实际 MVS：

| 模块 | 版本 | 状态 |
| --- | --- | --- |
| `github.com/cloudwego/eino` | `v0.9.15` | `PASS` |
| `github.com/cloudwego/eino-ext/components/indexer/milvus` | `v0.0.0-20260820123736-6752ff8da9b1` | `PASS` |
| `github.com/cloudwego/eino-ext/components/document/loader/file` | `v0.0.0-20260820123736-6752ff8da9b1` | `PASS` |
| `github.com/cloudwego/eino-ext/components/tool/duckduckgo/v2` | `v2.0.0-20260820123736-6752ff8da9b1` | `PASS` |
| `github.com/cloudwego/eino-ext/components/model/openai` | `v0.1.13` | `PASS` |
| `github.com/cloudwego/eino-ext/libs/acl/openai` | `v0.1.17` | `PASS` |
| `github.com/gogf/gf/v2` | `v2.10.2` | `PASS` |
| `gorm.io/gorm` / `gorm.io/driver/mysql` | `v1.31.2` / `v1.6.0` | `PASS` |
| `github.com/redis/go-redis/v9` | `v9.22.0` | `PASS` |
| `github.com/milvus-io/milvus-sdk-go/v2` | `v2.4.2` | `PASS` |
| `github.com/golang-jwt/jwt/v5` | `v5.3.1` | `PASS` |
| `github.com/bytedance/sonic` | `v1.15.2` | `PASS` |
| `golang.org/x/crypto` / `golang.org/x/time` | `v0.55.0` / `v0.15.0` | `PASS` |

结构化预发布扫描只返回已授权的 `github.com/emirpasic/gods/v2 v2.0.0-alpha`。`go list -m all` 和 Go 源码 / `go.mod` 扫描均未发现 `components/embedding/dashscope`；`go.mod` 没有 `replace`，生产 Go 源码没有 Eino blank import。

未被真实源码使用的未来独立模块没有写入 `go.mod`。只读解析验证 `officialmcp@v0.1.1` 与 `adk/backend/local@v0.2.6` 均可下载并有 module checksum，状态 `PASS`。

### Eino 与既有行为

恢复后重新运行 Red：

```text
$ go test ./internal/ai/compat -run '^TestEinoPublicContracts$' -count=1
FAIL SentinelOps/internal/ai/compat [setup failed]
... no required module provides package github.com/cloudwego/eino/adk/middlewares/dynamictool/toolsearch
```

升级后同一 Case 实际执行并 `PASS`。contract 固定 ADK Runner、CheckPointStore / Deleter、ChatModelAgent middleware、AgentTool、Retry / Failover、Skill、Tool Search 和 ToolCallMiddlewares 的公共编译边界。

计划指定命令：

```bash
go test ./internal/ai/compat ./internal/ai/agent/plan_pipeline ./internal/ai/tools/... -count=1
go vet ./internal/ai/compat/... ./internal/ai/agent/plan_pipeline/...
```

均退出 0，状态 `PASS`。`internal/ai/compat` 的目标 Case 实际执行；`plan_pipeline` 和各 `tools` 包没有测试文件，只记编译 `PASS`，不冒充测试 Case。

直接行为回归：

```bash
go test -v ./internal/ai/models ./internal/ai/embedder -count=1
go test -v ./internal/config ./internal/ai/rerank ./internal/ai/trace ./internal/controller/chat ./internal/ai/agent/ops_pipeline -count=1
go test ./internal/ai/indexer -run '^$' -count=1
```

离线 Case 全部 `PASS`，覆盖 Provider -> Catalog -> Routing、Thinking Options、OpenAI-compatible Chat、OpenAI ACL Embedding 与 2048 维检查、Rerank、Usage/Cost、DeepThinking timeout 和 Ops 状态转换。`indexer` 在 Go 1.27.0 下编译 `PASS`，P00 的 `undefined: GoMapIterator` 已消失。Sonic v1.15.2 在 Go 1.27 上提示 AST fast path 回退到 `encoding/json`，不影响编译或 Case 结果，已如实记录。

文本生成、Tool Calling、Embedding、Rerank 在线 Case 因未设置 `SENTINELOPS_ONLINE_TEST=1` 而跳过，状态 `NOT RUN`，不作为本单元通过证据。

### 依赖图与版本残留

以下命令均退出 0：

```bash
go list -m all
go mod graph
go mod why -m github.com/bytedance/sonic
go mod why -m github.com/cloudwego/eino
go mod why -m github.com/cloudwego/eino-ext/libs/acl/openai
go mod why -m go.opentelemetry.io/otel
go mod tidy -diff
```

依赖图共 1583 条边；`why` 分别追溯到 `internal/ai/indexer`、`internal/ai/agent/plan_pipeline`、`internal/ai/embedder` 和 GoFrame HTTP。`go mod tidy -diff` 无输出。旧 Go 版本扫描只命中不可改写的 `P00-baseline.md` 历史事实；当前 `go.mod`、Dockerfile 和 README 均已同步到 1.27.0。

### 后端镜像

两段 `FROM` 均精确为 `golang:1.27.0-alpine`。直接从当前工作目录构建时，Docker 在发送 context 阶段因现有 `.runtime/etcd/member` 权限拒绝而退出，尚未执行 Dockerfile；本单元没有读取该路径，也没有扩张到 `.dockerignore` 或 Compose 改造。

随后用 `git archive HEAD` 建立只含跟踪文件的 throwaway context，显式覆盖本单元 `go.mod`、`go.sum`、Dockerfile 和 contract test，在临时目录执行 `go mod vendor` 后构建：

```text
$ docker build --file <throwaway>/manifest/docker/Dockerfile.backend --tag sentinelops-p01-backend:local <throwaway>
... naming to docker.io/library/sentinelops-p01-backend:local done
$ docker run --rm sentinelops-p01-backend:local go version
go version go1.27.0 linux/amd64
```

构建与镜像内工具链检查均为 `PASS`。未启动项目服务或 Compose；测试容器由 `--rm` 删除，throwaway context 与本地测试镜像已清理。

### 最终局部门禁复跑

提交前将测试、vet、tidy、上述全部精确版本、canonical Go 声明、唯一预发布例外、无 DashScope / replace / blank import、当前 Go 旧版本残留和 `web/` 负向扫描组合为 fail-fast 命令重新执行，最终输出：

```text
P01_FINAL_LOCAL_GATE=PASS
AUTHORIZED_PRERELEASE=github.com/emirpasic/gods/v2 v2.0.0-alpha
```

退出码为 0，状态 `PASS`。

## 未运行范围

- 完整 `go test ./...`、完整 `go vet ./...`、race、完整前端 E2E、40+ Case 三轮 Eval、故障矩阵和在线供应商测试：`NOT RUN`，按执行 Plan `2.3` 留给对应单元或 P43。
- P02 及任何专业 Agent 迁移、Runtime、MCP、Skill、Approval、Effect、发布动作：`NOT RUN`。
