# P03 goose、Expand Schema 与版本检查

- Status: PASS
- Started from: `1a340e3e6de6e2a0dde8fd9c5747a5df109138cc`；`main`；开工时仅有用户对 `AGENTS.md` 的未提交规则补充，本单元保留且不暂存
- Spec references: 上位 Spec `7.1～7.9`；执行 Plan `P03`、`2.1～2.7`
- Actual files: `migrations/00001_runtime_foundation.sql`～`00006_retention_indexes.sql`；`internal/dao/mysql/{database.go,schema_version.go,migrations_test.go}`；删除 `internal/dao/mysql/indexes.go`；`main.go`；`manifest/docker/{Dockerfile.migrate,docker-compose.yml,docker-compose.test.yml}`；`go.mod` / `go.sum`；本证据
- Red test and expected failure: 先建立完整 contract tests 后，在隔离 MySQL 上运行目标过滤命令；包按预期编译 `FAIL`，明确缺少 `openAndCheckSchema`、`CheckSchemaVersion`、`RequiredSchemaVersion` 与 `ErrSchemaVersionMismatch`，不是 0 tests 或环境跳过
- Local commands: 见“执行记录”
- Results: P03 局部门禁 `PASS`；完整后端/前端、Eval、Hosted CI、共享/真实数据库 Migration 与 P43 全量门禁 `NOT RUN`
- Key assertions: 空库和当前 GORM Schema snapshot 均从版本 0 Up 到 6；Spec `7.3～7.9` 全部 contract 通过；旧模型读写兼容；legacy Run 为 `runtime_mode=legacy`；Trace cached/reasoning Token 保留；应用启动不执行 DDL且版本差异 fail-fast；独立 migrate 镜像为 goose v3.27.3；Down 只在 throwaway 数据库验证；同 project 完整清理
- Deviations from recommended route: 测试通过本地锁定版 goose CLI 驱动同一 SQL，而不把 migration runner 链接进应用模块；当前 GORM snapshot 的 legacy `TEXT`、nullable/default 语义原样保留；Expand 未采用 foreign key，以避免给既有数据增加破坏性约束，ID/事务语义留给既定 Store 单元
- Raw artifact references: 无；Compose config 与 Down 短日志只写入 `/tmp`，验证后未保留；未创建 `.artifacts` 内容
- Unfinished items: P03 无未完成项；P04 启动角色/Secret、P07 cutover/backfill、P08 及后续 runtime/DAO 接线不属于本单元

## Boundary Audit

- 目标：用 goose v3.27.3 的六个不可变 SQL Migration 接管当前 Schema 与 Spec `7.3～7.9` 的 Expand DDL；应用启动仅打开数据库并核对 Schema version；提供独立 migrate 镜像/Job 与一次性 MySQL 测试夹具。
- 明确非目标：不实现 P04 的 `api / worker / all` 角色与 Secret fail-fast，不实现 P07 Legacy cutover/backfill，不实现 P08 及后续 Store primitive、claim、Checkpoint Adapter、Approval/Effect 运行时语义、Evidence DAO 映射或 Contract cleanup。
- 兼容契约：保留当前 21 个 GORM 模型/表、旧列、旧数据与 DAO 读写；Expand 后旧模型仍可读写；Trace 的 `cached_input_tokens` / `reasoning_tokens` 不丢失；既有 Run 显式获得 `runtime_mode=legacy`。
- 安全不变量：只操作数据库名以 `sentinelops_p03` 开头的一次性 MySQL；不读取本地真实配置或 Key；不启动 dev/prod Compose；测试 project 无固定容器名、共享/external network、宿主机数据 bind mount或固定端口；每次 `up` 后只对同一 project 执行 `down -v --remove-orphans`。
- 预计修改：`migrations/`、`internal/dao/mysql` 的 migration/version contract 与生产初始化、`main.go`、`manifest/docker` 的 migrate 接线和隔离测试夹具、`go.mod/go.sum`、本证据文件。
- 验证方式：真实 Red；空库 Up、当前 Schema snapshot Up、旧模型兼容、完整 Schema contract、版本不匹配、启动无 DDL、一次性 Down、Compose config、goose status、受影响包测试与 `go vet`。
- 回滚方式：应用版本通过本单元独立提交的普通反向提交恢复；生产部署不自动执行 Down；Down 仅在已验证的 throwaway 数据库运行。

