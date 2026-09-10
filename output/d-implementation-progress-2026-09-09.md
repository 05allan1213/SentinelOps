# D 阶段实施进度

范围：D-01、D-02、D-03、D-04、D-05、D-06、D-07；完成后停止，不进入 E/F。

基线：`c0b819158fad3fbe02132050d47c9261b65d1a4a`。独立分支 `feat/phase-d-20260908`，工作树 `/home/monody/project/.worktrees/sentinelops-d`。原仓库 `/home/monody/project/SentinelOps` 的未跟踪 `grill-truth.md` 保留。

## 前置确认

- B1 六个任务和 H 三个任务的完成提交均属于当前基线历史；核对了既有完成账本、后续 B1 修复、Runtime 服务/控制器与测试。
- 已自行启动既有隔离 MySQL，并仅向测试子进程设置 DSN。`go test -p 1 ./api/runtime/... ./internal/service/runtime ./internal/controller/runtime ./internal/controller/chat -count=1 -timeout=30m`：PASS。
- H 中缺少真实 Agent Eval、gray/rollback 事实时的 `unavailable/not_run` 是既定契约，不作为 D 阶段阻塞。

## 任务结果

| Task | 状态 | 提交与证据 |
|---|---|---|
| D-01 | PASS | `968d2c5`、`14e2022`；单元循环、精确依赖、registry-neutral lockfile；一次修复复审通过 |
| D-02 | PASS | `eb232a5`、`a6b5ddd`；URL/图片/代码安全与围栏辅助；111 聚焦测试通过，一次修复复审通过 |
| D-03 | PASS | `f8f61ac`、`ba71dea`；共享渲染器、受限语言包；144 全量单测、3 浏览器用例初轮通过；修复后 37 聚焦单测及 1 新剪贴板浏览器用例通过，复审通过 |
| D-04 | PASS | `70c2de7`；8 处调用迁移、原文保留；166 单测通过、review 批准 |
| D-05 | PASS | `3c9db3e`、`cbc2f5e`；消息节流、围栏与终态/滚动稳定；187 单测及 6 浏览器用例通过，修复复审通过 |
| D-06 | PASS | `29d2011`、`b63d390`；单调游标、GET 有限重连、可见性与清理；38 SSE 用例通过，修复复审通过 |
| D-07 | PASS | `1f7acf1`、`562a6c9`；v2-first 创建、GET 恢复、去重和真实状态映射；245 单测、22 浏览器用例通过，一次修复复审通过 |

## 已知基线与验证边界

- D-01 的旧 Playwright smoke 实跑为 2 PASS / 1 FAIL：dark fixture 找不到“思考链路”；与原基线一致，归属 F-02，本轮不扩大范围修复。
- 仓库 lint 为 0 errors / 68 既有 warnings，build 保留已有大 chunk 提示；D-02 触及文件的 ESLint 无输出。
- 后续浏览器 fixture 与真实 Worker/Provider 证据分别记录，不将前者当作真实 Runtime/Effect 成功。
- 最终验证：`npm run test:unit` 245 PASS；`npm run lint` 0 errors / 68 既有 warnings；`npm run build` PASS；受控桌面 Playwright 22/22 PASS；受限高亮语言与 registry-neutral lockfile 核对 PASS；`go test ./internal/controller/chat -count=1` PASS。
- 未实现门禁：真实 Worker/Provider E2E、E、F、Hosted CI、应用镜像、rollout/rollback、P43：NOT RUN。
