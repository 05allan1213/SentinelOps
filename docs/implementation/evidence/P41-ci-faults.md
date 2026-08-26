# P41 GitHub Actions、集成与故障注入

- Status: `PASS`（P41 局部门禁）
- Started from: `64caa27ed91cfee9c9c01091bbe055b9b9fbbe74`（`main`，P40 提交后）
- Completed: 2026-08-26
- Spec references: 实施计划 P41；上位 Spec Task 11；PR、隔离集成、定时/手动供应商 Eval、故障矩阵与镜像 contract
- Commit: 由仓库外实施计划台账回填本单元本地提交完整 SHA；未 push

## Boundary Audit

- 目标：新增 PR、隔离 integration、定时/手动 provider Eval 三类 GitHub Actions；提供本地/CI 共用薄脚本；定义可审计的故障矩阵和三包代表 Case；构建 API、Worker、frontend、migrate 镜像并记录基础镜像与应用镜像内容摘要。
- 明确非目标：不实施 P42/P43，不修改生产 Runtime、Store、Queue、Provider、MCP 或发布控制，不运行 Hosted CI、完整供应商 Eval、完整 `go test -race ./...`、发布或回滚。
- 兼容契约：复用 P38 的 `manifest/docker/docker-compose.yml` + `docker-compose.test.yml` 和唯一 project `sentinelops-e2e`；生产 Compose 保持不变；P39/P40 `cmd/agenteval`、Dataset 与 pending baseline 保持唯一生产 Runtime Eval 路径。
- 隔离与安全不变量：合并 Compose 无固定容器名、external/shared network 或开发/生产数据卷；宿主端口只绑定 `127.0.0.1`；cleanup 只对 `sentinelops-e2e` 执行 `down -v --remove-orphans`；脚本不开启命令回显，不输出 Secret、完整 DSN、Authorization、Cookie、模型输入或供应商原始响应。
- CI 供应链契约：所有第三方 Action 使用官方 release tag 对应的完整 40 位 commit SHA；PR、integration、provider Eval 职责分离；provider SecretRef 缺失只报告 `NOT RUN`，不伪造 PASS。
- 故障恢复契约：同一 durable `run_id` 保持不变，新接管轮换 attempt/trace；结果标签只能是 `RESUME PASS`、`REPLAY PASS` 或 `PARKED AS DESIGNED`；外部 Effect 不确定窗口禁止盲重试或伪装 succeeded。
- 回滚方式：通过本单元独立本地提交的普通反向提交恢复；验证未修改 remote、共享数据库或开发/生产 Compose project。

## Build-or-Reuse

| 现有能力 | 剩余缺口 | 最薄实现 |
| --- | --- | --- |
| P38 唯一 `sentinelops-e2e` Compose、provider double、真实 Playwright 审批/unknown 链 | 缺少 CI 统一入口、镜像 inventory 与 Hosted workflow | 薄 Bash 入口只编排既有 Compose、Go tests 和 Playwright，不创建第二套测试栈 |
| P10-P28 Recovery selector、Checkpoint、Worker generation/lease、Approval、Effect Ledger 与 GORMStore | 缺少跨恢复点的声明式故障目录和代表性门禁 | `manifest/ci/fault-matrix.yaml` + test-only loader；代表测试直接调用既有 selector、Runner、DAG 与状态机 |
| P06 Redactor、P04 SecretRef、P39/P40 `agenteval`/Dataset | 缺少定时/手动 provider preflight/Eval 编排 | `provider-eval.sh` 只检查引用、运行四类 online preflight、调用既有 CLI，并仅保存脱敏报告 |
| 既有 Dockerfile 与 Compose build 定义 | 缺少统一版本断言和可复核 digest | `image-contract.sh` 校验 Go/Node builder、隔离配置、构建所需服务并输出 JSON inventory |

没有新增 Runtime、Store、Queue、Provider、MCP 协议、Agent Loop、Effect 执行器或持久化实现。

## Red Evidence And Minimal Repairs

