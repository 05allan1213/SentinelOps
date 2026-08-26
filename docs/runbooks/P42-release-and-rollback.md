# P42 Durable Runtime 发布与回滚 Runbook

本 Runbook 只定义可执行机制和检查点。P42 不执行真实灰度、切流、回滚、legacy 删除或 Contract Migration；这些动作必须在 P43 具备真实环境授权和证据后逐步执行。

## 固定约束

- 两个镜像变量都必须是不可变 digest：`SENTINELOPS_CURRENT_IMAGE` 与 `SENTINELOPS_COMPATIBILITY_IMAGE`。对应 `*_RUNTIME_VERSION` 必须精确等于镜像引用的 `sha256:<digest>` 部分；生产 Runtime 对 `development`、tag 或非法值直接拒绝启动。
- 仓库内 `config.yaml`、`config.docker.yaml` 的九项静态 cap 全部默认关闭。发布 overlay 只读挂载两份完整替换配置；必须记录其 SHA-256，且 current API/Worker 共用同一份配置。不得靠直接改容器内文件开启能力。
- 所有 Gate 修改必须通过 admin `POST /api/settings/v1/runtime-gates` 提交完整九项 `dynamic_caps` 和非空 `reason`；不得直接更新共享数据库。
- 生效值始终为 `frozen AND current static cap AND current dynamic switch`。动态或静态值只能关闭历史 Run 的能力，不能重新打开 frozen=false 的能力。
- `shadow_mode` 当前或 frozen 为 true 时，L1/L2 都关闭。Shadow Run 不能原地晋升为写 Run，必须在新 Gate 时刻创建新 Run。
- Worker 只认领与自身 exact `runtime_version` 相同的 Run；应用回退不执行 Schema Down。
- 每一步只有在“进入条件”全部满足后才能执行；命中“停止条件”立即停在当前检查点并按“恢复判定”处理。

## 发布前命令

```bash
export SENTINELOPS_CURRENT_IMAGE='registry.example/sentinelops@sha256:<current-digest>'
export SENTINELOPS_COMPATIBILITY_IMAGE='registry.example/sentinelops@sha256:<compatibility-digest>'
export SENTINELOPS_CURRENT_RUNTIME_VERSION='sha256:<current-digest>'
export SENTINELOPS_COMPATIBILITY_RUNTIME_VERSION='sha256:<compatibility-digest>'
export SENTINELOPS_CURRENT_CONFIG='/absolute/path/current-config.yaml'
export SENTINELOPS_COMPATIBILITY_CONFIG='/absolute/path/compatibility-config.yaml'

[[ "${SENTINELOPS_CURRENT_IMAGE##*@}" == "${SENTINELOPS_CURRENT_RUNTIME_VERSION}" ]]
[[ "${SENTINELOPS_COMPATIBILITY_IMAGE##*@}" == "${SENTINELOPS_COMPATIBILITY_RUNTIME_VERSION}" ]]
[[ "${SENTINELOPS_CURRENT_RUNTIME_VERSION}" =~ ^sha256:[0-9a-f]{64}$ ]]
[[ "${SENTINELOPS_COMPATIBILITY_RUNTIME_VERSION}" =~ ^sha256:[0-9a-f]{64}$ ]]
test -f "${SENTINELOPS_CURRENT_CONFIG}"
test -f "${SENTINELOPS_COMPATIBILITY_CONFIG}"
sha256sum "${SENTINELOPS_CURRENT_CONFIG}" "${SENTINELOPS_COMPATIBILITY_CONFIG}"

docker compose \
  -f manifest/docker/docker-compose.yml \
  -f manifest/docker/docker-compose.release.yml \
  config
```

初始动态向量必须完整关闭：

```json
{
  "dynamic_caps": {
    "agent_runtime.enabled": false,
    "agent_runtime.accept_new_runs": false,
    "agent_runtime.shadow_mode": false,
    "agent_runtime.l1_writes": false,
    "agent_runtime.l2_writes": false,
    "agent_runtime.admin_query_database_debug": false,
    "mcp.enabled": false,
    "skill.enabled": false,
    "langfuse.enabled": false
  },
  "reason": "release checkpoint: initialize all runtime gates closed"
}
```

