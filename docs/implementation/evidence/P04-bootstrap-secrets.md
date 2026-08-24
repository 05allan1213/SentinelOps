# P04 启动角色与 Secret fail-fast

- Status: PASS
- Started from: `c530e3c53c64553ec297e6d429cc217f68cb2c37`；`main`；开工时工作树干净
- Spec references: 上位 Spec `3.5`、Task 2；执行 Plan `P04`、`2.1～2.7`
- Actual files: `internal/bootstrap/{api.go,worker.go,all.go,config.go,bootstrap_test.go}`；`internal/config/{config.go,config_test.go,secret.go,secrets_test.go}`；`main.go`；`utility/auth/{jwt.go,jwt_test.go}`；`internal/dao/mysql/{database.go,seed.go}`；现有 Chat/Embedding/Rerank/SMTP 构造点及测试；`manifest/config/config*.yaml`；`manifest/docker/{Dockerfile.backend,docker-compose.yml,docker-compose.test.yml,docker.sh,nginx/nginx.conf}`；`.gitignore`、`README.md`；本证据
- Red test and expected failure: 先新增计划命名的角色/生产/配置/Secret 测试，精确命令以编译 `FAIL` 退出，缺少 `SecretRef`、角色类型和 bootstrap dependency，明确不是 0 tests 或环境跳过
- Local commands: 见“局部门禁与执行记录”
- Results: P04 Red 为预期 `FAIL`；最终局部门禁 `PASS`；在线供应商、真实服务栈启动、完整后端/P43/Eval `NOT RUN`
- Key assertions: 同一二进制显式解析三种角色，API 不启动 Worker、Worker 不绑定 HTTP、生产拒绝 `all`；生产缺库/库不可用/default/空必需 Secret 均先于服务启动失败；唯一 Resolver 只接受 `env:`/`file:` 引用并在显式回调后清零临时字节；JWT/admin/model/SMTP 已接线，MCP/Effect 只保留同一 contract 引用而未提前实现后续能力；Provider/Catalog/Routing、有限 Driver、完整 local replacement 和配置先于 WarmUp 均有测试；durable Agent Gate 关闭
- Deviations from recommended route: 为在不丢失、不打印本地既有模型 Key 的情况下完成引用迁移，将其机械移动到被忽略且权限为 `0600` 的 `manifest/config/.secrets/model_api_key`，local 配置只保存 `file:` 引用；生产配置只使用 `env:` 引用
- Raw artifact references: 无
- Unfinished items: P04 无未完成项；P05 及后续单元未实施

## Boundary Audit

- 目标：同一二进制显式支持 `api`、`worker`、`all` 三种角色；`all` 仅允许开发环境；生产环境对数据库和必需 Secret fail-fast；以唯一 Resolver 从引用即时解析 JWT、管理员初始密码、模型、MCP、SMTP 与 Effect Secret；保留 Provider → Model Catalog → Routing、完整 local replacement、有限 Driver 校验和配置先于 Retrieval WarmUp 的唯一启动路径；在 P03 隔离 Compose 夹具上补 API/Worker/migrate 轮廓。
- 明确非目标：不实现 P05 RBAC/身份映射，不实现 durable Agent、Run claim/执行、Scheduler/Queue 新抽象，不实现 MCP/Effect 的后续业务能力，不执行 P43 全量验证，不修改上位 Spec/Plan 契约或 remote；提交后只按执行 Plan 规定回填 P04 台账。
- 兼容契约：保留既有 HTTP 路由、前端静态托管默认值、现有后台 scheduler/knowledge worker、GoFrame 配置 Adapter、Provider/Catalog/Routing 引用与 OpenAI-compatible Chat/Embedding、DashScope-compatible Rerank 行为；durable Agent Gate 保持关闭。
- 安全不变量：生产缺库、库不可用、默认或空必需 Secret 均拒绝启动；配置与可持久化对象只保存 Secret 引用，不记录解析值；所有显式解析共用一个 Resolver，调用结束清零临时字节；业务代码不比较具体 Provider 名；不读取、打印、暂存或提交 `config.local.yaml` 的 Key；测试 Compose 不使用固定容器名、共享网络、宿主机数据卷或固定端口。
- 预计文件：`internal/bootstrap/*`、`internal/config/*`、模型/Embedding/Rerank 的现有构造点和测试、JWT/admin/SMTP 的现有 Secret 消费点、`internal/dao/mysql`、`main.go`、`manifest/config/config*.yaml`、`manifest/docker/{Dockerfile.backend,docker-compose.yml,docker-compose.test.yml}`、本证据。
- 验证方式：先运行计划列出的 Red 测试确认真实失败；再运行 P04 精确局部测试、受影响包测试与 `go vet`、根包前端托管测试、两份 Compose config、Secret/Provider 名负向扫描及 Git ignore/track 检查。
- 回滚方式：通过本单元独立本地提交的普通反向提交恢复；不执行数据库 Down，不修改或推送 remote。

