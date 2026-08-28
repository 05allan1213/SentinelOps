# P43 最终收口验证

- Status: `NOT COMPLETE — NOT RUN`
- 学习项目功能验收：`PASS`
- 二改实施：`结束`
- Verified application candidate: `b76c14330faad412e4ad31c0d4e058f7f2ba3753`（`main`，未 push）
- Completed: 2026-08-28（Asia/Shanghai）
- Spec references: 实施计划 P43、`/home/monody/project/P43.md`、仓库 `AGENTS.md`
- Commit: 本文件不自引用报告提交；完整 SHA 由仓库外实施计划 P43 台账回填

## 最终结论

- `学习项目功能验收：PASS`：当前应用候选可由缓存构建出 migrate/API/Worker/frontend 镜像，唯一隔离 Compose 栈可完成 Migration、启动、健康等待、HTTP 访问与清理；精简核心流程验收通过。
- `P43 计划状态：NOT COMPLETE — NOT RUN`：完整 40 Case x 3 Runtime Eval、approved baseline compare、Hosted CI、真实灰度和回滚未执行，因此不声明生产级 `COMPLETE`。
- `二改实施：结束`：按用户最终决策停止 P43，不再继续后续开发或验证单元。

## Boundary Audit

- 目标：确认最终应用候选可构建、可启动、核心功能可用，保存最终证据并清理唯一 `sentinelops-final` project。
- 明确非目标：不切换模型、不调整 Eval 数据集、不执行 40 x 3 Eval、不运行 Hosted CI、不 push、不执行灰度或回滚、不扩展功能或架构。
- 兼容契约：复用既有 Compose、P38 浏览器链、P39/P40 Runtime Eval Adapter、P41 故障矩阵、唯一 `workflow.GORMStore`、`RuntimeHandler` 与官方 Eino Agent/Runner/Checkpoint 能力。
- 安全不变量：Approval/RBAC、Effect Ledger 去重、generation fence、Evidence citation、MCP SSRF/allowlist、Skill read-only 和九 Gate 断言不放宽；Artifact 不记录 Secret、完整 DSN、Token、模型输入或供应商响应正文。
- 实际源码改动：仅收紧 `manifest/test/provider-double` 的 unknown 故障夹具识别并增加回归测试；生产 Runtime、模型配置、Eval Dataset 和安全断言未修改。
- 回滚：对独立夹具修复提交做普通反向提交；报告提交只含本文件。未修改 remote、共享服务、生产数据或发布状态。

## Build-or-Reuse

| 现有能力 | 复用结论 | 本轮最小缺口 |
| --- | --- | --- |
| P38 real Compose Approval/Effect/SSE 链 | 直接复用两份 Playwright spec 和 `sentinelops-final` 合并配置 | 修复 provider-double 把嵌套消息中的 incidental `unknown` 误判为故障 Case |
| P39/P40 production Runtime Eval | 复用真实 Provider 代表 8 Case 的 MySQL truth 报告 | 不重复统计型 Eval；仅复用未受后续 Eval-only 提交影响的 8/8 证据 |
| P41 镜像、集成和故障矩阵 | 复用 Dockerfile、Compose 与 18 Case 矩阵 | 在最终应用候选上重新缓存构建全部镜像并重建角色 |
| Eino / Eino-ext | 继续使用官方 AgentTool、Runner、Checkpoint、MCP 和 Skill middleware | 无新 Adapter、Loop、Store、Registry 或协议实现 |

## 最终应用候选与镜像

在干净工作树 `0e2e1f9488e14161ce1f7ffc50d60420bca5113b` 开始本次收口。live smoke 暴露测试 Provider double 误判后，以独立提交
`b76c14330faad412e4ad31c0d4e058f7f2ba3753` 修复并作为最终应用候选。

```bash
COMPOSE_PROJECT_NAME=sentinelops-final docker compose -p sentinelops-final \
  -f manifest/docker/docker-compose.yml \
  -f manifest/docker/docker-compose.test.yml \
  build --progress=plain --build-arg GOPROXY=https://goproxy.cn,direct \
  migrate api worker frontend provider-double nginx
```

- Result: `PASS`；使用 BuildKit 缓存，没有 `--no-cache`，全部六类应用镜像构建成功。
- Builder: Go `1.27.0`，Node `24.19.0`，goose `v3.27.3`。
- 下表是本地内容寻址 image ID，不冒充 registry digest 或已发布 Artifact。