## Build-or-Reuse

- 现有代码能力：保留 GORM 作为 ORM/查询/事务层，复用现有 21 个模型和 `internal/ai/workflow.GORMStore`，不新建 Store 或 Schema DSL。
- 官方能力：直接使用锁定版 goose v3.27.3 解析、执行、记录版本和提供 CLI，不复制 migration runner 或版本表协议；MySQL `information_schema` 用于 contract 查询。
- 剩余业务缺口：现有生产启动仍执行 `AutoMigrate` 与额外索引 DDL，没有空库/current snapshot 的版本化基线、启动版本检查、完整 durable Schema contract 或隔离 migrate Job。
- 最薄 Adapter：只增加 MySQL Schema version 检查与 DSN 打开函数；migration 执行留给 goose CLI/Job，应用进程不嵌入自动 Up。

## Red 证据

先添加隔离 Compose、migration/version/Schema contract tests，再执行：

```text
$ docker compose -p sentinelops-p03 -f manifest/docker/docker-compose.test.yml up -d --wait mysql
$ SENTINELOPS_TEST_DSN=<redacted> go test ./internal/dao/mysql -run 'Test(Migrations|SchemaVersion|ApplicationStartup|RuntimeSchemaContract|LegacyApplication)' -count=1
internal/dao/mysql/migrations_test.go:55:12: undefined: openAndCheckSchema
internal/dao/mysql/migrations_test.go:56:21: undefined: ErrSchemaVersionMismatch
internal/dao/mysql/migrations_test.go:75:51: undefined: RequiredSchemaVersion
internal/dao/mysql/migrations_test.go:152:12: undefined: CheckSchemaVersion
FAIL SentinelOps/internal/dao/mysql [build failed]
```

命令退出 1，状态为预期 `FAIL`。失败后 finally 只清理 `sentinelops-p03` project；没有启动或修改 dev/prod 栈。

## Schema 与兼容证据

六个顺序 SQL migration 由 goose 自己维护 `goose_db_version`，空库和当前 Schema snapshot 最终都为精确版本 6。`00001` 先建立现有 21 表的不可变基线，再以 Expand 增加 durable Run/Event/Session 列；`00002～00006` 分别覆盖 Eino opaque Checkpoint、Approval/Effect、RBAC/settings 索引、Evidence metadata 与 retention 索引。应用仍使用现有 GORM DAO，但 `database.go` 与 `main.go` 已无 `AutoMigrate`、`Migrator` 或启动期索引 DDL；旧 `CreateIndexes` helper 已删除。

`TestRuntimeSchemaContract` 与 current-snapshot 路径逐项读取 `information_schema`，核对：

- `workflow_runs` 的全部 legacy 列和 Spec `7.3` durable 列、nullable/default、active Session 唯一约束，以及 claim/reap/status/user scope/retention 组合索引；
- `workflow_events` 的 `(run_id, seq)` 唯一约束、unsigned durable seq、版本化 payload 与 replay/retention 索引；
- `workflow_checkpoints` 的 legacy JSON 和 nullable Eino ID/blob/hash/version/generation/expiry 列，以及 Eino ID 唯一约束；
- `agent_approvals`、`agent_effects` 的 Spec 全字段、CAS version、稳定身份唯一约束、reconcile/status/expiry/retention 索引；
- `session_state_revisions` 的 session+revision 唯一约束；三个既有 Knowledge 表的全部 Evidence metadata 与 scope/version 索引；
- 当前 Trace Run/Node 的 `cached_input_tokens` 和 `reasoning_tokens` 类型、nullable/default 与旧值不变。

旧 GORM model contract 在 Expand 空库中实际创建并读回 Workflow Run/Event/Checkpoint、Knowledge Base、Trace Run/Node；current-snapshot fixture 在 Up 后仍存在，`runtime_mode` 精确为 `legacy`。没有为后续 P07/P08 添加 backfill、claim 或 Store 运行时逻辑。

## 启动、migrate Job 与安全边界

