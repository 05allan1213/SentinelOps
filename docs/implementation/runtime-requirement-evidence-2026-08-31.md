# Runtime requirement evidence — Phase F

日期：2026-09-12。基线 E：`a1e80e7`；分支 `feat/phase-f-20260911`。本文件冻结 F-01、F-02、F-03、F-05、F-06、F-07、F-08、F-09、F-10 的交付证据。

最终状态：指定 F 阶段的实现、审查修复、桌面验收与证据整理已完成。UTC 配置下六个后端聚焦包全部 PASS；矩阵包含 51 项本地验收 PASS、1 项 NEEDS PRODUCT DECISION、1 项 OUT OF SCOPE。外部门禁按下文范围单独记录，不能据此宣称全项目生产就绪。

## 判定范围

矩阵结果限定为本计划的本地公共契约、受控桌面 UI 和明示的真实宿主用例。不会把只读缺失状态的正确显示等同于 Eval/Release/rollout 能力已经执行。Hosted CI、真实灰度/回滚、完整 P43、实际 L1/L2 外部 Effect 执行均未在本阶段验收；无移动端或应用镜像验收。

G1：`TZ=UTC SENTINELOPS_TEST_DSN=<隔离MySQL且loc=UTC> go test -p 1 ./api/runtime/... ./internal/service/runtime ./internal/controller/runtime ./internal/dao/mysql ./internal/ai/workflow ./internal/ai/runtime -count=1 -timeout 30m`。日志 `output/f-evidence-2026-09-11/backend-final-utc.log`。

G2：`npm run test:unit`、`npm run lint`、`npm run build`。日志 `final-unit.log`、`final-lint.log`、`final-build.log`，同一证据目录。

G3：`SENTINELOPS_E2E_BASE_URL=http://127.0.0.1:5174 npx playwright test --workers=1`；完整受控 UI 154/154 PASS，日志 `f09-browser-suite.log`。Chromium 1280/1440 两项目；原有显式尺寸循环保留，故部分场景在两个项目重复执行，不能解释为 154 个互异业务场景。

G4：同一宿主端口，加 `SENTINELOPS_LIVE_AUTH_FILE=<真实登录JSON文件>` 执行 `npx playwright test tests/ui/runtime-regression.spec.ts tests/ui/runtime-visual-states.spec.ts --workers=1`。无 live auth 时真实用例明确 skip；本阶段已配置真实服务执行，不以 skip 计为 PASS。最终日志 `f09-live-pass.log`；截图、脱敏事实与 Recovery JSON 位于 `output/playwright/runtime-{1280,1440}/live/`。

下表 Test command/case 中 G1/G2/G3/G4 指以上完整可复现命令。每行 Source evidence 是当前真实文件；commit SHA 列将在 F-09 提交后冻结。