每次修改后必须读取 `GET /api/settings/v1/runtime-gates` 和 `GET /api/settings/v1/runtime-gates/audit?limit=20`，确认 `static_caps`、`dynamic_caps`、`current_effective` 与审计 actor/reason/time 完整一致。

## 发布步骤 ①～⑫

### ① 框架升级且行为不变

- 进入条件：候选提交已通过当前单元局部门禁；Eino/Eino-ext 锁定版本、Go toolchain、镜像 digest、`runtime_version` 与完整配置 SHA-256 可查询且逐项匹配。
- 动作：部署只包含框架升级、Gate 仍全部关闭的 current 镜像；不创建 durable Run。
- 观测：启动日志、健康检查、legacy 只读/API 基线、错误率与资源曲线无行为漂移。
- 停止条件：依赖版本、回调生命周期、cancel/checkpoint 行为与证据不一致。
- 恢复判定：保持 Gate 全关并回到 compatibility 镜像；确认没有新 durable Run 后才重试。

### ② 执行 Expand Migration 并标记 legacy

- 进入条件：Migration artifact 已在一次性副本验证 Up；当前迁移仅为 Expand，绝不包含 Contract 删除。
- 动作：使用 pinned goose 执行 Expand Up，并确认 legacy Run/Checkpoint/Ops 数据仍可读。
- 观测：schema version、索引、`runtime_mode`、legacy reader 与 migration audit。
- 停止条件：任何 Down、列/表删除、legacy reader 失败或不可逆数据改写。
- 恢复判定：停止应用推进；保留已扩展 Schema，修复 forward，不执行 Schema Down。

### ③ 停止旧 Run、drain/审计终态化并核对 claim

- 进入条件：旧入口可停止接收新任务；持 lease Worker 支持 recursive safe-point cancel。
- 动作：关闭旧入口，向旧 Worker 发正常终止信号，等待 Resume/Replay/parked 收敛；只使用既有应用 primitive 终态化，不直接改表。
- 观测：按 `runtime_mode,status,runtime_version` 聚合 Run；核对 active lease、waiting approval、unknown Effect、Checkpoint 与 Session lock。
- 停止条件：仍有未识别 owner、重复 claim、无 Checkpoint 的有副作用 Run，或终态证据缺失。
- 恢复判定：继续使用匹配版本 Worker 收敛；无法证明安全的 Run 保持 parked，禁止强行解锁 Session。

### ④ 部署 API/Worker，保持 `agent_runtime.enabled=false`

- 进入条件：①～③通过；current/compatibility digest、对应 `runtime_version` 和只读完整配置 SHA-256 已复核；current 配置的九项静态 cap 与本次发布批准范围一致。
- 动作：用 release overlay 部署 current API/Worker，但完整动态向量保持全关。
- 观测：API/Worker 健康；`current_effective` 全 false；`workflow_runs` 无新增 durable Run；Worker 无 claim。
- 停止条件：Gate 全关仍创建/认领 Run、建立 MCP/Skill/Langfuse 资源或执行 Effect。
- 恢复判定：立即停止 current Worker，回 compatibility 镜像；保留 Schema 和全部审计证据。

### ⑤ Shadow/只读对照

- 进入条件：④稳定；静态 cap 允许 `enabled/accept_new_runs/shadow_mode`；测试身份和对照采样规则已批准。
- 动作：设置 `enabled=true`、`accept_new_runs=true`、`shadow_mode=true`，L1/L2/MCP/Skill/admin query/Langfuse 维持 false；只对白名单入口放行。
- 观测：新 Run frozen snapshot 含 shadow=true、L1/L2=false；新旧输出可对照；业务 Mutation 和 Effect 数量均为 0。
- 停止条件：出现任一 Effect/业务写、非白名单 Run、恢复 hash 漂移或 trace 缺失。
- 恢复判定：关闭 `accept_new_runs`，drain Shadow Run；修复后创建全新 Shadow Run，不复用旧 snapshot 晋升。

### ⑥ 白名单用户

- 进入条件：⑤零 Mutation 且对照质量达标；网关/鉴权层白名单可审计。
- 动作：在外部入口逐批扩大用户白名单；九项 Gate 不因白名单扩大而改变。
- 观测：按用户、Run、错误、延迟、成本和 Evidence 完整度分组；非白名单请求不创建 Run。
- 停止条件：越权、跨用户数据、不可解释差异、预算或延迟越界。
- 恢复判定：收缩入口白名单并关闭 `accept_new_runs`；已有 Run 按原 frozen snapshot 收敛。