## Build-or-Reuse

- 现有代码能力：原位复用 `internal/config` 的完整配置选择、GoFrame Adapter、Provider/Catalog/Routing/Driver 校验与 `Current`，复用 P03 的只读 Schema 检查、独立 migrate Job 和隔离 Compose；复用现有 GoFrame HTTP 路由及 scheduler/knowledge worker，不新建 Runtime、Store、Registry、Queue 或 Agent Loop。
- Eino / Eino-ext 官方能力：继续通过已锁定的官方 OpenAI Chat 与 OpenAI ACL Embedding Adapter 构造模型；P04 不新增 Eino 生命周期抽象，显式 Retrieval WarmUp 仍是现有模型和检索初始化入口。
- 剩余业务缺口：`main.go` 同时绑定 HTTP 与启动全部后台任务；Worker 无独立阻塞生命周期；配置仍保存明文/default Secret 与供应商旧 Embedding Driver 名；数据库未配置在生产可静默跳过；模型和 SMTP 调用点直接读取明文配置。
- 最薄 Adapter：新增只负责编排角色和依赖顺序的 `internal/bootstrap`，以可注入 Hooks 复用现有初始化函数；在现有 `internal/config` 增加有限 SecretRef/Resolver contract 与生产校验，不建设第二套配置中心或通用 Secret 管理平台。

## Red 证据

先写计划列出的角色、生产、配置和 Secret 测试，再执行：

```text
$ go test ./internal/bootstrap ./utility/auth ./internal/dao/mysql ./internal/config \
    -run 'Test(Bootstrap|Production|Config|Secret|Provider|Driver)' -count=1
internal/bootstrap/bootstrap_test.go:12:33: undefined: appconfig.SecretRef
internal/bootstrap/bootstrap_test.go:24:3: unknown field App in struct literal of type config.Config
internal/bootstrap/bootstrap_test.go:79:68: undefined: dependencies
internal/config/secrets_test.go:15:13: undefined: SecretRef
internal/config/secrets_test.go:30:3: unknown field Database in struct literal of type Config
FAIL SentinelOps/internal/bootstrap [build failed]
FAIL SentinelOps/internal/config [build failed]
```

命令退出 1，失败来自 P04 尚未实现的 contract。`utility/auth` 和 `internal/dao/mysql` 当时没有命中目标 Case，不用于替代上述真实 Red。

## 实现结果

- `main.go` 只解析 `api / worker / all` 并委托 `internal/bootstrap`；API 绑定原 HTTP 路由，Worker 启动原 scheduler、knowledge worker 与 Ops compensation，`all` 只在显式 `development` 配置下组合两者。
- bootstrap 保留“完整配置选择/校验并设置 GoFrame Adapter/Current → Secret/数据库/角色依赖 → Retrieval WarmUp → API/Worker 生命周期”的单一路径。API 不调用 Worker hook，Worker 不调用 HTTP hook；生产 DB 引用缺失、解析失败、连接/Schema 检查失败都在绑定服务前返回错误。
- `internal/config` 原位保留 Provider → Model Catalog → Routing；Provider Endpoint 改为三个有限 Driver key：OpenAI-compatible Chat、OpenAI-compatible Embedding、DashScope-compatible Rerank。构造 Chat/Embedding/Rerank 时只从 provider-qualified Catalog Ref 解析 Provider、Endpoint、厂商 Model ID 与 SecretRef，不比较示例 Provider 名。
- 配置对象、JSON 和模型解析元数据只保存 SecretRef；旧 `dsn`、JWT `secret`、`admin_password`、Provider `api_key` 与 SMTP `smtp_pass` 字段即使出现也 fail-fast。JWT、管理员 seed、模型构造和 SMTP 发送只在 `UseSecret` 显式回调中取得临时字节；SMTP 明文 Tool 参数直接拒绝。
- `config.local.yaml` 继续完整替代且不合并；测试只比较配置键形状和引用，不读取 Secret 文件。文件与 `.secrets/` 均被 Git 忽略且未跟踪，本地模型 Secret 文件权限为 `0600`。
- 生产 Compose 使用独立 `api`、`worker`、`migrate` 服务；API 不挂 Docker socket 或共享写入目录，Worker 保留既有 Ops 所需挂载；Nginx 和部署脚本指向 API。P03 测试夹具增加无固定容器名、共享/external network、宿主机数据 bind mount或固定宿主端口的依赖与角色轮廓。