| Requirement ID | Task ID | Test command/case | Source evidence | Acceptance result (PASS/FAIL/NOT RUN) | commit SHA |
| --- | --- | --- | --- | --- | --- |
| R01 | B0-01, B0-03, B1-01, B1-02, B1-06, E-01, E-03, E-04, F-09 | G1 + G3 `runtime-runs.spec.ts` + G4 | `internal/service/runtime/service.go` | PASS | 85d23fa |
| R02 | C-01, C-02, B1-03, E-05, F-09 | G1 + G3 `runtime-recovery.spec.ts` + G4 | `internal/ai/workflow/attempts.go` | PASS | 85d23fa |
| R03 | C-01, C-05, B1-02, H-02, E-04, E-07, F-09 | G1 + G3 `runtime-capabilities.spec.ts` + G4 | `internal/ai/runtime/worker_snapshot.go` | PASS | 85d23fa |
| R04 | C-02, C-05, C-06, B1-02/B1-03, E-04/E-05, F-09 | G1 + G3 `runtime-recovery.spec.ts` + G4 | `internal/ai/workflow/lease.go` | PASS | 85d23fa |
| R05 | C-04, C-06, B1-03, E-05, F-09 | G1 + G3 `runtime-recovery.spec.ts` + G4 | `internal/ai/workflow/eino_checkpoint_store.go` | PASS | 85d23fa |
| R06 | C-03, C-04, C-06, B1-03, B1-06, E-05, D-07, F-09 | G1 + G3 `runtime-regression.spec.ts` + G4 | `internal/ai/workflow/operations.go` | PASS | 85d23fa |
| R07 | B0-03, C-02, C-04, C-05, C-06, B1-02/B1-03, H-01, E-04/E-05/E-07, F-09 | G1 + G3 `runtime-detail-overview.spec.ts` + G4 | `internal/ai/runtime/snapshot.go` | PASS | 85d23fa |
| R08 | B0-02, B1-02, B1-05, E-04, E-06, F-09 | G1 + G3 `runtime-detail-resources.spec.ts` | `internal/service/runtime/evidence_trace.go` | PASS | 85d23fa |
| R09 | B1-02, B1-05, E-06, F-09 | G1 + G3 `runtime-detail-resources.spec.ts` | `internal/ai/workflow/session_revision.go` | PASS | 85d23fa |
| R10 | B0-03, B1-02, B1-04/B1-05, H-01, E-04/E-06/E-07, F-09 | G1 + G3 `runtime-detail-resources.spec.ts` | `internal/service/runtime/safety_resources.go` | PASS | 85d23fa |
| R11 | B0-02, B1-01/B1-02/B1-05/B1-06, C-04, E-01/E-03/E-05, F-09 | G1 + G3 `runtime-recovery.spec.ts` | `internal/service/runtime/contract.go` | PASS | 85d23fa |
| R12 | B0-03, B1-02, B1-06, H-01, E-04/E-07, F-09 | G1 + G3 `runtime-capabilities.spec.ts` | `internal/service/runtime/safety.go` | PASS | 85d23fa |
| R13 | B0-03, B1-02, C-06, H-01, E-04/E-07, F-09 | G1 + G3 `runtime-capabilities.spec.ts` | `internal/ai/runtime/gates.go` | PASS | 85d23fa |
| R14 | H-01, E-07, F-09 | G1 + G3 `runtime-capabilities.spec.ts` | `internal/service/runtime/capabilities.go` | PASS | 85d23fa |
| R15 | C-04/C-06, B1-04, E-05/E-06, F-09 | G1 + G3 `runtime-recovery.spec.ts` | `internal/ai/workflow/approval_lifecycle.go` | PASS | 85d23fa |
| R16 | C-04/C-06, B1-04, E-05/E-06, F-09 | G1 + G3 `runtime-detail-resources.spec.ts` | `internal/ai/workflow/approvals.go` | PASS | 85d23fa |
| R17 | C-03/C-04/C-06, B1-04, E-06, F-09 | G1 + G3 `runtime-detail-resources.spec.ts` | `internal/ai/workflow/effects.go` | PASS | 85d23fa |
| R18 | C-04/C-06, B1-04, E-06, F-09 | G1 + G3 `runtime-detail-resources.spec.ts` | `internal/ai/workflow/reconciliation.go` | PASS | 85d23fa |
| R19 | B1-02, C-06, E-04/E-06, F-09 | G1 + G3 `runtime-detail-overview.spec.ts` | `internal/ai/runtime/budget.go` | PASS | 85d23fa |
| R20 | C-02, B1-02/B1-05, E-05/E-06, F-09 | G1 + G3 `runtime-detail-resources.spec.ts` | `internal/service/runtime/evidence_trace.go` | PASS | 85d23fa |
| R21 | C-02, B1-02/B1-05, E-05/E-06, F-09 | G1 + G3 `runtime-detail-resources.spec.ts` | `internal/service/runtime/evidence_trace.go` | PASS | 85d23fa |
| R22 | B0-02, B1-05, E-06, F-09 | G1 + G3 `runtime-detail-resources.spec.ts` | `internal/service/runtime/evidence_trace.go` | PASS | 85d23fa |
| R23 | C-05, H-01, E-07, F-09 | G1 + G3 `runtime-capabilities.spec.ts` | `internal/service/runtime/capabilities.go` | PASS | 85d23fa |
| R24 | C-05, H-01, E-07, F-09 | G1 + G3 `runtime-capabilities.spec.ts` | `internal/service/runtime/capabilities.go` | PASS | 85d23fa |
| R25 | C-02/C-06, B1-03/B1-04, H-01, E-04/E-06, F-09 | G1 + G3 `runtime-detail-overview.spec.ts` | `internal/ai/runtime/runner.go` | PASS | 85d23fa |
| R26 | B0-03, C-02/C-03/C-06, B1-03/B1-06, E-02/E-04, D-07, F-09 | G1 + G3 `runtime-sse.spec.ts` + G4 | `internal/service/runtime/timeline.go` | PASS | 85d23fa |
| R27 | B1-05, E-06, F-09 | G1 + G3 `runtime-detail-resources.spec.ts` | `internal/service/runtime/evidence_trace.go` | PASS | 85d23fa |
| R28 | H-03, E-07, F-09 | G1 + G3 `runtime-capabilities.spec.ts` | `internal/service/runtime/eval_release.go` | PASS | 85d23fa |
| R29 | H-03, E-07, F-09 | G1 + G3 `runtime-capabilities.spec.ts` | `internal/service/runtime/eval_release.go` | PASS | 85d23fa |
| R30 | H-02/H-03, E-07, F-09 | G1 + G3 `runtime-capabilities.spec.ts` | `internal/ai/runtime/retention.go` | PASS | 85d23fa |
| R31 | C-05, B1-02/B1-06, H-01/H-02, E-02/E-03/E-07, F-09 | G1 + G3 `runtime-regression.spec.ts` + G4 | `internal/ai/runtime/worker_snapshot.go` | PASS | 85d23fa |
| R32 | B0-01/B0-02, C-01, B1-01/B1-02/B1-05, E-01/E-06, F-09/F-10 | G1 + G3 `runtime-regression.spec.ts` + G4 | `internal/controller/runtime/runtime.go` | PASS | 85d23fa |
| F01 | D-01, D-03, D-04, F-09 | G2 + G3 `markdown.spec.ts` | `web/src/components/markdown/MarkdownRenderer.tsx` | PASS | 85d23fa |
| F02 | D-02/D-03/D-04, F-08, F-09 | G2 + G3 `markdown.spec.ts` | `web/src/components/markdown/MarkdownRenderer.tsx` | PASS | 85d23fa |
| F03 | D-02/D-03/D-04, F-09 | G2 + G3 `markdown.spec.ts` | `web/src/components/markdown/MarkdownRenderer.tsx` | PASS | 85d23fa |
| F04 | D-02/D-03, D-04, F-09 | G2 + G3 `markdown.spec.ts` | `web/src/components/markdown/MarkdownRenderer.tsx` | PASS | 85d23fa |
| F05 | D-02/D-03/D-05, F-01, F-07, F-09 | G2 + G3 `layout-regressions.spec.ts` | `web/src/components/markdown/MarkdownRenderer.tsx` | PASS | 85d23fa |
| F06 | D-02/D-03, F-08, F-09 | G2 + G3 `markdown.spec.ts` | `web/src/components/markdown/MarkdownRenderer.tsx` | PASS | 85d23fa |
| F07 | D-02/D-03, F-08, F-09 | G2 + G3 `markdown.spec.ts` | `web/src/components/markdown/MarkdownRenderer.tsx` | PASS | 85d23fa |
| F08 | D-02/D-04, F-10 | G2 + G3 `markdown.spec.ts` | `web/src/components/markdown/MarkdownRenderer.tsx` | PASS | 85d23fa |
| F09 | D-05/D-07, E-02, F-09 | G2 + G3 `chat-streaming-markdown.spec.ts` | `web/src/components/markdown/MarkdownRenderer.tsx` | PASS | 85d23fa |
| F10 | C-07, D-06/D-07, E-02, F-09 | G2 + G3 `chat-reconnect.spec.ts` | `web/src/services/chat.ts` | PASS | 85d23fa |
| F11 | D-06/D-07, E-02, F-09 | G2 + G3 `runtime-sse.spec.ts` | `web/src/hooks/useSSECursor.ts` | PASS | 85d23fa |
| F12 | D-06, E-02, F-09 | G2 + G3 `runtime-sse.spec.ts` | `web/src/hooks/useRuntimeEventTail.ts` | PASS | 85d23fa |
| F13 | D-05/D-07, E-00, F-01/F-07, F-09 | G2 + G3 `chat-streaming-markdown.spec.ts` | `web/src/pages/chat/index.tsx` | PASS | 85d23fa |
| F14 | F-03, F-10 | G2 + G3 `legacy-demo-labels.spec.ts` | `web/src/App.tsx` | NEEDS PRODUCT DECISION | 85d23fa |
| F15 | E-00, F-01, F-07, F-09 | G2 + G3 `layout-regressions.spec.ts` | `web/src/pages/event-analysis/index.tsx` | PASS | 85d23fa |
| F16 | F-03, E-07, F-09 | G2 + G3 `legacy-demo-labels.spec.ts` | `web/src/pages/event-analysis/index.tsx` | PASS | 85d23fa |
| F17 | F-02, H-03, E-07, F-09 | G2 + G3 `rag-eval-resilience.spec.ts` | `web/src/services/rageval.ts` | PASS | 85d23fa |
| F18 | F-02, F-09 | G2 + G3 `../smoke/current-ui.spec.ts` | `web/src/App.tsx` | PASS | 85d23fa |
| F19 | F-03, E-04, F-08, F-09 | G2 + G3 `legacy-demo-labels.spec.ts` | `web/src/components/layout/Layout.tsx` | PASS | 85d23fa |
| F20 | Global constraints, F-09/F-10 | F20 范围核对 | `docs/implementation/phase-f-boundaries-2026-09-11.md` | OUT OF SCOPE | 85d23fa |
| F21 | D-05/D-06, E-00, F-05/F-07/F-08/F-09 | G2 + G3 `runtime-visual-states.spec.ts` | `web/src/assets/styles/index.css` | PASS | 85d23fa |

