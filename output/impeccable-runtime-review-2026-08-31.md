# F-07 Operate polish — 2026-09-11

保留 E-00 visual direction：深色 Sidebar、靛蓝、白色高密度 Runtime 工作区、原状态语义和现有字体；不做新视觉体系。impeccable context/polish/craft-floor 已读取；缺少 PRODUCT.md 不阻塞现有界面的局部修复。

初始源码/浏览器发现（修复前记录）：

| Finding | affected file/component | severity | fix | verification | status |
| --- | --- | --- | --- | --- | --- |
| IP1 长 reason_code 使用固定高度、不换行，容易超出质量状态区域 | RuntimeQualityState | P2 | 可换行、最小高度与局部宽度 | visual long reason cases 1280/1440 | PASS |
| IP2 Runtime/Chat 缺少整体 reduced-motion 边界，按钮过渡和 pending spinner 未统一响应偏好 | CSS / Layout | P2 | 两个工作区及 Recovery dialog 的 scoped reduced-motion | media emulation + terminal checks | PASS |
| IP3 Operation 查询错误文案说可重试但没有按钮，旧数据刷新失败无状态提醒 | OperationProgress | P1 | 可执行重试、保留旧值并标明刷新错误 | operation-error browser | PASS |
| IP4 Runtime 次要文字 gray-500 在白底对比不足，focus 样式不一致 | CSS Runtime scope | P2 | 局部次要色与统一可见 focus | computed contrast + keyboard cases | PASS |

表格/Timeline/状态 badge 的既有紧凑布局保留；没有另造 card 系统、字体或动效库。证据写入 output/playwright/runtime-1280 和 runtime-1440；只验收桌面。

验证：f07-baseline.log 在两尺寸复现长 reason overflow；f07-browser.log 10/10 PASS，包含 Operation 错误重试/保留旧状态，failed/parked/reconciling/succeeded、Timeline 空和不可用、reduced-motion。f07-build.log PASS。已人工查看 1280 baseline/failed.png 和 final/failed.png，文字换行保留完整内容，未隐藏页面溢出。IP4 对比数值与最终键盘检查由紧随的 F-08 共用断言补充。
