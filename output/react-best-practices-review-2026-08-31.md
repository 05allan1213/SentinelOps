# F-06 React review — 2026-09-11

基线 b2b7c70；检查当前 React/Vite 实现，Next.js/RSC 特有建议不应用。初始发现记录于修复前。

| Finding | affected file/component | severity | fix | verification | status |
| --- | --- | --- | --- | --- | --- |
| RP1 所有页面静态导入，主包 2311.31 kB / gzip 713.01 kB，Runtime/Markdown/图表影响首次加载 | App.tsx | P1 | 静态可分析的 React.lazy 路由边界，保留轻量 Login/布局 | build bundle + navigation | PASS |
| RP2 Runtime 已有局部 PanelBoundary，但路由渲染异常或 chunk 加载失败缺少共同边界 | Layout.tsx / route content | P1 | 唯一路由错误边界，保留 Sidebar，显式重试 | component failure/retry assertion | PASS |
| RP3 RAG refresh 重复 mount 触发、旧请求可覆盖新筛选、后台仍轮询 | rag-eval/index.tsx | P2 | 清除重复 effect、版本隔离旧响应、隐藏时暂停刷新 | race/visibility browser case | PASS |

已核查：Runtime server state 在 TanStack Query，Zustand 只保存 UI/auth；完整 query keys 包含全部参数；detail 无 previous placeholder；Recovery 无 optimistic success；唯一 QueryClient。Runtime/Chat 使用既有 api.ts client；历史 ingest.ts 是另一种 API-key 鉴权客户端，F 未创建或迁移它，不能声称全仓库只有一个 Axios 实例。

SSE：useSSECursor 的 EffectEvent 读取最新 handler，source cleanup 中先 active=false 再 unbind/abort；游标按 Run 隔离且单调；Runtime server status 决定关闭，历史 Effect/parked 不决定成功；visibility 刷新与 reader 恢复各有职责。基线已有跨 Run、同 Run recovery、operation 重复终结回归。

责任/派生状态：Runtime Tabs 分组件、DTO 在 service 层，无 Zustand server mirror；Chat 使用既有本地会话和消息记录，不复制 Runtime server status。Markdown 纯展示，流式 fence 增量扫描，代码复制原文，未知语言降级。Layout 订阅可收窄，但未观察需要额外 memo 的性能缺陷，不机械加 memo。

错误/loading/partial/accessibility/cleanup：E 六项修复及 F-05 回归覆盖查询错误保留旧数据、分页、focus/ESC/portal；本轮 RP2 补齐路由错误；后续 F-07/F-08 继续视觉及可访问性审查。

最终验证：f06-unit.log 16 文件/288 用例 PASS；f06-browser-recheck.log 26/26 PASS；f06-race-dialog.log 6/6 PASS（包含迟到旧响应、503 保留数据、Dialog focus）；f06-final-lint.log 0 errors/67 既有 warnings；f06-final-build.log PASS。主入口约 299 KB，Markdown 约 237 KB，图表独立约 1133 KB，图表大 chunk warning 保留。F-05 alias 改为同一 primitive 的组件包装，消除新增 Fast Refresh warning。最早 f06-browser.log 服务已退出，连接拒绝为环境失败；recheck 由 Playwright 管理 Vite 生命周期。