## 真实链路及边界

- 使用宿主 Go API `127.0.0.1:8001`、独立 Worker 和 Vite；MySQL `127.0.0.1:13307`、Redis、Milvus/etcd/MinIO 仅为依赖容器。5173 被环境占用并拒绝绑定，按计划允许的 `SENTINELOPS_E2E_BASE_URL` 使用 5174；不改产品路由。
- 新建隔离 `sentinelops_f_host` 数据库，执行原 Goose 迁移；测试套件使用要求的 `sentinelops_phase03` 前缀。初次 DSN 名称不合法、时区不一致及退出的 Vite 记录均保留为环境失败，不计入通过。
- 通过现有 admin Gate API 在隔离库设置 L0 admission + shadow，L1/L2 写入均关闭；记录审计理由，未改 Gate 实现或直接写表绕过授权。MCP/telemetry 在本次隔离配置中关闭，Worker 显示 configured/observed 区别；不宣称真实 MCP/Skill 业务执行。
- 初次“禁止所有工具”的请求不满足 Plan Agent 的规划工具协议，真实 Run 为 failed/no tool call；改用只读规划请求后由真实 Provider/Worker 完成，保留两种实际终态。不能把失败原始记录覆盖成成功。
- 真实 Recovery 暴露 Runtime `WriteStatus` 在 JSON 前写状态文本；F-09 仅改为 `WriteHeader`，全局 ResponseMiddleware 不变。新 HTTP 回归覆盖 202/400/403/404/409/422/503 的单一 envelope。Cancel Operation canonical 终态为 canceled，测试按源码契约检查，未把取消显示成 Run 成功。
- live 用例证明 v2 创建、Worker Attempt、持久化观测、终态事件重放、after_seq 增量为空、同 Session 只有一个 Run、重连前后 Effect 集合不变、Cancel 202/幂等复用/Operation terminal。此 L0 样例无外部 Effect 写入；实际外部 Effect 和 Provider 故障矩阵不能从零 Effect 推断。
- Resume/Replay/Restore 的合法性、fencing、Checkpoint/Eino 行为由 G1 数据库集成测试和 G3 受控 UI 验证；真实 Provider Resume/Restore 本阶段 NOT RUN。

