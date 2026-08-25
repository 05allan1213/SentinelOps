# P37 Approval / unknown 前端与假成功清理

- Status: `PASS`
- Started from: `acb80f8` (`main`，开工时工作树干净)
- Spec references: 实施计划 P37；上位 Spec 6.5、6.7、Task 10

## Boundary Audit

- 目标：复用现有 Axios、TanStack Query、authStore、ConfirmDialog 和 RunsPanel，接入 pending Approval、Run/Effect 状态与 admin unknown resolution；在无后端证据时不显示成功；用可控 HTTP mock 验证 UI 状态机。
- 明确非目标：不修改 Go API、Approval/Effect Store、Worker、Migration、Compose、真实后端链、真实 Provider、Playwright Compose 链（留 P38）、Eval、部署或远程状态。
- 兼容契约：保留 `/ops/v1/approvals`、`/ops/v1/effects/unknown`、`/ops/v1/runs` 现有路径和 payload；复用唯一 Axios 实例、QueryClient、authStore 与现有 ConfirmDialog，不创建 Approval 全局 Store、第二 QueryClient、第二 Axios 或 Effect executor。
- 安全不变量：preparing 不展示；pending 展示 Tool、风险、目标、脱敏 proposal、hash 和 expiry；approve/reject 理由必填且双击只发一个 Mutation；viewer/operator 不显示决定入口；409 already-decided/expired/hash invalidated 显示稳定状态；parked/reconciling/unknown 轮询或刷新不得变成成功；admin unknown resolution 只提交决定，不执行 Effect；ActionSandbox 与 MitigationConsole 仅 Proposal Preview。
- 预计修改：`web/src/services/api.ts`、`web/src/services/ops.ts`、`web/src/components/common/ConfirmDialog.tsx`、`web/src/pages/dashboard/components/ActionQueue.tsx`、`web/src/pages/ops/RunsPanel.tsx`、`web/src/pages/ops/index.tsx`、`web/src/pages/event-analysis/components/ActionSandbox.tsx`、`web/src/pages/event-analysis/components/MitigationConsole.tsx`、`web/tests/ui/approval-ui.spec.ts`；本证据文件与执行计划台账。
- 验证方式：`cd web && npm run lint && npm run build && npx playwright test tests/ui/approval-ui.spec.ts --project=chromium`；补充源码扫描确认单一 Axios/QueryClient/authStore、无假执行成功。
- 回滚方式：通过本单元独立本地提交的普通反向提交恢复；不修改后端数据库、Compose 或 remote。

## Build-or-Reuse

| 现有能力 | 剩余缺口 | 最薄实现 |
| --- | --- | --- |
| Axios interceptor、TanStack QueryClientProvider、persisted authStore、ConfirmDialog、ops API/controller、RunsPanel polling | 前端没有 Approval/unknown DTO、CAS mutation、角色门控或非终态 UI | 在 `opsService` 增加 typed methods/query keys；ActionQueue 用 `useQuery/useMutation` 直接消费同一 service；RunsPanel 只补状态映射；ConfirmDialog 增加 reason 字段；Preview 组件删除假执行成功 |

## Red evidence

首次运行 `npx playwright test tests/ui/approval-ui.spec.ts --project=chromium` 为 `FAIL`：页面没有 Approval/unknown 数据面板、parked 状态文案和 Preview 接入；该失败命中本单元新增断言。实现后同一测试共 6 个 Case `PASS`。

## Actual files

- `web/src/services/api.ts`：保留同一 Axios，增加带 HTTP status 的 typed `ApiRequestError`，用于稳定处理 409/503。
- `web/src/services/ops.ts`：增加 Approval/UnknownEffect DTO、Query key、list/detail/approve/reject/resolve 方法。
- `web/src/pages/dashboard/components/ActionQueue.tsx`：Query 驱动 pending Approval 与 unknown Effect，角色门控、CAS payload、理由校验、409 稳定状态、admin 三态对账。
- `web/src/pages/ops/{index.tsx,RunsPanel.tsx}`：接入待办面板并区分 waiting/reconciling/parked/success，未知状态不显示成功。
- `web/src/components/common/ConfirmDialog.tsx`：复用同一对话框增加必填 reason。
- `web/src/pages/event-analysis/{index.tsx,components/ActionSandbox.tsx,components/MitigationConsole.tsx}`：接入 Proposal Preview，移除“规则已应用”假成功。
- `web/tests/ui/approval-ui.spec.ts`：可控 HTTP mock-route UI 状态机测试。

## Verification ledger

| Gate | Command | Result |
| --- | --- | --- |
| 前端 lint | `cd web && npm run lint` | `PASS`：TypeScript、ESLint 退出码 0；仅存量 warnings，无 errors |
| 前端 build | `cd web && npm run build` | `PASS`：Vite 生产构建完成；仅既有 chunk size warning |
| P37 Playwright | `cd web && npx playwright test tests/ui/approval-ui.spec.ts --project=chromium` | `PASS`：6/6；覆盖 preparing、pending facts/hash/expiry、reason、viewer/operator gate、parked、unknown、admin 3-state、409、double-click single POST、poll failure、Preview |
| 差异检查 | `git diff --check` | `PASS` |
| 架构/假成功扫描 | `rg` scoped to P37 files for second Axios/QueryClient/authStore/Approval Store and `规则已应用` | `PASS`：P37 新增代码只复用既有实例；无 Approval 全局 Store；假成功文案只在测试负向断言 |

## Key assertions

- `opsService.listApprovals` 只将 `pending` 暴露给 UI，`preparing` 被过滤；proposal 展示 target、脱敏参数、hash、expiry。
- approve/reject 始终回传当前 proposal hash/version，理由为空不会提交；ConfirmDialog 关闭后 Mutation 仍只发一次，409 映射为已处理/过期/提案失效稳定状态。
- viewer/operator 没有决定按钮；admin 对 unknown Effect 只调用 `/effects/{id}/resolve`，没有 direct run 或 Effect executor 路径。
- `ActionQueue` 查询失败展示“未确认成功或已完成”，不把网络错误渲染为 all-clear；Run `parked`/`reconciling`/`waiting_approval` 与成功分开。
- ActionSandbox 和 MitigationConsole 明确 Proposal Preview，不显示或触发外部 Effect 成功。

## Unfinished / NOT RUN

- P38 真实 Compose + Playwright 审批链：`NOT RUN`，不属于本单元。
- 真实后端、真实 Provider、Hosted CI、全量 Eval、P43：`NOT RUN`。