| Service | Local image ID |
| --- | --- |
| API | `sha256:300222442ff6a9c2c1377e35e2b687066a58ab37c60025af3f8116dbb6a8a5d6` |
| Worker | `sha256:3ec1d83abecf6821c13eddd289f6cf329be2e3243ebe9b2853ee103815c8444b` |
| frontend | `sha256:78b59a859ad5584cb331f5068a0ff098271cd08c04a0dbd8899a9aa1d1acbfbc` |
| migrate | `sha256:6c4a65e5483fb33a4a675b7813c4ec0a0910291ffc97054b1ed4ad77ae8b8cca` |
| provider-double | `sha256:35e46daba114ef05bcc5cead868cf01ada6d88e5b688b10e2d9876744a0e7cc2` |
| nginx | `sha256:d8ad948225dcaeceb7192a10d3d588f9b9342e1b13176048fea573fabb4743d9` |

## Compose 启动、健康与清理

```bash
SENTINELOPS_E2E_HTTP_PORT=18080 SENTINELOPS_E2E_MYSQL_PORT=13306 \
COMPOSE_PROJECT_NAME=sentinelops-final docker compose -p sentinelops-final \
  -f manifest/docker/docker-compose.yml \
  -f manifest/docker/docker-compose.test.yml up -d --wait

# 最终 tag 构建后强制重建当前角色
docker compose -p sentinelops-final -f manifest/docker/docker-compose.yml \
  -f manifest/docker/docker-compose.test.yml \
  up -d --wait --force-recreate api worker nginx

curl --fail --silent --show-error http://127.0.0.1:18080/
```

- Result: `PASS`。API、Worker、frontend、nginx、MySQL、Redis、Milvus、etcd、MinIO、provider-double 均完成 Compose 健康等待；migrate 与 runtime-gates 按一次性 Job 语义退出 0。
- HTTP 根路径可达；服务、端口、网络和卷只属于显式 project `sentinelops-final`。

```bash
docker compose -p sentinelops-final -f manifest/docker/docker-compose.yml \
  -f manifest/docker/docker-compose.test.yml down -v --remove-orphans
docker ps -a --filter label=com.docker.compose.project=sentinelops-final
docker network ls --filter label=com.docker.compose.project=sentinelops-final
docker volume ls --filter label=com.docker.compose.project=sentinelops-final
```

- Cleanup: `PASS`；容器、网络、卷均零残留。未停止或删除其它 Compose project、容器或卷。

## 精简真实功能验收

| 功能 | 证据 | Result |
| --- | --- | --- |
| 普通对话 / SSE | 真实 Provider 代表 Case `routing-conversation-success`；当前树 `TestAPIWorkerFocusedIntegration` | `PASS` |
| DeepThinking / Plan / Ops 查询 | `plan-01-success` 实际调用 `event_analysis_agent,get_current_time,query_events`；Plan/Eino AgentTool 与 Ops read-only 聚焦测试 | `PASS` |
| RAG 引用 | `evidence-rag-success` 实际调用 `query_internal_docs`，canonical Evidence 有效；Evidence integrity/injection 测试 | `PASS` |
| HITL 批准 / Effect 防重复 / SSE reconnect | 当前隔离栈 Playwright：Approval 后 Resume，Primary/Derived Effect 各一次，终态重连不再调用模型或 Effect | `PASS` |
| HITL 拒绝 / RBAC / CAS | viewer 403、hash mismatch 409、重复批准只产生一个事件、显式拒绝产生零 Effect | `PASS` |
| unknown Effect | 外部结果未知后保持 parked；只允许 admin 接受不确定性并进入 canceled 终态 | `PASS` |
| Resume / Replay | `recovery-01-resume-success`；当前树官方 Runner/Checkpoint 聚焦测试 | `PASS` |
| MCP read-only | `mcp-01-readonly-success` 实际调用 `context7__resolve-library-id,mcp_agent`；官方 SDK、SSRF/allowlist 聚焦测试 | `PASS` |
| Skill read-only | `skill-01-readonly-success` 实际调用 `skill,skill_agent`；只读 Backend 拒绝 Mutation/Execution | `PASS` |

真实 Provider 代表报告为 8/8、零重试、零安全违规：Task Success、Tool Selection、Tool Call Success 均为 100%。该报告运行于 `6507134f90faefbc9056faa2684640056aa96c3b`，其中已包含 Approval attempt 收口；其后至 `0e2e1f9` 只修改 `internal/ai/eval` 三个文件，本轮 `b76c143` 只修改测试 Provider double，故核心 Runtime 证据可复用。它不是 40 x 3 Eval，也不作为完整 Eval 的替代品。