## 未决事项和停止边界

`/logs` 继续 NEEDS PRODUCT DECISION：无路由/导航新增，无文件删除，没有生成产品决定文件。Header 保持 unmounted，理由见 phase-f-boundaries-2026-09-11.md。

F20 移动端和前后端应用镜像 OUT OF SCOPE。Hosted CI、生产部署、rollout/rollback、完整 P43 NOT RUN。可选 H-03 的不存在事实源仍为 unavailable/not_observed/not_run；其正确只读展示为本地 PASS，不表示外部门禁通过。

下一步授权 Task：无。完成 F 后结束本 Goal；不 push、不合并、不自动进入任何后续阶段。若要确认 /logs 产品入口或做上述外部门禁，需要另一个明确任务。

## 分 Task 交付

| Task | 交付 | 本地提交 |
| --- | --- | --- |
| F-01 | Event Analysis 受约束列与 Chat 编辑宽度；已有助手气泡回归保留 | 464ec7a |
| F-02 | RAG payload 校验/未知指标/局部失败保留数据；theme 根节点同步 | 9bc1465 |
| F-03 | legacy/demo 标识、Header unmounted、logs 决策边界 | 3102585 |
| F-05 | 单一 Radix Dialog 1.1.23、焦点/ESC/返回触发器/表单门禁 | b2b7c70 |
| F-06 | React 审查闭环；路由懒加载、错误边界、RAG 竞态与后台行为 | 80c7f80 |
| F-07 | Operate 视觉修复；reason 换行、可读对比、reduced-motion、Operation 重试 | 432f550 |
| F-08 | Guidelines 闭环；键盘 Tabs/skip link/native select/语义链接/复制错误反馈 | 06fb9fa |
| F-09 | 双桌面项目、真实 Runtime 场景、当前 Approval fixture；修复真实 HTTP JSON 前缀 | 85d23fa |
| F-10 | 本文件、case-matrix.json、live-fact-audit.json、scope-audit.json、日志和截图；自动验证脚本 | 证据提交见 git log |