## 局部门禁与执行记录

计划原命令与新增配置 contract：

```text
$ go test ./internal/bootstrap ./utility/auth ./internal/dao/mysql \
    -run 'Test(Bootstrap|Production|Config)' -count=1 -v
PASS: 角色边界、显式角色解析、all 开发限制、缺失/不可用数据库、default/空 Secret、配置先于 WarmUp、JWT 空 Secret
ok SentinelOps/internal/bootstrap
ok SentinelOps/utility/auth
ok SentinelOps/internal/dao/mysql [no tests to run]

$ go test ./internal/config -run 'Test(Config|Secret|Provider|Driver)' -count=1 -v
PASS: 引用-only 序列化、legacy 明文字段拒绝、唯一/短生命周期 Resolver、跨 Provider 同名厂商 Model 隔离、完整 local replacement、有限 Driver Endpoint

$ go test . -run '^TestFrontendHostingEnabled' -count=1 -v
PASS: 2 cases
```

`internal/dao/mysql` 在计划过滤器下没有 P04 Case；生产 DB 的缺失/不可用边界由实际 bootstrap 初始化 hook 测试覆盖，P03 已提交的真实 MySQL Migration/Schema contract 未在 P04 重跑或冒充本单元证据。

受影响既有行为、静态检查和配置：

```text
$ go test ./internal/ai/models ./internal/ai/embedder ./internal/ai/rerank \
    ./internal/ai/ops/actions -count=1
PASS

$ go vet ./internal/bootstrap ./internal/config ./utility/auth ./internal/dao/mysql \
    ./internal/ai/models ./internal/ai/embedder ./internal/ai/rerank \
    ./internal/ai/ops/actions .
PASS

$ bash -n manifest/docker/docker.sh
PASS

$ docker compose -f manifest/docker/docker-compose.yml config
PASS

$ docker compose -f manifest/docker/docker-compose.test.yml \
    --profile bootstrap --profile migration config
PASS
```

只含 Git 跟踪基线、本单元修改和未忽略新增文件的 throwaway context 先执行 `go mod vendor`，再使用当前 `Dockerfile.backend` 构建：

```text
$ docker build --file <throwaway>/manifest/docker/Dockerfile.backend \
    --tag sentinelops-p04-backend:local <throwaway>
PASS
$ docker image inspect sentinelops-p04-backend:local --format '{{json .Config.Cmd}}'
["./server","api"]
$ docker run --rm sentinelops-p04-backend:local go version
go version go1.27.0 linux/amd64
```

测试镜像已删除；临时 context 已移入系统回收站。第一次组合命令因安全层拒绝不可恢复清理而未执行，第二次因误用 zsh 特殊变量 `path` 在复制阶段失败且临时目录随后移入回收站；修正为非保留变量后的上述构建才是 `PASS` 证据。

负向扫描最终输出 `P04_NEGATIVE_GATES=PASS`，范围包括：生产 Go 无示例 Provider 名/Provider 名比较；tracked 配置无旧 Driver、模型/JWT/admin/DB/SMTP 明文字段或已知默认 Secret；测试 Compose 无固定容器名、external network 或宿主 bind mount；local 配置与 Secret 文件均 ignored/untracked，Secret 文件精确为 `0600`。首次组合扫描存在 shell 引号构造错误，未当作门禁结果；修正后的 fail-fast 命令退出 0。

另一次受影响包探索命令未加 P04 filter，因没有提供 `SENTINELOPS_TEST_DSN` 而命中 P03 的七个真实 MySQL 测试并退出 1；未连接任何数据库。该结果不属于 P04 必需失败，最终只使用上面的精确局部门禁，且没有把 `[no tests to run]` 当成 MySQL PASS。

第一次提交前组合检查由 `git diff --check` 找到 README 架构图一处行尾空格；当场做了纯机械修复。随后使用 `set -e` 重跑 `git diff --check`、全部上述测试/vet/脚本/Compose 门禁，组合命令退出 0。

## NOT RUN

- 在线文本生成、Tool Calling、Embedding、Rerank preflight：未设置 `SENTINELOPS_ONLINE_TEST=1`，`NOT RUN`；P04 不把协议 preflight 当作完整 Eval。
- 真实生产/共享数据库、完整 Compose 服务启动、共享 Secret backend、Hosted CI、完整 `go test ./...`、race、前端 E2E、40+ Case Eval、灰度与发布：`NOT RUN`，分别留给对应单元或 P43。
- P05 RBAC、durable Agent、MCP/Effect 业务调用、Resume/Replay/Approval/Effect 语义：`NOT RUN`，未跨单元实现。