`CheckSchemaVersion` 只读 goose 最后一条版本记录并要求精确版本 6。空 Schema 上调用启动打开函数返回 `ErrSchemaVersionMismatch`，随后 `information_schema` 仍为 0 张表；版本 5 同样返回该错误。`main.go` 对配置了数据库但版本不匹配的情况 fail-fast，不再降级继续运行。

测试 Compose 使用 MySQL `8.0.43`、tmpfs 数据目录和宿主机随机端口，没有 `container_name`、固定/共享/external network、宿主机数据 bind mount或固定冲突端口。数据库安全 helper 只接受基库名 `sentinelops_p03`，测试子库名也固定此前缀。所有 Up/Down 均在这一 throwaway project 内；每轮 finally 执行同 project 的 `down -v --remove-orphans`。最终检查无 P03 容器或 volume 残留。

独立 migrate 镜像使用 Go `1.27.0` 构建 goose `v3.27.3`，只复制 goose binary 与 `migrations/`。第一次容器内构建访问官方 Go proxy 超时，真实状态为 `FAIL`；使用同一 Dockerfile/版本、host network 与当前本地代理重试后 `PASS`。镜像内 `goose -version` 为 `v3.27.3`，Compose migrate Job 从 0 执行到 6，host CLI `status` 显示六个文件全部 Applied。验证后本地测试镜像已删除。

## 局部门禁

最终局部门禁输出：

```text
$ docker compose -p sentinelops-p03 -f manifest/docker/docker-compose.test.yml config
PASS

$ SENTINELOPS_GOOSE_BIN=<goose-v3.27.3> SENTINELOPS_TEST_DSN=<redacted> \
    go test ./internal/dao/mysql -run 'Test(Migrations|SchemaVersion|ApplicationStartup|RuntimeSchemaContract|LegacyApplication)' -count=1
ok  SentinelOps/internal/dao/mysql

$ SENTINELOPS_GOOSE_BIN=<goose-v3.27.3> SENTINELOPS_TEST_DSN=<redacted> \
    go test ./internal/dao/mysql -count=1
ok  SentinelOps/internal/dao/mysql

$ go test . -count=1
ok  SentinelOps

$ go vet ./internal/dao/mysql .
PASS

$ goose -dir migrations mysql <redacted-dsn> status
00001_runtime_foundation.sql -- 00006_retention_indexes.sql: Applied

$ goose -dir migrations mysql <redacted-disposable-dsn> down-to 0
current version: 0
```

`TestMigrationsDownOnDisposableDatabase` 还断言 Down 后除 goose 自身版本表外没有业务表。生产回滚不调用 Down；没有连接共享、开发或真实数据库。

## 执行记录

以下命令均在仓库根执行；DSN、代理地址与临时端口未进入证据：

```bash
docker compose -p sentinelops-p03 -f manifest/docker/docker-compose.test.yml config
docker compose -p sentinelops-p03 -f manifest/docker/docker-compose.test.yml up -d --wait mysql
GOBIN=/tmp/sentinelops-p03-tools go install github.com/pressly/goose/v3/cmd/goose@v3.27.3
SENTINELOPS_GOOSE_BIN=<pinned> SENTINELOPS_TEST_DSN=<redacted> go test ./internal/dao/mysql -run <P03-filter> -count=1
SENTINELOPS_GOOSE_BIN=<pinned> SENTINELOPS_TEST_DSN=<redacted> go test ./internal/dao/mysql -count=1
go test . -count=1
go vet ./internal/dao/mysql .
docker compose -p sentinelops-p03 -f manifest/docker/docker-compose.test.yml --profile migration build migrate
docker build --network host --build-arg HTTPS_PROXY=<local> --file manifest/docker/Dockerfile.migrate --tag sentinelops-p03-migrate .
docker compose -p sentinelops-p03 -f manifest/docker/docker-compose.test.yml --profile migration run --rm migrate
goose -dir migrations mysql <redacted-disposable-dsn> status
goose -dir migrations mysql <redacted-disposable-dsn> down-to 0
docker compose -p sentinelops-p03 -f manifest/docker/docker-compose.test.yml --profile migration down -v --remove-orphans
docker image rm sentinelops-p03-migrate
```

未运行完整 `go test -race ./...`、完整前端/E2E、Eval、故障矩阵、Hosted CI、共享/真实数据库 Migration、生产 Down、灰度或发布动作；这些按 Plan 留给对应单元或 P43。
