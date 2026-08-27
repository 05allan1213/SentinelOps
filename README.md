# SentinelOps

**安全事件智能研判多智能体协同平台** — 基于大语言模型的新一代安全运营自动化系统，通过 9 个专业 AI Agent 协同工作，实现安全事件的智能监控、深度分析、风险评估、智能运维。系统融合 RAG 检索增强、联网威胁情报、全链路可观测性等先进技术，将传统需要数小时的人工研判工作压缩至秒级，显著提升安全团队的响应效率和决策质量。

![Go](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat&logo=go)
![GoFrame](https://img.shields.io/badge/GoFrame-v2.7.1-blue?style=flat)
![Eino](https://img.shields.io/badge/Eino-v0.3.0+-purple?style=flat)
![React](https://img.shields.io/badge/React-18-61DAFB?style=flat&logo=react)
![License](https://img.shields.io/badge/License-MIT-green?style=flat)

---

## 目录

- [项目简介](#项目简介)
- [系统架构](#系统架构)
- [核心功能模块](#核心功能模块)
    - [1. 多 Agent 协同分析引擎](#1-多-agent-协同分析引擎)
    - [2. 深度思考模式](#2-深度思考模式plan-agent-supervisor-worker)
    - [3. 联网威胁情报](#3-联网威胁情报intelligence-agent)
    - [4. RAG 增强检索管道](#4-rag-增强检索管道)
    - [5. 知识库管理](#5-知识库管理)
    - [6. 多源情报订阅、自动抓取与告警接入](#6-多源情报订阅自动抓取与告警接入)
    - [7. 全链路可观测性](#7-全链路可观测性trace)
    - [8. RAG 质量评估](#8-rag-质量评估)
    - [9. 分层记忆管理](#9-分层记忆管理)
    - [10. 智能运维](#10-智能运维)
    - [11. API 限流保护](#11-api-限流保护)
    - [12. 结构化报告生成](#12-结构化报告生成)
    - [13. SSE 流式输出](#13-sse-流式输出)
- [技术栈](#技术栈)
- [快速开始](#快速开始)
- [部署](#部署)
- [许可证](#许可证)

---

## 项目简介

SentinelOps 是面向企业安全运营的智能研判平台，基于多 Agent 协同架构实现安全事件的自动化分析与响应。系统融合多源威胁情报并构建统一的告警接入与标准化处理体系，通过多角色 AI Agent 的协同分工完成事件去重、关联分析、风险评估与报告生成，并结合自动化响应编排能力，打通从安全研判到处置执行的端到端闭环。

**核心能力**

- **多 Agent 协同**：9 个专业 Agent 分工协作，覆盖智能对话、联网搜索、事件分析、报告生成、风险评估、解决方案、威胁情报、摘要压缩、智能运维
- **混合检索增强**：BM25 稀疏向量 + 语义稠密向量，RRF 融合排序，Rerank 精排，语义缓存加速
- **深度思考模式**：Plan-Execute-Replan 循环架构，Supervisor-Worker 协同调度，处理复杂多步骤任务
- **全链路可观测**：请求级和节点级追踪，记录耗时、成本、检索质量，支持问题定位和性能优化
- **AI 智能运维**：自主决策，自动执行 IP 封禁、多渠道告警通知与事件状态更新，打通研判到响应处置的全流程闭环
- **质量评估体系**：KPI 仪表盘、用户反馈、趋势分析，量化系统表现

本项目提供了企业级安全 AI 系统的完整工程实现，涵盖多轮对话记忆、RAG 检索质量、多 Agent 编排、深度推理、可观测性等关键技术问题的解决方案。

---

**问答页面预览图：**

<img src="docs/images/问答页面预览图.png" width="100%" alt="问答页面预览图">

<img src="docs/images/问答页面预览图2.png" width="100%" alt="问答页面预览图2">

<img src="docs/images/问答页面预览图3.png" width="100%" alt="问答页面预览图3">

<img src="docs/images/问答页面预览图4.png" width="100%" alt="问答页面预览图4">

<img src="docs/images/问答页面预览图5.png" width="100%" alt="问答页面预览图5">

**Agent 事件分析页面预览图：**

<img src="docs/images/Agent事件分析页面预览图.png" width="100%" alt="Agent事件分析页面预览图">

**AI 知识库管理图：**

<img src="docs/images/AI知识库管理图.png" width="100%" alt="AI知识库管理图">

<img src="docs/images/AI知识库管理图2.png" width="100%" alt="AI知识库管理图2">

<img src="docs/images/AI知识库管理图3.png" width="100%" alt="AI知识库管理图3">

**AI 智能运维：**

<img src="docs/images/AI智能运维.png" width="100%" alt="AI智能运维">

**安全态势总览与实时监控：**

<img src="docs/images/安全态势总览与实时监控.png" width="100%" alt="安全态势总览与实时监控">

**Agent 全链路观测：**

<img src="docs/images/Agent全链路观测.png" width="100%" alt="Agent全链路观测">

<img src="docs/images/Agent全链路观测2.png" width="100%" alt="Agent全链路观测2">

## 系统架构

<img src="docs/images/系统架构图.png" width="100%" alt="系统架构图">

```
┌─────────────────────────────────────────────────────────────────────┐
│           用户层: Web UI (React 18)  |  REST API (SSE)            │
└────────────────────────────────┬────────────────────────────────────┘
                                 ▼
┌─────────────────────────────────────────────────────────────────────┐
│ Nginx 网关 (:80)                                                    │
│ 路由分发:                                                            │
│         /      -> frontend:80                                       │
│         /api   -> api:8001                                          │
│         /swagger -> api:8001/swagger                                │
│         /api/chat -> api:8001（SSE 直通）                            │
│ 安全能力: include 黑名单文件 · deny IP · reload 热更新                │
└────────────────────────────────┬────────────────────────────────────┘
                                 ▼
┌─────────────────────────────────────────────────────────────────────┐
│ 后端 API 层 (GoFrame v2 · :8001)                                   │
│ 路由: chat/event/report/knowledge/trace/rageval/ingest             │
│ 中间件: JWT 认证 · SessionId · SSE 响应头                           │
│ 限流: 令牌桶两层限流（全局 QPS → 模块级用户 QPS）                      │
│ 告警接入: X-API-Key 认证 · Webhook/CEF/LEEF/API Push               │
└────────────────────────────────┬────────────────────────────────────┘
                                 │
                        ┌────────┴────────┐
                        │  deep_thinking? │
                        └────────┬────────┘
             ┌──── false ────────┤────── true ──────┐
             ▼                                      ▼
┌────────────────────────────┐   ┌────────────────────────────────────┐
│ 标准意图路由                │   │ Plan Agent (深度思考)              │
│ (default Profile)            │   │ (Supervisor-Worker 架构)           │
├────────────────────────────┤   ├────────────────────────────────────┤
│ 识别 7 类意图:              │   │ Planner (reasoning Profile):              │
│ • chat   - 通用对话         │   │  └─ 任务分解 & 步骤规划            │
│ • event  - 事件分析         │   │                                    │
│ • report - 报告生成         │   │ Executor (default Profile):             │
│ • risk   - 风险评估         │   │  ├─ event_analysis_agent           │
│ • solve  - 解决方案         │   │  ├─ report_agent                   │
│ • intel  - 威胁情报         │   │  ├─ risk_assessment_agent          │
│ • ops    - 智能运维         │   │  ├─ solve_agent                    │
│                            │   │  ├─ intel_agent                    │
│ 容错: 降级到 Chat Agent     │   │  └─ ops_agent                      │
│                            │   │                                    │
│                            │   │ Replanner:                         │
│                            │   │  └─ 继续/终止决策                  │
└────────────────────────────┘   └────────────────────────────────────┘
             │                                      │
             └──────────────────┬───────────────────┘
                                ▼
┌────────────────────────────────────────────────────────────────────────────────────────────────────────┐
│                              Agent 执行层 (9 个专业 Agent)                                              │
├────────────────────────────────────────────────────────────────────────────────────────────────────────┤
│  ┌────────┐ ┌────────┐ ┌────────┐ ┌────────┐ ┌────────┐ ┌────────┐ ┌────────┐ ┌────────┐ ┌────────┐    │
│  │ Chat   │ │ Event  │ │ Report │ │ Risk   │ │ Plan   │ │ Solve  │ │ Intel  │ │Summary │ │  Ops   │    │
│  │ Agent  │ │ Agent  │ │ Agent  │ │ Agent  │ │ Agent  │ │ Agent  │ │ Agent  │ │ Agent  │ │ Agent  │    │
│  ├────────┤ ├────────┤ ├────────┤ ├────────┤ ├────────┤ ├────────┤ ├────────┤ ├────────┤ ├────────┤    │
│  │ ReAct  │ │ ReAct  │ │ ReAct  │ │ ReAct  │ │Superv. │ │ ReAct  │ │ ReAct  │ │Linear  │ │ ReAct  │    │
│  │ +RAG   │ │ +RAG   │ │ +RAG   │ │ +RAG   │ │ Worker │ │ +RAG   │ │+Search │ │ 无 RAG │ │ 无 RAG │    │
│  └────────┘ └────────┘ └────────┘ └────────┘ └────────┘ └────────┘ └────────┘ └────────┘ └────────┘    │
│                                                                                                        │
│ 工厂模式: agent.NewSingletonAgent() · sync.Once 单例初始化                                              │
│ 工具注册: tools.GetMany(names) · 全局注册表按需获取                                                      │
│ 模型选择: routing.chat.default / routing.chat.reasoning                                                     │
└───────────────────────────────────┬────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────────────────────────────┐
│                              RAG 检索层 (Hybrid Retrieval + RRF Fusion)                         │
├─────────────────────────────────────────────────────────────────────────────────────────────────┤
│  第一阶段：查询预处理 & 向量化                                                                   │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐    ┌──────────────┐                 │
│  │ 查询优化      │ -> │ 向量嵌入      │ -> │ 语义缓存      │ -> │ 多路检索      │                 │
│  ├──────────────┤    ├──────────────┤    ├──────────────┤    ├──────────────┤                 │
│  │术语归一化     │    │qwen3.7 embed │    │Redis缓存     │    │并行执行       │                 │
│  │消除代词歧义   │    │2048维稠密向量│    │余弦≥0.85命中 │    │子查询并发     │                 │
│  │拆分子问题     │    │估算Token成本 │    │TTL 24小时    │    │结果去重合并   │                 │
│  └──────────────┘    └──────────────┘    └──────────────┘    └──────────────┘                 │
│                                                                      ↓                          │
│  第二阶段：混合检索 & 精排过滤                                                                   │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐    ┌──────────────┐                 │
│  │ 混合检索      │ -> │ 精排重打分    │ -> │ 过滤截断      │ -> │ 返回上下文    │                 │
│  ├──────────────┤    ├──────────────┤    ├──────────────┤    ├──────────────┤                 │
│  │Milvus双路     │    │qwen3-rerank  │    │MinScore≥0.30 │    │FinalTopK=3   │                 │
│  │Dense语义匹配  │    │精排模型打分   │    │过滤低分文档   │    │送入LLM上下文 │                 │
│  │Sparse词匹配   │    │提升Top质量   │    │TopK截断控制  │    │平衡质量成本   │                 │
│  │RRF融合k=60   │    │              │    │              │    │              │                 │
│  └──────────────┘    └──────────────┘    └──────────────┘    └──────────────┘                 │
└─────────────────────────────────────────────────────────────────────────────────────────────────┘
                                  │
                                  ▼
┌─────────────────────────────────────────────────────────────────────┐
│                        知识库管理层 (Knowledge Base)                 │
├─────────────────────────────────────────────────────────────────────┤
│  文档上传: PDF / Markdown / Docx / Code                             │
│                                                                     │
│  分块策略 (自动选择):                                                │
│  • structure_aware  - Markdown 文档 (保留结构)                      │
│  • hierarchical     - 其他格式 (层次分块)                           │
│                                                                     │
│  索引构建: 文档分块 → Embedder → Milvus (documents 分区)             │
│                                                                     │
│  向量管理: 启用/禁用文档 · 相似度搜索 · 分块查看                      │
└─────────────────────────────────┬───────────────────────────────────┘
                                  │
                                  ▼
┌─────────────────────────────────────────────────────────────────────┐
│                          工具调用层 (17 个工具)                      │
├─────────────────────────────────────────────────────────────────────┤
│  ┌─ event/ ──────────────┐  ┌─ report/ ─────────────────────────┐  │
│  │ • query_events         │  │ • query_reports                   │  │
│  │ • search_similar_events│  │ • query_report_templates          │  │
│  │ • query_subscriptions  │  │ • create_report                   │  │
│  └────────────────────────┘  └───────────────────────────────────┘  │
│                                                                     │
│  ┌─ intelligence/ ───────┐  ┌─ system/ ─────────────────────────┐  │
│  │ • web_search (Tavily)  │  │ • get_current_time                │  │
│  │ • save_intelligence    │  │ • query_database                  │  │
│  └────────────────────────┘  │ • query_internal_docs             │  │
│                               └───────────────────────────────────┘  │
│  ┌─ ops/ ────────────────────────────────────────────────────────┐  │
│  │ • trigger_ops          • block_ip        • update_event_status │  │
│  │ • notify_dingtalk      • notify_wecom    • notify_email        │  │
│  └───────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────┬───────────────────────────────────┘
┌─────────────────────────────────────────────────────────────────────┐
│                            存储层 (Storage)                        │
├─────────────────────────────────────────────────────────────────────┤
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐    │
│  │ MySQL           │  │ Redis           │  │ Milvus          │    │
│  ├─────────────────┤  ├─────────────────┤  ├─────────────────┤    │
│  │ • events        │  │ 语义缓存:        │ │ • 事件向量       │    │
│  │ • reports       │  │   └─ 余弦 >= 0.85│ │ • 文档向量       │    │
│  │ • subscriptions │  │   └─ TTL 24h     │ │ • 知识分块       │    │
│  │ • trace_runs    │  │                  │ │ • 相似检索       │    │
│  │ • trace_nodes   │  │ 分层记忆:        │ │ • 分区隔离       │    │
│  │ • kb_documents  │  │   └─ 短期: 最近 N │ │   (events/docs) │    │
│  │ • feedbacks     │  │   └─ 长期: 摘要   │ │                  │    │
│  │                 │  │   └─ TTL 30 天    │ │ 混合检索:        │    │
│  │                 │  │                  │ │   └─ Dense+Sparse│    │
│  │                 │  │ 会话管理:        │ │   └─ RRF 融合    │    │
│  │                 │  │   └─ session_id   │ │                  │    │
│  └─────────────────┘  └─────────────────┘  └─────────────────┘    │
│                                                                     │
│  ┌───────────────────────────────────────────────────────────────┐  │
│  │ Scheduler (后台调度器 · goroutine)                            │  │
│  │ • Fetcher: RSS/GitHub 情报抓取 → MySQL (每 15 分钟)          │  │
│  │ • Indexer: 文档向量嵌入 → Milvus (每 20 分钟)                │  │
│  │ • Ingest:  外部告警接入 → 归一化 → SHA256 去重 → MySQL       │  │
│  └───────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────┬───────────────────────────────────┘
                                  │
                                  ▼
┌─────────────────────────────────────────────────────────────────────┐
│                        智能运维层 (AI Ops)                           │
├─────────────────────────────────────────────────────────────────────┤
│  触发方式:                                                           │
│  • 事件列表一键运维按钮 → EventAnalysis Agent 分析 → Ops Agent 执行      │
│  • RAG 对话 ops 意图 → Ops Agent (query_events → trigger_ops)        │
│  • 深度思考 ops_agent Worker → trigger_ops → ExecuteRun              │
│                                                                     │
│  执行动作:                          数据存储:                        │
│  • update_event_status (状态更新)   • ops_runs (任务记录)            │
│  • block_ip (IP 封禁)               • ops_run_steps (步骤明细)       │
│  • notify_dingtalk / wecom / email  • ops_protected_assets (白名单)  │
│                                                                     │
│  保护机制: 受保护资产白名单 · 封禁前自动检查 · 防止误封关键资产         │
└─────────────────────────────────┬───────────────────────────────────┘
                                  │
                                  ▼
┌─────────────────────────────────────────────────────────────────────┐
│                      全链路可观测性 (Observability)                  │
├─────────────────────────────────────────────────────────────────────┤
│  9 种节点追踪:                                                       │
│  • Eino 自动埋点: LLM / TOOL / RETRIEVER / EMBEDDING / LAMBDA       │
│    └─ 通过 callbacks.Handler 自动捕获 OnStart/OnEnd/OnError        │
│  • 手动埋点: AGENT / CACHE / DB / RERANK                            │
│    └─ StartSpan/FinishSpan 包裹关键逻辑                             │
│                                                                     │
│  性能监控:                                                           │
│  • 慢查询检测 - 阈值 100ms，记录完整 SQL                            │
│  • Token 消耗统计 - 按模型分类统计 input/output tokens              │
│  • 成本计算 - 根据模型单价实时计算费用                              │
│  • 异步写入 - goroutine 批量写入，零阻塞主流程                      │
│                                                                     │
│  关键特性:                                                           │
│  • 上下文传递 - ActiveTrace 通过 context 跨组件传递                 │
│  • SpanStack - 支持嵌套 span，自动维护父子关系                      │
│                                                                     │
│  数据存储: agent_trace_runs / agent_trace_nodes (MySQL)             │
└─────────────────────────────────┬───────────────────────────────────┘
                                  │
                                  ▼
┌─────────────────────────────────────────────────────────────────────┐
│                      RAG 质量评估 (Quality Assessment)               │
├─────────────────────────────────────────────────────────────────────┤
│  KPI 指标:                                                           │
│  • 缓存命中率                                                       │
│  • 平均检索耗时                                                     │
│  • Token 消耗 & 成本趋势                                            │
│                                                                     │
│  用户反馈:                                                           │
│  • 满意度统计                                                       │
│  • 问题回溯                                                         │
│  • 质量趋势分析                                                     │
│                                                                     │
│  数据存储: message_feedbacks                                        │
└─────────────────────────────────────────────────────────────────────┘
```

---

## 核心功能模块

### 1. 多 Agent 协同分析引擎

安全事件研判需要多维度专业能力协同：关联历史事件、评估风险等级、生成处置方案、输出分析报告。系统通过 9 个专业 Agent 分工协作，每个 Agent 专注特定领域，通过意图路由实现智能分发。

#### 主要流程

```
用户消息 → 意图识别 (Router LLM) → 路由决策
                                      ↓
                    ┌─────────────────┴─────────────────┐
                    ▼                                   ▼
            标准意图路由                               深度思考模式
            (单一任务)                                (复杂多步骤)
                    ↓                                   ↓
        ┌───────────┴───────────┐                   Plan Agent
        ▼           ▼           ▼                Supervisor-Worker
    Chat Agent  Event Agent  Report Agent             协同调度
    Risk Agent  Solve Agent  Intel Agent  Ops Agent        ↓
    Summary Agent (9个专业Agent)                   Planner 任务分解
                    ↓                                   ↓
            工具调用 + RAG检索 + 联网搜索             Executor 调度执行 (调用 Worker Agent)
            (Intel Agent 调用 web_search)                ↓ 
            流式返回结果                            Replanner 评估决策
                                                        ↓
                                                   循环反馈直至完成
```

**核心特性**

- **意图自动识别**：Router LLM 快速识别用户意图，支持 chat/event/report/risk/solve/intel/ops 七类任务分类
- **容错降级机制**：识别失败或置信度不足时自动降级到 Chat Agent 兜底，保证系统可用性
- **工厂模式管理**：所有 Agent 通过单例工厂统一管理，使用 sync.Once 保证线程安全的懒初始化
- **工具按需加载**：全局工具注册表支持按名称获取工具子集，包含事件查询、报告生成、情报搜索、系统工具、智能运维五大类共17个工具

---

### 2. 深度思考模式（Plan Agent Supervisor-Worker）

复杂安全任务往往需要多个 Agent 协同完成，如"分析近期高危事件、评估风险、生成处置报告"。深度思考模式通过 Plan-Execute-Replan 循环架构，实现多步骤任务的自动规划与执行。

#### 主要流程

```
用户请求 (deep_thinking=true)
        ↓
    Planner 任务分解 (reasoning Profile)
    - 分析用户意图和复杂度
    - 拆解为多个子任务步骤
    - 生成执行计划
        ↓
    Executor 调度执行 (default Profile)
    - 根据计划调用 Worker Agent:
      · event_analysis_agent (事件分析)
      · report_agent (报告生成)
      · risk_assessment_agent (风险评估)
      · solve_agent (解决方案)
      · intel_agent (联网搜索)
      · ops_agent (智能运维触发)
    - 注入历史上下文 (进程内 SessionMemory)
        ↓
    Worker 执行任务
    - RAG 检索相关知识
    - 联网搜索最新情报
    - 调用专业工具
    - 返回执行结果 (自动截断防溢出)
        ↓
    Replanner 评估决策 (reasoning Profile)
    - 检查任务完成度
    - 判断是否需要继续
    - 决策: 继续执行 / 终止返回
        ↓
    循环反馈直至完成
```

**核心特性**

- **Supervisor-Worker 架构**：Planner 负责任务分解和步骤规划，Executor 调度 Worker 工具执行具体任务，Replanner 评估执行结果并决策下一步
- **Worker 上下文隔离**：每个 Worker 通过独立 context 派生，避免 Eino compose state 冲突，保证执行环境干净
- **历史上下文传递**：从进程内 SessionMemory 提取最近对话历史注入查询，Worker 能感知对话背景，零 Redis 开销
- **输出截断防溢出**：Worker 返回值自动截断，防止上下文爆炸影响后续推理质量

---

### 3. 联网威胁情报（Intelligence Agent）

传统 RAG 仅能检索已有知识，面对新出现的 CVE、零日漏洞、最新攻击组织动态时存在盲区。Intelligence Agent 通过集成联网搜索能力，实现实时威胁情报获取与沉淀。

#### 主要流程

```
Intelligence Agent (ReAct + 联网搜索)
        ↓
    判断是否需要联网搜索
    - 检查前端联网开关状态
    - 评估查询是否需要实时信息
        ↓
    web_search 工具调用
    - Tavily API 深度搜索模式
    - 搜索最新 CVE、零日漏洞、攻击组织动态
    - 返回高质量搜索结果
        ↓
    LLM 分析总结
    - 提取关键信息 (漏洞详情/影响范围/修复方案)
    - 评估信息可信度
    - 生成结构化情报摘要
        ↓
    save_intelligence 工具调用
    - 写入 MySQL events 表
    - 标记来源为联网搜索
    - 记录时间戳和元数据
        ↓
    异步 Indexer 向量化
    - 后台定时任务触发
    - qwen3.7-text-embedding 向量化
    - 写入 Milvus (events 分区)
        ↓
    下次检索可直接命中
```

**核心特性**

- **专业搜索引擎集成**：接入 Tavily Search API，支持深度搜索模式，结果质量优于通用搜索引擎
- **情报自动沉淀**：Agent 分析总结的情报自动写入数据库，触发异步向量化，下次检索可直接命中
- **联网开关控制**：前端提供联网搜索开关，关闭时工具返回提示而不执行实际搜索，避免不必要的 API 消耗
- **职责单一设计**：专注"联网采集 → 分析 → 沉淀"流程，不与本地事件工具混用，避免角色混淆

---

### 4. RAG 增强检索管道

简单的向量检索难以满足生产需求。系统构建了完整的检索管道，涵盖查询优化、混合检索、精排过滤和缓存策略，系统性解决检索质量问题。

#### 主要流程

```
用户查询
        ↓
    查询优化 (Normalize/Rewrite/Split)
    - 术语归一化 (统一专业术语)
    - 消除代词歧义 (补全上下文)
    - 拆分子问题 (复杂查询分解)
        ↓
    向量嵌入 (qwen3.7-text-embedding)
    - 生成 2048 维稠密向量
    - 估算 Token 成本
        ↓
    语义缓存检查 (Redis)
    - 计算余弦相似度
    - 阈值 >= 0.85 命中缓存
    - TTL 24 小时
        ↓ (未命中)
    混合检索 (Milvus)
    - Dense 路径: 稠密向量 + COSINE 相似度
    - Sparse 路径: BM25 稀疏向量 + IP 内积
    - RRF 融合排序 (k=60)
    - 多路并行检索 (子查询并发)
        ↓
    Rerank 精排 (qwen3-rerank)
    - 精排模型重新打分
    - 提升 Top 结果质量
        ↓
    过滤截断
    - MinScore 过滤低分文档
    - FinalTopK 截断控制数量
        ↓
    返回文档上下文 (送入 LLM)
```

**核心特性**

- **查询优化三段式**：术语归一化消除歧义，查询重写补全上下文，子问题拆分提升召回完整性
- **混合检索策略**：稠密向量捕捉语义相似性，稀疏向量精确词匹配，RRF 算法自动融合排序无需手动调权
- **语义缓存加速**：Redis 缓存检索结果，相似查询直接命中跳过向量检索，显著降低延迟
- **Rerank 精排提升**：精排模型重新打分，提升 Top 结果质量，过滤低分文档控制上下文质量

---

### 5. 知识库管理

安全团队积累的内部知识需要系统化管理，才能在 RAG 检索时精准命中。系统支持多格式文档上传、智能分块策略和全生命周期管理。

#### 主要流程

```
Web 上传文件
        ↓
    文件保存到本地存储
        ↓
    Worker Pool 异步处理
        ↓
    文档解析 (PDF/DOCX/Markdown/Code)
        ↓
    智能分块策略选择
    - Markdown → structure_aware (保留结构)
    - 其他格式 → hierarchical (父子分块)
    - 代码文件 → code (语法感知)
        ↓
    向量化 (qwen3.7-text-embedding)
        ↓
    Milvus 索引写入 (documents 分区)
        ↓
    状态更新 (pending → indexed)
```

**核心特性**

- **多知识库隔离**：支持创建多个知识库，通过元数据字段逻辑隔离
- **智能分块策略**：基于文件类型自动选择，父子结构感知分块，滑动窗口分块，代码语法感知分块
- **多格式解析器**：支持 PDF/DOCX/Markdown/代码文件，三阶段 fallback 处理异常文档
- **全生命周期管理**：状态追踪，支持重建索引

---

### 6. 多源情报订阅、自动抓取与告警接入

安全运营需要持续获取最新威胁情报，同时接收外部安全设备（WAF、SIEM、IDS/IPS）产生的实时告警。系统通过订阅机制自动抓取多源情报，并提供标准化的多格式告警接入通道，统一归一化后写入事件库。

#### 主要流程

```
┌─ 被动订阅抓取 ──────────────────────────────────────────────┐
│  Scheduler 定时调度 (goroutine 后台运行)                     │
│  RSS Fetcher / GitHub Fetcher 并行抓取                      │
│  - RSS 订阅源 (厂商安全公告)                                 │
│  - GitHub Security Advisories (开源漏洞)                    │
│  内容解析与提取 (标题/描述/CVE/发布时间)                      │
└─────────────────────────────────────────────────────────────┘

┌─ 主动告警接入 ──────────────────────────────────────────────┐
│  外部安全设备 / SIEM → X-API-Key 认证                        │
│  ├─ POST /api/ingest/v1/push    — 标准化 REST（系统推荐）    │
│  ├─ POST /api/ingest/v1/webhook — 通用 JSON（Splunk/Elastic）│
│  ├─ POST /api/ingest/v1/cef     — ArcSight CEF（text/plain）│
│  └─ POST /api/ingest/v1/leef    — IBM QRadar LEEF           │
│  格式解析与字段归一化                                        │
│  - Webhook: 多字段别名 fallback (title/name/alert_name)     │
│  - CEF: Header + Extension，severity 数字→等级映射          │
│  - LEEF: Tab 分隔属性，提取 eventID/sev/msg                 │
└─────────────────────────────────────────────────────────────┘

          ↓ (两路汇聚)
    去重检查 (SHA256 内容指纹)
    - 计算 SHA256(title|source|content[:200])[:32]
    - 重复数据返回 is_new=false，不重复写入
          ↓
    MySQL 入库 (event_type 标记渠道来源)
    - rss / github / webhook / cef / leef / api_push
    - 保存原始 raw_payload 供审计
          ↓
    Indexer 批量向量化 (定时触发)
    - qwen3.7-text-embedding 向量化 → Milvus (events 分区)
    - 写入稠密向量 + 稀疏向量 (BM25)，供 RAG 检索
```

**核心特性**

- **双路情报汇聚**：被动订阅（RSS/GitHub 定时抓取）与主动接入（外部设备推送）两路并行，覆盖情报来源
- **四种接入格式**：API Push、Webhook、CEF（ArcSight）、LEEF（IBM QRadar），兼容主流安全设备
- **独立 API Key 认证**：接入端点使用 `X-API-Key` 请求头，与 JWT 用户认证隔离，支持在管理界面查看和重置
- **SHA256 内容去重**：统一基于内容指纹去重，避免订阅抓取与主动推送产生重复数据
- **渠道标签追踪**：事件列表显示来源渠道标签，接入历史面板展示最近 10 条外部接入记录
- **异步向量化**：新事件入库后批量向量化，不阻塞主流程

---

### 7. 全链路可观测性（Trace）

AI 系统问题定位困难，需要全链路追踪每个环节的耗时、成本和质量指标。系统通过多层埋点实现完整的可观测性。

#### 主要流程

```
Controller 接收请求
        ↓
    StartRun 创建追踪会话 (trace_id/session_id)
        ↓
    Eino Callbacks 自动埋点
    - LLM 调用 (模型名/tokens/耗时)
    - TOOL 调用 (工具名/参数/结果)
    - RETRIEVER 检索 (召回数/相似度)
    - EMBEDDING 向量化 (tokens/成本)
        ↓
    手动 Span 埋点
    - AGENT 执行 (意图/状态)
    - CACHE 操作 (命中/未命中)
    - RERANK 精排 (分数分布)
        ↓
    GORM Plugin 自动埋点
    - DB 慢查询检测 (阈值/SQL语句)
        ↓
    异步批量写入 MySQL
    - agent_trace_runs (请求级汇总)
    - agent_trace_nodes (节点级明细)
        ↓
    FinishRun 计算总耗时和成本
```

**核心特性**

- **两级数据结构**：请求级记录总耗时和成本，节点级记录每个处理环节的详细信息
- **多种埋点方式**：Eino callbacks 自动埋点，手动 Span 包装，GORM Plugin 拦截数据库操作
- **检索质量指标**：记录召回文档数、相似度分数、Rerank 分数，量化检索质量
- **成本估算**：根据 Token 消耗和模型单价计算成本，支持多模型成本分布分析

---

### 8. RAG 质量评估

追踪系统告诉你每次请求发生了什么，RAG 质量评估模块告诉你系统整体表现如何。

#### 主要流程

```
agent_trace_runs/nodes 数据源
        ↓
    KPI 指标聚合计算
    - 成功率 (status='success' 占比)
    - 平均延迟 & P95 延迟
    - 平均召回文档数
    - 平均最高相似度分
    - 缓存命中率 (CACHE 节点统计)
    - 平均 Rerank 分数
        ↓
    模型成本分布统计
    - LLM 模型 (Qwen3.7 Max default/reasoning Profile)
    - Embedding 模型 (qwen3.7-text-embedding)
    - Rerank 模型 (qwen3-rerank)
    - 按模型聚合 Token 消耗和成本
        ↓
    用户反馈收集
    - 点赞/点踩写入 message_feedbacks
    - 按 (session_id, message_index) 唯一标识
        ↓
    ECharts 可视化渲染
    - 响应耗时趋势图
    - 检索得分趋势图
    - 成本分布饼图
        ↓
    链路关联查询
    - 按 session_id 筛选
    - 点击跳转 Trace 详情
```

**核心特性**

- **KPI 仪表盘**：聚合成功率、延迟、召回文档数、相似度分数、缓存命中率等关键指标
- **模型成本分布**：按模型名称统计 Token 消耗和成本占比，支持多模型成本分析
- **用户反馈系统**：聊天界面支持点赞/点踩，统计满意度趋势
- **链路关联**：按 session_id 筛选具体链路，点击查看 Trace 详情定位根因

---

### 9. 分层记忆管理

对话历史全量送入 LLM 会导致 Token 快速增长和成本飙升。系统通过三层记忆架构，在控制成本的前提下保留关键上下文，并结合用户偏好隐式推断实现个性化响应。

#### 会话记忆流程

```
用户消息写入 SessionMemory（进程内）
         ↓
  双触发器检查
  ├─ Token 数 > 3000  主触发，动态计算最小裁剪数
  └─ 消息数 > 30      辅触发，固定批次 10 条
         ↓ 异步
  Summary Agent 压缩
  提取头部旧消息 → LLM 生成摘要 → 裁剪头部 → 更新 LongTermSummary
         ↓ 持久化到 Redis（TTL 30 天）
  session:{id}:recent   短期消息列表
  session:{id}:summary  长期摘要文本
         ↓ 注入 Prompt
  [摘要消息, recent 消息...]  → LLM
```

#### 用户偏好更新流程

```
偏好信号来源
  ├─ 对话行为：消息数 ≥ N 时触发
  └─ 点踩反馈：用户点踩时携带原因标签
         ↓
  按信号类型处理
  ├─ 明确标签（too_verbose/too_brief/too_technical/not_technical_enough）
  │    → 直接写对应偏好字段，零 LLM 开销
  ├─ 质量标签（inaccurate/off_topic）
  │    → 内容问题，不影响偏好
  └─ 对话行为 / 无标签
       → LLM 从对话中提取用户背景
         ├─ 有旧偏好 → 合并更新（保留稳定特征，修正变化部分）
         └─ 无旧偏好 → 首次提取
         ↓
  写入 MySQL user_preferences → 失效进程内缓存（5min TTL）
         ↓ 每次 SubAgent 执行前读取
  task.Query = "【用户偏好】..." + task.Query
```

**核心特性**

- **三层记忆架构**：进程内 SessionMemory → Redis → MySQL 用户偏好（跨会话长期记忆）
- **双触发器压缩**：Token 数主触发，消息数辅触发
- **用户偏好**：显式反馈与隐式推断持续更新用户偏好，提升回答质量

---

### 10. 智能运维

安全事件发生后，人工响应流程繁琐且容易遗漏。智能运维模块通过 AI Agent 自动完成事件分析、IP 封禁、多渠道通知和状态更新，将响应时间从小时级压缩至秒级。

#### 主要流程

```
安全事件触发
        ↓
    事件分析 Agent（EventAnalysis Pipeline）
    - RAG 检索相关历史事件和知识库
    - 分析攻击手法、攻击源、攻击目标
    - 评估风险等级，生成处置建议
        ↓
    Ops Agent（ReAct 推理 + 工具调用）
    - 依据分析结论自动决策执行顺序
    ├─ update_event_status → 将事件状态更新为 processing
    ├─ block_ip → 封禁明确恶意 IP（受保护资产自动跳过）
    └─ 多渠道通知（并行）
       ├─ notify_dingtalk → 钉钉 Webhook 推送
       ├─ notify_wecom   → 企业微信 Webhook 推送
       └─ notify_email   → SMTP 邮件发送
        ↓
    执行结果持久化
    - ops_runs 记录任务状态（running/success/failed）
    - ops_run_steps 记录每步骤输出、耗时、重试次数
        ↓
    前端实时轮询展示
    - 运维任务列表（状态/耗时/触发事件）
    - 展开查看步骤明细（动作类型/输出/状态）
    - 统计大盘（总执行次数/成功/失败/成功率）
```

**核心特性**

- **AI 自主决策**：Ops Agent 基于事件分析结论自主判断执行哪些动作、以何种顺序执行，无需预定义规则
- **受保护资产白名单**：维护白名单（IP/域名/主机），封禁前自动检查，防止误封关键资产
- **多渠道通知**：支持钉钉、企业微信、邮件三种通知渠道
- **步骤级可观测**：每个执行步骤独立记录状态、输出、错误信息和重试次数，便于排查失败原因
- **统计大盘**：实时聚合总执行次数、成功率等 KPI，快速评估运维自动化效果

---

### 11. API 限流保护


系统使用两层令牌桶限制超额流量，并按业务模块隔离不同成本的请求。

#### 主要流程

```
请求到达
        ↓
    第一层：全局令牌桶（所有用户共享，防止服务整体被打爆）
    └─ 超出 rate_global_qps → 429 "服务繁忙，请稍后重试"
        ↓
    第二层：模块级用户令牌桶（按路径归属模块 + 用户 ID 隔离）
    └─ 超出模块 QPS → 429 "请求过于频繁，请稍后重试"
        ↓
    放行 → 业务处理或模型调用
```

**核心特性**

- **差异化限速策略**：按业务模块独立配置 QPS 上限，LLM 密集型接口与只读查询接口隔离限速，避免低优先级流量挤占高价值资源
- **令牌桶突发容忍**：burst 容量设为 ceil(QPS)，令牌积累后可瞬间消耗，在保证平均速率的同时兼容合理的流量突发

---

### 12. 结构化报告生成

安全团队每周/每月需要输出安全报告，手工整理数据、写分析、排版耗时巨大。Report Agent 自动完成这个过程。

#### 主要流程

```
Report Agent (ReAct + RAG)
      ↓
query_events → 获取时间范围内事件数据
      ↓
query_report_templates → 获取报告模板
      ↓
RAG 检索 → 相关历史事件和内部文档
      ↓
LLM 生成 → 结构化报告内容
      ↓
create_report → 持久化到 MySQL reports 表
```

**核心特性**

- **多类型报告**：支持周报、月报、自定义报告，对应不同的时间范围和分析维度
- **工具链协同**：通过工具获取事件数据和模板，ReAct 推理生成内容，自动持久化
- **历史报告检索**：支持按时间范围、类型过滤查询历史报告，参考历史写法和结构
- **RAG 增强**：通过 Milvus 检索相关历史事件和内部文档，保证报告内容有据可查

---

### 13. SSE 流式输出

AI 分析可能需要数十秒才能完成，如果等分析完再一次性返回，用户体验极差。系统所有 AI 分析过程都通过 Server-Sent Events 实时推流，用户可以看到推理步骤和工具调用过程。同时通过工作流持久化机制解决了并发写入冲突、会话回滚和断线续传三个核心问题。

#### 主要流程

```
Controller 接收请求
        ↓
    创建 workflow_runs 记录，获取 run_id
        ↓
    推送 meta 事件 (sessionId / run_id / timestamp)
        ↓
    启动心跳 goroutine（每 15s 推送 keepalive 注释行，防代理超时断连）
        ↓
    推送 status 事件 (Agent 路由状态)
        ↓
    流式推送内容块 (chat/event/report/risk/solve/intel/plan/plan_step)
    每条事件通过 atomic.Int64 原子递增 seq，落库到 workflow_events
        ↓
    推送 done 事件 (流结束)，更新 workflow_runs 状态
```

#### 断线续传流程

```
首次请求：
    前端收到每条 SSE 事件 → 将 run_id + seq 存入 sessionStorage

断线重连：
    前端携带 run_id + last_seq 重新请求
        ↓
    后端 SELECT * FROM workflow_events WHERE run_id=? AND seq > last_seq
        ↓
    replayWorkflowEvents 补发缺失事件给前端
        ↓
    Agent 不重新执行，前端内容无缝续接
```

**核心特性**

- **多类型事件**：meta / status / 内容流 / plan_step / error / done
- **断线续传**：事件实时落库，重连携带 `run_id + last_seq`，后端补发缺失事件
- **会话回滚**：Plan Agent 失败时回滚本轮 UserMessage，同步截断进程内存和 Redis

---
## 技术栈

| 层次 | 技术 | 说明 |
|------|------|------|
| 后端框架 | GoFrame v2.7.1 | HTTP 服务器、配置管理、日志 |
| AI 编排 | Cloudwego Eino | 多 Agent 管道编排、ReAct、Graph、Plan-Execute-Replan |
| 对话模型 | Qwen3.7 Max | 主要推理与分析（OpenAI 兼容接口） |
| 嵌入模型 | DashScope qwen3.7-text-embedding | 向量嵌入 |
| Rerank 模型 | DashScope qwen3-rerank | 检索结果重排序精排 |
| 联网搜索 | Tavily Search API | 专为 AI Agent 设计的搜索接口 |
| 关系数据库 | MySQL 8.0+ | 事件、订阅、报告、用户、知识库、追踪持久化 |
| 向量数据库 | Milvus 2.x | 事件/文档向量检索（RAG） |
| 缓存 | Redis 7.x | 语义缓存 + 对话历史 + 会话摘要 |
| 前端 | React 18 + TypeScript + Vite | 现代化 Web 界面 |
| Web 网关 | Nginx 1.27 | 统一入口、反向代理前后端、SSE 转发、IP 黑名单封禁 |
| 容器编排 | Docker Compose | 前后端分离部署与基础设施编排 |
| 状态管理 | Zustand | 前端全局状态 |
| 样式 | TailwindCSS | 响应式 UI |
| 图表 | ECharts | 趋势图、分布图 |

---

## 快速开始

### 前置条件

- Go 1.27.0
- Node.js 18+ / npm
- MySQL 8.0+
- Redis 7.x
- Milvus 2.x

### 1. 启动依赖服务

```bash
docker compose -f manifest/docker/docker-compose.dev.yml up -d
```

### 2. 配置

复制并编辑配置文件：

```bash
cp manifest/config/config.yaml manifest/config/config.local.yaml
```

`config.local.yaml` 是完整配置替代文件，不会与 `config.yaml` 合并。配置只保存
`env:` / `file:` Secret 引用；解析后的值不得写入配置、日志或持久化对象。该文件和
`manifest/config/.secrets/` 均被 Git 忽略，禁止打印、回传或提交 Secret。

开发环境默认运行 `all`；也可显式运行 `go run . api` 或 `go run . worker`。生产配置只允许
`api` / `worker`，并要求数据库、JWT、初始管理员和当前 Provider 的环境 Secret 非空且不是默认值。

模型配置采用 Provider → Model Catalog → Routing 三层结构。仓库内的
`aliyun_bailian` 是当前开发示例，并非代码固定值：

- `routing.chat.default`：`glm-5`，`enable_thinking=false`
- `routing.chat.reasoning`：同一模型，`enable_thinking=true`
- `routing.embedding.default`：`qwen3.7-text-embedding`，固定 2048 维
- `routing.rerank.default`：`qwen3-rerank`

北京端点为：Chat / Embedding 使用
`https://dashscope.aliyuncs.com/compatible-mode/v1`，Rerank 使用
`https://dashscope.aliyuncs.com/compatible-api/v1`。

价格按中国内地北京地域、人民币付费价估算，忽略免费额度，计价单位均为每百万 Token：Chat
输入 ¥12、缓存输入 ¥2.4、输出（含推理）¥36；Embedding 输入 ¥0.5；Rerank 输入 ¥0.5。
来源为[阿里云百炼模型价格](https://help.aliyun.com/zh/model-studio/model-pricing)，核对日期：2026-08-24。
缓存 Token 已包含在输入 Token 中，推理 Token 已包含在输出 Token 中，成本不会重复累计。

在线测试默认跳过。填写本地 Key 后显式运行：

```bash
SENTINELOPS_ONLINE_TEST=1 GOTOOLCHAIN=go1.27.0 go test ./internal/ai/models ./internal/ai/embedder ./internal/ai/rerank
```

未来新增供应商时，在 `providers` 增加该供应商实际使用的协议端点，在 `model_catalog` 增加
`provider/model` 形式的模型引用，再调整 `routing`。配置校验只要求该 Provider 的模型 Driver
实际使用的端点；业务代码只使用 Profile，并从模型引用动态解析 API Key、端点和厂商模型 ID。

### 3. 启动后端

```bash
GOTOOLCHAIN=go1.27.0 go run .
# 服务运行在 http://localhost:8001
```

### 4. 启动前端

```bash
cd web
npm install
npm run dev
# 前端运行在 http://localhost:5173
```

## 部署

### Docker 一键部署（推荐）

当前采用前后端分离部署，Nginx 作为统一入口网关，一条命令可启动基础设施、后端、前端和网关服务。

**容器组成：**
- `backend`：GoFrame 后端服务（容器内 `:8001`）
- `frontend`：前端静态资源服务
- `nginx`：统一入口、反向代理、SSE 转发、IP 黑名单封禁
- `mysql` / `redis` / `milvus(standalone)` / `etcd` / `minio` / `attu`

```bash
# 首次部署前生成 vendor 目录
go mod vendor

cd manifest/docker
bash docker.sh
```

启动完成后访问：
- 前端界面：http://localhost
- OpenAPI：http://localhost/api.json
- Swagger API：http://localhost/swagger
- Attu（Milvus 管理）：http://localhost:8000

### 开发环境

```bash
# 仅启动基础设施
docker compose -f manifest/docker/docker-compose.dev.yml up -d

# 启动后端（默认 http://localhost:8001）
go run main.go

# 启动前端（另开终端，默认 http://localhost:5173）
cd web && npm install && npm run dev
```

### 生产部署建议

- 修改 `config.docker.yaml` 中的默认管理员密码和 JWT Secret
- 配置 HTTPS 反向代理（Nginx）
- 配置 Redis 持久化
- 使用 Milvus 生产集群
- 监控 LLM API 配额

## 许可证

MIT License — 详见 [LICENSE](LICENSE) 文件

---

## 贡献

欢迎提交 Issue 和 Pull Request。提交代码前请确保：

```bash
go fmt ./...      # 格式化代码
go vet ./...      # 静态检查
go test ./...     # 运行测试
```
