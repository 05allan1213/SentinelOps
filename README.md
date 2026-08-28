# SentinelOps

[![CI](https://github.com/05allan1213/SentinelOps/actions/workflows/pr.yml/badge.svg)](https://github.com/05allan1213/SentinelOps/actions/workflows/pr.yml) [![Go 1.27.0](https://img.shields.io/badge/Go-1.27.0-00ADD8?logo=go)](https://go.dev/) [![Eino v0.9.15](https://img.shields.io/badge/Eino-v0.9.15-6f42c1)](https://github.com/cloudwego/eino)

面向安全运营的 Agent Runtime 与响应平台：基于 Cloudwego Eino，把 Agent 的规划、执行、暂停审批、恢复重放和副作用落账组织成一条可追踪、可校验的工程链路。

> 不只回答“模型说了什么”，还记录“运行到哪一步、谁批准了什么、哪些副作用已经发生”。

**项目定位**：把 Eino Agent 放进一个可恢复、可审计、可治理的 Runtime。核心实现集中在 Eino Plan–Execute–Replan、MySQL Durable Runtime、RBAC + HITL + Effect Ledger，以及 Evidence RAG + MCP/Skill。

| Durable Agent Runtime | HITL + Effect Ledger | Evidence RAG + MCP + Skill |
| --- | --- | --- |
| Eino Plan–Execute–Replan、专业 AgentTool、MySQL Run/Checkpoint，以及 generation-fenced lease。 | RuntimeHandler 统一接入 RBAC、Policy、Gate、Approval 生命周期和幂等 Effect。 | Scope-aware 混合检索输出 Evidence；MCP 只读目录与 Skill Pipeline 复用 Runtime 的 Policy、Budget、Trace。 |

~~~mermaid
flowchart LR
  UI["Web Console"] --> API["GoFrame API"]
  API --> ROUTER["Intent Router + Agent Registry"]
  ROUTER --> SUBAGENTS["Chat / Event / Report / Risk / Solve / Intelligence / Ops"]
  SUBAGENTS --> RAG["Evidence RAG"]
  API --> RUN["Durable Run API"]
  RUN --> WORKER["Worker"]
  WORKER --> RT["RuntimeHandler: Gate / Policy / Budget / Trace"]
  RT --> PLAN["Eino Plan → Execute → Replan"]
  PLAN --> TOOLS["Specialist AgentTool: MCP / Skill"]
  PLAN --> RAG
  RT --> HITL["HITL Approval"]
  RT --> EFFECT["Effect Ledger"]
  RT --> STORE["GORMStore: Run / Event / Checkpoint"]
  STORE --> MYSQL[("MySQL")]
  RAG --> MILVUS[("Milvus")]
  RAG --> REDIS[("Redis semantic cache")]
~~~

上图是首屏摘要；下面的 C4 Context/Container、核心流程和能力边界会展开部署边界、数据流以及实际代码约束。

---

## 目录

- [项目简介](#项目简介)
- [能力边界](#能力边界)
- [系统架构](#系统架构)
  - [C4 System Context](#c4-system-context)
  - [C4 Container](#c4-container)
  - [部署形态](#部署形态)
- [核心流程](#核心流程)
- [核心能力](#核心能力)
  - [Agent 编排](#1-agent-编排)
  - [RAG 检索](#2-rag-检索)
  - [知识库与文档索引](#3-知识库与文档索引)
  - [告警接入与订阅调度](#4-告警接入与订阅调度)
  - [Durable Runtime、HITL 与 Effect](#5-durable-runtimehitl-与-effect)
  - [安全、策略与可靠性](#6-安全策略与可靠性)
  - [可观测性与 RAG Eval](#7-可观测性与-rag-eval)
  - [记忆、Skill 与 MCP](#8-记忆skill-与-mcp)
  - [Web Console](#9-web-console)
- [技术栈](#技术栈)
- [快速开始](#快速开始)
- [部署](#部署)
- [验证](#验证)
- [仓库结构](#仓库结构)
- [当前边界与已知限制](#当前边界与已知限制)
- [贡献](#贡献)

## 项目简介

SentinelOps 面向需要持续接收安全事件、查询内部知识、生成研判结论并留下审计证据的安全运营场景。代码把在线请求和后台执行拆成两个边界：

1. **标准模式**：请求进入 `ExecuteIntent`，由 `START → Router → Executor → END` 图识别意图，再从 Registry 调用对应 SubAgent；
2. **深度思考/持久化模式**：API 只创建不可变 Run，Worker 认领并执行官方 `planexecute`，中途把事件、checkpoint、审批和效果写入 MySQL。

这两条路径共享模型路由、工具注册、RAG、权限和 Trace 能力，但职责边界不同。当前业务意图 SubAgent 为七个：Chat、Event、Report、Risk、Solve、Intelligence、Ops；Summary 是独立的摘要压缩流水线，Plan 是深度模式的外层编排器。

### 能力状态标记

| 标记 | 含义 |
| --- | --- |
| 默认可运行 | 仓库提供了对应代码路径和本地启动/Compose wiring；实际运行仍需要通过配置校验。 |
| 需外部依赖/凭据 | 需要模型 Provider、Milvus、Redis、MySQL、Tavily、Langfuse、SMTP、Webhook 或 MCP 等外部资源。 |
| 受 Gate/部署条件约束 | 代码会显式检查环境、Gate、Policy、Approval、Worker 或生产部署条件。 |

## 能力边界

| 能力 | 状态 | 当前实现边界 |
| --- | --- | --- |
| 标准聊天与七类意图路由 | 需外部依赖/凭据 | Router 使用 `routing.chat.default`；置信度低于 `0.70`、解析失败或未知意图时降级为 Chat。 |
| 深度思考与 durable Run | 受 Gate/部署条件约束 | 生产 Worker 当前只接受 `plan_agent`；`agent_runtime.enabled` 与 `accept_new_runs` 必须有效。 |
| RAG 混合检索与 Rerank | 需外部依赖/凭据 | 依赖 Embedding、Milvus、Redis；混合检索失败时回退到 dense。 |
| 知识库索引 | 需外部依赖/凭据 | Parser 明确实现 PDF、DOCX、Markdown 和 Go/Python/Java；上传控制器额外接受 TXT/PPTX，但当前 Parser 没有对应实现，不能承诺这两类文件索引成功。 |
| 告警接入 | 默认可运行 | Webhook、CEF、LEEF、API Push 统一归一化、去重并异步写入向量索引；接入端点使用独立的 `X-API-Key`。 |
| AI 运维动作 | 受 Gate/部署条件约束 | `update_event_status`、`create_report`、`save_intelligence`、`block_ip` 和通知动作经过 RuntimeHandler、Policy、Approval、Effect 与 Gate。 |
| Langfuse、Tavily、SMTP、DingTalk、WeCom | 需外部依赖/凭据 | 只有配置了相应端点和 SecretRef 才能形成完整链路。 |
| L1/L2 写入 | 受 Gate/部署条件约束 | 默认配置是 `shadow_mode: true`；effective Gate 会关闭 L1/L2 writes，即使静态配置中的开关为 true。 |

## 系统架构

### 启动角色

同一个 Go 二进制由 `main.go` 解析角色参数或 `SENTINELOPS_ROLE`：

| 角色 | HTTP | 后台任务 | 约束 |
| --- | --- | --- | --- |
| `api` | 绑定 `:8001` | 启动知识索引队列 | 在 durable 路径中只创建/读取 Run，不持有该路径的 Agent 执行。 |
| `worker` | 不绑定 HTTP | Scheduler、知识索引、durable Worker、Retention | 生产部署建议独立运行。 |
| `all` | 绑定 `:8001` | 同时启动 | 仅允许 `development`；未指定角色时默认使用它。 |

配置目录默认是 `manifest/config`，可用 `SENTINELOPS_CONFIG_DIR` 覆盖。加载器优先选择同目录下的 `config.local.yaml`，两份 YAML 是“完整替换”关系，不会合并。

### C4 System Context

下面的上下文图只展示 SentinelOps 与人员、外部 Provider 和外部系统之间的边界。

~~~mermaid
C4Context
  title SentinelOps - System Context

  Person(operator, "安全运营人员", "查看事件、发起分析、审批高风险动作")
  System(sentinelops, "SentinelOps", "安全事件接入、Agent 研判、知识检索、响应编排与审计")
  System_Ext(alerts, "外部告警源", "Webhook / CEF / LEEF / API Push")
  System_Ext(models, "Model Provider", "Chat / Embedding / Rerank")
  System_Ext(tavily, "Tavily Search", "可选的联网威胁情报搜索")
  System_Ext(context7, "Context7 MCP", "配置的文档查询工具")
  System_Ext(soar, "外部处置与通知系统", "DingTalk / WeCom / SMTP")

  Rel(operator, sentinelops, "使用 Web Console 与分析能力")
  Rel(alerts, sentinelops, "推送标准化或设备格式告警")
  Rel(sentinelops, models, "调用模型与向量服务")
  Rel(sentinelops, tavily, "按配置执行联网搜索")
  Rel(sentinelops, context7, "按 allowlist 查询外部文档")
  Rel(sentinelops, soar, "在 Gate/Policy/Approval 允许时执行动作")
~~~

### C4 Container

容器图对应仓库的可部署边界；MySQL、Redis、Milvus 是持久化/基础设施容器，Agent 的共享库和 Eino Graph 属于 API/Worker 内部实现，不单独虚构成部署容器。

~~~mermaid
C4Container
  title SentinelOps - Container Diagram

  Person(operator, "安全运营人员", "操作 Web Console")
  System_Ext(models, "Model Provider", "Chat / Embedding / Rerank")
  System_Ext(tavily, "Tavily Search", "可选联网搜索")
  System_Ext(context7, "Context7 MCP", "配置的 MCP Server")
  System_Ext(soar, "通知与处置系统", "DingTalk / WeCom / SMTP")

  System_Boundary(platform, "SentinelOps") {
    Container(gateway, "Nginx Gateway", "Nginx 1.27", "一体化部署的统一入口、SSE 代理、IP deny 规则")
    Container(web, "Web Console", "React 19 + TypeScript + Vite", "事件、聊天、知识库、Trace、RAG Eval 与 Ops 页面")
    Container(api, "API Service", "GoFrame v2", "认证、RBAC、限流、业务查询、标准模式与 durable Run API")
    Container(worker, "Worker Service", "Go + Eino ADK", "订阅调度、索引队列、durable Plan 执行、Retention")
    Container(migrate, "Migration Job", "Goose v3.27.3", "执行版本化 MySQL migrations")
    ContainerDb(mysql, "MySQL", "MySQL 8.0", "业务实体、workflow、approval/effect、Trace 与设置")
    ContainerDb(redis, "Redis", "Redis 7.4", "会话消息、长期摘要与 semantic cache")
    ContainerDb(milvus, "Milvus", "Milvus 2.5 standalone", "rag_store 集合的 dense/sparse 向量与 metadata")
  }

  Rel(operator, gateway, "访问一体化部署", "HTTP")
  Rel(operator, web, "开发模式访问", "HTTP :5173")
  Rel(gateway, web, "代理静态资源", "HTTP")
  Rel(gateway, api, "代理 /api、SSE 与 OpenAPI", "HTTP")
  Rel(web, api, "调用业务模块", "JSON / SSE")
  Rel(api, mysql, "读写业务与 Run", "GORM / MySQL")
  Rel(api, redis, "读写会话与缓存", "Redis")
  Rel(api, milvus, "执行检索", "Milvus SDK")
  Rel(worker, mysql, "认领 Run、写事件与状态", "GORM / MySQL")
  Rel(worker, redis, "读取会话与缓存", "Redis")
  Rel(worker, milvus, "写入事件/文档向量", "Milvus SDK")
  Rel(migrate, mysql, "执行 migrations", "Goose")
  Rel(api, models, "Chat / Embedding / Rerank")
  Rel(worker, models, "Agent 与索引调用")
  Rel(api, tavily, "可选联网搜索")
  Rel(worker, tavily, "可选联网搜索")
  Rel(worker, context7, "受 allowlist 的 MCP 调用")
  Rel(worker, soar, "受策略控制的处置/通知")
~~~

### 部署形态

| 形态 | 启动内容 | 适用场景 | 入口 |
| --- | --- | --- | --- |
| 开发模式 | `docker-compose.dev.yml` 启动 Context7、etcd、MinIO、Milvus、Attu、Redis、MySQL 和 migrate；API/Worker/前端在宿主机运行 | 本地开发、调试单个模块 | Vite `http://localhost:5173`，API `http://localhost:8001` |
| 一体化模式 | `docker-compose.yml` 启动基础设施、migrate、API、Worker、frontend、Nginx | 本地演示或单机部署 | Nginx `http://localhost`，Attu `http://localhost:8000` |

一体化 Compose 中，API 容器监听内部 `:8001`，Nginx 对 `/api/chat/` 关闭 buffering 并设置较长 read timeout，便于 SSE 流式传输。IP 黑名单文件由 Worker 写入共享目录，Nginx 通过 `include` 在入口层加载。

## 核心流程

### 标准意图路由

~~~mermaid
flowchart TD
  A([收到聊天请求]) --> B{deep_thinking?}
  B -->|是| P[进入 Plan / Execute / Replan]
  B -->|否| R[Router 使用 chat.default]
  R --> C{confidence >= 0.70 且意图有效?}
  C -->|否| F[降级为 Chat]
  C -->|是| E[Executor 从 Registry 获取 SubAgent]
  E --> D{SubAgent 已注册?}
  D -->|否| F
  D -->|是| S[执行 Chat / Event / Report / Risk / Solve / Intel / Ops]
  F --> S
  S --> Z([流式输出结果])
  P --> Z
~~~

### Durable Run、Worker 与 SSE

~~~mermaid
sequenceDiagram
  autonumber
  participant UI as Web Console
  participant API as API Service
  participant DB as MySQL GORMStore
  participant W as Durable Worker
  participant E as Eino Plan Agent

  UI->>API: 创建 durable Run
  API->>DB: 原子创建 Run、Revision 0、snapshot、run.created
  API-->>UI: 返回 run_id
  W->>DB: 轮询并领取 fenced lease
  W->>E: 执行 Planner / Executor / Replanner
  E->>DB: 写 workflow_events、checkpoint、approval/effect 状态
  API->>DB: 按 after_seq 读取事件
  DB-->>API: 返回持久化事件
  API-->>UI: SSE meta/status/content/plan_step/done
  UI-->>API: 断线后携带 run_id + last_seq 重连
  API->>DB: 只读取 seq > last_seq 的事件
  DB-->>API: replay 缺失事件
  Note over UI,W: API 断线只停止读取，不取消 Worker
~~~

### RAG 检索

~~~mermaid
flowchart LR
  Q[用户查询] --> N[Normalize]
  N --> RW{启用 Rewrite / Split?}
  RW -->|Rewrite| R[改写上下文]
  RW -->|Split| S[拆分子问题]
  RW -->|否| O[保留原查询]
  R --> S
  S --> M[多路并行 Retrieve + Dedup]
  O --> M
  M --> EMB[Embedding]
  EMB --> CACHE{Redis semantic cache 命中?}
  CACHE -->|是| FIL[文档状态、Scope 与 metadata 交集过滤]
  CACHE -->|否| HYB[Milvus Dense COSINE + BM25 IP]
  HYB --> RRF[RRF 融合；失败回退 dense]
  RRF --> FIL
  FIL --> RR[Rerank（需要时）]
  RR --> TOP[FinalTopK 文档]
  TOP --> PROMPT[以不受信任 User 数据边界注入 Evidence]
~~~

### 告警接入与异步索引

~~~mermaid
flowchart TD
  A[Webhook / CEF / LEEF / API Push] --> P[解析并归一化 NormalizedAlert]
  P --> K[标题 + 来源 + 内容片段生成 SHA-256 去重键]
  K --> Q{重复?}
  Q -->|是| R[返回 is_new=false]
  Q -->|否| DB[(MySQL events)]
  DB --> IDX[异步 IndexDocuments]
  IDX --> V[Embedding + Milvus events 分区]
  V --> DONE[后续 RAG 可检索]
~~~

## 核心能力

### 1. Agent 编排

#### 标准模式：Router → Executor → SubAgent

标准模式在 `internal/ai/intent` 中构建非单例的 Eino Graph。Router 只负责意图识别和置信度检查，Executor 通过全局 Registry 获取 SubAgent；模型调用失败、JSON 解析失败、未知意图或未注册 Agent 都会安全降级到 Chat。

实际业务意图 SubAgent：

| SubAgent | Profile | RAG 查询处理 | 最大步数/迭代 | 主要工具或职责 |
| --- | --- | --- | ---: | --- |
| Chat | default | 共享会话历史与 RAG | 25 | 通用安全问答、事件/订阅/报告查询 |
| Event | default | Rewrite + Split + 并行检索 + Rerank | 25 | 事件查询、相似事件、关联分析 |
| Report | reasoning | Rewrite + Split + 并行检索 + Rerank | 30 | 周报/月报/自定义报告与模板 |
| Risk | reasoning | Rewrite + Split + 并行检索 + Rerank | 25 | CVE、攻击路径、影响范围和风险评估 |
| Solve | reasoning | 不 Rewrite/Split | 10 | 单事件应急处置方案 |
| Intelligence | default | 不 Rewrite/Split | 12 | 联网搜索、CVE/威胁组织分析、情报沉淀 |
| Ops | default | 不 Rewrite/Split | 20 | 状态更新、IP 封禁、通知和响应编排 |

公共专业 Agent 由 `internal/ai/agent/base/builder.go` 构建 RAG + ReAct DAG：

`InputToChat` 与 `RetrievalNode` 并行，`EvidencePrompt` 将检索结果放入不受信任的 User 数据边界，`Template` 使用 `AllPredecessor` fan-in 后驱动 `ReactAgent`。`NewSingletonAgent` 用 `sync.Once` 懒初始化进程级 runner，避免每个请求重复编译图。

#### 深度思考：官方 Plan/Execute/Replan

`internal/service/chat.ExecuteDeepThink` 先执行可取消的预思考流，再进入官方 Eino `planexecute.New`。Planner 使用 reasoning Profile 生成结构化 Plan，Executor 使用 default Profile 执行当前步骤，Replanner 根据执行结果决定继续还是 Respond，外层最多 20 次迭代。

Executor 通过官方 `adk.NewAgentTool` 调度：

- `event_analysis_agent`
- `report_agent`
- `risk_assessment_agent`
- `solve_agent`
- `intelligence_agent`
- `ops_agent`
- `mcp_agent`
- `skill_agent`
- `query_internal_docs`
- `get_current_time`

生产 durable Worker 只允许外层 `plan_agent`，并复用同一个 `RuntimeHandler`、GORMStore、GateEvaluator 和按 Attempt 创建的 Langfuse runtime。Summary Agent 是独立线性 DAG，用于会话摘要压缩，不计入业务意图路由数量。

### 2. RAG 检索

`internal/ai/retrieval.Retriever` 的固定阶段是：

1. 在存在 budget provider 时预留 RAG 预算；
2. 调用 Embedding；
3. 查询 Redis semantic cache；
4. 未命中时访问 Milvus hybrid 或 dense search；
5. 过滤、截断并写回缓存。

当前配置默认值：

| 参数 | 默认值 |
| --- | ---: |
| semantic cache TTL | 24 小时 |
| cache threshold | 0.85 |
| 初始 TopK | 5 |
| FinalTopK | 3 |
| dense MinScore | 0.30 |
| hybrid | true |
| RRF k | 60 |

Hybrid Search 使用 dense vector + COSINE 和 BM25 sparse vector + IP，由 Milvus `HybridSearch` 使用 RRF 融合。混合调用失败时回退到 dense，避免单一路径故障阻断只读分析。专业 Agent 还可以执行 Normalize、Rewrite、Split、多路并行检索、去重和 Rerank。

知识库 `documents` 分区是 fail-closed：检索结果必须同时满足 MySQL 文档状态、Access Scope、source/content/indexed version、chunk 状态和 Milvus metadata；缺少 Scope 或 MySQL 真值源不可用时返回不可用证据，而不是放宽过滤。

当前 Milvus 约定：

- Database：`sentinel`
- Collection：`rag_store`
- Partition：`events`、`documents`
- Dense vector：2048 维，COSINE
- Sparse vector：BM25，IP
- metadata：JSON

### 3. 知识库与文档索引

上传后的文档进入 Worker Pool，由 `knowledge_index_pipeline.BuildAndIndex` 完成解析、分块、Embedding 和 Milvus 写入；该路径是直接调用的索引流水线，不依赖 Eino Graph，以便同时返回完整的 ChunkResult 并写入 MySQL。

#### Parser 与 Chunker

| 文件类型/策略 | 实现 |
| --- | --- |
| PDF | 文本提取，并在失败时经过 pdfcpu 修复、解密和 legacy xref 兼容路径 |
| DOCX | 纯文本解析或 Heading 结构解析 |
| Markdown | 保留标题标记，提取 H1-H6 层级路径 |
| Go / Python / Java | 代码读取 + 函数/类/接口边界感知分块 |
| `hierarchical` | 父块/子块分层；默认 parent 1024、child 256、overlap 40 rune |
| `sliding_window` | 按窗口和重叠切分，优先在换行或句末边界对齐 |
| `code` | 语法声明边界分块，保留函数/类名为 SectionTitle |

向量化使用子块内容，检索命中后把 parent content 和 section metadata 交给 LLM，兼顾召回聚焦和回答上下文完整性。索引批次固定为 10 个文档，Worker Pool 并发度和队列容量由配置控制。

### 4. 告警接入与订阅调度

#### 外部告警接入

`internal/controller/ingest` 将 Webhook、CEF、LEEF 和 API Push 统一转换成 `NormalizedAlert`，随后走同一个 `ingest.Ingest`：

- Webhook 支持通用 JSON 字段别名；
- CEF 解析 Header/Extension，并映射 severity；
- LEEF 解析 Tab 分隔属性；
- API Push 接受标准化 title、content、severity、source、CVE 和扩展字段；
- 去重键由标题、来源和内容片段生成 SHA-256 截断值；
- 新事件先写 MySQL，再异步写入 Milvus `events` 分区。

接入路由使用 `AuthDisabledWriteGuard` 与 `IngestAPIKeyMiddleware`，不复用浏览器 JWT 身份。

#### RSS/GitHub 订阅

Scheduler 为每个启用订阅维护独立 goroutine，支持 Create/Update/Resume 时热注册，Pause/Delete 时注销。默认抓取间隔为 15 分钟；新事件和新告警入库后会按调用路径立即或异步触发向量索引，批次大小由 `scheduler.index_batch_size` 控制。

- RSS/Atom 由 gofeed 解析；
- 普通 GitHub 仓库抓最近 Releases 和 Security Advisories；
- URL 包含 `/security/advisories` 时只抓 Advisories，减少无关 API 调用；
- 抓取结果经过 Extract、严重程度推断、CVE 提取、去重、MySQL 入库和向量索引；
- 另有每 30 分钟一次、单次最多 20 条的高危存量事件补偿扫描。

### 5. Durable Runtime、HITL 与 Effect

durable 路径把执行状态从进程内存提升到 MySQL：

- API 原子创建 Run、Session Revision 0、冻结 Runtime Snapshot、预算和 `run.created`；
- Worker 通过 fenced lease/generation 认领，过期或失效的 Worker 不能继续提交；
- workflow event 使用持久化 `seq`，checkpoint 支持 resume/replay；
- API 与 Worker 解耦，浏览器断线只停止读取，Worker 继续执行；
- Run 输入 immutable，Snapshot 记录模型、工具、Gate、Skill/MCP 等兼容身份；
- 终态统一通过 `CompleteRunAndCommitSession` 写入并释放 Session。

HITL 与 Effect 机制把建议和副作用分开：

- L1/L2 mutation 生成 Approval/Effect，Approval 绑定 checkpoint fingerprint；
- approve/reject/resume 经过角色与资源 Scope 校验；
- primary/derived effect ledger 提供幂等键和重试边界；
- 外部副作用结果未知时进入 parked/reconciliation，不把未知状态渲染成成功；
- `block_ip`、事件状态更新、报告/情报写入和通知均经过 Effect/Gate/Policy 路径。

### 6. 安全、策略与可靠性

#### SecretRef

配置只保存 `env:&lt;name&gt;` 或 `file:&lt;path&gt;` 引用。`UseSecret` 在回调结束后清零解析出的临时字节；生产启动会拒绝缺失、空值或默认 Secret。请勿把真实 Key、密码、Webhook 或 DSN 写入 README、日志、Trace 或提交。

#### Auth、RBAC 与资源 Scope

- JWT Identity 由服务端从 Token 生成，不信任客户端传入的 `user_id`；
- 角色包含 viewer、operator、approver、admin；
- HTTP 层执行粗粒度 RBAC，Service 层继续检查资源 Scope 和文档 ACL；
- auth-disabled 模式注入只读 viewer，并拒绝业务写入；
- ingest 使用独立 API Key，和用户 JWT 分离。

#### 模型可靠性

模型访问通过 Provider → Model Catalog → Routing 三层解析，不把业务代码绑定到单一厂商。配置提供 Eino Retry、provider-qualified breaker/failover、QPS limiter 和 Agent 超时；失败时由各调用路径按代码定义回退或 fail-closed。

### 7. 可观测性与 RAG Eval

Trace 由 Eino callbacks、手动 spans 和 GORM Plugin 共同产生，节点类型覆盖：

`LLM`、`TOOL`、`RETRIEVER`、`EMBEDDING`、`LAMBDA`、`AGENT`、`CACHE`、`DB`、`RERANK`。

MySQL 中保存请求级 `agent_trace_runs` 和节点级 `agent_trace_nodes`，可记录耗时、Token、成本、召回数量、相似度、Rerank 分数和慢查询信息。Langfuse 是可选的 Attempt-scoped exporter，由 Gate 和 SecretRef 同时控制。

RAG Eval 页面从已有 Trace 聚合 KPI，不重新执行 RAG：

- 成功率、平均/P95 延迟；
- 召回文档数、最高分、缓存命中率、Rerank 分数；
- 按模型聚合 Token 和成本；
- 聊天点赞/点踩写入 `message_feedbacks`；
- 通过 session_id 关联 Trace 详情。

### 8. 记忆、Skill 与 MCP

#### 分层记忆

- 进程内 `SessionMemory` 保存近期消息；
- Redis 保存近期消息和长期摘要，默认 Chat TTL 为 30 天；
- MySQL `user_preferences` 保存跨会话偏好；
- Token 超过 3000 是摘要主触发器，消息数超过 30 是辅助触发器，每次默认压缩 10 条并至少保留 4 条近期消息；
- 显式反馈标签可直接更新偏好，其他场景由 Summary/Preference 流程按配置推断。

#### Skill

Skill 使用官方 Eino Skill middleware。当前仓库的 `manifest/skills` 包含 `evidence-summary`、`incident-triage`、`response-checklist`；local filesystem backend 只读，限制最大字节数，并受 L0 tool allowlist 与 Gate 约束。

#### MCP

MCP 使用官方 MCP SDK/Eino officialmcp，按配置限制 transport、host、port、tool allowlist、页数、结果字节数和超时。开发配置提供 Context7 示例；禁用 MCP 时不会创建 session。

### 9. Web Console

前端由 React 19、TypeScript、Vite、Zustand、TailwindCSS 和 ECharts 组成。真实路由来自 `web/src/App.tsx`：

`/login`、`/dashboard`、`/subscriptions`、`/events`、`/events/analysis`、`/reports`、`/chat`、`/settings`、`/term-mapping`、`/traces`、`/traces/:traceId`、`/knowledge`、`/rag-eval`、`/ingest`、`/ops`。

UI 层覆盖登录与权限、事件态势、订阅管理、聊天/SSE、知识库、Trace、RAG Eval、外部接入示例和 Ops Run/Approval。Playwright smoke 测试还验证未认证跳转、关键路由、主布局和暗色模式；审批页面测试覆盖 viewer 权限、unknown effect、并发决定冲突、preview 和失败轮询。

## 技术栈

| 层次 | 技术 | 版本/用途 |
| --- | --- | --- |
| 语言与后端 | Go | 1.27.0 |
| HTTP/配置 | GoFrame | v2.10.2 |
| Agent 编排 | Cloudwego Eino / Eino ADK | v0.9.15 |
| 关系数据库 | MySQL + GORM | MySQL 8.0；GORM v1.31.2 |
| 数据库迁移 | Goose | v3.27.3（迁移文件 00001–00008） |
| 向量数据库 | Milvus SDK | v2.4.2；Compose 镜像 2.5.10 |
| 缓存 | go-redis | v9.22.0；Compose Redis 7.4 |
| 认证 | golang-jwt | v5.3.1 |
| 可观测性 | OpenTelemetry SDK | v1.44.0，可选 Langfuse exporter |
| 前端 | React / ReactDOM | 19.2.8 |
| 类型与构建 | TypeScript / Vite | 7.0.2 / 8.2.2 |
| UI 状态与样式 | Zustand / TailwindCSS | 5.0.15 / 4.3.3 |
| 图表与浏览器测试 | ECharts / Playwright | 6.1.0 / 1.62.1 |
| 网关 | Nginx | 1.27-alpine |

模型、Embedding 和 Rerank 通过配置的 Provider/Model Catalog/Route 解析。仓库示例使用 OpenAI-compatible Chat/Embedding 与 DashScope-compatible Rerank 驱动，但业务代码按路由引用，不把厂商名称写死在 Agent 中。

## 快速开始

### 前置条件

- Go 1.27.0；
- Node.js 24.19.0 和 npm；
- Docker Engine 与 Docker Compose；
- 可访问的 MySQL 8.0、Redis 7.x、Milvus 2.x（开发模式由 Compose 提供）；
- 至少一个可用的 Chat/Embedding/Rerank Provider Secret。

以下命令均在仓库根目录 `/home/monody/project/SentinelOps` 执行。

### 1. 创建本地配置并注入 Secret

启动本地 API/Worker 和前端前，先把基础配置复制为本地覆盖文件。`config.local.yaml` 已被 Git 忽略，加载器会优先读取它，并将其作为 `config.yaml` 的完整替换，而不是逐字段合并：

~~~bash
cp manifest/config/config.yaml manifest/config/config.local.yaml
~~~

后端从该文件读取 Provider、数据库和认证配置；前端本地开发通过 Vite 的 `/api` 代理访问后端，因此前后端启动前都先完成这一步。默认模型 API Key 由 `providers.*.secret_ref: env:SENTINELOPS_MODEL_API_KEY` 注入，不要把明文 `api_key` 写进 YAML。

如果本机已经有 `config.local.yaml`，不要重复覆盖其中的 Secret，直接编辑现有文件。然后在环境中提供配置引用所需的值。示例变量名如下（值请使用你自己的 Secret，不要提交）：

~~~bash
export SENTINELOPS_MYSQL_DSN='root:change-me@tcp(127.0.0.1:3307)/sentinelops?parseTime=true&multiStatements=true'
export SENTINELOPS_MODEL_API_KEY='replace-with-model-provider-key'
export SENTINELOPS_JWT_SECRET='replace-with-a-random-jwt-secret'
export SENTINELOPS_ADMIN_PASSWORD='replace-with-admin-password'
~~~

`config.yaml` 默认把 Langfuse 标记为 enabled，但没有填写 public/secret key 引用；如果没有 Langfuse 凭据，请在 `config.local.yaml` 中把 `observability.langfuse.enabled` 设为 `false`，否则配置校验会拒绝启动。若启用 Langfuse、SMTP、DingTalk、WeCom、Effect 或 MCP Header，再为对应 `SecretRef` 提供环境变量或文件引用。

数据库、JWT、Provider、SMTP、MCP 和 Effect 等受 SecretRef 管理的字段只接受 `env:` / `file:` 引用；Tavily/GitHub token 目前仍是配置字段，请只写入被 Git 忽略的本地配置。任何情况下都不要把真实 Secret 写到 README、日志、Trace 或提交。

可用环境变量指定另一份完整配置：

~~~bash
export SENTINELOPS_CONFIG_DIR=/path/to/config-dir
~~~

### 2. 启动开发依赖

~~~bash
docker compose -f manifest/docker/docker-compose.dev.yml up -d --build
~~~

该 Compose 只启动基础设施和 migrate；Context7 默认暴露在 `127.0.0.1:3333/mcp`，Milvus 暴露 `19530`，Redis 暴露 `16379`，MySQL 暴露 `3307`，Attu 暴露 `8000`。

全新数据库会执行版本化迁移并写入学习环境的 `admin` 与 `user1` 默认账号（密码均为 `123456`）。它们只用于本地学习，部署前请替换或清理。

### 3. 启动后端

开发环境可用单进程模式：

~~~bash
go run . all
~~~

也可以拆分 API 和 Worker：

~~~bash
# 终端 1
go run . api

# 终端 2
go run . worker
~~~

默认 API 地址为 `http://localhost:8001`。如果使用 `config.local.yaml` 中的 `127.0.0.1:8001`，前端开发代理仍会把 `/api` 转发到该地址。

### 4. 启动前端

~~~bash
npm ci --prefix web
npm run dev --prefix web
~~~

默认前端地址为 `http://127.0.0.1:5173`。前端脚本还提供 `npm run lint --prefix web`、`npm run build --prefix web` 和 `npm run test:smoke --prefix web`。

## 部署

### 一体化 Docker Compose

`manifest/docker/docker.sh` 会按顺序执行前端构建、基础设施启动、migrate/API/Worker/frontend 镜像构建、数据库迁移、服务启动，并等待 API 健康地址 `/api.json`：

~~~bash
cd manifest/docker
bash docker.sh
~~~

脚本最终提供：

- Web Console：`http://localhost`
- OpenAPI 文档：`http://localhost/api.json`
- Swagger：`http://localhost/swagger`
- Attu：`http://localhost:8000`

Compose 文件中的默认值是开发/学习示例，不应直接视为生产安全配置。生产部署至少应显式设置 MySQL DSN、JWT Secret、管理员密码、模型 Provider Secret，并根据实际环境关闭不需要的 MCP、Langfuse、外部通知和写入 Gate。

### 生产角色拆分

生产镜像会把 `config.docker.yaml` 复制为容器内的 `config.local.yaml`，并将环境标记为 `production`。生产不允许 `all` 角色；容器内建议单独运行：

~~~bash
./server api
./server worker
~~~

如果在宿主机直接运行二进制，请先准备一份完整的生产 `config.local.yaml`，不要直接把 `config.docker.yaml` 当作可自动选择的文件。

生产启动还会校验当前 durable runtime version、数据库引用、JWT/管理员 Secret 和所有已配置 Provider 的 Secret。外部通知、Nginx 黑名单、Docker socket、Langfuse 和真实模型配额应按部署环境单独审查。

## 验证

仓库提供与 Pull Request workflow 对应的本地质量门禁：

~~~bash
# 后端：gofmt、tidy、vet、staticcheck、govulncheck、race test
SENTINELOPS_TEST_DSN='root:password@tcp(127.0.0.1:3306)/sentinelops?parseTime=true' \
  scripts/ci/pr.sh --lane backend

# 合同：Workflow、Agent、MCP、Skill、Policy
scripts/ci/pr.sh --lane contracts

# 前端：npm ci、lint、build
scripts/ci/pr.sh --lane frontend
~~~

也可以运行全部门禁：

~~~bash
scripts/ci/pr.sh --lane all
~~~

前端 smoke 测试：

~~~bash
npm run test:smoke --prefix web
~~~

`scripts/ci/pr.sh --lane backend` 要求 `SENTINELOPS_TEST_DSN`、固定版本的 staticcheck 和 govulncheck；未准备这些依赖时，门禁会按设计失败。Hosted CI 没有在本 README 重写过程中代为运行，外部 Provider、真实通知系统和生产部署也不因本地测试通过而自动获得上线结论。

## 仓库结构

~~~text
.
├── api/                     # GoFrame API 请求/响应定义
├── internal/
│   ├── bootstrap/           # api / worker / all 启动编排
│   ├── ai/
│   │   ├── agent/           # Chat、专业 Agent、Plan、Skill、MCP、索引流水线
│   │   ├── intent/          # Router、Executor、SubAgent Registry
│   │   ├── retrieval/       # Milvus dense/hybrid、cache、Scope 过滤
│   │   ├── workflow/        # Run、lease、checkpoint、approval、effect、recovery
│   │   ├── policy/          # RBAC、canonical JSON、redaction、工具目录
│   │   └── trace/           # Span、Token、成本与 Langfuse 适配
│   ├── controller/          # chat/event/report/knowledge/ops 等控制器
│   ├── service/             # chat、knowledge、ingest、pipeline、scheduler
│   └── dao/                 # MySQL 与 Milvus 数据访问
├── manifest/
│   ├── config/              # 基础、local、Docker 配置与 SecretRef 示例
│   ├── docker/              # Dockerfile、Compose、Nginx、docker.sh
│   └── skills/              # evidence-summary、incident-triage、response-checklist
├── migrations/              # Goose 00001–00008
├── scripts/ci/              # backend/frontend/contracts 质量门禁
├── utility/                 # auth、middleware 等通用能力
├── web/                     # React Web Console 与 Playwright 测试
├── main.go
└── go.mod
~~~

## 当前边界与已知限制

- Chat、Embedding、Rerank、Tavily、Context7、Langfuse、SMTP 和通知 Webhook 都依赖外部服务；仓库中的 Provider 只是配置示例，不代表服务凭据已提供。
- `all` 角色仅用于 development；生产 durable Runtime 必须拆分 API 与 Worker，并通过 runtime version、Gate 和 Snapshot 兼容性检查。
- 默认 `shadow_mode: true` 会关闭 effective L1/L2 writes；AI 运维页面可以展示计划、审批和结果，但不等同于已经打开真实副作用。

## 贡献

欢迎通过 Issue 讨论问题，通过 Pull Request 提交改进。提交前建议：

1. 先确认改动对应的模块和 Gate/Policy 边界；
2. 为行为变化补充或更新 Go/前端/合同测试；
3. 运行与改动相关的最小门禁，条件允许时运行 `scripts/ci/pr.sh --lane all`；
4. 在 PR 中区分本地 PASS、外部依赖未运行的 NOT RUN，以及真实失败的 FAIL。