设计审查证据：`output/react-best-practices-review-2026-08-31.md`、`output/impeccable-runtime-review-2026-08-31.md`、`output/web-interface-guidelines-runtime-2026-08-31.md`。各审查初始发现和修复验证均保留；未把自动 detector 当作人工浏览器检查的替代。

已人工查看 1280 长 reason 修复前后和 1440 真实 Attempts 截图；其余逐状态 screenshot/DOM/console/overflow 由浏览器用例检查。全部 164 条已执行用例记录见 `case-matrix.json`，其中完整受控套件 154 条，补充真实链路/视觉状态套件 10 条（有重复状态检查）。

性能：主入口从约 2311 kB / gzip 713 kB 拆到约 299 kB / gzip 94 kB；Markdown 约 237 kB、图表约 1133 kB 按需加载。保留图表 >500 kB 提示，不宣称完成全应用 bundle 优化。

最终自动核查命令：`python3 output/f-evidence-2026-09-11/verify-handoff.py`；检查 53 行唯一性、源码/提交存在、冻结枚举、客户端/路由边界、各日志结果和两尺寸真实事实/截图。未决和范围排除不会被写成 PASS。

控制台补充验收：`node output/f-evidence-2026-09-11/audit-live-console.cjs` 复用已完成真实 Run，22 次只读页面访问 PASS；`live-console-audit.json` 记录每次 document/main 宽度，console error 和 pageerror 均为零。该检查不创建新 Run。

构建溯源：宿主二进制在 HTTP 修复后、F-09 提交前编译，因此 DTO 保留实际 `06fb9fa+dirty` VCS 元数据；产品修复与 85d23fa 一致，不改写观测版本。二进制及关键源码摘要见 `build-inputs.json`。

## 最终门禁结果

| 检查 | 最终结果 | 证据 |
| --- | --- | --- |
| 前端单测 | PASS：16 文件 / 289 用例 | final-unit.log |
| TypeScript + lint | PASS：0 errors / 67 既有 warnings | final-lint.log |
| 前端构建 | PASS；图表大 chunk 提示保留 | final-build.log |
| 完整桌面受控浏览器回归 | PASS：154/154 | f09-browser-suite.log |
| 补充真实链路与视觉状态 | PASS：10/10，包含 4 个真实 API/Worker/Provider/DB 用例 | f09-live-pass.log |
| npm run test:smoke | PASS：6/6，两桌面项目 | final-smoke.log |
| 真实只读控制台/宽度 | PASS：22 次页面访问，零 console/page errors，无 document/main 横向溢出 | live-console-audit.json |
| Runtime API / Service / Controller / DAO / workflow / runtime | PASS：带隔离 DSN，TZ=UTC/loc=UTC；workflow 932.791s，runtime 142.428s | backend-final-utc.log |
| Runtime HTTP 单一 JSON envelope | PASS：202/400/403/404/409/422/503 | f09-http-fixed.log；失败基线 f09-http-baseline.log |
| 53 行矩阵与源码/提交/枚举/范围核查 | PASS | handoff-verification.log |

所有证据日志均在 `output/f-evidence-2026-09-11/`。最初无效 DSN、时区偏移、端口/进程故障、fixture 断言不匹配，以及真实 HTTP 前缀缺陷的原始失败日志保留；以上表格仅描述修复后的最终运行，不抹去历史失败。

环境收尾：验收完成后停止本次宿主 API/Worker/Vite 和五个 `sentinelops-f-*` 依赖容器，保留隔离数据库卷与忽略的本地验证配置/二进制，未删除用户原有文件。详情见 cleanup.json。`grill-truth.md` 保持用户原有未跟踪状态。

日志格式：提交的 `.log` 副本仅移除终端 ANSI 控制序列、CR 和行尾空白，未改写结果或错误文本；逐字原始日志另存 `raw-logs.tar.gz`。全部交付文件摘要见 `artifact-sha256.json`。