## 本轮实际命令与结果

| Gate | Command summary | Result |
| --- | --- | --- |
| Provider double regression | `go test ./manifest/test/provider-double -count=1` | `PASS` |
| Current live browser chain | `npx playwright test tests/e2e/approval-flow.spec.ts tests/e2e/unknown-effect.spec.ts --project=chromium --workers=1` | `PASS`：4/4 |
| Runtime / Workflow focused | `go test ./internal/ai/runtime ./internal/ai/workflow -run '<focused cases>' -count=1`，使用同一隔离 MySQL 的专用 `sentinelops_p03` 库 | `PASS` |
| Core contracts | Plan、DeepThinking、RAG、Ops、MCP、Skill 七包聚焦测试 | `PASS` |
| Final images | 上述六类 Compose build | `PASS` |
| Final startup / health / HTTP | `up -d --wait`、角色强制重建、HTTP root | `PASS` |
| Final cleanup | `down -v --remove-orphans` + label inventory | `PASS`：零残留 |

首次浏览器运行是 3 PASS / 1 FAIL：普通封禁夹具被 provider-double 的宽泛 `unknown` marker 路由到 `webhook_out`。生产 Runtime 未修改；修复把识别收紧到显式“unknown/未知 + 外部 Effect”意图，单测与 live 4/4 重跑均 PASS。

首次 Runtime 聚焦命令被测试防误操作护栏拒绝，因为 DSN 使用 `sentinelops_e2e` 而不是允许的 `sentinelops_p03`。该次未执行产品断言；在同一 throwaway MySQL 内创建专用空库并重跑后两包 PASS，护栏未修改。

## 复用的既有 P43 PASS 证据

| Domain | Reused result | Artifact |
| --- | --- | --- |
| 最终静态/宽回归（`0e2e1f9`） | gofmt、vet、staticcheck、govulncheck、`go test -race ./...` PASS | `.artifacts/implementation/P43/p43.2/` |
| Migration 双夹具 | 空库与 current-schema 等价 fixture PASS | `.artifacts/implementation/P43/p43.4/empty/`、`p43.4/current/` |
| 前端 / 浏览器全回归 | npm ci/lint/build PASS；Playwright 13/13 PASS | `.artifacts/implementation/P43/p43.3/` |
| Durable / 安全集成 | contracts、focused integration、九 Gate PASS | `.artifacts/implementation/P43/p43.5/` |
| 故障矩阵 | 18/18：7 Resume、3 Replay、8 `PARKED AS DESIGNED` | `.artifacts/implementation/P43/fault-matrix/` |
| Provider preflight | 文本生成、Tool Calling、2048 维 Embedding、Rerank 4/4 PASS | `.artifacts/implementation/P43/p43.7/provider-preflight-local-fixed.log` |
| 代表真实 Runtime | 8/8、零重试、零安全违规 | `.artifacts/implementation/P43/candidate-6507134f90fa-direct/run/rep8-qwen36flash-round2.json` |

## NOT RUN 与遗留限制

| Gate | Status | Reason |
| --- | --- | --- |
| 完整 40 Case x 3 Runtime Eval | `NOT RUN` | 用户明确决定不再执行；历史部分运行不能替代完整报告 |
| Approved baseline compare | `NOT RUN` | 没有独立批准的 baseline，不生成或伪造 approved baseline |
| Hosted PR / integration / provider Eval | `NOT RUN` | 未授权 push；本次不修改远端配置 |
| Registry push / registry digest | `NOT RUN` | 只有本地内容寻址镜像，不声明已发布镜像 |
| 灰度、真实切流与回滚演练 | `NOT RUN` | 用户明确排除生产发布门禁；未触碰共享或生产环境 |
| Contract Migration / legacy 删除 | `NOT RUN` | P42 审计为 no-op，仍保留 reader/审计/回滚用途 |

上述必需项存在 `NOT RUN`，所以 P43 计划状态只能是 `NOT COMPLETE — NOT RUN`。学习项目核心功能可用不等于生产发布就绪，也不改变这一判定。

## Raw Artifact References

- 最终 SHA 镜像、Compose、HTTP 与清理：`.artifacts/implementation/P43/final-b76c14330faa/`
- 当前树精简功能与夹具修复前后证据：`.artifacts/implementation/P43/final-0e2e1f9488e1/`
- Artifact 目录被 `.gitignore` 排除；提交只包含脱敏摘要，不包含 Secret、完整 DSN、Authorization、Cookie、Token、模型输入或供应商原始响应。