1. 首次运行计划中的代表性故障命令时，三个 package 均返回 `[no tests to run]`；该结果只证明编译，不计为测试 PASS。
2. 加入代表测试后首次真实运行按预期失败：`manifest/ci/fault-matrix.yaml` 尚不存在。增加严格 YAML loader、矩阵和既有语义断言后，三个 package 均实际执行并 PASS。
3. `image-contract.sh --no-build` 暴露两个已有 Dockerfile 的 Nginx `1.27-alpine` 摘要已无法从官方 registry 解析。`docker buildx imagetools inspect nginx:1.27-alpine` 确认当前 `1.27.5-alpine` 多架构摘要为 `sha256:65645c7bb6a0661892a8b03b89d0743208a18dd2f3f17a54ef4b76fb8e2f2a10`；只替换摘要，不升级到 Nginx 1.28。
4. 完整镜像构建前两次在 migrate 阶段失败，均为容器访问 `proxy.golang.org` 超时；API、Worker、frontend、provider-double、nginx 已成功。宿主通过 loopback proxy 联网，但 build 容器不能访问宿主 loopback。最小修复为 Dockerfile 默认仍使用 `https://proxy.golang.org,direct`，并允许 `SENTINELOPS_CI_GOPROXY` 显式覆盖；脚本拒绝带 `@` 或空白的值，避免凭证进入日志。本机用不含凭证的 `https://goproxy.cn,direct` 完成构建，goose 版本仍锁定并验证为 `v3.27.3`。

以上失败均已被后续同范围 PASS 覆盖；没有弱化断言、跳过镜像或伪造线上结果。

## Actual Files

- `.github/workflows/pr.yml`
- `.github/workflows/integration.yml`
- `.github/workflows/provider-eval.yml`
- `scripts/ci/pr.sh`
- `scripts/ci/workflow-contract.sh`
- `scripts/ci/image-contract.sh`
- `scripts/ci/integration.sh`
- `scripts/ci/provider-eval.sh`
- `scripts/ci/redact.go`
- `manifest/ci/fault-matrix.yaml`
- `internal/testutil/faultmatrix/matrix.go`
- `internal/ai/runtime/p41_fault_injection_test.go`
- `internal/ai/effects/p41_fault_injection_test.go`
- `internal/ai/workflow/p41_fault_injection_test.go`
- `manifest/docker/docker-compose.test.yml`
- `manifest/docker/Dockerfile.frontend`
- `manifest/docker/Dockerfile.nginx.e2e`
- `manifest/docker/Dockerfile.migrate`
- `.gitignore`
- `docs/implementation/evidence/P41-ci-faults.md`

生产 `manifest/docker/docker-compose.yml` 未修改。

## Workflow Contracts

| Workflow | Trigger / responsibility | Result |
| --- | --- | --- |
| `pr.yml` | `pull_request` / manual；后端 gofmt、tidy diff、vet、固定版本 staticcheck/govulncheck、race；前端 Node `24.19.0`、`npm ci`、lint/build；scripted Agent/MCP/Skill/Policy/Budget/fault contracts | 配置与本地薄入口 `PASS`；Hosted job `NOT RUN` |
| `integration.yml` | `main` push / manual；唯一 `sentinelops-e2e` 镜像、migration、代表集成/故障和 P38 Playwright | 配置、隔离、镜像 contract `PASS`；完整 workflow/脚本端到端 `NOT RUN` |
| `provider-eval.yml` | 仅 `workflow_dispatch` / schedule；四类 online preflight 后调用 P39/P40 production Runtime Eval | 缺少引用时本地 `NOT RUN`；真实供应商与 Hosted job `NOT RUN` |

第三方 Action pins：

- `actions/checkout` v4.4.0: `11d5960a326750d5838078e36cf38b85af677262`
- `actions/setup-go` v6.5.0: `924ae3a1cded613372ab5595356fb5720e22ba16`
- `actions/setup-node` v6.5.0: `249970729cb0ef3589644e2896645e5dc5ba9c38`
- `actions/upload-artifact` v4.6.2: `ea165f8d65b6e75b540449e92b4886f43607fa02`

