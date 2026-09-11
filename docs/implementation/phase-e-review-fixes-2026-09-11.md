# 阶段 E 审查问题修复（2026-09-11）

范围：修复 `e-phase-independent-review-2026-09-11.md` 的全部 6 项发现，保持 E 阶段边界。

分支：`feat/phase-e-20260910`；修复基线：`b8b01a6378e6d8bb85b1c439f10aa399be23eebb`。
工作树：`/home/monody/project/.worktrees/sentinelops-e`。
修复提交：`03ccdad8f24630f848ee2f7dbde95d488be92a04`（`fix(web-runtime): repair phase E review findings`）。
合并：已以 fast-forward 合入本地 `main`（合入点为 `feat/phase-e-20260910` 的本次记录提交）；`origin/main` 未推送。
提交前在同一工作树复跑：`npm run test:unit` 13 文件 / 279 用例 PASS；`npm run build` PASS（保留 >500 kB chunk 提示）；`npx playwright test tests/ui/runtime- --workers=1` 31/31 PASS。

## 修复与证据对应

| 发现 | 修复 | 验证 |
| --- | --- | --- |
| P1：Effect 终态误关 Run SSE | `useRuntimeEventTail` 订阅同一 Run Query，只以服务端当前 Run 状态关流。Effect/Approval 事件刷新其资源及 Run，不能设置 Run 终态。 | `runtime-event-tail.test.ts`：Effect 成功后继续读取 |
| P1：同 Run 恢复与历史 parked 误停流 | 移除永久终态标记；恢复后的服务端 Run Query 更新重新启用 reader，并沿用已确认游标。共享 `streamFetch`/`useSSECursor` 增加默认开启的 `stopOnRunTerminal` 参数，Runtime 传 false，使历史 Run 事件只触发事实刷新。Chat 保留原默认行为。 | 同 Run parked→running 后以 after_seq=1 重连；历史 parked 后继续接收 Agent 事件；原有跨 Run 隔离测试 |
| P2：查询失败伪装为空 | 五个详情列表接入统一错误提示与重试，有旧数据时标记保留旧数据；Timeline 独立展示错误、quality 和 unavailable，删除伪造的 available/complete 默认值。Context 历史展开失败不再导致元数据和收起按钮消失。 | 五类资源错误单测；Timeline 浏览器首次失败→重试成功→刷新失败仍保留旧行 |
| P2：时间筛选格式错误 | 在请求和 Query key 共用的参数归一化边界，将 datetime-local 转为浏览器本地时区对应的 RFC3339 UTC 字符串；带时区 URL 回填为本地控件值。 | Service 请求参数、等价 Query key 单测；浏览器输入→RFC3339 参数→刷新保留筛选 |
| P2：详情只能看第一页 | Timeline、Attempts、Checkpoint、Effects、Evidence、Trace 增加真实分页与 has_next 控制。SSE 接收帧保留在既有 events Query key 下，分页 Timeline 根据事件失效重读，避免将事件无差别插入所有过滤页。 | 1280/1440 浏览器验证六种集合的下一页、最后页禁用、上一页及局部宽度；过滤页缓存不污染单测 |
| P2：第二次 Recovery 终结不刷新 | 将 notified 标记绑定 operation_id；每次不同 Operation 终结都刷新 Run/Attempts/Timeline/Effects/Checkpoint 并调用终态回调。终结图标区分成功和其他终态。 | 连续 op-1/op-2 终结两次通知与资源失效单测；既有 Recovery 浏览器用例 |

## 实现边界

- 继续使用唯一 Axios client、QueryClient 和既有服务端接口；没有新增 Zustand 服务端状态、执行旁路或乐观成功。
- 两个新增组件仅统一详情资源的错误/重试显示和分页控制；没有新依赖或设计系统。
- 共享 SSE 的新参数默认 true；只有 Runtime 的消费者明确禁用按历史事件名关流，Chat 的默认语义不变。
- `[DONE]` 和传输关闭仍只表示连接结束，不能推断 Run 成功；详情显示事件最后更新时间与连接错误/手动重连。
- 未修改后端实现、迁移、依赖或应用配置；不进入 F，不把受控测试当作真实 Worker 执行证据。

## 最终验证

本次审查的 6 项问题已修复，并通过对应的本地回归验证。扩展 Chat 检查出现一次未再次复现的超时，保留以下原始结果，不将首轮整套验证改写为全绿。

| 检查 | 结果 | 证据 |
| --- | --- | --- |
| 前端单测 | PASS：13 文件 / 279 用例 | unit.log |
| lint | PASS：0 errors / 68 warnings | lint.log |
| build | PASS；保留 >500 kB chunk 提示 | build.log |
| Runtime API DTO 包 | PASS：`go test ./api/runtime/... -count=1` | go-api.log |
| Runtime 浏览器回归 | PASS：31/31，1280/1440 | browser.log 中 Runtime 用例全部通过 |
| 扩展 Chat 重连与流式 Markdown | 首轮 19/20 PASS，1 个上传后重连用例 FAIL | browser.log：1280 下等待恢复 SSE URL 超过默认 5 秒 |
| 失败的 Chat 用例单独复跑 | PASS：两尺寸各重复 3 次，6/6 | chat-upload-recheck.log |
| `git diff --check` | PASS | scope.log |
| DSN-backed service/controller 集成测试 | NOT RUN：本次未配置测试 DSN | 本次没有后端实现改动 |
| 真实 Worker/Provider E2E、hosted CI、部署/回滚、F | NOT RUN | 保持原阶段和证据边界 |

首轮联合命令：`npx playwright test tests/ui/runtime- tests/ui/chat-reconnect.spec.ts tests/ui/chat-streaming-markdown.spec.ts --workers=1`，结果 50/51 PASS。
失败项复跑命令：`npx playwright test tests/ui/chat-reconnect.spec.ts -g 'upload preserves' --repeat-each=3 --workers=1`，结果 6/6 PASS。

Chat 页面和该测试文件未修改；共享 SSE 新参数的默认行为仍与修复前相同。该次超时未再次复现，根因未证实，不能据此声称测试不存在偶发问题。此限制不影响六项 Runtime 发现已由对应回归覆盖的结论。

日志：`output/e-fix-evidence-2026-09-11/`。原始审查 FAIL 报告和失败日志保留为修复前证据；本记录描述修复后的工作树。
