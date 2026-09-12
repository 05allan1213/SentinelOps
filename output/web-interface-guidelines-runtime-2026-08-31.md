# F-08 Web Interface Guidelines — 2026-09-11

来源 https://raw.githubusercontent.com/vercel-labs/web-interface-guidelines/main/command.md；审查前重新下载，副本位于 f-evidence-2026-09-11/web-interface-guidelines-source.md。

初次审查（修复前）：

| Finding | affected file/component | severity | fix | verification | status |
| --- | --- | --- | --- | --- | --- |
| WG1 tablist 缺少键盘方向键、tab/panel 关联 | RunDetailTabs / detail | P1 | roving tabindex、Arrow/Home/End、aria-controls/labelledby | keyboard regression | PASS |
| WG2 主内容无 skip link | Layout | P2 | 键盘可见 skip link、main focus target | keyboard regression | PASS |
| WG3 代码复制失败无反馈、timer 未清理，长代码区键盘滚动不可达 | CodeBlock / MarkdownRenderer | P2 | live feedback、cleanup、focusable scroll regions | unit/browser markdown | PASS |
| WG4 Recovery/筛选原生输入缺少 name/autocomplete，dialog 滚动链未限定 | RecoveryDialog / filters / dialog | P2 | 明确输入用途、autoComplete off、overscroll contain | DOM assertions | PASS |
| WG5 Runtime 行/返回使用 button navigation，无法新标签打开 | RuntimeRunsTable / detail | P2 | 保留视觉，用 Link 表达导航 | navigation href tests | PASS |
| WG6 Chat icon-only 按钮部分仅 title，focus 无统一后备，transition-all | chat / CSS | P2 | accessible names、focus-visible、精确过渡属性 | keyboard + static scan | PASS |

无障碍规则与冻结产品要求冲突时以本计划为准：Recovery 必须在 reason/generation 无效时禁用提交；不会为通用“submit stays enabled”建议放开此门。F20 移动端 OUT OF SCOPE。

安全检查：Markdown 不允许 raw HTML、危险协议不可点击、图片 https/lazy/no-referrer/dimensions；现有测试继续运行。表格均局部滚动，query 每页有界；颜色语义不更改，日期使用 locale 格式。主题维持 html.dark 与 store 一致；应用工作区是混合明暗界面，按面板实际背景设置原生控件颜色，不能把白色 Runtime 强行转成全暗。

复查：f08-browser.log 13/13 PASS，涵盖 skip-link/主内容焦点、Tab Arrow/Home/End、labelled panel、native select、Run 链接、Markdown 安全与 Dialog；f08-unit.log 16 文件/288 PASS，额外 clipboard denial 用例随最终单测执行。f08-lint.log 0 errors / 67 既有 warnings；f08-build.log PASS。Runtime gray-500 = #667085，白底对比约 4.97:1。Markdown 链接/图片原有安全边界完整保留。

审查范围补充：Runtime 的旧 CustomSelect 不具备原生 select 键盘语义，已在 RuntimeRunFilters 局部替换为原生 select，未引入其他 primitive。ChatInput 的上传/发送、消息编辑/反馈和 Sidebar 收起按钮补齐 accessible name。API/DTO/Recovery legality 无改动。

impeccable detector 仅报告 RunDetailTabs 的 border-b-2 + rounded tab；这是活动 Tab 指示线，不是 rounded card accent，保留既有 Tab 语义，记录为误报，非未解决缺陷。未重复运行 detector。

F20 OUT OF SCOPE。F14 NEEDS PRODUCT DECISION。审查没有新增其他产品例外。