### ⑦ 只读 MCP/Skill

- 进入条件：⑥稳定；MCP Catalog 与 Skill 内容 hash 已冻结；外部端点/文件目录均为只读。
- 动作：分别开启 `mcp.enabled`、`skill.enabled`，每次只改一项完整向量并保留 shadow=true。
- 观测：Session/Backend 仅在 Gate 通过后创建；每次远端 Tool 或 Skill List/Get 前重验；无写 Tool、路径逃逸或资源泄漏。
- 停止条件：Gate 关闭后仍新建 Session/Backend/查询，或 Catalog/hash 与 snapshot 不符。
- 恢复判定：关闭对应 Gate；已建资源按 Attempt 结束关闭，旧 frozen=false Run 不得因重开动态 Gate 获得能力。

### ⑧ L1 提案与审批

- 进入条件：⑦稳定；审批 RBAC、proposal hash、checkpoint fingerprint、Effect Ledger 告警均可观测。
- 动作：关闭 shadow，开启 `l1_writes=true`、保持 `l2_writes=false`；仅为新白名单 Run 提交 L1 Proposal。旧 Shadow Run 永不晋升。
- 观测：Approval pending/approved 与 exact Checkpoint 绑定；Resume 和每个 endpoint 前 Gate 均为 true；Effect 与业务写一一对应。
- 停止条件：已批准请求在 Gate 关闭后仍写、重复 Effect、无 Ledger 写、审批身份或 hash 不匹配。
- 恢复判定：先关闭 `accept_new_runs` 与 `l1_writes`；未执行批准保持无 Effect，执行中按 safe-point/ledger 语义收敛。

### ⑨ 测试环境/资产白名单 L2

- 进入条件：⑧稳定且 L1 无 unknown；L2 仅面向隔离环境和明确资产白名单。
- 动作：在完整向量中开启 `l2_writes=true`；生产资产仍由外部策略拒绝。
- 观测：每个 L2 有双重审批要求、Effect key、endpoint 发送边界和 reconciliation 证据。
- 停止条件：非白名单资产、unknown 无法 parked、外部 endpoint 结果与 Ledger 不一致。
- 恢复判定：关闭 `accept_new_runs/l2_writes`，停止新发送；unknown 保持 parked 并走证据化 admin disposition。

### ⑩ 新 Runtime 默认后删除旧 Ops 写入口

- 进入条件：P43 对默认流量、回滚和观察期均给出 PASS；legacy Ops 写调用者已为 0。
- 动作：以后续独立变更删除旧 Ops 写入口；保留 legacy 读、审计和 rollback 所需对象。
- 观测：调用图与运行指标均无 legacy write；durable 写全部经 Approval/Effect。
- 停止条件：仍有 scheduler/controller/人工流程依赖旧写入口。
- 恢复判定：不删除；继续保持现有 nil/deny Gate。P42 不执行本步。

### ⑪ 观察期结束后删除旧 Graph，保留共享检索

- 进入条件：计划要求的 Task 11 指标达标、观察期结束、P43 rollout/rollback 演练 PASS。
- 动作：以后续独立变更删除旧 Graph 编排，保留已审计的共享 retrieval 能力。
- 观测：入口、依赖图、Eval、恢复样本与 shared retrieval 回归。
- 停止条件：任何仍在运行/恢复的 legacy reader 或 Graph 依赖。
- 恢复判定：不删除并延长观察期。P42 不执行本步。

### ⑫ Contract Migration

- 进入条件：legacy reader/Worker 为 0；非终态 legacy Run 为 0；未决恢复依赖为 0；观察期和 P43 全部 PASS；迁移已在一次性 post-cutover Schema 副本验证。
- 动作：仅对审计确认确需清理的对象执行独立、带 precondition 的 Contract Migration。
- 观测：precondition query、Schema diff、应用读写、备份/恢复证据。
- 停止条件：任一 precondition 非 0、迁移混入 Expand Up、需要 Schema Down 才能回滚。
- 恢复判定：不执行 Contract。当前 P42 审计为 no-op，因此没有 `00007` 空迁移，P42/P43 均不得伪造执行记录。

## 回滚步骤 ①～⑥

