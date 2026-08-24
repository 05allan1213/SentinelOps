# P01 后端与 Eino 精确升级

- Status: `BLOCKED`
- Started from: `main` / `2d6c0e44c1c07dc8f287d7032151eb9b9d66744a`，起始工作树干净
- Spec references: 上位 Spec `4.1`、`4.2`、`Task 1A`；执行 Plan `P01`
- Actual files: `docs/implementation/evidence/P01-backend-upgrade.md`；临时 contract test 与半升级依赖已撤回，业务源码零变化
- Red test and expected failure: 临时新增 Eino v0.9.15 公共 API 编译契约；旧 Eino v0.7.13 因缺少 `adk/middlewares/dynamictool/toolsearch` 按预期编译失败
- Local commands: 见“执行与停止证据”
- Results: Red contract `FAIL`（预期）；锁定版本解析 `PASS`；P01 局部门禁 `BLOCKED`
- Key assertions: GoFrame v2.10.2 直接依赖唯一可解析的 `github.com/emirpasic/gods/v2 v2.0.0-alpha`，与“完整依赖图无 alpha / beta / rc”冲突；Go 1.27.0 的 `go mod tidy` 会删除同版本冗余 `toolchain go1.27.0`，与“go 与 toolchain 都精确锁为 1.27.0”冲突
- Deviations from recommended route: 命中锁定依赖与版本门禁的关键前提不一致停止条件，未进入兼容适配、Docker/README 同步或局部门禁
- Raw artifact references: 无；关键短输出已内联，未产生或保留包含 Secret 的原始文件
- Unfinished items: 需要先通过 ADR 修订互斥门禁；P01 功能改动、验证和完成提交均未执行，P02 未开始

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

## ADR 建议（待确认）

需要同时明确两个决策后才能重新执行 P01：

1. Go 声明采用 Go 1.27 的 canonical 形式 `go 1.27.0`，允许省略被 `go mod tidy` 判定为冗余的同版本 `toolchain` 行；Docker 仍精确使用 Go 1.27.0。
2. 在以下方案中选择其一：明确允许 GoFrame v2.10.2 自身唯一锁定的 `gods/v2 v2.0.0-alpha` 作为受控传递依赖例外；或调整 GoFrame 目标版本/“依赖图无预发布版”门禁。当前证据不支持在不改变 Spec 的情况下自行选择。