`git ls-remote` 对上述官方 tag 返回相同完整 SHA；`workflow-contract.sh` 拒绝 tag、分支名和短 SHA。

## Fault Matrix

矩阵 schema 为 `sentinelops/fault-matrix/v1`，共 18 个 Case：

| Outcome | Count | Representative proof |
| --- | ---: | --- |
| `RESUME PASS` | 7 | HITL exact checkpoint、SIGTERM、SIGKILL/OOM + valid checkpoint、lease lost、已成功 Effect 复用 |
| `REPLAY PASS` | 3 | SIGKILL/OOM + no checkpoint、无 Approval/Effect 恢复依赖、model stream 失败 |
| `PARKED AS DESIGNED` | 8 | external pending/unknown、Approval fingerprint/checkpoint 损坏、MCP 断线、MySQL 不确定窗口、预算停止 |

全局断言要求 `run_id` 不变、attempt/trace 轮换、Context/Budget/Runtime Snapshot 保留、Mutation 去重。代表性测试复用既有 Recovery selector/官方 Runner、Effect invocation/DAG、GORMStore 状态机与 generation fence；test helper 不注册生产 Provider。

## Image Inventory

执行：

```bash
SENTINELOPS_CI_GOPROXY=https://goproxy.cn,direct \
  scripts/ci/image-contract.sh --output-dir .artifacts/p41/image-contract-final
```

结果：`PASS`。该环境变量只处理本机 Docker build 的外网限制；Dockerfile 与 Hosted CI 默认仍为 `https://proxy.golang.org,direct`。

基础镜像：

| Reference | Resolved digest |
| --- | --- |
| `golang:1.27.0-alpine` | `sha256:4c9fe60190a2a3350ddc51de80d0224b8a6698d12bdfc999fee45ea9d6c46dbc` |
| `golang:1.27.0-bookworm` | `sha256:ded31c68586d2e49e760acc2e65a884b23d032e9bbbed0ae0c55abd3fcaf4452` |
| `node:24.19.0-alpine` | `sha256:d32cdf619f63fe0471182d08996dd516c6275bb5fd31ae06e55a570bd9e1ad43` |
| `nginx:1.27-alpine` pinned multi-arch | `sha256:65645c7bb6a0661892a8b03b89d0743208a18dd2f3f17a54ef4b76fb8e2f2a10` |
| `debian:bookworm-slim` | `sha256:88200866dfff7ea7f5cbcb6ec7c8a701889efe6fe859fe64d6990e4b07ea4171` |
| `alpine:3.22` | `sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce` |

本地内容寻址应用镜像：

| Service | Digest |
| --- | --- |
| API | `sha256:e060098b1ad3ae2f825f5ef8f02dccbc9149062600cba9c5cd25d08737442d03` |
| Worker | `sha256:79665b9b7a568dc88ebc922e328ac91c46eb2ac87e3fac3d4d9a21363a111036` |
| frontend | `sha256:10cb83d61ee34c33845d83be593b5629366a2943874fe1a0e16b0c66ab72af91` |
| migrate | `sha256:3dcaf03f1ae8663f79a0b129b69b7c54ecfc9c7bebebf3e8f8c49584a5a17b72` |
| provider-double | `sha256:b7cd9e9cc85a32f1dc0b6b172e1bacfa04ac01b2801a01138d4461d0659f2703` |
| nginx | `sha256:f0f3befad0bedcceffa6536f796ec0cc416036bc659f020ddda90122337021f5` |

这些是本地 BuildKit/containerd 内容摘要，不冒充已 push 的 registry digest。

## Verification Ledger

本机工具：Go `1.27.0`、Node `24.19.0`、npm `12.0.2`、Docker `29.7.1`、Compose `v5.4.0`、actionlint `v1.7.12`、ShellCheck `0.11.0`、staticcheck `2026.2.1 (0.8.1)`、govulncheck `v1.7.0`、Git `2.43.0`。

