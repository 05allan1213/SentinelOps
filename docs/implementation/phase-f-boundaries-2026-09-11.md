# Phase F 范围和前置门

E 完成确认：当前 main 基线 a1e80e7 包含 E-00..E-07 和 03ccdad 六项审查修复；参考 phase-e-review-fixes-2026-09-11.md、output/e-phase-repair-2026-09-11.md。31/31 Runtime 浏览器通过；Chat 曾一次超时，随后 6/6 通过，本阶段重新验证。

F-03 Header 决定：leave Header unmounted。Runtime 已有页面标题、导航和 Run 上下文；现有 Layout 不挂载 Header，不需要第二个顶栏。Header/Layout 无本任务修改。

F-03 /logs：NEEDS PRODUCT DECISION。保留文件和现有路由状态；不新增导航、不注册路由、不删除文件，也不生成 logs-route-decision-2026-08-31.md。此项不阻断 Runtime 核心。

F-01 基线：真实宿主 Vite 重跑原始源码，1280 主内容横向溢出；1440 通过。F-02 缺失/空/404 dashboard 均失败。原始日志 output/f-evidence-2026-09-11/baseline-browser-valid.log。较早日志中的缺包/连接拒绝只属于环境故障。修复后联合 10/10 PASS，构建 PASS。

仅 F-01/F-02/F-03/F-05/F-06/F-07/F-08/F-09/F-10；无 F-04；不 push、不合并、不开始后续阶段；移动端及应用镜像 OUT OF SCOPE。