### ① 关闭 `accept_new_runs/l1_writes/l2_writes`

- 进入条件：发现发布停止信号或人工批准回滚。
- 动作：一次 POST 提交完整九项向量，至少将上述三项设为 false；高风险事件同时关闭 MCP/Skill/Langfuse/admin query。
- 观测：GET 与 audit 显示新值；新 Run 数和新 Effect 数立即归零。
- 停止条件：设置事务失败、审计缺失或任一当前 effective 仍为 true。
- 恢复判定：停止后续操作，保持入口封禁；修复 settings 事务后重试完整向量。

### ② 对持 lease Worker 发 recursive safe-point cancel

- 进入条件：①已确认；当前 lease owner 清单已保存。
- 动作：正常停止 current Worker，例如 `docker compose ... stop -t 300 worker`，由 Worker 触发官方 recursive safe-point cancel。
- 观测：新 Checkpoint generation 与 lease 对齐；Run 进入 pending/retryable/parked/terminal；无 endpoint 在 Gate 关闭后启动。
- 停止条件：lease 丢失后仍写、Checkpoint generation 不匹配、取消超时且副作用边界未知。
- 恢复判定：保持进程停止；将不确定 Run parked，禁止直接重置 lease 或 Session lock。

### ③ 收敛 waiting approval、unknown Effect 与不兼容 Run

- 进入条件：②完成；状态清单和对应 runtime_version 已导出。
- 动作：让匹配版本 Worker 处理可安全 Resume/Replay 项；waiting approval、unknown Effect、runtime incompatible 项保持或进入 parked，并使用既有 admin API 提交证据化处置。
- 观测：Approval 不被改写；unknown 不显示成功；park reason、Effect evidence、Checkpoint 均可查询。
- 停止条件：需要猜测外部发送结果、覆盖审批决定或手工改数据库才能继续。
- 恢复判定：继续 parked 并升级人工审查；不以“恢复服务”为由伪造终态。

### ④ 回退应用但不 Down Schema

- 进入条件：①～③安全收敛；compatibility digest 已验证。
- 动作：将 `SENTINELOPS_CURRENT_IMAGE`、`SENTINELOPS_CURRENT_RUNTIME_VERSION` 与 `SENTINELOPS_CURRENT_CONFIG` 一并切到 compatibility 三元组，重新部署 API/Worker；保留 Expand Schema，禁止只换镜像而沿用另一版本身份或配置。
- 观测：健康检查、legacy 读、Schema version、审计/Checkpoint/Effect 数据完整。
- 停止条件：旧应用不能读取 Expand Schema或尝试执行 Down。
- 恢复判定：重新部署能读当前 Schema 的最后已知版本；只做 forward fix。

### ⑤ 旧版本不得 Resume 新 Checkpoint

- 进入条件：④完成。
- 动作：核对旧 Worker exact `runtime_version` claim；保持新版本 Run parked 或无人认领。
- 观测：按 `runtime_version,status,lease_owner` 查询，旧 Worker claim 的 Run 版本必须与镜像完全一致。
- 停止条件：跨版本 claim、Resume 或 compatibility hash 不一致仍继续执行。
- 恢复判定：立即停止该 Worker；修复版本条件后再启动，不删除 Checkpoint。

### ⑥ 用匹配版本 Worker 恢复或显式迁移

- 进入条件：新版本 Run 的 frozen snapshot、Checkpoint 与 Effect 证据完整。
- 动作：把回滚前 current 的 image digest、`runtime_version` 和完整配置作为 compatibility 三元组，仅启动 `compatibility-worker` profile 处理其 exact runtime_version；若没有经批准的 admin 迁移 primitive，则保持 parked。
- 观测：只有匹配版本 Run 被 claim；Resume/Replay 选择、Approval 与 Effect 仍受当前 deny-only Gate。
- 停止条件：需要跨版本反序列化、手工改 hash、删除 Checkpoint 或绕过 Effect Ledger。
- 恢复判定：停止兼容 Worker并保持 parked；提交最小 ADR/迁移设计，不能在事故中即兴改 Schema 或状态。

## 完成判定

只有十二个发布检查点或六个回滚检查点各自留下进入条件、观测结果、停止判定和恢复结论，才能称对应演练完成。P42 仅证明机制与文档可执行；真实执行结果属于 P43。