| Gate | Command / evidence | Result |
| --- | --- | --- |
| Workflow YAML | `actionlint .github/workflows/*.yml` | `PASS` |
| Shell syntax | `bash -n scripts/ci/*.sh` | `PASS` |
| Shell static analysis | `shellcheck scripts/ci/*.sh` | `PASS` |
| Workflow separation / immutable pins | `scripts/ci/workflow-contract.sh` | `PASS` |
| Agent/MCP/Skill/Policy/Budget contracts | `scripts/ci/pr.sh --lane contracts` | `PASS`；所有列出的 package 均实际执行 |
| Representative fault injection | `go test ./internal/ai/runtime ./internal/ai/effects ./internal/ai/workflow -run 'TestFaultInjectionRepresentative' -count=1` | `PASS`；三个 package 均实际执行 |
| Compose isolation | `docker compose -p sentinelops-e2e -f manifest/docker/docker-compose.yml -f manifest/docker/docker-compose.test.yml config --format json` | `PASS`；project 精确为 `sentinelops-e2e`，12 个服务、1 个私有 default network、6 个 `sentinelops_e2e_*` volume；只发布 loopback 端口 |
| Builder versions | `rg -n '^FROM (golang:1\.27\.0|node:24\.19\.0)' manifest/docker/Dockerfile.*` | `PASS`；backend/API、Worker e2e、provider-double、migrate、frontend 均命中 |
| Image/version contract | 上述 `SENTINELOPS_CI_GOPROXY=... scripts/ci/image-contract.sh` | `PASS`；API、Worker、frontend、migrate、provider-double、nginx 均构建并进入 inventory |
| Migrate tool | `docker run --rm sentinelops-e2e-migrate -version` | `PASS`：`goose version: v3.27.3` |
| Frontend lane | `scripts/ci/pr.sh --lane frontend` | `PASS`；`npm ci`、lint、build 退出 0；0 error、67 条既有 warning |
| Redactor compile | `go test ./scripts/ci -count=1` | `PASS`（`[no test files]`，只计 package 编译） |
| Go formatting | `goimports -l` 对全部 P41 Go 文件 | `PASS`；无输出 |
| Whitespace | `git diff --check` | `PASS` |
| Missing provider references | 清空四个 Eval 环境引用后运行 `scripts/ci/provider-eval.sh` | `NOT RUN`；退出 0，`status.json` 只记录缺失引用名称，不含值 |

诊断性运行 `go test ./... -count=1` 曾在 Compose/MySQL 启动前执行；依赖数据库的 package 因未提供 `SENTINELOPS_TEST_DSN` 失败。该命令不属于 P41 局部门禁，结果记为 `NOT RUN`，不冒充产品回归失败或 PASS。

## Unfinished / NOT RUN

- `scripts/ci/pr.sh --lane backend`、完整 `go test -race ./...`、完整 staticcheck/govulncheck Hosted 结果：`NOT RUN`；workflow 已定义，按计划不以本地 P41 局部结果冒充 Hosted 硬门禁。
- `scripts/ci/integration.sh` 的完整 Compose 启动、migration、代表数据库集成和 P38 Playwright 串联：`NOT RUN`；本单元实际完成 Compose isolation、完整所需镜像构建和代表性故障测试。
- provider 四类在线 preflight、P39/P40 production Runtime Eval、40+ Dataset 三轮、baseline compare：`NOT RUN`；未提供 SecretRef/API/MySQL 环境。
- Hosted PR/integration/provider workflows、Artifact 上传、发布、回滚演练：`NOT RUN`；未 push。
- P42/P43：`NOT RUN`；达到可进入 P42 的条件后停止，不实施后续单元。

## Raw Artifact And Secret Boundary

- 本地构建 inventory 位于被忽略的 `.artifacts/p41/`，不随提交进入仓库；本文件只记录非敏感标签、计数和内容摘要。
- provider 原始输出只允许存在于 `mktemp` 目录，进入持久化 Artifact 前经过 P06 Redactor；cleanup 删除临时目录。
- 未记录 Secret、完整 DSN、Authorization、Cookie、Token、模型输入或供应商响应正文。

## Rollback

通过本单元独立本地提交的普通反向提交恢复。运行中的局部门禁没有启动或清理其它 Compose project，没有修改 remote、共享数据库、生产数据卷或发布状态。
